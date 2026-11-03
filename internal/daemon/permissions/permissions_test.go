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
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/daemon/permissions"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
	"github.com/masterkeysrd/warp"
)

func setupTestWorkspace(t *testing.T) (string, *workspace.Workspace) {
	t.Helper()
	xdg.SetTestEnv(t.TempDir(), "tasksmith-permissions-test")
	t.Cleanup(xdg.ClearCache)

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
	rules := permissions.DeriveGrantRules("tool", api.ActionExec, "")
	if !slices.Equal(rules, []string{"tool(*)"}) {
		t.Fatalf("empty target: got %v", rules)
	}

	// ActionRead / Write / Delete
	rwRules := permissions.DeriveGrantRules("write_file", api.ActionWrite, "internal/pkg/file.go")
	if !slices.Equal(rwRules, []string{"write_file(internal/pkg/file.go)", "write_file(internal/pkg/**)"}) {
		t.Fatalf("read/write rules: got %v", rwRules)
	}

	// ActionExec with subcommands
	execRules := permissions.DeriveGrantRules("bash", api.ActionExec, "go test ./...")
	if !slices.Equal(execRules, []string{"bash(go test *)", "bash(go *)"}) {
		t.Fatalf("exec rules with subcommand: got %v", execRules)
	}

	// ActionExec with flags or single binary
	execSingle := permissions.DeriveGrantRules("bash", api.ActionExec, "ls -la")
	if !slices.Equal(execSingle, []string{"bash(ls *)"}) {
		t.Fatalf("exec rules with flag: got %v", execSingle)
	}

	// Bash command classified as ActionWrite with multi-line commit message
	commitRules := permissions.DeriveGrantRules("bash", api.ActionWrite, "git commit --amend -m \"feat(daemon): add fsutils\n\nBody text\"")
	if !slices.Equal(commitRules, []string{"bash(git commit *)", "bash(git *)"}) {
		t.Fatalf("commit rules: got %v", commitRules)
	}

	// Compound command
	compoundRules := permissions.DeriveGrantRules("bash", api.ActionExec, "make && ./run")
	if !slices.Equal(compoundRules, []string{"bash(make *)"}) {
		t.Fatalf("compound rules: got %v", compoundRules)
	}

	// Leading environment variable assignments
	envRules := permissions.DeriveGrantRules("bash", api.ActionExec, "GOOS=linux go build ./...")
	if !slices.Equal(envRules, []string{"bash(go build *)", "bash(go *)"}) {
		t.Fatalf("env rules: got %v", envRules)
	}

	// ActionNet with host
	netRules := permissions.DeriveGrantRules("web_fetch", api.ActionNet, "https://api.github.com/v1")
	if !slices.Equal(netRules, []string{"web_fetch(api.github.com)", "web_fetch(https://api.github.com/v1)"}) {
		t.Fatalf("net rules with host: got %v", netRules)
	}

	// ActionNet without host
	netNoHost := permissions.DeriveGrantRules("web_fetch", api.ActionNet, "invalid-url")
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
	glob, err := mgr.Get(api.ScopeGlobal)
	if err != nil {
		t.Fatalf("mgr.Get(ScopeGlobal): %v", err)
	}
	if len(glob.Allow) != 0 || len(glob.Deny) != 0 {
		t.Fatalf("expected empty global perms, got %+v", glob)
	}

	proj, err := mgr.Get(api.ScopeProject)
	if err != nil {
		t.Fatalf("mgr.Get(ScopeProject): %v", err)
	}
	if len(proj.Allow) != 0 || len(proj.Deny) != 0 {
		t.Fatalf("expected empty project perms, got %+v", proj)
	}

	// Invalid scope
	if _, err := mgr.Get(api.ScopeSession); !errors.Is(err, permissions.ErrInvalidScope) {
		t.Fatalf("expected ErrInvalidScope for ScopeSession on Manager, got %v", err)
	}
	if err := mgr.Set(api.ScopeSession, api.Permissions{}); !errors.Is(err, permissions.ErrInvalidScope) {
		t.Fatalf("expected ErrInvalidScope for Set ScopeSession on Manager, got %v", err)
	}

	// AddRule and RemoveRule with deduplication
	if err := mgr.AddRule(api.ScopeGlobal, api.EffectAllow, "bash(go *)"); err != nil {
		t.Fatalf("AddRule: %v", err)
	}
	// duplicate ignored
	if err := mgr.AddRule(api.ScopeGlobal, api.EffectAllow, "bash(go *)"); err != nil {
		t.Fatalf("AddRule duplicate: %v", err)
	}
	if err := mgr.AddRule(api.ScopeGlobal, api.EffectDeny, "bash(rm -rf *)"); err != nil {
		t.Fatalf("AddRule deny: %v", err)
	}

	glob, err = mgr.Get(api.ScopeGlobal)
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
	if err := mgr.RemoveRule(api.ScopeGlobal, api.EffectAllow, "bash(go *)"); err != nil {
		t.Fatalf("RemoveRule: %v", err)
	}
	glob, _ = mgr.Get(api.ScopeGlobal)
	if len(glob.Allow) != 0 {
		t.Fatalf("expected empty allow after RemoveRule, got %v", glob.Allow)
	}

	// Set with validation error
	if err := mgr.Set(api.ScopeProject, api.Permissions{
		Allow: []string{"invalid_bare_tool"},
	}); !errors.Is(err, permissions.ErrInvalidRule) {
		t.Fatalf("expected ErrInvalidRule on Set, got %v", err)
	}

	// Project path file persistence
	if err := mgr.Set(api.ScopeProject, api.Permissions{
		Allow: []string{"edit(internal/**)", "edit(internal/**)"},
		Deny:  []string{"bash(kill *)"},
	}); err != nil {
		t.Fatalf("Set project perms: %v", err)
	}
	proj, _ = mgr.Get(api.ScopeProject)
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

	var updatedPerms api.Permissions
	updateCount := 0
	sess := mgr.Attach(api.ModeDefault, api.Permissions{}, func(p api.Permissions) error {
		updatedPerms = p.Clone()
		updateCount++
		return nil
	})

	if sess.Mode() != api.ModeDefault {
		t.Fatalf("expected ModeDefault, got %s", sess.Mode())
	}

	safeReadPlan := &tools.Plan{
		ToolName: "view",
		Action:   api.ActionRead,
		Safety:   api.SafetySafe,
		Target:   "README.md",
		ReadOnly: true,
	}
	outsideReadPlan := &tools.Plan{
		ToolName: "view",
		Action:   api.ActionRead,
		Safety:   api.SafetyOutside,
		Target:   "/outside/somefile.txt",
		ReadOnly: true,
	}
	unsafeReadPlan := &tools.Plan{
		ToolName: "view",
		Action:   api.ActionRead,
		Safety:   api.SafetyUnsafe,
		Target:   "/etc/hosts",
		ReadOnly: true,
	}
	writePlan := &tools.Plan{
		ToolName: "write_file",
		Action:   api.ActionWrite,
		Safety:   api.SafetySafe,
		Target:   "internal/pkg/a.go",
		Diff:     "+package pkg",
		ReadOnly: false,
	}
	unsafePlan := &tools.Plan{
		ToolName: "bash",
		Action:   api.ActionDelete,
		Safety:   api.SafetyUnsafe,
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
	if err := sess.ApplyDecision(outReq, api.PermissionDecision{Kind: api.DecisionAllowSession}); err != nil {
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
	sess.SetMode(api.ModeAcceptEdits)
	if dec, _, _ := sess.Evaluate(writePlan); dec != tools.AccessAllow {
		t.Fatalf("ModeAcceptEdits writePlan: expected AccessAllow, got %s", dec)
	}
	if dec, _, _ := sess.Evaluate(unsafePlan); dec != tools.AccessAsk {
		t.Fatalf("ModeAcceptEdits unsafePlan: expected AccessAsk, got %s", dec)
	}

	// 3. ModePlan
	sess.SetMode(api.ModePlan)
	if dec, _, _ := sess.Evaluate(writePlan); dec != tools.AccessDeny {
		t.Fatalf("ModePlan writePlan: expected AccessDeny, got %s", dec)
	}
	if dec, _, _ := sess.Evaluate(safeReadPlan); dec != tools.AccessAllow {
		t.Fatalf("ModePlan safeReadPlan: expected AccessAllow, got %s", dec)
	}

	// 4. ModeAsk
	sess.SetMode(api.ModeAsk)
	if dec, _, _ := sess.Evaluate(safeReadPlan); dec != tools.AccessAsk {
		t.Fatalf("ModeAsk safeReadPlan: expected AccessAsk, got %s", dec)
	}
	_ = sess.AddAllow("view(README.md)")
	if dec, _, _ := sess.Evaluate(safeReadPlan); dec != tools.AccessAllow {
		t.Fatalf("ModeAsk with allow rule: expected AccessAllow, got %s", dec)
	}
	_ = sess.Clear()

	// 5. ModeAuto
	sess.SetMode(api.ModeAuto)
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
	if err := mgr.AddRule(api.ScopeProject, api.EffectDeny, "write_file(internal/**)"); err != nil {
		t.Fatalf("AddRule ScopeProject Deny: %v", err)
	}
	_ = sess.AddAllow("write_file(*)")
	if dec, _, _ := sess.Evaluate(writePlan); dec != tools.AccessDeny {
		t.Fatalf("Project Deny rule did not take precedence: got %s", dec)
	}
	_ = mgr.RemoveRule(api.ScopeProject, api.EffectDeny, "write_file(internal/**)")

	// Global-level Deny rule also takes precedence
	if err := mgr.AddRule(api.ScopeGlobal, api.EffectDeny, "write_file(*)"); err != nil {
		t.Fatalf("AddRule ScopeGlobal Deny: %v", err)
	}
	if dec, _, _ := sess.Evaluate(writePlan); dec != tools.AccessDeny {
		t.Fatalf("Global Deny rule did not take precedence: got %s", dec)
	}
	_ = mgr.RemoveRule(api.ScopeGlobal, api.EffectDeny, "write_file(*)")

	// 7. ApplyDecision with Reject
	dummyReq := &api.PermissionRequest{
		ToolName: "bash",
		Action:   api.ActionExec,
		Target:   "rm -rf /",
	}
	if err := sess.ApplyDecision(dummyReq, api.PermissionDecision{Kind: api.DecisionReject, Feedback: "blocked"}); !errors.Is(err, tools.ErrPermissionDenied) || !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("ApplyDecision Reject with feedback failed: %v", err)
	}
	if err := sess.ApplyDecision(dummyReq, api.PermissionDecision{Kind: api.DecisionReject}); !errors.Is(err, tools.ErrPermissionDenied) {
		t.Fatalf("ApplyDecision Reject without feedback failed: %v", err)
	}
	if err := sess.ApplyDecision(dummyReq, api.PermissionDecision{Kind: "unknown"}); !errors.Is(err, tools.ErrPermissionDenied) {
		t.Fatalf("ApplyDecision unknown kind failed: %v", err)
	}

	// 8. Workspace policies: Exclude, Include, Dangerous, OpenWorld
	sess.SetPolicies(&warp.Policies{
		Tools: &warp.ToolPolicies{
			Exclude:   []string{"excluded_*"},
			Include:   []string{"view", "write_file"},
			Dangerous: warp.ToolPolicyDeny,
			OpenWorld: warp.ToolPolicyDeny,
		},
	})
	if dec, _, _ := sess.Evaluate(&tools.Plan{ToolName: "excluded_tool"}); dec != tools.AccessDeny {
		t.Fatalf("expected excluded tool to be denied, got %s", dec)
	}
	if dec, _, _ := sess.Evaluate(&tools.Plan{ToolName: "unlisted_tool"}); dec != tools.AccessDeny {
		t.Fatalf("expected unlisted tool to be denied, got %s", dec)
	}
	if dec, _, _ := sess.Evaluate(&tools.Plan{ToolName: "view", Safety: api.SafetyUnsafe}); dec != tools.AccessDeny {
		t.Fatalf("expected dangerous plan to be denied when dangerous=deny, got %s", dec)
	}
	if dec, _, _ := sess.Evaluate(&tools.Plan{ToolName: "view", Action: api.ActionNet}); dec != tools.AccessDeny {
		t.Fatalf("expected ActionNet plan to be denied when openWorld=deny, got %s", dec)
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
	sess := mgr.Attach(api.ModeDefault, api.Permissions{}, nil)

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
		Action:   api.ActionExec,
		Safety:   api.SafetySafe,
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

	sess := mgr.Attach(api.ModeDefault, api.Permissions{}, nil)

	req := &api.PermissionRequest{
		ToolName:      "bash",
		Action:        api.ActionExec,
		Safety:        api.SafetyOutside,
		Target:        "git status",
		GrantPatterns: []string{"bash(git status *)", "bash(git *)"},
	}

	// 1. Scope: project
	err = sess.ApplyDecision(req, api.PermissionDecision{
		Kind:  api.DecisionAllowSession,
		Scope: api.ScopeProject,
	})
	if err != nil {
		t.Fatalf("ApplyDecision ScopeProject failed: %v", err)
	}

	projPerms, err := mgr.Get(api.ScopeProject)
	if err != nil {
		t.Fatalf("mgr.Get(ScopeProject): %v", err)
	}
	if !slices.Contains(projPerms.Allow, "bash(git status *)") {
		t.Fatalf("expected bash(git status *) in project perms, got %v", projPerms.Allow)
	}

	// Subsequent evaluate should allow git status
	plan := &tools.Plan{
		ToolName: "bash",
		Action:   api.ActionExec,
		Safety:   api.SafetyOutside,
		Target:   "git status",
	}
	if dec, _, _ := sess.Evaluate(plan); dec != tools.AccessAllow {
		t.Fatalf("expected AccessAllow after project grant, got %s", dec)
	}

	// 2. Scope: global
	reqGlobal := &api.PermissionRequest{
		ToolName:      "bash",
		Action:        api.ActionExec,
		Safety:        api.SafetyOutside,
		Target:        "go version",
		GrantPatterns: []string{"bash(go *)"},
	}
	err = sess.ApplyDecision(reqGlobal, api.PermissionDecision{
		Kind:  api.DecisionAllowSession,
		Scope: api.ScopeGlobal,
	})
	if err != nil {
		t.Fatalf("ApplyDecision ScopeGlobal failed: %v", err)
	}

	globPerms, err := mgr.Get(api.ScopeGlobal)
	if err != nil {
		t.Fatalf("mgr.Get(ScopeGlobal): %v", err)
	}
	if !slices.Contains(globPerms.Allow, "bash(go *)") {
		t.Fatalf("expected bash(go *) in global perms, got %v", globPerms.Allow)
	}

	// 3. Invalid scope
	err = sess.ApplyDecision(req, api.PermissionDecision{
		Kind:  api.DecisionAllowSession,
		Scope: "unsupported_scope",
	})
	if !errors.Is(err, permissions.ErrInvalidScope) {
		t.Fatalf("expected ErrInvalidScope for unknown scope, got %v", err)
	}

	// 4. ScopeProject with nil manager
	nilMgrSess := permissions.NewSession(api.ModeDefault, api.Permissions{}, nil)
	err = nilMgrSess.ApplyDecision(req, api.PermissionDecision{
		Kind:  api.DecisionAllowSession,
		Scope: api.ScopeProject,
	})
	if !errors.Is(err, permissions.ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig when manager is nil, got %v", err)
	}
	_ = dir
}

// TestSessionManagerReadOnlyPolicy verifies that policies.tools.readOnly denies
// runtime plans that are not read-only, while still permitting read-only plans.
func TestSessionManagerReadOnlyPolicy(t *testing.T) {
	_, ws := setupTestWorkspace(t)
	mgr, err := permissions.New(permissions.Config{
		Workspace:  ws,
		GlobalPath: filepath.Join(t.TempDir(), "global.json"),
	})
	if err != nil {
		t.Fatalf("permissions.New: %v", err)
	}
	sess := mgr.Attach(api.ModeDefault, api.Permissions{}, nil)

	sess.SetPolicies(&warp.Policies{Tools: &warp.ToolPolicies{ReadOnly: true}})

	// A non-read-only plan is denied by the readOnly ceiling even in ModeDefault.
	if dec, req, _ := sess.Evaluate(&tools.Plan{ToolName: "write_file", Action: api.ActionWrite, Safety: api.SafetySafe, Target: "a.go", ReadOnly: false}); dec != tools.AccessDeny || req != nil {
		t.Fatalf("expected readOnly ceiling to deny non-read-only plan, got %s, %+v", dec, req)
	}

	// A read-only, safe plan is still permitted.
	if dec, _, _ := sess.Evaluate(&tools.Plan{ToolName: "view", Action: api.ActionRead, Safety: api.SafetySafe, Target: "README.md", ReadOnly: true}); dec != tools.AccessAllow {
		t.Fatalf("expected readOnly ceiling to allow read-only safe plan, got %s", dec)
	}
}

// TestSessionManagerAskPolicyForcesConfirmation verifies that policies.tools.ask
// forces an AccessAsk on an otherwise auto-approvable plan until an explicit
// allow rule is granted.
func TestSessionManagerAskPolicyForcesConfirmation(t *testing.T) {
	_, ws := setupTestWorkspace(t)
	mgr, err := permissions.New(permissions.Config{
		Workspace:  ws,
		GlobalPath: filepath.Join(t.TempDir(), "global.json"),
	})
	if err != nil {
		t.Fatalf("permissions.New: %v", err)
	}
	sess := mgr.Attach(api.ModeDefault, api.Permissions{}, nil)

	sess.SetPolicies(&warp.Policies{Tools: &warp.ToolPolicies{Ask: []string{"view"}}})

	plan := &tools.Plan{ToolName: "view", Action: api.ActionRead, Safety: api.SafetySafe, Target: "README.md", ReadOnly: true}

	// The ask ceiling forces confirmation even though the plan is a safe read.
	dec, req, err := sess.Evaluate(plan)
	if err != nil || dec != tools.AccessAsk || req == nil {
		t.Fatalf("expected ask ceiling to force AccessAsk, got %s, %+v, %v", dec, req, err)
	}

	// Granting an explicit allow rule satisfies the ask ceiling.
	if err := sess.AddAllow("view(README.md)"); err != nil {
		t.Fatalf("AddAllow: %v", err)
	}
	if dec, _, _ := sess.Evaluate(plan); dec != tools.AccessAllow {
		t.Fatalf("expected AccessAllow after allow rule satisfies ask ceiling, got %s", dec)
	}
}

// TestSessionManagerOpenWorldPolicy verifies that policies.tools.openWorld: ask
// forces confirmation on network (ActionNet) plans.
func TestSessionManagerOpenWorldPolicy(t *testing.T) {
	_, ws := setupTestWorkspace(t)
	mgr, err := permissions.New(permissions.Config{
		Workspace:  ws,
		GlobalPath: filepath.Join(t.TempDir(), "global.json"),
	})
	if err != nil {
		t.Fatalf("permissions.New: %v", err)
	}
	sess := mgr.Attach(api.ModeDefault, api.Permissions{}, nil)

	sess.SetPolicies(&warp.Policies{Tools: &warp.ToolPolicies{OpenWorld: warp.ToolPolicyAsk}})

	plan := &tools.Plan{ToolName: "web_fetch", Action: api.ActionNet, Safety: api.SafetySafe, Target: "https://example.com", ReadOnly: true}

	dec, req, err := sess.Evaluate(plan)
	if err != nil || dec != tools.AccessAsk || req == nil {
		t.Fatalf("expected openWorld:ask to force AccessAsk on ActionNet plan, got %s, %+v, %v", dec, req, err)
	}

	// An explicit allow rule satisfies the openWorld ask ceiling.
	if err := sess.AddAllow("web_fetch(https://example.com)"); err != nil {
		t.Fatalf("AddAllow: %v", err)
	}
	if dec, _, _ := sess.Evaluate(plan); dec != tools.AccessAllow {
		t.Fatalf("expected AccessAllow after allow rule satisfies openWorld ask, got %s", dec)
	}
}

// TestSessionManagerNamespaceQualifiedPatterns verifies that include, exclude,
// and ask patterns match namespace-qualified names (namespace/name and
// namespace/Kind/name), not just the bare short name.
func TestSessionManagerNamespaceQualifiedPatterns(t *testing.T) {
	xdg.SetTestEnv(t.TempDir(), "tasksmith-permissions-ns-test")
	t.Cleanup(xdg.ClearCache)

	dir := t.TempDir()
	if realDir, err := filepath.EvalSymlinks(dir); err == nil {
		dir = realDir
	}

	toolsDir := filepath.Join(dir, ".agents", "tools")
	if err := os.MkdirAll(toolsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	toolManifest := "---\napiVersion: warp/v1alpha1\nkind: Tool\nmetadata:\n  name: lint\n  description: Run linter\nspec:\n  command: [\"golangci-lint\", \"run\"]\n---\nRun the linter.\n"
	if err := os.WriteFile(filepath.Join(toolsDir, "lint.tool.md"), []byte(toolManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	ws, err := workspace.Open(context.Background(), dir)
	if err != nil {
		t.Fatalf("workspace.Open: %v", err)
	}

	tool, ok := ws.Catalog().GetTool("lint")
	if !ok {
		t.Fatal("expected lint tool to be loaded into catalog")
	}
	nsName := tool.GetNamespace() + "/" + tool.GetName()
	qualName := tool.QualifiedName()

	mgr, err := permissions.New(permissions.Config{
		Workspace:  ws,
		GlobalPath: filepath.Join(t.TempDir(), "global.json"),
	})
	if err != nil {
		t.Fatalf("permissions.New: %v", err)
	}
	sess := mgr.Attach(api.ModeDefault, api.Permissions{}, nil)

	lintPlan := func() *tools.Plan {
		return &tools.Plan{ToolName: "lint", Action: api.ActionExec, Safety: api.SafetySafe, Target: "lint", ReadOnly: false}
	}

	// Exclude via qualified name denies the tool.
	sess.SetPolicies(&warp.Policies{Tools: &warp.ToolPolicies{Exclude: []string{qualName}}})
	if dec, _, _ := sess.Evaluate(lintPlan()); dec != tools.AccessDeny {
		t.Fatalf("expected qualified exclude %q to deny lint, got %s", qualName, dec)
	}

	// Ask via namespace name forces confirmation.
	sess.SetPolicies(&warp.Policies{Tools: &warp.ToolPolicies{Ask: []string{nsName}}})
	if dec, req, _ := sess.Evaluate(lintPlan()); dec != tools.AccessAsk || req == nil {
		t.Fatalf("expected namespace-qualified ask %q to force AccessAsk on lint, got %s, %+v", nsName, dec, req)
	}

	// Include via qualified name: an unlisted tool is denied by the include
	// filter, while lint is permitted once an allow rule is granted.
	sess.SetPolicies(&warp.Policies{Tools: &warp.ToolPolicies{Include: []string{qualName}}})
	if dec, _, _ := sess.Evaluate(&tools.Plan{ToolName: "other", Action: api.ActionExec, Safety: api.SafetySafe, Target: "other", ReadOnly: false}); dec != tools.AccessDeny {
		t.Fatalf("expected unlisted tool to be denied by include filter, got %s", dec)
	}
	if err := sess.AddAllow("lint(*)"); err != nil {
		t.Fatalf("AddAllow: %v", err)
	}
	if dec, _, _ := sess.Evaluate(lintPlan()); dec != tools.AccessAllow {
		t.Fatalf("expected lint to be allowed via qualified include + allow rule, got %s", dec)
	}
}

// TestSessionManagerAgentPolicyNarrowing verifies that the active agent's tool
// policies narrow (but cannot widen) the workspace policy ceiling, following the
// allow < ask < deny monotonic ordering.
func TestSessionManagerAgentPolicyNarrowing(t *testing.T) {
	_, ws := setupTestWorkspace(t)
	mgr, err := permissions.New(permissions.Config{
		Workspace:  ws,
		GlobalPath: filepath.Join(t.TempDir(), "global.json"),
	})
	if err != nil {
		t.Fatalf("permissions.New: %v", err)
	}

	dangerousPlan := &tools.Plan{ToolName: "bash", Action: api.ActionDelete, Safety: api.SafetyUnsafe, Target: ".git/config", ReadOnly: false}

	// 1. Agent narrows a workspace "ask" ceiling to "deny".
	sess := mgr.Attach(api.ModeDefault, api.Permissions{}, nil)
	sess.SetPolicies(&warp.Policies{Tools: &warp.ToolPolicies{Dangerous: warp.ToolPolicyAsk}})
	sess.SetAgentPolicies(&warp.Policies{Tools: &warp.ToolPolicies{Dangerous: warp.ToolPolicyDeny}})
	if dec, _, _ := sess.Evaluate(dangerousPlan); dec != tools.AccessDeny {
		t.Fatalf("expected agent deny to narrow workspace ask to deny, got %s", dec)
	}

	// 2. Agent cannot widen a workspace "deny" ceiling back to "ask".
	sess = mgr.Attach(api.ModeDefault, api.Permissions{}, nil)
	sess.SetPolicies(&warp.Policies{Tools: &warp.ToolPolicies{Dangerous: warp.ToolPolicyDeny}})
	sess.SetAgentPolicies(&warp.Policies{Tools: &warp.ToolPolicies{Dangerous: warp.ToolPolicyAsk}})
	if dec, _, _ := sess.Evaluate(dangerousPlan); dec != tools.AccessDeny {
		t.Fatalf("expected agent to not widen workspace deny, got %s", dec)
	}

	// 3. Agent "allow" cannot widen a workspace "ask" ceiling to "allow".
	sess = mgr.Attach(api.ModeDefault, api.Permissions{}, nil)
	sess.SetPolicies(&warp.Policies{Tools: &warp.ToolPolicies{Dangerous: warp.ToolPolicyAsk}})
	sess.SetAgentPolicies(&warp.Policies{Tools: &warp.ToolPolicies{Dangerous: warp.ToolPolicyAllow}})
	if dec, req, _ := sess.Evaluate(dangerousPlan); dec != tools.AccessAsk || req == nil {
		t.Fatalf("expected agent allow to not widen workspace ask, got %s, %+v", dec, req)
	}
}
