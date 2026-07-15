package session

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jmoiron/sqlx"
	loomsqlite "github.com/masterkeysrd/loom/checkpoint/sqlite"
	coredb "github.com/masterkeysrd/tasksmith/internal/core/db"
	"github.com/masterkeysrd/tasksmith/internal/session/resource"
)

var migrations = []string{
	`CREATE TABLE IF NOT EXISTS sessions (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		created_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL
	);`,
	`CREATE TABLE IF NOT EXISTS messages (
		id TEXT PRIMARY KEY,
		session_id TEXT NOT NULL,
		role TEXT NOT NULL,
		content TEXT NOT NULL,
		created_at DATETIME NOT NULL,
		FOREIGN KEY(session_id) REFERENCES sessions(id) ON DELETE CASCADE
	);`,
	`ALTER TABLE sessions ADD COLUMN archived_at DATETIME;`,
	`ALTER TABLE sessions ADD COLUMN agent_name TEXT;`,
	`ALTER TABLE sessions ADD COLUMN provider_name TEXT;`,
	`ALTER TABLE sessions ADD COLUMN model_name TEXT;`,
	`ALTER TABLE sessions ADD COLUMN todos TEXT DEFAULT '[]';`,
	`ALTER TABLE sessions ADD COLUMN last_turn_metrics TEXT;`,
	`CREATE TABLE IF NOT EXISTS session_resources (
		id         TEXT PRIMARY KEY,
		session_id TEXT NOT NULL,
		kind       TEXT NOT NULL,
		key        TEXT NOT NULL DEFAULT '',
		data       TEXT NOT NULL DEFAULT '{}',
		created_at DATETIME NOT NULL,
		FOREIGN KEY(session_id) REFERENCES sessions(id) ON DELETE CASCADE
	);`,
	`CREATE INDEX IF NOT EXISTS idx_sr_session_kind ON session_resources(session_id, kind);`,
	`CREATE INDEX IF NOT EXISTS idx_sr_key ON session_resources(session_id, kind, key);`,
	`ALTER TABLE sessions ADD COLUMN settings TEXT DEFAULT '{}';`,
}

type sqliteStore struct {
	db      *sqlx.DB
	checkDB *sqlx.DB
}

// NewSQLiteStore creates a new Store instance backed by SQLite.
func NewSQLiteStore(db *sqlx.DB, checkDB *sqlx.DB) (Store, error) {
	if err := coredb.Migrate(db, "session", migrations); err != nil {
		return nil, err
	}
	return &sqliteStore{db: db, checkDB: checkDB}, nil
}

func (s *sqliteStore) CreateSession(ctx context.Context, sd SessionData) error {
	query := `INSERT INTO sessions (id, title, settings, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`
	_, err := s.db.ExecContext(ctx, query, sd.ID, sd.Title, sd.Settings, sd.CreatedAt, sd.UpdatedAt)
	return err
}

func (s *sqliteStore) ListSessions(ctx context.Context, query ListSessionsQuery) ([]SessionData, error) {
	var sessions []SessionData
	sqlStr := `SELECT id, title, todos, last_turn_metrics, settings, created_at, updated_at FROM sessions WHERE archived_at IS NULL ORDER BY updated_at DESC`
	var err error
	if query.Limit > 0 {
		sqlStr += " LIMIT ? OFFSET ?"
		err = s.db.SelectContext(ctx, &sessions, sqlStr, query.Limit, query.Offset)
	} else {
		err = s.db.SelectContext(ctx, &sessions, sqlStr)
	}
	return sessions, err
}

func (s *sqliteStore) GetSession(ctx context.Context, id string) (*SessionData, error) {
	var sd SessionData
	query := `SELECT id, title, todos, last_turn_metrics, settings, created_at, updated_at FROM sessions WHERE id = ?`
	err := s.db.GetContext(ctx, &sd, query, id)
	if err != nil {
		return nil, err
	}
	return &sd, nil
}

func (s *sqliteStore) DeleteSession(ctx context.Context, id string) error {
	query := `DELETE FROM sessions WHERE id = ?`
	_, err := s.db.ExecContext(ctx, query, id)
	return err
}

func (s *sqliteStore) RenameSession(ctx context.Context, id, title string) error {
	query := `UPDATE sessions SET title = ?, updated_at = ? WHERE id = ?`
	_, err := s.db.ExecContext(ctx, query, title, time.Now().UTC(), id)
	return err
}

func (s *sqliteStore) UpdateSessionConfig(ctx context.Context, id string, cfg SessionConfig) error {
	data, err := json.Marshal(cfg.Settings)
	if err != nil {
		return err
	}
	settingsJSON := string(data)
	query := `UPDATE sessions SET settings = ?, updated_at = ? WHERE id = ?`
	_, err = s.db.ExecContext(ctx, query, settingsJSON, time.Now().UTC(), id)
	return err
}

func (s *sqliteStore) UpdateSessionTodos(ctx context.Context, id string, todosJSON string) error {
	query := `UPDATE sessions SET todos = ?, updated_at = ? WHERE id = ?`
	_, err := s.db.ExecContext(ctx, query, todosJSON, time.Now().UTC(), id)
	return err
}

func (s *sqliteStore) UpdateSessionMetrics(ctx context.Context, id string, metrics SessionMetrics) error {
	data, err := json.Marshal(metrics)
	if err != nil {
		return err
	}
	query := `UPDATE sessions SET last_turn_metrics = ?, updated_at = ? WHERE id = ?`
	_, err = s.db.ExecContext(ctx, query, string(data), time.Now().UTC(), id)
	return err
}

func (s *sqliteStore) ArchiveSession(ctx context.Context, id string) error {
	query := `UPDATE sessions SET archived_at = ? WHERE id = ?`
	_, err := s.db.ExecContext(ctx, query, time.Now().UTC(), id)
	return err
}

func (s *sqliteStore) AppendMessage(ctx context.Context, md MessageData, updatedAt time.Time) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	queryMsg := `INSERT OR REPLACE INTO messages (id, session_id, role, content, created_at) VALUES (?, ?, ?, ?, ?)`
	_, err = tx.ExecContext(ctx, queryMsg, md.ID, md.SessionID, md.Role, md.Content, md.CreatedAt)
	if err != nil {
		return err
	}

	querySess := `UPDATE sessions SET updated_at = ? WHERE id = ?`
	_, err = tx.ExecContext(ctx, querySess, updatedAt, md.SessionID)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (s *sqliteStore) GetMessages(ctx context.Context, sessionID string) ([]MessageData, error) {
	var messages []MessageData
	query := `SELECT id, session_id, role, content, created_at FROM messages WHERE session_id = ? ORDER BY created_at ASC, id ASC`
	err := s.db.SelectContext(ctx, &messages, query, sessionID)
	return messages, err
}

func (s *sqliteStore) NewCheckpointer() (*loomsqlite.Checkpointer, error) {
	return loomsqlite.NewCheckpointer(s.checkDB.DB)
}

func (s *sqliteStore) ResourceStore() *resource.Store {
	return resource.NewStore(s.db)
}
