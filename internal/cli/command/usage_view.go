package command

import (
	"fmt"
	"strings"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/ui/format"
)

// lineStyle selects how a usage line is emphasized. The layout never depends
// on it: every line is plain text that reads the same without styling.
type lineStyle int

const (
	lineNormal lineStyle = iota
	lineHeading
	lineDim
	lineWarn
)

type usageLine struct {
	Text  string
	Style lineStyle
	// LabelLen is the length of a leading "Label:" prefix rendered in the
	// label colour; zero means the line has no label.
	LabelLen int
}

// wrapWidth is the column at which long usage lines are wrapped.
const wrapWidth = 100

// Thresholds for usage insights. They are fixed; an insight only appears when
// its threshold is met, so a quiet session shows none.
const (
	insightLargeContextShare = 0.20
	insightCacheWriteShare   = 0.40
	insightCompactionShare   = 0.20
	insightMinToolCalls      = 4
	insightToolErrorRate     = 0.25
	insightDenials           = 3
	insightSlowToolShare     = 0.70
	insightSlowToolMin       = 5 * time.Second
	insightCacheMisses       = 3
	insightIdleRatio         = 10
	insightIdleMin           = time.Hour
	insightProviderErrorRate = 0.10
	insightProviderMinCalls  = 5
)

type insight struct {
	Headline string
	Hint     string
}

func tok(n int64) string { return format.FormatTokens(int(n)) }

func pct(part, whole int64) int {
	if whole <= 0 {
		return 0
	}
	return int(float64(part) / float64(whole) * 100)
}

func fraction(part, whole int64) float64 {
	if whole <= 0 {
		return 0
	}
	return float64(part) / float64(whole)
}

func costOf(g api.UsageGroup) string {
	return format.FormatCostNano(g.CostNano, g.UnpricedCalls > 0)
}

// fmtDuration renders a duration in its two largest units (e.g. "3h 52m").
func fmtDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < time.Minute:
		if d < 10*time.Second {
			return fmt.Sprintf("%.1fs", d.Seconds())
		}
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm %02ds", int(d.Minutes()), int(d.Seconds())%60)
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh %02dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		days := int(d.Hours()) / 24
		return fmt.Sprintf("%dd %dh", days, int(d.Hours())%24)
	}
}

func fmtMS(ms int64) string { return fmtDuration(time.Duration(ms) * time.Millisecond) }

func plural(n int64, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

// wrapLine breaks text at wrapWidth on spaces. Continuation lines are indented
// by indent spaces.
func wrapLine(text string, indent int, style lineStyle) []usageLine {
	if len([]rune(text)) <= wrapWidth {
		return []usageLine{{Text: text, Style: style}}
	}
	lead := len(text) - len(strings.TrimLeft(text, " "))
	var out []usageLine
	pad := strings.Repeat(" ", indent)
	cur := strings.Repeat(" ", lead)
	empty := true
	for word := range strings.FieldsSeq(text) {
		if !empty && len([]rune(cur))+1+len([]rune(word)) > wrapWidth {
			out = append(out, usageLine{Text: cur, Style: style})
			cur = pad + word
			continue
		}
		if empty {
			cur += word
			empty = false
		} else {
			cur += " " + word
		}
	}
	if !empty {
		out = append(out, usageLine{Text: cur, Style: style})
	}
	return out
}

func kv(label, value string) usageLine {
	return usageLine{Text: fmt.Sprintf("%-23s%s", label+":", value), LabelLen: len(label) + 1}
}

func tokenBreakdown(g api.UsageGroup) string {
	return fmt.Sprintf("%s uncached input, %s cache read, %s cache write, %s output",
		tok(g.Tokens.Uncached()), tok(g.Tokens.CacheRead), tok(g.Tokens.CacheWrite), tok(g.Tokens.Output))
}

func changesText(c api.UsageChanges) string {
	return fmt.Sprintf("%d lines added, %d lines removed", c.LinesAdded, c.LinesRemoved)
}

func toolLine(t api.ToolUsage, width int) string {
	parts := []string{plural(t.Calls, "call", "calls")}
	if t.Errors > 0 {
		parts = append(parts, plural(t.Errors, "error", "errors"))
	}
	if t.Denied > 0 {
		parts = append(parts, fmt.Sprintf("%d denied", t.Denied))
	}
	if t.Canceled > 0 {
		parts = append(parts, fmt.Sprintf("%d canceled", t.Canceled))
	}
	text := fmt.Sprintf("%*s:  %s", width, t.Tool, strings.Join(parts, ", "))
	if t.DurationMS > 0 {
		text += fmt.Sprintf(" · %s total", fmtMS(t.DurationMS))
		if t.MaxDurationMS > 0 && t.Calls > 1 {
			text += fmt.Sprintf(" (slowest %s)", fmtMS(t.MaxDurationMS))
		}
	}
	if t.LinesAdded > 0 || t.LinesRemoved > 0 {
		text += fmt.Sprintf(" · +%d/−%d lines", t.LinesAdded, t.LinesRemoved)
	}
	return "    " + text
}

func toolLines(tools []api.ToolUsage) []usageLine {
	if len(tools) == 0 {
		return nil
	}
	var calls, errs, denied, canceled, durMS int64
	width := 0
	for _, t := range tools {
		calls += t.Calls
		errs += t.Errors
		denied += t.Denied
		canceled += t.Canceled
		durMS += t.DurationMS
		width = max(width, len(t.Tool))
	}
	ok := calls - errs - denied - canceled
	summary := fmt.Sprintf("%d total · %d ok · %s · %d denied · %d canceled", calls, ok, plural(errs, "error", "errors"), denied, canceled)
	out := []usageLine{kv("Tool calls", summary)}
	if durMS > 0 {
		out = append(out, kv("Tool time", fmt.Sprintf("%s total · avg %s", fmtMS(durMS), fmtMS(durMS/max(calls, 1)))))
	}
	out = append(out, usageLine{Text: "Tools:", Style: lineHeading})
	for _, t := range tools {
		out = append(out, wrapLine(toolLine(t, width), 8, lineNormal)...)
	}
	return out
}

func toolInsights(tools []api.ToolUsage) []insight {
	var out []insight
	var totalMS int64
	for _, t := range tools {
		totalMS += t.DurationMS
	}
	for _, t := range tools {
		if t.Calls >= insightMinToolCalls && fraction(t.Errors, t.Calls) >= insightToolErrorRate {
			out = append(out, insight{
				Headline: fmt.Sprintf("%s failed %d%% of its %d calls", t.Tool, pct(t.Errors, t.Calls), t.Calls),
				Hint:     "Check the arguments or commands the agent is passing, or add guidance to the agent's instructions.",
			})
		}
		if t.Denied >= insightDenials {
			out = append(out, insight{
				Headline: fmt.Sprintf("%s was denied %d times", t.Tool, t.Denied),
				Hint:     "Add an allow rule with /permissions or change the permission mode if this is expected.",
			})
		}
		if time.Duration(totalMS)*time.Millisecond >= insightSlowToolMin && fraction(t.DurationMS, totalMS) >= insightSlowToolShare {
			out = append(out, insight{
				Headline: fmt.Sprintf("%s accounts for %d%% of tool time", t.Tool, pct(t.DurationMS, totalMS)),
				Hint:     "Slow commands delay every turn; consider running them in the background.",
			})
		}
	}
	return out
}

func insightLines(items []insight) []usageLine {
	if len(items) == 0 {
		return nil
	}
	out := []usageLine{{}, {Text: "What's driving your usage?", Style: lineHeading},
		{Text: "Independent characteristics of this usage, not a breakdown.", Style: lineDim}}
	for _, it := range items {
		out = append(out, wrapLine("  "+it.Headline, 4, lineWarn)...)
		if it.Hint != "" {
			out = append(out, wrapLine("    "+it.Hint, 4, lineDim)...)
		}
	}
	return out
}

func sessionInsights(u api.SessionUsage) []insight {
	var out []insight
	t := u.Totals
	if t.CostNano > 0 {
		if f := fraction(t.LargeContextCostNano, t.CostNano); f >= insightLargeContextShare {
			out = append(out, insight{
				Headline: fmt.Sprintf("%d%% of cost came from calls above %s context", int(f*100), tok(api.LargeContextTokens)),
				Hint:     "Longer contexts cost more even when cached. Use /compact mid-task and /clear when switching tasks.",
			})
		}
		if f := fraction(t.CostCacheWriteNano, t.CostNano); f >= insightCacheWriteShare {
			out = append(out, insight{
				Headline: fmt.Sprintf("%d%% of cost came from cache writes", int(f*100)),
				Hint:     "The prompt cache is being rebuilt often. Avoid changing early context such as instructions or tool sets mid-session.",
			})
		}
		for _, p := range u.Purposes {
			if p.Key == "compaction" {
				if f := fraction(p.CostNano, t.CostNano); f >= insightCompactionShare {
					out = append(out, insight{
						Headline: fmt.Sprintf("%d%% of cost came from compaction", int(f*100)),
						Hint:     "Frequent compaction means the context fills quickly. Start a fresh session between unrelated tasks.",
					})
				}
			}
		}
	}
	if u.Cache.Misses >= insightCacheMisses {
		out = append(out, insight{
			Headline: fmt.Sprintf("%d prompt cache misses", u.Cache.Misses),
			Hint:     "Cache reads dropped between consecutive calls. Long pauses between turns or edits to early context are common causes.",
		})
	}
	if !u.FirstAt.IsZero() {
		wall := u.LastAt.Sub(u.FirstAt)
		modelTime := time.Duration(t.DurationMS) * time.Millisecond
		if wall >= insightIdleMin && modelTime > 0 && wall >= insightIdleRatio*modelTime {
			out = append(out, insight{
				Headline: "this session was mostly idle",
				Hint:     "Wall-clock time is far larger than model time, so the prompt cache has probably gone cold; the next turn may re-cache a large prompt.",
			})
		}
	}
	if t.UnpricedCalls > 0 {
		out = append(out, insight{
			Headline: plural(t.UnpricedCalls, "call had", "calls had") + " no pricing",
			Hint:     "Add a cost entry for the model in the workspace manifest so costs are exact.",
		})
	}
	return append(out, toolInsights(u.Tools)...)
}

func cacheLines(u api.SessionUsage, now time.Time) []usageLine {
	c := u.Cache
	if c.Requests == 0 {
		return nil
	}
	text := fmt.Sprintf("%s · %d%% of input tokens from cache · %s",
		plural(c.Requests, "request", "requests"), int(c.HitRate()*100), plural(c.Misses, "miss", "misses"))
	if c.Misses > 0 && !c.LastMissAt.IsZero() {
		text += fmt.Sprintf(" (last %s ago; %s since the previous call", fmtDuration(now.Sub(c.LastMissAt)), fmtMS(c.LastMissGapMS))
		if c.LastMissRecachedTokens > 0 {
			text += fmt.Sprintf("; %s tokens re-cached", tok(c.LastMissRecachedTokens))
		}
		text += ")"
	}
	if c.Rebuilds > 0 {
		text += fmt.Sprintf(" · %s after compaction", plural(c.Rebuilds, "expected rebuild", "expected rebuilds"))
	}
	if !u.LastAt.IsZero() {
		text += fmt.Sprintf(" · last used %s ago", fmtDuration(now.Sub(u.LastAt)))
	}
	t := u.Totals
	out := wrapLine("Prompt cache:          "+text, 23, lineNormal)
	out[0].LabelLen = len("Prompt cache:")
	return append(out, kv("Cache cost", fmt.Sprintf("read %s · write %s",
		format.FormatCostNano(t.CostCacheReadNano, t.UnpricedCalls > 0), format.FormatCostNano(t.CostCacheWriteNano, t.UnpricedCalls > 0))))
}

func groupLabel(g api.UsageGroup) string {
	if g.Key == "" || g.Key == "/" {
		return "(unknown)"
	}
	return g.Key
}

// modelLines renders one indented line per group, with labels right-aligned.
func modelLines(groups []api.UsageGroup) []usageLine {
	width := 0
	for _, g := range groups {
		width = max(width, len(groupLabel(g)))
	}
	var out []usageLine
	for _, g := range groups {
		line := fmt.Sprintf("    %*s:  %s (%s)", width, groupLabel(g), tokenBreakdown(g), costOf(g))
		out = append(out, wrapLine(line, width+8, lineNormal)...)
	}
	return out
}

// sessionLines renders the Session tab.
func sessionLines(u api.SessionUsage, now time.Time) []usageLine {
	t := u.Totals
	if t.Calls == 0 && len(u.Tools) == 0 && u.Changes.Files == 0 {
		return []usageLine{{Text: "No usage recorded for this session yet.", Style: lineDim}}
	}
	out := []usageLine{
		kv("Total cost", costOf(t)),
		kv("Total duration (API)", fmtMS(t.DurationMS)),
	}
	if !u.FirstAt.IsZero() {
		out = append(out, kv("Total duration (wall)", fmtDuration(u.LastAt.Sub(u.FirstAt))))
	}
	out = append(out, kv("Total code changes", changesText(u.Changes)))
	if len(u.Models) > 0 {
		out = append(out, usageLine{Text: "Usage by model:", Style: lineHeading})
		out = append(out, modelLines(u.Models)...)
	}
	out = append(out, cacheLines(u, now)...)
	if t.UnpricedCalls > 0 {
		out = append(out, usageLine{Text: fmt.Sprintf("≥ %d of %d calls had no pricing data, so costs are a lower bound.", t.UnpricedCalls, t.Calls), Style: lineDim})
	}
	out = append(out, toolLines(u.Tools)...)
	return append(out, insightLines(sessionInsights(u))...)
}

func trendText(cur, prev api.UsageGroup) string {
	if prev.CostNano <= 0 {
		return ""
	}
	delta := float64(cur.CostNano-prev.CostNano) / float64(prev.CostNano) * 100
	sign := "+"
	if delta < 0 {
		sign = "−"
		delta = -delta
	}
	return fmt.Sprintf("%s%.0f%% vs the previous period (%s → %s)", sign, delta,
		format.FormatCostNano(prev.CostNano, false), format.FormatCostNano(cur.CostNano, false))
}

func groupTitle(by api.UsageGroupBy) string {
	if by == api.UsageGroupByDay {
		return "Usage by day"
	}
	return "Usage by " + string(by)
}

// shareLines renders groups with a share-of-cost column (or of tokens when no
// group has a known cost) as aligned text.
func shareLines(title string, groups []api.UsageGroup, totals api.UsageGroup, mark bool) []usageLine {
	if len(groups) == 0 {
		return nil
	}
	byCost := totals.CostNano > 0
	width := 0
	for _, g := range groups {
		width = max(width, len(groupLabel(g)))
	}
	out := []usageLine{{Text: title + ":"}}
	var top int64 = -1
	topIdx := -1
	if mark {
		for i, g := range groups {
			if g.CostNano > top {
				top, topIdx = g.CostNano, i
			}
		}
	}
	for i, g := range groups {
		share := pct(g.CostNano, totals.CostNano)
		if !byCost {
			share = pct(g.Tokens.Total(), totals.Tokens.Total())
		}
		star := " "
		if i == topIdx && top > 0 {
			star = "*"
		}
		out = append(out, usageLine{Text: fmt.Sprintf("  %s%*s  %5d calls  %8s tokens  %9s  %3d%%",
			star, width, groupLabel(g), g.Calls, tok(g.Tokens.Total()), costOf(g), share)})
	}
	return out
}

func summaryInsights(s api.UsageSummary) []insight {
	var out []insight
	if s.Previous != nil && s.Previous.CostNano > 0 && s.Totals.CostNano > 0 {
		delta := fraction(s.Totals.CostNano-s.Previous.CostNano, s.Previous.CostNano)
		if delta >= 0.5 {
			out = append(out, insight{
				Headline: fmt.Sprintf("spend is up %d%% compared with the previous period", int(delta*100)),
				Hint:     "Check the by-day and top-session views to find what changed.",
			})
		}
	}
	if s.GroupBy == api.UsageGroupByModel {
		var names []string
		for _, g := range s.Groups {
			if g.UnpricedCalls > 0 {
				names = append(names, groupLabel(g))
			}
		}
		if len(names) > 0 {
			out = append(out, insight{
				Headline: "models without pricing: " + strings.Join(names, ", "),
				Hint:     "Add a cost entry for them in the workspace manifest so costs are exact.",
			})
		}
	}
	if s.GroupBy == api.UsageGroupByProvider {
		for _, g := range s.Groups {
			if g.Calls >= insightProviderMinCalls && fraction(g.Errors, g.Calls) >= insightProviderErrorRate {
				out = append(out, insight{
					Headline: fmt.Sprintf("%s failed or was canceled on %d%% of %d calls", groupLabel(g), pct(g.Errors, g.Calls), g.Calls),
					Hint:     "Check the provider's status and your credentials or rate limits.",
				})
			}
		}
	}
	if s.GroupBy == api.UsageGroupByPurpose {
		for _, g := range s.Groups {
			if g.Key == "compaction" && fraction(g.CostNano, s.Totals.CostNano) >= insightCompactionShare {
				out = append(out, insight{
					Headline: fmt.Sprintf("%d%% of cost came from compaction", pct(g.CostNano, s.Totals.CostNano)),
					Hint:     "Frequent compaction means contexts fill quickly. Start fresh sessions between unrelated tasks.",
				})
			}
		}
	}
	return append(out, toolInsights(s.Tools)...)
}

// summaryLines renders the Workspace and Global tabs. rangeLabel describes the
// period (e.g. "Last 30 days").
func summaryLines(s api.UsageSummary, rangeLabel string) []usageLine {
	t := s.Totals
	if t.Calls == 0 && len(s.Tools) == 0 && s.Changes.Files == 0 {
		return []usageLine{{Text: rangeLabel + ": no usage recorded.", Style: lineDim}}
	}
	head := rangeLabel
	if !s.Since.IsZero() && s.Since.Year() > 1970 {
		head += " (since " + s.Since.Local().Format("2006-01-02") + ")"
	}
	out := []usageLine{{Text: head, Style: lineHeading}}
	out = append(out, kv("Total cost", costOf(t)))
	if trend := trendText(t, derefGroup(s.Previous)); trend != "" {
		out = append(out, kv("Trend", trend))
	}
	sessions := fmt.Sprintf("%d · %s", s.Sessions, plural(t.Calls, "LLM call", "LLM calls"))
	if s.Sessions > 0 && t.CostNano > 0 {
		sessions += " · avg " + format.FormatCostNano(t.CostNano/s.Sessions, t.UnpricedCalls > 0) + " per session"
	}
	out = append(out, kv("Sessions", sessions))
	out = append(out, kv("Total duration (API)", fmtMS(t.DurationMS)))
	changes := changesText(s.Changes)
	if lines := s.Changes.LinesAdded + s.Changes.LinesRemoved; lines > 0 && t.CostNano > 0 {
		changes += fmt.Sprintf(" · %s per 100 changed lines", format.FormatCostNano(t.CostNano*100/lines, t.UnpricedCalls > 0))
	}
	out = append(out, kv("Total code changes", changes))
	out = append(out, kv("Tokens", tokenBreakdown(t)))
	out = append(out, kv("Prompt cache", fmt.Sprintf("%d%% of input tokens from cache · read %s · write %s",
		pct(t.Tokens.CacheRead, t.Tokens.Input),
		format.FormatCostNano(t.CostCacheReadNano, t.UnpricedCalls > 0), format.FormatCostNano(t.CostCacheWriteNano, t.UnpricedCalls > 0))))

	out = append(out, usageLine{})
	out = append(out, shareLines(groupTitle(s.GroupBy), s.Groups, t, s.GroupBy == api.UsageGroupByDay)...)
	if len(s.Workspaces) > 0 {
		out = append(out, usageLine{})
		out = append(out, shareLines("Usage by workspace", s.Workspaces, t, false)...)
	}
	if len(s.TopSessions) > 0 {
		out = append(out, usageLine{}, usageLine{Text: "Top sessions by cost:", Style: lineHeading})
		for _, ts := range s.TopSessions {
			label := ts.Title
			if label == "" {
				label = shortID(ts.SessionID)
			}
			line := fmt.Sprintf("  %9s  %4d calls  %s", format.FormatCostNano(ts.CostNano, false), ts.Calls, label)
			if ts.Workspace != "" && s.Scope == api.UsageScopeGlobal {
				line += "  (" + ts.Workspace + ")"
			}
			out = append(out, wrapLine(line, 29, lineNormal)...)
		}
	}
	if tl := toolLines(s.Tools); len(tl) > 0 {
		out = append(out, usageLine{})
		out = append(out, tl...)
	}
	if t.UnpricedCalls > 0 {
		out = append(out, usageLine{}, usageLine{Text: fmt.Sprintf("≥ %d of %d calls had no pricing data, so costs are a lower bound.", t.UnpricedCalls, t.Calls), Style: lineDim})
	}
	return append(out, insightLines(summaryInsights(s))...)
}

func derefGroup(g *api.UsageGroup) api.UsageGroup {
	if g == nil {
		return api.UsageGroup{}
	}
	return *g
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// usageRange is a selectable period for workspace and global views.
type usageRange struct {
	Key   string
	Label string
	Span  time.Duration // zero means all time
}

var usageRanges = []usageRange{
	{Key: "24h", Label: "Last 24 hours", Span: 24 * time.Hour},
	{Key: "7d", Label: "Last 7 days", Span: 7 * 24 * time.Hour},
	{Key: "30d", Label: "Last 30 days", Span: 30 * 24 * time.Hour},
	{Key: "all", Label: "All time"},
}

func (r usageRange) since(now time.Time) time.Time {
	if r.Span == 0 {
		return time.Unix(0, 0).UTC()
	}
	return now.Add(-r.Span)
}

// usageGroupKeys maps the group-by hotkeys to dimensions, in display order.
var usageGroupKeys = []struct {
	Key string
	By  api.UsageGroupBy
}{
	{"m", api.UsageGroupByModel},
	{"p", api.UsageGroupByProvider},
	{"a", api.UsageGroupByAgent},
	{"u", api.UsageGroupByPurpose},
	{"i", api.UsageGroupByTier},
	{"d", api.UsageGroupByDay},
}
