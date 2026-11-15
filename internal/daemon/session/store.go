package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
)

// timestampFormat is a fixed-width UTC RFC3339Nano layout so SQLite TEXT
// ordering (ORDER BY updated_at DESC, created_at DESC) matches chronological order.
const timestampFormat = "2006-01-02T15:04:05.000000000Z"

// ListFilter specifies filtering criteria for listing sessions.
type ListFilter struct {
	ParentID *string
}

// Store persists session metadata in SQLite.
type Store interface {
	Save(ctx context.Context, sess *Session) error
	Get(ctx context.Context, id string) (*Session, error)
	List(ctx context.Context, filter ListFilter) ([]*Session, error)
	Delete(ctx context.Context, id string) error
}

type sqliteStore struct {
	db *sql.DB
}

var _ Store = (*sqliteStore)(nil)

// NewStore initializes a SQLite-backed Store and runs schema migrations.
func NewStore(ctx context.Context, db *sql.DB) (Store, error) {
	if err := Migrate(ctx, db); err != nil {
		return nil, err
	}
	return &sqliteStore{db: db}, nil
}

func (s *sqliteStore) Save(ctx context.Context, sess *Session) error {
	if sess == nil {
		return errors.New("session: nil session")
	}

	snap := sess.Clone()
	if snap.ID == "" {
		return ErrEmptySessionID
	}
	createdAt := snap.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	updatedAt := snap.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = createdAt
	}

	configBytes, err := json.Marshal(snap.Config)
	if err != nil {
		return fmt.Errorf("session: marshal config: %w", err)
	}
	permsBytes, err := json.Marshal(snap.Permissions)
	if err != nil {
		return fmt.Errorf("session: marshal permissions: %w", err)
	}

	query := `INSERT INTO sessions (id, title, config, permissions, parent_id, work_dir, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    title       = excluded.title,
    config      = excluded.config,
    permissions = excluded.permissions,
    parent_id   = excluded.parent_id,
    work_dir    = excluded.work_dir,
    updated_at  = excluded.updated_at`

	_, err = s.db.ExecContext(
		ctx,
		query,
		snap.ID,
		snap.Title,
		string(configBytes),
		string(permsBytes),
		snap.ParentID,
		snap.WorkDir,
		formatTime(createdAt),
		formatTime(updatedAt),
	)
	if err != nil {
		return fmt.Errorf("session: save session %q: %w", snap.ID, err)
	}
	return nil
}

func (s *sqliteStore) Get(ctx context.Context, id string) (*Session, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT id, title, config, permissions, parent_id, work_dir, created_at, updated_at FROM sessions WHERE id = ?`,
		id,
	)
	sess, err := scanSession(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrSessionNotFound
		}
		return nil, fmt.Errorf("session: get session %q: %w", id, err)
	}
	return sess, nil
}

func (s *sqliteStore) List(ctx context.Context, filter ListFilter) ([]*Session, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if filter.ParentID != nil {
		rows, err = s.db.QueryContext(
			ctx,
			`SELECT id, title, config, permissions, parent_id, work_dir, created_at, updated_at FROM sessions WHERE parent_id = ? ORDER BY updated_at DESC, id DESC`,
			*filter.ParentID,
		)
	} else {
		rows, err = s.db.QueryContext(
			ctx,
			`SELECT id, title, config, permissions, parent_id, work_dir, created_at, updated_at FROM sessions ORDER BY updated_at DESC, id DESC`,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("session: list sessions: %w", err)
	}
	defer rows.Close()

	sessions := make([]*Session, 0)
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, fmt.Errorf("session: scan session row: %w", err)
		}
		sessions = append(sessions, sess)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("session: iterate sessions: %w", err)
	}
	return sessions, nil
}

func (s *sqliteStore) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("session: delete session %q: %w", id, err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("session: check rows affected for session %q: %w", id, err)
	}
	if affected == 0 {
		return ErrSessionNotFound
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanSession(r rowScanner) (*Session, error) {
	var (
		sess         Session
		configRaw    string
		permsRaw     string
		createdAtRaw string
		updatedAtRaw string
	)
	if err := r.Scan(&sess.ID, &sess.Title, &configRaw, &permsRaw, &sess.ParentID, &sess.WorkDir, &createdAtRaw, &updatedAtRaw); err != nil {
		return nil, err
	}

	if configRaw != "" {
		if err := json.Unmarshal([]byte(configRaw), &sess.Config); err != nil {
			return nil, fmt.Errorf("parse config: %w", err)
		}
	}
	if sess.Config.Mode == "" {
		sess.Config.Mode = api.ModeDefault
	}

	if permsRaw != "" {
		if err := json.Unmarshal([]byte(permsRaw), &sess.Permissions); err != nil {
			return nil, fmt.Errorf("parse permissions: %w", err)
		}
	}

	createdAt, err := parseTime(createdAtRaw)
	if err != nil {
		return nil, fmt.Errorf("parse created_at %q: %w", createdAtRaw, err)
	}
	updatedAt, err := parseTime(updatedAtRaw)
	if err != nil {
		return nil, fmt.Errorf("parse updated_at %q: %w", updatedAtRaw, err)
	}

	sess.CreatedAt = createdAt
	sess.UpdatedAt = updatedAt
	sess.State = NewState()
	return &sess, nil
}

func formatTime(t time.Time) string {
	return t.UTC().Format(timestampFormat)
}

func parseTime(raw string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}
