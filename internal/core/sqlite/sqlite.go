package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// Sentinel errors returned by the sqlite package.
var (
	ErrInvalidPath  = errors.New("sqlite: database path must not be empty")
	ErrNilDB        = errors.New("sqlite: database connection must not be nil")
	ErrInvalidScope = errors.New("sqlite: migration scope must be a non-empty alphanumeric/underscore identifier")
)

const pragmaQuery = "_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)"

// Open opens a SQLite database at path with standard production pragmas
// (`busy_timeout=5000`, `journal_mode=WAL`, `foreign_keys=ON`, `synchronous=NORMAL`)
// applied to all connections in the pool.
func Open(ctx context.Context, path string) (*sql.DB, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, ErrInvalidPath
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	isMemory := path == ":memory:"
	if !isMemory && !strings.HasPrefix(path, "file:") {
		dir := filepath.Dir(path)
		if dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return nil, fmt.Errorf("sqlite: create database directory %q: %w", dir, err)
			}
		}
	}

	dsn := buildDSN(path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("sqlite: open %q: %w", path, err)
	}

	if isMemory {
		db.SetMaxOpenConns(1)
	}

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlite: ping %q: %w", path, err)
	}

	if _, err := db.ExecContext(ctx, `PRAGMA foreign_keys = ON;`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlite: enable foreign_keys on %q: %w", path, err)
	}

	return db, nil
}

// Migrate executes any unapplied SQL migrations in migrations for the given
// subsystem scope, tracking applied indices in `<scope>_migrations`.
func Migrate(ctx context.Context, db *sql.DB, scope string, migrations []string) error {
	if db == nil {
		return ErrNilDB
	}
	scope = strings.TrimSpace(scope)
	if !isValidScope(scope) {
		return ErrInvalidScope
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	tableName := scope + "_migrations"
	createTableSQL := fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS %s (
    version INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL DEFAULT (strftime('%%Y-%%m-%%dT%%H:%%M:%%SZ', 'now'))
);`, tableName)

	if _, err := db.ExecContext(ctx, createTableSQL); err != nil {
		return fmt.Errorf("sqlite: create %s table: %w", tableName, err)
	}

	var currentVersion int
	queryVersionSQL := fmt.Sprintf(`SELECT version FROM %s ORDER BY version DESC LIMIT 1`, tableName)
	err := db.QueryRowContext(ctx, queryVersionSQL).Scan(&currentVersion)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			currentVersion = -1
		} else {
			return fmt.Errorf("sqlite: query %s version: %w", tableName, err)
		}
	}

	insertVersionSQL := fmt.Sprintf(`INSERT INTO %s (version) VALUES (?)`, tableName)
	for i := currentVersion + 1; i < len(migrations); i++ {
		if err := applyMigration(ctx, db, scope, insertVersionSQL, i, migrations[i]); err != nil {
			return err
		}
	}

	return nil
}

func applyMigration(ctx context.Context, db *sql.DB, scope, insertVersionSQL string, version int, ddl string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlite: begin %s migration %d: %w", scope, version, err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if _, err := tx.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("sqlite: apply %s migration %d: %w", scope, version, err)
	}

	if _, err := tx.ExecContext(ctx, insertVersionSQL, version); err != nil {
		return fmt.Errorf("sqlite: record %s migration %d: %w", scope, version, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sqlite: commit %s migration %d: %w", scope, version, err)
	}
	return nil
}

func buildDSN(path string) string {
	if strings.Contains(path, "?") {
		return path + "&" + pragmaQuery
	}
	return path + "?" + pragmaQuery
}

func isValidScope(scope string) bool {
	if scope == "" {
		return false
	}
	for i := 0; i < len(scope); i++ {
		c := scope[i]
		isAlpha := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		isDigit := c >= '0' && c <= '9'
		if !isAlpha && !isDigit && c != '_' {
			return false
		}
	}
	return true
}
