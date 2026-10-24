package std

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/daemon/sandbox"
	"github.com/masterkeysrd/tasksmith/internal/daemon/scope"
)

func newTestStd(t *testing.T, allowLoopback bool) (*Std, *MemoryTracker, string) {
	t.Helper()
	workDir := t.TempDir()

	sb, err := sandbox.New(sandbox.Config{
		Mode:    sandbox.ModeLocal,
		WorkDir: workDir,
		Network: sandbox.NetworkPolicy{
			AllowLoopback: allowLoopback,
		},
		DefaultTimeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("sandbox.New failed: %v", err)
	}
	t.Cleanup(func() {
		_ = sb.Close()
	})

	tracker := NewMemoryTracker()
	logger := log.New(io.Discard, log.LevelDebug)

	s, err := New(Config{
		Sandbox: sb,
		Tracker: tracker,
		Logger:  logger,
	})
	if err != nil {
		t.Fatalf("std.New failed: %v", err)
	}

	return s, tracker, workDir
}

func TestConfigValidate(t *testing.T) {
	s, tracker, _ := newTestStd(t, false)
	logger := log.New(io.Discard, log.LevelInfo)

	var nilCfg *Config
	if err := nilCfg.Validate(); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig for nil *Config, got %v", err)
	}

	tests := []struct {
		name string
		cfg  Config
	}{
		{
			name: "missing sandbox",
			cfg:  Config{Tracker: tracker, Logger: logger},
		},
		{
			name: "missing tracker",
			cfg:  Config{Sandbox: s.Sandbox(), Logger: logger},
		},
		{
			name: "missing logger",
			cfg:  Config{Sandbox: s.Sandbox(), Tracker: tracker},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(tc.cfg)
			if !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("expected ErrInvalidConfig, got %v", err)
			}
		})
	}

	if s.Sandbox() == nil {
		t.Fatal("expected non-nil Sandbox()")
	}
	if s.Tracker() != tracker {
		t.Fatal("expected Tracker() to match injected tracker")
	}
}

func TestMemoryTrackerConcurrencyAndCloning(t *testing.T) {
	tracker := NewMemoryTracker()
	ctx := context.Background()

	if err := tracker.RecordRead(ctx, "./dir/read_only.txt", "hash-read"); err != nil {
		t.Fatalf("RecordRead: %v", err)
	}
	if h, ok := tracker.LastHash("dir/read_only.txt"); !ok || h != "hash-read" {
		t.Fatalf("expected LastHash 'hash-read', got %q (ok=%v)", h, ok)
	}
	if _, ok := tracker.LastContent("dir/read_only.txt"); ok {
		t.Fatal("expected LastContent to be false for file that was only read")
	}
	if len(tracker.TrackedPaths()) != 0 {
		t.Fatalf("expected TrackedPaths to be empty before RecordChange, got %v", tracker.TrackedPaths())
	}

	orig := []byte("hello")
	seedChange := FileChange{
		Path:         "./dir/file.txt",
		Source:       SourceAgent,
		Tool:         "write",
		Kind:         KindCreated,
		AfterHash:    "hash1",
		AfterContent: orig,
	}
	if err := tracker.RecordChange(ctx, &seedChange); err != nil {
		t.Fatalf("RecordChange: %v", err)
	}
	if seedChange.ID == "" {
		t.Fatal("expected RecordChange to populate seedChange.ID")
	}
	orig[0] = 'X'

	gotContent, ok := tracker.LastContent("dir/file.txt")
	if !ok || string(gotContent) != "hello" {
		t.Fatalf("expected defensive copy 'hello', got %q (ok=%v)", string(gotContent), ok)
	}
	gotContent[0] = 'Z'
	gotAgain, _ := tracker.LastContent("dir/file.txt")
	if string(gotAgain) != "hello" {
		t.Fatalf("expected internal state to remain 'hello', got %q", string(gotAgain))
	}

	var wg sync.WaitGroup
	for i := range 16 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ch := FileChange{
				Path:          "dir/file.txt",
				Source:        SourceAgent,
				Tool:          "write",
				Kind:          KindModified,
				BeforeHash:    "h1",
				AfterHash:     "h2",
				BeforeContent: []byte("a"),
				AfterContent:  []byte("b"),
			}
			_ = tracker.RecordChange(ctx, &ch)
			_, _ = tracker.LastHash("dir/file.txt")
			_, _ = tracker.LastContent("dir/file.txt")
			_ = tracker.TrackedPaths()
			_ = tracker.Changes()
			_ = tracker.ChangesForPath("dir/file.txt")
		}(i)
	}
	wg.Wait()

	if len(tracker.Changes()) != 17 {
		t.Fatalf("expected 17 changes, got %d", len(tracker.Changes()))
	}
	if len(tracker.ChangesForPath("dir/file.txt")) != 17 {
		t.Fatalf("expected 17 changes for dir/file.txt, got %d", len(tracker.ChangesForPath("dir/file.txt")))
	}

	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()
	if err := tracker.RecordRead(cancelCtx, "a.txt", "h"); err == nil {
		t.Fatal("expected error for canceled context in RecordRead")
	}
	if err := tracker.RecordChange(cancelCtx, &FileChange{Path: "a.txt"}); err == nil {
		t.Fatal("expected error for canceled context in RecordChange")
	}
	if err := tracker.RecordChange(ctx, nil); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for nil change, got %v", err)
	}
}

func TestMemoryTrackerScopePropagation(t *testing.T) {
	tracker := NewMemoryTracker()
	ctx := scope.With(context.Background(), scope.Scope{
		TurnID:     "turn-01",
		MessageID:  "msg-02",
		ToolCallID: "tc-03",
		ToolName:   "write",
	})

	// Agent change with empty scope fields should inherit from context
	ch := FileChange{
		Path:      "test.txt",
		Source:    SourceAgent,
		Kind:      KindCreated,
		AfterHash: "hash-01",
	}
	if err := tracker.RecordChange(ctx, &ch); err != nil {
		t.Fatalf("RecordChange: %v", err)
	}
	if ch.TurnID != "turn-01" {
		t.Errorf("ch.TurnID = %q, want turn-01", ch.TurnID)
	}
	if ch.MessageID != "msg-02" {
		t.Errorf("ch.MessageID = %q, want msg-02", ch.MessageID)
	}
	if ch.ToolCallID != "tc-03" {
		t.Errorf("ch.ToolCallID = %q, want tc-03", ch.ToolCallID)
	}
	if ch.Tool != "write" {
		t.Errorf("ch.Tool = %q, want write", ch.Tool)
	}

	// External change should NOT inherit from scope
	extCh := FileChange{
		Path:      "test.txt",
		Source:    SourceExternal,
		Kind:      KindModified,
		AfterHash: "hash-02",
	}
	if err := tracker.RecordChange(ctx, &extCh); err != nil {
		t.Fatalf("RecordChange ext: %v", err)
	}
	if extCh.TurnID != "" || extCh.MessageID != "" || extCh.ToolCallID != "" || extCh.Tool != "" {
		t.Errorf("expected empty scope fields for SourceExternal, got %+v", extCh)
	}
}

func TestViewAndExternalChangeDetection(t *testing.T) {
	s, tracker, workDir := newTestStd(t, false)
	ctx := context.Background()

	filePath := filepath.Join(workDir, "notes.txt")
	if err := os.WriteFile(filePath, []byte("line1\nline2\nline3\nline4\nline5\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// View with offset & limit
	res, err := s.View(ctx, ViewRequest{
		Path:   "notes.txt",
		Offset: 2,
		Limit:  2,
	})
	if err != nil {
		t.Fatalf("View failed: %v", err)
	}
	if res.StartLine != 2 || res.EndLine != 3 || res.TotalLines != 5 || !res.Truncated {
		t.Fatalf("unexpected line metadata: %+v", res)
	}
	if res.Content != "line2\nline3" {
		t.Fatalf("unexpected content: %q", res.Content)
	}
	if !strings.Contains(res.Numbered, "2\tline2") || !strings.Contains(res.Numbered, "3\tline3") {
		t.Fatalf("unexpected numbered output: %q", res.Numbered)
	}

	// No changes should be recorded on initial read, only read state.
	if len(tracker.Changes()) != 0 {
		t.Fatalf("expected 0 changes on initial read, got %d", len(tracker.Changes()))
	}
	firstHash, ok := tracker.LastHash("notes.txt")
	if !ok || firstHash != res.Hash {
		t.Fatalf("expected tracked hash %q, got %q", res.Hash, firstHash)
	}

	// Because notes.txt was only read (not yet changed in the session), modifying it on disk
	// and calling View updates the read hash without recording a FileChange.
	if err := os.WriteFile(filePath, []byte("line1\nline2-external\nline3\nline4\nline5\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	resAfterReadOnlyMod, err := s.View(ctx, ViewRequest{Path: "notes.txt"})
	if err != nil {
		t.Fatalf("View after read-only mod failed: %v", err)
	}
	if len(tracker.Changes()) != 0 {
		t.Fatalf("expected 0 changes when re-reading file with no prior session changes, got %d", len(tracker.Changes()))
	}
	if h, _ := tracker.LastHash("notes.txt"); h != resAfterReadOnlyMod.Hash {
		t.Fatalf("expected updated read hash %q, got %q", resAfterReadOnlyMod.Hash, h)
	}

	// Now perform an agent Edit on notes.txt so it has a prior change in the session,
	// then modify it externally on disk and call View again.
	editRes, err := s.Edit(ctx, EditRequest{
		Path:    "notes.txt",
		OldText: "line2-external",
		NewText: "line2-agent",
	})
	if err != nil {
		t.Fatalf("Edit failed: %v", err)
	}

	if err := os.WriteFile(filePath, []byte("line1\nline2-mod\nline3\nline4\nline5\nline6\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	res2, err := s.View(ctx, ViewRequest{Path: filePath})
	if err != nil {
		t.Fatalf("second View failed: %v", err)
	}
	if res2.TotalLines != 6 || res2.Truncated {
		t.Fatalf("unexpected second View result: %+v", res2)
	}

	changes := tracker.Changes()
	if len(changes) != 2 {
		t.Fatalf("expected 2 changes (1 agent edit + 1 external change), got %d", len(changes))
	}
	ch := changes[1]
	if ch.Source != SourceExternal || ch.Tool != "external" || ch.Kind != KindModified {
		t.Fatalf("unexpected external change attributes: %+v", ch)
	}
	if ch.BeforeHash != editRes.AfterHash || ch.AfterHash != res2.Hash {
		t.Fatalf("unexpected hashes in external change: %+v", ch)
	}
	if ch.Additions != 2 || ch.Deletions != 1 {
		t.Fatalf("expected +2/-1 stats, got +%d/-%d", ch.Additions, ch.Deletions)
	}

	// Empty file & offset past total lines
	emptyPath := filepath.Join(workDir, "empty.txt")
	if err := os.WriteFile(emptyPath, nil, 0o644); err != nil {
		t.Fatalf("WriteFile empty: %v", err)
	}
	emptyRes, err := s.View(ctx, ViewRequest{Path: "empty.txt"})
	if err != nil {
		t.Fatalf("View empty: %v", err)
	}
	if emptyRes.TotalLines != 0 || emptyRes.Content != "" {
		t.Fatalf("expected 0 lines for empty file, got %+v", emptyRes)
	}

	pastEndRes, err := s.View(ctx, ViewRequest{Path: "notes.txt", Offset: 100})
	if err != nil {
		t.Fatalf("View past end: %v", err)
	}
	if pastEndRes.Content != "" || pastEndRes.StartLine != 0 {
		t.Fatalf("expected empty slice past end, got %+v", pastEndRes)
	}

	// Binary file rejection
	binPath := filepath.Join(workDir, "app.bin")
	if err := os.WriteFile(binPath, []byte{'E', 'L', 'F', 0x00, 0x01, 0x02}, 0o644); err != nil {
		t.Fatalf("WriteFile bin: %v", err)
	}
	if _, err := s.View(ctx, ViewRequest{Path: "app.bin"}); !errors.Is(err, ErrBinaryFile) {
		t.Fatalf("expected ErrBinaryFile, got %v", err)
	}

	// Invalid arguments
	if _, err := s.View(ctx, ViewRequest{Path: ""}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for empty path, got %v", err)
	}
	if _, err := s.View(ctx, ViewRequest{Path: "."}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for dot path, got %v", err)
	}
	if _, err := s.View(ctx, ViewRequest{Path: "../outside.txt"}); !errors.Is(err, sandbox.ErrPathEscape) {
		t.Fatalf("expected sandbox.ErrPathEscape, got %v", err)
	}
}

func TestListGlobAndGrep(t *testing.T) {
	s, _, workDir := newTestStd(t, false)
	ctx := context.Background()

	files := map[string]string{
		"README.md":               "# Project Title\nWelcome to TaskSmith\n",
		".hidden":                 "secret=true\n",
		".git/config":             "[core]\n",
		".bare/HEAD":              "ref: refs/heads/main\n",
		"pkg/alpha/alpha.go":      "package alpha\n\nfunc AlphaHelper() string {\n\treturn \"alpha\"\n}\n",
		"pkg/alpha/alpha_test.go": "package alpha\n\nfunc TestAlpha() {}\n",
		"pkg/beta/deep/beta.go":   "package deep\n\n// TODO: refactor beta\nfunc BetaWork() {}\n",
		"vendor/ignored.go":       "package vendor\n",
		"assets/logo.bin":         "PNG\x00\x01\x02",
	}

	for rel, content := range files {
		full := filepath.Join(workDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", rel, err)
		}
	}

	// 1. List top-level non-hidden
	listRes, err := s.List(ctx, ListRequest{})
	if err != nil {
		t.Fatalf("List top-level failed: %v", err)
	}
	var names []string
	for _, e := range listRes.Entries {
		names = append(names, e.Name)
	}
	if strings.Join(names, ",") != "README.md,assets,pkg,vendor" {
		t.Fatalf("unexpected top-level entries: %v", names)
	}

	// 2. List recursive with MaxDepth, Ignore, and Limit
	recList, err := s.List(ctx, ListRequest{
		Recursive: true,
		MaxDepth:  2,
		Ignore:    []string{"vendor", "assets"},
		Limit:     3,
	})
	if err != nil {
		t.Fatalf("List recursive failed: %v", err)
	}
	if !recList.Truncated || len(recList.Entries) != 3 {
		t.Fatalf("expected truncated list of 3 entries, got %+v", recList)
	}

	// 3. Glob with ** multi-directory wildcard (skips .git and .bare automatically)
	globRes, err := s.Glob(ctx, GlobRequest{
		Pattern: "**/*.go",
		Ignore:  []string{"vendor/**"},
	})
	if err != nil {
		t.Fatalf("Glob **/*.go failed: %v", err)
	}
	expectedGo := "pkg/alpha/alpha.go,pkg/alpha/alpha_test.go,pkg/beta/deep/beta.go"
	if strings.Join(globRes.Matches, ",") != expectedGo {
		t.Fatalf("expected glob matches %q, got %q", expectedGo, strings.Join(globRes.Matches, ","))
	}

	// Glob scoped to subdirectory with Limit
	subGlob, err := s.Glob(ctx, GlobRequest{
		Path:    "pkg",
		Pattern: "**/*_test.go",
		Limit:   1,
	})
	if err != nil {
		t.Fatalf("subGlob failed: %v", err)
	}
	if len(subGlob.Matches) != 1 || subGlob.Matches[0] != "pkg/alpha/alpha_test.go" {
		t.Fatalf("unexpected subGlob matches: %+v", subGlob)
	}

	// Glob targeting hidden file
	hiddenGlob, err := s.Glob(ctx, GlobRequest{
		Pattern: ".hid*",
	})
	if err != nil {
		t.Fatalf("hiddenGlob failed: %v", err)
	}
	if len(hiddenGlob.Matches) != 1 || hiddenGlob.Matches[0] != ".hidden" {
		t.Fatalf("expected [.hidden], got %v", hiddenGlob.Matches)
	}

	// 4. Grep with CaseInsensitive, Include, and ContextLines
	grepRes, err := s.Grep(ctx, GrepRequest{
		Pattern:         "alphahelper|todo",
		CaseInsensitive: true,
		Include:         "*.go",
		ContextLines:    1,
	})
	if err != nil {
		t.Fatalf("Grep failed: %v", err)
	}
	if len(grepRes.Matches) != 2 {
		t.Fatalf("expected 2 grep matches, got %+v", grepRes.Matches)
	}
	if grepRes.Matches[0].Path != "pkg/alpha/alpha.go" || grepRes.Matches[0].Line != 3 {
		t.Fatalf("unexpected first grep match: %+v", grepRes.Matches[0])
	}
	if len(grepRes.Matches[0].After) != 1 || !strings.Contains(grepRes.Matches[0].After[0], "return \"alpha\"") {
		t.Fatalf("expected 1 after context line, got %+v", grepRes.Matches[0].After)
	}

	// Grep on single file
	singleGrep, err := s.Grep(ctx, GrepRequest{
		Path:    "README.md",
		Pattern: "TaskSmith",
	})
	if err != nil {
		t.Fatalf("single file Grep failed: %v", err)
	}
	if len(singleGrep.Matches) != 1 || singleGrep.Matches[0].Line != 2 {
		t.Fatalf("unexpected single file grep result: %+v", singleGrep)
	}

	// Invalid patterns
	if _, err := s.Glob(ctx, GlobRequest{Pattern: "[invalid"}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for invalid glob, got %v", err)
	}
	if _, err := s.Grep(ctx, GrepRequest{Pattern: "(unclosed"}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for invalid regex, got %v", err)
	}
}

func TestWriteEditRemoveCASLifecycle(t *testing.T) {
	s, tracker, workDir := newTestStd(t, false)
	ctx := context.Background()

	// 1. Write new file (does not require prior View)
	wRes, err := s.Write(ctx, WriteRequest{
		Path:    "src/hello.go",
		Content: "package main\n\nfunc main() {\n\tprintln(\"v1\")\n}\n",
	})
	if err != nil {
		t.Fatalf("Write new file failed: %v", err)
	}
	if !wRes.Created || wRes.Additions != 5 || wRes.Deletions != 0 {
		t.Fatalf("unexpected WriteResult for new file: %+v", wRes)
	}

	// 2. Create an untracked file directly on disk to test ErrFileNotRead
	untrackedDiskPath := filepath.Join(workDir, "untracked.txt")
	if err := os.WriteFile(untrackedDiskPath, []byte("initial\n"), 0o644); err != nil {
		t.Fatalf("os.WriteFile: %v", err)
	}

	if _, err := s.Write(ctx, WriteRequest{Path: "untracked.txt", Content: "overwrite\n"}); !errors.Is(err, ErrFileNotRead) {
		t.Fatalf("expected ErrFileNotRead on Write to unread file, got %v", err)
	}
	if _, err := s.Edit(ctx, EditRequest{Path: "untracked.txt", OldText: "initial", NewText: "changed"}); !errors.Is(err, ErrFileNotRead) {
		t.Fatalf("expected ErrFileNotRead on Edit to unread file, got %v", err)
	}
	if _, err := s.Remove(ctx, RemoveRequest{Path: "untracked.txt"}); !errors.Is(err, ErrFileNotRead) {
		t.Fatalf("expected ErrFileNotRead on Remove to unread file, got %v", err)
	}

	// 3. View "untracked.txt", then modify it externally on disk to trigger ErrFileModified (CAS conflict)
	if _, err := s.View(ctx, ViewRequest{Path: "untracked.txt"}); err != nil {
		t.Fatalf("View untracked.txt: %v", err)
	}
	if err := os.WriteFile(untrackedDiskPath, []byte("concurrent external edit\n"), 0o644); err != nil {
		t.Fatalf("os.WriteFile: %v", err)
	}

	if _, err := s.Write(ctx, WriteRequest{Path: "untracked.txt", Content: "agent overwrite\n"}); !errors.Is(err, ErrFileModified) {
		t.Fatalf("expected ErrFileModified on Write after external edit, got %v", err)
	}
	if _, err := s.Edit(ctx, EditRequest{Path: "untracked.txt", OldText: "concurrent", NewText: "agent"}); !errors.Is(err, ErrFileModified) {
		t.Fatalf("expected ErrFileModified on Edit after external edit, got %v", err)
	}
	if _, err := s.Remove(ctx, RemoveRequest{Path: "untracked.txt"}); !errors.Is(err, ErrFileModified) {
		t.Fatalf("expected ErrFileModified on Remove after external edit, got %v", err)
	}

	// 4. Edit "src/hello.go" (which was written by the agent, so its hash is tracked)
	if _, err := s.Edit(ctx, EditRequest{
		Path:    "src/hello.go",
		OldText: "nonexistent",
		NewText: "v2",
	}); !errors.Is(err, ErrTargetNotFound) {
		t.Fatalf("expected ErrTargetNotFound, got %v", err)
	}

	if _, err := s.Edit(ctx, EditRequest{
		Path:    "src/hello.go",
		OldText: "main",
		NewText: "app",
	}); !errors.Is(err, ErrAmbiguousEdit) {
		t.Fatalf("expected ErrAmbiguousEdit for multiple matches without ReplaceAll, got %v", err)
	}

	// DryRun Edit does not mutate disk or tracker
	dryEdit, err := s.Edit(ctx, EditRequest{
		Path:    "src/hello.go",
		OldText: "\"v1\"",
		NewText: "\"v2\"",
		DryRun:  true,
	})
	if err != nil {
		t.Fatalf("DryRun Edit failed: %v", err)
	}
	if !dryEdit.DryRun || dryEdit.Replacements != 1 {
		t.Fatalf("unexpected DryRun EditResult: %+v", dryEdit)
	}

	// Real Edit
	editRes, err := s.Edit(ctx, EditRequest{
		Path:    "src/hello.go",
		OldText: "\"v1\"",
		NewText: "\"v2\"",
	})
	if err != nil {
		t.Fatalf("Edit failed: %v", err)
	}
	if editRes.Replacements != 1 || editRes.Additions != 1 || editRes.Deletions != 1 {
		t.Fatalf("unexpected EditResult: %+v", editRes)
	}

	// ReplaceAll Edit
	editAllRes, err := s.Edit(ctx, EditRequest{
		Path:       "src/hello.go",
		OldText:    "main",
		NewText:    "entry",
		ReplaceAll: true,
	})
	if err != nil {
		t.Fatalf("ReplaceAll Edit failed: %v", err)
	}
	if editAllRes.Replacements != 2 {
		t.Fatalf("expected 2 replacements, got %d", editAllRes.Replacements)
	}

	// 5. Remove "src/hello.go"
	rmRes, err := s.Remove(ctx, RemoveRequest{Path: "src/hello.go"})
	if err != nil {
		t.Fatalf("Remove failed: %v", err)
	}
	if rmRes.Deletions != 5 {
		t.Fatalf("expected 5 deleted lines, got %d", rmRes.Deletions)
	}
	if _, ok := tracker.LastHash("src/hello.go"); ok {
		t.Fatal("expected LastHash to be false after file deletion")
	}

	// Verify recorded history for "src/hello.go": Created -> Modified -> Modified -> Deleted
	history := tracker.ChangesForPath("src/hello.go")
	if len(history) != 4 {
		t.Fatalf("expected 4 changes for src/hello.go, got %d", len(history))
	}
	expectedKinds := []ChangeKind{KindCreated, KindModified, KindModified, KindDeleted}
	expectedTools := []string{"write", "edit", "edit", "remove"}
	for i := range history {
		if history[i].Source != SourceAgent {
			t.Fatalf("change %d expected SourceAgent, got %s", i, history[i].Source)
		}
		if history[i].Kind != expectedKinds[i] || history[i].Tool != expectedTools[i] {
			t.Fatalf("change %d expected (%s, %s), got (%s, %s)", i, expectedKinds[i], expectedTools[i], history[i].Kind, history[i].Tool)
		}
	}
}

func TestBashAndSyncExternalChanges(t *testing.T) {
	s, tracker, _ := newTestStd(t, false)
	ctx := context.Background()

	// Create two tracked files via Write
	if _, err := s.Write(ctx, WriteRequest{Path: "tracked_mod.txt", Content: "before\n"}); err != nil {
		t.Fatalf("Write tracked_mod.txt: %v", err)
	}
	if _, err := s.Write(ctx, WriteRequest{Path: "tracked_del.txt", Content: "to be deleted\n"}); err != nil {
		t.Fatalf("Write tracked_del.txt: %v", err)
	}

	// Run a Bash command that modifies tracked_mod.txt and deletes tracked_del.txt
	bashRes, err := s.Bash(ctx, BashRequest{
		Command: "printf 'after\\nsecond line\\n' > tracked_mod.txt && rm tracked_del.txt && printf 'done\\n'",
	})
	if err != nil {
		t.Fatalf("Bash failed: %v", err)
	}
	if bashRes.ExitCode != 0 || bashRes.Stdout != "done\n" {
		t.Fatalf("unexpected BashResult: %+v", bashRes)
	}
	if len(bashRes.ExternalChanges) != 2 {
		t.Fatalf("expected 2 ExternalChanges from Bash, got %+v", bashRes.ExternalChanges)
	}

	var sawMod, sawDel bool
	for _, ch := range bashRes.ExternalChanges {
		if len(ch.ID) != 36 || ch.ID[14] != '7' {
			t.Fatalf("expected SyncExternalChanges to return FileChange with populated UUIDv7 ID, got %q", ch.ID)
		}
		if ch.Source != SourceExternal || ch.Tool != "external" {
			t.Fatalf("expected SourceExternal and tool 'external', got %+v", ch)
		}
		switch ch.Path {
		case "tracked_mod.txt":
			sawMod = ch.Kind == KindModified && ch.Additions == 2 && ch.Deletions == 1
		case "tracked_del.txt":
			sawDel = ch.Kind == KindDeleted && ch.AfterHash == "" && ch.Deletions == 1
		}
	}
	if !sawMod || !sawDel {
		t.Fatalf("missing expected external changes (sawMod=%v, sawDel=%v): %+v", sawMod, sawDel, bashRes.ExternalChanges)
	}

	// Calling SyncExternalChanges again without further disk edits should return 0 changes
	again, err := s.SyncExternalChanges(ctx)
	if err != nil {
		t.Fatalf("SyncExternalChanges: %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("expected 0 changes on idempotent SyncExternalChanges, got %+v", again)
	}

	// Verify tracker state was updated by SyncExternalChanges
	if _, ok := tracker.LastHash("tracked_del.txt"); ok {
		t.Fatal("expected deleted file to have no active hash in tracker")
	}
	if _, ok := tracker.LastHash("tracked_mod.txt"); !ok {
		t.Fatal("expected modified file to have updated hash in tracker")
	}
}

func TestFetchAndDownload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json":
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Echo-Method", r.Method)
			w.Header().Set("X-Custom", r.Header.Get("X-Custom"))
			_, _ = w.Write([]byte(`{"ok":true,"msg":"hello world"}`))
		case "/artifact.txt":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("downloaded line 1\ndownloaded line 2\n"))
		case "/error":
			http.Error(w, "not found", http.StatusNotFound)
		default:
			http.Error(w, "bad request", http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	// 1. Verify sandbox network policy blocks loopback when AllowLoopback is false
	sBlocked, _, _ := newTestStd(t, false)
	if _, err := sBlocked.Fetch(context.Background(), FetchRequest{URL: srv.URL + "/json"}); !errors.Is(err, sandbox.ErrLoopbackBlocked) {
		t.Fatalf("expected sandbox.ErrLoopbackBlocked when AllowLoopback=false, got %v", err)
	}

	// 2. With AllowLoopback=true, test Fetch and Download
	s, tracker, _ := newTestStd(t, true)
	ctx := context.Background()

	fetchRes, err := s.Fetch(ctx, FetchRequest{
		URL:     srv.URL + "/json",
		Method:  "POST",
		Headers: map[string]string{"X-Custom": "tasksmith"},
		Body:    []byte(`{"ping":1}`),
	})
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}
	if fetchRes.StatusCode != http.StatusOK || fetchRes.Headers["x-custom"] != "tasksmith" || fetchRes.Headers["x-echo-method"] != "POST" {
		t.Fatalf("unexpected FetchResult: %+v", fetchRes)
	}
	if !strings.Contains(fetchRes.Text, `"ok":true`) || fetchRes.Truncated {
		t.Fatalf("unexpected FetchResult text: %+v", fetchRes)
	}

	// Fetch truncation via MaxBytes
	truncRes, err := s.Fetch(ctx, FetchRequest{
		URL:      srv.URL + "/json",
		MaxBytes: 5,
	})
	if err != nil {
		t.Fatalf("Fetch with MaxBytes failed: %v", err)
	}
	if !truncRes.Truncated || len(truncRes.Body) != 5 {
		t.Fatalf("expected truncated 5-byte response, got %+v", truncRes)
	}

	// Fetch DryRun
	dryFetch, err := s.Fetch(ctx, FetchRequest{
		URL:    srv.URL + "/json",
		DryRun: true,
	})
	if err != nil {
		t.Fatalf("Fetch DryRun failed: %v", err)
	}
	if !dryFetch.DryRun || dryFetch.StatusCode != 0 {
		t.Fatalf("unexpected dryFetch: %+v", dryFetch)
	}

	// Download DryRun
	dryDl, err := s.Download(ctx, DownloadRequest{
		URL:    srv.URL + "/artifact.txt",
		Path:   "downloads/dry_artifact.txt",
		DryRun: true,
	})
	if err != nil {
		t.Fatalf("Download DryRun failed: %v", err)
	}
	if !dryDl.DryRun || !dryDl.Created {
		t.Fatalf("unexpected dryDl: %+v", dryDl)
	}

	// Download new file
	dlRes, err := s.Download(ctx, DownloadRequest{
		URL:  srv.URL + "/artifact.txt",
		Path: "downloads/artifact.txt",
	})
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}
	if !dlRes.Created || dlRes.BytesWritten == 0 {
		t.Fatalf("unexpected DownloadResult: %+v", dlRes)
	}

	// Verify Download recorded SourceAgent + tool "download"
	dlChanges := tracker.ChangesForPath("downloads/artifact.txt")
	if len(dlChanges) != 1 || dlChanges[0].Source != SourceAgent || dlChanges[0].Tool != "download" || dlChanges[0].Kind != KindCreated {
		t.Fatalf("unexpected download tracker entry: %+v", dlChanges)
	}

	// Verify downloaded file can be read via View
	viewRes, err := s.View(ctx, ViewRequest{Path: "downloads/artifact.txt"})
	if err != nil {
		t.Fatalf("View downloaded file failed: %v", err)
	}
	if viewRes.TotalLines != 2 || viewRes.Hash != dlRes.AfterHash {
		t.Fatalf("unexpected ViewResult after download: %+v", viewRes)
	}

	// Download error status and MaxBytes exceeded
	if _, err := s.Download(ctx, DownloadRequest{URL: srv.URL + "/error", Path: "downloads/err.txt"}); err == nil {
		t.Fatal("expected error for HTTP 404 on Download")
	}
	if _, err := s.Download(ctx, DownloadRequest{URL: srv.URL + "/artifact.txt", Path: "downloads/small.txt", MaxBytes: 4}); err == nil {
		t.Fatal("expected error when Download exceeds MaxBytes")
	}
	if _, err := s.Fetch(ctx, FetchRequest{URL: "ftp://example.com/file"}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for ftp scheme, got %v", err)
	}
}

func TestEdgeCasesAndValidationBranches(t *testing.T) {
	s, _, workDir := newTestStd(t, true)
	ctx := context.Background()

	// Write Force and DryRun on existing file
	target := filepath.Join(workDir, "force_target.txt")
	if err := os.WriteFile(target, []byte("line1\nline2\n"), 0o640); err != nil {
		t.Fatalf("os.WriteFile: %v", err)
	}
	dryWrite, err := s.Write(ctx, WriteRequest{
		Path:    "force_target.txt",
		Content: "line1\nline2-dry\n",
		Force:   true,
		DryRun:  true,
	})
	if err != nil || !dryWrite.DryRun || dryWrite.Created {
		t.Fatalf("unexpected dryWrite: %+v, err=%v", dryWrite, err)
	}

	forcedWrite, err := s.Write(ctx, WriteRequest{
		Path:    "force_target.txt",
		Content: "line1\nline2-forced\nline3\n",
		Force:   true,
		Mode:    0o600,
	})
	if err != nil || forcedWrite.Created {
		t.Fatalf("unexpected forcedWrite: %+v, err=%v", forcedWrite, err)
	}

	// DryRun Remove and Force Remove
	dryRm, err := s.Remove(ctx, RemoveRequest{
		Path:   "force_target.txt",
		DryRun: true,
	})
	if err != nil || !dryRm.DryRun {
		t.Fatalf("unexpected dryRm: %+v, err=%v", dryRm, err)
	}

	// Modify on disk and Force Remove
	if err := os.WriteFile(target, []byte("externally touched\n"), 0o600); err != nil {
		t.Fatalf("os.WriteFile: %v", err)
	}
	if _, err := s.Remove(ctx, RemoveRequest{Path: "force_target.txt", Force: true}); err != nil {
		t.Fatalf("expected Force Remove to succeed, got %v", err)
	}

	// Argument validation errors across operations
	if _, err := s.List(ctx, ListRequest{Path: "nonexistent"}); err == nil {
		t.Fatal("expected error listing nonexistent directory")
	}
	if _, err := s.List(ctx, ListRequest{Ignore: []string{"[bad"}}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for bad ignore pattern, got %v", err)
	}
	if _, err := s.Glob(ctx, GlobRequest{Pattern: ""}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for empty glob pattern, got %v", err)
	}
	if _, err := s.Grep(ctx, GrepRequest{Pattern: ""}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for empty grep pattern, got %v", err)
	}
	if _, err := s.Grep(ctx, GrepRequest{Pattern: "foo", Include: "[bad"}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for bad grep include, got %v", err)
	}
	if _, err := s.Write(ctx, WriteRequest{Path: "", Content: "x"}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for empty write path, got %v", err)
	}
	if _, err := s.Write(ctx, WriteRequest{Path: ".", Content: "x"}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for dot write path, got %v", err)
	}
	if _, err := s.Edit(ctx, EditRequest{Path: "", OldText: "a", NewText: "b"}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for empty edit path, got %v", err)
	}
	if _, err := s.Edit(ctx, EditRequest{Path: "a.txt", OldText: "", NewText: "b"}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for empty OldText, got %v", err)
	}
	if _, err := s.Remove(ctx, RemoveRequest{Path: ""}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for empty remove path, got %v", err)
	}
	if _, err := s.Bash(ctx, BashRequest{Command: "   "}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for blank bash command, got %v", err)
	}
	if _, err := s.Fetch(ctx, FetchRequest{URL: ""}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for empty fetch URL, got %v", err)
	}
	if _, err := s.Fetch(ctx, FetchRequest{URL: "http://"}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for hostless URL, got %v", err)
	}
	if _, err := s.Download(ctx, DownloadRequest{URL: "", Path: "a.txt"}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for empty download URL, got %v", err)
	}
	if _, err := s.Download(ctx, DownloadRequest{URL: "https://example.com/a", Path: ""}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for empty download path, got %v", err)
	}

	// Bash with stdin, env, and timeout
	bashStdinRes, err := s.Bash(ctx, BashRequest{
		Command: "cat && printf ':%s' \"$CUSTOM_VAR\"",
		Stdin:   "from-stdin",
		Env:     map[string]string{"CUSTOM_VAR": "val"},
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Bash with stdin/env failed: %v", err)
	}
	if bashStdinRes.Stdout != "from-stdin:val" {
		t.Fatalf("expected 'from-stdin:val', got %q", bashStdinRes.Stdout)
	}

	// Grep with Before/After context and Limit
	if _, err := s.Write(ctx, WriteRequest{
		Path:    "ctx.txt",
		Content: "l1\nmatch1\nl3\nmatch2\nl5\n",
	}); err != nil {
		t.Fatalf("Write ctx.txt: %v", err)
	}
	ctxGrep, err := s.Grep(ctx, GrepRequest{
		Path:         "ctx.txt",
		Pattern:      "match",
		ContextLines: 1,
		Limit:        1,
	})
	if err != nil {
		t.Fatalf("Grep ctx.txt: %v", err)
	}
	if !ctxGrep.Truncated || len(ctxGrep.Matches) != 1 || len(ctxGrep.Matches[0].Before) != 1 || len(ctxGrep.Matches[0].After) != 1 {
		t.Fatalf("unexpected ctxGrep: %+v", ctxGrep)
	}
}

func TestExpectedHashCASAndUUIDv7(t *testing.T) {
	s, tracker, workDir := newTestStd(t, true)
	ctx := context.Background()

	// 1. Write a new file when ExpectedHash is non-empty -> ErrFileModified (file does not exist)
	if _, err := s.Write(ctx, WriteRequest{
		Path:         "missing.txt",
		Content:      "hello\n",
		ExpectedHash: "deadbeef",
	}); !errors.Is(err, ErrFileModified) {
		t.Fatalf("expected ErrFileModified when ExpectedHash is set for nonexistent file on Write, got %v", err)
	}

	if _, err := s.Download(ctx, DownloadRequest{
		URL:          "http://127.0.0.1:1/unused",
		Path:         "missing.txt",
		ExpectedHash: "deadbeef",
	}); !errors.Is(err, ErrFileModified) {
		t.Fatalf("expected ErrFileModified when ExpectedHash is set for nonexistent file on Download, got %v", err)
	}

	// 2. Create a file directly on disk (untracked in tracker) and mutate it using explicit ExpectedHash
	diskFile := filepath.Join(workDir, "explicit.txt")
	initialBytes := []byte("alpha\nbeta\n")
	if err := os.WriteFile(diskFile, initialBytes, 0o644); err != nil {
		t.Fatalf("os.WriteFile: %v", err)
	}
	initialHash := sha256Hex(initialBytes)

	// Mismatched ExpectedHash -> ErrFileModified on Write, Edit, Remove
	if _, err := s.Write(ctx, WriteRequest{
		Path:         "explicit.txt",
		Content:      "new\n",
		ExpectedHash: "wrong-hash",
	}); !errors.Is(err, ErrFileModified) {
		t.Fatalf("expected ErrFileModified for wrong ExpectedHash on Write, got %v", err)
	}
	if _, err := s.Edit(ctx, EditRequest{
		Path:         "explicit.txt",
		OldText:      "alpha",
		NewText:      "ALPHA",
		ExpectedHash: "wrong-hash",
	}); !errors.Is(err, ErrFileModified) {
		t.Fatalf("expected ErrFileModified for wrong ExpectedHash on Edit, got %v", err)
	}
	if _, err := s.Remove(ctx, RemoveRequest{
		Path:         "explicit.txt",
		ExpectedHash: "wrong-hash",
	}); !errors.Is(err, ErrFileModified) {
		t.Fatalf("expected ErrFileModified for wrong ExpectedHash on Remove, got %v", err)
	}

	// Matching ExpectedHash succeeds even though file wasn't read via View
	editRes, err := s.Edit(ctx, EditRequest{
		Path:         "explicit.txt",
		OldText:      "alpha",
		NewText:      "ALPHA",
		ExpectedHash: initialHash,
	})
	if err != nil {
		t.Fatalf("expected Edit with matching ExpectedHash to succeed, got %v", err)
	}

	writeRes, err := s.Write(ctx, WriteRequest{
		Path:         "explicit.txt",
		Content:      "ALPHA\nbeta\ngamma\n",
		ExpectedHash: editRes.AfterHash,
	})
	if err != nil {
		t.Fatalf("expected Write with matching ExpectedHash to succeed, got %v", err)
	}

	if _, err := s.Remove(ctx, RemoveRequest{
		Path:         "explicit.txt",
		ExpectedHash: writeRes.AfterHash,
	}); err != nil {
		t.Fatalf("expected Remove with matching ExpectedHash to succeed, got %v", err)
	}

	// Verify all recorded changes have non-empty UUIDv7 IDs (version nibble '7' at index 14)
	changes := tracker.Changes()
	if len(changes) != 3 {
		t.Fatalf("expected 3 changes, got %d", len(changes))
	}
	for i, ch := range changes {
		if len(ch.ID) != 36 || ch.ID[14] != '7' {
			t.Fatalf("change %d expected UUIDv7 ID, got %q", i, ch.ID)
		}
	}
}
