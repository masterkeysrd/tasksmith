package command

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/tui/testutils"
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
		Purposes: []api.UsageGroup{
			{Key: "permission_review", Calls: 3, CostNano: 15_000_000, Tokens: api.UsageTokens{Input: 1_200, Output: 120}},
		},
		Review: &api.UsagePermissionReview{
			Reviews:    4,
			Approved:   3,
			Deferred:   1,
			DurationMS: 200,
			Outcomes:   map[string]int64{"approved": 3, "below_threshold": 1},
		},
	}
	out := strings.Join(strings.Fields(text(sessionLines(u, now))), " ")
	for _, want := range []string{
		"Total cost:", "≥ $1.00", "Total duration (API):", "Total duration (wall):", "2h 59m",
		"312 lines added, 48 lines removed",
		"20.0k uncached input, 90.0k cache read, 10.0k cache write, 8.0k output",
		"75% of input tokens from cache", "3 misses", "1 expected rebuild after compaction", "8.0k tokens re-cached",
		"4 errors", "3 denied", "slowest 4.0s", "+312/−48 lines",
		"Permission reviews: 4 reviews · 3 approved · 1 deferred · 200ms total (avg 50ms)",
		"Review inference: 3 calls · 1.3k tokens · $0.01",
		"Outcomes: approved 3, below_threshold 1",
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
	env := testutils.NewEnv(t, 100, 20)
	env.Render(usageBrowser(usageBrowserProps{tab: usageTabWorkspace}))
	if got := env.KeyPress("g"); !strings.Contains(got, "group: provider") {
		t.Fatalf("g did not cycle the group:\n%s", got)
	}
	env.KeyPress("i")
	env.KeyPress("t")
	env.KeyPress("t")
	bar := env.KeyPress("t")
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
	lines := quotaLines(snap, nil, "pool/a", now, quotaOptions{Width: 100})
	out := text(lines)

	if !strings.Contains(out, "pool · dev@example.com") {
		t.Errorf("provider heading missing:\n%s", out)
	}
	if !strings.Contains(out, "● Current pool (2 models) · a, b") {
		t.Errorf("active shared pool line missing:\n%s", out)
	}
	if !strings.Contains(out, "94%  resets in 3h40m (") || !strings.Contains(out, "40%  resets in 2d (") {
		t.Errorf("multi-bucket bars/countdowns missing:\n%s", out)
	}
	if !strings.Contains(out, "requests") || !strings.Contains(out, "0%  300/300 requests") {
		t.Errorf("empty discrete-unit bucket row missing:\n%s", out)
	}
	if !strings.Contains(out, "unlimited") || !strings.Contains(out, "∞  unlimited") {
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
	out := text(quotaLines(snap, live, "pool/a", now, quotaOptions{Width: 100}))

	if !strings.Contains(out, "41%  resets in 40m (") {
		t.Errorf("live percentage and reset time not applied:\n%s", out)
	}
	if strings.Contains(out, "94%") {
		t.Errorf("stale fetched percentage still rendered:\n%s", out)
	}
	if !strings.Contains(out, "daily") || !strings.Contains(out, "50%") {
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
	out := text(quotaLines(snap, nil, "", now, quotaOptions{Width: 100}))
	if n := strings.Count(out, "Shared pool (2 models) · claude-sonnet, gemini-pro"); n != 1 {
		t.Errorf("shared-pool group count = %d, want 1:\n%s", n, out)
	}
	if !strings.Contains(out, "  gemini-flash") {
		t.Errorf("single-model group missing:\n%s", out)
	}
	if !strings.Contains(out, "resets in 5h (") || !strings.Contains(out, "resets in 2h (") {
		t.Errorf("bucket resets missing:\n%s", out)
	}
}

func TestQuotaLinesLargePoolSummarizesPrioritizesActiveAndExpands(t *testing.T) {
	now := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	reset5h := now.Add(4*time.Hour + 51*time.Minute)
	resetWeekly := now.Add(162*time.Hour + 51*time.Minute)

	models := map[string][]string{
		"antigravity/claude-opus-5-5-high":   {"3p-5h", "3p-weekly"},
		"antigravity/claude-opus-5-5-low":    {"3p-5h", "3p-weekly"},
		"antigravity/claude-sonnet-5-5-high": {"3p-5h", "3p-weekly"},
		"antigravity/claude-sonnet-5-5-low":  {"3p-5h", "3p-weekly"},
	}
	for i := range 20 {
		models[fmt.Sprintf("antigravity/gemini-model-%02d", i)] = []string{"gemini-5h", "gemini-weekly"}
	}
	models["antigravity/gemini-z-active"] = []string{"gemini-5h", "gemini-weekly"}

	snap := &api.QuotaSnapshotResponse{
		Providers: []api.ProviderQuotaSnapshot{
			{
				Provider: "antigravity",
				Account:  "dev@example.com",
				Buckets: map[string]api.QuotaBucket{
					"3p-5h":         {ID: "3p-5h", RemainingFraction: 1.0},
					"3p-weekly":     {ID: "3p-weekly", RemainingFraction: 1.0},
					"gemini-5h":     {ID: "gemini-5h", RemainingFraction: 0.97, ResetAt: &reset5h},
					"gemini-weekly": {ID: "gemini-weekly", RemainingFraction: 0.85, ResetAt: &resetWeekly},
				},
				Models: models,
			},
		},
	}

	const width = 92
	collapsed := quotaLines(snap, nil, "antigravity/gemini-z-active", now, quotaOptions{Width: width})
	out := text(collapsed)

	for _, l := range collapsed {
		if n := len([]rune(l.Text)); n > width {
			t.Errorf("collapsed line exceeds width %d (%d runes): %q", width, n, l.Text)
		}
	}
	activeIdx := strings.Index(out, "● Current pool (21 models)")
	otherIdx := strings.Index(out, "Shared pool (4 models)")
	if activeIdx < 0 || otherIdx < 0 || activeIdx > otherIdx {
		t.Fatalf("expected active pool before other shared pool:\n%s", out)
	}
	if !strings.Contains(out, "gemini-z-active") || !strings.Contains(out, "more") {
		t.Errorf("expected active model in collapsed preview with +N more:\n%s", out)
	}
	if strings.Contains(out, "antigravity/gemini-") {
		t.Errorf("redundant provider prefix should be stripped:\n%s", out)
	}
	if !strings.Contains(out, "resets in 6d18h (") {
		t.Errorf("weekly reset should format in days+hours:\n%s", out)
	}

	expanded := quotaLines(snap, nil, "antigravity/gemini-z-active", now, quotaOptions{Width: width, ShowModels: true})
	expOut := text(expanded)
	for _, l := range expanded {
		if n := len([]rune(l.Text)); n > width {
			t.Errorf("expanded line exceeds width %d (%d runes): %q", width, n, l.Text)
		}
	}
	if !strings.Contains(expOut, "gemini-z-active (current)") || !strings.Contains(expOut, "gemini-model-19") {
		t.Errorf("expected full wrapped model list when expanded:\n%s", expOut)
	}
}

func TestQuotaResetText(t *testing.T) {
	now := time.Date(2026, 1, 2, 10, 0, 0, 0, time.Local)
	sameDay := now.Add(4*time.Hour + 51*time.Minute)
	nextDay := now.Add(20 * time.Hour)
	week := now.Add(162*time.Hour + 51*time.Minute)
	twoWeeks := now.Add(14 * 24 * time.Hour)
	past := now.Add(-time.Minute)

	if got := quotaResetText(nil, now); got != "" {
		t.Errorf("nil reset = %q, want empty", got)
	}
	if got := quotaResetText(&past, now); got != "resets now" {
		t.Errorf("past reset = %q, want resets now", got)
	}
	if got := quotaResetText(&sameDay, now); got != "resets in 4h51m (14:51)" {
		t.Errorf("same-day reset = %q", got)
	}
	if got := quotaResetText(&nextDay, now); !strings.HasPrefix(got, "resets in 20h (Sat 06:00)") {
		t.Errorf("next-day reset = %q", got)
	}
	if got := quotaResetText(&week, now); !strings.HasPrefix(got, "resets in 6d18h (Fri 04:51)") {
		t.Errorf("weekly reset = %q", got)
	}
	if got := quotaResetText(&twoWeeks, now); got != "resets in 14d (Jan 16 10:00)" {
		t.Errorf("two-week reset = %q", got)
	}
}

func TestUsageBrowserQuotasRenderAndModelsToggle(t *testing.T) {
	store.ClearSlot()
	t.Cleanup(store.ClearSlot)

	env := testutils.NewEnv(t, 100, 30)
	out := env.Render(usageBrowser(usageBrowserProps{slotID: "slot-usage-quotas", tab: usageTabQuotas}))
	if !strings.Contains(out, "[4 Quotas]") {
		t.Fatalf("expected active Quotas tab:\n%s", out)
	}
	if !strings.Contains(out, "m Models") {
		t.Fatalf("expected m Models keyboard hint:\n%s", out)
	}

	out = env.Press(key.Key{Code: 'm', Text: "m"})
	if !strings.Contains(out, "m Hide models") {
		t.Fatalf("expected m Hide models after pressing m:\n%s", out)
	}

	// Render multi-span styled quota lines through renderUsageLine onto a framebuffer.
	now := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	reset := now.Add(4 * time.Hour)
	lines := quotaLines(&api.QuotaSnapshotResponse{
		Providers: []api.ProviderQuotaSnapshot{{
			Provider: "antigravity",
			Account:  "dev@example.com",
			Buckets: map[string]api.QuotaBucket{
				"gemini-5h": {ID: "gemini-5h", RemainingFraction: 0.97, ResetAt: &reset},
			},
			Models: map[string][]string{
				"antigravity/gemini-4-neon": {"gemini-5h"},
				"antigravity/gemini-flash":  {"gemini-5h"},
			},
		}},
	}, nil, "antigravity/gemini-4-neon", now, quotaOptions{Width: 96})

	rendered := testutils.Render(t, reportPanel(reportPanelProps{
		Title: "Usage", Tabs: usageTabs, Active: usageTabQuotas, Lines: lines,
	}), 96, 20)
	for _, want := range []string{
		"antigravity · dev@example.com",
		"● Current pool (2 models) · gemini-4-neon, gemini-flash",
		"gemini-5h",
		"97%",
		"resets in 4h (",
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("rendered quota view missing %q:\n%s", want, rendered)
		}
	}
}

func TestQuotaLinesEmpty(t *testing.T) {
	lines := quotaLines(nil, nil, "", time.Now(), quotaOptions{})
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
