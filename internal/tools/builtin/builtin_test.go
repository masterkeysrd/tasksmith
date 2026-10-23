package builtin_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/shellguard"
	"github.com/masterkeysrd/tasksmith/internal/sandbox"
	"github.com/masterkeysrd/tasksmith/internal/std"
	"github.com/masterkeysrd/tasksmith/internal/tools"
	"github.com/masterkeysrd/tasksmith/internal/tools/builtin"
	"github.com/masterkeysrd/tasksmith/internal/workspace"
)

func setupBuiltinEnv(t *testing.T, allowLoopback bool) (string, *tools.Container, *std.Std, sandbox.Sandbox, *workspace.Workspace) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	if realDir, err := filepath.EvalSymlinks(dir); err == nil {
		dir = realDir
	}

	ws, err := workspace.Open(ctx, dir, workspace.WithBuiltinFS(builtin.FS()))
	if err != nil {
		t.Fatalf("workspace.Open failed: %v", err)
	}

	sb, err := sandbox.New(sandbox.Config{
		Mode:    sandbox.ModeLocal,
		WorkDir: dir,
		Network: sandbox.NetworkPolicy{
			AllowLoopback: allowLoopback,
		},
	})
	if err != nil {
		t.Fatalf("sandbox.New failed: %v", err)
	}
	t.Cleanup(func() {
		_ = sb.Close()
	})

	stdLib, err := std.New(std.Config{
		Sandbox: sb,
		Tracker: std.NewMemoryTracker(),
		Logger:  log.Default(),
	})
	if err != nil {
		t.Fatalf("std.New failed: %v", err)
	}

	guard := shellguard.New(shellguard.Config{
		WorkspaceDir: dir,
	})

	builtinTools, err := builtin.New(builtin.Config{
		Std:     stdLib,
		Sandbox: sb,
		Guard:   guard,
	})
	if err != nil {
		t.Fatalf("builtin.New failed: %v", err)
	}

	container, err := tools.NewContainer(tools.Config{
		Tools:     builtinTools,
		Sandbox:   sb,
		Workspace: ws,
		Logger:    log.Default(),
	})
	if err != nil {
		t.Fatalf("tools.NewContainer failed: %v", err)
	}

	return dir, container, stdLib, sb, ws
}

func TestBuiltinConfigValidate(t *testing.T) {
	_, _, stdLib, sb, _ := setupBuiltinEnv(t, false)
	guard := shellguard.New(shellguard.Config{WorkspaceDir: sb.Platform().WorkDir})

	valid := builtin.Config{
		Std:     stdLib,
		Sandbox: sb,
		Guard:   guard,
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("expected valid config, got: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*builtin.Config)
	}{
		{"nil Std", func(c *builtin.Config) { c.Std = nil }},
		{"nil Sandbox", func(c *builtin.Config) { c.Sandbox = nil }},
		{"nil Guard", func(c *builtin.Config) { c.Guard = nil }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := valid
			tc.mutate(&cfg)
			err := cfg.Validate()
			if !errors.Is(err, builtin.ErrInvalidConfig) {
				t.Fatalf("expected ErrInvalidConfig for %s, got %v", tc.name, err)
			}
			if _, err := builtin.New(cfg); !errors.Is(err, builtin.ErrInvalidConfig) {
				t.Fatalf("expected builtin.New to return ErrInvalidConfig, got %v", err)
			}
		})
	}

	// Workspace opened without builtin.FS() must fail with tools.ErrToolNotFound in tools.NewContainer.
	emptyWS, err := workspace.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	builtinTools, err := builtin.New(valid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tools.NewContainer(tools.Config{
		Tools:     builtinTools,
		Sandbox:   sb,
		Workspace: emptyWS,
		Logger:    log.Default(),
	}); !errors.Is(err, tools.ErrToolNotFound) {
		t.Fatalf("expected tools.ErrToolNotFound when workspace lacks built-in manifests, got %v", err)
	}
}

func TestFSAndWorkspaceResolution(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	ws, err := workspace.Open(ctx, dir, workspace.WithBuiltinFS(builtin.FS()))
	if err != nil {
		t.Fatalf("workspace.Open failed: %v", err)
	}

	expected := []string{
		builtin.ToolBash,
		builtin.ToolDownload,
		builtin.ToolEdit,
		builtin.ToolGlob,
		builtin.ToolGrep,
		builtin.ToolLS,
		builtin.ToolRemove,
		builtin.ToolView,
		builtin.ToolWebFetch,
		builtin.ToolWrite,
	}

	resolvedTools := ws.Tools()
	var gotNames []string
	for _, rt := range resolvedTools {
		gotNames = append(gotNames, rt.Name)
		if rt.Namespace != workspace.NamespaceSystem {
			t.Errorf("expected tool %q to be in namespace %q, got %q", rt.Name, workspace.NamespaceSystem, rt.Namespace)
		}
		if rt.DisplayName == "" {
			t.Errorf("expected tool %q to have DisplayName", rt.Name)
		}
		if rt.Description == "" {
			t.Errorf("expected tool %q to have Description", rt.Name)
		}
		if rt.Instructions == "" {
			t.Errorf("expected tool %q to have markdown Instructions", rt.Name)
		}
		if rt.Annotations == nil {
			t.Errorf("expected tool %q to have Annotations", rt.Name)
		}
	}
	slices.Sort(gotNames)
	if !slices.Equal(gotNames, expected) {
		t.Fatalf("expected resolved tools %v, got %v", expected, gotNames)
	}
}

func TestReadOnlyToolsPrepareEvaluateExecute(t *testing.T) {
	ctx := context.Background()
	dir, c, _, _, _ := setupBuiltinEnv(t, false)

	if err := os.MkdirAll(filepath.Join(dir, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pkg", "hello.go"), []byte("package pkg\n\nfunc Hello() string { return \"world\" }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	calls := []*message.ToolCall{
		{
			ID:   "call-view",
			Name: builtin.ToolView,
			Args: map[string]any{"path": "pkg/hello.go"},
		},
		{
			ID:   "call-ls",
			Name: builtin.ToolLS,
			Args: map[string]any{"path": "pkg"},
		},
		{
			ID:   "call-glob",
			Name: builtin.ToolGlob,
			Args: map[string]any{"pattern": "**/*.go"},
		},
		{
			ID:   "call-grep",
			Name: builtin.ToolGrep,
			Args: map[string]any{"pattern": "Hello", "path": "pkg"},
		},
	}

	for _, tc := range calls {
		t.Run(tc.Name, func(t *testing.T) {
			plan, err := c.Prepare(ctx, tc)
			if err != nil {
				t.Fatalf("Prepare(%s) failed: %v", tc.Name, err)
			}
			if plan.Action != tools.ActionRead || plan.Safety != tools.SafetySafe || !plan.ReadOnly {
				t.Fatalf("unexpected plan for %s: %+v", tc.Name, plan)
			}

			dec, permReq, err := c.Evaluate(plan)
			if err != nil {
				t.Fatalf("Evaluate(%s) failed: %v", tc.Name, err)
			}
			if dec != tools.AccessAllow || permReq != nil {
				t.Fatalf("expected AccessAllow and nil permReq for %s, got %s, %+v", tc.Name, dec, permReq)
			}

			res, err := c.Execute(ctx, tc)
			if err != nil {
				t.Fatalf("Execute(%s) failed: %v", tc.Name, err)
			}
			if res.IsError {
				t.Fatalf("Execute(%s) returned tool error: %s", tc.Name, res.Content.Text())
			}
			parsedID, parseErr := uuid.Parse(res.ID)
			if parseErr != nil || parsedID.Version() != 7 {
				t.Fatalf("expected UUIDv7 on tool result message, got %q (err: %v)", res.ID, parseErr)
			}
			if res.StructuredContent == nil {
				t.Fatalf("expected non-nil structured content for %s", tc.Name)
			}
		})
	}
}

func TestWriteEditAndCASConflict(t *testing.T) {
	ctx := context.Background()
	dir, c, _, _, _ := setupBuiltinEnv(t, false)

	// 1. Write new file (DryRun in Prepare, then Execute)
	writeCall := &message.ToolCall{
		ID:   "call-write-1",
		Name: builtin.ToolWrite,
		Args: map[string]any{
			"path":    "sub/newfile.txt",
			"content": "line 1\nline 2\n",
		},
	}

	plan, err := c.Prepare(ctx, writeCall)
	if err != nil {
		t.Fatalf("Prepare(write) failed: %v", err)
	}
	if plan.Action != tools.ActionWrite || plan.Safety != tools.SafetySafe {
		t.Fatalf("expected ActionWrite + SafetySafe, got %+v", plan)
	}
	if !strings.Contains(plan.Diff, "+line 1") {
		t.Fatalf("expected unified diff preview in plan, got %q", plan.Diff)
	}
	dec, permReq, err := c.Evaluate(plan)
	if err != nil || dec != tools.AccessAsk || permReq == nil {
		t.Fatalf("expected AccessAsk with PermissionRequest, got %s, %+v, %v", dec, permReq, err)
	}
	if !slices.Contains(permReq.GrantPatterns, "sub/**") {
		t.Fatalf("expected sub/** in permReq.GrantPatterns, got %v", permReq.GrantPatterns)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "sub", "newfile.txt")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected file not to exist after Prepare DryRun")
	}

	res, err := c.Execute(ctx, writeCall)
	if err != nil || res.IsError {
		t.Fatalf("Execute(write) failed: err=%v, text=%s", err, res.Content.Text())
	}

	// 2. Edit unread file fails in Prepare with std.ErrFileNotRead
	filePath := filepath.Join(dir, "config.txt")
	if err := os.WriteFile(filePath, []byte("port=8080\nhost=localhost\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	editCall := &message.ToolCall{
		ID:   "call-edit-1",
		Name: builtin.ToolEdit,
		Args: map[string]any{
			"path":     "config.txt",
			"old_text": "port=8080",
			"new_text": "port=9090",
		},
	}
	if _, err := c.Prepare(ctx, editCall); !errors.Is(err, std.ErrFileNotRead) {
		t.Fatalf("expected std.ErrFileNotRead from Prepare on unread file, got %v", err)
	}

	// 3. View the file first
	viewCall := &message.ToolCall{
		ID:   "call-view-1",
		Name: builtin.ToolView,
		Args: map[string]any{"path": "config.txt"},
	}
	if _, err := c.Execute(ctx, viewCall); err != nil {
		t.Fatalf("Execute(view) failed: %v", err)
	}

	// 4. Prepare(edit) produces unified Diff without modifying FileTracker state
	editPlan, err := c.Prepare(ctx, editCall)
	if err != nil {
		t.Fatalf("Prepare(edit) after view failed: %v", err)
	}
	if !strings.Contains(editPlan.Diff, "-port=8080") || !strings.Contains(editPlan.Diff, "+port=9090") {
		t.Fatalf("expected unified diff in Plan, got %q", editPlan.Diff)
	}

	// 5. Modify the file on disk between Prepare and Execute -> CAS conflict!
	if err := os.WriteFile(filePath, []byte("port=7070\nhost=localhost\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	casRes, err := c.Execute(ctx, editCall)
	if err != nil {
		t.Fatalf("Execute returned fatal error instead of tool error: %v", err)
	}
	if !casRes.IsError || !strings.Contains(casRes.Content.Text(), std.ErrFileModified.Error()) {
		t.Fatalf("expected IsError with ErrFileModified, got isError=%v, text=%q", casRes.IsError, casRes.Content.Text())
	}

	// 6. Re-read with view, Prepare, and Execute cleanly
	if _, err := c.Execute(ctx, viewCall); err != nil {
		t.Fatal(err)
	}
	editCall2 := &message.ToolCall{
		ID:   "call-edit-2",
		Name: builtin.ToolEdit,
		Args: map[string]any{
			"path":     "config.txt",
			"old_text": "port=7070",
			"new_text": "port=9090",
		},
	}
	if _, err := c.Prepare(ctx, editCall2); err != nil {
		t.Fatal(err)
	}
	cleanRes, err := c.Execute(ctx, editCall2)
	if err != nil || cleanRes.IsError {
		t.Fatalf("expected clean edit execution, got err=%v, text=%s", err, cleanRes.Content.Text())
	}
}

func TestBashWebFetchDownloadRemoveAndEdgeCases(t *testing.T) {
	ctx := context.Background()
	dir, c, _, _, _ := setupBuiltinEnv(t, true)

	// 1. Bash classifications and execution
	for _, bashCase := range []struct {
		cmd    string
		dir    string
		action tools.ActionType
		safety tools.SafetyLevel
	}{
		{"echo hello", "", tools.ActionRead, tools.SafetySafe},
		{"rm -rf /", "", tools.ActionDelete, tools.SafetyUnsafe},
		{"mkdir sub", ".", tools.ActionWrite, tools.SafetySafe},
		{"echo 'unterminated", "", tools.ActionExec, tools.SafetyUnsafe},
		{"cd .", "", tools.ActionRead, tools.SafetySafe},
		{"echo hello > $DYNAMIC_OUT", "", tools.ActionWrite, tools.SafetyUnknown},
		{"./run.sh", "", tools.ActionExec, tools.SafetySafe},
	} {
		args := map[string]any{"command": bashCase.cmd}
		if bashCase.dir != "" {
			args["dir"] = bashCase.dir
		}
		p, err := c.Prepare(ctx, &message.ToolCall{
			ID:   "bash-case",
			Name: builtin.ToolBash,
			Args: args,
		})
		if err != nil {
			t.Fatalf("Prepare(bash %q) failed: %v", bashCase.cmd, err)
		}
		if p.Action != bashCase.action || p.Safety != bashCase.safety {
			t.Fatalf("for bash %q: expected (%s, %s), got (%s, %s)", bashCase.cmd, bashCase.action, bashCase.safety, p.Action, p.Safety)
		}
	}

	echoRes, err := c.Execute(ctx, &message.ToolCall{
		ID:   "bash-echo",
		Name: builtin.ToolBash,
		Args: map[string]any{"command": "echo hello", "timeout_ms": 5000},
	})
	if err != nil || echoRes.IsError {
		t.Fatalf("Execute(echo hello) failed: err=%v, res=%+v", err, echoRes)
	}
	bashOut, ok := echoRes.StructuredContent.(builtin.BashOutput)
	if !ok || !strings.Contains(bashOut.Stdout, "hello") {
		t.Fatalf("unexpected bash output: %+v", echoRes.StructuredContent)
	}

	// 2. web_fetch + download + remove
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = fmt.Fprint(w, "remote payload\n")
	}))
	defer ts.Close()

	fetchCall := &message.ToolCall{
		ID:   "call-fetch",
		Name: builtin.ToolWebFetch,
		Args: map[string]any{
			"url":        ts.URL + "/hello",
			"method":     "POST",
			"body":       "ping",
			"timeout_ms": 5000,
		},
	}
	fetchPlan, err := c.Prepare(ctx, fetchCall)
	if err != nil || fetchPlan.Action != tools.ActionNet || fetchPlan.Safety != tools.SafetyUnknown || !fetchPlan.ReadOnly {
		t.Fatalf("unexpected web_fetch plan: %+v, err=%v", fetchPlan, err)
	}
	fetchRes, err := c.Execute(ctx, fetchCall)
	if err != nil || fetchRes.IsError {
		t.Fatalf("Execute(web_fetch) failed: err=%v, res=%+v", err, fetchRes)
	}
	fetchOut, ok := fetchRes.StructuredContent.(builtin.WebFetchOutput)
	if !ok || !strings.Contains(fetchOut.Text, "remote payload") {
		t.Fatalf("unexpected web_fetch output: %+v", fetchRes.StructuredContent)
	}

	// Download to existing unread file fails in Prepare with std.ErrFileNotRead
	existingUnread := filepath.Join(dir, "existing_unread.txt")
	if err := os.WriteFile(existingUnread, []byte("initial\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dlUnreadCall := &message.ToolCall{
		ID:   "call-dl-unread",
		Name: builtin.ToolDownload,
		Args: map[string]any{"url": ts.URL + "/file.txt", "path": "existing_unread.txt"},
	}
	if _, err := c.Prepare(ctx, dlUnreadCall); !errors.Is(err, std.ErrFileNotRead) {
		t.Fatalf("expected std.ErrFileNotRead on download to unread existing file, got %v", err)
	}

	// Download to new file
	dlCall := &message.ToolCall{
		ID:   "call-dl",
		Name: builtin.ToolDownload,
		Args: map[string]any{
			"url":        ts.URL + "/file.txt",
			"path":       "downloaded.txt",
			"timeout_ms": 5000,
		},
	}
	dlPlan, err := c.Prepare(ctx, dlCall)
	if err != nil || dlPlan.Action != tools.ActionWrite || dlPlan.Safety != tools.SafetyUnknown {
		t.Fatalf("unexpected download plan: %+v, err=%v", dlPlan, err)
	}
	dlRes, err := c.Execute(ctx, dlCall)
	if err != nil || dlRes.IsError {
		t.Fatalf("Execute(download) failed: err=%v, text=%s", err, dlRes.Content.Text())
	}

	// Remove downloaded file
	rmCall := &message.ToolCall{
		ID:   "call-rm",
		Name: builtin.ToolRemove,
		Args: map[string]any{"path": "downloaded.txt"},
	}
	rmPlan, err := c.Prepare(ctx, rmCall)
	if err != nil || rmPlan.Action != tools.ActionDelete || rmPlan.Safety != tools.SafetyUnknown {
		t.Fatalf("unexpected remove plan: %+v, err=%v", rmPlan, err)
	}
	rmRes, err := c.Execute(ctx, rmCall)
	if err != nil || rmRes.IsError {
		t.Fatalf("Execute(remove) failed: err=%v, text=%s", err, rmRes.Content.Text())
	}

	// 3. Protected paths and outside/sensitive path classification in Prepare + Gate.Evaluate
	protPlan, err := c.Prepare(ctx, &message.ToolCall{
		ID:   "prot-write",
		Name: builtin.ToolWrite,
		Args: map[string]any{"path": ".agents/config.yaml", "content": "x: 1\n"},
	})
	if err != nil || protPlan.Safety != tools.SafetyUnsafe {
		t.Fatalf("expected SafetyUnsafe for .agents/ write, got %+v, err=%v", protPlan, err)
	}

	// Read-only tools targeting ../somefile.txt produce SafetyOutside and evaluate to AccessAsk,
	// and can be granted for the session via DecisionAllowSession.
	for _, roOutside := range []*message.ToolCall{
		{ID: "out-view", Name: builtin.ToolView, Args: map[string]any{"path": "../somefile.txt"}},
		{ID: "out-ls", Name: builtin.ToolLS, Args: map[string]any{"path": "../somefile.txt"}},
		{ID: "out-glob", Name: builtin.ToolGlob, Args: map[string]any{"pattern": "*.go", "path": "../somefile.txt"}},
		{ID: "out-grep", Name: builtin.ToolGrep, Args: map[string]any{"pattern": "foo", "path": "../somefile.txt"}},
	} {
		outPlan, err := c.Prepare(ctx, roOutside)
		if err != nil {
			t.Fatalf("expected Prepare(%s) on ../somefile.txt to succeed, got %v", roOutside.Name, err)
		}
		if outPlan.Safety != tools.SafetyOutside || outPlan.Action != tools.ActionRead || !outPlan.ReadOnly {
			t.Fatalf("expected SafetyOutside + ActionRead + ReadOnly for %s on ../somefile.txt, got %+v", roOutside.Name, outPlan)
		}
		dec, permReq, err := c.Evaluate(outPlan)
		if err != nil || dec != tools.AccessAsk || permReq == nil {
			t.Fatalf("expected AccessAsk for %s on SafetyOutside plan, got %s, %+v, %v", roOutside.Name, dec, permReq, err)
		}
		if err := c.Gate().ApplyDecision(permReq, tools.PermissionDecision{Kind: tools.DecisionAllowSession}); err != nil {
			t.Fatalf("ApplyDecision(DecisionAllowSession) failed: %v", err)
		}
		if dec2, _, err := c.Evaluate(outPlan); err != nil || dec2 != tools.AccessAllow {
			t.Fatalf("expected AccessAllow after session grant for SafetyOutside (%s), got %s, %v", roOutside.Name, dec2, err)
		}
		c.ClearGrants()
	}

	// Read-only tools targeting sensitive system/credential paths (/etc/hosts, ~/.ssh/id_rsa)
	// produce SafetyUnsafe and always prompt even if a wildcard session grant exists.
	c.AddGrant(tools.Grant{ToolName: "*", Action: tools.ActionRead, Pattern: "*"})
	for _, sensPath := range []string{"/etc/hosts", "~/.ssh/id_rsa"} {
		for _, roSens := range []*message.ToolCall{
			{ID: "sens-view", Name: builtin.ToolView, Args: map[string]any{"path": sensPath}},
			{ID: "sens-ls", Name: builtin.ToolLS, Args: map[string]any{"path": sensPath}},
			{ID: "sens-glob", Name: builtin.ToolGlob, Args: map[string]any{"pattern": "*", "path": sensPath}},
			{ID: "sens-grep", Name: builtin.ToolGrep, Args: map[string]any{"pattern": "root", "path": sensPath}},
		} {
			sensPlan, err := c.Prepare(ctx, roSens)
			if err != nil {
				t.Fatalf("expected Prepare(%s, %q) to succeed, got %v", roSens.Name, sensPath, err)
			}
			if sensPlan.Safety != tools.SafetyUnsafe {
				t.Fatalf("expected SafetyUnsafe for %s on %q, got %s", roSens.Name, sensPath, sensPlan.Safety)
			}
			dec, permReq, err := c.Evaluate(sensPlan)
			if err != nil || dec != tools.AccessAsk || permReq == nil {
				t.Fatalf("expected SafetyUnsafe on %s (%q) to always evaluate to AccessAsk despite session grant, got %s", roSens.Name, sensPath, dec)
			}
		}
	}
	c.ClearGrants()

	for _, bad := range []*message.ToolCall{
		{ID: "1", Name: builtin.ToolView, Args: map[string]any{"path": "   "}},
		{ID: "2", Name: builtin.ToolGlob, Args: map[string]any{"pattern": "   "}},
		{ID: "3", Name: builtin.ToolGrep, Args: map[string]any{"pattern": ""}},
		{ID: "4", Name: builtin.ToolBash, Args: map[string]any{"command": "   "}},
		{ID: "5", Name: builtin.ToolWebFetch, Args: map[string]any{"url": "   "}},
		{ID: "6", Name: builtin.ToolWebFetch, Args: map[string]any{"url": "ftp://example.com"}},
		{ID: "7", Name: builtin.ToolWebFetch, Args: map[string]any{"url": "http://"}},
	} {
		if _, err := c.Prepare(ctx, bad); !errors.Is(err, std.ErrInvalidArgument) {
			t.Fatalf("expected ErrInvalidArgument for %s (%v), got %v", bad.Name, bad.Args, err)
		}
	}
}
