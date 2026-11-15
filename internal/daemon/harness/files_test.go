package harness

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/daemon/permissions"
	"github.com/masterkeysrd/tasksmith/internal/daemon/sandbox"
	"github.com/masterkeysrd/tasksmith/internal/daemon/std"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
	"github.com/masterkeysrd/warp"
)

func setupTestResolver(t *testing.T) (*AttachmentResolver, *std.MemoryTracker, string) {
	t.Helper()
	xdg.SetTestEnv(t.TempDir(), "tasksmith-files-test")
	t.Cleanup(xdg.ClearCache)

	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docs", "readme.md"), []byte("# Docs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("SECRET=123\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env.example"), []byte("SECRET=xxx\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	sb, err := sandbox.New(sandbox.Config{
		Mode:    sandbox.ModeLocal,
		WorkDir: dir,
	})
	if err != nil {
		t.Fatalf("sandbox.New: %v", err)
	}
	t.Cleanup(func() {
		_ = sb.Close()
	})

	tracker := std.NewMemoryTracker()
	stdLib, err := std.New(std.Config{
		Sandbox: sb,
		Tracker: tracker,
		Logger:  log.Default(),
	})
	if err != nil {
		t.Fatalf("std.New: %v", err)
	}

	ws, err := workspace.Open(context.Background(), dir)
	if err != nil {
		t.Fatalf("workspace.Open: %v", err)
	}

	pm := permissions.NewSession(api.ModeAuto, api.Permissions{}, nil)

	resolver, err := NewAttachmentResolver(AttachmentResolverConfig{
		Workspace:   ws,
		Std:         stdLib,
		Sandbox:     sb,
		Permissions: pm,
		WorkDir:     dir,
		Logger:      log.Default(),
	})
	if err != nil {
		t.Fatalf("NewAttachmentResolver: %v", err)
	}
	return resolver, tracker, dir
}

func TestCheckAccess(t *testing.T) {
	resolver, _, dir := setupTestResolver(t)
	ctx := context.Background()

	// 1. Allowed file
	if err := resolver.CheckFileAccess(ctx, FileMention{Path: "main.go"}); err != nil {
		t.Errorf("main.go expected allowed, got error: %v", err)
	}

	// 2. Allowed directory
	if err := resolver.CheckFileAccess(ctx, FileMention{Path: "docs/", IsDir: true}); err != nil {
		t.Errorf("docs/ expected allowed, got error: %v", err)
	}

	// 3. Allowed .env.example
	if err := resolver.CheckFileAccess(ctx, FileMention{Path: ".env.example"}); err != nil {
		t.Errorf(".env.example expected allowed, got error: %v", err)
	}

	// 4. Secret .env file rejected
	if err := resolver.CheckFileAccess(ctx, FileMention{Path: ".env"}); err == nil {
		t.Errorf(".env expected error, got nil")
	}

	// 5. Escaping path rejected
	if err := resolver.CheckFileAccess(ctx, FileMention{Path: "../escaping.txt"}); err == nil {
		t.Errorf("../escaping.txt expected error, got nil")
	}

	// 6. Symlink pointing outside workspace rejected
	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "outside.txt")
	if err := os.WriteFile(outsideFile, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideFile, filepath.Join(dir, "escape_link.txt")); err != nil {
		t.Fatal(err)
	}
	if err := resolver.CheckFileAccess(ctx, FileMention{Path: "escape_link.txt"}); err == nil {
		t.Errorf("symlink outside workspace expected error, got nil")
	}

	// 7. In-workspace symlink pointing to .env rejected
	if err := os.Symlink(filepath.Join(dir, ".env"), filepath.Join(dir, "innocent.txt")); err != nil {
		t.Fatal(err)
	}
	if err := resolver.CheckFileAccess(ctx, FileMention{Path: "innocent.txt"}); err == nil {
		t.Errorf("symlink to .env expected error, got nil")
	}

	// 8. Internal repo dir rejected
	if err := resolver.CheckFileAccess(ctx, FileMention{Path: ".git/config"}); err == nil {
		t.Errorf(".git/config expected error, got nil")
	}

	// 9. Deny rule rejected (both direct and via symlink)
	if err := os.Symlink(filepath.Join(dir, "main.go"), filepath.Join(dir, "alias_main.go")); err != nil {
		t.Fatal(err)
	}
	if err := resolver.perms.AddDeny("view(main.go)"); err != nil {
		t.Fatal(err)
	}
	if err := resolver.CheckFileAccess(ctx, FileMention{Path: "main.go"}); err == nil {
		t.Errorf("denied main.go expected error, got nil")
	}
	if err := resolver.CheckFileAccess(ctx, FileMention{Path: "alias_main.go"}); err == nil {
		t.Errorf("symlink to denied main.go expected error, got nil")
	}

	// 10. Policy ceiling rejected
	resolver.perms.SetPolicies(&warp.Policies{
		Tools: &warp.ToolPolicies{
			Exclude: []string{"view"},
		},
	})
	if err := resolver.CheckFileAccess(ctx, FileMention{Path: "docs/readme.md"}); err == nil {
		t.Errorf("policy-excluded view expected error, got nil")
	}
}

func TestResolveUserMessage(t *testing.T) {
	resolver, tracker, _ := setupTestResolver(t)
	ctx := context.Background()

	// 1. Text containing @file:.env without Attachments does NOT parse or inject
	plainMsg := message.NewUserText("Please review @file:.env")
	_, plainReminder, _ := resolver.ResolveUserMessage(ctx, plainMsg, State{})
	if plainReminder != nil {
		t.Fatalf("expected nil reminder when message has no attachments, got: %+v", plainReminder)
	}

	// 2. Structured attachments: single file with ref, directory with ref, root dir without ref
	msg := message.NewUserText("Please review @main.go#L1-L2, @docs/, and root ./")
	SetAttachments(msg, []api.Attachment{
		{Kind: api.AttachmentFile, Ref: "@main.go#L1-L2", Target: "main.go", StartLine: 1, EndLine: 2},
		{Kind: api.AttachmentFile, Ref: "@docs/", Target: "docs/"},
		{Kind: api.AttachmentFile, Target: "./"},
	})

	_, reminder, _ := resolver.ResolveUserMessage(ctx, msg, State{})
	if reminder == nil {
		t.Fatalf("expected non-nil reminder")
	}

	text := reminder.GetContent().Text()
	if !strings.Contains(text, "<system_reminders>") || !strings.Contains(text, "</system_reminders>") {
		t.Errorf("missing <system_reminders> tags in %q", text)
	}
	if !strings.Contains(text, `<context_file ref="@main.go#L1-L2" path="main.go" lines="1-2">`) {
		t.Errorf("missing context_file tag with ref in %q", text)
	}
	if !strings.Contains(text, "package main") {
		t.Errorf("missing line 1 content in %q", text)
	}
	if !strings.Contains(text, `<context_directory ref="@docs/" path="docs/">`) || !strings.Contains(text, "readme.md") {
		t.Errorf("missing docs/ directory listing with ref in %q", text)
	}
	if !strings.Contains(text, `<context_directory path=".">`) {
		t.Errorf("missing root context_directory tag without ref in %q", text)
	}
	if !strings.Contains(text, ".env.example") {
		t.Errorf("expected safe hidden template .env.example in directory listing: %q", text)
	}
	if strings.Contains(text, "- .env (") {
		t.Errorf("secret .env must not appear in directory listing: %q", text)
	}

	// Verify CAS read was tracked in memory tracker
	recent := tracker.RecentPaths()
	if !slices.Contains(recent, "main.go") {
		t.Errorf("expected main.go in RecentPaths, got: %v", recent)
	}

	// 3. Deduplication: mentioning same file & range twice only injects once
	dedupMsg := message.NewUserText("Compare @main.go and @main.go")
	SetAttachments(dedupMsg, []api.Attachment{
		{Kind: api.AttachmentFile, Ref: "@main.go", Target: "main.go"},
		{Kind: api.AttachmentFile, Ref: "@main.go", Target: "main.go"},
	})
	_, dedupReminder, _ := resolver.ResolveUserMessage(ctx, dedupMsg, State{})
	if dedupReminder == nil {
		t.Fatalf("expected non-nil dedup reminder")
	}
	dedupText := dedupReminder.GetContent().Text()
	if strings.Count(dedupText, `<context_file`) != 1 {
		t.Errorf("expected exactly 1 <context_file> injection for duplicate mention, got:\n%s", dedupText)
	}

	// 4. Two different line slices of the same file inject both slices
	multiSliceMsg := message.NewUserText("Compare @main.go#L1-L1 and @main.go#L2-L2")
	SetAttachments(multiSliceMsg, []api.Attachment{
		{Kind: api.AttachmentFile, Ref: "@main.go#L1-L1", Target: "main.go", StartLine: 1, EndLine: 1},
		{Kind: api.AttachmentFile, Ref: "@main.go#L2-L2", Target: "main.go", StartLine: 2, EndLine: 2},
	})
	_, multiReminder, _ := resolver.ResolveUserMessage(ctx, multiSliceMsg, State{})
	if multiReminder == nil {
		t.Fatalf("expected non-nil multiReminder")
	}
	multiText := multiReminder.GetContent().Text()
	if strings.Count(multiText, `<context_file`) != 2 {
		t.Errorf("expected 2 distinct <context_file> slices, got:\n%s", multiText)
	}
	if !strings.Contains(multiText, `lines="1-1"`) || !strings.Contains(multiText, `lines="2-2"`) {
		t.Errorf("expected both line ranges in output, got:\n%s", multiText)
	}
}
