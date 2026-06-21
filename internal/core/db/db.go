package db

import (
	"database/sql"
	"fmt"
	"path/filepath"

	"github.com/jmoiron/sqlx"
	"github.com/masterkeysrd/tasksmith/internal/core/fsutil"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	_ "modernc.org/sqlite"
)

// Open opens a SQLite connection using sqlx, sets tuning pragmas, and returns the DB.
func Open(workspacePath, filename string) (*sqlx.DB, error) {
	wsDir, err := xdg.WorkspaceDir(workspacePath)
	if err != nil {
		return nil, fmt.Errorf("failed to get workspace dir: %w", err)
	}

	if err := fsutil.EnsureDir(wsDir); err != nil {
		return nil, fmt.Errorf("failed to ensure workspace dir: %w", err)
	}

	dbPath := filepath.Join(wsDir, filename)
	db, err := sqlx.Connect("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Apply tuning pragmas for SQLite (WAL, busy timeout, and Foreign Keys)
	pragmas := []string{
		"PRAGMA journal_mode = WAL;",
		"PRAGMA foreign_keys = ON;",
		"PRAGMA busy_timeout = 5000;",
	}
	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("failed to set pragma %q: %w", pragma, err)
		}
	}

	return db, nil
}

// Migrate applies sequential SQL statements for a given group in the schema_migrations table.
// Each query inside the migrations slice is version-tracked by index (0-indexed).
func Migrate(db *sqlx.DB, group string, migrations []string) error {
	setupQuery := `
	CREATE TABLE IF NOT EXISTS schema_migrations (
		group_name TEXT NOT NULL,
		version INTEGER NOT NULL,
		applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (group_name, version)
	);
	`
	if _, err := db.Exec(setupQuery); err != nil {
		return fmt.Errorf("failed to create migrations table: %w", err)
	}

	var currentVersion int = -1
	err := db.Get(&currentVersion, "SELECT COALESCE(MAX(version), -1) FROM schema_migrations WHERE group_name = ?", group)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("failed to retrieve migrations version: %w", err)
	}

	for i := currentVersion + 1; i < len(migrations); i++ {
		tx, err := db.Beginx()
		if err != nil {
			return fmt.Errorf("failed to start migration transaction: %w", err)
		}

		if _, err := tx.Exec(migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to run migration version %d for group %q: %w", i, group, err)
		}

		insertQuery := `INSERT INTO schema_migrations (group_name, version) VALUES (?, ?)`
		if _, err := tx.Exec(insertQuery, group, i); err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to record migration version %d for group %q: %w", i, group, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("failed to commit migration transaction: %w", err)
		}
	}

	return nil
}
