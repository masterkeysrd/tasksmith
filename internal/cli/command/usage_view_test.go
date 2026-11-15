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

func TestQuotaLinesSharedPool(t *testing.T) {
	now := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	reset := now.Add(3*time.Hour + 40*time.Minute)
	quotas := []api.ModelQuota{
		{Model: "pool/b", Provider: "pool", Account: "dev@example.com", RemainingFraction: 0.94, ResetAt: reset},
		{Model: "pool/a", Provider: "pool", Account: "dev@example.com", RemainingFraction: 0.94, ResetAt: reset},
		{Model: "solo/x", Provider: "solo", RemainingFraction: 0},
	}
	lines := quotaLines(quotas, nil, "pool/a", now)
	out := text(lines)

	if !strings.Contains(out, "pool · dev@example.com (shared pool · 2 models)") {
		t.Errorf("shared pool heading missing:\n%s", out)
	}
	if n := strings.Count(out, "resets in 3h40m at"); n != 1 {
		t.Errorf("shared reset rendered %d times, want once per pool:\n%s", n, out)
	}
	if !strings.Contains(out, "94%") || !strings.Contains(out, "█") {
		t.Errorf("quota bar missing:\n%s", out)
	}
	if !strings.Contains(out, "pool/a · current") {
		t.Errorf("active model not marked:\n%s", out)
	}
	if !strings.Contains(out, "0%  solo/x") {
		t.Errorf("empty window row missing:\n%s", out)
	}
	// Gauge bands: the healthy pool rows read green, the exhausted window red.
	var good, danger int
	for _, l := range lines {
		switch l.Style {
		case lineGood:
			good++
		case lineDanger:
			danger++
		}
	}
	if good != 2 || danger != 1 {
		t.Errorf("gauge styles: good=%d danger=%d, want 2 pool rows green and the empty window red:\n%s", good, danger, out)
	}
}

func TestQuotaLinesLiveReadingOverridesFetch(t *testing.T) {
	now := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	reset := now.Add(3 * time.Hour)
	quotas := []api.ModelQuota{
		{Model: "pool/a", Provider: "pool", Account: "dev@example.com", RemainingFraction: 0.94, ResetAt: reset},
		{Model: "pool/b", Provider: "pool", Account: "dev@example.com", RemainingFraction: 0.94, ResetAt: reset},
		{Model: "other/c", Provider: "other", RemainingFraction: 0.5, ResetAt: reset.Add(time.Hour)},
	}
	live := &api.ModelQuota{Model: "pool/a", Provider: "pool", Account: "dev@example.com", RemainingFraction: 0.41, ResetAt: now.Add(40 * time.Minute)}
	out := text(quotaLines(quotas, live, "pool/a", now))

	// The live reading replaces the fetched percentage for its whole pool...
	if !strings.Contains(out, "41%") {
		t.Errorf("live percentage not applied:\n%s", out)
	}
	if strings.Contains(out, "94%") {
		t.Errorf("stale fetched percentage still rendered:\n%s", out)
	}
	// ...including shared-pool peers, and the countdown follows the fresh reset.
	if !strings.Contains(out, "resets in 40m at") {
		t.Errorf("live reset time not applied:\n%s", out)
	}
	// Unrelated pools keep their fetched values.
	if !strings.Contains(out, "50%  other/c") {
		t.Errorf("unrelated pool changed:\n%s", out)
	}
}

func TestQuotaLinesCurrentWithoutLiveReading(t *testing.T) {
	now := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	quotas := []api.ModelQuota{
		{Model: "pool/a", Provider: "pool", RemainingFraction: 0.8, ResetAt: now.Add(time.Hour)},
	}
	out := text(quotaLines(quotas, nil, "pool/a", now))
	if !strings.Contains(out, "pool/a · current") {
		t.Errorf("current marker lost when no live reading arrived yet:\n%s", out)
	}
}

func TestQuotaLinesGroupsInterleavedPools(t *testing.T) {
	now := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	resetA := now.Add(2 * time.Hour)
	resetB := now.Add(5 * time.Hour)
	// Model names interleave alphabetically across two pools of one provider.
	quotas := []api.ModelQuota{
		{Model: "prov/claude-sonnet", Provider: "prov", RemainingFraction: 0.5, ResetAt: resetB},
		{Model: "prov/gemini-flash", Provider: "prov", RemainingFraction: 0.5, ResetAt: resetA},
		{Model: "prov/gemini-pro", Provider: "prov", RemainingFraction: 0.5, ResetAt: resetB},
	}
	out := text(quotaLines(quotas, nil, "", now))
	// Pool B (2 models) must render as ONE section; pool A holds one model,
	// so it gets a plain heading.
	if n := strings.Count(out, "prov (shared pool · 2 models)"); n != 1 {
		t.Errorf("shared-pool headings = %d, want the 2-model pool once:\n%s", n, out)
	}
	if n := strings.Count(out, "resets in 5h at"); n != 1 {
		t.Errorf("pool B reset rendered %d times, want once:\n%s", n, out)
	}
	if n := strings.Count(out, "resets in 2h at"); n != 1 {
		t.Errorf("pool A reset rendered %d times, want once:\n%s", n, out)
	}
	for _, model := range []string{"prov/claude-sonnet", "prov/gemini-flash", "prov/gemini-pro"} {
		if !strings.Contains(out, model) {
			t.Errorf("row %s missing:\n%s", model, out)
		}
	}
}

func TestQuotaLinesNoResetModelsStaySeparate(t *testing.T) {
	quotas := []api.ModelQuota{
		{Model: "prov/a", Provider: "prov", RemainingFraction: 0.7},
		{Model: "prov/b", Provider: "prov", RemainingFraction: 0.2},
	}
	out := text(quotaLines(quotas, nil, "", time.Now()))
	if strings.Contains(out, "shared pool") {
		t.Errorf("models without reset times merged into a false shared pool:\n%s", out)
	}
	if !strings.Contains(out, "70%  prov/a") || !strings.Contains(out, "20%  prov/b") {
		t.Errorf("independent windows missing:\n%s", out)
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

func TestFetchModelQuotas(t *testing.T) {
	quotas, err := fetchModelQuotas(context.Background(), quotasTransport{})
	if err != nil {
		t.Fatal(err)
	}
	if len(quotas) != 1 || quotas[0].Model != "pool/a" {
		t.Fatalf("quotas = %+v, want only the quota-billed chat model", quotas)
	}
}

// quotasTransport serves one quota-billed chat model, one priced model, and
// one quota-billed embedding model that must be skipped.
type quotasTransport struct{}

func (quotasTransport) ListModels(context.Context) ([]api.Model, error) {
	return []api.Model{
		{ID: "pool/a", Provider: "pool", Name: "A", QuotaBilled: true},
		{ID: "priced/b", Provider: "priced", Name: "B", Pricing: &api.ModelPricing{Input: 1, Output: 2}},
		{ID: "pool/c", Provider: "pool", Name: "C", QuotaBilled: true, Capabilities: api.ModelCapabilities{Completion: new(false)}},
	}, nil
}

func (quotasTransport) GetModelQuota(_ context.Context, model string) (*api.ModelQuota, error) {
	if model != "pool/a" {
		return nil, nil
	}
	return &api.ModelQuota{Model: model, Provider: "pool", RemainingFraction: 0.5}, nil
}
