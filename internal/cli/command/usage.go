package command

import (
	"cmp"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	uicomponents "github.com/masterkeysrd/tasksmith/internal/ui/components"
	"github.com/masterkeysrd/tasksmith/internal/ui/styles"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

func (r *Registry) registerUsageCommand() {
	r.Register(Command{
		Name:        "usage",
		Usage:       "/usage [session|workspace|global|quotas]",
		Description: "Browse token usage, cost, cache and tool metrics",
		Handler:     handleUsage,
	})
}

type usageTab int

const (
	usageTabSession usageTab = iota
	usageTabWorkspace
	usageTabGlobal
	usageTabQuotas
)

var usageTabs = []uicomponents.TabItem{
	{Label: "Session"}, {Label: "Workspace"}, {Label: "Global"}, {Label: "Quotas"},
}

// statsTabs bounds the /stats browser, which has no quota windows of its
// own: quota capacity is a cost concept and lives in /usage only.
var statsTabs = usageTabs[:usageTabQuotas]

func handleUsage(args []string) error {
	tab := usageTabSession
	if len(args) > 1 {
		return fmt.Errorf("usage: /usage [session|workspace|global|quotas]")
	}
	if len(args) == 1 {
		switch strings.ToLower(strings.TrimSpace(args[0])) {
		case "session":
		case "workspace":
			tab = usageTabWorkspace
		case "global":
			tab = usageTabGlobal
		case "quotas":
			tab = usageTabQuotas
		default:
			return fmt.Errorf("unknown usage view %q (expected session, workspace, global or quotas)", args[0])
		}
	}
	store.EnqueueSlot(store.SlotItem{
		Kind: store.SlotKindPicker,
		Data: func(slotID string) trail.Node {
			return usageBrowser(usageBrowserProps{slotID: slotID, tab: tab})
		},
	})
	return nil
}

type usageBrowserProps struct {
	slotID string
	tab    usageTab
}

// usageFetch is the load state of the Workspace, Global, and Quotas tabs.
type usageFetch struct {
	key     string
	loading bool
	err     string
	summary *api.UsageSummary
	quotas  *api.QuotaSnapshotResponse
}

// usageViewRows is how many lines of a usage view are visible at once.
const usageViewRows = 22

// usageBrowser renders the /usage slot: Session, Workspace, Global, and Quotas
// tabs with group-by, range, and model-list hotkeys.
var usageBrowser = trail.FC("UsageBrowser", func(props usageBrowserProps) trail.Node {
	winSize := trail.UseWindowSize()

	tab, setTab := kitex.UseState(props.tab)
	groupIdx, setGroup := kitex.UseState(0)
	group := usageGroupKeys[groupIdx()].By
	rangeIdx, setRangeIdx := kitex.UseState(2)
	showModels, setShowModels := kitex.UseState(false)
	tick, setTick := kitex.UseState(0)
	offset, setOffset := kitex.UseState(0)
	fetch, setFetch := kitex.UseState(usageFetch{})

	// Subscribing to the live totals re-renders the Session tab on every
	// usage_updated event, and the live quota reading re-renders the Quotas
	// tab as the daemon pushes quota_updated events.
	_ = store.UseUsageTotals()
	_ = store.UseQuota()

	viewWidth := wrapWidth
	if winSize.Width > 0 {
		viewWidth = max(48, winSize.Width-2)
	}

	currentKey := fmt.Sprintf("%d/%s/%d/%d", tab(), group, rangeIdx(), tick())
	kitex.UseEffectCleanup(func() func() {
		c := store.Client()
		if c == nil {
			setFetch(usageFetch{key: currentKey, err: "Client unavailable."})
			return nil
		}
		if tab() == usageTabQuotas {
			setFetch(usageFetch{key: currentKey, loading: true})
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			key := currentKey
			go func() {
				defer cancel()
				quotas, err := fetchQuotaSnapshots(ctx, c)
				if err != nil {
					setFetch(usageFetch{key: key, err: err.Error()})
					return
				}
				setFetch(usageFetch{key: key, quotas: quotas})
			}()
			return cancel
		}
		if tab() == usageTabSession {
			return nil
		}
		scope := api.UsageScopeWorkspace
		if tab() == usageTabGlobal {
			scope = api.UsageScopeGlobal
		}
		setFetch(usageFetch{key: currentKey, loading: true})
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		key, by, rng := currentKey, group, usageRanges[rangeIdx()]
		go func() {
			defer cancel()
			summary, err := c.Usage(ctx, api.UsageRequest{Scope: scope, GroupBy: by, Since: rng.since(time.Now())})
			if err != nil {
				setFetch(usageFetch{key: key, err: err.Error()})
				return
			}
			setFetch(usageFetch{key: key, summary: &summary})
		}()
		return cancel
	}, []any{currentKey})

	var lines []usageLine
	now := time.Now()
	switch {
	case tab() == usageTabSession:
		if store.GetSession().Session == nil {
			lines = []usageLine{{Text: "No active session.", Style: lineDim}}
		} else {
			lines = sessionLines(store.GetUsage(), now)
		}
	case tab() == usageTabQuotas:
		switch {
		case fetch().key != currentKey || fetch().loading:
			lines = []usageLine{{Text: "Loading quotas...", Style: lineDim}}
		case fetch().err != "":
			lines = []usageLine{{Text: "Could not load quotas: " + fetch().err, Style: lineWarn}}
		default:
			lines = quotaLines(
				fetch().quotas,
				store.GetQuota(),
				store.GetSession().ActiveModelID(),
				now,
				quotaOptions{Width: viewWidth, ShowModels: showModels()},
			)
		}
	case fetch().key != currentKey || fetch().loading:
		lines = []usageLine{{Text: "Loading usage...", Style: lineDim}}
	case fetch().err != "":
		lines = []usageLine{{Text: "Could not load usage: " + fetch().err, Style: lineWarn}}
	default:
		lines = summaryLines(*fetch().summary, usageRanges[rangeIdx()].Label)
	}

	summary := tab() == usageTabWorkspace || tab() == usageTabGlobal
	meta := ""
	var actions []uicomponents.KeyAction
	if summary {
		meta = fmt.Sprintf("group: %s · range: %s", group, usageRanges[rangeIdx()].Key)
		selectGroup := func(i int) {
			setGroup(i)
			setOffset(0)
		}
		actions = append(actions, uicomponents.KeyAction{
			Keys: []string{"g"}, Hint: "g/m/p/a/u/i/d", Label: "Group by",
			OnPress: func() { selectGroup((groupIdx() + 1) % len(usageGroupKeys)) },
		})
		for i, g := range usageGroupKeys {
			actions = append(actions, uicomponents.KeyAction{
				Keys: []string{g.Key}, OnPress: func() { selectGroup(i) },
			})
		}
	}
	if tab() == usageTabQuotas {
		label := "Models"
		if showModels() {
			label = "Hide models"
		}
		actions = append(actions, uicomponents.KeyAction{
			Keys: []string{"m"}, Label: label,
			OnPress: func() {
				setShowModels(!showModels())
				setOffset(0)
			},
		})
	}
	return reportPanel(reportPanelProps{
		Title:     "Usage",
		Tabs:      usageTabs,
		Active:    tab(),
		Meta:      meta,
		Lines:     lines,
		Offset:    offset(),
		SetOffset: setOffset,
		SetTab:    setTab,
		Actions:   actions,
		ShowRange: summary,
		NextRange: func() { setRangeIdx((rangeIdx() + 1) % len(usageRanges)) },
		Refresh:   func() { setTick(tick() + 1) },
		Close:     func() { store.RemoveSlot(props.slotID) },
	})
})

type reportPanelProps struct {
	Title     string
	Tabs      []uicomponents.TabItem
	Active    usageTab
	Meta      string
	Lines     []usageLine
	Offset    int
	SetOffset func(int)
	SetTab    func(usageTab)
	Actions   []uicomponents.KeyAction
	ShowRange bool
	NextRange func()
	Refresh   func()
	Close     func()
}

type reportPanelStyles struct {
	Body, Label style.Style
	Lines       []style.Style
}

func newReportPanelStyles(s *theme.Scheme) reportPanelStyles {
	common := styles.NewCommon(s)
	label := style.S().
		Foreground(s.Color.Icon.Primary).
		WhiteSpace(style.WhiteSpacePre)
	return reportPanelStyles{
		Body:  common.Column.MarginBottom(1),
		Label: label,
		Lines: []style.Style{
			lineNormal:  common.Primary,
			lineHeading: label.Bold(true),
			lineDim:     common.Secondary,
			lineMuted:   common.Muted,
			lineTrack:   style.S().Foreground(s.Color.Text.Disabled),
			lineAccent:  common.Accent,
			lineWarn:    common.Warning,
			lineDanger:  common.Error,
			lineGood:    common.Success,
		},
	}
}

var reportPanel = trail.FC("ReportPanel", func(p reportPanelProps) trail.Node {
	st := styles.Use(newReportPanelStyles)
	maxOffset := max(0, len(p.Lines)-usageViewRows)
	pos := min(p.Offset, maxOffset)
	end := min(len(p.Lines), pos+usageViewRows)
	scroll := func(delta int) func() {
		return func() { p.SetOffset(min(maxOffset, max(0, pos+delta))) }
	}
	actions := uicomponents.TabKeyActions(len(p.Tabs), int(p.Active), func(i int) {
		p.SetTab(usageTab(i))
		p.SetOffset(0)
	}, false)
	for i, action := range actions {
		switch action.Keys[0] {
		case "tab":
			actions[i].Keys = append(action.Keys, "right", "l")
			actions[i].Hint, actions[i].Label = "←/→", "Tab"
		case "shift+tab":
			actions[i].Keys = append(action.Keys, "left", "h")
		}
	}
	actions = append(actions,
		uicomponents.KeyAction{Keys: []string{"up", "k", "ctrl+p"}, Hint: "↑/↓", Label: "Scroll", OnPress: scroll(-1)},
		uicomponents.KeyAction{Keys: []string{"down", "j", "ctrl+n"}, OnPress: scroll(1)},
		uicomponents.KeyAction{Keys: []string{"pgup", "b", "ctrl+u", "ctrl+b"}, OnPress: scroll(-usageViewRows)},
		uicomponents.KeyAction{Keys: []string{"pgdown", "space", "f", "ctrl+d", "ctrl+f"}, OnPress: scroll(usageViewRows)},
	)
	actions = append(actions, p.Actions...)
	rangeLabel := ""
	if p.ShowRange {
		rangeLabel = "Range"
	}
	actions = append(actions,
		uicomponents.KeyAction{Keys: []string{"t"}, Label: rangeLabel, OnPress: func() {
			p.NextRange()
			p.SetOffset(0)
		}},
		uicomponents.KeyAction{Keys: []string{"r"}, Label: "Refresh", OnPress: p.Refresh},
		uicomponents.KeyAction{Keys: []string{"esc", "escape", "q"}, Label: "Close", OnPress: p.Close},
	)
	bindings := uicomponents.UseKeyMap(actions, true)
	return uicomponents.Panel(uicomponents.PanelProps{
		Header: renderUsageLine(st, usageLine{Text: p.Title, Style: lineHeading}), Bindings: bindings,
	},
		uicomponents.TabBar(uicomponents.TabBarProps{
			Tabs: p.Tabs, Active: int(p.Active), Meta: p.Meta, Numbered: true, MarginBottom: 1,
		}),
		trail.Box(trail.BoxProps{Style: st.Body},
			kitex.Map(p.Lines[pos:end], func(l usageLine, _ int) trail.Node { return renderUsageLine(st, l) }),
			kitex.If(maxOffset > 0, func() trail.Node {
				return renderUsageLine(st, usageLine{
					Text:  fmt.Sprintf("lines %d-%d of %d", pos+1, end, len(p.Lines)),
					Style: lineMuted,
				})
			}),
		),
	)
})

func renderUsageLine(st reportPanelStyles, l usageLine) trail.Node {
	span := func(text string, face style.Style) trail.Node {
		return trail.Span(trail.SpanProps{Style: face.WhiteSpace(style.WhiteSpacePre)}, trail.Text(text))
	}
	if len(l.Spans) > 0 {
		return trail.Box(trail.BoxProps{}, kitex.Map(l.Spans, func(sp usageSpan, _ int) trail.Node {
			return span(sp.Text, st.Lines[sp.Style].Bold(sp.Bold || sp.Style == lineHeading))
		}))
	}
	text := cmp.Or(l.Text, " ")
	if l.LabelLen > 0 && l.LabelLen <= len(text) {
		return trail.Box(trail.BoxProps{}, span(text[:l.LabelLen], st.Label), span(text[l.LabelLen:], st.Lines[l.Style]))
	}
	return trail.Box(trail.BoxProps{}, span(text, st.Lines[l.Style]))
}

// quotaClient fetches workspace quota snapshots for the Quotas tab.
type quotaClient interface {
	GetQuotaSnapshots(ctx context.Context) (*api.QuotaSnapshotResponse, error)
}

// fetchQuotaSnapshots reads the live multi-bucket quota snapshots across all
// quota-capable providers in the workspace.
func fetchQuotaSnapshots(ctx context.Context, c quotaClient) (*api.QuotaSnapshotResponse, error) {
	return c.GetQuotaSnapshots(ctx)
}
