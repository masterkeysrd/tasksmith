package metrics

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/sqlite"
)

// MigrationScope is the [sqlite.Migrate] scope of the metrics tables.
const MigrationScope = "metrics"

// TimeLayout is the fixed-width UTC layout used for created_at so that text
// ordering is chronological.
const TimeLayout = "2006-01-02T15:04:05.000000000Z"

const (
	queueSize = 4096
	batchSize = 128
)

// ErrClosed is returned by [Store.Record] after [Store.Close].
var ErrClosed = errors.New("metrics: store is closed")

var migrations = []string{`
CREATE TABLE IF NOT EXISTS metrics_records (
    id                TEXT PRIMARY KEY,
    created_at        TEXT NOT NULL,
    kind              TEXT NOT NULL,
    status            TEXT NOT NULL,
    workspace_root    TEXT NOT NULL DEFAULT '',
    session_id        TEXT NOT NULL DEFAULT '',
    parent_session_id TEXT NOT NULL DEFAULT '',
    turn_id           TEXT NOT NULL DEFAULT '',
    message_id        TEXT NOT NULL DEFAULT '',
    agent             TEXT NOT NULL DEFAULT '',
    attrs             TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS metrics_records_session   ON metrics_records (session_id, created_at);
CREATE INDEX IF NOT EXISTS metrics_records_parent    ON metrics_records (parent_session_id) WHERE parent_session_id != '';
CREATE INDEX IF NOT EXISTS metrics_records_workspace ON metrics_records (workspace_root, created_at);
CREATE INDEX IF NOT EXISTS metrics_records_kind_time ON metrics_records (kind, created_at);

CREATE TABLE IF NOT EXISTS metrics_measurements (
    record_id TEXT    NOT NULL REFERENCES metrics_records(id) ON DELETE CASCADE,
    metric    TEXT    NOT NULL,
    value     INTEGER NOT NULL,
    PRIMARY KEY (record_id, metric)
);

CREATE INDEX IF NOT EXISTS metrics_measurements_metric ON metrics_measurements (metric);
`}

type item struct {
	entry Entry
	flush chan struct{}
}

// Store is the asynchronous SQLite [Recorder]. Records are queued in memory
// and written in batched transactions by a background goroutine.
type Store struct {
	db     *sql.DB
	logger log.Interface

	mu     sync.RWMutex
	closed bool
	queue  chan item
	done   chan struct{}
}

var _ Recorder = (*Store)(nil)

// NewStore migrates the metrics schema on db and starts the background writer.
// The caller keeps ownership of db and must call [Store.Close] before closing it.
func NewStore(ctx context.Context, db *sql.DB, logger log.Interface) (*Store, error) {
	if db == nil {
		return nil, sqlite.ErrNilDB
	}
	if err := sqlite.Migrate(ctx, db, MigrationScope, migrations); err != nil {
		return nil, fmt.Errorf("metrics: migrate: %w", err)
	}
	s := &Store{
		db:     db,
		logger: logger.ForComponent("metrics"),
		queue:  make(chan item, queueSize),
		done:   make(chan struct{}),
	}
	go s.run()
	return s, nil
}

// DB returns the underlying database handle.
func (s *Store) DB() *sql.DB { return s.db }

// Record attributes r from the scope in ctx and queues it for writing. It
// never blocks: when the queue is full the record is dropped with a warning.
func (s *Store) Record(ctx context.Context, r Record) error {
	return s.Write(NewEntry(ctx, r))
}

// Write queues an already attributed entry.
func (s *Store) Write(e Entry) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return ErrClosed
	}
	select {
	case s.queue <- item{entry: e}:
	default:
		s.logger.Warn("metrics queue full; dropping record", log.String("kind", string(e.Kind)))
	}
	return nil
}

// Flush blocks until every record queued before the call is written.
func (s *Store) Flush(ctx context.Context) error {
	ack := make(chan struct{})
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return ErrClosed
	}
	select {
	case s.queue <- item{flush: ack}:
		s.mu.RUnlock()
	case <-ctx.Done():
		s.mu.RUnlock()
		return ctx.Err()
	}
	select {
	case <-ack:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close flushes the queue and stops the writer. It does not close the database.
func (s *Store) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	close(s.queue)
	s.mu.Unlock()
	<-s.done
	return nil
}

func (s *Store) run() {
	defer close(s.done)
	for first := range s.queue {
		batch := []Entry{}
		var acks []chan struct{}
		collect := func(it item) {
			if it.flush != nil {
				acks = append(acks, it.flush)
				return
			}
			batch = append(batch, it.entry)
		}
		collect(first)
	drain:
		for len(batch) < batchSize {
			select {
			case it, ok := <-s.queue:
				if !ok {
					break drain
				}
				collect(it)
			default:
				break drain
			}
		}
		if err := s.insert(batch); err != nil {
			s.logger.Warn("metrics write failed", log.String("error", err.Error()), log.Int("records", len(batch)))
		}
		for _, ack := range acks {
			close(ack)
		}
	}
}

func (s *Store) insert(batch []Entry) error {
	if len(batch) == 0 {
		return nil
	}
	ctx := context.Background()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	recStmt, err := tx.PrepareContext(ctx, `INSERT INTO metrics_records
(id, created_at, kind, status, workspace_root, session_id, parent_session_id, turn_id, message_id, agent, attrs)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer recStmt.Close()
	mStmt, err := tx.PrepareContext(ctx, `INSERT OR REPLACE INTO metrics_measurements (record_id, metric, value) VALUES (?, ?, ?)`)
	if err != nil {
		return err
	}
	defer mStmt.Close()

	for _, e := range batch {
		attrs := make(map[string]string, len(e.Attrs))
		for _, a := range e.Attrs {
			attrs[a.Key] = a.Value
		}
		raw, err := json.Marshal(attrs)
		if err != nil {
			return err
		}
		if _, err := recStmt.ExecContext(ctx, e.ID, e.CreatedAt.UTC().Format(TimeLayout), string(e.Kind), string(e.Status),
			e.WorkspaceRoot, e.SessionID, e.ParentSessionID, e.TurnID, e.MessageID, e.Agent, string(raw)); err != nil {
			return err
		}
		for _, m := range e.Measurements {
			if _, err := mStmt.ExecContext(ctx, e.ID, string(m.Metric), m.Value); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func decodeAttrs(raw string) ([]Attr, error) {
	var m map[string]string
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil, fmt.Errorf("metrics: decode attrs: %w", err)
	}
	out := make([]Attr, 0, len(m))
	for k, v := range m {
		out = append(out, Attr{Key: k, Value: v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}
