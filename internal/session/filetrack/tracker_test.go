package filetrack_test

import (
	"context"
	"crypto/sha256"
	"fmt"
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

func TestFileTracker_Summary_SkipBinaryAndLarge(t *testing.T) {
	db, err := sqlx.Connect("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to connect to in-memory sqlite: %v", err)
	}
	defer db.Close()

	if err := coredb.Migrate(db, "session", testMigrations); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

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

	// Create binary file (PNG signature)
	pngPath := "test.png"
	absPngPath := filepath.Join(workspaceDir, pngPath)
	if err := os.WriteFile(absPngPath, []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"), 0644); err != nil {
		t.Fatalf("failed to write test png: %v", err)
	}

	if err := ft.Record(ctx, filetrack.Change{
		ToolName: "write",
		Path:     pngPath,
		Kind:     filetrack.Created,
	}, "", ""); err != nil {
		t.Fatalf("failed to record change: %v", err)
	}

	// Create a very large text file (> 1MB)
	largePath := "large.txt"
	absLargePath := filepath.Join(workspaceDir, largePath)
	largeBytes := make([]byte, 1000005)
	for i := range largeBytes {
		largeBytes[i] = 'a'
	}
	if err := os.WriteFile(absLargePath, largeBytes, 0644); err != nil {
		t.Fatalf("failed to write large file: %v", err)
	}

	if err := ft.Record(ctx, filetrack.Change{
		ToolName: "write",
		Path:     largePath,
		Kind:     filetrack.Created,
	}, "", ""); err != nil {
		t.Fatalf("failed to record change: %v", err)
	}

	summaries, err := ft.Summary(ctx)
	if err != nil {
		t.Fatalf("Summary failed: %v", err)
	}

	if len(summaries) != 2 {
		t.Fatalf("expected 2 summaries, got %d", len(summaries))
	}

	for _, sum := range summaries {
		if sum.NetAdditions != 0 || sum.NetDeletions != 0 {
			t.Errorf("expected 0 additions/deletions for binary or large file %s, got %+v", sum.Path, sum)
		}
	}
}

func TestFileTracker_ReadJournal_LargeLines(t *testing.T) {
	db, err := sqlx.Connect("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to connect to in-memory sqlite: %v", err)
	}
	defer db.Close()

	if err := coredb.Migrate(db, "session", testMigrations); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

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

	// Record a file modification where the baseline content is larger than 64KB (e.g. 100KB)
	filePath := "large_baseline.txt"
	absPath := filepath.Join(workspaceDir, filePath)
	baselineBytes := make([]byte, 100000) // 100KB
	for i := range baselineBytes {
		baselineBytes[i] = 'a'
	}
	if err := os.WriteFile(absPath, baselineBytes, 0644); err != nil {
		t.Fatalf("failed to write baseline: %v", err)
	}

	// This records a baseline entry in journal with 100KB line
	if err := ft.Record(ctx, filetrack.Change{
		ToolName: "write",
		Path:     filePath,
		Kind:     filetrack.Modified,
	}, "diff", string(baselineBytes)); err != nil {
		t.Fatalf("failed to record modification: %v", err)
	}

	// Read journal should read it successfully without "token too long" error
	entries, err := ft.ReadJournal(ctx, filePath)
	if err != nil {
		t.Fatalf("ReadJournal failed on 100KB line: %v", err)
	}

	if len(entries) != 2 { // baseline + change
		t.Errorf("expected 2 entries, got %d", len(entries))
	}
}

func TestFileTracker_Binary_MetadataOnly(t *testing.T) {
	db, err := sqlx.Connect("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to connect to in-memory sqlite: %v", err)
	}
	defer db.Close()

	if err := coredb.Migrate(db, "session", testMigrations); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

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

	// 1. Record creation of a binary file
	pngPath := "test.png"
	absPngPath := filepath.Join(workspaceDir, pngPath)
	pngBytes := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	if err := os.WriteFile(absPngPath, pngBytes, 0644); err != nil {
		t.Fatalf("failed to write test png: %v", err)
	}

	if err := ft.Record(ctx, filetrack.Change{
		ToolName: "write",
		Path:     pngPath,
		Kind:     filetrack.Created,
	}, "", ""); err != nil {
		t.Fatalf("failed to record created binary change: %v", err)
	}

	// Verify that the journal has IsBinary = true
	entries, err := ft.ReadJournal(ctx, pngPath)
	if err != nil {
		t.Fatalf("ReadJournal failed: %v", err)
	}
	if len(entries) < 1 || !entries[0].IsBinary {
		t.Errorf("expected baseline entry to have IsBinary = true, got %+v", entries[0])
	}

	// Verify that NO .last file was written
	h := sha256.Sum256([]byte(pngPath))
	lastFile := filepath.Join(changesDir, fmt.Sprintf("%x.last", h[:8]))
	if _, err := os.Stat(lastFile); err == nil || !os.IsNotExist(err) {
		t.Error("expected no .last backup file to be written for binary file")
	}

	// 2. Reverting a created binary file should delete it (no backup needed for deletion)
	if err := ft.RevertToBaseline(ctx, pngPath, false); err != nil {
		t.Errorf("expected reverting created binary to succeed, got error: %v", err)
	}
	if _, err := os.Stat(absPngPath); !os.IsNotExist(err) {
		t.Error("expected created binary file to be deleted on revert")
	}

	// 3. Record modification of a binary file
	if err := os.WriteFile(absPngPath, pngBytes, 0644); err != nil {
		t.Fatalf("failed to write test png again: %v", err)
	}
	ft2 := filetrack.NewTracker(store, "session-1", changesDir, workspaceDir)
	if err := ft2.Record(ctx, filetrack.Change{
		ToolName: "write",
		Path:     pngPath,
		Kind:     filetrack.Modified,
	}, "", ""); err != nil {
		t.Fatalf("failed to record modified binary change: %v", err)
	}

	// Reverting modified binary should fail since no backup exists
	if err := ft2.RevertToBaseline(ctx, pngPath, false); err == nil {
		t.Error("expected reverting modified binary to fail (no backup), but got nil")
	}

	// 4. Test conflict detection for binary files
	conflict, err := ft2.CheckConflict(ctx, pngPath)
	if err != nil {
		t.Fatalf("CheckConflict failed: %v", err)
	}
	if conflict {
		t.Error("expected no conflict initially")
	}

	// Mutate on disk externally
	if err := os.WriteFile(absPngPath, append(pngBytes, 'x'), 0644); err != nil {
		t.Fatalf("failed to mutate png externally: %v", err)
	}

	conflict, err = ft2.CheckConflict(ctx, pngPath)
	if err != nil {
		t.Fatalf("CheckConflict failed: %v", err)
	}
	if !conflict {
		t.Error("expected conflict after external modification of binary file")
	}
}
