package session

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/masterkeysrd/tasksmith/internal/core/sqlite"
)

var migrations = []string{
	// 0: Initial sessions table
	`
CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL DEFAULT '',
    config TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
`,
	// 1: Add permissions column
	`ALTER TABLE sessions ADD COLUMN permissions TEXT NOT NULL DEFAULT '{}';`,
	// 2: Add parent_id and work_dir columns
	`ALTER TABLE sessions ADD COLUMN parent_id TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN work_dir TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_sessions_parent_id ON sessions(parent_id);`,
}

// Migrate executes SQLite migrations for the session schema.
func Migrate(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return ErrInvalidConfig
	}
	if err := sqlite.Migrate(ctx, db, "session", migrations); err != nil {
		return fmt.Errorf("session: migrate schema: %w", err)
	}
	return nil
}
