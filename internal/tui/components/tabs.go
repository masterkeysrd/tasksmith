package components

import (
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
)

type tabsState struct {
	value    func() any
	setValue func(any)
}

var tabsCtx = kitex.CreateContext[*tabsState](nil)

// TabsProps defines the properties for the Tabs component.
type TabsProps struct {
	// Value is the currently active tab value (controlled).
	Value any
	// DefaultValue is the initial active tab value (uncontrolled).
	DefaultValue any
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
			FlexDirection(style.FlexColumn)

	TabListStyle = style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexRow).
			Gap(2).
			BorderBottom(true, style.BorderSingle)
)

// Tabs is a top-level component that manages a set of Tab and TabPanel nodes.
var Tabs = kitex.FCC("Tabs", func(props TabsProps) kitex.Node {
	active, setActive := kitex.UseState(props.DefaultValue)

	current := active
	if props.Value != nil {
		current = func() any { return props.Value }
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

	// Organize children into tab list and panels.
	var tabs []kitex.Node
	var panels []kitex.Node

	var unpack func(n kitex.Node)
	unpack = func(n kitex.Node) {
		if n == nil {
			return
		}
		tag := n.TagName()
		switch tag {
		case "Tab":
			tabs = append(tabs, n)
		case "TabPanel":
			panels = append(panels, n)
		case "Fragment", "Map", "If", "Else":
			for _, c := range n.Children() {
				unpack(c)
			}
		default:
			// Treat other nodes (Box, Text, etc.) as part of the tab list.
			tabs = append(tabs, n)
		}
	}

	for _, child := range props.Children {
		unpack(child)
	}

	return tabsCtx.Provider(state,
		kitex.Box(kitex.BoxProps{
			Style: TabsBaseStyle.Merge(props.Style),
		},
			kitex.Box(kitex.BoxProps{Style: TabListStyle}, tabs...),
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

	isActive := state.value() == props.Value

	return Button(ButtonProps{
		Variant:   ButtonText,
		Active:    isActive,
		Disabled:  props.Disabled,
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

// TabPanel is a content container that only renders its children when its Value matches the active Tab.
var TabPanel = kitex.FCC("TabPanel", func(props TabPanelProps) kitex.Node {
	state := kitex.UseContext(tabsCtx)
	if state == nil {
		return kitex.Text("TabPanel must be used inside Tabs")
	}

	return kitex.If(state.value() == props.Value, func() kitex.Node {
		return kitex.Box(kitex.BoxProps{
			Style: style.S().Padding(1).Merge(props.Style),
		}, props.Children...)
	})
})
