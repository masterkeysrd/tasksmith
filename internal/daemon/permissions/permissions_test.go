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

	// ActionNet without host: the literal target plus the tool-wide rule,
	// since a non-URL net target cannot be narrowed to a host.
	netNoHost := permissions.DeriveGrantRules("web_fetch", api.ActionNet, "invalid-url")
	if !slices.Equal(netNoHost, []string{"web_fetch(invalid-url)", "web_fetch(*)"}) {
		t.Fatalf("net rules without host: got %v", netNoHost)
	}

	// web_search targets are queries, not URLs: offer the query and the
	// tool-wide rule so "always allow" can cover later searches.
	searchRules := permissions.DeriveGrantRules("web_search", api.ActionNet, "go generics")
	if !slices.Equal(searchRules, []string{"web_search(go generics)", "web_search(*)"}) {
		t.Fatalf("web_search rules: got %v", searchRules)
	}

	// A query with parentheses cannot round-trip through ParseRule, so only
	// the tool-wide rule is offered.
	parenRules := permissions.DeriveGrantRules("web_search", api.ActionNet, `fmt.Errorf("%w") in func main()`)
	if !slices.Equal(parenRules, []string{"web_search(*)"}) {
		t.Fatalf("web_search rules with parentheses: got %v", parenRules)
	}
	for _, rule := range parenRules {
		if _, _, err := permissions.ParseRule(rule); err != nil {
			t.Fatalf("derived rule %q does not parse: %v", rule, err)
		}
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

// attachTestSession wires a fresh global permissions file and returns a
// session manager attached in the given mode.
func attachTestSession(t *testing.T, mode api.PermissionMode) permissions.SessionManager {
	t.Helper()
	_, ws := setupTestWorkspace(t)
	mgr, err := permissions.New(permissions.Config{
		Workspace:  ws,
		GlobalPath: filepath.Join(t.TempDir(), "global.json"),
	})
	if err != nil {
		t.Fatalf("permissions.New: %v", err)
	}
	return mgr.Attach(mode, api.Permissions{}, nil)
}

func bypassBashPlan(target string) *tools.Plan {
	return &tools.Plan{
		ToolName:      "bash",
		Action:        api.ActionExec,
		Safety:        api.SafetyUnsafe,
		Target:        target,
		ReadOnly:      false,
		Unsandboxed:   true,
		BypassSandbox: true,
	}
}

func excludedBashPlan(target string) *tools.Plan {
	return &tools.Plan{ToolName: "bash", Action: api.ActionExec, Safety: api.SafetySafe, Target: target, Unsandboxed: true}
}

func sandboxedBashPlan(target string) *tools.Plan {
	return &tools.Plan{ToolName: "bash", Action: api.ActionExec, Safety: api.SafetySafe, Target: target}
}

func TestMatchRuleUnsandboxedNamespace(t *testing.T) {
	bypass := bypassBashPlan("git commit -m x")
	sandboxed := sandboxedBashPlan("git commit -m x")

	cases := []struct {
		rule  string
		plan  *tools.Plan
		match bool
	}{
		{"bash(git *)", bypass, false},
		{"bash_unsandboxed(git commit *)", bypass, true},
		{"bash_unsandboxed(git *)", sandboxed, false},
		{"bash(git *)", sandboxed, true},
		{"*(*)", bypass, true},
	}
	for _, tc := range cases {
		got := permissions.MatchRule(tc.rule, tc.plan)
		if got != tc.match {
			t.Errorf("MatchRule(%q, unsandboxed=%v bypass=%v): got %v, want %v", tc.rule, tc.plan.Unsandboxed, tc.plan.BypassSandbox, got, tc.match)
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

func TestEffectivePermissions(t *testing.T) {
	_, ws := setupTestWorkspace(t)
	globalPath := filepath.Join(t.TempDir(), "global.json")

	mgr, err := permissions.New(permissions.Config{
		Workspace:  ws,
		GlobalPath: globalPath,
	})
	if err != nil {
		t.Fatalf("permissions.New: %v", err)
	}

	// 1. Set global permissions with defaultMode
	if err := mgr.Set(api.ScopeGlobal, api.Permissions{
		DefaultMode: api.ModeDefault,
		Allow:       []string{"view(*)"},
	}); err != nil {
		t.Fatalf("Set global: %v", err)
	}

	eff, err := mgr.Effective()
	if err != nil {
		t.Fatalf("Effective: %v", err)
	}
	if eff.DefaultMode != api.ModeDefault {
		t.Fatalf("expected DefaultMode default, got %v", eff.DefaultMode)
	}

	// 2. Set project permissions overriding defaultMode
	if err := mgr.Set(api.ScopeProject, api.Permissions{
		DefaultMode: api.ModeAcceptEdits,
		Allow:       []string{"edit(src/**)"},
	}); err != nil {
		t.Fatalf("Set project: %v", err)
	}

	eff2, err := mgr.Effective()
	if err != nil {
		t.Fatalf("Effective: %v", err)
	}
	if eff2.DefaultMode != api.ModeAcceptEdits {
		t.Fatalf("expected project DefaultMode accept-edits override, got %v", eff2.DefaultMode)
	}
	// Allow union: view(*), edit(src/**)
	if len(eff2.Allow) != 2 {
		t.Fatalf("expected 2 allow rules in union, got %v", eff2.Allow)
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

	// 3. ModePlan shares base permission gate with ModeDefault
	sess.SetMode(api.ModePlan)
	if dec, _, _ := sess.Evaluate(writePlan); dec != tools.AccessAsk {
		t.Fatalf("ModePlan writePlan: expected AccessAsk, got %s", dec)
	}
	if dec, _, _ := sess.Evaluate(safeReadPlan); dec != tools.AccessAllow {
		t.Fatalf("ModePlan safeReadPlan: expected AccessAllow, got %s", dec)
	}

	// 4. ModeAsk shares base permission gate with ModeDefault
	sess.SetMode(api.ModeAsk)
	if dec, _, _ := sess.Evaluate(safeReadPlan); dec != tools.AccessAllow {
		t.Fatalf("ModeAsk safeReadPlan: expected AccessAllow, got %s", dec)
	}
	if dec, _, _ := sess.Evaluate(writePlan); dec != tools.AccessAsk {
		t.Fatalf("ModeAsk writePlan: expected AccessAsk, got %s", dec)
	}
	_ = sess.AddAllow("write_file(internal/pkg/a.go)")
	if dec, _, _ := sess.Evaluate(writePlan); dec != tools.AccessAllow {
		t.Fatalf("ModeAsk with allow rule: expected AccessAllow, got %s", dec)
	}
	_ = sess.Clear()

	// 5. ModeAuto (tightened): safe non-delete plans auto-approve; anything
	// Unsafe/Outside/Delete needs a matching allow rule or falls through to Ask
	// for the permission reviewer / human.
	sess.SetMode(api.ModeAuto)
	if dec, _, _ := sess.Evaluate(writePlan); dec != tools.AccessAllow {
		t.Fatalf("ModeAuto writePlan: expected AccessAllow, got %s", dec)
	}
	if dec, _, _ := sess.Evaluate(unsafePlan); dec != tools.AccessAsk {
		t.Fatalf("ModeAuto unsafePlan: expected AccessAsk, got %s", dec)
	}
	if dec, _, _ := sess.Evaluate(outsideReadPlan); dec != tools.AccessAsk {
		t.Fatalf("ModeAuto outsideReadPlan: expected AccessAsk, got %s", dec)
	}
	if dec, _, _ := sess.Evaluate(unsafeReadPlan); dec != tools.AccessAsk {
		t.Fatalf("ModeAuto unsafeReadPlan: expected AccessAsk, got %s", dec)
	}
	// A matching allow rule still auto-approves a non-safe (but non-Unsafe) plan.
	if dec, _, _ := sess.Evaluate(outsideReadPlan); dec != tools.AccessAsk {
		t.Fatalf("ModeAuto outsideReadPlan pre-grant: expected AccessAsk, got %s", dec)
	}
	_ = sess.AddAllow("view(/outside/somefile.txt)")
	if dec, _, _ := sess.Evaluate(outsideReadPlan); dec != tools.AccessAllow {
		t.Fatalf("ModeAuto outsideReadPlan with allow rule: expected AccessAllow, got %s", dec)
	}
	// Unsafe plans stay behind the ask gate even with a matching allow rule.
	_ = sess.AddAllow("view(/etc/hosts)")
	if dec, _, _ := sess.Evaluate(unsafeReadPlan); dec != tools.AccessAsk {
		t.Fatalf("ModeAuto unsafeReadPlan with allow rule: expected AccessAsk, got %s", dec)
	}
	_ = sess.Clear()

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

// TestSessionManagerIncludeDoesNotDenyHostApprovals verifies that an explicit
// tools.include allow-list does not silently deny the synthetic "net" plans
// the sandbox proxy mints for unlisted hosts: those plans represent egress
// requested through an already-approved tool, so they must still reach the
// ask stage (and openWorld:deny must still deny them).
func TestSessionManagerIncludeDoesNotDenyHostApprovals(t *testing.T) {
	sess := attachTestSession(t, api.ModeDefault)
	sess.SetPolicies(&warp.Policies{
		Tools: &warp.ToolPolicies{Include: []string{"bash", "view", "edit"}},
	})

	netPlan := &tools.Plan{ToolName: "net", Action: api.ActionNet, Safety: api.SafetyOutside, Target: "api.example.com"}
	dec, req, err := sess.Evaluate(netPlan)
	if dec != tools.AccessAsk || req == nil || err != nil {
		t.Fatalf("synthetic net plan with include list: got %s (req=%+v err=%v), want ask", dec, req, err)
	}

	sess.SetPolicies(&warp.Policies{
		Tools: &warp.ToolPolicies{Include: []string{"bash"}, OpenWorld: warp.ToolPolicyDeny},
	})
	if dec, _, _ := sess.Evaluate(&tools.Plan{ToolName: "net", Action: api.ActionNet, Safety: api.SafetyOutside, Target: "api.example.com"}); dec != tools.AccessDeny {
		t.Fatalf("synthetic net plan with openWorld=deny: got %s, want deny", dec)
	}
}

// TestSessionManagerAutoModeDoesNotAutoApproveUnlistedHosts verifies the
// sandbox.json network allowlist stays authoritative in auto mode: a synthetic
// host-approval plan prompts unless an explicit net(...) allow rule matches.
func TestSessionManagerAutoModeDoesNotAutoApproveUnlistedHosts(t *testing.T) {
	sess := attachTestSession(t, api.ModeAuto)
	netPlan := &tools.Plan{ToolName: "net", Action: api.ActionNet, Safety: api.SafetyOutside, Target: "unlisted.example.com"}
	dec, req, err := sess.Evaluate(netPlan)
	if dec != tools.AccessAsk || req == nil || err != nil {
		t.Fatalf("auto mode unlisted host: got %s (req=%+v err=%v), want ask", dec, req, err)
	}

	if err := sess.AddAllow("net(other.example.com)"); err != nil {
		t.Fatalf("AddAllow: %v", err)
	}
	if dec, _, _ := sess.Evaluate(netPlan); dec != tools.AccessAsk {
		t.Fatalf("auto mode with unrelated net rule: got %s, want ask", dec)
	}
	if err := sess.AddAllow("net(unlisted.example.com)"); err != nil {
		t.Fatalf("AddAllow: %v", err)
	}
	if dec, _, _ := sess.Evaluate(netPlan); dec != tools.AccessAllow {
		t.Fatalf("auto mode with matching net rule: got %s, want allow", dec)
	}
}

func TestSessionManagerUnsandboxedDeniesSpanNamespaces(t *testing.T) {
	// A plain bash(git *) deny covers sandboxed, bypass, and excluded plans.
	for _, plan := range []*tools.Plan{sandboxedBashPlan("git commit -m x"), bypassBashPlan("git commit -m x"), excludedBashPlan("git commit -m x")} {
		sess := attachTestSession(t, api.ModeAuto)
		if err := sess.AddDeny("bash(git *)"); err != nil {
			t.Fatalf("AddDeny: %v", err)
		}
		if dec, _, _ := sess.Evaluate(plan); dec != tools.AccessDeny {
			t.Errorf("bash(git *) deny (unsandboxed=%v bypass=%v): got %s, want deny", plan.Unsandboxed, plan.BypassSandbox, dec)
		}
	}

	// bash_unsandboxed(git *) denies only the kernel-bypassing variants.
	sess := attachTestSession(t, api.ModeAuto)
	if err := sess.AddDeny("bash_unsandboxed(git *)"); err != nil {
		t.Fatalf("AddDeny: %v", err)
	}
	if dec, _, _ := sess.Evaluate(bypassBashPlan("git commit -m x")); dec != tools.AccessDeny {
		t.Errorf("expected bash_unsandboxed deny to cover bypass plan, got %s", dec)
	}
	if dec, _, _ := sess.Evaluate(excludedBashPlan("git commit -m x")); dec != tools.AccessDeny {
		t.Errorf("expected bash_unsandboxed deny to cover excluded plan, got %s", dec)
	}
	if dec, _, _ := sess.Evaluate(sandboxedBashPlan("git commit -m x")); dec == tools.AccessDeny {
		t.Error("bash_unsandboxed deny must not cover a sandboxed plan")
	}
}

func TestSessionManagerBypassSandboxEvaluation(t *testing.T) {
	for _, mode := range []api.PermissionMode{api.ModeDefault, api.ModeAcceptEdits, api.ModeAuto} {
		sess := attachTestSession(t, mode)
		if err := sess.AddAllow("bash(git commit *)"); err != nil {
			t.Fatalf("AddAllow: %v", err)
		}
		dec, req, err := sess.Evaluate(bypassBashPlan("git commit -m x"))
		if err != nil {
			t.Fatalf("Evaluate(%s): %v", mode, err)
		}
		if dec != tools.AccessAsk || req == nil {
			t.Fatalf("mode %s: expected AccessAsk despite sandboxed allow rule, got %s", mode, dec)
		}
		if !req.Unsandboxed || !req.BypassSandbox {
			t.Errorf("mode %s: expected request flagged Unsandboxed+BypassSandbox, got %+v", mode, req)
		}
		for _, p := range req.GrantPatterns {
			if !strings.HasPrefix(p, "bash_unsandboxed(") {
				t.Errorf("mode %s: expected grant pattern in bash_unsandboxed namespace, got %q", mode, p)
			}
		}

		// Granting the session decision must not widen the sandboxed namespace.
		if err := sess.ApplyDecision(req, api.PermissionDecision{Kind: api.DecisionAllowSession, GrantPattern: req.GrantPatterns[0]}); err != nil {
			t.Fatalf("ApplyDecision: %v", err)
		}
		if dec, _, _ := sess.Evaluate(bypassBashPlan("git commit -m x")); dec != tools.AccessAllow {
			t.Errorf("mode %s: expected AccessAllow after bash_unsandboxed grant, got %s", mode, dec)
		}
		// A sandboxed plan is unaffected by the unsandboxed grant: it keeps
		// its ordinary mode-based outcome (ask in default/accept_edits,
		// auto-allow in auto) and is never flagged unsandboxed.
		wantSandboxed := tools.AccessAsk
		if mode == api.ModeAuto {
			wantSandboxed = tools.AccessAllow
		}
		if dec, req2, _ := sess.Evaluate(sandboxedBashPlan("git status")); dec != wantSandboxed {
			t.Errorf("mode %s: sandboxed plan unexpectedly %s (%+v)", mode, dec, req2)
		} else if req2 != nil && req2.Unsandboxed {
			t.Errorf("mode %s: sandboxed plan request marked unsandboxed: %+v", mode, req2)
		}
	}

	// Plan mode prompts for bypass plans (shares default gate, does not auto-allow).
	sess := attachTestSession(t, api.ModePlan)
	if dec, _, _ := sess.Evaluate(bypassBashPlan("git commit -m x")); dec != tools.AccessAsk {
		t.Errorf("expected plan mode to ask for bypass plan without grant, got %s", dec)
	}
	if err := sess.AddAllow("bash_unsandboxed(git commit *)"); err != nil {
		t.Fatalf("AddAllow: %v", err)
	}
	if dec, _, _ := sess.Evaluate(bypassBashPlan("git commit -m x")); dec != tools.AccessAllow {
		t.Errorf("expected plan mode with bypass grant to allow, got %s", dec)
	}
}

func TestSessionManagerAutoAllowBashMatrix(t *testing.T) {
	safeBash := sandboxedBashPlan("go test ./...")

	// Widened case: default mode, native backend, safe sandboxed bash.
	sess := attachTestSession(t, api.ModeDefault)
	sess.SetRuntimePolicy(&permissions.RuntimePolicy{AutoAllowBash: true, SandboxNative: true})
	if dec, _, _ := sess.Evaluate(safeBash); dec != tools.AccessAllow {
		t.Errorf("expected autoAllowBash to allow safe sandboxed bash, got %s", dec)
	}

	// Unsafe or outside bash stays prompted.
	if dec, _, _ := sess.Evaluate(&tools.Plan{ToolName: "bash", Action: api.ActionExec, Safety: api.SafetyUnsafe, Target: "rm -rf build"}); dec != tools.AccessAsk {
		t.Errorf("expected unsafe bash to still prompt, got %s", dec)
	}
	if dec, _, _ := sess.Evaluate(&tools.Plan{ToolName: "bash", Action: api.ActionExec, Safety: api.SafetyOutside, Target: "curl https://example.com"}); dec != tools.AccessAsk {
		t.Errorf("expected outside-safety bash to still prompt, got %s", dec)
	}

	// Unsandboxed (excluded-command) plans are never auto-allowed.
	if dec, _, _ := sess.Evaluate(excludedBashPlan("git commit -m x")); dec != tools.AccessAsk {
		t.Errorf("expected unsandboxed excluded plan to still prompt, got %s", dec)
	}

	// Other tools are unaffected.
	if dec, _, _ := sess.Evaluate(&tools.Plan{ToolName: "web_fetch", Action: api.ActionNet, Safety: api.SafetySafe, Target: "https://example.com"}); dec != tools.AccessAsk {
		t.Errorf("expected non-bash tool to still prompt, got %s", dec)
	}

	// Local backend disables widening.
	sess.SetRuntimePolicy(&permissions.RuntimePolicy{AutoAllowBash: true, SandboxNative: false})
	if dec, _, _ := sess.Evaluate(safeBash); dec != tools.AccessAsk {
		t.Errorf("expected no widening on non-native backend, got %s", dec)
	}

	// Nil policy clears widening.
	sess.SetRuntimePolicy(nil)
	if dec, _, _ := sess.Evaluate(safeBash); dec != tools.AccessAsk {
		t.Errorf("expected nil runtime policy to disable widening, got %s", dec)
	}

	// Plan and ask modes still prompt for mutating bash.
	plan := attachTestSession(t, api.ModePlan)
	plan.SetRuntimePolicy(&permissions.RuntimePolicy{AutoAllowBash: true, SandboxNative: true})
	if dec, _, _ := plan.Evaluate(safeBash); dec != tools.AccessAsk {
		t.Errorf("expected plan mode to prompt, got %s", dec)
	}
	ask := attachTestSession(t, api.ModeAsk)
	ask.SetRuntimePolicy(&permissions.RuntimePolicy{AutoAllowBash: true, SandboxNative: true})
	if dec, _, _ := ask.Evaluate(safeBash); dec != tools.AccessAsk {
		t.Errorf("expected ask mode to prompt, got %s", dec)
	}

	// accept_edits widens too.
	edits := attachTestSession(t, api.ModeAcceptEdits)
	edits.SetRuntimePolicy(&permissions.RuntimePolicy{AutoAllowBash: true, SandboxNative: true})
	if dec, _, _ := edits.Evaluate(safeBash); dec != tools.AccessAllow {
		t.Errorf("expected accept_edits widening, got %s", dec)
	}

	// RuntimePolicy round-trips a copy.
	got := edits.RuntimePolicy()
	if got == nil || !got.AutoAllowBash || !got.SandboxNative {
		t.Errorf("expected RuntimePolicy round-trip, got %+v", got)
	}
	if got != nil {
		got.AutoAllowBash = false
		if again := edits.RuntimePolicy(); again == nil || !again.AutoAllowBash {
			t.Error("RuntimePolicy() must return a copy, not the live state")
		}
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

// TestApplyDecisionUnsandboxedGrantNamespaces verifies grants key into the
// correct rule namespace: only bypass requests mint bash_unsandboxed rules,
// bypass grants re-key plain patterns instead of widening the sandboxed
// namespace, and non-bypass requests can never mint unsandboxed rules.
func TestApplyDecisionUnsandboxedGrantNamespaces(t *testing.T) {
	// Bypass request: a plain bash(...) grant pattern must be re-keyed into
	// the unsandboxed namespace.
	sess := attachTestSession(t, api.ModeDefault)
	bypassReq := &api.PermissionRequest{
		ID: "req-bypass", ToolName: "bash", Action: api.ActionExec,
		Target: "git commit -m x", Unsandboxed: true, BypassSandbox: true,
		GrantPatterns: []string{"bash_unsandboxed(git commit *)"},
	}
	if err := sess.ApplyDecision(bypassReq, api.PermissionDecision{
		Kind: api.DecisionAllowSession, GrantPattern: "bash(git *)",
	}); err != nil {
		t.Fatalf("ApplyDecision(bypass, plain pattern): %v", err)
	}
	if dec, _, _ := sess.Evaluate(bypassBashPlan("git commit -m x")); dec != tools.AccessAllow {
		t.Errorf("expected re-keyed bash_unsandboxed(git *) grant to approve bypass plan, got %s", dec)
	}
	if dec, _, _ := sess.Evaluate(sandboxedBashPlan("git status")); dec != tools.AccessAsk {
		t.Errorf("bypass grant must not widen the sandboxed namespace, got %s", dec)
	}

	// Bypass request: a pattern for an unrelated tool is rejected.
	if err := sess.ApplyDecision(bypassReq, api.PermissionDecision{
		Kind: api.DecisionAllowSession, GrantPattern: "write(src/**)",
	}); err == nil {
		t.Error("expected rejection of unrelated grant pattern on bypass request")
	}

	// ExcludedCommands request (Unsandboxed but not BypassSandbox): granting
	// mints an ordinary bash(...) rule that covers the next excluded run, and
	// an explicit unsandboxed-namespace pattern is rejected.
	sess = attachTestSession(t, api.ModeDefault)
	excludedReq := &api.PermissionRequest{
		ID: "req-excluded", ToolName: "bash", Action: api.ActionExec,
		Target: "git commit -m x", Unsandboxed: true,
		GrantPatterns: []string{"bash(git commit *)", "bash(git *)"},
	}
	if err := sess.ApplyDecision(excludedReq, api.PermissionDecision{Kind: api.DecisionAllowSession}); err != nil {
		t.Fatalf("ApplyDecision(excluded, default pattern): %v", err)
	}
	if dec, _, _ := sess.Evaluate(excludedBashPlan("git commit -m y")); dec != tools.AccessAllow {
		t.Errorf("expected bash(git commit *) grant to cover the next excluded run, got %s", dec)
	}
	if err := sess.ApplyDecision(excludedReq, api.PermissionDecision{
		Kind: api.DecisionAllowSession, GrantPattern: "bash_unsandboxed(git *)",
	}); err == nil {
		t.Error("expected rejection of unsandboxed grant pattern on non-bypass request")
	}
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

// TestSessionManagerForSubagentIsolatesAgentPolicies verifies that ForSubagent
// shares session mode, workspace policies, and allow/deny rules with the parent
// SessionManager while isolating SetAgentPolicies per subagent.
func TestSessionManagerForSubagentIsolatesAgentPolicies(t *testing.T) {
	_, ws := setupTestWorkspace(t)
	mgr, err := permissions.New(permissions.Config{
		Workspace:  ws,
		GlobalPath: filepath.Join(t.TempDir(), "global.json"),
	})
	if err != nil {
		t.Fatalf("permissions.New: %v", err)
	}

	parent := mgr.Attach(api.ModeAuto, api.Permissions{}, nil)
	child := parent.ForSubagent()

	// Child restricts tools to view only (excluding bash), simulating explore.
	child.SetAgentPolicies(&warp.Policies{
		Tools: &warp.ToolPolicies{
			Include: []string{"view"},
			Exclude: []string{"bash"},
		},
	})

	bashPlan := &tools.Plan{ToolName: "bash", Action: api.ActionExec, Safety: api.SafetySafe, Target: "go version"}
	viewPlan := &tools.Plan{ToolName: "view", Action: api.ActionRead, Safety: api.SafetySafe, Target: "README.md", ReadOnly: true}

	// Child denies bash and allows view.
	if dec, _, _ := child.Evaluate(bashPlan); dec != tools.AccessDeny {
		t.Fatalf("expected child to deny bash under explore agent policies, got %s", dec)
	}
	if dec, _, _ := child.Evaluate(viewPlan); dec != tools.AccessAllow {
		t.Fatalf("expected child to allow view, got %s", dec)
	}

	// Parent must still allow bash because its agentPolicies were not mutated by child.
	if parent.AgentPolicies() != nil {
		t.Fatalf("expected parent AgentPolicies to remain nil, got %+v", parent.AgentPolicies())
	}
	if dec, _, _ := parent.Evaluate(bashPlan); dec != tools.AccessAllow {
		t.Fatalf("expected parent to still allow bash in ModeAuto, got %s", dec)
	}

	// Session-scoped rules and mode changes remain shared between parent and child.
	if err := child.AddAllow("view(README.md)"); err != nil {
		t.Fatalf("child.AddAllow: %v", err)
	}
	if got := parent.Permissions().Allow; len(got) != 1 || got[0] != "view(README.md)" {
		t.Fatalf("expected parent to see shared allow rule from child, got %v", got)
	}

	parent.SetMode(api.ModeAsk)
	if got := child.Mode(); got != api.ModeAsk {
		t.Fatalf("expected child mode to reflect parent SetMode, got %q", got)
	}
}

func TestFileAccessChecker(t *testing.T) {
	dir, ws := setupTestWorkspace(t)
	mgr, err := permissions.New(permissions.Config{
		Workspace:  ws,
		GlobalPath: filepath.Join(t.TempDir(), "global.json"),
	})
	if err != nil {
		t.Fatalf("permissions.New: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("SECRET=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env.example"), []byte("SECRET=\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Project-scoped deny rule must apply even when evaluating through an attached SessionManager.
	if err := mgr.AddRule(api.ScopeProject, api.EffectDeny, "view(secret.txt)"); err != nil {
		t.Fatalf("AddRule project deny: %v", err)
	}
	sess := mgr.Attach(api.ModeAuto, api.Permissions{}, nil)
	if err := sess.AddDeny("ls(docs)"); err != nil {
		t.Fatalf("AddDeny session: %v", err)
	}

	checker := permissions.NewFileAccessChecker(permissions.FileAccessConfig{
		WorkDir: dir,
		Session: sess,
	})

	if err := checker.Check("main.go", "main.go", false); err != nil {
		t.Errorf("expected main.go allowed, got %v", err)
	}
	if err := checker.Check(".env.example", ".env.example", false); err != nil {
		t.Errorf("expected .env.example allowed, got %v", err)
	}
	if err := checker.Check(".env", ".env", false); err == nil {
		t.Errorf("expected .env prohibited, got nil")
	}
	if err := checker.Check(".git/config", ".git/config", false); err == nil {
		t.Errorf("expected .git/config prohibited, got nil")
	}
	if err := checker.Check(".ssh/id_rsa", ".ssh/id_rsa", false); err == nil {
		t.Errorf("expected .ssh/id_rsa prohibited, got nil")
	}
	if err := checker.Check("../outside.txt", "../outside.txt", false); err == nil {
		t.Errorf("expected escaping path prohibited, got nil")
	}
	if err := checker.Check("secret.txt", "secret.txt", false); err == nil {
		t.Errorf("expected project-denied secret.txt prohibited, got nil")
	}
	if err := checker.Check("docs/", "docs/", true); err == nil {
		t.Errorf("expected session-denied docs/ prohibited, got nil")
	}

	// Symlink to .env inside workspace and symlink escaping workspace must both be rejected.
	if err := os.Symlink(filepath.Join(dir, ".env"), filepath.Join(dir, "env_link.txt")); err != nil {
		t.Fatal(err)
	}
	if err := checker.Check("env_link.txt", "env_link.txt", false); err == nil {
		t.Errorf("expected symlink to .env prohibited, got nil")
	}

	outsideFile := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outsideFile, []byte("out\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideFile, filepath.Join(dir, "escape_link.txt")); err != nil {
		t.Fatal(err)
	}
	if err := checker.Check("escape_link.txt", "escape_link.txt", false); err == nil {
		t.Errorf("expected symlink outside workspace prohibited, got nil")
	}

	// Policy ceiling excluding view must reject files.
	sess.SetAgentPolicies(&warp.Policies{
		Tools: &warp.ToolPolicies{Exclude: []string{"view"}},
	})
	restrictedChecker := permissions.NewFileAccessChecker(permissions.FileAccessConfig{
		WorkDir: dir,
		Session: sess,
	})
	if err := restrictedChecker.Check("main.go", "main.go", false); err == nil {
		t.Errorf("expected agent-policy-excluded view prohibited, got nil")
	}
}

func TestPlansDirAutoAllowAndRestrictedWorkspace(t *testing.T) {
	xdgRoot := t.TempDir()
	xdg.SetTestEnv(xdgRoot, "tasksmith-perm-plans-test")
	t.Cleanup(xdg.ClearCache)

	wsDir := t.TempDir()
	plansDir := filepath.Join(t.TempDir(), "sessions", "s1", "plans")

	planWrite := &tools.Plan{
		ToolName: "write",
		Action:   api.ActionWrite,
		Safety:   api.SafetySafe,
		Target:   filepath.Join(plansDir, "implementation.md"),
	}
	planRemove := &tools.Plan{
		ToolName: "remove",
		Action:   api.ActionDelete,
		Safety:   api.SafetySafe,
		Target:   filepath.Join(plansDir, "implementation.md"),
	}
	repoRemove := &tools.Plan{
		ToolName: "remove",
		Action:   api.ActionDelete,
		Safety:   api.SafetySafe,
		Target:   "main.go",
	}
	repoWrite := &tools.Plan{
		ToolName: "write",
		Action:   api.ActionWrite,
		Safety:   api.SafetySafe,
		Target:   "main.go",
	}
	safeRead := &tools.Plan{
		ToolName: "view",
		Action:   api.ActionRead,
		Safety:   api.SafetySafe,
		Target:   "main.go",
		ReadOnly: true,
	}

	// 1. Normal workspace: plansDir writes auto-allow in every mode; repo writes prompt in default/plan/ask.
	sess := attachTestSession(t, api.ModeDefault)
	sess.SetPlansDir(plansDir)
	for _, mode := range []api.PermissionMode{api.ModeDefault, api.ModePlan, api.ModeAsk, api.ModeAcceptEdits, api.ModeAuto} {
		sess.SetMode(mode)
		if dec, _, _ := sess.Evaluate(planWrite); dec != tools.AccessAllow {
			t.Fatalf("mode %s: expected plansDir write auto-allowed, got %s", mode, dec)
		}
		if dec, _, _ := sess.Evaluate(planRemove); dec != tools.AccessAllow {
			t.Fatalf("mode %s: expected plansDir remove auto-allowed, got %s", mode, dec)
		}
	}
	sess.SetMode(api.ModePlan)
	if dec, _, _ := sess.Evaluate(repoWrite); dec != tools.AccessAsk {
		t.Fatalf("ModePlan repo write: expected AccessAsk, got %s", dec)
	}
	if dec, _, _ := sess.Evaluate(repoRemove); dec != tools.AccessAsk {
		t.Fatalf("ModePlan repo remove: expected AccessAsk, got %s", dec)
	}

	// Explicit deny rule still blocks plansDir writes.
	if err := sess.AddDeny("write(*)"); err != nil {
		t.Fatal(err)
	}
	if dec, _, _ := sess.Evaluate(planWrite); dec != tools.AccessDeny {
		t.Fatalf("denied write: expected AccessDeny, got %s", dec)
	}

	// 2. Restricted workspace: starts in default mode, blocks repo mutations even in ModeAuto, keeps plansDir writable.
	wsRestricted, err := workspace.Open(context.Background(), wsDir, workspace.WithRestrictedMode(true))
	if err != nil {
		t.Fatal(err)
	}
	mgr, err := permissions.New(permissions.Config{
		Workspace:  wsRestricted,
		GlobalPath: filepath.Join(t.TempDir(), "global.json"),
	})
	if err != nil {
		t.Fatal(err)
	}
	restrictedSess := mgr.Attach(api.ModeDefault, api.Permissions{}, nil)
	restrictedSess.SetPlansDir(plansDir)

	if dec, _, _ := restrictedSess.Evaluate(safeRead); dec != tools.AccessAllow {
		t.Fatalf("restricted safe read: expected AccessAllow, got %s", dec)
	}
	if dec, _, _ := restrictedSess.Evaluate(repoWrite); dec != tools.AccessDeny {
		t.Fatalf("restricted repo write: expected AccessDeny, got %s", dec)
	}
	if dec, _, _ := restrictedSess.Evaluate(repoRemove); dec != tools.AccessDeny {
		t.Fatalf("restricted repo remove: expected AccessDeny, got %s", dec)
	}
	restrictedSess.SetMode(api.ModeAuto)
	if dec, _, _ := restrictedSess.Evaluate(repoWrite); dec != tools.AccessDeny {
		t.Fatalf("restricted repo write in ModeAuto: expected AccessDeny, got %s", dec)
	}
	if dec, _, _ := restrictedSess.Evaluate(planWrite); dec != tools.AccessAllow {
		t.Fatalf("restricted plansDir write: expected AccessAllow, got %s", dec)
	}
	if dec, _, _ := restrictedSess.Evaluate(planRemove); dec != tools.AccessAllow {
		t.Fatalf("restricted plansDir remove: expected AccessAllow, got %s", dec)
	}
}

func TestSessionManagerSetWorkspaceEnforcesSubprojectTrust(t *testing.T) {
	xdg.SetTestEnv(t.TempDir(), "tasksmith-perm-subws-test")
	t.Cleanup(xdg.ClearCache)

	rootDir := t.TempDir()
	subDir := filepath.Join(rootDir, "sub")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "WORKSPACE.md"), []byte("---\napiVersion: warp/v1alpha1\nkind: Workspace\nmetadata:\n  name: mono\nspec:\n  projects: [\".\", \"sub\"]\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rootWS, err := workspace.Open(context.Background(), rootDir, workspace.WithRestrictedMode(false))
	if err != nil {
		t.Fatal(err)
	}
	subRestrictedWS, err := workspace.Open(context.Background(), subDir, workspace.WithRestrictedMode(true))
	if err != nil {
		t.Fatal(err)
	}

	mgr, err := permissions.New(permissions.Config{
		Workspace:  rootWS,
		GlobalPath: filepath.Join(t.TempDir(), "global.json"),
	})
	if err != nil {
		t.Fatal(err)
	}

	subSess := mgr.Attach(api.ModeAuto, api.Permissions{}, nil)
	subSess.SetWorkspace(subRestrictedWS)

	repoWrite := &tools.Plan{ToolName: "write", Action: api.ActionWrite, Safety: api.SafetySafe, Target: "file.txt"}
	if dec, _, _ := subSess.Evaluate(repoWrite); dec != tools.AccessDeny {
		t.Fatalf("expected restricted subproject workspace to deny repo write even under trusted root manager, got %s", dec)
	}
}

func TestSessionManagerAutoModeReversibleDelete(t *testing.T) {
	sess := attachTestSession(t, api.ModeAuto)

	safeReversibleDelete := &tools.Plan{
		ToolName:   "remove",
		Action:     api.ActionDelete,
		Safety:     api.SafetySafe,
		Target:     "internal/pkg/old.go",
		Reversible: true,
	}
	if dec, _, _ := sess.Evaluate(safeReversibleDelete); dec != tools.AccessAllow {
		t.Fatalf("ModeAuto safe reversible delete = %s, want allow", dec)
	}

	unsafeReversibleDelete := &tools.Plan{
		ToolName:   "remove",
		Action:     api.ActionDelete,
		Safety:     api.SafetyUnsafe,
		Target:     ".agents/rules/security.rule.md",
		Reversible: true,
	}
	if dec, _, _ := sess.Evaluate(unsafeReversibleDelete); dec != tools.AccessAsk {
		t.Fatalf("ModeAuto unsafe reversible delete = %s, want ask", dec)
	}

	irreversibleDelete := &tools.Plan{
		ToolName:   "bash",
		Action:     api.ActionDelete,
		Safety:     api.SafetySafe,
		Target:     "rm -f internal/pkg/old.go",
		Reversible: false,
	}
	if dec, _, _ := sess.Evaluate(irreversibleDelete); dec != tools.AccessAsk {
		t.Fatalf("ModeAuto irreversible delete = %s, want ask", dec)
	}

	defaultSess := attachTestSession(t, api.ModeDefault)
	if dec, _, _ := defaultSess.Evaluate(safeReversibleDelete); dec != tools.AccessAsk {
		t.Fatalf("ModeDefault safe reversible delete = %s, want ask", dec)
	}
}

func TestMissingMentionThroughOutsideDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	checker := permissions.NewFileAccessChecker(permissions.FileAccessConfig{WorkDir: root})
	if err := checker.Check("link/missing", "", false); err == nil {
		t.Fatal("accepted missing path below external directory")
	}
}
