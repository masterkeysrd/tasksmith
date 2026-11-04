package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/daemon/models"
	"github.com/masterkeysrd/warp"
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
		name := p.Metadata.Name
		if _, ok := expected[name]; ok {
			expected[name] = true
		}
		if name == "typesafe" {
			decDefault := ""
			if p.Spec.DefaultModels != nil {
				decDefault = p.Spec.DefaultModels.Decision
			}
			if decDefault != "jev-latest" {
				t.Errorf("typesafe default decision model = %q, want %q", decDefault, "jev-latest")
			}
			foundJev := false
			for _, m := range p.Spec.Models {
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
		DefaultModels:   map[warp.ModelRole]string{warp.ModelRoleCompletion: "gpt-5.5"},
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
	if pol := ws.Catalog().Policies(); pol == nil || pol.Tools == nil || len(pol.Tools.Include) != 3 {
		t.Errorf("unexpected workspace policies: %+v", pol)
	}

	// Test CopyConfig into a new worktree directory (C3a/C3b promotion flow)
	// Write a warp.lock so we can verify it is promoted alongside WORKSPACE.md.
	lockContent := "source: github.com/example/plugin\nversion: v1.2.3\nhash: abc123\n"
	if err := os.WriteFile(filepath.Join(dir, "warp.lock"), []byte(lockContent), 0o644); err != nil {
		t.Fatalf("write warp.lock: %v", err)
	}

	dstDir := t.TempDir()
	if err := CopyConfig(dir, dstDir); err != nil {
		t.Fatalf("CopyConfig error: %v", err)
	}
	if !IsConfigured(dstDir) {
		t.Errorf("expected dstDir to be configured after CopyConfig")
	}
	for _, rel := range []string{"WORKSPACE.md", ".env", "warp.lock", filepath.Join(".agents", "providers", "openai.yaml")} {
		if _, err := os.Stat(filepath.Join(dstDir, rel)); err != nil {
			t.Errorf("CopyConfig missing %s in dstDir: %v", rel, err)
		}
	}

	// Verify warp.lock content was preserved byte-for-byte.
	copiedLock, err := os.ReadFile(filepath.Join(dstDir, "warp.lock"))
	if err != nil {
		t.Fatalf("read copied warp.lock: %v", err)
	}
	if string(copiedLock) != lockContent {
		t.Errorf("copied warp.lock content = %q, want %q", string(copiedLock), lockContent)
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

	mainAgent, ok := ws.Catalog().GetAgent("main")
	if !ok {
		t.Fatalf("expected builtin main agent to be resolved")
	}
	if mainAgent.Def.Namespace != NamespaceSystem {
		t.Errorf("expected main agent namespace %q, got %q", NamespaceSystem, mainAgent.Def.Namespace)
	}

	bashTool, ok := ws.Catalog().GetTool("bash")
	if !ok {
		t.Fatalf("expected injected bash tool to be resolved")
	}
	if bashTool.Namespace != NamespaceSystem || bashTool.Metadata.Description != "System bash tool" {
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
	mainAgent, ok = ws.Catalog().GetAgent("main")
	if !ok || mainAgent.Def.Namespace != NamespaceUser || mainAgent.Description != "User-tier main agent" {
		t.Errorf("expected user tier to shadow system tier for main agent, got %+v", mainAgent)
	}
	bashTool, ok = ws.Catalog().GetTool("bash")
	if !ok || bashTool.Namespace != NamespaceUser || bashTool.Metadata.Description != "User bash tool" {
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
	mainAgent, ok = ws.Catalog().GetAgent("main")
	if !ok || mainAgent.Def.Namespace != NamespaceWorkspace || mainAgent.Description != "Workspace-tier main agent" {
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
	mainAgent, ok = ws.Catalog().GetAgent("main")
	if !ok || ws.Catalog().ScopeNamespace(mainAgent.Def.BaseResource) != NamespaceLocal || mainAgent.Description != "Local-tier main agent" {
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
	explicitModels := ws.Catalog().ResolveModels(warp.ModelRoleCompletion, "gpt-5.4", "anthropic/claude-sonnet-4-6")
	if len(explicitModels) != 2 || explicitModels[0].QualifiedID != "openai/gpt-5.4" || explicitModels[1].QualifiedID != "anthropic/claude-sonnet-4-6" {
		t.Errorf("unexpected explicit models: %v", explicitModels)
	}

	// 2. RoleCompletion default from workspace
	compModels := ws.Catalog().ResolveModels(warp.ModelRoleCompletion)
	if len(compModels) != 1 || compModels[0].QualifiedID != "openai/gpt-5.5" {
		t.Errorf("RoleCompletion models = %v, want [openai/gpt-5.5]", compModels)
	}

	// 3. RoleDecision default from workspace
	decModels := ws.Catalog().ResolveModels(warp.ModelRoleDecision)
	if len(decModels) != 1 || decModels[0].QualifiedID != "typesafe/jev-latest" {
		t.Errorf("RoleDecision models = %v, want [typesafe/jev-latest]", decModels)
	}

	// 4. RoleEmbedding default from defaultProvider ("openai")
	embModels := ws.Catalog().ResolveModels(warp.ModelRoleEmbedding)
	if len(embModels) != 1 || embModels[0].QualifiedID != "openai/text-embedding-3-large" {
		t.Errorf("RoleEmbedding models = %v, want [openai/text-embedding-3-large]", embModels)
	}

	// 5. Provider with slash in model ID (e.g. "ddalcu/Qwen3.8-27B-MLX-Serve-4bit")
	mlxProvider := `apiVersion: warp/v1alpha1
kind: ModelProvider
metadata:
  name: mlx-serve
spec:
  type: ollama
  endpoint: http://localhost:11234
  defaultModels:
    completion: qwen3.8
  models:
    - id: ddalcu/Qwen3.8-27B-MLX-Serve-4bit
      name: qwen3.8-27b-mlx-serve-4bit
      label: Qwen 3.8 (MLX Serve)
      limits:
        context: 262144
        output: 32768
`
	if err := os.WriteFile(filepath.Join(provDir, "mlx-serve.yaml"), []byte(mlxProvider), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ws.Reload(ctx); err != nil {
		t.Fatalf("Reload error: %v", err)
	}

	for _, ref := range []string{
		"ddalcu/Qwen3.8-27B-MLX-Serve-4bit",
		"mlx-serve/ddalcu/Qwen3.8-27B-MLX-Serve-4bit",
		"qwen3.8-27b-mlx-serve-4bit",
		"mlx-serve/qwen3.8-27b-mlx-serve-4bit",
	} {
		resolved := ws.Catalog().ResolveModels(warp.ModelRoleCompletion, ref)
		if len(resolved) != 1 || resolved[0].QualifiedID != "mlx-serve/ddalcu/Qwen3.8-27B-MLX-Serve-4bit" {
			t.Errorf("ResolveModels(%q) = %v, want [mlx-serve/ddalcu/Qwen3.8-27B-MLX-Serve-4bit]", ref, resolved)
		}
		model, ok := ws.Catalog().ResolveModel(warp.ModelRoleCompletion, ref)
		if !ok || model.Model == nil || model.Model.Limits.Context != 262144 || model.Model.Limits.Output != 32768 {
			t.Errorf("ResolveModel(%q) = %+v, want limits 262144/32768", ref, model)
		}
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
	// Tool and MCP manifests
	toolManifest := `apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: lint
  description: Run linter
spec:
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
		"lint.tool.yaml":        toolManifest,
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

	// Verify DefaultAgentName
	if got := ws.Catalog().DefaultAgentName(); got != "custom-coder" {
		t.Errorf("DefaultAgentName() = %q, want %q", got, "custom-coder")
	}

	// Verify Queries
	sk, ok := ws.Catalog().GetSkill("go-style")
	if !ok {
		t.Errorf("expected go-style skill to be loaded")
	}
	cmd, ok := ws.Catalog().GetCommand("review")
	if !ok || len(ws.Catalog().Commands()) != 1 {
		t.Errorf("expected review command to be loaded")
	}
	if _, ok := ws.Catalog().GetTool("lint"); !ok {
		t.Errorf("expected tool 'lint' to be resolved via GetTool")
	}

	// Verify Render Workspace
	wsSpec := ws.Catalog().WorkspaceSpec()
	if wsSpec == nil {
		t.Fatal("expected workspace spec")
	}
	renderedWS, err := wsSpec.Render(&warp.WorkspaceRenderOptions{
		Globals: map[string]any{"RuntimeEnv": "test"},
	})
	if err != nil {
		t.Fatalf("Render Workspace error: %v", err)
	}
	if !strings.Contains(renderedWS, ws.RootDir()) || !strings.Contains(renderedWS, "with env test") {
		t.Errorf("unexpected RenderWorkspace output: %q", renderedWS)
	}

	// Verify Render Agent
	ag, ok := ws.Catalog().GetAgent("custom-coder")
	if !ok {
		t.Fatal("expected custom-coder agent")
	}
	resolvedAg, err := ws.Catalog().ResolveAgent("custom-coder")
	if err != nil {
		t.Fatalf("ResolveAgent error: %v", err)
	}
	renderedAgent, err := ag.Def.Render(&warp.AgentRenderOptions{
		Workspace: ws.Catalog().WorkspaceSpec(),
		Project:   ws.Catalog().Project(),
		Resolved:  resolvedAg,
		Globals:   map[string]any{"Focus": "concurrency"},
	})
	if err != nil {
		t.Fatalf("RenderAgent error: %v", err)
	}
	if !strings.Contains(renderedAgent, "Agent custom-coder working in Demo Root Project (concurrency).") ||
		!strings.Contains(renderedAgent, "Follow strict Go conventions.") {
		t.Errorf("unexpected RenderAgent output: %q", renderedAgent)
	}

	// Verify Render Skill
	renderedSkill, err := sk.Def.Render(&warp.SkillRenderOptions{
		Workspace: ws.Catalog().WorkspaceSpec(),
		Project:   ws.Catalog().Project(),
		Globals:   map[string]any{"PackageName": "workspace"},
	})
	if err != nil {
		t.Fatalf("RenderSkill error: %v", err)
	}
	if !strings.Contains(renderedSkill, "Apply Go rules for package workspace in "+ws.RootDir()) {
		t.Errorf("unexpected RenderSkill output: %q", renderedSkill)
	}

	// Verify Render Command
	cmdArgs := map[string]string{
		"pr_number": "42",
		"branch":    "feat/workspace",
	}
	renderedCmd, err := cmd.Def.Render(&warp.CommandRenderOptions{
		Workspace: ws.Catalog().WorkspaceSpec(),
		Project:   ws.Catalog().Project(),
		Args:      BuildPositionalArgs(cmd.Def.Spec.Arguments, cmdArgs),
		Globals:   MergeStringArgs(cmdArgs, nil),
	})
	if err != nil {
		t.Fatalf("RenderCommand error: %v", err)
	}
	if !strings.Contains(renderedCmd, "Review PR #42 on branch feat/workspace (arg1=42).") {
		t.Errorf("unexpected RenderCommand output: %q", renderedCmd)
	}

	// Verify ResolveAgent
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
	insideDir := filepath.Join(ws.RootDir(), ".agents", "skills", "test-skill")
	if err := os.MkdirAll(insideDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// 1. Path inside workspace root
	if got := ws.PortablePath(insideDir); got != filepath.Join(".agents", "skills", "test-skill") {
		t.Errorf("PortablePath inside = %q, want %q", got, filepath.Join(".agents", "skills", "test-skill"))
	}

	// 2. Empty path
	if got := ws.PortablePath(""); got != "" {
		t.Errorf("PortablePath empty = %q, want empty", got)
	}
}

// warnContains reports whether any warning in warnings contains sub.
func warnContains(warnings []string, sub string) bool {
	for _, w := range warnings {
		if strings.Contains(w, sub) {
			return true
		}
	}
	return false
}

// builtinToolManifests returns a MapFS of minimal system-namespace tool
// manifests for the given names, so that built-in agents whose spec.tools
// reference them resolve without a "references missing tool" validation warning.
func builtinToolManifests(names []string) fstest.MapFS {
	m := fstest.MapFS{}
	for _, name := range names {
		m["tools/"+name+".tool.md"] = &fstest.MapFile{Data: []byte(fmt.Sprintf(`---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: %s
  description: Builtin %s tool
spec:
  annotations:
    isReadOnly: true
    isIdempotent: true
---
Builtin %s tool.
`, name, name, name))}
	}
	return m
}

// builtinToolNames is the set of built-in system tools referenced by the
// embedded built-in agents' spec.tools.
var builtinToolNames = []string{
	"ask_question", "bash", "download", "edit", "glob", "grep", "invoke_agent",
	"load_rule", "load_skill", "ls", "manage_agents", "remove", "tasks", "todos",
	"tool_search", "view", "web_fetch", "write",
}

// TestCatalogValidationWarnings verifies that warp.Registry.Validate() failures
// are surfaced as non-fatal diagnostic warnings via Catalog.Warnings() instead
// of crashing workspace.Open.
func TestCatalogValidationWarnings(t *testing.T) {
	setupTestXDG(t)
	ctx := context.Background()

	// 1. A valid workspace produces zero warnings.
	t.Run("ValidWorkspaceZeroWarnings", func(t *testing.T) {
		dir := t.TempDir()
		manifest := `---
apiVersion: warp/v1alpha1
kind: Workspace
metadata:
  name: valid-ws
spec:
  defaultAgent: main
---
Valid workspace instructions.
`
		if err := os.WriteFile(filepath.Join(dir, "WORKSPACE.md"), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}

		// Load the built-in tool manifests so the embedded built-in agents'
		// spec.tools references resolve without "references missing tool"
		// warnings.
		ws, err := Open(ctx, dir, WithBuiltinFS(builtinToolManifests(builtinToolNames)))
		if err != nil {
			t.Fatalf("Open error: %v", err)
		}
		if got := ws.Catalog().Warnings(); len(got) != 0 {
			t.Errorf("expected zero warnings for a valid workspace, got %v", got)
		}
	})

	// 2. Invalid defaultAgent reference: Open succeeds and the message is surfaced.
	t.Run("InvalidDefaultAgent", func(t *testing.T) {
		dir := t.TempDir()
		manifest := `---
apiVersion: warp/v1alpha1
kind: Workspace
metadata:
  name: bad-agent-ws
spec:
  defaultAgent: non-existent-agent
---
`
		if err := os.WriteFile(filepath.Join(dir, "WORKSPACE.md"), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}

		ws, err := Open(ctx, dir)
		if err != nil {
			t.Fatalf("Open should not fail on an invalid defaultAgent, got: %v", err)
		}
		if got := ws.Catalog().Warnings(); !warnContains(got, "missing agent") {
			t.Errorf("expected a warning about the missing defaultAgent, got %v", got)
		}
	})

	// 3. Invalid defaultProvider reference (with a provider present): Open succeeds
	// and the message is surfaced.
	t.Run("InvalidDefaultProvider", func(t *testing.T) {
		dir := t.TempDir()
		manifest := `---
apiVersion: warp/v1alpha1
kind: Workspace
metadata:
  name: bad-provider-ws
spec:
  defaultAgent: main
  defaultProvider: missing-provider
---
`
		if err := os.WriteFile(filepath.Join(dir, "WORKSPACE.md"), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}

		provDir := filepath.Join(dir, ".agents", "providers")
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
  models:
    - id: gpt-5.5
      name: gpt-5.5
      capabilities:
        completion: true
`
		if err := os.WriteFile(filepath.Join(provDir, "openai.yaml"), []byte(openaiProvider), 0o644); err != nil {
			t.Fatal(err)
		}

		ws, err := Open(ctx, dir)
		if err != nil {
			t.Fatalf("Open should not fail on an invalid defaultProvider, got: %v", err)
		}
		if got := ws.Catalog().Warnings(); !warnContains(got, "missing provider") {
			t.Errorf("expected a warning about the missing defaultProvider, got %v", got)
		}
	})

	// 4. Invalid modelAliases (empty target): Open succeeds and the message is surfaced.
	t.Run("InvalidModelAliases", func(t *testing.T) {
		dir := t.TempDir()
		manifest := `---
apiVersion: warp/v1alpha1
kind: Workspace
metadata:
  name: bad-alias-ws
spec:
  modelAliases:
    fast: "   "
---
`
		if err := os.WriteFile(filepath.Join(dir, "WORKSPACE.md"), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}

		ws, err := Open(ctx, dir)
		if err != nil {
			t.Fatalf("Open should not fail on invalid modelAliases, got: %v", err)
		}
		if got := ws.Catalog().Warnings(); !warnContains(got, "modelAliases") {
			t.Errorf("expected a warning about invalid modelAliases, got %v", got)
		}
	})

	// 5. Warnings() returns a defensive copy: mutating the returned slice must not
	// affect the catalog's internal state.
	t.Run("WarningsDefensiveCopy", func(t *testing.T) {
		dir := t.TempDir()
		manifest := `---
apiVersion: warp/v1alpha1
kind: Workspace
metadata:
  name: copy-ws
spec:
  defaultAgent: non-existent-agent
---
`
		if err := os.WriteFile(filepath.Join(dir, "WORKSPACE.md"), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}

		ws, err := Open(ctx, dir)
		if err != nil {
			t.Fatalf("Open error: %v", err)
		}

		first := ws.Catalog().Warnings()
		if len(first) == 0 {
			t.Fatalf("expected at least one validation warning")
		}
		original := first[0]
		first[0] = "MUTATED"

		second := ws.Catalog().Warnings()
		if len(second) != len(first) {
			t.Fatalf("expected %d warnings on second call, got %d", len(first), len(second))
		}
		if second[0] == "MUTATED" {
			t.Errorf("mutating the returned Warnings() slice affected internal state: %q", second[0])
		}
		if second[0] != original {
			t.Errorf("expected second Warnings()[0] = %q, got %q", original, second[0])
		}
	})

	// 6. An agent that resolves to more than MaxRecommendedEagerTools eager tools
	// produces a diagnostic warning advising the user to rely on deferred loading.
	t.Run("TooManyEagerToolsWarning", func(t *testing.T) {
		dir := t.TempDir()
		manifest := `---
apiVersion: warp/v1alpha1
kind: Workspace
metadata:
  name: many-tools-ws
spec:
  defaultAgent: big
---
`
		if err := os.WriteFile(filepath.Join(dir, "WORKSPACE.md"), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}

		// Inject 25 system-namespace tools so the agent resolves to more than
		// MaxRecommendedEagerTools (20) eager tools.
		mfs := builtinToolManifests(builtinToolNames)
		for i := range 25 {
			name := fmt.Sprintf("tool%02d", i)
			mfs["tools/"+name+".tool.md"] = &fstest.MapFile{Data: []byte(fmt.Sprintf(`---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: %s
  description: Builtin %s tool
spec:
  annotations:
    isReadOnly: true
---
Builtin %s tool.
`, name, name, name))}
		}

		agentsDir := filepath.Join(dir, ".agents")
		if err := os.MkdirAll(agentsDir, 0o755); err != nil {
			t.Fatal(err)
		}
		bigAgent := `---
apiVersion: warp/v1alpha1
kind: Agent
metadata:
  name: big
  description: Agent with many eager tools
spec:
  tools: ["*"]
---
Big agent instructions.
`
		if err := os.WriteFile(filepath.Join(agentsDir, "big.agent.md"), []byte(bigAgent), 0o644); err != nil {
			t.Fatal(err)
		}

		ws, err := Open(ctx, dir, WithBuiltinFS(mfs))
		if err != nil {
			t.Fatalf("Open error: %v", err)
		}
		if got := ws.Catalog().Warnings(); !warnContains(got, "consider reducing spec.tools to use deferred loading") {
			t.Errorf("expected a too-many-eager-tools warning, got %v", got)
		}
	})
}

// TestModelAliasesResolution verifies that workspace.spec.modelAliases and
// provider/@alias shorthands resolve through ws.Catalog().ResolveModels as well
// as through models.Catalog.Get and models.Catalog.Resolve.
func TestModelAliasesResolution(t *testing.T) {
	setupTestXDG(t)
	ctx := context.Background()

	wsDir := t.TempDir()
	wsManifest := `---
apiVersion: warp/v1alpha1
kind: Workspace
metadata:
  name: alias-ws
spec:
  defaultProvider: openai
  modelAliases:
    fast: openai/gpt-5.5
    default: openai/gpt-5.5
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
  models:
    - id: gpt-5.5
      name: gpt-5.5
      aliases:
        - fast
      capabilities:
        completion: true
`
	if err := os.WriteFile(filepath.Join(provDir, "openai.yaml"), []byte(openaiProvider), 0o644); err != nil {
		t.Fatal(err)
	}

	ws, err := Open(ctx, wsDir)
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}

	// 1. ws.Catalog().ResolveModels resolves workspace.spec.modelAliases and
	// provider/@alias shorthands to the canonical provider/model-id qualified ID.
	for _, ref := range []string{"@fast", "@default", "openai/@fast"} {
		resolved := ws.Catalog().ResolveModels(warp.ModelRoleCompletion, ref)
		if len(resolved) != 1 || resolved[0].QualifiedID != "openai/gpt-5.5" {
			t.Errorf("ResolveModels(%q) = %v, want [openai/gpt-5.5]", ref, resolved)
		}
	}

	// 2. models.Catalog.Get and Resolve delegate alias resolution to warp.
	mc, err := models.Build(ws.Catalog(), nil)
	if err != nil {
		t.Fatalf("models.Build error: %v", err)
	}
	for _, ref := range []string{"@fast", "@default", "openai/@fast"} {
		m, ok := mc.Get(models.ID(ref))
		if !ok || m.ID != models.ID("openai/gpt-5.5") {
			t.Errorf("models.Catalog.Get(%q) = %s, %v; want openai/gpt-5.5", ref, m.ID, ok)
		}
	}
	if m, ok := mc.Resolve(models.RoleCompletion, "@fast"); !ok || m.ID != models.ID("openai/gpt-5.5") {
		t.Errorf("models.Catalog.Resolve(@fast) = %s, %v; want openai/gpt-5.5", m.ID, ok)
	}
}

// TestModelCatalogUnknownBareID verifies that a workspace with a defaultProvider
// does not synthesize an unlisted model from an unknown bare ID, while declared
// models and aliases still canonicalize to the provider/model-id.
func TestModelCatalogUnknownBareID(t *testing.T) {
	setupTestXDG(t)
	ctx := context.Background()

	wsDir := t.TempDir()
	wsManifest := `---
apiVersion: warp/v1alpha1
kind: Workspace
metadata:
  name: unknown-bare-ws
spec:
  defaultProvider: openai
  modelAliases:
    fast: openai/gpt-5.5
    default: openai/gpt-5.5
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
  models:
    - id: gpt-5.5
      name: gpt-5.5
      aliases:
        - fast
      capabilities:
        completion: true
`
	if err := os.WriteFile(filepath.Join(provDir, "openai.yaml"), []byte(openaiProvider), 0o644); err != nil {
		t.Fatal(err)
	}

	ws, err := Open(ctx, wsDir)
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}

	mc, err := models.Build(ws.Catalog(), nil)
	if err != nil {
		t.Fatalf("models.Build error: %v", err)
	}

	// An unknown bare ID must not be synthesized from defaultProvider.
	if _, ok := mc.Get(models.ID("unknown-bare-id")); ok {
		t.Errorf("Get(unknown-bare-id) should not resolve to a synthesized model")
	}

	// Aliases and a declared model still canonicalize to the provider/model-id.
	for _, ref := range []string{"@fast", "@default", "gpt-5.5"} {
		m, ok := mc.Get(models.ID(ref))
		if !ok {
			t.Errorf("Get(%q) not found", ref)
			continue
		}
		if m.ID != models.ID("openai/gpt-5.5") {
			t.Errorf("Get(%q) = %s, want openai/gpt-5.5", ref, m.ID)
		}
	}
}

// TestInitializeModelAliases verifies that InitOptions.ModelAliases is scaffolded
// into WORKSPACE.md and resolves end-to-end after opening the workspace.
func TestInitializeModelAliases(t *testing.T) {
	setupTestXDG(t)
	ctx := context.Background()

	dir := t.TempDir()
	opts := InitOptions{
		ProjectName:  "alias-init-ws",
		Provider:     "openai",
		DefaultAgent: "main",
		ModelAliases: map[string]string{
			"fast":    "openai/gpt-5.5",
			"default": "openai/gpt-5.5",
		},
	}
	if err := Initialize(ctx, dir, opts); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	wsBytes, err := os.ReadFile(filepath.Join(dir, "WORKSPACE.md"))
	if err != nil {
		t.Fatalf("read WORKSPACE.md: %v", err)
	}
	if !strings.Contains(string(wsBytes), "modelAliases") {
		t.Errorf("WORKSPACE.md missing modelAliases: %s", string(wsBytes))
	}

	ws, err := Open(ctx, dir)
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	mc, err := models.Build(ws.Catalog(), nil)
	if err != nil {
		t.Fatalf("models.Build error: %v", err)
	}
	if m, ok := mc.Get(models.ID("@fast")); !ok || m.ID != models.ID("openai/gpt-5.5") {
		t.Errorf("Get(@fast) = %s, %v; want openai/gpt-5.5", m.ID, ok)
	}
}

// TestCatalogProjects verifies Catalog.Projects() returns all mapped projects
// sorted by name and that Catalog.Project() resolves the cwd-scoped project
// from the same set.
func TestCatalogProjects(t *testing.T) {
	setupTestXDG(t)
	ctx := context.Background()

	rootDir := t.TempDir()
	subProjDir := filepath.Join(rootDir, "packages", "service")
	if err := os.MkdirAll(subProjDir, 0o755); err != nil {
		t.Fatal(err)
	}

	wsManifest := `---
apiVersion: warp/v1alpha1
kind: Workspace
metadata:
  name: multi-project-ws
spec:
  projects: [".", "packages/service"]
---
Workspace root instructions.
`
	if err := os.WriteFile(filepath.Join(rootDir, "WORKSPACE.md"), []byte(wsManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	ws, err := Open(ctx, rootDir)
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}

	projects := ws.Catalog().Projects()
	if len(projects) < 2 {
		t.Fatalf("expected at least 2 mapped projects, got %d: %+v", len(projects), projects)
	}

	// Projects must be returned sorted by name.
	for i := 1; i < len(projects); i++ {
		if projects[i-1].Name > projects[i].Name {
			t.Errorf("Projects() not sorted by name: %q precedes %q", projects[i].Name, projects[i-1].Name)
		}
	}

	// The cwd-scoped project must be resolvable and present in the returned set.
	current := ws.Catalog().Project()
	if current == nil {
		t.Fatalf("expected Project() to resolve the cwd-scoped project")
	}
	found := false
	for _, p := range projects {
		if p.Name == current.Name {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("cwd-scoped project %q not found in Projects()", current.Name)
	}
}

// TestCatalogPlugins verifies Catalog.Plugins() returns a deep-copied slice of
// the plugins declared in WORKSPACE.md and returns nil when the catalog has no
// registry or no plugins are declared.
func TestCatalogPlugins(t *testing.T) {
	// A catalog with no registry yields nil projects and nil plugins.
	empty := NewCatalog(nil, "", "")
	if got := empty.Projects(); got != nil {
		t.Errorf("expected nil Projects() for registry-less catalog, got %+v", got)
	}
	if got := empty.Plugins(); got != nil {
		t.Errorf("expected nil Plugins() for registry-less catalog, got %+v", got)
	}

	// A registry whose workspace declares no plugins yields nil plugins.
	emptyWS := &warp.Workspace{
		Def: &warp.WorkspaceDef{
			BaseResource: warp.BaseResource{
				APIVersion: warp.APIVersion,
				Kind:       warp.KindWorkspace,
				Metadata:   warp.Metadata{Name: "no-plugin-ws"},
			},
		},
	}
	if got := NewCatalog(warp.NewRegistry(emptyWS), "", "").Plugins(); got != nil {
		t.Errorf("expected nil Plugins() when no plugins declared, got %+v", got)
	}

	// A registry whose workspace declares plugins yields a deep copy.
	ws := &warp.Workspace{
		Def: &warp.WorkspaceDef{
			BaseResource: warp.BaseResource{
				APIVersion: warp.APIVersion,
				Kind:       warp.KindWorkspace,
				Metadata:   warp.Metadata{Name: "plugin-ws"},
			},
			Spec: warp.WorkspaceDefSpec{
				Plugins: []warp.WorkspacePlugin{
					{
						Source:    "github.com/example/plugin",
						Version:   "v1.2.3",
						Namespace: "example",
						Imports: &warp.ResourceFilter{
							Include: []string{"agents/*"},
							Exclude: []string{"agents/secret/*"},
						},
					},
				},
			},
		},
	}
	cat := NewCatalog(warp.NewRegistry(ws), "", "")

	plugins := cat.Plugins()
	if len(plugins) != 1 {
		t.Fatalf("expected 1 declared plugin, got %d: %+v", len(plugins), plugins)
	}
	if plugins[0].Source != "github.com/example/plugin" || plugins[0].Version != "v1.2.3" || plugins[0].Namespace != "example" {
		t.Errorf("unexpected plugin fields: %+v", plugins[0])
	}
	if plugins[0].Imports == nil || len(plugins[0].Imports.Include) != 1 || plugins[0].Imports.Include[0] != "agents/*" {
		t.Errorf("unexpected plugin imports: %+v", plugins[0].Imports)
	}

	// Mutating the returned slice must not affect the catalog's internal state.
	plugins[0].Source = "MUTATED"
	plugins[0].Imports.Include[0] = "MUTATED"
	again := cat.Plugins()
	if again[0].Source == "MUTATED" || again[0].Imports.Include[0] == "MUTATED" {
		t.Errorf("mutating the returned Plugins() slice affected internal state: %+v", again)
	}
}

func TestParseThinkingAdaptive(t *testing.T) {
	got := ParseThinking(&warp.AgentThinking{Type: warp.ThinkingStrategyAdaptive})
	if got == nil {
		t.Fatal("ParseThinking(adaptive) returned nil, want non-nil")
	}
	if got.Type != "adaptive" {
		t.Errorf("ParseThinking(adaptive).Type = %q, want %q", got.Type, "adaptive")
	}
	if !got.Enabled {
		t.Errorf("ParseThinking(adaptive).Enabled = false, want true")
	}
}

func TestDenyAllAgentResolvesZeroTools(t *testing.T) {
	setupTestXDG(t)
	ctx := context.Background()

	// Inject a builtin tool manifest so the catalog has a tool to deny.
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

	dir := t.TempDir()
	wsManifest := `---
apiVersion: warp/v1alpha1
kind: Workspace
metadata:
  name: deny-all-ws
---
Workspace instructions.
`
	if err := os.WriteFile(filepath.Join(dir, "WORKSPACE.md"), []byte(wsManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	agentsDir := filepath.Join(dir, ".agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// deny-all agent: include a non-existent tool so no tool matches the allowlist.
	denyAllAgent := `---
apiVersion: warp/v1alpha1
kind: Agent
metadata:
  name: deny-all
  description: Agent with no tools
spec:
  tools: ["*"]
  policies:
    tools:
      include: ["nonexistent-tool"]
---
Deny-all agent instructions.
`
	if err := os.WriteFile(filepath.Join(agentsDir, "deny-all.agent.md"), []byte(denyAllAgent), 0o644); err != nil {
		t.Fatal(err)
	}

	// full-access agent: no policies, so it should keep every workspace tool.
	fullAccessAgent := `---
apiVersion: warp/v1alpha1
kind: Agent
metadata:
  name: full-access
  description: Agent with all tools
spec:
  tools: ["*"]
---
Full-access agent instructions.
`
	if err := os.WriteFile(filepath.Join(agentsDir, "full-access.agent.md"), []byte(fullAccessAgent), 0o644); err != nil {
		t.Fatal(err)
	}

	ws, err := Open(ctx, dir, WithBuiltinFS(injectedFS))
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}

	// The catalog must expose the injected tool.
	if _, ok := ws.Catalog().GetTool("bash"); !ok {
		t.Fatal("expected injected bash tool to be resolved in catalog")
	}

	// The deny-all agent must resolve to zero tools (fail-closed allowlist).
	resolved, err := ws.Catalog().ResolveAgent("deny-all")
	if err != nil {
		t.Fatalf("ResolveAgent(deny-all) error: %v", err)
	}
	if len(resolved.Tools) != 0 {
		t.Fatalf("expected deny-all agent to resolve to 0 tools, got %d", len(resolved.Tools))
	}

	// The context breakdown must report zero tool tokens for the deny-all agent.
	breakdown := ws.Catalog().AgentContext("deny-all")
	if total := breakdown.SystemTools + breakdown.CustomTools + breakdown.MCPTools; total != 0 {
		t.Errorf("expected 0 tool tokens for deny-all agent, got system=%d custom=%d mcp=%d",
			breakdown.SystemTools, breakdown.CustomTools, breakdown.MCPTools)
	}

	// Regression: an agent with no policies still resolves to every workspace tool.
	fullResolved, err := ws.Catalog().ResolveAgent("full-access")
	if err != nil {
		t.Fatalf("ResolveAgent(full-access) error: %v", err)
	}
	if len(fullResolved.Tools) == 0 {
		t.Error("expected full-access agent to resolve to all workspace tools, got 0")
	}
}

// TestCatalogToolsForSkill verifies that api.Skill.Tools is populated from
// spec.tools and that Catalog.ToolsForSkill resolves a skill's declared tools
// even when they are absent from the agent's ambient spec.tools, while strictly
// enforcing the workspace and agent tool policy ceilings.
func TestCatalogToolsForSkill(t *testing.T) {
	// Workspace policy ceiling denies all dangerous tools.
	ws := &warp.Workspace{
		Def: &warp.WorkspaceDef{
			BaseResource: warp.BaseResource{
				APIVersion: warp.APIVersion,
				Kind:       warp.KindWorkspace,
				Metadata:   warp.Metadata{Name: "skill-tools-ws"},
			},
			Spec: warp.WorkspaceDefSpec{
				DefaultAgent: "main",
				Policies: &warp.Policies{
					Tools: &warp.ToolPolicies{Dangerous: warp.ToolPolicyDeny},
				},
			},
		},
	}

	mkTool := func(name string, ann *warp.ToolAnnotation) *warp.Tool {
		return &warp.Tool{
			BaseResource: warp.BaseResource{
				APIVersion: warp.APIVersion,
				Kind:       warp.KindTool,
				Namespace:  warp.NamespaceWorkspace,
				Metadata:   warp.Metadata{Name: name},
			},
			Spec: warp.ToolSpec{Annotations: ann},
		}
	}
	readFile := mkTool("read-file", &warp.ToolAnnotation{IsReadOnly: true})
	writeFile := mkTool("write-file", nil)
	bashTool := mkTool("bash", &warp.ToolAnnotation{IsDangerous: true})
	webSearch := mkTool("web-search", &warp.ToolAnnotation{IsOpenWorld: true})

	// The skill declares all four tools; none are ambient on the agent.
	attachedSkill := &warp.Skill{
		BaseResource: warp.BaseResource{
			APIVersion: warp.APIVersion,
			Kind:       warp.KindSkill,
			Namespace:  warp.NamespaceWorkspace,
			Metadata:   warp.Metadata{Name: "data-analyst"},
		},
		Spec: warp.SkillSpec{Tools: []string{"read-file", "write-file", "bash", "web-search"}},
	}
	// orphanSkill is not attached to any agent.
	orphanSkill := &warp.Skill{
		BaseResource: warp.BaseResource{
			APIVersion: warp.APIVersion,
			Kind:       warp.KindSkill,
			Namespace:  warp.NamespaceWorkspace,
			Metadata:   warp.Metadata{Name: "orphan"},
		},
		Spec: warp.SkillSpec{Tools: []string{"read-file"}},
	}

	// Agent "main" attaches data-analyst and further restricts via policies.tools:
	//   readOnly: true    -> denies write-file (not read-only)
	//   openWorld: deny   -> denies web-search
	//   exclude: [write-file]
	// The workspace policy denies bash (dangerous: deny).
	mainAgent := &warp.Agent{
		BaseResource: warp.BaseResource{
			APIVersion: warp.APIVersion,
			Kind:       warp.KindAgent,
			Namespace:  warp.NamespaceWorkspace,
			Metadata:   warp.Metadata{Name: "main"},
		},
		Spec: warp.AgentSpec{
			Skills: []string{"data-analyst"},
			Policies: &warp.Policies{
				Tools: &warp.ToolPolicies{
					ReadOnly:  true,
					OpenWorld: warp.ToolPolicyDeny,
					Exclude:   []string{"write-file"},
				},
			},
		},
	}

	reg := warp.NewRegistry(ws)
	for _, res := range []warp.Resource{readFile, writeFile, bashTool, webSearch, attachedSkill, orphanSkill, mainAgent} {
		reg.Set(res.QualifiedName(), res)
	}
	if err := reg.Validate(); err != nil {
		t.Fatalf("registry validation failed: %v", err)
	}

	cat := NewCatalog(reg, "", "")

	// 1. api.Skill.Tools is populated from spec.tools in Skills() and GetSkill().
	wantTools := []string{"read-file", "write-file", "bash", "web-search"}
	skills := cat.Skills()
	var dataAnalyst *SkillSnapshot
	for i := range skills {
		if skills[i].Name == "data-analyst" {
			dataAnalyst = &skills[i]
		}
	}
	if dataAnalyst == nil {
		t.Fatal("expected data-analyst skill in Skills()")
	}
	if !slices.Equal(dataAnalyst.Tools, wantTools) {
		t.Errorf("Skills() data-analyst Tools = %v, want %v", dataAnalyst.Tools, wantTools)
	}

	got, ok := cat.GetSkill("data-analyst")
	if !ok {
		t.Fatal("GetSkill(data-analyst) not found")
	}
	if !slices.Equal(got.Tools, wantTools) {
		t.Errorf("GetSkill(data-analyst).Tools = %v, want %v", got.Tools, wantTools)
	}

	// 2. ToolsForSkill resolves the skill's declared tools even though none are
	//    ambient on the agent, but strictly filters out tools denied by the
	//    workspace or agent policy ceiling.
	tools, err := cat.ToolsForSkill("main", "data-analyst")
	if err != nil {
		t.Fatalf("ToolsForSkill(main, data-analyst) error: %v", err)
	}
	gotNames := toolNames(t, tools)
	if !slices.Equal(gotNames, []string{"read-file"}) {
		t.Errorf("ToolsForSkill(main, data-analyst) = %v, want [read-file] "+
			"(write-file denied by readOnly/exclude, bash denied by workspace dangerous:deny, web-search denied by agent openWorld:deny)", gotNames)
	}

	// 2b. The workspace default agent is used when agentName is empty.
	if _, err := cat.ToolsForSkill("", "data-analyst"); err != nil {
		t.Errorf("ToolsForSkill(\"\", data-analyst) using default agent error: %v", err)
	}

	// 3. ToolsForSkill returns an error when the skill is not enabled for the agent.
	if _, err := cat.ToolsForSkill("main", "orphan"); err == nil {
		t.Error("expected error when requesting tools for a skill not enabled for the agent, got nil")
	}

	// 4. A registry-less catalog guards against a nil resolver.
	if _, err := NewCatalog(nil, "", "").ToolsForSkill("main", "data-analyst"); err == nil {
		t.Error("expected error from registry-less catalog ToolsForSkill, got nil")
	}
}

// toolNames returns the metadata names of tools sorted deterministically.
func toolNames(t *testing.T, tools []*warp.Tool) []string {
	t.Helper()
	names := make([]string, 0, len(tools))
	for _, tl := range tools {
		names = append(names, tl.Metadata.Name)
	}
	slices.Sort(names)
	return names
}

// TestCatalogResolveCommand verifies that api.Command exposes the full command
// schema (Agent, Context, Skills, Rules, Models, Tools) in Catalog.Commands()
// and Catalog.GetCommand(), and that Catalog.ResolveCommand resolves the
// executing agent, eagerly resolved skills and rules, and policy-filtered
// effective tools (including tools declared by the command's skills), while
// rejecting commands not enabled for the active agent unless the command
// declares an explicit spec.agent override.
func TestCatalogResolveCommand(t *testing.T) {
	// Workspace policy ceiling denies all dangerous tools.
	ws := &warp.Workspace{
		Def: &warp.WorkspaceDef{
			BaseResource: warp.BaseResource{
				APIVersion: warp.APIVersion,
				Kind:       warp.KindWorkspace,
				Metadata:   warp.Metadata{Name: "resolve-cmd-ws"},
			},
			Spec: warp.WorkspaceDefSpec{
				DefaultAgent: "strict-agent",
				Policies: &warp.Policies{
					Tools: &warp.ToolPolicies{Dangerous: warp.ToolPolicyDeny},
				},
			},
		},
	}

	mkTool := func(name string, ann *warp.ToolAnnotation) *warp.Tool {
		return &warp.Tool{
			BaseResource: warp.BaseResource{
				APIVersion: warp.APIVersion,
				Kind:       warp.KindTool,
				Namespace:  warp.NamespaceWorkspace,
				Metadata:   warp.Metadata{Name: name},
			},
			Spec: warp.ToolSpec{Annotations: ann},
		}
	}
	toolCmd := mkTool("tool-cmd", &warp.ToolAnnotation{IsReadOnly: true})
	toolSkill := mkTool("tool-skill", &warp.ToolAnnotation{IsReadOnly: true})
	toolDangerous := mkTool("tool-dangerous", &warp.ToolAnnotation{IsDangerous: true})
	toolExcluded := mkTool("tool-excluded", nil)
	toolUnlisted := mkTool("tool-unlisted", nil)

	// doc-skill declares three tools: one unlocked via the command (tool-skill),
	// one denied by the workspace policy (tool-dangerous), and one not in the
	// agent's include allowlist (tool-unlisted).
	docSkill := &warp.Skill{
		BaseResource: warp.BaseResource{
			APIVersion: warp.APIVersion,
			Kind:       warp.KindSkill,
			Namespace:  warp.NamespaceWorkspace,
			Metadata:   warp.Metadata{Name: "doc-skill"},
		},
		Spec: warp.SkillSpec{Tools: []string{"tool-skill", "tool-dangerous", "tool-unlisted"}},
	}

	mkRule := func(name string, priority int) *warp.Rule {
		return &warp.Rule{
			BaseResource: warp.BaseResource{
				APIVersion: warp.APIVersion,
				Kind:       warp.KindRule,
				Namespace:  warp.NamespaceWorkspace,
				Metadata:   warp.Metadata{Name: name},
			},
			Spec: warp.RuleSpec{Priority: priority, Triggers: warp.RuleTriggers{Always: false}},
		}
	}
	ruleLow := mkRule("rule-low", 10)
	ruleHigh := mkRule("rule-high", 100)

	// strict-agent enables agent-cmd and restricts tools via its policy ceiling:
	//   include: [tool-cmd, tool-skill] -\u003e only these pass
	//   exclude: [tool-excluded]
	//   dangerous: deny
	strictAgent := &warp.Agent{
		BaseResource: warp.BaseResource{
			APIVersion: warp.APIVersion,
			Kind:       warp.KindAgent,
			Namespace:  warp.NamespaceWorkspace,
			Metadata:   warp.Metadata{Name: "strict-agent"},
		},
		Spec: warp.AgentSpec{
			Commands: []string{"agent-cmd"},
			Policies: &warp.Policies{
				Tools: &warp.ToolPolicies{
					Include:   []string{"tool-cmd", "tool-skill"},
					Exclude:   []string{"tool-excluded"},
					Dangerous: warp.ToolPolicyDeny,
				},
			},
		},
	}

	// explicit-cmd declares an explicit agent override, so it resolves even
	// though it is not listed in strict-agent's spec.commands.
	explicitCmd := &warp.Command{
		BaseResource: warp.BaseResource{
			APIVersion: warp.APIVersion,
			Kind:       warp.KindCommand,
			Namespace:  warp.NamespaceWorkspace,
			Metadata:   warp.Metadata{Name: "explicit-cmd"},
		},
		Spec: warp.CommandSpec{
			Agent:   "strict-agent",
			Context: warp.CommandContextFork,
			Models:  []string{"gpt-5.5"},
			Skills:  []string{"doc-skill"},
			Rules:   []string{"rule-low", "rule-high"},
			Tools:   []string{"tool-cmd", "tool-excluded", "tool-unlisted"},
		},
	}

	// agent-cmd is enabled for strict-agent via its spec.commands list and
	// declares no explicit agent override.
	agentCmd := &warp.Command{
		BaseResource: warp.BaseResource{
			APIVersion: warp.APIVersion,
			Kind:       warp.KindCommand,
			Namespace:  warp.NamespaceWorkspace,
			Metadata:   warp.Metadata{Name: "agent-cmd"},
		},
		Spec: warp.CommandSpec{
			Skills: []string{"doc-skill"},
			Tools:  []string{"tool-cmd"},
		},
	}

	// unenabled-cmd is not enabled for any agent and declares no agent override.
	unenabledCmd := &warp.Command{
		BaseResource: warp.BaseResource{
			APIVersion: warp.APIVersion,
			Kind:       warp.KindCommand,
			Namespace:  warp.NamespaceWorkspace,
			Metadata:   warp.Metadata{Name: "unenabled-cmd"},
		},
	}

	reg := warp.NewRegistry(ws)
	for _, res := range []warp.Resource{
		toolCmd, toolSkill, toolDangerous, toolExcluded, toolUnlisted,
		docSkill, ruleLow, ruleHigh, strictAgent,
		explicitCmd, agentCmd, unenabledCmd,
	} {
		reg.Set(res.QualifiedName(), res)
	}
	if err := reg.Validate(); err != nil {
		t.Fatalf("registry validation failed: %v", err)
	}

	cat := NewCatalog(reg, "", "")

	// 1. api.Command exposes the full schema in Commands() and GetCommand().
	commands := cat.Commands()
	var explicit *CommandSnapshot
	for i := range commands {
		if commands[i].Name == "explicit-cmd" {
			explicit = &commands[i]
		}
	}
	if explicit == nil {
		t.Fatal("expected explicit-cmd in Commands()")
	}
	if explicit.Agent != "strict-agent" {
		t.Errorf("Commands() explicit-cmd Agent = %q, want %q", explicit.Agent, "strict-agent")
	}
	if explicit.Context != string(warp.CommandContextFork) {
		t.Errorf("Commands() explicit-cmd Context = %q, want %q", explicit.Context, string(warp.CommandContextFork))
	}
	if !slices.Equal(explicit.Skills, []string{"doc-skill"}) {
		t.Errorf("Commands() explicit-cmd Skills = %v, want [doc-skill]", explicit.Skills)
	}
	if !slices.Equal(explicit.Rules, []string{"rule-low", "rule-high"}) {
		t.Errorf("Commands() explicit-cmd Rules = %v, want [rule-low rule-high]", explicit.Rules)
	}
	if !slices.Equal(explicit.Models, []string{"gpt-5.5"}) {
		t.Errorf("Commands() explicit-cmd Models = %v, want [gpt-5.5]", explicit.Models)
	}
	if !slices.Equal(explicit.Tools, []string{"tool-cmd", "tool-excluded", "tool-unlisted"}) {
		t.Errorf("Commands() explicit-cmd Tools = %v, want [tool-cmd tool-excluded tool-unlisted]", explicit.Tools)
	}

	got, ok := cat.GetCommand("explicit-cmd")
	if !ok {
		t.Fatal("GetCommand(explicit-cmd) not found")
	}
	if got.Agent != "strict-agent" || got.Context != string(warp.CommandContextFork) {
		t.Errorf("GetCommand(explicit-cmd) Agent/Context = %q/%q, want strict-agent/fork", got.Agent, got.Context)
	}
	if !slices.Equal(got.Skills, []string{"doc-skill"}) || !slices.Equal(got.Rules, []string{"rule-low", "rule-high"}) {
		t.Errorf("GetCommand(explicit-cmd) Skills/Rules = %v/%v, want [doc-skill]/[rule-low rule-high]", got.Skills, got.Rules)
	}
	if !slices.Equal(got.Models, []string{"gpt-5.5"}) || !slices.Equal(got.Tools, []string{"tool-cmd", "tool-excluded", "tool-unlisted"}) {
		t.Errorf("GetCommand(explicit-cmd) Models/Tools = %v/%v", got.Models, got.Tools)
	}

	// 2. ResolveCommand resolves the explicit-agent command (the override bypasses
	//    the enablement check) with its executing agent, eager skills/rules, and
	//    policy-filtered effective tools (including the skill's tool-skill).
	resCmd, err := cat.ResolveCommand("strict-agent", "explicit-cmd")
	if err != nil {
		t.Fatalf("ResolveCommand(strict-agent, explicit-cmd) error: %v", err)
	}
	if resCmd.Command == nil || resCmd.Command.Metadata.Name != "explicit-cmd" {
		t.Fatalf("expected resolved command explicit-cmd, got %+v", resCmd.Command)
	}
	if resCmd.Agent == nil || resCmd.Agent.Agent == nil || resCmd.Agent.Agent.Metadata.Name != "strict-agent" {
		t.Errorf("expected executing agent strict-agent, got %+v", resCmd.Agent)
	}
	if len(resCmd.Skills) != 1 || resCmd.Skills[0].Metadata.Name != "doc-skill" {
		t.Errorf("expected [doc-skill] eager skills, got %v", resCmd.Skills)
	}
	if len(resCmd.Rules) != 2 || resCmd.Rules[0].Metadata.Name != "rule-high" || resCmd.Rules[1].Metadata.Name != "rule-low" {
		t.Errorf("expected rules sorted [rule-high, rule-low], got %v", resCmd.Rules)
	}
	if !resCmd.Rules[0].Spec.Triggers.Always || !resCmd.Rules[1].Spec.Triggers.Always {
		t.Errorf("expected command eager rules to have Always=true, got %v, %v",
			resCmd.Rules[0].Spec.Triggers.Always, resCmd.Rules[1].Spec.Triggers.Always)
	}
	// Effective tools: tool-cmd + tool-skill (from doc-skill); tool-dangerous is
	// denied by the workspace dangerous:deny policy, tool-excluded is excluded by
	// the agent, and tool-unlisted is not in the agent include allowlist.
	if got := toolNames(t, resCmd.Tools); !slices.Equal(got, []string{"tool-cmd", "tool-skill"}) {
		t.Errorf("ResolveCommand effective tools = %v, want [tool-cmd tool-skill]", got)
	}

	// 3. ResolveCommand resolves an agent-enabled command with the invoking agent.
	agentResCmd, err := cat.ResolveCommand("strict-agent", "agent-cmd")
	if err != nil {
		t.Fatalf("ResolveCommand(strict-agent, agent-cmd) error: %v", err)
	}
	if agentResCmd.Agent == nil || agentResCmd.Agent.Agent == nil || agentResCmd.Agent.Agent.Metadata.Name != "strict-agent" {
		t.Errorf("expected executing agent strict-agent for agent-cmd, got %+v", agentResCmd.Agent)
	}

	// 4. ResolveCommand rejects a command not enabled for the agent when it
	//    declares no explicit agent override.
	if _, err := cat.ResolveCommand("strict-agent", "unenabled-cmd"); err == nil {
		t.Error("expected error resolving unenabled-cmd for strict-agent, got nil")
	} else if !strings.Contains(err.Error(), "is not enabled for agent") {
		t.Errorf("expected enablement error, got: %v", err)
	}

	// 5. A registry-less catalog guards against a nil resolver.
	if _, err := NewCatalog(nil, "", "").ResolveCommand("strict-agent", "explicit-cmd"); err == nil {
		t.Error("expected error from registry-less catalog ResolveCommand, got nil")
	}
}

// TestBuildDeferredToolsCatalog verifies that BuildDeferredToolsCatalog groups a
// resolved agent's deferred custom tools and deferred MCP tools into a compact
// directory, and that AgentRenderGlobals exposes HasTool("tool_search") and the
// DeferredTools directory.
func TestBuildDeferredToolsCatalog(t *testing.T) {
	setupTestXDG(t)
	ctx := context.Background()

	dir := t.TempDir()
	agentsDir := filepath.Join(dir, ".agents")
	if err := os.MkdirAll(filepath.Join(agentsDir, "tools"), 0o755); err != nil {
		t.Fatal(err)
	}

	// One eager custom tool, one deferred custom tool, and two deferred MCP tools.
	tools := []struct {
		file    string
		content string
	}{
		{
			"eager-tool.tool.md",
			`---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: eager-tool
  description: An eager tool
spec:
  command: ["sh", "-c"]
---
Eager tool.
`,
		},
		{
			"linter.tool.md",
			`---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: linter
  description: Run the linter
spec:
  command: ["golangci-lint", "run"]
---
Linter tool.
`,
		},
		{
			"gh-create.tool.md",
			`---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: mcp__github__create_issue
  description: Create a GitHub issue
spec:
  command: ["sh", "-c"]
---
Create issue.
`,
		},
		{
			"gh-list.tool.md",
			`---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: mcp__github__list_prs
  description: List GitHub pull requests
spec:
  command: ["sh", "-c"]
---
List PRs.
`,
		},
	}
	for _, tl := range tools {
		if err := os.WriteFile(filepath.Join(agentsDir, "tools", tl.file), []byte(tl.content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// The agent loads eager-tool eagerly and defers linter + the two MCP tools.
	agentContent := `---
apiVersion: warp/v1alpha1
kind: Agent
metadata:
  name: deferred-agent
spec:
  tools:
    - eager-tool
  policies:
    tools:
      include:
        - eager-tool
        - linter
        - mcp__github__create_issue
        - mcp__github__list_prs
---
Deferred agent.
`
	if err := os.WriteFile(filepath.Join(agentsDir, "deferred-agent.agent.md"), []byte(agentContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// The all-eager agent loads every custom tool eagerly and declares no MCP
	// tools, so nothing is deferred.
	allEagerContent := `---
apiVersion: warp/v1alpha1
kind: Agent
metadata:
  name: all-eager
spec:
  tools:
    - eager-tool
    - linter
  policies:
    tools:
      include:
        - eager-tool
        - linter
---
All-eager agent.
`
	if err := os.WriteFile(filepath.Join(agentsDir, "all-eager.agent.md"), []byte(allEagerContent), 0o644); err != nil {
		t.Fatal(err)
	}

	ws, err := Open(ctx, dir)
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	cat := ws.Catalog()

	resolved, err := cat.ResolveAgent("deferred-agent")
	if err != nil {
		t.Fatalf("ResolveAgent: %v", err)
	}

	// 1. BuildDeferredToolsCatalog groups linter under Tools and the two MCP
	//    tools under the github server.
	dec := cat.BuildDeferredToolsCatalog(resolved)
	if dec == nil {
		t.Fatal("expected non-nil DeferredToolsCatalog")
	}
	if len(dec.Tools) != 1 || dec.Tools[0].Name != "linter" || dec.Tools[0].Description != "Run the linter" {
		t.Errorf("unexpected deferred tools: %+v", dec.Tools)
	}
	if len(dec.MCPServers) != 1 {
		t.Fatalf("expected 1 MCP server summary, got %d: %+v", len(dec.MCPServers), dec.MCPServers)
	}
	gh := dec.MCPServers[0]
	if gh.Name != "github" || gh.Prefix != "mcp__github__*" {
		t.Errorf("unexpected MCP server summary: %+v", gh)
	}
	foundCreate, foundList := false, false
	for _, tn := range gh.Tools {
		switch tn {
		case "create_issue":
			foundCreate = true
		case "list_prs":
			foundList = true
		}
	}
	if !foundCreate || !foundList {
		t.Errorf("expected github server tools [create_issue list_prs], got %v", gh.Tools)
	}

	// 2. AgentRenderGlobals exposes HasTool("tool_search") (deferred tools exist)
	//    and the DeferredTools directory; deferred tools are not "has tool".
	globals := cat.AgentRenderGlobals(resolved)
	hasTool, ok := globals["HasTool"].(func(string) bool)
	if !ok {
		t.Fatalf("expected HasTool func, got %T", globals["HasTool"])
	}
	if !hasTool("tool_search") {
		t.Error("expected HasTool(tool_search) true when deferred tools exist")
	}
	if !hasTool("eager-tool") {
		t.Error("expected HasTool(eager-tool) true for eager tool")
	}
	if hasTool("linter") {
		t.Error("expected HasTool(linter) false for deferred tool")
	}
	if _, ok := globals["DeferredTools"].(*DeferredToolsCatalog); !ok {
		t.Errorf("expected DeferredTools *DeferredToolsCatalog, got %T", globals["DeferredTools"])
	}

	// 3. The all-eager agent has no deferred tools: BuildDeferredToolsCatalog is
	//    nil and HasTool("tool_search") is false.
	resolvedEager, err := cat.ResolveAgent("all-eager")
	if err != nil {
		t.Fatalf("ResolveAgent(all-eager): %v", err)
	}
	if dec := cat.BuildDeferredToolsCatalog(resolvedEager); dec != nil {
		t.Errorf("expected nil DeferredToolsCatalog for all-eager agent, got %+v", dec)
	}
	globalsEager := cat.AgentRenderGlobals(resolvedEager)
	if hasToolEager, ok := globalsEager["HasTool"].(func(string) bool); ok {
		if hasToolEager("tool_search") {
			t.Error("expected HasTool(tool_search) false when no deferred tools exist")
		}
	}
}
