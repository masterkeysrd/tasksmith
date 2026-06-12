package workspace

import (
	"testing"
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
		"lsp_diagnostics", "lsp_search", "lsp_restart",
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
