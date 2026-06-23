package session

import (
	"context"
	"time"

	"github.com/jmoiron/sqlx"
	loomsqlite "github.com/masterkeysrd/loom/checkpoint/sqlite"
	coredb "github.com/masterkeysrd/tasksmith/internal/core/db"
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
	query := `INSERT INTO sessions (id, title, agent_name, provider_name, model_name, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`
	_, err := s.db.ExecContext(ctx, query, sd.ID, sd.Title, sd.AgentName, sd.ProviderName, sd.ModelName, sd.CreatedAt, sd.UpdatedAt)
	return err
}

func (s *sqliteStore) ListSessions(ctx context.Context) ([]SessionData, error) {
	var sessions []SessionData
	query := `SELECT id, title, agent_name, provider_name, model_name, todos, created_at, updated_at FROM sessions WHERE archived_at IS NULL ORDER BY updated_at DESC`
	err := s.db.SelectContext(ctx, &sessions, query)
	return sessions, err
}

func (s *sqliteStore) GetSession(ctx context.Context, id string) (*SessionData, error) {
	var sd SessionData
	query := `SELECT id, title, agent_name, provider_name, model_name, todos, created_at, updated_at FROM sessions WHERE id = ?`
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
	query := `UPDATE sessions SET agent_name = ?, provider_name = ?, model_name = ?, updated_at = ? WHERE id = ?`
	_, err := s.db.ExecContext(ctx, query, cfg.AgentName, cfg.ProviderName, cfg.ModelName, time.Now().UTC(), id)
	return err
}

func (s *sqliteStore) UpdateSessionTodos(ctx context.Context, id string, todosJSON string) error {
	query := `UPDATE sessions SET todos = ?, updated_at = ? WHERE id = ?`
	_, err := s.db.ExecContext(ctx, query, todosJSON, time.Now().UTC(), id)
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
	query := `SELECT id, session_id, role, content, created_at FROM messages WHERE session_id = ? ORDER BY id ASC`
	err := s.db.SelectContext(ctx, &messages, query, sessionID)
	return messages, err
}

func (s *sqliteStore) NewCheckpointer() (*loomsqlite.Checkpointer, error) {
	return loomsqlite.NewCheckpointer(s.checkDB.DB)
}
