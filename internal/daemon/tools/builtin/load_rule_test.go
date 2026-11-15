package builtin_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/shellguard"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools/builtin"
)

// TestLoadRuleTool verifies that load_rule renders a workspace rule's full
// instructions (indexed by short name, namespace/name, and qualified name),
// reports its priority and portable path, and rejects an empty or unknown rule
// name.
func TestLoadRuleTool(t *testing.T) {
	ctx := context.Background()
	dir, _, stdLib, sb, ws := setupBuiltinEnv(t, false)

	// Create a conditional rule manifest in the workspace.
	rulesDir := filepath.Join(dir, ".agents", "rules")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ruleContent := `---
apiVersion: warp/v1alpha1
kind: Rule
metadata:
  name: go-error-handling
  description: Go error handling guidelines
spec:
  priority: 30
  triggers:
    globs: ["**/*.go"]
    keywords: ["error", "fmt"]
---
Always wrap and propagate errors in Go.
`
	if err := os.WriteFile(filepath.Join(rulesDir, "go-error-handling.rule.md"), []byte(ruleContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Reload workspace to pick up the new rule.
	if err := ws.Reload(ctx); err != nil {
		t.Fatalf("ws.Reload: %v", err)
	}

	resolvedMain, err := ws.Catalog().ResolveAgent("main")
	if err != nil {
		t.Fatal(err)
	}
	guard := shellguard.New(shellguard.Config{WorkspaceDir: dir})
	mainTools, err := builtin.New(builtin.Config{
		Std:          stdLib,
		Sandbox:      sb,
		Guard:        guard,
		Workspace:    ws,
		Rules:        resolvedMain.Rules,
		AllowedTools: ws.Catalog().ToolNames(),
	})
	if err != nil {
		t.Fatal(err)
	}
	agentContainer, err := tools.NewContainer(withExecDeps(t, tools.Config{
		Tools:        mainTools,
		Sandbox:      sb,
		Workspace:    ws,
		Logger:       log.Default(),
		AllowedTools: ws.Catalog().ToolNames(),
	}))
	if err != nil {
		t.Fatal(err)
	}

	// 1. Success: load by short name.
	call := &message.ToolCall{
		ID:   "call-load-rule-1",
		Name: builtin.ToolLoadRule,
		Args: map[string]any{"rule": "go-error-handling"},
	}
	plan, err := agentContainer.Prepare(ctx, call)
	if err != nil {
		t.Fatalf("Prepare load_rule: %v", err)
	}
	if plan.Safety != api.SafetySafe || !plan.ReadOnly || plan.Action != api.ActionRead {
		t.Fatalf("load_rule plan = %+v, want read-only safe", plan)
	}

	res, err := agentContainer.Execute(ctx, call)
	if err != nil {
		t.Fatalf("Execute load_rule: %v", err)
	}
	if res.IsError {
		t.Fatalf("load_rule reported error: %+v", res)
	}
	out, ok := res.StructuredContent.(api.LoadRuleOutput)
	if !ok {
		t.Fatalf("expected api.LoadRuleOutput, got %T", res.StructuredContent)
	}
	if out.Rule != "go-error-handling" {
		t.Errorf("LoadRuleOutput.Rule = %q, want go-error-handling", out.Rule)
	}
	if out.Priority != 30 {
		t.Errorf("LoadRuleOutput.Priority = %d, want 30", out.Priority)
	}
	if !strings.Contains(out.Instructions, "wrap and propagate errors") {
		t.Errorf("LoadRuleOutput.Instructions = %q, want rendered body", out.Instructions)
	}
	if out.Path == "" {
		t.Error("LoadRuleOutput.Path is empty, want a portable path")
	}
	if !strings.Contains(out.TextContent(), "Rule path:") {
		t.Errorf("TextContent() = %q, want a Rule path prefix", out.TextContent())
	}

	// 2. Empty rule name is rejected at Prepare.
	emptyCall := &message.ToolCall{ID: "call-load-rule-empty", Name: builtin.ToolLoadRule, Args: map[string]any{"rule": "  "}}
	if _, err := agentContainer.Prepare(ctx, emptyCall); err == nil {
		t.Error("Prepare with empty rule name = nil error, want error")
	}

	// 3. Unknown rule name is rejected at Execute.
	unknownCall := &message.ToolCall{ID: "call-load-rule-unknown", Name: builtin.ToolLoadRule, Args: map[string]any{"rule": "does-not-exist"}}
	resUnknown, err := agentContainer.Execute(ctx, unknownCall)
	if err != nil {
		t.Fatalf("Execute load_rule unknown: %v", err)
	}
	if !resUnknown.IsError {
		t.Errorf("Execute with unknown rule name: IsError = false, want true")
	}
}
