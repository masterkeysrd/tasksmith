package filetrack

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	coredb "github.com/masterkeysrd/tasksmith/internal/core/db"
	"github.com/masterkeysrd/tasksmith/internal/session/resource"
)

var testMigrations = []string{
	`CREATE TABLE IF NOT EXISTS sessions (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		created_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL
	);`,
	`CREATE TABLE IF NOT EXISTS session_resources (
		id         TEXT PRIMARY KEY,
		session_id TEXT NOT NULL,
		kind       TEXT NOT NULL,
		key        TEXT NOT NULL DEFAULT '',
		data       TEXT NOT NULL DEFAULT '{}',
		created_at DATETIME NOT NULL,
		FOREIGN KEY(session_id) REFERENCES sessions(id) ON DELETE CASCADE
	);`,
}

func TestSessionTracker_ExternalChanges(t *testing.T) {
	dir := t.TempDir()
	db, err := coredb.Open(dir, "test.db")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	if err := coredb.Migrate(db, "test_session", testMigrations); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	sessionID := "test-session"
	_, err = db.Exec(`INSERT INTO sessions (id, title, created_at, updated_at) VALUES (?, ?, ?, ?)`, sessionID, "Test Session", time.Now(), time.Now())
	if err != nil {
		t.Fatalf("failed to insert test session: %v", err)
	}

	store := resource.NewStore(db)
	changesDir := filepath.Join(dir, "changes")

	wsTracker := NewWorkspaceTracker(dir)
	if err := wsTracker.Start(context.Background()); err != nil {
		t.Fatalf("failed to start workspace tracker: %v", err)
	}
	defer wsTracker.Stop()

	ft := NewTracker(store, sessionID, changesDir, dir, wsTracker)
	defer ft.Close()

	// 1. Create file and expect it to be recorded synchronously by agent
	filePath := "test.txt"
	absPath := filepath.Join(dir, filePath)
	content := "hello world"
	hashVal := fmt.Sprintf("%x", sha256.Sum256([]byte(content)))

	ft.ExpectWrite(filePath, hashVal)
	if err := os.WriteFile(absPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	// Write synchronous Record
	err = ft.Record(context.Background(), Change{
		ToolName:  "write",
		Path:      filePath,
		Kind:      Created,
		Additions: 1,
	}, "", "")
	if err != nil {
		t.Fatalf("Record failed: %v", err)
	}

	// Wait for any async fsnotify event to process
	time.Sleep(100 * time.Millisecond)

	// Since it matched the expected write hash, the journal should only have 2 entries (baseline + write)
	entries, err := ft.ReadJournal(context.Background(), filePath)
	if err != nil {
		t.Fatalf("ReadJournal failed: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 journal entries (baseline + write), got %d: %+v", len(entries), entries)
	}

	// 2. Perform external modification (unexpected write)
	newContent := "hello world modified externally"
	if err := os.WriteFile(absPath, []byte(newContent), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	// Wait for watcher to detect and session tracker to process
	time.Sleep(200 * time.Millisecond)

	entries, err = ft.ReadJournal(context.Background(), filePath)
	if err != nil {
		t.Fatalf("ReadJournal failed: %v", err)
	}

	// Unexpected write should be logged as "external"
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d: %+v", len(entries), entries)
	}
	if entries[2].ToolName != "external" {
		t.Errorf("expected last entry tool name to be 'external', got %q", entries[2].ToolName)
	}
}
