package workspace

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
)

func TestWorkspace_Presets(t *testing.T) {
	w := New(".")

	providers := w.ProvidersPresets()
	if len(providers) == 0 {
		t.Error("Expected provider presets to be loaded, got 0")
	}

	foundAnthropic := false
	for _, p := range providers {
		if p.Metadata.Name == "anthropic" {
			foundAnthropic = true
			break
		}
	}
	if !foundAnthropic {
		t.Error("Anthropic provider preset not found")
	}

	tools := w.ToolsPresets()
	if len(tools) < 15 {
		t.Errorf("Expected at least 15 tool presets, got %d", len(tools))
	}

	expectedTools := []string{
		"ls", "view", "write", "grep", "glob", "remove", "edit",
		"fetch", "web_fetch", "web_search", "download",
		"bash",
		"lsp_diagnostics", "lsp_symbols", "lsp_restart",
		"mcp_read_resources",
	}

	for _, name := range expectedTools {
		found := false
		for _, tool := range tools {
			if tool.Metadata.Name == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Tool preset %s not found", name)
		}
	}
}

func TestWorkspace_Initialize(t *testing.T) {
	tempCWD := t.TempDir()
	w := New(tempCWD)

	tempConfigDir := t.TempDir()
	tempDataDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tempConfigDir)
	t.Setenv("XDG_DATA_HOME", tempDataDir)
	t.Setenv("TASKSMITH_APPNAME", "tasksmith-test")
	// Clear xdg cache
	xdg.ClearCache()
	defer xdg.ClearCache()

	opts := InitializationOptions{
		ProjectName:      "test-project",
		SelectedProvider: "openai",
		APIKey:           "sk-test-api-key",
		Endpoint:         "https://api.openai.com/v1",
		DefaultModel:     "gpt-5.5",
		Theme:            "tokyo-night",
		AuthorizedTools:  map[string]bool{"ls": true, "write": true},
	}

	err := w.Initialize(context.Background(), opts)
	if err != nil {
		t.Fatalf("unexpected error during Initialize: %v", err)
	}

	// 1. Verify sentinel setup.json
	wsDir, err := xdg.WorkspaceDir(tempCWD)
	if err != nil {
		t.Fatalf("failed to get workspace dir: %v", err)
	}
	sentinelPath := filepath.Join(wsDir, "setup.json")
	if _, err := os.Stat(sentinelPath); err != nil {
		t.Errorf("sentinel setup.json not created: %v", err)
	}

	// 2. Verify global theme.json
	cfgDir, err := xdg.SubConfigDir()
	if err != nil {
		t.Fatalf("failed to get config dir: %v", err)
	}
	cfgPath := filepath.Join(cfgDir, "theme.json")
	if _, err := os.Stat(cfgPath); err != nil {
		t.Errorf("global theme.json not created: %v", err)
	}
	cfgData, err := os.ReadFile(cfgPath)
	if err == nil {
		var config struct {
			Theme string `json:"theme"`
		}
		if err := json.Unmarshal(cfgData, &config); err != nil {
			t.Errorf("failed to unmarshal config json: %v", err)
		} else if config.Theme != "tokyo-night" {
			t.Errorf("expected theme tokyo-night, got %s", config.Theme)
		}
	}

	// 3. Verify WORKSPACE.md
	wsPath := filepath.Join(tempCWD, "WORKSPACE.md")
	if _, err := os.Stat(wsPath); err != nil {
		t.Errorf("WORKSPACE.md not created: %v", err)
	} else {
		wsData, _ := os.ReadFile(wsPath)
		wsContent := string(wsData)
		if !strings.Contains(wsContent, "policies:") {
			t.Errorf("WORKSPACE.md does not contain policies: %s", wsContent)
		}
		if !strings.Contains(wsContent, "ls") || !strings.Contains(wsContent, "write") {
			t.Errorf("WORKSPACE.md policies does not include authorized tools: %s", wsContent)
		}
	}

	// 4. Verify .agents/providers/openai.yaml
	providerPath := filepath.Join(tempCWD, ".agents", "providers", "openai.yaml")
	if _, err := os.Stat(providerPath); err != nil {
		t.Errorf("provider yaml not created: %v", err)
	}

	// 5. Verify .env and .gitignore
	envPath := filepath.Join(tempCWD, ".env")
	if _, err := os.Stat(envPath); err != nil {
		t.Errorf(".env file not created: %v", err)
	} else {
		envData, _ := os.ReadFile(envPath)
		if !strings.Contains(string(envData), "OPENAI_API_KEY=sk-test-api-key") {
			t.Errorf(".env does not contain API key: %s", string(envData))
		}
	}

	gitignorePath := filepath.Join(tempCWD, ".gitignore")
	if _, err := os.Stat(gitignorePath); err != nil {
		t.Errorf(".gitignore not created: %v", err)
	} else {
		ignoreData, _ := os.ReadFile(gitignorePath)
		if !strings.Contains(string(ignoreData), ".env") {
			t.Errorf(".gitignore does not contain .env: %s", string(ignoreData))
		}
	}

	// 6. Verify reloading workspace returns correct configurations (Preloading check)
	wReloaded := New(tempCWD)
	err = wReloaded.Load(context.Background())
	if err != nil {
		t.Fatalf("failed to reload initialized workspace: %v", err)
	}
	cfg, err := wReloaded.GetWorkspaceConfig(context.Background())
	if err != nil {
		t.Fatalf("failed to get workspace config: %v", err)
	}
	if !cfg.IsConfigured {
		t.Error("expected IsConfigured to be true")
	}
	if cfg.Name != "test-project" {
		t.Errorf("expected name test-project, got %s", cfg.Name)
	}
	if cfg.DefaultProvider != "openai" {
		t.Errorf("expected default provider openai, got %s", cfg.DefaultProvider)
	}
	if !cfg.AuthorizedTools["ls"] || !cfg.AuthorizedTools["write"] {
		t.Errorf("expected authorized tools to include ls and write, got %v", cfg.AuthorizedTools)
	}
}

func TestWorkspace_ResolveDefaults(t *testing.T) {
	w := New(".")
	agent, provider, model, err := w.ResolveDefaults(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if agent != "main" || provider != "ollama" || model != "qwen3.6:35b-a3b-coding-nvfp4" {
		t.Errorf("expected defaults, got %s, %s, %s", agent, provider, model)
	}

	w.SetAgentOverride("custom-agent")
	agent, _, _, err = w.ResolveDefaults(context.Background())
	if err != nil {
		t.Fatalf("expected no error resolving defaults with override, got %v", err)
	}
	if agent != "custom-agent" {
		t.Errorf("expected agent override to be custom-agent, got %s", agent)
	}
}

func TestWorkspace_Contexts(t *testing.T) {
	w := New("../..")
	err := w.Load(context.Background())
	if err != nil {
		t.Fatalf("failed to load workspace: %v", err)
	}

	contexts := w.Contexts()
	if len(contexts) == 0 {
		t.Error("Expected at least one Context resource, got 0")
	}
	foundLocal := false
	for _, c := range contexts {
		if strings.Contains(c.Spec.Instructions, "TaskSmith") {
			foundLocal = true
		}
	}
	if !foundLocal {
		t.Error("Expected local AGENT.md context to be loaded")
	}
}

func TestWorkspace_SyntheticResolveAgentTools(t *testing.T) {
	tempCWD := t.TempDir()
	w := New(tempCWD)
	err := w.Load(context.Background())
	if err != nil {
		t.Fatalf("failed to load workspace: %v", err)
	}

	resolved, err := w.ResolveAgent(context.Background(), "main")
	if err != nil {
		t.Fatalf("failed to resolve main agent: %v", err)
	}

	if resolved == nil {
		t.Fatal("expected resolved agent to be non-nil")
	}

	if len(resolved.Tools) == 0 {
		t.Error("expected resolved agent in synthetic workspace to have tools loaded, got 0")
	}

	foundBash := false
	for _, tool := range resolved.Tools {
		if tool.Metadata.Name == "bash" {
			foundBash = true
			break
		}
	}
	if !foundBash {
		t.Error("expected resolved agent to include 'bash' tool")
	}
}
