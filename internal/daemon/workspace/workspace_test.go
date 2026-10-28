package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
)

func setupTestXDG(t *testing.T) string {
	t.Helper()
	xdgRoot := t.TempDir()
	xdg.SetTestEnv(xdgRoot, "tasksmith-test")
	t.Cleanup(func() {
		xdg.ClearCache()
	})
	return xdgRoot
}

func TestPresets(t *testing.T) {
	setupTestXDG(t)

	presets := Presets()
	if len(presets) < 6 {
		t.Fatalf("expected at least 6 provider presets, got %d", len(presets))
	}

	expected := map[string]bool{
		"anthropic":    false,
		"antigravity":  false,
		"google-genai": false,
		"ollama":       false,
		"openai":       false,
		"typesafe":     false,
	}

	for _, p := range presets {
		if _, ok := expected[p.Name]; ok {
			expected[p.Name] = true
		}
		if p.Name == "typesafe" {
			if p.DefaultModels[RoleDecision] != "jev-latest" {
				t.Errorf("typesafe default decision model = %q, want %q", p.DefaultModels[RoleDecision], "jev-latest")
			}
			foundJev := false
			for _, m := range p.Models {
				if m.ID == "jev-latest" {
					foundJev = true
					if !m.Capabilities.SupportsDecision() {
						t.Errorf("expected jev-latest to support decision capability")
					}
					if m.Capabilities.SupportsCompletion() {
						t.Errorf("expected jev-latest completion capability to be false")
					}
				}
			}
			if !foundJev {
				t.Errorf("expected typesafe preset to include jev-latest model")
			}
		}
	}

	for name, found := range expected {
		if !found {
			t.Errorf("missing expected provider preset %q", name)
		}
	}
}

func TestIsConfigured_IsInitialized_Initialize_CopyConfig(t *testing.T) {
	setupTestXDG(t)
	ctx := context.Background()

	dir := t.TempDir()
	if IsConfigured(dir) {
		t.Fatalf("expected empty dir %q to not be configured", dir)
	}
	if IsInitialized(dir) {
		t.Fatalf("expected empty dir %q to not be initialized", dir)
	}

	// Test TrustOnly initialization first on a separate directory
	trustDir := t.TempDir()
	if err := Initialize(ctx, trustDir, InitOptions{TrustOnly: true}); err != nil {
		t.Fatalf("Initialize(TrustOnly) error: %v", err)
	}
	if IsConfigured(trustDir) {
		t.Errorf("expected TrustOnly dir to not have WORKSPACE.md or .agents")
	}
	if !IsInitialized(trustDir) {
		t.Errorf("expected TrustOnly dir to report IsInitialized == true via setup.json")
	}

	// Full initialization on dir
	opts := InitOptions{
		ProjectName:     "demo-workspace",
		Provider:        "openai",
		APIKey:          "sk-test-secret-123",
		Endpoint:        "https://api.openai.com/v1",
		DefaultModel:    "gpt-5.5",
		DefaultAgent:    "main",
		AuthorizedTools: []string{"ls", "view", "write"},
	}
	if err := Initialize(ctx, dir, opts); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	if !IsConfigured(dir) {
		t.Errorf("expected dir to be configured after Initialize")
	}
	if !IsInitialized(dir) {
		t.Errorf("expected dir to be initialized after Initialize")
	}

	// Verify setup.json exists in XDG workspace dir
	wsDataDir, err := xdg.WorkspaceDir(dir)
	if err != nil {
		t.Fatalf("xdg.WorkspaceDir error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wsDataDir, "setup.json")); err != nil {
		t.Errorf("expected setup.json to exist in %s: %v", wsDataDir, err)
	}

	// Verify theme.json is NOT written (UI themes are not a workspace responsibility)
	cfgDir, err := xdg.SubConfigDir()
	if err != nil {
		t.Fatalf("xdg.SubConfigDir error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cfgDir, "theme.json")); !os.IsNotExist(err) {
		t.Errorf("expected theme.json to NOT be written by workspace.Initialize")
	}

	// Verify WORKSPACE.md
	wsBytes, err := os.ReadFile(filepath.Join(dir, "WORKSPACE.md"))
	if err != nil {
		t.Fatalf("read WORKSPACE.md: %v", err)
	}
	wsContent := string(wsBytes)
	if !strings.Contains(wsContent, "name: demo-workspace") {
		t.Errorf("WORKSPACE.md missing name: %s", wsContent)
	}
	if !strings.Contains(wsContent, "defaultProvider: openai") {
		t.Errorf("WORKSPACE.md missing defaultProvider: %s", wsContent)
	}

	// Verify .agents/providers/openai.yaml
	provPath := filepath.Join(dir, ".agents", "providers", "openai.yaml")
	if _, err := os.Stat(provPath); err != nil {
		t.Errorf("expected provider file at %s: %v", provPath, err)
	}

	// Verify .env and .gitignore
	envBytes, err := os.ReadFile(filepath.Join(dir, ".env"))
	if err != nil {
		t.Fatalf("read .env: %v", err)
	}
	if !strings.Contains(string(envBytes), "OPENAI_API_KEY=sk-test-secret-123") {
		t.Errorf("unexpected .env content: %s", string(envBytes))
	}

	gitignoreBytes, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	if !strings.Contains(string(gitignoreBytes), ".env") {
		t.Errorf(".gitignore missing .env entry: %s", string(gitignoreBytes))
	}

	// Open initialized workspace and verify domain state
	ws, err := Open(ctx, dir)
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	if ws.IsSynthetic() {
		t.Errorf("expected IsSynthetic() == false")
	}
	if ws.Name() != "demo-workspace" {
		t.Errorf("ws.Name() = %q, want %q", ws.Name(), "demo-workspace")
	}
	if got := ws.Env()["OPENAI_API_KEY"]; got != "sk-test-secret-123" {
		t.Errorf("ws.Env()[OPENAI_API_KEY] = %q, want %q", got, "sk-test-secret-123")
	}
	if pol := ws.Policies(); pol == nil || pol.Tools == nil || len(pol.Tools.Include) != 3 {
		t.Errorf("unexpected workspace policies: %+v", pol)
	}

	// Test CopyConfig into a new worktree directory (C3a/C3b promotion flow)
	dstDir := t.TempDir()
	if err := CopyConfig(dir, dstDir); err != nil {
		t.Fatalf("CopyConfig error: %v", err)
	}
	if !IsConfigured(dstDir) {
		t.Errorf("expected dstDir to be configured after CopyConfig")
	}
	for _, rel := range []string{"WORKSPACE.md", ".env", filepath.Join(".agents", "providers", "openai.yaml")} {
		if _, err := os.Stat(filepath.Join(dstDir, rel)); err != nil {
			t.Errorf("CopyConfig missing %s in dstDir: %v", rel, err)
		}
	}
}

func TestFourTierResolutionPriority(t *testing.T) {
	setupTestXDG(t)
	ctx := context.Background()

	// Inject a builtin tool manifest and custom system agent via WithBuiltinFS
	injectedFS := fstest.MapFS{
		"tools/bash.tool.md": &fstest.MapFile{
			Data: []byte(`---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: bash
  description: System bash tool
spec:
  command: ["sh", "-c"]
  annotations:
    isDangerous: true
---
System bash instructions.
`),
		},
	}

	rootDir := t.TempDir()
	subProjDir := filepath.Join(rootDir, "packages", "service")
	if err := os.MkdirAll(subProjDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write WORKSPACE.md declaring both "." and "packages/service"
	wsManifest := `---
apiVersion: warp/v1alpha1
kind: Workspace
metadata:
  name: multi-tier-ws
spec:
  projects: [".", "packages/service"]
---
Workspace root instructions.
`
	if err := os.WriteFile(filepath.Join(rootDir, "WORKSPACE.md"), []byte(wsManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	// Tier 1 (system): Open at subProjDir with only system resources present
	ws, err := Open(ctx, subProjDir, WithBuiltinFS(injectedFS))
	if err != nil {
		t.Fatalf("Open (tier 1) error: %v", err)
	}

	mainAgent, ok := ws.GetAgent("main")
	if !ok {
		t.Fatalf("expected builtin main agent to be resolved")
	}
	if mainAgent.Namespace != NamespaceSystem {
		t.Errorf("expected main agent namespace %q, got %q", NamespaceSystem, mainAgent.Namespace)
	}

	bashTool, ok := ws.GetTool("bash")
	if !ok {
		t.Fatalf("expected injected bash tool to be resolved")
	}
	if bashTool.Namespace != NamespaceSystem || bashTool.Description != "System bash tool" {
		t.Errorf("unexpected tier 1 bash tool: %+v", bashTool)
	}

	// Tier 2 (user): Write user-global override in $XDG_CONFIG_HOME/tasksmith-test/agents/
	userAgentsDir, err := xdg.SubConfigDir("agents")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(userAgentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	userMainAgent := `---
apiVersion: warp/v1alpha1
kind: Agent
metadata:
  name: main
  description: User-tier main agent
spec:
  temperature: 0.2
---
User tier instructions.
`
	userBashTool := `---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: bash
  description: User bash tool
spec:
  command: ["zsh", "-c"]
---
User bash instructions.
`
	if err := os.WriteFile(filepath.Join(userAgentsDir, "main.agent.md"), []byte(userMainAgent), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(userAgentsDir, "bash.tool.md"), []byte(userBashTool), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := ws.Reload(ctx); err != nil {
		t.Fatalf("Reload (tier 2) error: %v", err)
	}
	mainAgent, ok = ws.GetAgent("main")
	if !ok || mainAgent.Namespace != NamespaceUser || mainAgent.Description != "User-tier main agent" {
		t.Errorf("expected user tier to shadow system tier for main agent, got %+v", mainAgent)
	}
	bashTool, ok = ws.GetTool("bash")
	if !ok || bashTool.Namespace != NamespaceUser || bashTool.Description != "User bash tool" {
		t.Errorf("expected user tier to shadow system tier for bash tool, got %+v", bashTool)
	}

	// Tier 3 (workspace): Write workspace-level override in <rootDir>/.agents/
	wsAgentsDir := filepath.Join(rootDir, ".agents")
	if err := os.MkdirAll(wsAgentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	wsMainAgent := `---
apiVersion: warp/v1alpha1
kind: Agent
metadata:
  name: main
  description: Workspace-tier main agent
spec:
  temperature: 0.4
---
Workspace tier instructions.
`
	if err := os.WriteFile(filepath.Join(wsAgentsDir, "main.agent.md"), []byte(wsMainAgent), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := ws.Reload(ctx); err != nil {
		t.Fatalf("Reload (tier 3) error: %v", err)
	}
	mainAgent, ok = ws.GetAgent("main")
	if !ok || mainAgent.Namespace != NamespaceWorkspace || mainAgent.Description != "Workspace-tier main agent" {
		t.Errorf("expected workspace tier to shadow user tier for main agent, got %+v", mainAgent)
	}

	// Tier 4 (local): Write project-local override in <subProjDir>/.agents/
	localAgentsDir := filepath.Join(subProjDir, ".agents")
	if err := os.MkdirAll(localAgentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	localMainAgent := `---
apiVersion: warp/v1alpha1
kind: Agent
metadata:
  name: main
  description: Local-tier main agent
spec:
  temperature: 0.1
---
Local project tier instructions.
`
	if err := os.WriteFile(filepath.Join(localAgentsDir, "main.agent.md"), []byte(localMainAgent), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := ws.Reload(ctx); err != nil {
		t.Fatalf("Reload (tier 4) error: %v", err)
	}
	mainAgent, ok = ws.GetAgent("main")
	if !ok || mainAgent.Namespace != NamespaceLocal || mainAgent.Description != "Local-tier main agent" {
		t.Errorf("expected local tier to shadow workspace tier for main agent, got %+v", mainAgent)
	}
}

func TestIsolatedEnvManagement(t *testing.T) {
	setupTestXDG(t)
	ctx := context.Background()

	// 0. Set host environment variables to test host fallback and precedence
	t.Setenv("HOST_FALLBACK_KEY", "from-host-env")
	t.Setenv("SHARED_KEY", "from-host-env-should-be-overridden")

	// 1. Create global user .env in XDG config dir
	userEnvPath, err := xdg.SubConfigDir(".env")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(userEnvPath), 0o755); err != nil {
		t.Fatal(err)
	}
	userEnvContent := "GLOBAL_KEY=from-user-global\nSHARED_KEY=from-user-global\n"
	if err := os.WriteFile(userEnvPath, []byte(userEnvContent), 0o600); err != nil {
		t.Fatal(err)
	}

	// 2. Create workspace .env overriding SHARED_KEY
	wsDir := t.TempDir()
	wsEnvContent := "SHARED_KEY=from-workspace\nWS_ONLY_KEY=ws-secret\n"
	if err := os.WriteFile(filepath.Join(wsDir, ".env"), []byte(wsEnvContent), 0o600); err != nil {
		t.Fatal(err)
	}

	ws, err := Open(ctx, wsDir)
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}

	envMap := ws.Env()
	if envMap["HOST_FALLBACK_KEY"] != "from-host-env" {
		t.Errorf("HOST_FALLBACK_KEY = %q, want %q", envMap["HOST_FALLBACK_KEY"], "from-host-env")
	}
	if envMap["GLOBAL_KEY"] != "from-user-global" {
		t.Errorf("GLOBAL_KEY = %q, want %q", envMap["GLOBAL_KEY"], "from-user-global")
	}
	if envMap["SHARED_KEY"] != "from-workspace" {
		t.Errorf("SHARED_KEY = %q, want %q", envMap["SHARED_KEY"], "from-workspace")
	}
	if envMap["WS_ONLY_KEY"] != "ws-secret" {
		t.Errorf("WS_ONLY_KEY = %q, want %q", envMap["WS_ONLY_KEY"], "ws-secret")
	}

	// Ensure host os.Getenv was not polluted
	if os.Getenv("WS_ONLY_KEY") != "" {
		t.Errorf("expected host environment variable WS_ONLY_KEY to remain unset")
	}

	// Verify defensive copy
	envMap["MUTATED"] = "true"
	if _, exists := ws.Env()["MUTATED"]; exists {
		t.Errorf("mutating returned Env() map should not affect workspace state")
	}

	// 3. Test SetEnv updates in-memory map and persists to <rootDir>/.env
	if err := ws.SetEnv("NEW_RUNTIME_KEY", "runtime-val"); err != nil {
		t.Fatalf("SetEnv error: %v", err)
	}
	if got := ws.Env()["NEW_RUNTIME_KEY"]; got != "runtime-val" {
		t.Errorf("ws.Env()[NEW_RUNTIME_KEY] = %q, want %q", got, "runtime-val")
	}

	diskBytes, err := os.ReadFile(filepath.Join(wsDir, ".env"))
	if err != nil {
		t.Fatalf("read .env after SetEnv: %v", err)
	}
	if !strings.Contains(string(diskBytes), "NEW_RUNTIME_KEY=runtime-val") {
		t.Errorf("expected NEW_RUNTIME_KEY in on-disk .env, got:\n%s", string(diskBytes))
	}
}

func TestResolveModels(t *testing.T) {
	setupTestXDG(t)
	ctx := context.Background()

	wsDir := t.TempDir()
	wsManifest := `---
apiVersion: warp/v1alpha1
kind: Workspace
metadata:
  name: model-routing-ws
spec:
  defaultProvider: openai
  defaultModels:
    completion: gpt-5.5
    decision: typesafe/jev-latest
---
`
	if err := os.WriteFile(filepath.Join(wsDir, "WORKSPACE.md"), []byte(wsManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	provDir := filepath.Join(wsDir, ".agents", "providers")
	if err := os.MkdirAll(provDir, 0o755); err != nil {
		t.Fatal(err)
	}

	openaiProvider := `apiVersion: warp/v1alpha1
kind: ModelProvider
metadata:
  name: openai
spec:
  type: openai
  endpoint: https://api.openai.com/v1
  defaultModel: gpt-5.5
  defaultModels:
    completion: gpt-5.5
    embedding: text-embedding-3-large
  models:
    - id: gpt-5.5
      name: gpt-5.5
      label: GPT-5.5
      limits:
        context: 128000
      capabilities:
        completion: true
    - id: text-embedding-3-large
      name: text-embedding-3-large
      label: Text Embedding 3 Large
      limits:
        context: 8192
        dimensions: 3072
      capabilities:
        completion: false
        embedding: true
`
	typesafeProvider := `apiVersion: warp/v1alpha1
kind: ModelProvider
metadata:
  name: typesafe
spec:
  type: typesafe
  endpoint: https://api.typesafe.ai
  defaultModels:
    decision: jev-latest
  models:
    - id: jev-latest
      name: jev-latest
      label: Jev Latest
      limits:
        context: 64000
      capabilities:
        completion: false
        decision: true
`
	if err := os.WriteFile(filepath.Join(provDir, "openai.yaml"), []byte(openaiProvider), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(provDir, "typesafe.yaml"), []byte(typesafeProvider), 0o644); err != nil {
		t.Fatal(err)
	}

	ws, err := Open(ctx, wsDir)
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}

	// 1. Explicit override (unqualified should qualify with defaultProvider "openai")
	explicitModels := ws.ResolveModels(RoleCompletion, "gpt-5.4", "anthropic/claude-sonnet-4-6")
	if len(explicitModels) != 2 || explicitModels[0] != "openai/gpt-5.4" || explicitModels[1] != "anthropic/claude-sonnet-4-6" {
		t.Errorf("unexpected explicit models: %v", explicitModels)
	}

	// 2. RoleCompletion default from workspace
	compModels := ws.ResolveModels(RoleCompletion)
	if len(compModels) != 1 || compModels[0] != "openai/gpt-5.5" {
		t.Errorf("RoleCompletion models = %v, want [openai/gpt-5.5]", compModels)
	}

	// 3. RoleDecision default from workspace
	decModels := ws.ResolveModels(RoleDecision)
	if len(decModels) != 1 || decModels[0] != "typesafe/jev-latest" {
		t.Errorf("RoleDecision models = %v, want [typesafe/jev-latest]", decModels)
	}

	// 4. RoleEmbedding default from defaultProvider ("openai")
	embModels := ws.ResolveModels(RoleEmbedding)
	if len(embModels) != 1 || embModels[0] != "openai/text-embedding-3-large" {
		t.Errorf("RoleEmbedding models = %v, want [openai/text-embedding-3-large]", embModels)
	}
}

func TestResourcesAndTemplateRendering(t *testing.T) {
	setupTestXDG(t)
	ctx := context.Background()

	wsDir := t.TempDir()
	wsManifest := `---
apiVersion: warp/v1alpha1
kind: Workspace
metadata:
  name: render-ws
spec:
  defaultAgent: custom-coder
---
Workspace at {{.Workspace.Dir}} with env {{.RuntimeEnv}}.
`
	if err := os.WriteFile(filepath.Join(wsDir, "WORKSPACE.md"), []byte(wsManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	agentsMd := `---
apiVersion: warp/v1alpha1
kind: Context
metadata:
  name: root-context
  displayName: Demo Root Project
---
Follow strict Go conventions.
`
	if err := os.WriteFile(filepath.Join(wsDir, "AGENTS.md"), []byte(agentsMd), 0o644); err != nil {
		t.Fatal(err)
	}

	agentsDir := filepath.Join(wsDir, ".agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Agent manifest
	agentManifest := `---
apiVersion: warp/v1alpha1
kind: Agent
metadata:
  name: custom-coder
  description: Custom coding agent
spec:
  models: ["openai/gpt-5.5"]
  skills: ["go-style"]
---
Agent {{.Agent.Name}} working in {{.Project.DisplayName}} ({{.Focus}}).
Context: {{.Context.Instructions}}
`
	// Skill manifest
	skillManifest := `---
apiVersion: warp/v1alpha1
kind: Skill
metadata:
  name: go-style
  description: Go style guide skill
spec:
  useWhen: Writing Go code
  keywords: ["go", "style"]
---
Apply Go rules for package {{.PackageName}} in {{.WorkspaceDir}}.
`
	// Command manifest
	cmdManifest := `---
apiVersion: warp/v1alpha1
kind: Command
metadata:
  name: review
  description: Review a pull request
spec:
  arguments:
    - name: pr_number
    - name: branch
---
Review PR #{{.pr_number}} on branch $branch (arg1=$1).
`
	// Decision manifest
	decisionManifest := `---
apiVersion: warp/v1alpha1
kind: Decision
metadata:
  name: safety-check
  description: Evaluate command safety
spec:
  models: ["typesafe/jev-latest"]
  arguments:
    - name: command
  questions:
    - name: is_safe
      type: predicate
      instructions: Is the command safe to run unattended?
      threshold: 0.85
      criteria:
        true: Read-only inspection command
        false: Mutates filesystem or network state
---
Evaluate command: {{.command}} in directory {{.TargetDir}}.
`
	// Toolkit and MCP manifests
	toolkitManifest := `apiVersion: warp/v1alpha1
kind: Toolkit
metadata:
  name: dev-toolkit
  description: Developer toolkit
spec:
  tools:
    - name: lint
      description: Run linter
      command: ["golangci-lint", "run"]
`
	mcpManifest := `apiVersion: warp/v1alpha1
kind: MCP
metadata:
  name: github-mcp
  description: GitHub MCP server
spec:
  type: stdio
  command: ["npx", "-y", "@modelcontextprotocol/server-github"]
`

	files := map[string]string{
		"custom-coder.agent.md": agentManifest,
		"go-style.skill.md":     skillManifest,
		"review.command.md":     cmdManifest,
		"safety.decision.md":    decisionManifest,
		"dev.toolkit.yaml":      toolkitManifest,
		"github.mcp.yaml":       mcpManifest,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(agentsDir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	ws, err := Open(ctx, wsDir)
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}

	// Verify DefaultAgentName & SetAgentOverride
	if got := ws.DefaultAgentName(); got != "custom-coder" {
		t.Errorf("DefaultAgentName() = %q, want %q", got, "custom-coder")
	}
	ws.SetAgentOverride("override-agent")
	if got := ws.DefaultAgentName(); got != "override-agent" {
		t.Errorf("DefaultAgentName() after override = %q, want %q", got, "override-agent")
	}

	// Verify Queries
	if _, ok := ws.GetSkill("go-style"); !ok || len(ws.Skills()) != 1 {
		t.Errorf("expected go-style skill to be loaded")
	}
	if _, ok := ws.GetCommand("review"); !ok || len(ws.Commands()) != 1 {
		t.Errorf("expected review command to be loaded")
	}
	dec, ok := ws.GetDecision("safety-check")
	if !ok || len(ws.Decisions()) != 1 || len(dec.Questions) != 1 || dec.Questions[0].Type != DecisionQuestionPredicate {
		t.Errorf("unexpected decision resource: %+v", dec)
	}
	if _, ok := ws.GetToolkit("dev-toolkit"); !ok || len(ws.Toolkits()) != 1 {
		t.Errorf("expected dev-toolkit to be loaded")
	}
	if _, ok := ws.GetTool("lint"); !ok {
		t.Errorf("expected inline tool 'lint' from dev-toolkit to be resolved via GetTool")
	}
	if _, ok := ws.GetMCP("github-mcp"); !ok || len(ws.MCPs()) != 1 {
		t.Errorf("expected github-mcp to be loaded")
	}
	if projects := ws.Projects(); len(projects) != 1 || !strings.Contains(projects[0].Instructions, "Follow strict Go conventions.") {
		t.Errorf("unexpected projects: %+v", projects)
	}

	// Verify RenderWorkspace
	renderedWS, err := ws.RenderWorkspace(map[string]any{"RuntimeEnv": "test"})
	if err != nil {
		t.Fatalf("RenderWorkspace error: %v", err)
	}
	if !strings.Contains(renderedWS, ws.RootDir()) || !strings.Contains(renderedWS, "with env test") {
		t.Errorf("unexpected RenderWorkspace output: %q", renderedWS)
	}

	// Verify RenderAgent
	renderedAgent, err := ws.RenderAgent("custom-coder", map[string]string{"Focus": "concurrency"}, nil)
	if err != nil {
		t.Fatalf("RenderAgent error: %v", err)
	}
	if !strings.Contains(renderedAgent, "Agent custom-coder working in Demo Root Project (concurrency).") ||
		!strings.Contains(renderedAgent, "Follow strict Go conventions.") {
		t.Errorf("unexpected RenderAgent output: %q", renderedAgent)
	}

	// Verify RenderSkill
	renderedSkill, err := ws.RenderSkill("go-style", map[string]string{"PackageName": "workspace"}, nil)
	if err != nil {
		t.Fatalf("RenderSkill error: %v", err)
	}
	if !strings.Contains(renderedSkill, "Apply Go rules for package workspace in "+ws.RootDir()) {
		t.Errorf("unexpected RenderSkill output: %q", renderedSkill)
	}

	// Verify RenderCommand
	renderedCmd, err := ws.RenderCommand("review", map[string]string{
		"pr_number": "42",
		"branch":    "feat/workspace",
	}, nil)
	if err != nil {
		t.Fatalf("RenderCommand error: %v", err)
	}
	if !strings.Contains(renderedCmd, "Review PR #42 on branch feat/workspace (arg1=42).") {
		t.Errorf("unexpected RenderCommand output: %q", renderedCmd)
	}

	// Verify RenderDecision
	renderedDec, err := ws.RenderDecision("safety-check", map[string]any{
		"command": "rm -rf /tmp/foo",
	}, map[string]any{
		"TargetDir": "/workspace",
	})
	if err != nil {
		t.Fatalf("RenderDecision error: %v", err)
	}
	if !strings.Contains(renderedDec, "Evaluate command: rm -rf /tmp/foo in directory /workspace.") {
		t.Errorf("unexpected RenderDecision output: %q", renderedDec)
	}

	// Verify ResolveAgent
	resolvedAg, err := ws.ResolveAgent("custom-coder")
	if err != nil {
		t.Fatalf("ResolveAgent error: %v", err)
	}
	if resolvedAg == nil || resolvedAg.Agent == nil || resolvedAg.Agent.GetName() != "custom-coder" {
		t.Fatalf("expected resolved agent custom-coder, got: %+v", resolvedAg)
	}
	if len(resolvedAg.Skills) != 1 || resolvedAg.Skills[0].Metadata.Name != "go-style" {
		t.Errorf("expected 1 resolved skill (go-style), got: %v", resolvedAg.Skills)
	}
}

func TestWorkspacePortablePath(t *testing.T) {
	ctx := context.Background()
	wsDir := t.TempDir()

	ws, err := Open(ctx, wsDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := ws.Initialize(ctx, InitOptions{}); err != nil {
		t.Fatal(err)
	}

	// 1. Path inside workspace root
	inside := filepath.Join(ws.RootDir(), ".agents", "skills", "test-skill")
	if got := ws.PortablePath(inside); got != filepath.Join(".agents", "skills", "test-skill") {
		t.Errorf("PortablePath inside = %q, want %q", got, filepath.Join(".agents", "skills", "test-skill"))
	}

	// 2. Empty path
	if got := ws.PortablePath(""); got != "" {
		t.Errorf("PortablePath empty = %q, want empty", got)
	}
}
