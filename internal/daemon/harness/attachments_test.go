package harness

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/loom/tool"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tasks"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
)

type stubAttachmentMCPCaller struct {
	bodies map[string]api.MCPResourceResponse
}

func (s *stubAttachmentMCPCaller) CallMCPTool(context.Context, string, string, map[string]any) (tool.ToolStream, error) {
	return nil, errors.New("not implemented")
}

func (s *stubAttachmentMCPCaller) ReadMCPResource(_ context.Context, serverName, uri string, _ bool) (api.MCPResourceResponse, error) {
	if resp, ok := s.bodies[serverName+":"+uri]; ok {
		return resp, nil
	}
	return api.MCPResourceResponse{}, errors.New("resource not found")
}

func (s *stubAttachmentMCPCaller) HasAllowedMCPResources() bool { return true }

func setupAttachmentTestEnv(t *testing.T) (*AttachmentResolver, *workspace.Workspace, tools.Container) {
	t.Helper()
	xdg.SetTestEnv(t.TempDir(), "tasksmith-attachments-test")
	t.Cleanup(xdg.ClearCache)

	baseResolver, _, dir := setupTestResolver(t)
	rulesDir := filepath.Join(dir, ".agents", "rules")
	skillDir := filepath.Join(dir, ".agents", "skills", "release-runbook")
	toolsDir := filepath.Join(dir, ".agents", "tools")
	agentsSubDir := filepath.Join(dir, ".agents", "agents")
	for _, sub := range []string{rulesDir, skillDir, toolsDir, agentsSubDir} {
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	wsManifest := "---\napiVersion: warp/v1alpha1\nkind: Workspace\nmetadata:\n  name: att-ws\nspec:\n  defaultAgent: minimal\n---\nWorkspace.\n"
	if err := os.WriteFile(filepath.Join(dir, "WORKSPACE.md"), []byte(wsManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	agentManifest := "---\napiVersion: warp/v1alpha1\nkind: Agent\nmetadata:\n  name: minimal\n  description: Minimal agent with deferred release_helper\nspec:\n  rules:\n    - \"*\"\n  skills:\n    - release-runbook\n  tools:\n    - view\n---\nYou are minimal.\n"
	if err := os.WriteFile(filepath.Join(agentsSubDir, "minimal.agent.md"), []byte(agentManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	catalogOnlyRule := "---\napiVersion: warp/v1alpha1\nkind: Rule\nmetadata:\n  name: catalog-only-rule\n  description: Workspace rule not bound to minimal agent\nspec:\n  priority: 20\n---\nEnforce catalog-only constraints.\n"
	if err := os.WriteFile(filepath.Join(rulesDir, "catalog-only-rule.rule.md"), []byte(catalogOnlyRule), 0o644); err != nil {
		t.Fatal(err)
	}
	keywordRule := "---\napiVersion: warp/v1alpha1\nkind: Rule\nmetadata:\n  name: go-verification-gate\n  description: Run verification gate\nspec:\n  priority: 10\n  triggers:\n    keywords:\n      - verify\n---\nAlways run go test -count=1.\n"
	if err := os.WriteFile(filepath.Join(rulesDir, "go-verification-gate.rule.md"), []byte(keywordRule), 0o644); err != nil {
		t.Fatal(err)
	}
	customToolManifest := "---\napiVersion: warp/v1alpha1\nkind: Tool\nmetadata:\n  name: release_helper\n  description: Deferred release tool\nspec:\n  command: [\"echo\", \"ok\"]\n---\nHelper tool.\n"
	if err := os.WriteFile(filepath.Join(toolsDir, "release_helper.tool.md"), []byte(customToolManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	skillManifest := "---\napiVersion: warp/v1alpha1\nkind: Skill\nmetadata:\n  name: release-runbook\n  description: Step-by-step release procedure\nspec:\n  tools:\n    - release_helper\n---\nRun goreleaser check and verify checksums.\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	mcpManifest := "apiVersion: warp/v1alpha1\nkind: MCP\nmetadata:\n  name: notes\n  description: Notes MCP\nspec:\n  type: stdio\n  command: [\"notes-mcp\"]\n  policies:\n    resources:\n      exclude:\n        - \"notes://secret\"\n"
	if err := os.WriteFile(filepath.Join(dir, ".agents", "notes.mcp.yaml"), []byte(mcpManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	ws, err := workspace.Open(context.Background(), dir)
	if err != nil {
		t.Fatalf("workspace.Open: %v", err)
	}

	cat := ws.Catalog()
	resolved, err := cat.ResolveAgent("minimal")
	if err != nil {
		t.Fatalf("ResolveAgent: %v", err)
	}
	partition := cat.PartitionAgentTools(resolved)

	container, err := tools.NewContainer(tools.Config{
		Sandbox:       baseResolver.sandbox,
		Workspace:     ws,
		Logger:        log.Default(),
		CommandRunner: baseResolver.std,
		Tasks:         tasks.New(tasks.Config{Context: context.Background()}),
		Agent:         resolved.Agent,
		AllowedTools:  cat.ToolNames(),
		DeferredTools: partition.Deferred,
	})
	if err != nil {
		t.Fatalf("tools.NewContainer: %v", err)
	}

	rules := NewRuleMatcher(resolved, cat.WorkspaceSpec(), cat.Project(), cat.AgentRenderGlobals(resolved))

	caller := &stubAttachmentMCPCaller{
		bodies: map[string]api.MCPResourceResponse{
			"notes:notes://specs/spec-1": {
				URI:      "notes://specs/spec-1",
				MIMEType: "text/markdown",
				Text:     "# Spec 1\nUnified attachments.",
			},
			"notes:notes://secret": {
				URI:      "notes://secret",
				MIMEType: "text/plain",
				Text:     "top-secret-note",
			},
		},
	}

	resolver, err := NewAttachmentResolver(AttachmentResolverConfig{
		AgentName:   "minimal",
		Workspace:   ws,
		Tools:       container,
		Rules:       rules,
		Std:         baseResolver.std,
		Sandbox:     baseResolver.sandbox,
		Permissions: baseResolver.perms,
		WorkDir:     dir,
		MCPCaller:   func() tools.MCPCaller { return caller },
		Logger:      log.Default(),
	})
	if err != nil {
		t.Fatalf("NewAttachmentResolver: %v", err)
	}
	return resolver, ws, container
}

func TestAttachmentResolverValidate(t *testing.T) {
	resolver, _, _ := setupAttachmentTestEnv(t)
	ctx := context.Background()

	// 1. Valid file, rule, skill, and allowed MCP resource pass and normalize swapped lines
	got, err := resolver.Validate(ctx, []api.Attachment{
		{Kind: api.AttachmentFile, Ref: "@main.go#L3-L1", Target: "main.go", StartLine: 3, EndLine: 1, AllowProhibited: true},
		{Kind: api.AttachmentRule, Ref: "@rule:go-verification-gate", Target: "go-verification-gate"},
		{Kind: api.AttachmentSkill, Ref: "@skill:release-runbook", Target: "release-runbook"},
		{Kind: api.AttachmentMCP, Ref: "@mcp:notes:notes://specs/spec-1", Target: "notes:notes://specs/spec-1"},
	})
	if err != nil {
		t.Fatalf("expected valid attachments, got error: %v", err)
	}
	if got[0].StartLine != 1 || got[0].EndLine != 3 || got[0].AllowProhibited {
		t.Fatalf("unexpected normalized file attachment: %+v", got[0])
	}
	if got[2].Kind != api.AttachmentSkill || got[2].Target != "release-runbook" {
		t.Fatalf("unexpected normalized skill attachment: %+v", got[2])
	}

	// 2. Secret .env file rejected with 403
	if _, err := resolver.Validate(ctx, []api.Attachment{{Kind: api.AttachmentFile, Target: ".env"}}); err == nil {
		t.Fatal("expected .env attachment to be rejected")
	} else if apiErr, ok := errors.AsType[*api.APIError](err); !ok || apiErr.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 APIError for .env, got %T %v", err, err)
	}

	// 3. Prohibited MCP resource rejected with 403 unless AllowProhibited is true
	if _, err := resolver.Validate(ctx, []api.Attachment{{Kind: api.AttachmentMCP, Target: "notes:notes://secret"}}); err == nil {
		t.Fatal("expected prohibited MCP resource to be rejected without AllowProhibited")
	} else if apiErr, ok := errors.AsType[*api.APIError](err); !ok || apiErr.StatusCode != http.StatusForbidden || apiErr.Code != "mcp_policy_prohibited" {
		t.Fatalf("expected 403 mcp_policy_prohibited, got %T %v", err, err)
	}

	if _, err := resolver.Validate(ctx, []api.Attachment{{Kind: api.AttachmentMCP, Target: "notes:notes://secret", AllowProhibited: true}}); err != nil {
		t.Fatalf("expected prohibited MCP resource with AllowProhibited=true to succeed, got: %v", err)
	}

	// 4. Unknown MCP server rejected with 404
	if _, err := resolver.Validate(ctx, []api.Attachment{{Kind: api.AttachmentMCP, Target: "ghost:notes://specs/spec-1"}}); err == nil {
		t.Fatal("expected unknown MCP server to be rejected")
	} else if apiErr, ok := errors.AsType[*api.APIError](err); !ok || apiErr.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 APIError for unknown MCP server, got %T %v", err, err)
	}
}

func TestAttachmentResolverRuleAndSkillResolution(t *testing.T) {
	base, ws, container := setupAttachmentTestEnv(t)
	ctx := context.Background()

	// Construct a resolver with Rules == nil (simulating a rule-free agent) to prove
	// AttachmentResolver resolves workspace rules and skills directly from the catalog.
	resolver, err := NewAttachmentResolver(AttachmentResolverConfig{
		AgentName:   "minimal",
		Workspace:   ws,
		Tools:       container,
		Std:         base.std,
		Sandbox:     base.sandbox,
		Permissions: base.perms,
	})
	if err != nil {
		t.Fatalf("NewAttachmentResolver: %v", err)
	}

	// 1. Explicit AttachmentRule + AttachmentSkill (prose without structured attachment is ignored)
	msg := message.NewUserText("Apply @rule:catalog-only-rule and @skill:release-runbook.\n```\n@rule:other @skill:other\n```")
	SetAttachments(msg, []api.Attachment{
		{Kind: api.AttachmentRule, Ref: "@rule:catalog-only-rule", Target: "catalog-only-rule"},
		{Kind: api.AttachmentSkill, Ref: "@skill:release-runbook", Target: "release-runbook"},
	})

	state1, reminder, loaded := resolver.ResolveUserMessage(ctx, msg, State{})
	if reminder == nil {
		t.Fatal("expected <system_reminders> message for rule and skill attachments")
	}
	if len(loaded) != 1 || loaded[0].Rule != "catalog-only-rule" || loaded[0].Trigger != "manual" {
		t.Fatalf("unexpected loaded rule events: %+v", loaded)
	}
	if !state1.HasLoadedRule("catalog-only-rule") {
		t.Error("expected catalog-only-rule recorded in LoadedRules")
	}
	if !state1.HasLoadedSkill("release-runbook") {
		t.Error("expected release-runbook recorded in LoadedSkills")
	}

	text := reminder.GetContent().Text()
	for _, want := range []string{
		`<rule name="catalog-only-rule"`,
		`Enforce catalog-only constraints.`,
		`<skill name="release-runbook"`,
		`Run goreleaser check and verify checksums.`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("reminder missing %q:\n%s", want, text)
		}
	}
	if strings.Count(text, `<rule name="catalog-only-rule"`) != 1 {
		t.Errorf("expected deduplicated single <rule> block, got:\n%s", text)
	}
	if strings.Count(text, `<skill name="release-runbook"`) != 1 {
		t.Errorf("expected deduplicated single <skill> block, got:\n%s", text)
	}

	// Verify deferred skill tool release_helper was activated via ToolReferenceBlock!
	var activatedTool string
	for _, blk := range reminder.Content {
		if ref, ok := blk.(*message.ToolReferenceBlock); ok {
			activatedTool = ref.ToolName
		}
	}
	if activatedTool != "release_helper" {
		t.Errorf("expected deferred skill tool release_helper to be activated, got %q (blocks=%+v)", activatedTool, reminder.Content)
	}
	if len(state1.LoadedTools) != 1 || state1.LoadedTools[0] != "release_helper" {
		t.Errorf("expected release_helper recorded in state.LoadedTools, got %v", state1.LoadedTools)
	}

	// 2. Re-attaching the same rule and skill in the same compaction epoch is a no-op
	_, reminder2, loaded2 := resolver.ResolveUserMessage(ctx, msg, state1)
	if reminder2 != nil || len(loaded2) != 0 {
		t.Errorf("expected already-loaded rule and skill to be deduplicated, got reminder=%v loaded=%v", reminder2, loaded2)
	}
}

func TestAttachmentResolverSingleReminderCombinesAllKinds(t *testing.T) {
	resolver, _, _ := setupAttachmentTestEnv(t)
	ctx := context.Background()

	msg := message.NewUserText("Please verify @main.go with @rule:catalog-only-rule, @skill:release-runbook, and @mcp:notes:notes://specs/spec-1")
	SetAttachments(msg, []api.Attachment{
		{Kind: api.AttachmentRule, Ref: "@rule:catalog-only-rule", Target: "catalog-only-rule"},
		{Kind: api.AttachmentSkill, Ref: "@skill:release-runbook", Target: "release-runbook"},
		{Kind: api.AttachmentFile, Ref: "@main.go", Target: "main.go"},
		{Kind: api.AttachmentMCP, Ref: "@mcp:notes:notes://specs/spec-1", Target: "notes:notes://specs/spec-1"},
	})

	nextState, reminder, loaded := resolver.ResolveUserMessage(ctx, msg, State{})
	if reminder == nil {
		t.Fatal("expected synthesized <system_reminders> message")
	}
	if len(loaded) != 1 || loaded[0].Rule != "catalog-only-rule" {
		t.Fatalf("expected 1 loaded rule, got %+v", loaded)
	}
	if !nextState.HasSuggestedRule("go-verification-gate") {
		t.Fatal("expected keyword-matched go-verification-gate to be suggested")
	}

	text := reminder.GetContent().Text()
	if strings.Count(text, "<system_reminders>") != 1 || strings.Count(text, "</system_reminders>") != 1 {
		t.Fatalf("expected a single <system_reminders> wrapper, got:\n%s", text)
	}
	for _, want := range []string{
		`<rule name="catalog-only-rule"`,
		`Enforce catalog-only constraints.`,
		`go-verification-gate`,
		`<skill name="release-runbook"`,
		`Run goreleaser check and verify checksums.`,
		`<context_file ref="@main.go" path="main.go">`,
		`package main`,
		`<context_resource ref="@mcp:notes:notes://specs/spec-1" server="notes" uri="notes://specs/spec-1" mime_type="text/markdown">`,
		`# Spec 1`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("reminder missing %q:\n%s", want, text)
		}
	}
}
