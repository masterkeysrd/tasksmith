package components

import (
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
)

type tabsState struct {
	value    any
	setValue func(any)
}

var tabsCtx = kitex.CreateContext[*tabsState](nil)

// TabsProps defines the properties for the Tabs component.
type TabsProps struct {
	// Value is the currently active tab value (controlled).
	Value any
	// DefaultValue is the initial active tab value (uncontrolled).
	DefaultValue any
	// Color specifies the color variant of the tabs.
	Color PaperColor
	// Separator is an optional node rendered between the tab list and
	// the panels (e.g., a divider).
	Separator kitex.Node
	// OnChange is triggered when the active tab changes.
	OnChange func(any)
	// Style allows passing additional style overrides.
	Style style.Style
	// Children should contain Tab and TabPanel components.
	Children []kitex.Node
}

var (
	TabsBaseStyle = style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexColumn).
			Width(style.Percent(100)).
			Height(style.Percent(100))

	TabListStyle = style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexRow).
			AlignItems(style.AlignCenter).
			Gap(2).
			Padding(0, 1)
)

// Tabs is a top-level component that manages a set of Tab and TabPanel nodes.
var Tabs = kitex.FCC("Tabs", func(props TabsProps) kitex.Node {
	active, setActive := kitex.UseState(props.DefaultValue)

	current := active()
	if props.Value != nil {
		current = props.Value
	}

	toggle := func(val any) {
		if props.Value == nil {
			setActive(val)
		}
		if props.OnChange != nil {
			props.OnChange(val)
		}
	}

	state := &tabsState{
		value:    current,
		setValue: toggle,
	}

	factor := 1
	if props.Separator != nil {
		factor = 2
	}
	tabs := make([]kitex.Node, 0, len(props.Children)*factor)
	panels := make([]kitex.Node, 0, len(props.Children))

	var unpack func(n kitex.Node)
	unpack = func(n kitex.Node) {
		if n == nil {
			return
		}

		tag := n.TagName()
		switch tag {
		case "Tab":
			tabs = append(tabs, n)
			if props.Separator != nil {
				tabs = append(tabs, props.Separator)
			}
		case "TabPanel":
			panels = append(panels, n)
		case "Fragment", "Map", "If", "Else":
			for _, c := range n.Children() {
				unpack(c)
			}
		default:
			// Recurse into component nodes to find Tab/TabPanel if they are wrapped.
			// This handles FCC/FC wrapped nodes.
			for _, c := range n.Children() {
				unpack(c)
			}
			// If it's a leaf node that isn't a Tab/TabPanel, treat as part of tab list.
			if len(n.Children()) == 0 {
				tabs = append(tabs, n)
			}
		}
	}

	for _, child := range props.Children {
		unpack(child)
	}

	// Remove trailing separator if it exists.
	if props.Separator != nil && len(tabs) > 0 && tabs[len(tabs)-1] == props.Separator {
		tabs = tabs[:len(tabs)-1]
	}

	return tabsCtx.Provider(state,
		Paper(PaperProps{
			Color: props.Color,
			Style: TabsBaseStyle.Merge(props.Style),
		},
			kitex.Box(kitex.BoxProps{
				Style: TabListStyle,
			},
				tabs...,
			),
			kitex.Fragment(panels...),
		),
	)
})

// TabProps defines the properties for a single Tab trigger.
type TabProps struct {
	// Value is the unique identifier for this tab.
	Value any
	// Icon is an optional icon displayed before the label.
	Icon kitex.Node
	// Disabled indicates if the tab is interactive.
	Disabled bool
	// Color specifies the color variant of the tab.
	Color ButtonColor
	// Style allows passing additional style overrides.
	Style style.Style
	// Children is the label content.
	Children []kitex.Node
}

// Tab is a trigger component that switches the active tab in its parent Tabs.
var Tab = kitex.FCC("Tab", func(props TabProps) kitex.Node {
	state := kitex.UseContext(tabsCtx)
	if state == nil {
		return kitex.Text("Tab must be used inside Tabs")
	}

	isActive := state.value == props.Value
	return Button(ButtonProps{
		Variant:   ButtonText,
		Active:    isActive,
		Disabled:  props.Disabled,
		Color:     props.Color,
		StartIcon: props.Icon,
		Style:     props.Style,
		OnClick: func() {
			state.setValue(props.Value)
		},
	}, props.Children...)
})

// TabPanelProps defines the properties for a Tab's content area.
type TabPanelProps struct {
	// Value must match the Tab's Value to be displayed.
	Value any
	// Style allows passing additional style overrides.
	Style style.Style
	// Children is the content displayed when this tab is active.
	Children []kitex.Node
}

var (
	TabPanelStyle = style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexColumn).
		Flex(1, 1, style.Cells(0)).
		MinHeight(style.Cells(0)).
		Padding(1).
		Overflow(style.OverflowAuto)
)

// TabPanel is a content container that only renders its children when its Value matches the active Tab.
var TabPanel = kitex.FCC("TabPanel", func(props TabPanelProps) kitex.Node {
	state := kitex.UseContext(tabsCtx)
	if state == nil {
		return kitex.Text("TabPanel must be used inside Tabs")
	}

	return kitex.If(state.value == props.Value, func() kitex.Node {
		return kitex.Box(kitex.BoxProps{
			Style: TabPanelStyle.Merge(props.Style),
		}, props.Children...)
	})
})
