package command

import (
	"strings"
	"testing"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
)

func text(lines []usageLine) string {
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l.Text)
		b.WriteString("\n")
	}
	return b.String()
}

func TestSessionLinesContents(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	u := api.SessionUsage{
		SessionID: "s1",
		FirstAt:   now.Add(-3 * time.Hour),
		LastAt:    now.Add(-time.Minute),
		Totals: api.UsageGroup{
			Calls: 10, UnpricedCalls: 1, CostNano: 1_000_000_000, DurationMS: 60_000,
			CostCacheWriteNano: 500_000_000, CostCacheReadNano: 20_000_000, LargeContextCostNano: 400_000_000,
			Tokens: api.UsageTokens{Input: 120_000, Output: 8_000, CacheRead: 90_000, CacheWrite: 10_000},
		},
		Models:  []api.UsageGroup{{Key: "p/m", Calls: 10, CostNano: 1_000_000_000, Tokens: api.UsageTokens{Input: 120_000, Output: 8_000, CacheRead: 90_000, CacheWrite: 10_000}}},
		Changes: api.UsageChanges{LinesAdded: 312, LinesRemoved: 48, Files: 7},
		Cache: api.UsageCache{
			Requests: 10, InputTokens: 120_000, CacheReadTokens: 90_000, Misses: 3, Rebuilds: 1,
			LastMissAt: now.Add(-6 * time.Minute), LastMissGapMS: 3_600_000, LastMissRecachedTokens: 8_000,
		},
		Tools: []api.ToolUsage{
			{Tool: "bash", Calls: 10, Errors: 4, Denied: 3, DurationMS: 9_000, MaxDurationMS: 4_000},
			{Tool: "edit", Calls: 6, LinesAdded: 312, LinesRemoved: 48},
		},
	}
	out := strings.Join(strings.Fields(text(sessionLines(u, now))), " ")
	for _, want := range []string{
		"Total cost:", "≥ $1.00", "Total duration (API):", "Total duration (wall):", "2h 59m",
		"312 lines added, 48 lines removed",
		"20.0k uncached input, 90.0k cache read, 10.0k cache write, 8.0k output",
		"75% of input tokens from cache", "3 misses", "1 expected rebuild after compaction", "8.0k tokens re-cached",
		"4 errors", "3 denied", "slowest 4.0s", "+312/−48 lines",
		"What's driving your usage?",
		"40% of cost came from calls above 150.0k context",
		"50% of cost came from cache writes",
		"bash failed 40% of its 10 calls",
		"bash was denied 3 times",
		"3 prompt cache misses",
		"1 call had no pricing",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	for _, l := range sessionLines(u, now) {
		if len([]rune(l.Text)) > wrapWidth+2 {
			t.Errorf("line too long (%d): %q", len([]rune(l.Text)), l.Text)
		}
	}
}

func TestSessionLinesEmpty(t *testing.T) {
	out := text(sessionLines(api.SessionUsage{}, time.Now()))
	if !strings.Contains(out, "No usage recorded") {
		t.Fatalf("out = %q", out)
	}
}

func TestSummaryLinesGlobal(t *testing.T) {
	s := api.UsageSummary{
		Scope: api.UsageScopeGlobal, GroupBy: api.UsageGroupByModel,
		Since:    time.Now().Add(-30 * 24 * time.Hour),
		Sessions: 2,
		Totals: api.UsageGroup{Calls: 20, UnpricedCalls: 5, CostNano: 3_000_000_000,
			Tokens: api.UsageTokens{Input: 1000, Output: 100, CacheRead: 500}},
		Previous: &api.UsageGroup{CostNano: 1_000_000_000},
		Groups: []api.UsageGroup{
			{Key: "p/a", Calls: 15, CostNano: 2_000_000_000, Tokens: api.UsageTokens{Input: 800, Output: 80}},
			{Key: "p/b", Calls: 5, UnpricedCalls: 5, CostNano: 1_000_000_000, Tokens: api.UsageTokens{Input: 200, Output: 20}},
		},
		Workspaces:  []api.UsageGroup{{Key: "/ws", Calls: 20, CostNano: 3_000_000_000}},
		TopSessions: []api.UsageSession{{SessionID: "0123456789", Workspace: "/ws", CostNano: 2_000_000_000, Calls: 12}},
		Changes:     api.UsageChanges{LinesAdded: 90, LinesRemoved: 10},
	}
	out := text(summaryLines(s, "Last 30 days"))
	for _, want := range []string{
		"Last 30 days (since", "+200% vs the previous period", "avg ≥ $1.50 per session",
		"≥ $3.00 per 100 changed lines", "Usage by model:", "p/a", "66%",
		"Usage by workspace:", "/ws", "Top sessions by cost:", "01234567", "(/ws)",
		"models without pricing: p/b", "spend is up 200%",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestDayGroupMarksPeakDay(t *testing.T) {
	s := api.UsageSummary{
		GroupBy: api.UsageGroupByDay,
		Totals:  api.UsageGroup{Calls: 2, CostNano: 300},
		Groups: []api.UsageGroup{
			{Key: "2026-10-28", Calls: 1, CostNano: 100},
			{Key: "2026-10-29", Calls: 1, CostNano: 200},
		},
	}
	var peak string
	for _, l := range summaryLines(s, "All time") {
		if strings.Contains(l.Text, "*") {
			peak = l.Text
		}
	}
	if !strings.Contains(peak, "2026-10-29") {
		t.Fatalf("peak line = %q", peak)
	}
}

func TestUsageTabBar(t *testing.T) {
	bar := usageTabBar(usageTabWorkspace, api.UsageGroupByTier, usageRanges[1])
	for _, want := range []string{"1 Session", "[2 Workspace]", "3 Global", "group: tier", "range: 7d"} {
		if !strings.Contains(bar, want) {
			t.Errorf("missing %q in %q", want, bar)
		}
	}
}
