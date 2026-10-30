package components

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/masterkeysrd/kite/backend/mock"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/ui/format"
)

func renderTestNode(t *testing.T, node trail.Node, width, height int, check func(string) bool) string {
	t.Helper()
	b := mock.New(width, height)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go func() {
		_ = trail.Run(ctx, node, trail.WithBackend(b))
	}()

	var text string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		text = surfaceText(b)
		if check(text) {
			return text
		}
		time.Sleep(5 * time.Millisecond)
	}
	return text
}

func TestFormatTokens(t *testing.T) {
	tests := []struct {
		input int
		want  string
	}{
		{0, "0"},
		{840, "840"},
		{6000, "6.0k"},
		{109500, "109.5k"},
		{400000, "400.0k"},
		{1000000, "1.0M"},
		{2500000, "2.5M"},
		{-500, "-500"},
	}
	for _, tc := range tests {
		got := format.Tokens(tc.input)
		if got != tc.want {
			t.Errorf("FormatTokens(%d) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestTimelineEventCompactedAndCacheBroken(t *testing.T) {
	// 1. Compacted with token counts
	compEntry := api.TimelineEntry{
		ID:   "tl-c-1",
		Kind: api.TimelineKindCompacted,
		Compacted: &api.CompactedRecord{
			BeforeTokens: 148200,
			AfterTokens:  3400,
		},
	}
	text := renderTestNode(t, TimelineEvent(compEntry), 80, 24, func(s string) bool {
		return strings.Contains(s, "Context compacted (148.2k → 3.4k)")
	})
	if !strings.Contains(text, "Context compacted (148.2k → 3.4k)") {
		t.Fatalf("expected compacted notice text with tokens, got:\n%s", text)
	}

	// 2. Compacted without token counts
	compEntryNoTokens := api.TimelineEntry{
		ID:   "tl-c-2",
		Kind: api.TimelineKindCompacted,
	}
	text = renderTestNode(t, TimelineEvent(compEntryNoTokens), 80, 24, func(s string) bool {
		return strings.Contains(s, "Context compacted")
	})
	if !strings.Contains(text, "Context compacted") {
		t.Fatalf("expected compacted notice text, got:\n%s", text)
	}

	// 3. Cache broken with token counts
	cbEntry := api.TimelineEntry{
		ID:   "tl-cb-1",
		Kind: api.TimelineKindCacheBroken,
		CacheBroken: &api.CacheBrokenRecord{
			PrevCached: 94200,
			CurrCached: 0,
		},
	}
	text = renderTestNode(t, TimelineEvent(cbEntry), 80, 24, func(s string) bool {
		return strings.Contains(s, "Prompt cache miss (94.2k → 0 cached tokens)")
	})
	if !strings.Contains(text, "Prompt cache miss (94.2k → 0 cached tokens)") {
		t.Fatalf("expected cache broken notice text with tokens, got:\n%s", text)
	}

	// 4. Cache broken without token counts
	cbEntryNoTokens := api.TimelineEntry{
		ID:   "tl-cb-2",
		Kind: api.TimelineKindCacheBroken,
	}
	text = renderTestNode(t, TimelineEvent(cbEntryNoTokens), 80, 24, func(s string) bool {
		return strings.Contains(s, "Prompt cache miss")
	})
	if !strings.Contains(text, "Prompt cache miss") {
		t.Fatalf("expected cache broken notice text, got:\n%s", text)
	}
}

func TestSpinnerCompacting(t *testing.T) {
	text := renderTestNode(t, Spinner(SpinnerProps{Status: api.StatusCompacting}), 80, 24, func(s string) bool {
		return strings.Contains(s, "Compacting...")
	})
	if !strings.Contains(text, "Compacting...") {
		t.Fatalf("expected Compacting... in spinner, got:\n%s", text)
	}
}

func TestContextUsageRender(t *testing.T) {
	data := store.ContextUsageData{
		ModelName: "GPT-5.4 (high) · openai/gpt-5.4",
		Context: api.ContextState{
			UsedTokens:    109500,
			MaxTokens:     400000,
			ReserveTokens: 44000,
			TriggerTokens: 356000,
			FreeTokens:    246500,
			InputTokens:   100000,
			Breakdown: api.ContextBreakdown{
				SystemPrompt:   6000,
				ContextFiles:   1200,
				SystemTools:    4800,
				Skills:         686,
				Subagents:      420,
				UserMessages:   840,
				AgentResponses: 17500,
				ToolCalls:      78000,
			},
		},
	}

	text := renderTestNode(t, ContextUsage(data), 120, 30, func(s string) bool {
		return strings.Contains(s, "Token usage by category")
	})

	// Ensure no adjacent glyphs are jammed together without whitespace.
	for _, bad := range []string{"⛁◉", "◉⛶", "⛶⛝", "⛁⛶", "◉⛝"} {
		if strings.Contains(text, bad) {
			t.Errorf("found unspaced adjacent glyphs %q in render:\n%s", bad, text)
		}
	}

	for _, want := range []string{
		"/context",
		"Context Usage",
		"GPT-5.4 (high) · openai/gpt-5.4 · 109.5k/400.0k (27.4%)",
		"Token usage by category",
		"System prompt: 6.0k tokens",
		"Context files: 1.2k tokens",
		"System tools: 4.8k tokens",
		"Skills: 686 tokens",
		"Subagents: 420 tokens",
		"User messages: 840 tokens",
		"Agent responses: 17.5k tokens",
		"Tool calls: 78.0k tokens",
		"Free space: 246.5k tokens",
		"Autocompact buffer: 44.0k tokens",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("ContextUsage render missing %q, got:\n%s", want, text)
		}
	}
}

func TestContextUsageEstimatedHeader(t *testing.T) {
	data := store.ContextUsageData{
		ModelName: "DefaultModel",
		Context: api.ContextState{
			MaxTokens:   128000,
			InputTokens: 0, // Awaiting generation
			Breakdown: api.ContextBreakdown{
				SystemPrompt: 5000,
			},
		},
	}

	text := renderTestNode(t, ContextUsage(data), 120, 30, func(s string) bool {
		return strings.Contains(s, "Estimated usage by category (awaiting generation)")
	})
	if !strings.Contains(text, "Estimated usage by category (awaiting generation)") {
		t.Fatalf("expected estimated header, got:\n%s", text)
	}
}
