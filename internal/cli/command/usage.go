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
	uicomponents "github.com/masterkeysrd/tasksmith/internal/ui/components"
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

var usageTabNames = []string{"Session", "Workspace", "Global", "Quotas"}

// statsTabNames bounds the /stats browser, which has no quota windows of its
// own: quota capacity is a cost concept and lives in /usage only.
var statsTabNames = usageTabNames[:3]

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
	scheme := theme.UseTheme()
	winSize := trail.UseWindowSize()

	tab, setTab := kitex.UseState(props.tab)
	group, setGroup := kitex.UseState(api.UsageGroupByModel)
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

	currentKey := fmt.Sprintf("%d/%s/%d/%d", tab(), group(), rangeIdx(), tick())
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
		key, by, rng := currentKey, group(), usageRanges[rangeIdx()]
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
		case k.MatchString("4"):
			setTab(usageTabQuotas)
			setOffset(0)
		case k.MatchString("t"):
			setRangeIdx((rangeIdx() + 1) % len(usageRanges))
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
		default:
			if tab() == usageTabQuotas {
				if k.MatchString("m") {
					setShowModels(!showModels())
					setOffset(0)
				}
				return
			}
			if tab() != usageTabWorkspace && tab() != usageTabGlobal {
				return
			}
			for _, g := range usageGroupKeys {
				if k.MatchString(g.Key) {
					setGroup(g.By)
					setOffset(0)
					return
				}
			}
		}
	})

	border := style.SingleBorder().Top(true).Bottom(false).Left(false).Right(false).Color(scheme.Color.Border.Default)
	row := func(st style.Style, text string) trail.Node {
		return trail.Box(trail.BoxProps{}, trail.Span(trail.SpanProps{Style: st}, trail.Text(text)))
	}

	children := []trail.Node{
		row(style.S().WhiteSpace(style.WhiteSpacePre).Bold(true).Foreground(scheme.Color.Icon.Primary), "Usage"),
		renderUsageLine(scheme, usageTabBarLine(tab(), group(), usageRanges[rangeIdx()])),
		row(style.S().WhiteSpace(style.WhiteSpacePre), " "),
	}
	end := min(len(lines), pos+usageViewRows)
	for _, l := range lines[pos:end] {
		children = append(children, renderUsageLine(scheme, l))
	}
	if len(lines) > usageViewRows {
		children = append(children, row(style.S().WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Text.Muted),
			fmt.Sprintf("lines %d-%d of %d", pos+1, end, len(lines))))
	}

	bindings := []uicomponents.KeyBinding{
		{Key: "←/→", Label: "Tab"},
		{Key: "↑/↓", Label: "Scroll"},
	}
	if tab() == usageTabQuotas {
		label := "Models"
		if showModels() {
			label = "Hide models"
		}
		bindings = append(bindings, uicomponents.KeyBinding{Key: "m", Label: label})
	}
	bindings = append(bindings, uicomponents.KeyBinding{Key: "r", Label: "Refresh"})
	if tab() == usageTabWorkspace || tab() == usageTabGlobal {
		bindings = append(bindings,
			uicomponents.KeyBinding{Key: "m/p/a/u/i/d", Label: "Group by"},
			uicomponents.KeyBinding{Key: "t", Label: "Range"},
		)
	}
	bindings = append(bindings, uicomponents.KeyBinding{Key: "esc", Label: "Close"})
	children = append(children, trail.Box(trail.BoxProps{Style: style.S().Margin(1, 0, 0, 0)},
		uicomponents.KeyboardHint(uicomponents.KeyboardHintProps{Bindings: bindings})))

	return trail.Box(trail.BoxProps{
		Style: style.S().
			Border(border).
			Width(style.Percent(100)).
			FlexDirection(style.FlexColumn),
	}, children...)
})

func usageSpanStyle(scheme *theme.Scheme, st lineStyle, bold bool) style.Style {
	s := style.S().WhiteSpace(style.WhiteSpacePre)
	switch st {
	case lineHeading:
		s = s.Bold(true).Foreground(scheme.Color.Icon.Primary)
	case lineDim:
		s = s.Foreground(scheme.Color.Text.Secondary)
	case lineMuted:
		s = s.Foreground(scheme.Color.Text.Muted)
	case lineTrack:
		s = s.Foreground(scheme.Color.Text.Disabled)
	case lineAccent:
		s = s.Foreground(scheme.Color.Text.Accent)
	case lineWarn:
		s = s.Foreground(scheme.Color.Text.Warning)
	case lineDanger:
		s = s.Foreground(scheme.Color.Text.Error)
	case lineGood:
		s = s.Foreground(scheme.Color.Text.Success)
	default:
		s = s.Foreground(scheme.Color.Text.Primary)
	}
	if bold {
		s = s.Bold(true)
	}
	return s
}

// renderUsageLine turns a usageLine into a block-level trail row, honoring
// multi-span segments or leading key labels when present.
func renderUsageLine(scheme *theme.Scheme, l usageLine) trail.Node {
	if len(l.Spans) > 0 {
		nodes := make([]trail.Node, 0, len(l.Spans))
		for _, sp := range l.Spans {
			nodes = append(nodes, trail.Span(trail.SpanProps{
				Style: usageSpanStyle(scheme, sp.Style, sp.Bold),
			}, trail.Text(sp.Text)))
		}
		return trail.Box(trail.BoxProps{}, nodes...)
	}

	text := l.Text
	if text == "" {
		text = " "
	}
	st := usageSpanStyle(scheme, l.Style, false)
	if l.LabelLen <= 0 || l.LabelLen > len(text) {
		return trail.Box(trail.BoxProps{}, trail.Span(trail.SpanProps{Style: st}, trail.Text(text)))
	}
	labelStyle := style.S().WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Icon.Primary)
	return trail.Box(trail.BoxProps{},
		trail.Span(trail.SpanProps{Style: labelStyle}, trail.Text(text[:l.LabelLen])),
		trail.Span(trail.SpanProps{Style: st}, trail.Text(text[l.LabelLen:])),
	)
}

// usageTabBarLine builds the styled tab strip: the active tab highlighted in
// accent brackets, plus the active group-by and range for Workspace and Global.
func usageTabBarLine(active usageTab, by api.UsageGroupBy, rng usageRange) usageLine {
	spans := make([]usageSpan, 0, len(usageTabNames)*2+1)
	for i, name := range usageTabNames {
		if i > 0 {
			spans = append(spans, usageSpan{Text: "  ", Style: lineDim})
		}
		label := fmt.Sprintf("%d %s", i+1, name)
		if usageTab(i) == active {
			spans = append(spans, usageSpan{Text: "[" + label + "]", Style: lineAccent, Bold: true})
		} else {
			spans = append(spans, usageSpan{Text: label, Style: lineDim})
		}
	}
	if active == usageTabWorkspace || active == usageTabGlobal {
		spans = append(spans, usageSpan{
			Text:  fmt.Sprintf("    group: %s · range: %s", by, rng.Key),
			Style: lineMuted,
		})
	}
	return styledLine(lineDim, spans...)
}

// usageTabBar renders the plain-text tab strip.
func usageTabBar(active usageTab, by api.UsageGroupBy, rng usageRange) string {
	return usageTabBarLine(active, by, rng).Text
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
