package strutils_test

import (
	"slices"
	"testing"

	"github.com/masterkeysrd/tasksmith/internal/core/strutils"
)

func TestLevenshtein(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"abc", "", 3},
		{"", "abc", 3},
		{"kitten", "sitting", 3},
		{"qwen3", "qwen3.8", 2},
		{"café", "cafe", 1}, // rune-aware: 'é' -> 'e' is distance 1
	}
	for _, tc := range tests {
		if got := strutils.Levenshtein(tc.a, tc.b); got != tc.want {
			t.Errorf("Levenshtein(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestRankMatch(t *testing.T) {
	if tier, score, ok := strutils.RankMatch("Fast", "fast", 2); !ok || tier != 0 || score != 0 {
		t.Fatalf("expected tier 0 exact match, got (%d, %d, %v)", tier, score, ok)
	}
	if tier, _, ok := strutils.RankMatch("qwen3", "qwen3.8", 1); !ok || tier != 1 {
		t.Fatalf("expected tier 1 substring match, got tier=%d ok=%v", tier, ok)
	}
	if tier, score, ok := strutils.RankMatch("ollxma", "ollama", 2); !ok || tier != 2 || score != 1 {
		t.Fatalf("expected tier 2 edit distance=1, got (%d, %d, %v)", tier, score, ok)
	}
	if _, _, ok := strutils.RankMatch("xyz", "completely-different", 2); ok {
		t.Fatal("expected distant strings not to match")
	}
}

func TestTopClosestIDsAndSuggestClosestStrings(t *testing.T) {
	matches := []strutils.ClosestMatch{
		{ID: "b", Tier: 1, Score: 3},
		{ID: "a", Tier: 1, Score: 3},
		{ID: "b", Tier: 0, Score: 0}, // duplicates lower score should promote "b"
		{ID: "c", Tier: 2, Score: 1},
		{ID: "d", Tier: 2, Score: 2},
	}
	got := strutils.TopClosestIDs(matches, 3)
	want := []string{"b", "a", "c"}
	if !slices.Equal(got, want) {
		t.Fatalf("TopClosestIDs = %v, want %v", got, want)
	}

	gotStrings := strutils.SuggestClosestStrings("explre", []string{"explore", "general", "planner", "extra"}, 2)
	if !slices.Equal(gotStrings, []string{"explore"}) && (len(gotStrings) == 0 || gotStrings[0] != "explore") {
		t.Fatalf("SuggestClosestStrings = %v, want explore first", gotStrings)
	}

	if gotFmt := strutils.FormatDidYouMean(nil); gotFmt != "" {
		t.Fatalf("FormatDidYouMean(nil) = %q, want empty", gotFmt)
	}
	if gotFmt := strutils.FormatDidYouMean([]string{"alpha", "beta"}); gotFmt != ` (did you mean "alpha", "beta"?)` {
		t.Fatalf("FormatDidYouMean = %q", gotFmt)
	}
}
