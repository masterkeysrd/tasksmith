package models

import "testing"

func TestIDProviderName(t *testing.T) {
	for _, tc := range []struct {
		id       ID
		provider string
		name     string
		valid    bool
	}{
		{ID("anthropic/claude-3"), "anthropic", "claude-3", true},
		{ID("openrouter/x-ai/grok-build-0.1"), "openrouter", "x-ai/grok-build-0.1", true},
		{ID("mlx-serve/ddalcu/Qwen3.8-27B-MLX-Serve-4bit"), "mlx-serve", "ddalcu/Qwen3.8-27B-MLX-Serve-4bit", true},
		{ID("claude-3"), "claude-3", "", false},
		{ID(""), "", "", false},
		{ID("/claude"), "", "claude", false},
		{ID("anthropic/"), "anthropic", "", false},
	} {
		if got := tc.id.Provider(); got != tc.provider {
			t.Errorf("ID(%q).Provider() = %q, want %q", tc.id, got, tc.provider)
		}
		if got := tc.id.Name(); got != tc.name {
			t.Errorf("ID(%q).Name() = %q, want %q", tc.id, got, tc.name)
		}
		if got := tc.id.Valid(); got != tc.valid {
			t.Errorf("ID(%q).Valid() = %v, want %v", tc.id, got, tc.valid)
		}
	}
}

func TestNormalize(t *testing.T) {
	for _, tc := range []struct {
		provider, model, want string
	}{
		{"anthropic", "claude-3", "anthropic/claude-3"},
		{"anthropic", "anthropic/claude-3", "anthropic/claude-3"},
		{"", "claude-3", "claude-3"},
		{"anthropic", "", "anthropic"},
		{"anthropic", "  claude-3  ", "anthropic/claude-3"},
		{"", "openrouter/x-ai/grok", "openrouter/x-ai/grok"},
	} {
		if got := Normalize(tc.provider, tc.model); string(got) != tc.want {
			t.Errorf("Normalize(%q, %q) = %q, want %q", tc.provider, tc.model, got, tc.want)
		}
	}
}
