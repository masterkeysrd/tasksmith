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
