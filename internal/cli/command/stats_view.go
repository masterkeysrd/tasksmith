package command

import (
	"fmt"
	"sort"
	"strings"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/metrics"
	"github.com/masterkeysrd/tasksmith/internal/ui/format"
)

// statsSummaryMetrics are the summed measurements of one model group.
var statsSummaryMetrics = []metrics.Metric{
	metrics.TokensInput, metrics.TokensOutput, metrics.TokensCacheRead, metrics.TokensCacheWrite,
	metrics.LatencyDuration, metrics.LatencyTTFT, metrics.LatencyProcessing, metrics.LatencyGeneration,
	metrics.CostTotal,
}

// statsCountMetrics are measured per call, so their counts are the correct
// denominators for the averages /stats shows. Zero measurements are never
// recorded, so a metric recorded on only some calls would otherwise be
// averaged over every call and understate the result.
var statsCountMetrics = []metrics.Metric{
	metrics.LatencyDuration, metrics.LatencyTTFT, metrics.LatencyProcessing, metrics.LatencyGeneration,
	metrics.CostTotal,
}

// statsMaxMetrics are the worst-case latencies shown next to their averages.
var statsMaxMetrics = []metrics.Metric{metrics.LatencyDuration, metrics.LatencyTTFT}

// statsModel carries one model's measurements over the selected range.
type statsModel struct {
	Model string
	Calls int64

	Tokens   api.UsageTokens
	CostNano int64

	// Latency sums in milliseconds, with the number of calls that recorded
	// each one. A zero count means the latency was never measured.
	DurationMS, DurationN     int64
	TTFTMS, TTFTN             int64
	ProcessingMS, ProcessingN int64
	GenerationMS, GenerationN int64

	MaxDurationMS int64
	MaxTTFTMS     int64

	// CostN counts the calls that recorded a cost. The rest are unpriced, so
	// their cost is a lower bound.
	CostN int64
}

// promptTokens returns the prompt tokens the model actually had to read: the
// full prompt minus the part served from the prompt cache. Cache writes stay
// in, since those tokens were processed before being cached.
func (m statsModel) promptTokens() int64 {
	return max(0, m.Tokens.Input-m.Tokens.CacheRead)
}

// totalTokens returns prompt plus output tokens.
func (m statsModel) totalTokens() int64 { return m.Tokens.Input + m.Tokens.Output }

// rate is tokens per second over a latency sum, or -1 when the latency was
// never measured. Rendering a zero would claim a speed that was not observed.
func rate(tokens, latencyMS int64) float64 {
	if latencyMS <= 0 || tokens <= 0 {
		return -1
	}
	return float64(tokens) / float64(latencyMS) * 1000
}

// avgMS is the mean of a latency over the calls that recorded it, or -1.
func avgMS(sum, count int64) float64 {
	if count <= 0 || sum <= 0 {
		return -1
	}
	return float64(sum) / float64(count)
}

// latencyPair is the latency to divide a token count by, plus how much trust
// it deserves. N is the number of calls the sum covers; a sum covering fewer
// calls than the group holds cannot divide every call's tokens, so the
// resulting speed is an estimate. Derived marks a latency inferred from other
// timings rather than reported by the provider.
type latencyPair struct {
	Sum     int64
	N       int64
	Derived bool
}

// approximate reports whether a speed built from this latency should be marked
// as an estimate: either it was inferred, or it covers fewer calls than the
// group contains and so divides all tokens by part of the time.
func (p latencyPair) approximate(calls int64) bool {
	return p.Derived || p.N < calls
}

// processingLatency prefers the reported prompt processing time. Providers
// speaking the OpenAI protocol return no processing split at all (it is an
// Ollama extension), but no token can arrive before the prompt has been read,
// so time to first token bounds prefill from above and stands in for it.
func (m statsModel) processingLatency() latencyPair {
	if m.ProcessingN > 0 {
		return latencyPair{Sum: m.ProcessingMS, N: m.ProcessingN}
	}
	if m.TTFTN > 0 {
		return latencyPair{Sum: m.TTFTMS, N: m.TTFTN, Derived: true}
	}
	return latencyPair{}
}

// generationLatency prefers the reported generation time. Otherwise generation
// occupies the span between the first token and the end of the call. Both
// timings must cover the same calls, since subtracting a sum over fewer calls
// from a sum over more would attribute unrelated time to generation.
func (m statsModel) generationLatency() latencyPair {
	if m.GenerationN > 0 {
		return latencyPair{Sum: m.GenerationMS, N: m.GenerationN}
	}
	if m.DurationN == 0 || m.DurationN != m.TTFTN {
		return latencyPair{}
	}
	if sum := m.DurationMS - m.TTFTMS; sum > 0 {
		return latencyPair{Sum: sum, N: m.DurationN, Derived: true}
	}
	return latencyPair{}
}

// statsData is the result of the three queries behind one /stats load.
type statsData struct {
	Models []statsModel
}

// totals aggregates every model into one row.
func (d statsData) totals() statsModel {
	var t statsModel
	t.Model = "total"
	for _, m := range d.Models {
		t.Calls += m.Calls
		t.Tokens.Input += m.Tokens.Input
		t.Tokens.Output += m.Tokens.Output
		t.Tokens.CacheRead += m.Tokens.CacheRead
		t.Tokens.CacheWrite += m.Tokens.CacheWrite
		t.CostNano += m.CostNano
		t.DurationMS += m.DurationMS
		t.DurationN += m.DurationN
		t.TTFTMS += m.TTFTMS
		t.TTFTN += m.TTFTN
		t.ProcessingMS += m.ProcessingMS
		t.ProcessingN += m.ProcessingN
		t.GenerationMS += m.GenerationMS
		t.GenerationN += m.GenerationN
		t.CostN += m.CostN
		t.MaxDurationMS = max(t.MaxDurationMS, m.MaxDurationMS)
		t.MaxTTFTMS = max(t.MaxTTFTMS, m.MaxTTFTMS)
	}
	return t
}

// buildStats merges the rows of the sum, count and max queries by model. Rows
// are ordered by total tokens, largest first, so the models worth reading
// about sit at the top.
func buildStats(summary, counts, maxes api.MetricsResult) statsData {
	index := map[string]*statsModel{}
	order := []string{}
	byModel := func(res api.MetricsResult, apply func(m *statsModel, row api.MetricsRow)) {
		for _, row := range res.Rows {
			key := row.Group["attr.model"]
			m, ok := index[key]
			if !ok {
				m = &statsModel{Model: key}
				index[key] = m
				order = append(order, key)
			}
			apply(m, row)
		}
	}

	byModel(summary, func(m *statsModel, row api.MetricsRow) {
		m.Calls += row.Count
		m.Tokens.Input += row.Values[string(metrics.TokensInput)]
		m.Tokens.Output += row.Values[string(metrics.TokensOutput)]
		m.Tokens.CacheRead += row.Values[string(metrics.TokensCacheRead)]
		m.Tokens.CacheWrite += row.Values[string(metrics.TokensCacheWrite)]
		m.CostNano += row.Values[string(metrics.CostTotal)]
		m.DurationMS += row.Values[string(metrics.LatencyDuration)]
		m.TTFTMS += row.Values[string(metrics.LatencyTTFT)]
		m.ProcessingMS += row.Values[string(metrics.LatencyProcessing)]
		m.GenerationMS += row.Values[string(metrics.LatencyGeneration)]
	})
	byModel(counts, func(m *statsModel, row api.MetricsRow) {
		m.DurationN += row.Values[string(metrics.LatencyDuration)]
		m.TTFTN += row.Values[string(metrics.LatencyTTFT)]
		m.ProcessingN += row.Values[string(metrics.LatencyProcessing)]
		m.GenerationN += row.Values[string(metrics.LatencyGeneration)]
		m.CostN += row.Values[string(metrics.CostTotal)]
	})
	byModel(maxes, func(m *statsModel, row api.MetricsRow) {
		m.MaxDurationMS = max(m.MaxDurationMS, row.Values[string(metrics.LatencyDuration)])
		m.MaxTTFTMS = max(m.MaxTTFTMS, row.Values[string(metrics.LatencyTTFT)])
	})

	out := make([]statsModel, 0, len(order))
	for _, k := range order {
		out = append(out, *index[k])
	}
	sort.SliceStable(out, func(i, j int) bool {
		if ti, tj := out[i].totalTokens(), out[j].totalTokens(); ti != tj {
			return ti > tj
		}
		return out[i].Model < out[j].Model
	})
	return statsData{Models: out}
}

// statsRate renders a tokens-per-second figure. An unmeasured rate keeps just
// the dash, so a gap never reads as a speed. A rate computed from other
// timings is prefixed with ~ so it is never mistaken for a reported one.
func statsRate(perSecond float64, derived bool) string {
	if perSecond < 0 {
		return "—"
	}
	prefix := ""
	if derived {
		prefix = "~"
	}
	return prefix + format.Rate(perSecond) + " tok/s"
}

// statsAvg renders a mean latency, or a dash when it was never measured.
func statsAvg(sum, count int64) string {
	if avg := avgMS(sum, count); avg >= 0 {
		return format.DurationMilis(int64(avg))
	}
	return "—"
}

// statsMax renders a worst-case latency.
func statsMax(ms int64) string {
	if ms <= 0 {
		return "—"
	}
	return format.DurationMilis(ms)
}

// throughputLines renders the headline block.
func throughputLines(t statsModel) []usageLine {
	proc, gen := t.processingLatency(), t.generationLatency()
	out := []usageLine{{Text: "Throughput:", Style: lineHeading}}
	out = append(out, kv("Prompt processing", fmt.Sprintf("%s  uncached input",
		statsRate(rate(t.promptTokens(), proc.Sum), proc.approximate(t.Calls)))))
	out = append(out, kv("Token generation",
		statsRate(rate(t.Tokens.Output, gen.Sum), gen.approximate(t.Calls))))
	out = append(out, kv("Time to first token", fmt.Sprintf("%s avg  %s worst", statsAvg(t.TTFTMS, t.TTFTN), statsMax(t.MaxTTFTMS))))
	out = append(out, kv("End to end", fmt.Sprintf("%s avg  %s worst", statsAvg(t.DurationMS, t.DurationN), statsMax(t.MaxDurationMS))))
	return out
}

// statsModelLabel renders a model group key, keeping the empty key readable.
func statsModelLabel(model string) string {
	if model == "" || model == "/" {
		return "(unknown)"
	}
	return model
}

// statsTableLines renders the per-model table as aligned plain text, matching
// how /usage aligns its own columns.
func statsTableLines(models []statsModel) []usageLine {
	if len(models) == 0 {
		return nil
	}
	nameWidth := len("MODEL")
	for _, m := range models {
		nameWidth = max(nameWidth, len(statsModelLabel(m.Model)))
	}
	head := fmt.Sprintf("  %-*s  %6s  %8s  %7s  %8s  %8s  %7s",
		nameWidth, "MODEL", "REQ", "PROMPT", "OUTPUT", "CACHED", "TOTAL", "TOK/S")
	out := []usageLine{{Text: head, Style: lineHeading}}
	for _, m := range models {
		gen := m.generationLatency()
		out = append(out, usageLine{Text: fmt.Sprintf("  %-*s  %6d  %8s  %7s  %8s  %8s  %7s",
			nameWidth, statsModelLabel(m.Model), m.Calls,
			tok(m.promptTokens()), tok(m.Tokens.Output), tok(m.Tokens.CacheRead),
			tok(m.totalTokens()), statsRate(rate(m.Tokens.Output, gen.Sum), gen.approximate(m.Calls)))})
	}
	return out
}

// statsFootnote explains what the numbers are built from: whether speeds were
// measured or inferred, whether cost is known, and which calls are counted.
func statsFootnote(t statsModel) []usageLine {
	var out []usageLine
	if t.Calls > 0 {
		cost := "not priced"
		if t.CostN > 0 {
			cost = format.FormatCostNano(t.CostNano, t.CostN < t.Calls)
		}
		out = append(out, kv("Cost", cost))

		proc, gen := t.processingLatency(), t.generationLatency()
		if proc.Derived || gen.Derived {
			var inferred []string
			if proc.Derived {
				inferred = append(inferred, "Prompt processing")
			}
			if gen.Derived {
				inferred = append(inferred, "Token generation")
			}
			// The provider reports no split for these, so the speeds come from
			// the timings it does report. Saying so keeps a ~ figure from being
			// read as a measured one.
			out = append(out, usageLine{
				Text:  strings.Join(inferred, " and ") + " inferred from time to first token and total duration.",
				Style: lineDim,
			})
		} else if t.ProcessingN < t.Calls || t.GenerationN < t.Calls {
			var partial []string
			if t.ProcessingN < t.Calls {
				partial = append(partial, fmt.Sprintf("processing %d of %d", t.ProcessingN, t.Calls))
			}
			if t.GenerationN < t.Calls {
				partial = append(partial, fmt.Sprintf("generation %d of %d", t.GenerationN, t.Calls))
			}
			out = append(out, usageLine{
				Text:  "Timing coverage: " + strings.Join(partial, ", ") + " calls.",
				Style: lineDim,
			})
		}
	}
	out = append(out, usageLine{
		Text:  "Chat calls only; compaction and title calls are excluded.",
		Style: lineDim,
	})
	return out
}

// statsLines renders the body of one /stats tab.
func statsLines(d statsData, rangeLabel string) []usageLine {
	t := d.totals()
	if t.Calls == 0 {
		return []usageLine{
			{Text: rangeLabel + ": no chat calls recorded.", Style: lineDim},
			{Text: "Press t to widen the range.", Style: lineDim},
		}
	}
	out := throughputLines(t)
	out = append(out, usageLine{})
	out = append(out, usageLine{Text: "Per model:", Style: lineHeading})
	out = append(out, statsTableLines(d.Models)...)
	out = append(out, usageLine{})
	return append(out, statsFootnote(t)...)
}
