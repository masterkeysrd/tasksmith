package filetrack_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jmoiron/sqlx"
	coredb "github.com/masterkeysrd/tasksmith/internal/core/db"
	"github.com/masterkeysrd/tasksmith/internal/session/filetrack"
	"github.com/masterkeysrd/tasksmith/internal/session/resource"
	_ "modernc.org/sqlite"
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

func TestFileTracker(t *testing.T) {
	db, err := sqlx.Connect("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to connect to in-memory sqlite: %v", err)
	}
	defer db.Close()

	if err := coredb.Migrate(db, "session", testMigrations); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	// Insert test session
	_, err = db.Exec("INSERT INTO sessions (id, title, created_at, updated_at) VALUES ('session-1', 'Test', datetime('now'), datetime('now'))")
	if err != nil {
		t.Fatalf("failed to insert test session: %v", err)
	}

	store := resource.NewStore(db)

	tmpDir := t.TempDir()
	workspaceDir := filepath.Join(tmpDir, "workspace")
	changesDir := filepath.Join(tmpDir, "changes")

	if err := os.MkdirAll(workspaceDir, 0755); err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}

	ft := filetrack.NewTracker(store, "session-1", changesDir, workspaceDir)
	ctx := context.Background()

	// 1. Record creation of a file
	filePath := "hello.txt"
	absPath := filepath.Join(workspaceDir, filePath)
	if err := os.WriteFile(absPath, []byte("hello world"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	err = ft.Record(ctx, filetrack.Change{
		ToolName:  "write",
		Path:      filePath,
		Kind:      filetrack.Created,
		Additions: 1,
		Deletions: 0,
	}, "", "") // Created file has no diff and no oldContent
	if err != nil {
		t.Fatalf("Record failed: %v", err)
	}

	// Verify Summary shows created
	summaries, err := ft.Summary(ctx)
	if err != nil {
		t.Fatalf("Summary failed: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 file change summary, got %d", len(summaries))
	}
	if summaries[0].Path != filePath || summaries[0].Kind != filetrack.Created {
		t.Errorf("unexpected summary: %+v", summaries[0])
	}

	// Write updated content to workspace disk so dynamic diffing has the correct content
	if err := os.WriteFile(absPath, []byte("hello world\nextra line"), 0644); err != nil {
		t.Fatalf("failed to modify test file content on disk: %v", err)
	}

	// 2. Record modification of the file
	err = ft.Record(ctx, filetrack.Change{
		ToolName:  "edit",
		Path:      filePath,
		Kind:      filetrack.Modified,
		Additions: 2,
		Deletions: 1,
	}, "@@ -1 +1,2 @@\n-hello world\n+hello world\n+extra line", "hello world")
	if err != nil {
		t.Fatalf("Record failed: %v", err)
	}

	// Verify journal has entries
	entries, err := ft.ReadJournal(ctx, filePath)
	if err != nil {
		t.Fatalf("ReadJournal failed: %v", err)
	}
	if len(entries) != 3 { // baseline, created, modified
		t.Fatalf("expected 3 entries in journal, got %d", len(entries))
	}
	if entries[0].Kind != "baseline" {
		t.Errorf("expected baseline entry first, got kind %s", entries[0].Kind)
	}
	if entries[0].Content != "" {
		t.Errorf("expected baseline content for created file to be empty, got %q", entries[0].Content)
	}
	if entries[1].Kind != filetrack.Created {
		t.Errorf("expected second entry to be created, got %s", entries[1].Kind)
	}
	if entries[2].Kind != filetrack.Modified {
		t.Errorf("expected third entry to be modified, got %s", entries[2].Kind)
	}

	// Verify updated summaries
	summaries, err = ft.Summary(ctx)
	if err != nil {
		t.Fatalf("Summary failed: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 file change summary, got %d", len(summaries))
	}
	if summaries[0].Kind != filetrack.Created {
		// Even after edits, the net kind of a created file is Created
		t.Errorf("expected kind to remain Created, got %s", summaries[0].Kind)
	}
	if summaries[0].TotalEdits != 2 {
		t.Errorf("expected TotalEdits = 2, got %d", summaries[0].TotalEdits)
	}
	if summaries[0].NetAdditions != 2 { // Net additions relative to baseline of "" for hello world\nextra line is 2
		t.Errorf("expected NetAdditions = 2, got %d", summaries[0].NetAdditions)
	}

	// 3. Revert changes
	err = ft.RevertToBaseline(ctx, filePath, false)
	if err != nil {
		t.Fatalf("RevertToBaseline failed: %v", err)
	}

	// File hello.txt should be deleted because baseline content was empty (it was created)
	if _, err := os.Stat(absPath); !os.IsNotExist(err) {
		t.Errorf("expected hello.txt to be removed on revert, but it exists: %v", err)
	}

	// Summaries should be empty now
	summaries, err = ft.Summary(ctx)
	if err != nil {
		t.Fatalf("Summary failed: %v", err)
	}
	if len(summaries) != 0 {
		t.Errorf("expected 0 summaries after revert, got %d", len(summaries))
	}

	// 4. Test modifying an existing file
	filePath2 := "existing.txt"
	absPath2 := filepath.Join(workspaceDir, filePath2)
	if err := os.WriteFile(absPath2, []byte("original text"), 0644); err != nil {
		t.Fatalf("failed to write existing file: %v", err)
	}

	// Write new text to workspace
	if err := os.WriteFile(absPath2, []byte("modified text"), 0644); err != nil {
		t.Fatalf("failed to modify existing file: %v", err)
	}

	err = ft.Record(ctx, filetrack.Change{
		ToolName:  "edit",
		Path:      filePath2,
		Kind:      filetrack.Modified,
		Additions: 1,
		Deletions: 1,
	}, "diff info", "original text")
	if err != nil {
		t.Fatalf("Record failed: %v", err)
	}

	// Revert
	err = ft.RevertToBaseline(ctx, filePath2, false)
	if err != nil {
		t.Fatalf("RevertToBaseline failed: %v", err)
	}

	// Read content, should be original text
	content, err := os.ReadFile(absPath2)
	if err != nil {
		t.Fatalf("failed to read reverted file: %v", err)
	}
	if string(content) != "original text" {
		t.Errorf("expected 'original text', got %q", string(content))
	}

	// Summaries should be empty now
	summaries, err = ft.Summary(ctx)
	if err != nil {
		t.Fatalf("Summary failed: %v", err)
	}
	if len(summaries) != 0 {
		t.Errorf("expected 0 summaries after revert of existing file, got %d", len(summaries))
	}

	// 5. Test Conflict detection
	filePath3 := "conflict.txt"
	absPath3 := filepath.Join(workspaceDir, filePath3)
	if err := os.WriteFile(absPath3, []byte("agent text"), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	// Case A: No previous edit by agent, so no conflict possible
	hasConflict, err := ft.CheckConflict(ctx, filePath3)
	if err != nil {
		t.Fatalf("CheckConflict failed: %v", err)
	}
	if hasConflict {
		t.Errorf("expected no conflict before agent edits, got true")
	}

	// Agent records edit
	err = ft.Record(ctx, filetrack.Change{
		ToolName:  "write",
		Path:      filePath3,
		Kind:      filetrack.Created,
		Additions: 1,
		Deletions: 0,
	}, "", "")
	if err != nil {
		t.Fatalf("Record failed: %v", err)
	}

	// Case B: No manual changes made yet (disk hash matches agent record hash)
	hasConflict, err = ft.CheckConflict(ctx, filePath3)
	if err != nil {
		t.Fatalf("CheckConflict failed: %v", err)
	}
	if hasConflict {
		t.Errorf("expected no conflict when file matches agent edits, got true")
	}

	// Case C: User modifies file manually (disk hash now deviates from agent record hash)
	if err := os.WriteFile(absPath3, []byte("user modified text"), 0644); err != nil {
		t.Fatalf("failed to modify file: %v", err)
	}

	hasConflict, err = ft.CheckConflict(ctx, filePath3)
	if err != nil {
		t.Fatalf("CheckConflict failed: %v", err)
	}
	if !hasConflict {
		t.Errorf("expected conflict when file hash deviates, got false")
	}

	// 6. Test RecordRead and IsKnown
	filePath4 := "known_test.txt"
	absPath4 := filepath.Join(workspaceDir, filePath4)

	// Case A: File doesn't exist, has never been read or written -> not known
	known, err := ft.IsKnown(ctx, filePath4)
	if err != nil {
		t.Fatalf("IsKnown failed: %v", err)
	}
	if known {
		t.Errorf("expected file to be unknown initially")
	}

	// Case B: Create file on disk, still has never been read or written -> not known
	if err := os.WriteFile(absPath4, []byte("some content"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}
	known, err = ft.IsKnown(ctx, filePath4)
	if err != nil {
		t.Fatalf("IsKnown failed: %v", err)
	}
	if known {
		t.Errorf("expected file to be unknown after creation on disk but before RecordRead/Record")
	}

	// Case C: Record read -> should now be known
	if err := ft.RecordRead(ctx, filePath4); err != nil {
		t.Fatalf("RecordRead failed: %v", err)
	}
	known, err = ft.IsKnown(ctx, filePath4)
	if err != nil {
		t.Fatalf("IsKnown failed: %v", err)
	}
	if !known {
		t.Errorf("expected file to be known after RecordRead")
	}

	// Case D: Content modified on disk externally -> should no longer be known (stale)
	if err := os.WriteFile(absPath4, []byte("different content"), 0644); err != nil {
		t.Fatalf("failed to modify test file: %v", err)
	}
	known, err = ft.IsKnown(ctx, filePath4)
	if err != nil {
		t.Fatalf("IsKnown failed: %v", err)
	}
	if known {
		t.Errorf("expected file to be unknown after external modification")
	}

	// Case E: Record read again -> should be known again
	if err := ft.RecordRead(ctx, filePath4); err != nil {
		t.Fatalf("RecordRead failed: %v", err)
	}
	known, err = ft.IsKnown(ctx, filePath4)
	if err != nil {
		t.Fatalf("IsKnown failed: %v", err)
	}
	if !known {
		t.Errorf("expected file to be known again after second RecordRead")
	}
}
