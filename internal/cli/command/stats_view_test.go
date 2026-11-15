package command

import (
	"strings"
	"testing"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/metrics"
)

func statsRow(model string, values map[string]int64, count int64) api.MetricsRow {
	group := map[string]string{"attr.model": model}
	if model == "" {
		group = map[string]string{}
	}
	return api.MetricsRow{Group: group, Count: count, Values: values}
}

func TestRateAndAvgHandleMissingMeasurements(t *testing.T) {
	if got := rate(1000, 1000); got != 1000 {
		t.Errorf("rate(1000, 1000ms) = %v, want 1000 tok/s", got)
	}
	// A zero or absent latency must not read as "instant".
	for _, tc := range []struct{ tokens, ms int64 }{{100, 0}, {0, 500}, {100, -1}} {
		if got := rate(tc.tokens, tc.ms); got != -1 {
			t.Errorf("rate(%d, %d) = %v, want the not-measured sentinel", tc.tokens, tc.ms, got)
		}
	}
	if got := avgMS(900, 3); got != 300 {
		t.Errorf("avgMS(900, 3) = %v, want 300", got)
	}
	// The denominator is the calls that recorded the metric, so a zero count
	// must not produce a zero average.
	if got := avgMS(900, 0); got != -1 {
		t.Errorf("avgMS(900, 0) = %v, want the not-measured sentinel", got)
	}
}

func TestPromptTokensExcludesOnlyCacheReads(t *testing.T) {
	m := statsModel{Tokens: api.UsageTokens{Input: 1000, CacheRead: 600, CacheWrite: 200}}
	if got := m.promptTokens(); got != 400 {
		t.Errorf("promptTokens() = %d, want 400 (cache writes were processed)", got)
	}
	if got := (statsModel{Tokens: api.UsageTokens{CacheRead: 500}}).promptTokens(); got != 0 {
		t.Errorf("promptTokens() underflow guard = %d, want 0", got)
	}
}

func TestBuildStatsMergesSumsCountsAndMaxes(t *testing.T) {
	summary := api.MetricsResult{Rows: []api.MetricsRow{
		statsRow("opus", map[string]int64{
			string(metrics.TokensInput): 1000, string(metrics.TokensCacheRead): 500,
			string(metrics.TokensOutput): 200, string(metrics.LatencyProcessing): 500,
			string(metrics.LatencyGeneration): 2000, string(metrics.LatencyDuration): 4000,
			string(metrics.LatencyTTFT): 800, string(metrics.CostTotal): 1200,
		}, 4),
		statsRow("", map[string]int64{string(metrics.TokensInput): 50, string(metrics.TokensOutput): 10}, 1),
	}}
	counts := api.MetricsResult{Rows: []api.MetricsRow{
		// Two of the four calls recorded processing and generation; none of the
		// unknown-model calls recorded anything.
		statsRow("opus", map[string]int64{
			string(metrics.LatencyProcessing): 2, string(metrics.LatencyGeneration): 3,
			string(metrics.LatencyDuration): 4, string(metrics.LatencyTTFT): 4,
			string(metrics.CostTotal): 3,
		}, 4),
	}}
	maxes := api.MetricsResult{Rows: []api.MetricsRow{
		statsRow("opus", map[string]int64{
			string(metrics.LatencyDuration): 1900, string(metrics.LatencyTTFT): 640,
		}, 4),
	}}

	d := buildStats(summary, counts, maxes)
	if len(d.Models) != 2 {
		t.Fatalf("models = %+v", d.Models)
	}
	// Ordered by total tokens, so the unknown model is last.
	if d.Models[0].Model != "opus" || d.Models[1].Model != "" {
		t.Fatalf("ordering = [%s, %s], want opus first", d.Models[0].Model, d.Models[1].Model)
	}
	opus := d.Models[0]
	if opus.Calls != 4 || opus.ProcessingMS != 500 || opus.ProcessingN != 2 {
		t.Fatalf("opus = %+v", opus)
	}
	if opus.CostN != 3 {
		t.Errorf("opus.CostN = %d, want 3 priced calls", opus.CostN)
	}
	if opus.MaxDurationMS != 1900 || opus.MaxTTFTMS != 640 {
		t.Errorf("opus worst cases = %d/%d, want 1900/640", opus.MaxDurationMS, opus.MaxTTFTMS)
	}

	total := d.totals()
	if total.Calls != 5 || total.promptTokens() != 550 || total.Tokens.Output != 210 {
		t.Fatalf("totals = %+v", total)
	}
	// Generation speed uses the summed latency, not the per-call average.
	if got := rate(total.Tokens.Output, total.GenerationMS); got != 105 {
		t.Errorf("generation rate = %v tok/s, want 105", got)
	}
}

func TestStatsLinesEmptyRange(t *testing.T) {
	lines := statsLines(statsData{}, "Today")
	if len(lines) != 2 || !strings.Contains(lines[0].Text, "no chat calls") {
		t.Fatalf("empty range lines = %+v", lines)
	}
	if !strings.Contains(lines[1].Text, "widen the range") {
		t.Errorf("empty range should suggest widening: %+v", lines)
	}
}

func TestStatsLinesReportsLatencyCoverage(t *testing.T) {
	d := statsData{Models: []statsModel{{
		Model: "opus", Calls: 10,
		Tokens:       api.UsageTokens{Input: 2000, CacheRead: 1000, Output: 400},
		ProcessingMS: 1000, ProcessingN: 2,
		GenerationMS: 4000, GenerationN: 10,
		DurationMS: 9000, DurationN: 10, MaxDurationMS: 2100,
		TTFTMS: 1200, TTFTN: 10, MaxTTFTMS: 310, CostNano: 5e9, CostN: 10,
	}}}
	lines := statsLines(d, "Today")
	joined := strings.Join(textsOf(lines), "\n")

	// Prompt speed uses uncached input (1000) over processing (1000ms) = 1000 tok/s.
	if !strings.Contains(joined, "1000 tok/s") {
		t.Errorf("prompt processing missing from:\n%s", joined)
	}
	// Generation: 400 tokens over 4000ms = 100 tok/s (rounded, since the
	// formatter keeps decimals only below 100).
	if !strings.Contains(joined, "100 tok/s") {
		t.Errorf("token generation missing from:\n%s", joined)
	}
	if !strings.Contains(joined, "processing 2 of 10 calls") {
		t.Errorf("expected a latency coverage footnote:\n%s", joined)
	}
	if strings.Contains(joined, "generation 10 of 10") {
		t.Errorf("complete metrics should not be footnoted:\n%s", joined)
	}
	if !strings.Contains(joined, "Chat calls only") {
		t.Errorf("expected the purpose scope note:\n%s", joined)
	}
}

func TestStatsLinesShowsDashesWhenLatencyMissing(t *testing.T) {
	d := statsData{Models: []statsModel{{
		Model: "gpt", Calls: 3, Tokens: api.UsageTokens{Input: 900, Output: 30},
	}}}
	joined := strings.Join(textsOf(statsLines(d, "Today")), "\n")
	for _, want := range []string{"—", "processing 0 of 3"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "0.0 tok/s") {
		t.Errorf("unmeasured speed must not render as zero:\n%s", joined)
	}
}

func TestStatsTableAlignsColumns(t *testing.T) {
	lines := statsTableLines([]statsModel{
		{Model: "claude-opus-4-1", Calls: 42, GenerationN: 42,
			Tokens: api.UsageTokens{Input: 312400, CacheRead: 260100, Output: 9100}, GenerationMS: 130000},
		{Model: "gpt-5.2", Calls: 17, GenerationN: 17,
			Tokens: api.UsageTokens{Input: 88000, CacheRead: 6000, Output: 3400}, GenerationMS: 66000},
	})
	if len(lines) != 3 {
		t.Fatalf("lines = %+v", lines)
	}
	if lines[0].Style != lineHeading {
		t.Errorf("header should be a heading: %+v", lines[0])
	}
	// The rate column ends each row, so both rows must be the same length for
	// the column to line up.
	if len(lines[1].Text) != len(lines[2].Text) {
		t.Fatalf("rows differ in width:\n%s\n%s", lines[1].Text, lines[2].Text)
	}
	if !strings.Contains(lines[1].Text, "claude-opus-4-1") || !strings.Contains(lines[1].Text, "70.0 tok/s") {
		t.Errorf("row 1 = %q", lines[1].Text)
	}
	// 9100 prompt-less input: 312400-260100 = 52.3k
	if !strings.Contains(lines[1].Text, "52.3k") {
		t.Errorf("prompt column should show uncached input: %q", lines[1].Text)
	}
}

func TestStatsTableEmptyModelLabel(t *testing.T) {
	lines := statsTableLines([]statsModel{{Model: "", Calls: 1}})
	if !strings.Contains(lines[1].Text, "(unknown)") {
		t.Errorf("empty model key should read as unknown: %q", lines[1].Text)
	}
}

// A provider speaking the OpenAI protocol reports no processing or generation
// split, only time to first token and total duration. Those bracket both
// speeds, so the view derives them instead of showing dashes.
func TestStatsDerivesSpeedsWhenProviderOmitsTimingSplit(t *testing.T) {
	d := statsData{Models: []statsModel{{
		Model: "Qwen3.8-Flash-Next", Calls: 354,
		Tokens:     api.UsageTokens{Input: 46_310_702, CacheRead: 45_770_882, Output: 149_700},
		DurationMS: 2_690_000, DurationN: 354,
		TTFTMS: 77_500, TTFTN: 354,
		CostNano: 0, CostN: 0,
	}}}
	joined := strings.Join(textsOf(statsLines(d, "Today")), "\n")

	// Prefill is bounded by TTFT: 539,820 uncached input over 77.5s of TTFT.
	if !strings.Contains(joined, "~6965 tok/s") {
		t.Errorf("expected a derived prompt speed:\n%s", joined)
	}
	// Generation is duration minus TTFT: 149.7k over (2_690_000-77_500)ms.
	if !strings.Contains(joined, "~57.3 tok/s") {
		t.Errorf("expected a derived generation speed:\n%s", joined)
	}
	if !strings.Contains(joined, "Prompt processing and Token generation inferred from time to first token") {
		t.Errorf("expected the footnote to name the inferred speeds:\n%s", joined)
	}
	if strings.Contains(joined, "Token generation:      —") {
		t.Errorf("speeds should be derivable from the timings that do exist:\n%s", joined)
	}
}

// Marking an inferred speed is only honest if a measured one is left unmarked.
func TestStatsDoesNotMarkMeasuredSpeeds(t *testing.T) {
	d := statsData{Models: []statsModel{{
		Model: "claude", Calls: 4, GenerationN: 4, GenerationMS: 4000,
		ProcessingN: 4, ProcessingMS: 2000,
		Tokens: api.UsageTokens{Input: 1000, Output: 400},
	}}}
	joined := strings.Join(textsOf(statsLines(d, "Today")), "\n")
	if strings.Contains(joined, "~") {
		t.Errorf("measured speeds must not be marked as estimates:\n%s", joined)
	}
	if !strings.Contains(joined, "100 tok/s") {
		t.Errorf("expected the measured generation speed:\n%s", joined)
	}
}

// Dividing every call's tokens by the latency of only some calls inflates the
// result, so a partial sum must be marked as an estimate.
func TestStatsMarksPartiallyCoveredSpeeds(t *testing.T) {
	d := statsData{Models: []statsModel{{
		Model: "ollama", Calls: 10,
		GenerationN: 2, GenerationMS: 4000, ProcessingN: 10, ProcessingMS: 5000,
		Tokens: api.UsageTokens{Input: 5000, Output: 400},
	}}}
	joined := strings.Join(textsOf(statsLines(d, "Today")), "\n")
	if !strings.Contains(joined, "~100 tok/s") {
		t.Errorf("a speed over 2 of 10 calls should be marked approximate:\n%s", joined)
	}
}

// An unpriced local model has no cost to report; "≥ $0.00" would imply a
// lower bound that is actually just unknown.
func TestStatsCostReadsNotPricedForUnpricedModel(t *testing.T) {
	d := statsData{Models: []statsModel{{
		Model: "local-qwen", Calls: 354, GenerationN: 354, GenerationMS: 1000,
		Tokens: api.UsageTokens{Input: 100, Output: 10}, CostNano: 0, CostN: 0,
	}}}
	joined := strings.Join(textsOf(statsLines(d, "Today")), "\n")
	if !strings.Contains(joined, "Cost:") || !strings.Contains(joined, "not priced") {
		t.Errorf("expected an unpriced cost line:\n%s", joined)
	}
	if strings.Contains(joined, "≥ $0.00") {
		t.Errorf("a fully unpriced model should not read as a zero lower bound:\n%s", joined)
	}
}

// Subtracting a TTFT sum over more calls from a duration sum over fewer would
// attribute unrelated time to generation, so no speed is invented.
func TestStatsRefusesMismatchedDerivation(t *testing.T) {
	m := statsModel{
		Calls: 10, Tokens: api.UsageTokens{Output: 500},
		DurationMS: 5000, DurationN: 4,
		TTFTMS: 4000, TTFTN: 10,
	}
	if got := m.generationLatency(); got.Sum != 0 {
		t.Errorf("generationLatency() = %+v, want unusable when coverage differs", got)
	}
	joined := strings.Join(textsOf(statsLines(statsData{Models: []statsModel{m}}, "Today")), "\n")
	if strings.Contains(joined, "Token generation:      —") == false {
		t.Errorf("mismatched coverage should fall back to a dash:\n%s", joined)
	}
}

func TestStatsRangesStartAtTodayAndEndAtAllTime(t *testing.T) {
	if statsRanges[0].Key != "today" || statsRanges[len(statsRanges)-1].Key != "all" {
		t.Fatalf("ranges = %+v", statsRanges)
	}
	for _, r := range statsRanges {
		if r.Label == "" {
			t.Errorf("range %q has no label", r.Key)
		}
	}
	// Today must begin at local midnight, not 24 hours ago.
	now := time.Date(2026, 3, 14, 9, 30, 0, 0, time.Local)
	since := statsRanges[0].since(now)
	if want := time.Date(2026, 3, 14, 0, 0, 0, 0, time.Local); !since.Equal(want) {
		t.Errorf("today since = %v, want %v", since, want)
	}
	if got := statsRanges[1].since(now); !got.Equal(now.Add(-7 * 24 * time.Hour)) {
		t.Errorf("7d since = %v, want %v", got, now.Add(-7*24*time.Hour))
	}
	if got := statsRanges[3].since(now); got.Year() > 1970 {
		t.Errorf("all-time since = %v, want the epoch", got)
	}
}

func TestStatsRangesLeaveUsageRangesAlone(t *testing.T) {
	// /stats opening on Today must not change what /usage offers.
	if usageRanges[0].Key != "24h" || len(usageRanges) != 4 {
		t.Errorf("usageRanges changed: %+v", usageRanges)
	}
}

func textsOf(lines []usageLine) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = l.Text
	}
	return out
}
