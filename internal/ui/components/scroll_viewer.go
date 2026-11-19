package components

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/masterkeysrd/kite/dom"
	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

const (
	defaultScrollPageStep = 8
	scrollBottomOffset    = 9999
)

// ScrollOptions configures [UseScroll].
type ScrollOptions struct {
	InitialPos int
	PageStep   int
	FollowTail bool
	PageOnly   bool
	ExtraDeps  []any
	OnChange   func(pos int)
}

// ScrollState exposes the element ref, current offset, and key handler returned by [UseScroll].
type ScrollState struct {
	Ref        kitex.Ref[dom.Element]
	Pos        int
	AutoScroll bool
	SetPos     func(pos int)
	Reset      func()
	HandleKey  func(input string, k event.KeyEvent) bool
}

// UseScroll manages vertical scroll state and layout-effect synchronization for a
// scrollable [trail.Box].
func UseScroll(opts ScrollOptions) ScrollState {
	pageStep := opts.PageStep
	if pageStep <= 0 {
		pageStep = defaultScrollPageStep
	}
	initialPos := max(0, opts.InitialPos)

	pos, setPosState := kitex.UseState(initialPos)
	autoScroll, setAutoScroll := kitex.UseState(opts.FollowTail)
	boxRef := kitex.UseRef[dom.Element](nil)

	deps := make([]any, 0, 2+len(opts.ExtraDeps))
	deps = append(deps, pos(), autoScroll())
	deps = append(deps, opts.ExtraDeps...)

	kitex.UseLayoutEffect(func() {
		if el := boxRef.Current; el != nil {
			if autoScroll() {
				el.ScrollTo(0, scrollBottomOffset)
			} else {
				el.ScrollTo(0, pos())
			}
		}
	}, deps)

	setPos := func(next int) {
		if next < 0 {
			next = 0
		}
		if next != pos() {
			setPosState(next)
			if opts.OnChange != nil {
				opts.OnChange(next)
			}
		}
	}

	reset := func() {
		setAutoScroll(opts.FollowTail)
		setPos(0)
	}

	handleKey := func(input string, k event.KeyEvent) bool {
		unmodified := k.Mod&^(key.ModShift|key.ModCapsLock) == 0

		if (unmodified && (input == "G" || k.MatchString("G"))) || k.MatchString("end") {
			if opts.FollowTail {
				setAutoScroll(true)
			}
			setPos(scrollBottomOffset)
			return true
		}
		if (unmodified && (input == "g" || k.MatchString("g"))) || k.MatchString("home") {
			setAutoScroll(false)
			setPos(0)
			return true
		}

		if opts.PageOnly {
			switch {
			case k.MatchString("pgup", "ctrl+u", "ctrl+b"):
				setAutoScroll(false)
				setPos(max(0, pos()-pageStep))
				return true
			case k.MatchString("pgdown", "ctrl+d", "ctrl+f"):
				setPos(pos() + pageStep)
				return true
			default:
				return false
			}
		}

		switch {
		case k.MatchString("up", "k", "ctrl+p"):
			setAutoScroll(false)
			setPos(max(0, pos()-1))
			return true
		case k.MatchString("down", "j", "ctrl+n"):
			setPos(pos() + 1)
			return true
		case k.MatchString("pgup", "b", "ctrl+u", "ctrl+b"):
			setAutoScroll(false)
			setPos(max(0, pos()-pageStep))
			return true
		case input == " " || k.MatchString("pgdown", "space", "f", "ctrl+d", "ctrl+f"):
			setPos(pos() + pageStep)
			return true
		default:
			return false
		}
	}

	return ScrollState{
		Ref:        boxRef,
		Pos:        pos(),
		AutoScroll: autoScroll(),
		SetPos:     setPos,
		Reset:      reset,
		HandleKey:  handleKey,
	}
}

// ScrollViewerAction binds one or more keys to a custom action in [ScrollViewer].
type ScrollViewerAction struct {
	Keys    []string
	Handler func()
}

// ScrollViewerProps defines the properties for the [ScrollViewer] component.
type ScrollViewerProps struct {
	Header             []trail.Node
	HeaderMarginBottom int
	Meta               []trail.Node
	Markdown           string
	EmptyText          string
	InitialScroll      int
	OnScroll           func(pos int)
	MaxHeight          int
	OuterStyle         *style.Style
	ViewportStyle      *style.Style
	Disabled           bool
	FollowTail         bool
	ExtraDeps          []any
	OnClose            func()
	Actions            []ScrollViewerAction
	Bindings           []KeyBinding
	Children           []trail.Node
}

func matchScrollViewerKeys(input string, k event.KeyEvent, keys []string) bool {
	for _, keyName := range keys {
		if utf8.RuneCountInString(keyName) == 1 {
			if k.Mod&^(key.ModShift|key.ModCapsLock) != 0 {
				continue
			}
			if input == keyName || k.MatchString(keyName) {
				return true
			}
			continue
		}
		if k.MatchString(keyName) {
			return true
		}
	}
	return false
}

// ScrollViewer renders a full scrollable Markdown or custom-node inspector with
// an optional header (showing "  •  line +N" when scrolled), static metadata,
// custom key actions, and a bottom [KeyboardHint] bar.
var ScrollViewer = trail.FCC("ScrollViewer", func(props ScrollViewerProps) trail.Node {
	scheme := theme.UseTheme()
	winSize := trail.UseWindowSize()

	scroll := UseScroll(ScrollOptions{
		InitialPos: props.InitialScroll,
		FollowTail: props.FollowTail,
		ExtraDeps:  props.ExtraDeps,
		OnChange:   props.OnScroll,
	})

	trail.UseInput(func(input string, k event.KeyEvent) {
		if props.Disabled {
			return
		}
		for _, act := range props.Actions {
			if matchScrollViewerKeys(input, k, act.Keys) {
				if act.Handler != nil {
					act.Handler()
				}
				return
			}
		}
		if k.MatchString("esc", "escape") {
			if props.OnClose != nil {
				props.OnClose()
			}
			return
		}
		scroll.HandleKey(input, k)
	}, trail.InputOptions{IsActive: !props.Disabled})

	maxHeight := props.MaxHeight
	if maxHeight <= 0 {
		if winSize.Height > 0 {
			maxHeight = max(10, min(28, winSize.Height-6))
		} else {
			maxHeight = 18
		}
	}

	var children []trail.Node
	if len(props.Header) > 0 {
		headerItems := slices.Clone(props.Header)
		if !scroll.AutoScroll && scroll.Pos > 0 {
			mutedStyle := style.S().Foreground(scheme.Color.Text.Muted).WhiteSpace(style.WhiteSpacePreWrap)
			headerItems = append(headerItems, trail.Span(trail.SpanProps{
				Style: mutedStyle,
			}, trail.Text(fmt.Sprintf("  •  line +%d", scroll.Pos))))
		}
		headerStyle := style.S().FlexDirection(style.FlexRow).Width(style.Percent(100))
		if props.HeaderMarginBottom > 0 {
			headerStyle = headerStyle.Margin(0, 0, props.HeaderMarginBottom, 0)
		}
		children = append(children, trail.Box(trail.BoxProps{Style: headerStyle}, headerItems...))
	}

	if len(props.Meta) > 0 {
		children = append(children, props.Meta...)
	}

	var viewportContent trail.Node
	switch {
	case len(props.Children) > 0:
		viewportContent = trail.Box(trail.BoxProps{
			Style: style.S().FlexDirection(style.FlexColumn),
		}, props.Children...)
	case strings.TrimSpace(props.Markdown) != "":
		viewportContent = Markdown(MarkdownProps{Source: props.Markdown})
	case props.EmptyText != "":
		viewportContent = trail.Span(trail.SpanProps{
			Style: style.S().Foreground(scheme.Color.Text.Muted),
		}, trail.Text(props.EmptyText))
	}

	viewportStyle := style.S().
		Width(style.Percent(100)).
		MaxHeight(style.Cells(maxHeight)).
		OverflowY(style.OverflowAuto).
		ScrollbarY(true)
	if props.ViewportStyle != nil {
		viewportStyle = viewportStyle.Merge(*props.ViewportStyle)
	}

	children = append(children, trail.Box(trail.BoxProps{
		Ref:   scroll.Ref,
		Style: viewportStyle,
	}, viewportContent))

	if len(props.Bindings) > 0 {
		children = append(children, KeyboardHint(KeyboardHintProps{
			Bindings: props.Bindings,
		}))
	}

	outerStyle := style.S().
		Width(style.Percent(100)).
		FlexDirection(style.FlexColumn)
	if props.OuterStyle != nil {
		outerStyle = outerStyle.Merge(*props.OuterStyle)
	}

	return trail.Box(trail.BoxProps{Style: outerStyle}, children...)
})
