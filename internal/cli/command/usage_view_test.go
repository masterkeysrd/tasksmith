package command

import (
	"context"
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
	for _, want := range []string{"1 Session", "[2 Workspace]", "3 Global", "4 Quotas", "group: tier", "range: 7d"} {
		if !strings.Contains(bar, want) {
			t.Errorf("missing %q in %q", want, bar)
		}
	}
}

func TestQuotaLinesSharedPoolAndMultiBucket(t *testing.T) {
	now := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	reset5h := now.Add(3*time.Hour + 40*time.Minute)
	reset7d := now.Add(48 * time.Hour)
	snap := &api.QuotaSnapshotResponse{
		Providers: []api.ProviderQuotaSnapshot{
			{
				Provider: "pool",
				Account:  "dev@example.com",
				Buckets: map[string]api.QuotaBucket{
					"5h": {ID: "5h", RemainingFraction: 0.94, ResetAt: &reset5h},
					"7d": {ID: "7d", RemainingFraction: 0.40, ResetAt: &reset7d},
				},
				Models: map[string][]string{
					"pool/b": {"5h", "7d"},
					"pool/a": {"5h", "7d"},
				},
			},
			{
				Provider: "solo",
				Buckets: map[string]api.QuotaBucket{
					"requests":  {ID: "requests", Unit: api.QuotaUnitRequests, Used: 300, Limit: 300, RemainingFraction: 0},
					"unlimited": {ID: "unlimited", Unlimited: true, RemainingFraction: 1.0},
				},
				Models: map[string][]string{
					"solo/x": {"requests", "unlimited"},
				},
			},
		},
	}
	lines := quotaLines(snap, nil, "pool/a", now)
	out := text(lines)

	if !strings.Contains(out, "pool · dev@example.com") {
		t.Errorf("provider heading missing:\n%s", out)
	}
	if !strings.Contains(out, "Shared pool (2 models): pool/a · current, pool/b") {
		t.Errorf("shared pool line with active model marker missing:\n%s", out)
	}
	if !strings.Contains(out, "94%  5h · resets in 3h40m at") || !strings.Contains(out, "40%  7d · resets in 48h at") {
		t.Errorf("multi-bucket bars/countdowns missing:\n%s", out)
	}
	if !strings.Contains(out, "0%  requests · 300/300 requests") {
		t.Errorf("empty discrete-unit bucket row missing:\n%s", out)
	}
	if !strings.Contains(out, "∞     unlimited") {
		t.Errorf("unlimited bucket row missing:\n%s", out)
	}
	// Gauge bands: 5h (94%) + unlimited are green, 7d (40%) is warn, requests (0%) is danger.
	var good, warn, danger int
	for _, l := range lines {
		switch l.Style {
		case lineGood:
			good++
		case lineWarn:
			warn++
		case lineDanger:
			danger++
		}
	}
	if good != 2 || warn != 1 || danger != 1 {
		t.Errorf("gauge styles: good=%d warn=%d danger=%d, want 2 green, 1 warn, 1 danger:\n%s", good, warn, danger, out)
	}
}

func TestQuotaLinesLiveReadingOverridesFetch(t *testing.T) {
	now := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	reset := now.Add(3 * time.Hour)
	liveReset := now.Add(40 * time.Minute)
	snap := &api.QuotaSnapshotResponse{
		Providers: []api.ProviderQuotaSnapshot{
			{
				Provider: "pool",
				Account:  "dev@example.com",
				Buckets: map[string]api.QuotaBucket{
					"5h": {ID: "5h", RemainingFraction: 0.94, ResetAt: &reset},
				},
				Models: map[string][]string{
					"pool/a": {"5h"},
					"pool/b": {"5h"},
				},
			},
			{
				Provider: "other",
				Buckets: map[string]api.QuotaBucket{
					"daily": {ID: "daily", RemainingFraction: 0.5},
				},
				Models: map[string][]string{
					"other/c": {"daily"},
				},
			},
		},
	}
	live := &api.ModelQuota{
		Model:    "pool/a",
		Provider: "pool",
		Account:  "dev@example.com",
		Buckets:  []api.QuotaBucket{{ID: "5h", RemainingFraction: 0.41, ResetAt: &liveReset}},
	}
	out := text(quotaLines(snap, live, "pool/a", now))

	if !strings.Contains(out, "41%  5h · resets in 40m at") {
		t.Errorf("live percentage and reset time not applied:\n%s", out)
	}
	if strings.Contains(out, "94%") {
		t.Errorf("stale fetched percentage still rendered:\n%s", out)
	}
	if !strings.Contains(out, "50%  daily") {
		t.Errorf("unrelated pool changed:\n%s", out)
	}
}

func TestQuotaLinesGroupsDistinctBucketSetsUnderOneProvider(t *testing.T) {
	now := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	resetA := now.Add(2 * time.Hour)
	resetB := now.Add(5 * time.Hour)
	snap := &api.QuotaSnapshotResponse{
		Providers: []api.ProviderQuotaSnapshot{
			{
				Provider: "prov",
				Buckets: map[string]api.QuotaBucket{
					"flash": {ID: "flash", RemainingFraction: 0.7, ResetAt: &resetA},
					"pro":   {ID: "pro", RemainingFraction: 0.5, ResetAt: &resetB},
				},
				Models: map[string][]string{
					"prov/claude-sonnet": {"pro"},
					"prov/gemini-flash":  {"flash"},
					"prov/gemini-pro":    {"pro"},
				},
			},
		},
	}
	out := text(quotaLines(snap, nil, "", now))
	if n := strings.Count(out, "Shared pool (2 models): prov/claude-sonnet, prov/gemini-pro"); n != 1 {
		t.Errorf("shared-pool group count = %d, want 1:\n%s", n, out)
	}
	if !strings.Contains(out, "  prov/gemini-flash") {
		t.Errorf("single-model group missing:\n%s", out)
	}
	if !strings.Contains(out, "resets in 5h at") || !strings.Contains(out, "resets in 2h at") {
		t.Errorf("bucket resets missing:\n%s", out)
	}
}

func TestQuotaLinesEmpty(t *testing.T) {
	lines := quotaLines(nil, nil, "", time.Now())
	if len(lines) != 1 || !strings.Contains(lines[0].Text, "No quota-billed models") {
		t.Fatalf("lines = %+v, want a single explanatory line", lines)
	}
}

func TestQuotaGaugeStyle(t *testing.T) {
	for _, tc := range []struct {
		pct  int
		want lineStyle
	}{
		{0, lineDanger},
		{19, lineDanger},
		{20, lineWarn},
		{49, lineWarn},
		{50, lineGood},
		{100, lineGood},
	} {
		if got := quotaGaugeStyle(tc.pct); got != tc.want {
			t.Errorf("quotaGaugeStyle(%d) = %v, want %v", tc.pct, got, tc.want)
		}
	}
}

func TestFetchQuotaSnapshots(t *testing.T) {
	snap, err := fetchQuotaSnapshots(context.Background(), quotasTransport{})
	if err != nil {
		t.Fatal(err)
	}
	if snap == nil || len(snap.Providers) != 1 || snap.Providers[0].Provider != "pool" {
		t.Fatalf("snap = %+v, want pool snapshot", snap)
	}
}

type quotasTransport struct{}

func (quotasTransport) GetQuotaSnapshots(context.Context) (*api.QuotaSnapshotResponse, error) {
	return &api.QuotaSnapshotResponse{
		Providers: []api.ProviderQuotaSnapshot{
			{
				Provider: "pool",
				Buckets:  map[string]api.QuotaBucket{"5h": {ID: "5h", RemainingFraction: 0.5}},
				Models:   map[string][]string{"pool/a": {"5h"}},
			},
		},
	}, nil
}
