package journal_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/masterkeysrd/tasksmith/internal/core/sqlite"
	"github.com/masterkeysrd/tasksmith/internal/daemon/journal"
	"github.com/masterkeysrd/tasksmith/internal/daemon/sandbox"
	"github.com/masterkeysrd/tasksmith/internal/daemon/scope"
	"github.com/masterkeysrd/tasksmith/internal/daemon/std"
)

func setupTestJournal(t *testing.T) (*sql.DB, string, sandbox.FS) {
	t.Helper()
	ctx := context.Background()

	dbDir := t.TempDir()
	db, err := sqlite.Open(ctx, filepath.Join(dbDir, "test.db"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	workDir := t.TempDir()
	sb, err := sandbox.New(sandbox.Config{
		Mode:    sandbox.ModeLocal,
		WorkDir: workDir,
	})
	if err != nil {
		t.Fatalf("sandbox.New: %v", err)
	}
	t.Cleanup(func() {
		_ = sb.Close()
	})

	return db, t.TempDir(), sb.FS()
}

func TestJournalValidation(t *testing.T) {
	ctx := context.Background()

	db, blobsDir, _ := setupTestJournal(t)

	if _, err := journal.New(ctx, nil, "sess", blobsDir); !errors.Is(err, journal.ErrInvalidConfig) {
		t.Errorf("journal.New(nil db) = %v, want %v", err, journal.ErrInvalidConfig)
	}
	if _, err := journal.New(ctx, db, "", blobsDir); !errors.Is(err, journal.ErrInvalidConfig) {
		t.Errorf("journal.New empty sessionID = %v, want %v", err, journal.ErrInvalidConfig)
	}
	if _, err := journal.New(ctx, db, "sess-1", ""); !errors.Is(err, journal.ErrInvalidConfig) {
		t.Errorf("journal.New empty blobsDir = %v, want %v", err, journal.ErrInvalidConfig)
	}
}

func TestJournalRecordReadAndCAS(t *testing.T) {
	ctx := context.Background()
	db, blobsDir, _ := setupTestJournal(t)

	j, err := journal.New(ctx, db, "session-cas", blobsDir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// RecordRead only tracks hash in memory, no blobs or files created
	if err := j.RecordRead(ctx, "app.go", "hash-read-01"); err != nil {
		t.Fatalf("RecordRead: %v", err)
	}
	h, ok := j.LastHash("app.go")
	if !ok || h != "hash-read-01" {
		t.Fatalf("LastHash = (%q, %v), want (%q, true)", h, ok, "hash-read-01")
	}
	if _, ok := j.LastContent("app.go"); ok {
		t.Fatalf("expected LastContent to be false for file only read")
	}
	if len(j.TrackedPaths()) != 0 {
		t.Fatalf("expected 0 TrackedPaths before RecordChange, got %v", j.TrackedPaths())
	}
	entries, err := os.ReadDir(blobsDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ReadDir blobsDir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 blobs written on RecordRead, got %d", len(entries))
	}
}

func TestJournalRecordChangeAndScope(t *testing.T) {
	ctx := context.Background()
	db, blobsDir, _ := setupTestJournal(t)

	j, err := journal.New(ctx, db, "session-scope", blobsDir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	scopedCtx := scope.With(ctx, scope.Scope{
		SessionID:  "session-scope",
		TurnID:     "turn-100",
		MessageID:  "msg-200",
		ToolCallID: "tc-300",
		ToolName:   "write",
	})

	content1 := []byte("hello world\n")
	ch1 := &std.FileChange{
		Path:         "main.go",
		Source:       std.SourceAgent,
		Kind:         std.KindCreated,
		AfterContent: content1,
	}
	if err := j.RecordChange(scopedCtx, ch1); err != nil {
		t.Fatalf("RecordChange ch1: %v", err)
	}

	if ch1.ID == "" {
		t.Errorf("expected RecordChange to populate ch1.ID")
	}
	if ch1.TurnID != "turn-100" {
		t.Errorf("ch1.TurnID = %q, want turn-100", ch1.TurnID)
	}
	if ch1.MessageID != "msg-200" {
		t.Errorf("ch1.MessageID = %q, want msg-200", ch1.MessageID)
	}
	if ch1.ToolCallID != "tc-300" {
		t.Errorf("ch1.ToolCallID = %q, want tc-300", ch1.ToolCallID)
	}
	if ch1.Tool != "write" {
		t.Errorf("ch1.Tool = %q, want write", ch1.Tool)
	}

	// Verify CAS after RecordChange
	lastH, ok := j.LastHash("main.go")
	if !ok || lastH != ch1.AfterHash {
		t.Errorf("LastHash = (%q, %v), want (%q, true)", lastH, ok, ch1.AfterHash)
	}
	lastC, ok := j.LastContent("main.go")
	if !ok || string(lastC) != "hello world\n" {
		t.Errorf("LastContent = (%q, %v), want %q", string(lastC), ok, "hello world\n")
	}

	// Verify blob exists on disk
	blobPath := filepath.Join(blobsDir, ch1.AfterHash)
	if _, err := os.Stat(blobPath); err != nil {
		t.Fatalf("Stat blob %q: %v", blobPath, err)
	}

	// Record external change without scope inheritance
	content2 := []byte("hello external\n")
	ch2 := &std.FileChange{
		Path:         "main.go",
		Source:       std.SourceExternal,
		Kind:         std.KindModified,
		BeforeHash:   ch1.AfterHash,
		AfterContent: content2,
	}
	if err := j.RecordChange(scopedCtx, ch2); err != nil {
		t.Fatalf("RecordChange ch2: %v", err)
	}
	if ch2.TurnID != "" || ch2.MessageID != "" || ch2.ToolCallID != "" || ch2.Tool != "" {
		t.Errorf("expected empty scope fields for SourceExternal, got %+v", ch2)
	}

	// Query Changes and ChangesForPath
	allChanges, err := j.Changes(ctx)
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}
	if len(allChanges) != 2 {
		t.Fatalf("expected 2 changes, got %d", len(allChanges))
	}
	if allChanges[0].ID != ch1.ID || allChanges[1].ID != ch2.ID {
		t.Errorf("order mismatch: got [%s, %s], want [%s, %s]", allChanges[0].ID, allChanges[1].ID, ch1.ID, ch2.ID)
	}
	if allChanges[0].TurnID != "turn-100" || allChanges[0].ToolCallID != "tc-300" {
		t.Errorf("allChanges[0] missing scope fields: %+v", allChanges[0])
	}

	pathChanges, err := j.ChangesForPath(ctx, "main.go")
	if err != nil {
		t.Fatalf("ChangesForPath: %v", err)
	}
	if len(pathChanges) != 2 {
		t.Fatalf("expected 2 changes for main.go, got %d", len(pathChanges))
	}

	// Query DiffEntry
	diffText, err := j.DiffEntry(ctx, "main.go", ch2.ID)
	if err != nil {
		t.Fatalf("DiffEntry: %v", err)
	}
	if !strings.Contains(diffText, "-hello world") || !strings.Contains(diffText, "+hello external") {
		t.Errorf("unexpected diffText:\n%s", diffText)
	}

	// Non-existent DiffEntry
	if _, err := j.DiffEntry(ctx, "main.go", "unknown-entry"); !errors.Is(err, journal.ErrEntryNotFound) {
		t.Errorf("DiffEntry unknown-entry error = %v, want %v", err, journal.ErrEntryNotFound)
	}
}

func TestJournalRecoverAt(t *testing.T) {
	ctx := context.Background()
	db, blobsDir, fsys := setupTestJournal(t)

	j, err := journal.New(ctx, db, "session-recover", blobsDir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// File created with "v1\n"
	if err := fsys.WriteFile(ctx, "doc.txt", []byte("v1\n"), 0o644); err != nil {
		t.Fatalf("WriteFile v1: %v", err)
	}
	ch1 := &std.FileChange{
		Path:         "doc.txt",
		Source:       std.SourceAgent,
		Kind:         std.KindCreated,
		AfterContent: []byte("v1\n"),
	}
	if err := j.RecordChange(ctx, ch1); err != nil {
		t.Fatalf("RecordChange 1: %v", err)
	}

	// File modified to "v2\n"
	if err := fsys.WriteFile(ctx, "doc.txt", []byte("v2\n"), 0o644); err != nil {
		t.Fatalf("WriteFile v2: %v", err)
	}
	ch2 := &std.FileChange{
		Path:          "doc.txt",
		Source:        std.SourceAgent,
		Kind:          std.KindModified,
		BeforeHash:    ch1.AfterHash,
		BeforeContent: []byte("v1\n"),
		AfterContent:  []byte("v2\n"),
	}
	if err := j.RecordChange(ctx, ch2); err != nil {
		t.Fatalf("RecordChange 2: %v", err)
	}

	// RecoverAt ch2 before=true -> state should become "v1\n"
	if err := j.RecoverAt(ctx, fsys, "doc.txt", ch2.ID, true); err != nil {
		t.Fatalf("RecoverAt ch2 before: %v", err)
	}
	data, err := fsys.ReadFile(ctx, "doc.txt")
	if err != nil {
		t.Fatalf("ReadFile after RecoverAt: %v", err)
	}
	if string(data) != "v1\n" {
		t.Errorf("data after RecoverAt ch2 before = %q, want %q", string(data), "v1\n")
	}

	// RecoverAt ch1 before=true -> file should be deleted
	if err := j.RecoverAt(ctx, fsys, "doc.txt", ch1.ID, true); err != nil {
		t.Fatalf("RecoverAt ch1 before: %v", err)
	}
	if _, err := fsys.Stat(ctx, "doc.txt"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected doc.txt to not exist after RecoverAt ch1 before, err = %v", err)
	}

	// Errors
	if err := j.RecoverAt(ctx, fsys, "unknown.txt", ch1.ID, true); !errors.Is(err, journal.ErrNotFound) {
		t.Errorf("RecoverAt unknown file = %v, want %v", err, journal.ErrNotFound)
	}
	if err := j.RecoverAt(ctx, fsys, "doc.txt", "unknown-entry", true); !errors.Is(err, journal.ErrEntryNotFound) {
		t.Errorf("RecoverAt unknown entry = %v, want %v", err, journal.ErrEntryNotFound)
	}
}

func TestJournalRevertToBaselineThreeWayMerge(t *testing.T) {
	ctx := context.Background()
	db, blobsDir, fsys := setupTestJournal(t)

	j, err := journal.New(ctx, db, "session-revert", blobsDir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	baseline := "line1\nline2\nline3\nline4\nline5\n"
	if err := fsys.WriteFile(ctx, "file.txt", []byte(baseline), 0o644); err != nil {
		t.Fatalf("WriteFile baseline: %v", err)
	}

	// Agent edits line1 -> line1_agent
	agentEdited := "line1_agent\nline2\nline3\nline4\nline5\n"
	if err := fsys.WriteFile(ctx, "file.txt", []byte(agentEdited), 0o644); err != nil {
		t.Fatalf("WriteFile agent: %v", err)
	}
	chAgent := &std.FileChange{
		Path:          "file.txt",
		Source:        std.SourceAgent,
		Kind:          std.KindModified,
		BeforeContent: []byte(baseline),
		AfterContent:  []byte(agentEdited),
	}
	if err := j.RecordChange(ctx, chAgent); err != nil {
		t.Fatalf("RecordChange agent: %v", err)
	}

	// External non-overlapping edit on line5
	externalNonOverlapping := "line1_agent\nline2\nline3\nline4\nline5_external\n"
	if err := fsys.WriteFile(ctx, "file.txt", []byte(externalNonOverlapping), 0o644); err != nil {
		t.Fatalf("WriteFile external: %v", err)
	}

	// RevertToBaseline with force=false: 3-way merge should revert line1 while keeping line5_external
	if err := j.RevertToBaseline(ctx, fsys, "file.txt", false); err != nil {
		t.Fatalf("RevertToBaseline non-overlapping: %v", err)
	}
	merged, err := fsys.ReadFile(ctx, "file.txt")
	if err != nil {
		t.Fatalf("ReadFile merged: %v", err)
	}
	wantMerged := "line1\nline2\nline3\nline4\nline5_external\n"
	if string(merged) != wantMerged {
		t.Errorf("merged = %q, want %q", string(merged), wantMerged)
	}

	// External overlapping edit: modifying line1 externally
	overlappingExternal := "line1_conflict\nline2\nline3\nline4\nline5_external\n"
	if err := fsys.WriteFile(ctx, "file.txt", []byte(overlappingExternal), 0o644); err != nil {
		t.Fatalf("WriteFile conflict: %v", err)
	}

	// Another agent change on line1
	agentConflict := "line1_agent_again\nline2\nline3\nline4\nline5_external\n"
	chAgent2 := &std.FileChange{
		Path:          "file.txt",
		Source:        std.SourceAgent,
		Kind:          std.KindModified,
		BeforeContent: []byte(wantMerged),
		AfterContent:  []byte(agentConflict),
	}
	if err := j.RecordChange(ctx, chAgent2); err != nil {
		t.Fatalf("RecordChange agent conflict: %v", err)
	}

	// Revert without force should detect conflict
	if err := j.RevertToBaseline(ctx, fsys, "file.txt", false); !errors.Is(err, journal.ErrRevertConflict) {
		t.Errorf("RevertToBaseline conflict error = %v, want %v", err, journal.ErrRevertConflict)
	}

	// Revert with force=true should succeed and restore exact baseline
	if err := j.RevertToBaseline(ctx, fsys, "file.txt", true); err != nil {
		t.Fatalf("RevertToBaseline force=true: %v", err)
	}
	forced, err := fsys.ReadFile(ctx, "file.txt")
	if err != nil {
		t.Fatalf("ReadFile forced: %v", err)
	}
	if string(forced) != baseline {
		t.Errorf("forced = %q, want %q", string(forced), baseline)
	}

	// RevertSession on all tracked files
	if err := j.RevertSession(ctx, fsys, true); err != nil {
		t.Fatalf("RevertSession: %v", err)
	}
}

func TestJournalReloadAndSessionDeletion(t *testing.T) {
	ctx := context.Background()
	db, blobsDir, fsys := setupTestJournal(t)

	j1, err := journal.New(ctx, db, "session-reload", blobsDir)
	if err != nil {
		t.Fatalf("Open 1: %v", err)
	}

	content := []byte("package main\n")
	ch := &std.FileChange{
		Path:         "main.go",
		Source:       std.SourceAgent,
		Kind:         std.KindCreated,
		AfterContent: content,
	}
	if err := j1.RecordChange(ctx, ch); err != nil {
		t.Fatalf("RecordChange: %v", err)
	}

	// Open again (simulating process restart)
	j2, err := journal.New(ctx, db, "session-reload", blobsDir)
	if err != nil {
		t.Fatalf("Open 2: %v", err)
	}

	h, ok := j2.LastHash("main.go")
	if !ok || h != ch.AfterHash {
		t.Errorf("reloaded LastHash = (%q, %v), want (%q, true)", h, ok, ch.AfterHash)
	}
	c, ok := j2.LastContent("main.go")
	if !ok || string(c) != string(content) {
		t.Errorf("reloaded LastContent = (%q, %v), want (%q, true)", string(c), ok, string(content))
	}
	if paths := j2.TrackedPaths(); !reflect.DeepEqual(paths, []string{"main.go"}) {
		t.Errorf("reloaded TrackedPaths = %v, want [\"main.go\"]", paths)
	}

	// Delete removes records from SQLite
	if err := j2.Delete(ctx); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Open after deletion has no tracked files
	j3, err := journal.New(ctx, db, "session-reload", blobsDir)
	if err != nil {
		t.Fatalf("Open 3: %v", err)
	}
	if len(j3.TrackedPaths()) != 0 {
		t.Errorf("expected 0 tracked paths after DeleteSession, got %v", j3.TrackedPaths())
	}
	if err := j3.RevertToBaseline(ctx, fsys, "main.go", false); !errors.Is(err, journal.ErrNotFound) {
		t.Errorf("RevertToBaseline after DeleteSession error = %v, want %v", err, journal.ErrNotFound)
	}
}
