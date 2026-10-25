package permissions_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/daemon/permissions"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
)

func setupTestWorkspace(t *testing.T) (string, workspace.Workspace) {
	t.Helper()
	dir := t.TempDir()
	if realDir, err := filepath.EvalSymlinks(dir); err == nil {
		dir = realDir
	}
	ws, err := workspace.Open(context.Background(), dir)
	if err != nil {
		t.Fatalf("workspace.Open: %v", err)
	}
	return dir, ws
}

func TestParseAndFormatRule(t *testing.T) {
	valid := []struct {
		raw         string
		wantTool    string
		wantPattern string
	}{
		{"bash(go *)", "bash", "go *"},
		{"edit(internal/**)", "edit", "internal/**"},
		{"bash(*)", "bash", "*"},
		{"*(*)", "*", "*"},
		{"  view(readme.md)  ", "view", "readme.md"},
		{"tool( a/b/c )", "tool", "a/b/c"},
	}

	for _, tc := range valid {
		toolName, pattern, err := permissions.ParseRule(tc.raw)
		if err != nil {
			t.Fatalf("ParseRule(%q) failed: %v", tc.raw, err)
		}
		if toolName != tc.wantTool || pattern != tc.wantPattern {
			t.Fatalf("ParseRule(%q): got (%q, %q), want (%q, %q)", tc.raw, toolName, pattern, tc.wantTool, tc.wantPattern)
		}
	}

	invalid := []string{
		"bash",
		"bash()",
		"(go *)",
		"()",
		"  ",
		"bash(go *",
		"bash go *)",
		"tool()",
		"bash (go *)",
		"foo(bar)(baz)",
	}

	for _, raw := range invalid {
		_, _, err := permissions.ParseRule(raw)
		if !errors.Is(err, permissions.ErrInvalidRule) {
			t.Fatalf("ParseRule(%q): expected ErrInvalidRule, got %v", raw, err)
		}
	}

	if f := permissions.FormatRule("bash", "go *"); f != "bash(go *)" {
		t.Fatalf("FormatRule: got %q, want bash(go *)", f)
	}
	if f := permissions.FormatRule("bash", ""); f != "bash(*)" {
		t.Fatalf("FormatRule with empty pattern: got %q, want bash(*)", f)
	}
	if f := permissions.FormatRule(" bash ", "   "); f != "bash(*)" {
		t.Fatalf("FormatRule with whitespace: got %q, want bash(*)", f)
	}
}

func TestDeriveGrantRules(t *testing.T) {
	// Empty target
	rules := permissions.DeriveGrantRules("tool", tools.ActionExec, "")
	if !slices.Equal(rules, []string{"tool(*)"}) {
		t.Fatalf("empty target: got %v", rules)
	}

	// ActionRead / Write / Delete
	rwRules := permissions.DeriveGrantRules("write_file", tools.ActionWrite, "internal/pkg/file.go")
	if !slices.Equal(rwRules, []string{"write_file(internal/pkg/file.go)", "write_file(internal/pkg/**)"}) {
		t.Fatalf("read/write rules: got %v", rwRules)
	}

	// ActionExec with subcommands
	execRules := permissions.DeriveGrantRules("bash", tools.ActionExec, "go test ./...")
	if !slices.Equal(execRules, []string{"bash(go test *)", "bash(go *)"}) {
		t.Fatalf("exec rules with subcommand: got %v", execRules)
	}

	// ActionExec with flags or single binary
	execSingle := permissions.DeriveGrantRules("bash", tools.ActionExec, "ls -la")
	if !slices.Equal(execSingle, []string{"bash(ls *)"}) {
		t.Fatalf("exec rules with flag: got %v", execSingle)
	}

	// ActionNet with host
	netRules := permissions.DeriveGrantRules("web_fetch", tools.ActionNet, "https://api.github.com/v1")
	if !slices.Equal(netRules, []string{"web_fetch(api.github.com)", "web_fetch(https://api.github.com/v1)"}) {
		t.Fatalf("net rules with host: got %v", netRules)
	}

	// ActionNet without host
	netNoHost := permissions.DeriveGrantRules("web_fetch", tools.ActionNet, "invalid-url")
	if !slices.Equal(netNoHost, []string{"web_fetch(invalid-url)"}) {
		t.Fatalf("net rules without host: got %v", netNoHost)
	}
}

func TestMatchRule(t *testing.T) {
	cases := []struct {
		rule  string
		plan  *tools.Plan
		match bool
	}{
		{"*(*)", &tools.Plan{ToolName: "bash", Target: "anything"}, true},
		{"bash(*)", &tools.Plan{ToolName: "bash", Target: "ls -la"}, true},
		{"bash(*)", &tools.Plan{ToolName: "view", Target: "ls -la"}, false},
		{"bash(go *)", &tools.Plan{ToolName: "bash", Target: "go test ./..."}, true},
		{"bash(go *)", &tools.Plan{ToolName: "bash", Target: "cargo test"}, false},
		{"edit(internal/**)", &tools.Plan{ToolName: "edit", Target: "internal/pkg/a.go"}, true},
		{"edit(internal/**)", &tools.Plan{ToolName: "edit", Target: "cmd/main.go"}, false},
		{"view(*.example.com)", &tools.Plan{ToolName: "view", Target: "https://api.example.com/v1"}, true},
		{"view(*.example.com)", &tools.Plan{ToolName: "view", Target: "https://other.com/v1"}, false},
		{"write(cmd/**/main_?.go)", &tools.Plan{ToolName: "write", Target: "cmd/cli/main_1.go"}, true},
		{"write(cmd/**/main_?.go)", &tools.Plan{ToolName: "write", Target: "cmd/cli/main_12.go"}, false},
		{"invalid_rule", &tools.Plan{ToolName: "bash", Target: "ls"}, false},
		{"bash(*)", nil, false},
	}

	for _, tc := range cases {
		got := permissions.MatchRule(tc.rule, tc.plan)
		if got != tc.match {
			t.Errorf("MatchRule(%q, %+v): got %v, want %v", tc.rule, tc.plan, got, tc.match)
		}
	}
}

func TestManagerGlobalAndProjectPersistence(t *testing.T) {
	_, ws := setupTestWorkspace(t)
	globalPath := filepath.Join(t.TempDir(), "global_permissions.json")

	mgr, err := permissions.New(permissions.Config{
		Workspace:  ws,
		GlobalPath: globalPath,
	})
	if err != nil {
		t.Fatalf("permissions.New: %v", err)
	}

	// Initial reading when file does not exist returns empty Permissions
	glob, err := mgr.Get(permissions.ScopeGlobal)
	if err != nil {
		t.Fatalf("mgr.Get(ScopeGlobal): %v", err)
	}
	if len(glob.Allow) != 0 || len(glob.Deny) != 0 {
		t.Fatalf("expected empty global perms, got %+v", glob)
	}

	proj, err := mgr.Get(permissions.ScopeProject)
	if err != nil {
		t.Fatalf("mgr.Get(ScopeProject): %v", err)
	}
	if len(proj.Allow) != 0 || len(proj.Deny) != 0 {
		t.Fatalf("expected empty project perms, got %+v", proj)
	}

	// Invalid scope
	if _, err := mgr.Get(permissions.ScopeSession); !errors.Is(err, permissions.ErrInvalidScope) {
		t.Fatalf("expected ErrInvalidScope for ScopeSession on Manager, got %v", err)
	}
	if err := mgr.Set(permissions.ScopeSession, permissions.Permissions{}); !errors.Is(err, permissions.ErrInvalidScope) {
		t.Fatalf("expected ErrInvalidScope for Set ScopeSession on Manager, got %v", err)
	}

	// AddRule and RemoveRule with deduplication
	if err := mgr.AddRule(permissions.ScopeGlobal, permissions.EffectAllow, "bash(go *)"); err != nil {
		t.Fatalf("AddRule: %v", err)
	}
	// duplicate ignored
	if err := mgr.AddRule(permissions.ScopeGlobal, permissions.EffectAllow, "bash(go *)"); err != nil {
		t.Fatalf("AddRule duplicate: %v", err)
	}
	if err := mgr.AddRule(permissions.ScopeGlobal, permissions.EffectDeny, "bash(rm -rf *)"); err != nil {
		t.Fatalf("AddRule deny: %v", err)
	}

	glob, err = mgr.Get(permissions.ScopeGlobal)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(glob.Allow) != 1 || glob.Allow[0] != "bash(go *)" {
		t.Fatalf("unexpected global Allow: %v", glob.Allow)
	}
	if len(glob.Deny) != 1 || glob.Deny[0] != "bash(rm -rf *)" {
		t.Fatalf("unexpected global Deny: %v", glob.Deny)
	}

	// Verify file content is valid JSON on disk
	data, err := os.ReadFile(globalPath)
	if err != nil {
		t.Fatalf("ReadFile globalPath: %v", err)
	}
	if !strings.Contains(string(data), "bash(go *)") {
		t.Fatalf("expected file to contain rule, got %s", string(data))
	}

	// RemoveRule
	if err := mgr.RemoveRule(permissions.ScopeGlobal, permissions.EffectAllow, "bash(go *)"); err != nil {
		t.Fatalf("RemoveRule: %v", err)
	}
	glob, _ = mgr.Get(permissions.ScopeGlobal)
	if len(glob.Allow) != 0 {
		t.Fatalf("expected empty allow after RemoveRule, got %v", glob.Allow)
	}

	// Set with validation error
	if err := mgr.Set(permissions.ScopeProject, permissions.Permissions{
		Allow: []string{"invalid_bare_tool"},
	}); !errors.Is(err, permissions.ErrInvalidRule) {
		t.Fatalf("expected ErrInvalidRule on Set, got %v", err)
	}

	// Project path file persistence
	if err := mgr.Set(permissions.ScopeProject, permissions.Permissions{
		Allow: []string{"edit(internal/**)", "edit(internal/**)"},
		Deny:  []string{"bash(kill *)"},
	}); err != nil {
		t.Fatalf("Set project perms: %v", err)
	}
	proj, _ = mgr.Get(permissions.ScopeProject)
	if len(proj.Allow) != 1 || proj.Allow[0] != "edit(internal/**)" {
		t.Fatalf("expected deduplicated project allow rule, got %v", proj.Allow)
	}
}

func TestSessionManagerEvaluationModesAndPrecedence(t *testing.T) {
	_, ws := setupTestWorkspace(t)
	globalPath := filepath.Join(t.TempDir(), "global.json")

	mgr, err := permissions.New(permissions.Config{
		Workspace:  ws,
		GlobalPath: globalPath,
	})
	if err != nil {
		t.Fatalf("permissions.New: %v", err)
	}

	var updatedPerms permissions.Permissions
	updateCount := 0
	sess := mgr.Attach(tools.ModeDefault, permissions.Permissions{}, func(p permissions.Permissions) error {
		updatedPerms = p.Clone()
		updateCount++
		return nil
	})

	if sess.Mode() != tools.ModeDefault {
		t.Fatalf("expected ModeDefault, got %s", sess.Mode())
	}

	safeReadPlan := &tools.Plan{
		ToolName: "view",
		Action:   tools.ActionRead,
		Safety:   tools.SafetySafe,
		Target:   "README.md",
		ReadOnly: true,
	}
	outsideReadPlan := &tools.Plan{
		ToolName: "view",
		Action:   tools.ActionRead,
		Safety:   tools.SafetyOutside,
		Target:   "/outside/somefile.txt",
		ReadOnly: true,
	}
	unsafeReadPlan := &tools.Plan{
		ToolName: "view",
		Action:   tools.ActionRead,
		Safety:   tools.SafetyUnsafe,
		Target:   "/etc/hosts",
		ReadOnly: true,
	}
	writePlan := &tools.Plan{
		ToolName: "write_file",
		Action:   tools.ActionWrite,
		Safety:   tools.SafetySafe,
		Target:   "internal/pkg/a.go",
		Diff:     "+package pkg",
		ReadOnly: false,
	}
	unsafePlan := &tools.Plan{
		ToolName: "bash",
		Action:   tools.ActionDelete,
		Safety:   tools.SafetyUnsafe,
		Target:   ".git/config",
		ReadOnly: false,
	}

	// 1. ModeDefault
	if dec, req, err := sess.Evaluate(safeReadPlan); err != nil || dec != tools.AccessAllow || req != nil {
		t.Fatalf("ModeDefault safeRead: got %s, %+v, %v", dec, req, err)
	}
	outDec, outReq, err := sess.Evaluate(outsideReadPlan)
	if err != nil || outDec != tools.AccessAsk || outReq == nil {
		t.Fatalf("ModeDefault outsideRead: got %s, %+v, %v", outDec, outReq, err)
	}
	if u, uErr := uuid.Parse(outReq.ID); uErr != nil || u.Version() != 7 {
		t.Fatalf("expected UUIDv7 request ID, got %s", outReq.ID)
	}

	// ApplyDecision DecisionAllowSession
	if err := sess.ApplyDecision(outReq, tools.PermissionDecision{Kind: tools.DecisionAllowSession}); err != nil {
		t.Fatalf("ApplyDecision: %v", err)
	}
	if updateCount == 0 || len(updatedPerms.Allow) == 0 {
		t.Fatalf("expected onUpdate callback on ApplyDecision")
	}
	if dec, _, _ := sess.Evaluate(outsideReadPlan); dec != tools.AccessAllow {
		t.Fatalf("expected AccessAllow after session allow grant, got %s", dec)
	}

	// Wildcard grant doesn't bypass SafetyUnsafe in ModeDefault
	_ = sess.AddAllow("*(*)")
	if dec, req, _ := sess.Evaluate(unsafeReadPlan); dec != tools.AccessAsk || req == nil {
		t.Fatalf("unsafeReadPlan with wildcard grant: expected AccessAsk, got %s", dec)
	}
	_ = sess.Clear()

	// 2. ModeAcceptEdits
	sess.SetMode(tools.ModeAcceptEdits)
	if dec, _, _ := sess.Evaluate(writePlan); dec != tools.AccessAllow {
		t.Fatalf("ModeAcceptEdits writePlan: expected AccessAllow, got %s", dec)
	}
	if dec, _, _ := sess.Evaluate(unsafePlan); dec != tools.AccessAsk {
		t.Fatalf("ModeAcceptEdits unsafePlan: expected AccessAsk, got %s", dec)
	}

	// 3. ModePlan
	sess.SetMode(tools.ModePlan)
	if dec, _, _ := sess.Evaluate(writePlan); dec != tools.AccessDeny {
		t.Fatalf("ModePlan writePlan: expected AccessDeny, got %s", dec)
	}
	if dec, _, _ := sess.Evaluate(safeReadPlan); dec != tools.AccessAllow {
		t.Fatalf("ModePlan safeReadPlan: expected AccessAllow, got %s", dec)
	}

	// 4. ModeAsk
	sess.SetMode(tools.ModeAsk)
	if dec, _, _ := sess.Evaluate(safeReadPlan); dec != tools.AccessAsk {
		t.Fatalf("ModeAsk safeReadPlan: expected AccessAsk, got %s", dec)
	}
	_ = sess.AddAllow("view(README.md)")
	if dec, _, _ := sess.Evaluate(safeReadPlan); dec != tools.AccessAllow {
		t.Fatalf("ModeAsk with allow rule: expected AccessAllow, got %s", dec)
	}
	_ = sess.Clear()

	// 5. ModeAuto
	sess.SetMode(tools.ModeAuto)
	if dec, _, _ := sess.Evaluate(writePlan); dec != tools.AccessAllow {
		t.Fatalf("ModeAuto writePlan: expected AccessAllow, got %s", dec)
	}
	if dec, _, _ := sess.Evaluate(unsafePlan); dec != tools.AccessAllow {
		t.Fatalf("ModeAuto unsafePlan: expected AccessAllow, got %s", dec)
	}

	// 6. Deny precedence over Allow and ModeAuto!
	_ = sess.AddAllow("write_file(*)")
	_ = sess.AddDeny("write_file(internal/pkg/a.go)")
	// In ModeAuto, a matching Deny rule must result in AccessDeny!
	if dec, _, _ := sess.Evaluate(writePlan); dec != tools.AccessDeny {
		t.Fatalf("Deny rule did not take precedence over ModeAuto: got %s", dec)
	}

	// Project-level Deny rule also takes precedence over Session Allow in ModeAuto
	_ = sess.Clear()
	if err := mgr.AddRule(permissions.ScopeProject, permissions.EffectDeny, "write_file(internal/**)"); err != nil {
		t.Fatalf("AddRule ScopeProject Deny: %v", err)
	}
	_ = sess.AddAllow("write_file(*)")
	if dec, _, _ := sess.Evaluate(writePlan); dec != tools.AccessDeny {
		t.Fatalf("Project Deny rule did not take precedence: got %s", dec)
	}
	_ = mgr.RemoveRule(permissions.ScopeProject, permissions.EffectDeny, "write_file(internal/**)")

	// Global-level Deny rule also takes precedence
	if err := mgr.AddRule(permissions.ScopeGlobal, permissions.EffectDeny, "write_file(*)"); err != nil {
		t.Fatalf("AddRule ScopeGlobal Deny: %v", err)
	}
	if dec, _, _ := sess.Evaluate(writePlan); dec != tools.AccessDeny {
		t.Fatalf("Global Deny rule did not take precedence: got %s", dec)
	}
	_ = mgr.RemoveRule(permissions.ScopeGlobal, permissions.EffectDeny, "write_file(*)")

	// 7. ApplyDecision with Reject
	dummyReq := &tools.PermissionRequest{
		ToolName: "bash",
		Action:   tools.ActionExec,
		Target:   "rm -rf /",
	}
	if err := sess.ApplyDecision(dummyReq, tools.PermissionDecision{Kind: tools.DecisionReject, Feedback: "blocked"}); !errors.Is(err, tools.ErrPermissionDenied) || !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("ApplyDecision Reject with feedback failed: %v", err)
	}
	if err := sess.ApplyDecision(dummyReq, tools.PermissionDecision{Kind: tools.DecisionReject}); !errors.Is(err, tools.ErrPermissionDenied) {
		t.Fatalf("ApplyDecision Reject without feedback failed: %v", err)
	}
	if err := sess.ApplyDecision(dummyReq, tools.PermissionDecision{Kind: "unknown"}); !errors.Is(err, tools.ErrPermissionDenied) {
		t.Fatalf("ApplyDecision unknown kind failed: %v", err)
	}

	// 8. Workspace policies: Exclude, Include, AllowDangerous, AllowOpenWorld
	f := false
	sess.SetPolicies(&workspace.Policies{
		Tools: &workspace.ToolPolicies{
			Exclude:        []string{"excluded_*"},
			Include:        []string{"view", "write_file"},
			AllowDangerous: &f,
			AllowOpenWorld: &f,
		},
	})
	if dec, _, _ := sess.Evaluate(&tools.Plan{ToolName: "excluded_tool"}); dec != tools.AccessDeny {
		t.Fatalf("expected excluded tool to be denied, got %s", dec)
	}
	if dec, _, _ := sess.Evaluate(&tools.Plan{ToolName: "unlisted_tool"}); dec != tools.AccessDeny {
		t.Fatalf("expected unlisted tool to be denied, got %s", dec)
	}
	if dec, _, _ := sess.Evaluate(&tools.Plan{ToolName: "view", Safety: tools.SafetyUnsafe}); dec != tools.AccessDeny {
		t.Fatalf("expected dangerous plan to be denied when AllowDangerous=false, got %s", dec)
	}
	if dec, _, _ := sess.Evaluate(&tools.Plan{ToolName: "view", Action: tools.ActionNet}); dec != tools.AccessDeny {
		t.Fatalf("expected ActionNet plan to be denied when AllowOpenWorld=false, got %s", dec)
	}
}

func TestEvaluateMalformedPermissionsFile(t *testing.T) {
	dir, ws := setupTestWorkspace(t)
	globalPath := filepath.Join(t.TempDir(), "global.json")

	mgr, err := permissions.New(permissions.Config{
		Workspace:  ws,
		GlobalPath: globalPath,
	})
	if err != nil {
		t.Fatalf("permissions.New: %v", err)
	}
	sess := mgr.Attach(tools.ModeDefault, permissions.Permissions{}, nil)

	// Write corrupt JSON to project permissions file
	wsDataDir, err := xdg.WorkspaceDir(dir)
	if err != nil {
		t.Fatalf("xdg.WorkspaceDir: %v", err)
	}
	projectFile := filepath.Join(wsDataDir, "permissions.json")
	if err := os.MkdirAll(filepath.Dir(projectFile), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(projectFile, []byte("{invalid-json"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	plan := &tools.Plan{
		ToolName: "bash",
		Action:   tools.ActionExec,
		Safety:   tools.SafetySafe,
		Target:   "ls",
		ReadOnly: true,
	}

	dec, req, err := sess.Evaluate(plan)
	if dec != tools.AccessDeny {
		t.Fatalf("expected AccessDeny on corrupt project permissions file, got %s", dec)
	}
	if req != nil {
		t.Fatalf("expected nil PermissionRequest on corrupt project permissions file, got %+v", req)
	}
	if err == nil {
		t.Fatalf("expected error on corrupt project permissions file, got nil")
	}

	// Fix project file with valid JSON
	if err := os.WriteFile(projectFile, []byte("{}"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Corrupt global file
	if err := os.WriteFile(globalPath, []byte("bad-global-json"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	dec, req, err = sess.Evaluate(plan)
	if dec != tools.AccessDeny {
		t.Fatalf("expected AccessDeny on corrupt global permissions file, got %s", dec)
	}
	if req != nil {
		t.Fatalf("expected nil PermissionRequest on corrupt global permissions file, got %+v", req)
	}
	if err == nil {
		t.Fatalf("expected error on corrupt global permissions file, got nil")
	}
}

func TestApplyDecisionWithScopes(t *testing.T) {
	dir, ws := setupTestWorkspace(t)
	globalPath := filepath.Join(t.TempDir(), "global.json")

	mgr, err := permissions.New(permissions.Config{
		Workspace:  ws,
		GlobalPath: globalPath,
	})
	if err != nil {
		t.Fatalf("permissions.New: %v", err)
	}

	sess := mgr.Attach(tools.ModeDefault, permissions.Permissions{}, nil)

	req := &tools.PermissionRequest{
		ToolName:      "bash",
		Action:        tools.ActionExec,
		Safety:        tools.SafetyOutside,
		Target:        "git status",
		GrantPatterns: []string{"bash(git status *)", "bash(git *)"},
	}

	// 1. Scope: project
	err = sess.ApplyDecision(req, tools.PermissionDecision{
		Kind:  tools.DecisionAllowSession,
		Scope: string(permissions.ScopeProject),
	})
	if err != nil {
		t.Fatalf("ApplyDecision ScopeProject failed: %v", err)
	}

	projPerms, err := mgr.Get(permissions.ScopeProject)
	if err != nil {
		t.Fatalf("mgr.Get(ScopeProject): %v", err)
	}
	if !slices.Contains(projPerms.Allow, "bash(git status *)") {
		t.Fatalf("expected bash(git status *) in project perms, got %v", projPerms.Allow)
	}

	// Subsequent evaluate should allow git status
	plan := &tools.Plan{
		ToolName: "bash",
		Action:   tools.ActionExec,
		Safety:   tools.SafetyOutside,
		Target:   "git status",
	}
	if dec, _, _ := sess.Evaluate(plan); dec != tools.AccessAllow {
		t.Fatalf("expected AccessAllow after project grant, got %s", dec)
	}

	// 2. Scope: global
	reqGlobal := &tools.PermissionRequest{
		ToolName:      "bash",
		Action:        tools.ActionExec,
		Safety:        tools.SafetyOutside,
		Target:        "go version",
		GrantPatterns: []string{"bash(go *)"},
	}
	err = sess.ApplyDecision(reqGlobal, tools.PermissionDecision{
		Kind:  tools.DecisionAllowSession,
		Scope: string(permissions.ScopeGlobal),
	})
	if err != nil {
		t.Fatalf("ApplyDecision ScopeGlobal failed: %v", err)
	}

	globPerms, err := mgr.Get(permissions.ScopeGlobal)
	if err != nil {
		t.Fatalf("mgr.Get(ScopeGlobal): %v", err)
	}
	if !slices.Contains(globPerms.Allow, "bash(go *)") {
		t.Fatalf("expected bash(go *) in global perms, got %v", globPerms.Allow)
	}

	// 3. Invalid scope
	err = sess.ApplyDecision(req, tools.PermissionDecision{
		Kind:  tools.DecisionAllowSession,
		Scope: "unsupported_scope",
	})
	if !errors.Is(err, permissions.ErrInvalidScope) {
		t.Fatalf("expected ErrInvalidScope for unknown scope, got %v", err)
	}

	// 4. ScopeProject with nil manager
	nilMgrSess := permissions.NewSession(tools.ModeDefault, permissions.Permissions{}, nil)
	err = nilMgrSess.ApplyDecision(req, tools.PermissionDecision{
		Kind:  tools.DecisionAllowSession,
		Scope: string(permissions.ScopeProject),
	})
	if !errors.Is(err, permissions.ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig when manager is nil, got %v", err)
	}
	_ = dir
}
