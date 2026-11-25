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
	// 3: Add forked_from provenance column. Fork provenance deliberately does
	// not reuse parent_id: a fork is a top-level thread, not a subagent, so it
	// must stay outside the parent/child tree used for delegation and cascade
	// deletion.
	`ALTER TABLE sessions ADD COLUMN forked_from TEXT NOT NULL DEFAULT '';`,
	// 4: Index (parent_id, work_dir, updated_at) so per-subproject `--continue`
	// lookups stay fast across multi-repo workspaces.
	`CREATE INDEX IF NOT EXISTS idx_sessions_parent_work_dir ON sessions(parent_id, work_dir, updated_at);`,
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
