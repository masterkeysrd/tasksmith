package db_test

import (
	"testing"

	"github.com/masterkeysrd/tasksmith/internal/core/db"
)

func TestMigrate(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("TASKSMITH_APPNAME", "db-test")

	conn, err := db.Open(tmpDir, "test.db")
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	defer conn.Close()

	migrations := []string{
		`CREATE TABLE users (id TEXT PRIMARY KEY, name TEXT);`,
		`ALTER TABLE users ADD COLUMN email TEXT;`,
	}

	// Apply migrations
	err = db.Migrate(conn, "user_module", migrations)
	if err != nil {
		t.Fatalf("failed to apply migrations: %v", err)
	}

	// Apply again (should be no-op)
	err = db.Migrate(conn, "user_module", migrations)
	if err != nil {
		t.Fatalf("re-applying migrations failed: %v", err)
	}

	// Verify table structures exist
	_, err = conn.Exec("INSERT INTO users (id, name, email) VALUES ('1', 'David', 'david@example.com')")
	if err != nil {
		t.Errorf("failed to insert into migrated table: %v", err)
	}
}
