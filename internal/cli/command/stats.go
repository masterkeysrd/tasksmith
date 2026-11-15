package command

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/daemon/metrics"
	uicomponents "github.com/masterkeysrd/tasksmith/internal/ui/components"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

func (r *Registry) registerStatsCommand() {
	r.Register(Command{
		Name:        "stats",
		Usage:       "/stats [session|workspace|global]",
		Description: "Browse prompt processing and token generation speed per model",
		Handler:     handleStats,
	})
}

func handleStats(args []string) error {
	tab := usageTabSession
	if len(args) > 1 {
		return fmt.Errorf("stats: /stats [session|workspace|global]")
	}
	if len(args) == 1 {
		switch strings.ToLower(strings.TrimSpace(args[0])) {
		case "session":
		case "workspace":
			tab = usageTabWorkspace
		case "global":
			tab = usageTabGlobal
		default:
			return fmt.Errorf("unknown stats view %q (expected session, workspace or global)", args[0])
		}
	}
	store.EnqueueSlot(store.SlotItem{
		Kind: store.SlotKindPicker,
		Data: func(slotID string) trail.Node {
			return statsBrowser(statsBrowserProps{slotID: slotID, tab: tab})
		},
	})
	return nil
}

type statsBrowserProps struct {
	slotID string
	tab    usageTab
}

// statsFetch is the load state of one /stats tab. The three results are the
// sum, per-metric count and max queries, which cannot share one query because
// a row reports one value per metric name.
type statsFetch struct {
	key     string
	loading bool
	err     string
	data    *statsData
}

// statsAutoRefresh is how often a live Session view re-reads its own metrics.
const statsAutoRefresh = 15 * time.Second

// statsBrowser renders the /stats slot: throughput headline numbers and a
// per-model speed table over a selectable scope and range.
var statsBrowser = trail.FC("StatsBrowser", func(props statsBrowserProps) trail.Node {
	scheme := theme.UseTheme()

	tab, setTab := kitex.UseState(props.tab)
	rangeIdx, setRangeIdx := kitex.UseState(0)
	tick, setTick := kitex.UseState(0)
	offset, setOffset := kitex.UseState(0)
	fetch, setFetch := kitex.UseState(statsFetch{})

	// Subscribing to the live totals re-renders the Session tab on every
	// usage_updated event, so a running session updates without waiting for
	// the refresh interval.
	_ = store.UseUsageTotals()

	currentKey := fmt.Sprintf("%d/%d/%d", tab(), rangeIdx(), tick())
	kitex.UseEffectCleanup(func() func() {
		c := store.Client()
		if c == nil {
			setFetch(statsFetch{key: currentKey, err: "Client unavailable."})
			return nil
		}
		now := time.Now()
		rng := statsRanges[rangeIdx()]
		// Chat calls only: compaction re-reads a whole context and title
		// generation is a tiny call, so both would distort prompt speed.
		filter := api.MetricsFilter{Attrs: map[string]string{metrics.AttrPurpose: string(metrics.PurposeChat)}}
		if tab() == usageTabSession {
			session := store.GetSession()
			if session.Session == nil {
				// Rendered as "No active session." before the fetch state is read.
				setFetch(statsFetch{key: currentKey})
				return nil
			}
			filter.SessionID = session.Session.ID
			filter.IncludeChildren = true
		}
		base := api.MetricsQuery{
			Kind:   string(metrics.KindLLMCall),
			Filter: filter,
			Since:  rng.since(now),
		}
		setFetch(statsFetch{key: currentKey, loading: true})
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		key := currentKey
		scopeWorkspace := tab() == usageTabWorkspace
		go func() {
			defer cancel()
			// The metrics query endpoint does not resolve a workspace from the
			// request's working directory the way /v1/usage does, and records
			// store the resolved workspace root rather than whatever directory
			// the CLI happened to start in. Ask the daemon for the root.
			if scopeWorkspace {
				ws, err := c.GetWorkspace(ctx)
				if err != nil {
					setFetch(statsFetch{key: key, err: err.Error()})
					return
				}
				base.Filter.WorkspaceRoot = ws.RootDir
			}
			summary, err := queryStats(ctx, c, base, metricsAgg(statsSummaryMetrics, metrics.AggSum), []string{"attr.model"})
			if err != nil {
				setFetch(statsFetch{key: key, err: err.Error()})
				return
			}
			counts, err := queryStats(ctx, c, base, metricsAgg(statsCountMetrics, metrics.AggCount), []string{"attr.model"})
			if err != nil {
				setFetch(statsFetch{key: key, err: err.Error()})
				return
			}
			maxes, err := queryStats(ctx, c, base, metricsAgg(statsMaxMetrics, metrics.AggMax), []string{"attr.model"})
			if err != nil {
				setFetch(statsFetch{key: key, err: err.Error()})
				return
			}
			data := buildStats(summary, counts, maxes)
			setFetch(statsFetch{key: key, data: &data})
		}()
		return cancel
	}, []any{currentKey})

	// Only the Session tab refreshes on a timer: it reads the calls happening
	// right now, while the wider scopes scan history and gain nothing from a
	// background query every interval.
	kitex.UseInterval(func() {
		if tab() == usageTabSession {
			setTick(tick() + 1)
		}
	}, statsAutoRefresh, []any{tab})

	var lines []usageLine
	switch {
	case tab() == usageTabSession && store.GetSession().Session == nil:
		lines = []usageLine{{Text: "No active session.", Style: lineDim}}
	case fetch().key != currentKey || fetch().loading:
		lines = []usageLine{{Text: "Loading stats...", Style: lineDim}}
	case fetch().err != "":
		lines = []usageLine{{Text: "Could not load stats: " + fetch().err, Style: lineWarn}}
	case fetch().data == nil:
		lines = []usageLine{{Text: statsRanges[rangeIdx()].Label + ": no chat calls recorded.", Style: lineDim}}
	default:
		lines = statsLines(*fetch().data, statsRanges[rangeIdx()].Label)
	}

	maxOffset := max(0, len(lines)-usageViewRows)
	pos := min(offset(), maxOffset)

	trail.UseInput(func(_ string, k event.KeyEvent) {
		switch {
		case k.MatchString("esc", "escape", "q"):
			store.RemoveSlot(props.slotID)
		case k.MatchString("tab", "right", "l"):
			setTab((tab() + 1) % usageTab(len(usageTabNames)))
			setOffset(0)
		case k.MatchString("left", "shift+tab", "h"):
			setTab((tab() + usageTab(len(usageTabNames)) - 1) % usageTab(len(usageTabNames)))
			setOffset(0)
		case k.MatchString("1"):
			setTab(usageTabSession)
			setOffset(0)
		case k.MatchString("2"):
			setTab(usageTabWorkspace)
			setOffset(0)
		case k.MatchString("3"):
			setTab(usageTabGlobal)
			setOffset(0)
		case k.MatchString("t"):
			setRangeIdx((rangeIdx() + 1) % len(statsRanges))
			setOffset(0)
		case k.MatchString("r"):
			setTick(tick() + 1)
		case k.MatchString("up", "k", "ctrl+p"):
			setOffset(max(0, pos-1))
		case k.MatchString("down", "j", "ctrl+n"):
			setOffset(min(maxOffset, pos+1))
		case k.MatchString("pgup", "b", "ctrl+u", "ctrl+b"):
			setOffset(max(0, pos-usageViewRows))
		case k.MatchString("pgdown", "space", "f", "ctrl+d", "ctrl+f"):
			setOffset(min(maxOffset, pos+usageViewRows))
		}
	})

	border := style.SingleBorder().Top(true).Bottom(false).Left(false).Right(false).Color(scheme.Color.Border.Default)
	labelStyle := style.S().WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Icon.Primary)
	lineStyle := func(l usageLine) style.Style {
		st := style.S().WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Text.Primary)
		switch l.Style {
		case lineHeading:
			st = style.S().WhiteSpace(style.WhiteSpacePre).Bold(true).Foreground(scheme.Color.Icon.Primary)
		case lineDim:
			st = style.S().WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Text.Secondary)
		case lineWarn:
			st = style.S().WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Text.Warning)
		}
		return st
	}

	// Spans are inline, so each line lives in its own block-level Box.
	row := func(st style.Style, text string) trail.Node {
		return trail.Box(trail.BoxProps{}, trail.Span(trail.SpanProps{Style: st}, trail.Text(text)))
	}
	labeled := func(st style.Style, text string, labelLen int) trail.Node {
		if labelLen <= 0 || labelLen > len(text) {
			return row(st, text)
		}
		return trail.Box(trail.BoxProps{},
			trail.Span(trail.SpanProps{Style: labelStyle}, trail.Text(text[:labelLen])),
			trail.Span(trail.SpanProps{Style: st}, trail.Text(text[labelLen:])))
	}
	children := []trail.Node{
		row(style.S().WhiteSpace(style.WhiteSpacePre).Bold(true).Foreground(scheme.Color.Icon.Primary), "Stats"),
		row(style.S().WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Text.Secondary),
			statsTabBar(tab(), statsRanges[rangeIdx()])),
		row(style.S().WhiteSpace(style.WhiteSpacePre), " "),
	}
	end := min(len(lines), pos+usageViewRows)
	for _, l := range lines[pos:end] {
		text := l.Text
		if text == "" {
			text = " "
		}
		children = append(children, labeled(lineStyle(l), text, l.LabelLen))
	}
	if len(lines) > usageViewRows {
		children = append(children, row(style.S().WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Text.Muted),
			fmt.Sprintf("lines %d-%d of %d", pos+1, end, len(lines))))
	}

	bindings := []uicomponents.KeyBinding{
		{Key: "←/→", Label: "Tab"},
		{Key: "↑/↓", Label: "Scroll"},
		{Key: "t", Label: "Range"},
		{Key: "r", Label: "Refresh"},
		{Key: "esc", Label: "Close"},
	}
	children = append(children, trail.Box(trail.BoxProps{Style: style.S().Margin(1, 0, 0, 0)},
		uicomponents.KeyboardHint(uicomponents.KeyboardHintProps{Bindings: bindings})))

	return trail.Box(trail.BoxProps{
		Style: style.S().
			Border(border).
			Width(style.Percent(100)).
			FlexDirection(style.FlexColumn),
	}, children...)
})

// queryStats runs one aggregate pass over the metrics store.
func queryStats(ctx context.Context, c statsQuerier, base api.MetricsQuery, aggs []api.MetricsAggregate, groupBy []string) (api.MetricsResult, error) {
	q := base
	q.Aggregates = aggs
	q.GroupBy = groupBy
	return c.QueryMetrics(ctx, q)
}

// statsQuerier is the part of the daemon client /stats needs.
type statsQuerier interface {
	QueryMetrics(ctx context.Context, q api.MetricsQuery) (api.MetricsResult, error)
}

// metricsAgg builds one aggregate function over a list of metrics.
func metricsAgg(ms []metrics.Metric, fn metrics.AggFunc) []api.MetricsAggregate {
	out := make([]api.MetricsAggregate, len(ms))
	for i, m := range ms {
		out[i] = api.MetricsAggregate{Metric: string(m), Func: string(fn)}
	}
	return out
}

// statsTabBar renders the tab strip: the active tab in brackets, plus the
// active range.
func statsTabBar(active usageTab, rng usageRange) string {
	var parts []string
	for i, name := range usageTabNames {
		label := fmt.Sprintf("%d %s", i+1, name)
		if usageTab(i) == active {
			label = "[" + label + "]"
		}
		parts = append(parts, label)
	}
	return strings.Join(parts, "  ") + fmt.Sprintf("    range: %s", rng.Key)
}
