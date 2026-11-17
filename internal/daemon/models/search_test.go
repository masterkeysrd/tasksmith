package models_test

import (
	"slices"
	"testing"

	"github.com/masterkeysrd/tasksmith/internal/daemon/models"
)

func TestRankMatchAndSuggestClosest(t *testing.T) {
	catalog := []models.Model{
		{
			ID:          "alpha/claude-haiku-4-5",
			Name:        "Claude Haiku 4.5",
			Description: "Low-latency coding assistant",
			Aliases:     []string{"fast"},
		},
		{
			ID:          "alpha/claude-sonnet-4-6",
			Name:        "Claude Sonnet 4.6",
			Label:       "Sonnet Frontier",
			Description: "Balanced coding and architecture model",
			Aliases:     []string{"balanced", "smart"},
		},
		{
			ID:          "ollama/qwen3.8",
			Name:        "Qwen 3.8 Coder",
			Description: "Local open-weights coding model",
			Aliases:     []string{"fast"},
		},
	}

	// AliasRefs normalizes bare aliases to @-prefixed references.
	if got := catalog[0].AliasRefs(); !slices.Equal(got, []string{"@fast"}) {
		t.Fatalf("AliasRefs = %v, want [@fast]", got)
	}
	if got := (models.Model{}).AliasRefs(); got != nil {
		t.Fatalf("empty AliasRefs = %v, want nil", got)
	}

	// Exact @alias match hits Tier 0 (score 0).
	if tier, score, ok := models.RankMatch("@fast", catalog[0]); !ok || tier != 0 || score != 0 {
		t.Fatalf("RankMatch(@fast) = (%d, %d, %v), want (0, 0, true)", tier, score, ok)
	}

	// Provider-scoped @alias ("ollama/@fast") matches ollama, not alpha.
	if tier, score, ok := models.RankMatch("ollama/@fast", catalog[2]); !ok || tier != 0 || score != 0 {
		t.Fatalf("RankMatch(ollama/@fast, ollama) = (%d, %d, %v), want (0, 0, true)", tier, score, ok)
	}
	if _, _, ok := models.RankMatch("ollama/@fast", catalog[0]); ok {
		t.Fatal("expected ollama/@fast not to match alpha/claude-haiku-4-5")
	}

	// Display Name / Label and Description substring matching work, while typos in description do not.
	if tier, _, ok := models.RankMatch("Sonnet Frontier", catalog[1]); !ok || tier != 0 {
		t.Fatalf("RankMatch(Label) = tier %d ok %v, want tier 0", tier, ok)
	}
	if tier, _, ok := models.RankMatch("open-weights", catalog[2]); !ok || tier != 1 {
		t.Fatalf("RankMatch(Description) = tier %d ok %v, want tier 1", tier, ok)
	}

	// Specific queries do not over-match short aliases via reverse containment.
	for _, m := range catalog {
		if _, _, ok := models.RankMatch("deepseek-fast-coder", m); ok {
			t.Fatalf("expected deepseek-fast-coder not to match %s", m.ID)
		}
	}

	// SuggestClosest handles typos and falls back to same-provider models when only provider matches.
	if got := models.SuggestClosest(catalog, "ollam/qwen3.8", 3); !slices.Equal(got, []string{"ollama/qwen3.8"}) {
		t.Fatalf("SuggestClosest(typo) = %v, want [ollama/qwen3.8]", got)
	}
	if got := models.SuggestClosest(catalog, "alpha/unknown-model-xyz", 3); len(got) != 2 || !slices.Contains(got, "alpha/claude-haiku-4-5") || !slices.Contains(got, "alpha/claude-sonnet-4-6") {
		t.Fatalf("SuggestClosest(provider fallback) = %v, want both alpha/* models", got)
	}
}
