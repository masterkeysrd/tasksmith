package command

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/daemon/metrics"
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

	return reportPanel(reportPanelProps{
		Title:     "Stats",
		Tabs:      statsTabs,
		Active:    tab(),
		Meta:      "range: " + statsRanges[rangeIdx()].Key,
		Lines:     lines,
		Offset:    offset(),
		SetOffset: setOffset,
		SetTab:    setTab,
		ShowRange: true,
		NextRange: func() { setRangeIdx((rangeIdx() + 1) % len(statsRanges)) },
		Refresh:   func() { setTick(tick() + 1) },
		Close:     func() { store.RemoveSlot(props.slotID) },
	})
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
