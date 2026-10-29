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
	"time"

	"github.com/google/uuid"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/shellguard"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/daemon/permissions"
	"github.com/masterkeysrd/tasksmith/internal/daemon/sandbox"
	"github.com/masterkeysrd/tasksmith/internal/daemon/scope"
	"github.com/masterkeysrd/tasksmith/internal/daemon/std"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools/builtin"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
	"github.com/masterkeysrd/warp"
)

func setupBuiltinEnv(t *testing.T, allowLoopback bool) (string, tools.Container, *std.Std, sandbox.Sandbox, workspace.Workspace) {
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
		Std:         stdLib,
		Sandbox:     sb,
		Guard:       guard,
		Workspace:   ws,
		RenderSkill: func(name string, args map[string]string) (string, error) { return ws.RenderSkill(name, args, nil) },
	})
	if err != nil {
		t.Fatalf("builtin.New failed: %v", err)
	}

	sp, err := tools.NewSpiller(tools.SpillerConfig{
		BaseDir: filepath.Join(dir, ".spill"),
	})
	if err != nil {
		t.Fatalf("tools.NewSpiller failed: %v", err)
	}

	container, err := tools.NewContainer(tools.Config{
		Tools:     builtinTools,
		Sandbox:   sb,
		Workspace: ws,
		Logger:    log.Default(),
		Spiller:   sp,
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

	// Workspace opened without builtin.FS() must fail with tools.ErrToolNotFound in builtin.New.
	emptyWS, err := workspace.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfgWithEmptyWS := valid
	cfgWithEmptyWS.Workspace = emptyWS
	if _, err := builtin.New(cfgWithEmptyWS); !errors.Is(err, tools.ErrToolNotFound) {
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
		builtin.ToolAskQuestion,
		builtin.ToolBash,
		builtin.ToolDownload,
		builtin.ToolEdit,
		builtin.ToolGlob,
		builtin.ToolGrep,
		builtin.ToolLoadSkill,
		builtin.ToolLS,
		builtin.ToolRemove,
		builtin.ToolTasks,
		builtin.ToolTodos,
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
	dir, c, _, _, ws := setupBuiltinEnv(t, false)

	permMgr, err := permissions.New(permissions.Config{
		Workspace:  ws,
		GlobalPath: filepath.Join(t.TempDir(), "permissions.json"),
	})
	if err != nil {
		t.Fatalf("permissions.New: %v", err)
	}
	perm := permMgr.Attach(tools.ModeDefault, permissions.Permissions{}, nil)

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

			dec, permReq, err := perm.Evaluate(plan)
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
	dir, c, _, _, ws := setupBuiltinEnv(t, false)

	permMgr, err := permissions.New(permissions.Config{
		Workspace:  ws,
		GlobalPath: filepath.Join(t.TempDir(), "permissions.json"),
	})
	if err != nil {
		t.Fatalf("permissions.New: %v", err)
	}
	perm := permMgr.Attach(tools.ModeDefault, permissions.Permissions{}, nil)

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
	dec, permReq, err := perm.Evaluate(plan)
	if err != nil || dec != tools.AccessAsk || permReq == nil {
		t.Fatalf("expected AccessAsk with PermissionRequest, got %s, %+v, %v", dec, permReq, err)
	}
	if !slices.Contains(permReq.GrantPatterns, "write(sub/**)") {
		t.Fatalf("expected write(sub/**) in permReq.GrantPatterns, got %v", permReq.GrantPatterns)
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
	dir, c, _, _, ws := setupBuiltinEnv(t, true)

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
		Args: map[string]any{"command": "echo hello"},
	})
	if err != nil || echoRes.IsError {
		t.Fatalf("Execute(echo hello) failed: err=%v, res=%+v", err, echoRes)
	}
	bashOut, ok := echoRes.StructuredContent.(api.BashOutput)
	if !ok || !strings.Contains(bashOut.Output, "hello") {
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
			"url": ts.URL + "/hello",
		},
	}
	fetchPlan, err := c.Prepare(ctx, fetchCall)
	if err != nil || fetchPlan.Action != tools.ActionNet || fetchPlan.Safety != tools.SafetyUnknown || !fetchPlan.ReadOnly {
		t.Fatalf("unexpected web_fetch plan: %+v, err=%v", fetchPlan, err)
	}
	fetchRes, err := c.Execute(ctx, fetchCall)
	if err != nil || fetchRes.IsError {
		t.Fatalf("Execute(web_fetch) failed: err=%v, res=%+v, text=%s", err, fetchRes, fetchRes.Content.Text())
	}
	fetchOut, ok := fetchRes.StructuredContent.(api.WebFetchOutput)
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

	permMgr, err := permissions.New(permissions.Config{
		Workspace:  ws,
		GlobalPath: filepath.Join(t.TempDir(), "permissions.json"),
	})
	if err != nil {
		t.Fatalf("permissions.New: %v", err)
	}
	perm := permMgr.Attach(tools.ModeDefault, permissions.Permissions{}, nil)

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
		dec, permReq, err := perm.Evaluate(outPlan)
		if err != nil || dec != tools.AccessAsk || permReq == nil {
			t.Fatalf("expected AccessAsk for %s on SafetyOutside plan, got %s, %+v, %v", roOutside.Name, dec, permReq, err)
		}
		if err := perm.ApplyDecision(permReq, tools.PermissionDecision{Kind: tools.DecisionAllowSession}); err != nil {
			t.Fatalf("ApplyDecision(DecisionAllowSession) failed: %v", err)
		}
		if dec2, _, err := perm.Evaluate(outPlan); err != nil || dec2 != tools.AccessAllow {
			t.Fatalf("expected AccessAllow after session grant for SafetyOutside (%s), got %s, %v", roOutside.Name, dec2, err)
		}
		_ = perm.Clear()
	}

	// Read-only tools targeting sensitive system/credential paths (/etc/hosts, ~/.ssh/id_rsa)
	// produce SafetyUnsafe and always prompt even if a wildcard session grant exists.
	_ = perm.AddAllow("*(*)")
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
			dec, permReq, err := perm.Evaluate(sensPlan)
			if err != nil || dec != tools.AccessAsk || permReq == nil {
				t.Fatalf("expected SafetyUnsafe on %s (%q) to always evaluate to AccessAsk despite session grant, got %s", roSens.Name, sensPath, dec)
			}
		}
	}
	_ = perm.Clear()

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

func TestBashPrepareDescription(t *testing.T) {
	_, c, _, _, _ := setupBuiltinEnv(t, false)
	ctx := context.Background()

	plan, err := c.Prepare(ctx, &message.ToolCall{
		ID:   "call-desc",
		Name: builtin.ToolBash,
		Args: map[string]any{
			"command":     "go test ./...",
			"description": "Run project unit tests",
		},
	})
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
	if plan.Summary != "Run project unit tests" {
		t.Fatalf("expected Summary %q, got %q", "Run project unit tests", plan.Summary)
	}

	planNoDesc, err := c.Prepare(ctx, &message.ToolCall{
		ID:   "call-nodesc",
		Name: builtin.ToolBash,
		Args: map[string]any{
			"command": "go test ./...",
		},
	})
	if err != nil {
		t.Fatalf("Prepare without desc failed: %v", err)
	}
	if planNoDesc.Summary != "Run shell command: go test ./..." {
		t.Fatalf("expected default Summary, got %q", planNoDesc.Summary)
	}
}

func TestBashStreamingSpill(t *testing.T) {
	_, c, _, _, _ := setupBuiltinEnv(t, false)
	ctx := scope.With(context.Background(), scope.Scope{
		SessionID:  "sess-bash-spill",
		ToolCallID: "call-bash-spill",
	})

	// Command outputs 600 lines total (> DefaultSpillMaxLines 500), mixing stdout and stderr
	cmd := `for i in $(seq 1 300); do echo "stdout line $i"; echo "stderr line $i" >&2; done`
	res, err := c.Execute(ctx, &message.ToolCall{
		ID:   "call-bash-spill",
		Name: builtin.ToolBash,
		Args: map[string]any{
			"command":     cmd,
			"description": "Generate high volume output mixing stdout and stderr",
		},
	})
	if err != nil || res.IsError {
		t.Fatalf("Execute(bash spill) failed: err=%v, res=%+v", err, res)
	}

	bashOut, ok := res.StructuredContent.(api.BashOutput)
	if !ok {
		t.Fatalf("expected api.BashOutput, got: %T", res.StructuredContent)
	}
	if bashOut.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", bashOut.ExitCode)
	}

	// Verify truncation preview in Output
	if !strings.Contains(bashOut.Output, "truncated — full output saved to") {
		t.Fatalf("expected truncation notice in bash output, got:\n%s", bashOut.Output)
	}

	// Extract spilled file path from notice
	startIdx := strings.Index(bashOut.Output, "full output saved to ")
	if startIdx == -1 {
		t.Fatalf("did not find 'full output saved to ' in notice: %s", bashOut.Output)
	}
	startPath := startIdx + len("full output saved to ")
	endIdx := strings.Index(bashOut.Output[startPath:], ". Use view or grep")
	if endIdx == -1 {
		t.Fatalf("did not find end of path in notice: %s", bashOut.Output)
	}
	spillPath := bashOut.Output[startPath : startPath+endIdx]

	spillData, err := os.ReadFile(spillPath)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", spillPath, err)
	}

	// Full output on disk must contain lines from both stdout and stderr
	spillStr := string(spillData)
	if !strings.Contains(spillStr, "stdout line 1\n") || !strings.Contains(spillStr, "stderr line 1\n") {
		t.Fatalf("spilled content missing initial stdout/stderr lines")
	}
	if !strings.Contains(spillStr, "stdout line 300\n") || !strings.Contains(spillStr, "stderr line 300\n") {
		t.Fatalf("spilled content missing final stdout/stderr lines")
	}
}

func TestWebFetchStreamingSpill(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		for i := 1; i <= 600; i++ {
			fmt.Fprintf(w, "web line %03d\n", i)
		}
	}))
	defer ts.Close()

	_, c, _, _, _ := setupBuiltinEnv(t, true)
	ctx := scope.With(context.Background(), scope.Scope{
		SessionID:  "sess-fetch-spill",
		ToolCallID: "call-fetch-spill",
	})

	res, err := c.Execute(ctx, &message.ToolCall{
		ID:   "call-fetch-spill",
		Name: builtin.ToolWebFetch,
		Args: map[string]any{
			"url": ts.URL + "/large",
		},
	})
	if err != nil || res.IsError {
		t.Fatalf("Execute(web_fetch spill) failed: err=%v, res=%+v", err, res)
	}

	fetchOut, ok := res.StructuredContent.(api.WebFetchOutput)
	if !ok {
		t.Fatalf("expected api.WebFetchOutput, got: %T", res.StructuredContent)
	}
	if fetchOut.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", fetchOut.StatusCode)
	}

	// Verify truncation preview in Text
	if !strings.Contains(fetchOut.Text, "truncated — full output saved to") {
		t.Fatalf("expected truncation notice in web_fetch text, got:\n%s", fetchOut.Text)
	}

	// Extract spilled file path from notice
	startIdx := strings.Index(fetchOut.Text, "full output saved to ")
	if startIdx == -1 {
		t.Fatalf("did not find 'full output saved to ' in notice: %s", fetchOut.Text)
	}
	startPath := startIdx + len("full output saved to ")
	endIdx := strings.Index(fetchOut.Text[startPath:], ". Use view or grep")
	if endIdx == -1 {
		t.Fatalf("did not find end of path in notice: %s", fetchOut.Text)
	}
	spillPath := fetchOut.Text[startPath : startPath+endIdx]

	spillData, err := os.ReadFile(spillPath)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", spillPath, err)
	}

	spillStr := string(spillData)
	if !strings.Contains(spillStr, "web line 001\n") || !strings.Contains(spillStr, "web line 600\n") {
		t.Fatalf("spilled content missing initial or final lines")
	}
}

func TestSessionOutputsAndOutsidePathOperations(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	if realDir, err := filepath.EvalSymlinks(dir); err == nil {
		dir = realDir
	}
	sessionID := "sess-output-test"
	baseSpillDir := t.TempDir()
	if realBase, err := filepath.EvalSymlinks(baseSpillDir); err == nil {
		baseSpillDir = realBase
	}
	outputsDir := filepath.Join(baseSpillDir, "sessions", sessionID, "outputs")
	if err := os.MkdirAll(outputsDir, 0o755); err != nil {
		t.Fatalf("mkdir outputsDir: %v", err)
	}

	ws, err := workspace.Open(ctx, dir, workspace.WithBuiltinFS(builtin.FS()))
	if err != nil {
		t.Fatalf("workspace.Open failed: %v", err)
	}

	sb, err := sandbox.New(sandbox.Config{
		Mode:    sandbox.ModeLocal,
		WorkDir: dir,
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
		ReadDirs:     []string{outputsDir},
	})

	builtinTools, err := builtin.New(builtin.Config{
		Std:         stdLib,
		Sandbox:     sb,
		Guard:       guard,
		Workspace:   ws,
		RenderSkill: func(name string, args map[string]string) (string, error) { return ws.RenderSkill(name, args, nil) },
	})
	if err != nil {
		t.Fatalf("builtin.New failed: %v", err)
	}

	sp, err := tools.NewSpiller(tools.SpillerConfig{
		BaseDir: baseSpillDir,
	})
	if err != nil {
		t.Fatalf("tools.NewSpiller failed: %v", err)
	}

	c, err := tools.NewContainer(tools.Config{
		Tools:     builtinTools,
		Sandbox:   sb,
		Workspace: ws,
		Logger:    log.Default(),
		Spiller:   sp,
	})
	if err != nil {
		t.Fatalf("tools.NewContainer failed: %v", err)
	}

	// 1. Bash command spills > 500 lines to <outputsDir>/<tool_call_id>.txt
	toolCallID := "call-spill-test"
	scopedCtx := scope.With(ctx, scope.Scope{
		SessionID:  sessionID,
		ToolCallID: toolCallID,
	})

	bashCmd := `for i in $(seq 1 600); do echo "spilled line $i"; done`
	res, err := c.Execute(scopedCtx, &message.ToolCall{
		ID:   toolCallID,
		Name: builtin.ToolBash,
		Args: map[string]any{
			"command": bashCmd,
		},
	})
	if err != nil || res.IsError {
		t.Fatalf("Execute(bash spill) failed: err=%v, res=%+v", err, res)
	}

	expectedSpillPath := filepath.Join(outputsDir, toolCallID+".txt")
	if _, err := os.Stat(expectedSpillPath); err != nil {
		t.Fatalf("expected spilled file at %s: %v", expectedSpillPath, err)
	}

	// 2. View on spilled file path: Prepare (SafetySafe, ReadOnly: true) and Execute
	viewCall := &message.ToolCall{
		ID:   "call-view-spill",
		Name: builtin.ToolView,
		Args: map[string]any{
			"path": expectedSpillPath,
		},
	}
	viewPlan, err := c.Prepare(ctx, viewCall)
	if err != nil {
		t.Fatalf("Prepare(view spill) failed: %v", err)
	}
	if viewPlan.Safety != tools.SafetySafe || !viewPlan.ReadOnly || viewPlan.Action != tools.ActionRead {
		t.Fatalf("unexpected view plan on spilled path: %+v", viewPlan)
	}
	viewRes, err := c.Execute(ctx, viewCall)
	if err != nil || viewRes.IsError {
		t.Fatalf("Execute(view spill) failed: err=%v, res=%+v", err, viewRes)
	}
	viewOut, ok := viewRes.StructuredContent.(api.ViewOutput)
	if !ok {
		t.Fatalf("expected api.ViewOutput, got %T", viewRes.StructuredContent)
	}
	if !strings.Contains(viewOut.Content, "spilled line 1") || !strings.Contains(viewOut.Content, "spilled line 600") {
		t.Fatalf("view output missing expected spilled lines")
	}

	// 3. Grep on spilled file path: Prepare (SafetySafe, ReadOnly: true) and Execute
	grepCall := &message.ToolCall{
		ID:   "call-grep-spill",
		Name: builtin.ToolGrep,
		Args: map[string]any{
			"pattern": "spilled line 345",
			"path":    expectedSpillPath,
		},
	}
	grepPlan, err := c.Prepare(ctx, grepCall)
	if err != nil {
		t.Fatalf("Prepare(grep spill) failed: %v", err)
	}
	if grepPlan.Safety != tools.SafetySafe || !grepPlan.ReadOnly || grepPlan.Action != tools.ActionRead {
		t.Fatalf("unexpected grep plan on spilled path: %+v", grepPlan)
	}
	grepRes, err := c.Execute(ctx, grepCall)
	if err != nil || grepRes.IsError {
		t.Fatalf("Execute(grep spill) failed: err=%v, res=%+v", err, grepRes)
	}
	grepOut, ok := grepRes.StructuredContent.(api.GrepOutput)
	if !ok {
		t.Fatalf("expected api.GrepOutput, got %T", grepRes.StructuredContent)
	}
	if len(grepOut.Matches) == 0 || !strings.Contains(grepOut.Matches[0].Text, "spilled line 345") {
		t.Fatalf("grep output did not find expected match: %+v", grepOut)
	}

	// 4. Outside path operations (../outside.txt)
	outsideFilePath := filepath.Join(dir, "..", "outside.txt")
	t.Cleanup(func() {
		_ = os.Remove(outsideFilePath)
	})

	writeOutsideCall := &message.ToolCall{
		ID:   "call-write-outside",
		Name: builtin.ToolWrite,
		Args: map[string]any{
			"path":    "../outside.txt",
			"content": "outside data\nline two\n",
		},
	}
	writeOutsidePlan, err := c.Prepare(ctx, writeOutsideCall)
	if err != nil {
		t.Fatalf("Prepare(write outside) failed: %v", err)
	}
	if writeOutsidePlan.Safety != tools.SafetyUnsafe || writeOutsidePlan.Action != tools.ActionWrite {
		t.Fatalf("expected SafetyUnsafe for write ../outside.txt, got %+v", writeOutsidePlan)
	}
	writeOutsideRes, err := c.Execute(ctx, writeOutsideCall)
	if err != nil || writeOutsideRes.IsError {
		t.Fatalf("Execute(write outside) failed: err=%v, res=%+v", err, writeOutsideRes)
	}

	viewOutsideCall := &message.ToolCall{
		ID:   "call-view-outside",
		Name: builtin.ToolView,
		Args: map[string]any{
			"path": "../outside.txt",
		},
	}
	viewOutsidePlan, err := c.Prepare(ctx, viewOutsideCall)
	if err != nil {
		t.Fatalf("Prepare(view outside) failed: %v", err)
	}
	if viewOutsidePlan.Safety != tools.SafetyOutside || !viewOutsidePlan.ReadOnly || viewOutsidePlan.Action != tools.ActionRead {
		t.Fatalf("expected SafetyOutside + ReadOnly for view ../outside.txt, got %+v", viewOutsidePlan)
	}
	viewOutsideRes, err := c.Execute(ctx, viewOutsideCall)
	if err != nil || viewOutsideRes.IsError {
		t.Fatalf("Execute(view outside) failed: err=%v, res=%+v", err, viewOutsideRes)
	}
	viewOutsideOut, ok := viewOutsideRes.StructuredContent.(api.ViewOutput)
	if !ok {
		t.Fatalf("expected api.ViewOutput, got %T", viewOutsideRes.StructuredContent)
	}
	if !strings.Contains(viewOutsideOut.Content, "outside data") {
		t.Fatalf("expected 'outside data' in view output, got %q", viewOutsideOut.Content)
	}
}

func TestBashBackgroundAndTasksManagement(t *testing.T) {
	ctx := context.Background()
	_, container, _, _, _ := setupBuiltinEnv(t, false)

	// 1. Launch a background bash command with run_in_background: true
	bgCall := &message.ToolCall{
		ID:   "call-bash-bg",
		Name: builtin.ToolBash,
		Args: map[string]any{
			"command":           "echo 'server started' && read line && echo \"got: $line\"",
			"run_in_background": true,
		},
	}

	plan, err := container.Prepare(ctx, bgCall)
	if err != nil {
		t.Fatalf("Prepare(bash bg) failed: %v", err)
	}
	if plan.ToolName != builtin.ToolBash {
		t.Fatalf("unexpected plan ToolName: %q", plan.ToolName)
	}

	res, err := container.Execute(ctx, bgCall)
	if err != nil || res.IsError {
		t.Fatalf("Execute(bash bg) failed: err=%v, res=%+v", err, res)
	}

	out, ok := res.StructuredContent.(api.BashOutput)
	if !ok {
		t.Fatalf("expected api.BashOutput, got %T", res.StructuredContent)
	}
	if out.TaskID == "" {
		t.Fatal("expected non-empty TaskID for background bash command")
	}
	if out.Status != "running" {
		t.Fatalf("expected status 'running', got %q", out.Status)
	}
	if !strings.Contains(out.Output, "<background_task") {
		t.Fatalf("expected output to contain <background_task notice, got %q", out.Output)
	}

	taskID := out.TaskID

	// 2. tasks list
	listCall := &message.ToolCall{
		ID:   "call-tasks-list",
		Name: builtin.ToolTasks,
		Args: map[string]any{
			"action": "list",
		},
	}
	listPlan, err := container.Prepare(ctx, listCall)
	if err != nil {
		t.Fatalf("Prepare(tasks list) failed: %v", err)
	}
	if !listPlan.ReadOnly || listPlan.Action != tools.ActionRead {
		t.Fatalf("expected ReadOnly ActionRead for list, got %+v", listPlan)
	}

	listRes, err := container.Execute(ctx, listCall)
	if err != nil || listRes.IsError {
		t.Fatalf("Execute(tasks list) failed: err=%v, res=%+v", err, listRes)
	}
	listOut, ok := listRes.StructuredContent.(api.TasksOutput)
	if !ok {
		t.Fatalf("expected api.TasksOutput, got %T", listRes.StructuredContent)
	}
	if len(listOut.Tasks) != 1 || listOut.Tasks[0].ID != taskID {
		t.Fatalf("expected 1 task with ID %q, got %+v", taskID, listOut.Tasks)
	}

	// 3. tasks status
	statusCall := &message.ToolCall{
		ID:   "call-tasks-status",
		Name: builtin.ToolTasks,
		Args: map[string]any{
			"action":  "status",
			"task_id": taskID,
		},
	}
	statusRes, err := container.Execute(ctx, statusCall)
	if err != nil || statusRes.IsError {
		t.Fatalf("Execute(tasks status) failed: err=%v, res=%+v", err, statusRes)
	}
	statusOut, ok := statusRes.StructuredContent.(api.TasksOutput)
	if !ok {
		t.Fatalf("expected api.TasksOutput, got %T", statusRes.StructuredContent)
	}
	if statusOut.Status != "running" {
		t.Fatalf("expected task status 'running', got %q", statusOut.Status)
	}

	// 4. tasks send_input
	inputCall := &message.ToolCall{
		ID:   "call-tasks-input",
		Name: builtin.ToolTasks,
		Args: map[string]any{
			"action":  "send_input",
			"task_id": taskID,
			"input":   "hello from test\n",
		},
	}
	inputRes, err := container.Execute(ctx, inputCall)
	if err != nil || inputRes.IsError {
		t.Fatalf("Execute(tasks send_input) failed: err=%v, res=%+v", err, inputRes)
	}

	// Wait briefly for command to read stdin and finish
	time.Sleep(100 * time.Millisecond)

	// 5. Verify status again after completion
	statusRes2, err := container.Execute(ctx, statusCall)
	if err != nil || statusRes2.IsError {
		t.Fatalf("Execute(tasks status 2) failed: err=%v, res=%+v", err, statusRes2)
	}
	statusOut2 := statusRes2.StructuredContent.(api.TasksOutput)
	if statusOut2.Status != "completed" {
		t.Fatalf("expected task status 'completed', got %q", statusOut2.Status)
	}
	if !strings.Contains(statusOut2.Output, "got: hello from test") {
		t.Fatalf("expected task output to contain 'got: hello from test', got %q", statusOut2.Output)
	}

	// 6. Test kill on a long-running background command
	killBashCall := &message.ToolCall{
		ID:   "call-bash-hang",
		Name: builtin.ToolBash,
		Args: map[string]any{
			"command":           "sleep 60",
			"run_in_background": true,
		},
	}
	killBashRes, err := container.Execute(ctx, killBashCall)
	if err != nil || killBashRes.IsError {
		t.Fatalf("Execute(bash sleep) failed: err=%v, res=%+v", err, killBashRes)
	}
	hangTaskID := killBashRes.StructuredContent.(api.BashOutput).TaskID

	killCall := &message.ToolCall{
		ID:   "call-tasks-kill",
		Name: builtin.ToolTasks,
		Args: map[string]any{
			"action":  "kill",
			"task_id": hangTaskID,
		},
	}
	killRes, err := container.Execute(ctx, killCall)
	if err != nil || killRes.IsError {
		t.Fatalf("Execute(tasks kill) failed: err=%v, res=%+v", err, killRes)
	}

	time.Sleep(50 * time.Millisecond)

	// Verify status is canceled
	statusHangCall := &message.ToolCall{
		ID:   "call-tasks-status-hang",
		Name: builtin.ToolTasks,
		Args: map[string]any{
			"action":  "status",
			"task_id": hangTaskID,
		},
	}
	statusHangRes, err := container.Execute(ctx, statusHangCall)
	if err != nil || statusHangRes.IsError {
		t.Fatalf("Execute(tasks status hang) failed: err=%v, res=%+v", err, statusHangRes)
	}
	statusHangOut := statusHangRes.StructuredContent.(api.TasksOutput)
	if statusHangOut.Status != "canceled" {
		t.Fatalf("expected status 'canceled', got %q", statusHangOut.Status)
	}
}

func TestTodosTool(t *testing.T) {
	ctx := context.Background()
	_, container, _, _, _ := setupBuiltinEnv(t, false)

	// 1. Prepare valid todos
	call := &message.ToolCall{
		ID:   "call-todos-1",
		Name: builtin.ToolTodos,
		Args: map[string]any{
			"todos": []any{
				map[string]any{
					"content":     "First task",
					"active_text": "Doing first task",
					"status":      "completed",
				},
				map[string]any{
					"content":     "Second task",
					"active_text": "Working on second task",
					"status":      "in_progress",
				},
				map[string]any{
					"content":     "Third task",
					"active_text": "Queued third task",
					"status":      "pending",
				},
			},
		},
	}

	plan, err := container.Prepare(ctx, call)
	if err != nil {
		t.Fatalf("Prepare(todos) failed: %v", err)
	}
	if !plan.ReadOnly || plan.Action != tools.ActionRead || plan.Safety != tools.SafetySafe {
		t.Fatalf("unexpected plan: %+v", plan)
	}

	// 2. Execute valid todos
	res, err := container.Execute(ctx, call)
	if err != nil || res.IsError {
		t.Fatalf("Execute(todos) failed: err=%v, res=%+v", err, res)
	}
	out, ok := res.StructuredContent.(api.TodosOutput)
	if !ok {
		t.Fatalf("expected api.TodosOutput, got %T", res.StructuredContent)
	}
	if out.Total != 3 || out.Completed != 1 || out.InProgress != 1 || out.Pending != 1 {
		t.Fatalf("unexpected counts: total=%d, completed=%d, in_progress=%d, pending=%d",
			out.Total, out.Completed, out.InProgress, out.Pending)
	}
	if len(out.Todos) != 3 {
		t.Fatalf("expected 3 todos, got %d", len(out.Todos))
	}
	if out.Todos[1].ActiveText != "Working on second task" {
		t.Fatalf("expected active_text 'Working on second task', got %q", out.Todos[1].ActiveText)
	}

	// 3. Validation: missing content
	invalidCall := &message.ToolCall{
		ID:   "call-todos-invalid",
		Name: builtin.ToolTodos,
		Args: map[string]any{
			"todos": []any{
				map[string]any{
					"content": "",
					"status":  "pending",
				},
			},
		},
	}
	if _, err := container.Prepare(ctx, invalidCall); err == nil {
		t.Fatal("expected Prepare to fail on empty content")
	}

	// 4. Validation: invalid status
	invalidStatusCall := &message.ToolCall{
		ID:   "call-todos-invalid-status",
		Name: builtin.ToolTodos,
		Args: map[string]any{
			"todos": []any{
				map[string]any{
					"content": "Valid task",
					"status":  "not_a_valid_status",
				},
			},
		},
	}
	if _, err := container.Prepare(ctx, invalidStatusCall); err == nil {
		t.Fatal("expected Prepare to fail on invalid status")
	}
}

func TestBuiltinCustomizationSkillAndReference(t *testing.T) {
	ctx := context.Background()
	dir, _, stdLib, sb, ws := setupBuiltinEnv(t, false)
	builtinDir, err := xdg.BuiltinDir()
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := ws.ResolveAgent("main")
	if err != nil {
		t.Fatal(err)
	}
	guard := shellguard.New(shellguard.Config{WorkspaceDir: dir, ReadDirs: []string{builtinDir}})
	builtins, err := builtin.New(builtin.Config{Std: stdLib, Sandbox: sb, Guard: guard, Workspace: ws, Skills: resolved.Skills, RenderSkill: func(name string, args map[string]string) (string, error) { return ws.RenderSkill(name, args, nil) }})
	if err != nil {
		t.Fatal(err)
	}
	container, err := tools.NewContainer(tools.Config{Tools: builtins, Sandbox: sb, Workspace: ws, Logger: log.Default()})
	if err != nil {
		t.Fatal(err)
	}
	load := &message.ToolCall{ID: "load", Name: builtin.ToolLoadSkill, Args: map[string]any{"skill": "tasksmith-customizations"}}
	result, err := container.Execute(ctx, load)
	if err != nil || result.IsError || !strings.Contains(result.Content.Text(), "references/rules.md") || !strings.Contains(result.Content.Text(), "tasksmith-customizations") {
		t.Fatalf("load skill: %v %+v", err, result)
	}
	skill, ok := ws.GetSkill("tasksmith-customizations")
	if !ok {
		t.Fatal("skill not found")
	}
	ref := filepath.Join(skill.Directory, "references", "rules.md")
	view := &message.ToolCall{ID: "view", Name: builtin.ToolView, Args: map[string]any{"path": ref}}
	plan, err := container.Prepare(ctx, view)
	if err != nil || plan.Safety != tools.SafetySafe {
		t.Fatalf("view plan: %+v %v", plan, err)
	}
	result, err = container.Execute(ctx, view)
	if err != nil || result.IsError {
		t.Fatalf("view reference: %v %+v", err, result)
	}
	output, ok := result.StructuredContent.(api.ViewOutput)
	if !ok || !strings.Contains(output.Content, "# Rules") {
		t.Fatalf("unexpected view output: %+v", result.StructuredContent)
	}
	write := &message.ToolCall{ID: "write", Name: builtin.ToolWrite, Args: map[string]any{"path": ref, "content": "modified"}}
	plan, err = container.Prepare(ctx, write)
	if err != nil || plan.Safety != tools.SafetyUnsafe {
		t.Fatalf("write plan: %+v %v", plan, err)
	}
}

func TestLoadSkillTool(t *testing.T) {
	ctx := context.Background()
	dir, _, stdLib, sb, ws := setupBuiltinEnv(t, false)

	// Create a skill manifest in the workspace
	skillsDir := filepath.Join(dir, ".agents", "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	skillContent := `---
apiVersion: warp/v1alpha1
kind: Skill
metadata:
  name: docker-deploy
  description: Docker deployment guidelines
spec:
  arguments:
    - name: tag
      default: latest
    - name: image
      required: true
---
Deploying image {{.image}}:{{.tag}}
`
	if err := os.WriteFile(filepath.Join(skillsDir, "docker-deploy.skill.md"), []byte(skillContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Reload workspace to pick up new skill
	if err := ws.Reload(ctx); err != nil {
		t.Fatalf("ws.Reload: %v", err)
	}

	resolvedMain, err := ws.ResolveAgent("main")
	if err != nil {
		t.Fatal(err)
	}
	guard := shellguard.New(shellguard.Config{WorkspaceDir: dir})
	mainTools, err := builtin.New(builtin.Config{
		Std:         stdLib,
		Sandbox:     sb,
		Guard:       guard,
		Workspace:   ws,
		Skills:      resolvedMain.Skills,
		RenderSkill: func(name string, args map[string]string) (string, error) { return ws.RenderSkill(name, args, nil) },
	})
	if err != nil {
		t.Fatal(err)
	}
	agentContainer, err := tools.NewContainer(tools.Config{
		Tools:     mainTools,
		Sandbox:   sb,
		Workspace: ws,
		Logger:    log.Default(),
	})
	if err != nil {
		t.Fatal(err)
	}

	// 1. Success execution with required arg and defaulted arg
	call := &message.ToolCall{
		ID:   "call-load-skill-1",
		Name: builtin.ToolLoadSkill,
		Args: map[string]any{
			"skill": "docker-deploy",
			"arguments": map[string]any{
				"image": "my-service",
			},
		},
	}
	res, err := agentContainer.Execute(ctx, call)
	if err != nil {
		t.Fatalf("Execute load_skill: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", res.Content.Text())
	}
	if !strings.Contains(res.Content.Text(), "Deploying image my-service:latest") {
		t.Errorf("expected rendered skill with default tag, got: %q", res.Content.Text())
	}
	if !strings.Contains(res.Content.Text(), "Base directory: .agents/skills") {
		t.Errorf("expected base directory in output, got: %q", res.Content.Text())
	}

	// 2. Missing required argument
	callMissingArg := &message.ToolCall{
		ID:   "call-load-skill-missing",
		Name: builtin.ToolLoadSkill,
		Args: map[string]any{
			"skill": "docker-deploy",
		},
	}
	resMissing, err := agentContainer.Execute(ctx, callMissingArg)
	if err != nil {
		t.Fatalf("Execute load_skill missing: %v", err)
	}
	if !resMissing.IsError || !strings.Contains(resMissing.Content.Text(), "required argument") {
		t.Errorf("expected error mentioning required argument, got IsError=%v content=%q", resMissing.IsError, resMissing.Content.Text())
	}

	// 3. Non-existent skill
	callNotFound := &message.ToolCall{
		ID:   "call-load-skill-notfound",
		Name: builtin.ToolLoadSkill,
		Args: map[string]any{
			"skill": "non-existent-skill",
		},
	}
	resNotFound, err := agentContainer.Execute(ctx, callNotFound)
	if err != nil {
		t.Fatalf("Execute load_skill not found: %v", err)
	}
	if !resNotFound.IsError || (!strings.Contains(resNotFound.Content.Text(), "not found") && !strings.Contains(resNotFound.Content.Text(), "not accessible")) {
		t.Errorf("expected error mentioning not found, got IsError=%v content=%q", resNotFound.IsError, resNotFound.Content.Text())
	}

	// 4. Agent scoping: restricted agent cannot access skill outside its resolved skills
	otherSkill := `---
apiVersion: warp/v1alpha1
kind: Skill
metadata:
  name: other-skill
---
Other skill instructions.
`
	if err := os.WriteFile(filepath.Join(skillsDir, "other.skill.md"), []byte(otherSkill), 0o644); err != nil {
		t.Fatal(err)
	}

	agentsDir := filepath.Join(dir, ".agents")
	agentContent := `---
apiVersion: warp/v1alpha1
kind: Agent
metadata:
  name: restricted-agent
spec:
  skills:
    - other-skill
---
Restricted agent instructions.
`
	if err := os.WriteFile(filepath.Join(agentsDir, "restricted.agent.md"), []byte(agentContent), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ws.Reload(ctx); err != nil {
		t.Fatalf("ws.Reload: %v", err)
	}

	// Resolve restricted agent and construct tools using its resolved skills
	resolved, err := ws.ResolveAgent("restricted-agent")
	if err != nil {
		t.Fatal(err)
	}

	skills := resolved.Skills
	if skills == nil {
		skills = []warp.Skill{}
	}

	guard = shellguard.New(shellguard.Config{WorkspaceDir: dir})
	scopedTools, err := builtin.New(builtin.Config{
		Std:         stdLib,
		Sandbox:     sb,
		Guard:       guard,
		Workspace:   ws,
		Skills:      skills,
		RenderSkill: func(name string, args map[string]string) (string, error) { return ws.RenderSkill(name, args, nil) },
	})
	if err != nil {
		t.Fatal(err)
	}
	scopedContainer, err := tools.NewContainer(tools.Config{
		Tools:     scopedTools,
		Sandbox:   sb,
		Workspace: ws,
		Logger:    log.Default(),
	})
	if err != nil {
		t.Fatal(err)
	}

	callDisallowed := &message.ToolCall{
		ID:   "call-disallowed",
		Name: builtin.ToolLoadSkill,
		Args: map[string]any{
			"skill": "docker-deploy",
			"arguments": map[string]any{
				"image": "my-service",
			},
		},
	}
	resDisallowed, err := scopedContainer.Execute(ctx, callDisallowed)
	if err != nil {
		t.Fatalf("Execute load_skill disallowed: %v", err)
	}
	t.Logf("resDisallowed: IsError=%v, Content=%q", resDisallowed.IsError, resDisallowed.Content.Text())
	if !resDisallowed.IsError || !strings.Contains(resDisallowed.Content.Text(), "not accessible to this agent") {
		t.Errorf("expected skill to be rejected for restricted-agent, got: %q", resDisallowed.Content.Text())
	}
}
