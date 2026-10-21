package sqlite

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpen(t *testing.T) {
	ctx := context.Background()

	// 1. Reject empty or whitespace paths
	if _, err := Open(ctx, ""); !errors.Is(err, ErrInvalidPath) {
		t.Errorf("Open empty path error = %v, want %v", err, ErrInvalidPath)
	}
	if _, err := Open(ctx, "   "); !errors.Is(err, ErrInvalidPath) {
		t.Errorf("Open whitespace path error = %v, want %v", err, ErrInvalidPath)
	}

	// 2. Open file path in a nested directory that does not exist yet
	nestedDir := filepath.Join(t.TempDir(), "nested", "sub")
	dbPath := filepath.Join(nestedDir, "test.db")

	db, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Open(%q) error: %v", dbPath, err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	if _, err := os.Stat(nestedDir); err != nil {
		t.Fatalf("expected nested directory %q to be created: %v", nestedDir, err)
	}

	var fkEnabled int
	if err := db.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&fkEnabled); err != nil {
		t.Fatalf("query PRAGMA foreign_keys: %v", err)
	}
	if fkEnabled != 1 {
		t.Errorf("PRAGMA foreign_keys = %d, want 1", fkEnabled)
	}

	var journalMode string
	if err := db.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&journalMode); err != nil {
		t.Fatalf("query PRAGMA journal_mode: %v", err)
	}
	if !strings.EqualFold(journalMode, "wal") {
		t.Errorf("PRAGMA journal_mode = %q, want \"wal\"", journalMode)
	}

	// 3. Open :memory: and verify ON DELETE CASCADE works automatically
	memDB, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("Open(:memory:) error: %v", err)
	}
	t.Cleanup(func() {
		_ = memDB.Close()
	})

	ddl := `
CREATE TABLE parents (id TEXT PRIMARY KEY);
CREATE TABLE children (
    id TEXT PRIMARY KEY,
    parent_id TEXT NOT NULL,
    FOREIGN KEY (parent_id) REFERENCES parents(id) ON DELETE CASCADE
);
INSERT INTO parents (id) VALUES ('p1');
INSERT INTO children (id, parent_id) VALUES ('c1', 'p1'), ('c2', 'p1');
`
	if _, err := memDB.ExecContext(ctx, ddl); err != nil {
		t.Fatalf("setup cascade tables: %v", err)
	}

	if _, err := memDB.ExecContext(ctx, `DELETE FROM parents WHERE id = 'p1'`); err != nil {
		t.Fatalf("delete parent: %v", err)
	}

	var childCount int
	if err := memDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM children`).Scan(&childCount); err != nil {
		t.Fatalf("count children: %v", err)
	}
	if childCount != 0 {
		t.Errorf("expected 0 children after parent deletion with ON DELETE CASCADE, got %d", childCount)
	}
}

func TestMigrate(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "migrate.db"))
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	// 1. Validation errors
	if err := Migrate(ctx, nil, "session", nil); !errors.Is(err, ErrNilDB) {
		t.Errorf("Migrate nil DB error = %v, want %v", err, ErrNilDB)
	}
	for _, invalidScope := range []string{"", "   ", "bad-scope", "bad.scope", "bad;DROP TABLE x;", "scope$"} {
		if err := Migrate(ctx, db, invalidScope, nil); !errors.Is(err, ErrInvalidScope) {
			t.Errorf("Migrate(%q) error = %v, want %v", invalidScope, err, ErrInvalidScope)
		}
	}

	// 2. Initial migration run
	v1Migrations := []string{
		`CREATE TABLE items (id TEXT PRIMARY KEY, name TEXT NOT NULL);`,
		`CREATE INDEX idx_items_name ON items(name);`,
	}
	if err := Migrate(ctx, db, "items_scope", v1Migrations); err != nil {
		t.Fatalf("initial Migrate error: %v", err)
	}

	// 3. Idempotency on second run with identical slice
	if err := Migrate(ctx, db, "items_scope", v1Migrations); err != nil {
		t.Fatalf("idempotent Migrate error: %v", err)
	}

	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM items_scope_migrations`).Scan(&count); err != nil {
		t.Fatalf("count items_scope_migrations: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 migration rows, got %d", count)
	}

	// 4. Incremental migration when a new step is appended
	v2Migrations := append([]string{}, v1Migrations...)
	v2Migrations = append(v2Migrations, `ALTER TABLE items ADD COLUMN tag TEXT NOT NULL DEFAULT 'default';`)

	if err := Migrate(ctx, db, "items_scope", v2Migrations); err != nil {
		t.Fatalf("incremental Migrate error: %v", err)
	}

	if _, err := db.ExecContext(ctx, `INSERT INTO items (id, name, tag) VALUES ('1', 'alpha', 'v2')`); err != nil {
		t.Fatalf("insert into migrated items table: %v", err)
	}

	// 5. Rollback on broken SQL migration
	brokenMigrations := append([]string{}, v2Migrations...)
	brokenMigrations = append(brokenMigrations, `
INSERT INTO items (id, name, tag) VALUES ('temp', 'should-rollback', 'bad');
THIS IS INVALID SQL SYNTAX;
`)
	if err := Migrate(ctx, db, "items_scope", brokenMigrations); err == nil {
		t.Fatal("expected error from broken SQL migration, got nil")
	}

	var tempCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM items WHERE id = 'temp'`).Scan(&tempCount); err != nil {
		t.Fatalf("check rolled-back row: %v", err)
	}
	if tempCount != 0 {
		t.Errorf("expected failed migration transaction to roll back 'temp' row, got count=%d", tempCount)
	}

	var maxVersion int
	if err := db.QueryRowContext(ctx, `SELECT MAX(version) FROM items_scope_migrations`).Scan(&maxVersion); err != nil {
		t.Fatalf("query max version: %v", err)
	}
	if maxVersion != 2 {
		t.Errorf("expected max applied version to remain 2 after failed step 3, got %d", maxVersion)
	}

	// 6. Multiple independent scopes on the same *sql.DB
	otherScopeMigrations := []string{
		`CREATE TABLE logs (seq INTEGER PRIMARY KEY AUTOINCREMENT, msg TEXT NOT NULL);`,
	}
	if err := Migrate(ctx, db, "audit_scope", otherScopeMigrations); err != nil {
		t.Fatalf("Migrate second scope error: %v", err)
	}

	var auditCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_scope_migrations`).Scan(&auditCount); err != nil {
		t.Fatalf("count audit_scope_migrations: %v", err)
	}
	if auditCount != 1 {
		t.Errorf("expected 1 row in audit_scope_migrations, got %d", auditCount)
	}
}
