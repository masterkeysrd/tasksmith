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
		Usage:       "/usage [session|workspace|global]",
		Description: "Browse token usage, cost, cache and tool metrics",
		Handler:     handleUsage,
	})
}

type usageTab int

const (
	usageTabSession usageTab = iota
	usageTabWorkspace
	usageTabGlobal
)

var usageTabNames = []string{"Session", "Workspace", "Global"}

func handleUsage(args []string) error {
	tab := usageTabSession
	if len(args) > 1 {
		return fmt.Errorf("usage: /usage [session|workspace|global]")
	}
	if len(args) == 1 {
		switch strings.ToLower(strings.TrimSpace(args[0])) {
		case "session":
		case "workspace":
			tab = usageTabWorkspace
		case "global":
			tab = usageTabGlobal
		default:
			return fmt.Errorf("unknown usage view %q (expected session, workspace or global)", args[0])
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

// usageFetch is the load state of the Workspace and Global tabs.
type usageFetch struct {
	key     string
	loading bool
	err     string
	summary *api.UsageSummary
}

// usageViewRows is how many lines of a usage view are visible at once.
const usageViewRows = 22

// usageBrowser renders the /usage slot: a Session, Workspace and Global tab
// with group-by and range hotkeys. All content is plain aligned text.
var usageBrowser = trail.FC("UsageBrowser", func(props usageBrowserProps) trail.Node {
	scheme := theme.UseTheme()

	tab, setTab := kitex.UseState(props.tab)
	group, setGroup := kitex.UseState(api.UsageGroupByModel)
	rangeIdx, setRangeIdx := kitex.UseState(2)
	tick, setTick := kitex.UseState(0)
	offset, setOffset := kitex.UseState(0)
	fetch, setFetch := kitex.UseState(usageFetch{})

	// Subscribing to the live totals re-renders the Session tab on every
	// usage_updated event.
	_ = store.UseUsageTotals()

	currentKey := fmt.Sprintf("%d/%s/%d/%d", tab(), group(), rangeIdx(), tick())
	kitex.UseEffectCleanup(func() func() {
		if tab() == usageTabSession {
			return nil
		}
		c := store.Client()
		if c == nil {
			setFetch(usageFetch{key: currentKey, err: "Client unavailable."})
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
		case k.MatchString("t"):
			setRangeIdx((rangeIdx() + 1) % len(usageRanges))
			setOffset(0)
		case k.MatchString("r"):
			setTick(tick() + 1)
		case k.MatchString("up", "k"):
			setOffset(max(0, pos-1))
		case k.MatchString("down", "j"):
			setOffset(min(maxOffset, pos+1))
		case k.MatchString("pgup", "b", "ctrl+u"):
			setOffset(max(0, pos-usageViewRows))
		case k.MatchString("pgdown", "space", "f", "ctrl+d"):
			setOffset(min(maxOffset, pos+usageViewRows))
		default:
			if tab() == usageTabSession {
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
		row(style.S().WhiteSpace(style.WhiteSpacePre).Bold(true).Foreground(scheme.Color.Icon.Primary), "Usage"),
		row(style.S().WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Text.Secondary),
			usageTabBar(tab(), group(), usageRanges[rangeIdx()])),
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
		{Key: "r", Label: "Refresh"},
	}
	if tab() != usageTabSession {
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

// usageTabBar renders the tab strip: the active tab in brackets, plus the
// active group-by and range for the Workspace and Global tabs.
func usageTabBar(active usageTab, by api.UsageGroupBy, rng usageRange) string {
	var parts []string
	for i, name := range usageTabNames {
		label := fmt.Sprintf("%d %s", i+1, name)
		if usageTab(i) == active {
			label = "[" + label + "]"
		}
		parts = append(parts, label)
	}
	bar := strings.Join(parts, "  ")
	if active != usageTabSession {
		bar += fmt.Sprintf("    group: %s · range: %s", by, rng.Key)
	}
	return bar
}
