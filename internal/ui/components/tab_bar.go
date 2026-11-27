package components

import (
	"fmt"
	"strconv"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/ui/styles"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

// TabItem describes a single tab entry in [TabBar].
type TabItem struct {
	Label string
}

// TabBarProps configures the [TabBar] component.
type TabBarProps struct {
	Tabs         []TabItem
	Active       int
	Numbered     bool
	Meta         string
	MetaNode     trail.Node
	MarginBottom int
}

type tabBarStyles struct {
	Bar      style.Style
	Group    style.Style
	Active   style.Style
	Inactive style.Style
	Meta     style.Style
}

func newTabBarStyles(s *theme.Scheme) tabBarStyles {
	common := styles.NewCommon(s)
	return tabBarStyles{
		Bar: common.Row.
			Display(style.DisplayFlex).
			FlexWrap(style.FlexWrapOn).
			Gap(0, 4),
		Group: common.Row.
			Display(style.DisplayFlex).
			FlexWrap(style.FlexWrapOn).
			Gap(0, 2),
		Active: common.Accent.
			Bold(true).
			WhiteSpace(style.WhiteSpacePre),
		Inactive: common.Muted.
			WhiteSpace(style.WhiteSpacePre),
		Meta: common.Muted.
			WhiteSpace(style.WhiteSpacePreWrap),
	}
}

// TabBar renders a wrapping horizontal tab strip with the active tab
// highlighted in accent brackets and an optional muted metadata suffix.
var TabBar = trail.FC("TabBar", func(props TabBarProps) trail.Node {
	st := styles.Use(newTabBarStyles)
	if len(props.Tabs) == 0 {
		return kitex.Empty()
	}

	barStyle := st.Bar
	if props.MarginBottom > 0 {
		barStyle = barStyle.MarginBottom(props.MarginBottom)
	}

	meta := props.MetaNode
	if meta == nil && props.Meta != "" {
		meta = trail.Span(trail.SpanProps{Style: st.Meta}, trail.Text(props.Meta))
	}

	return trail.Box(trail.BoxProps{Style: barStyle},
		trail.Box(trail.BoxProps{Style: st.Group},
			kitex.Map(props.Tabs, func(tab TabItem, i int) trail.Node {
				label := tab.Label
				if props.Numbered {
					label = fmt.Sprintf("%d %s", i+1, label)
				}
				if i == props.Active {
					return trail.Span(trail.SpanProps{Style: st.Active}, trail.Text("["+label+"]"))
				}
				return trail.Span(trail.SpanProps{Style: st.Inactive}, trail.Text(label))
			}),
		),
		kitex.If(meta != nil, func() trail.Node {
			return meta
		}),
	)
})

// TabKeyActions builds [UseKeyMap]-compatible actions for cycling tabs via
// tab/shift+tab and jumping directly via number keys 1..count (up to 9).
// Only one concise hint ("tab / 1-N  Switch tab") is advertised when count > 1.
func TabKeyActions(count, active int, setActive func(int), disabled bool) []KeyAction {
	if count <= 1 || setActive == nil {
		return nil
	}

	active = ClampCursor(active, count)
	maxDirect := min(count, 9)
	hint := fmt.Sprintf("tab / 1-%d", maxDirect)

	actions := make([]KeyAction, 0, 2+maxDirect)
	actions = append(actions,
		KeyAction{
			Keys:     []string{"tab"},
			Hint:     hint,
			Label:    "Switch tab",
			Disabled: disabled,
			OnPress: func() {
				setActive((active + 1) % count)
			},
		},
		KeyAction{
			Keys:     []string{"shift+tab"},
			Disabled: disabled,
			OnPress: func() {
				setActive((active - 1 + count) % count)
			},
		},
	)
	for i := range maxDirect {
		idx := i
		actions = append(actions, KeyAction{
			Keys:     []string{strconv.Itoa(idx + 1)},
			Disabled: disabled,
			OnPress: func() {
				setActive(idx)
			},
		})
	}
	return actions
}
