package components

import (
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/tui/components/icon"
	"github.com/masterkeysrd/tasksmith/internal/tui/highlight"
)

type accordionState struct {
	expanded    func() bool
	setExpanded func(bool)
}

var accordionCtx = kitex.CreateContext[*accordionState](nil)

// AccordionProps defines the properties for the Accordion component.
type AccordionProps struct {
	// Group is the highlight group to use for theme-aware styling.
	Group highlight.Group
	// Variant specifies the visual variant of the accordion container.
	Variant PaperVariant
	// DefaultExpanded indicates if the accordion should be expanded by default (uncontrolled).
	DefaultExpanded bool
	// Expanded indicates if the accordion is expanded (controlled).
	Expanded *bool
	// OnChange is triggered when the accordion is toggled.
	OnChange func(bool)
	// Style allows passing additional style overrides.
	Style style.Style
	// Children should contain exactly one AccordionSummary and one AccordionDetails.
	Children []kitex.Node
}

// Accordion is a container component that can be expanded or collapsed.
// It coordinates state between AccordionSummary and AccordionDetails.
var Accordion = kitex.FCC("Accordion", func(props AccordionProps) kitex.Node {
	expanded, setExpanded := kitex.UseState(props.DefaultExpanded)

	isExpanded := expanded
	if props.Expanded != nil {
		isExpanded = func() bool { return *props.Expanded }
	}

	toggle := func(val bool) {
		if props.Expanded == nil {
			setExpanded(val)
		}
		if props.OnChange != nil {
			props.OnChange(val)
		}
	}

	state := &accordionState{
		expanded:    isExpanded,
		setExpanded: toggle,
	}

	// Lookup summary and details in children to organize layout.
	var summary, details kitex.Node
	for _, child := range props.Children {
		if child == nil {
			continue
		}
		switch child.TagName() {
		case "AccordionSummary":
			summary = child
		case "AccordionDetails":
			details = child
		}
	}

	return accordionCtx.Provider(state,
		Paper(PaperProps{
			Group:   props.Group,
			Variant: props.Variant,
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexColumn).
				Merge(props.Style),
		},
			kitex.If(summary != nil, func() kitex.Node {
				return summary
			}),
			kitex.If(details != nil, func() kitex.Node {
				return details
			}),
		),
	)
})

// AccordionSummaryProps defines the properties for the Accordion header.
type AccordionSummaryProps struct {
	// Group is the highlight group for the summary.
	Group highlight.Group
	// Style allows passing additional style overrides.
	Style style.Style
	// Children is the content of the summary.
	Children []kitex.Node
}

// AccordionSummary is the clickable header of an Accordion.
var AccordionSummary = kitex.FCC("AccordionSummary", func(props AccordionSummaryProps) kitex.Node {
	state := kitex.UseContext(accordionCtx)
	if state == nil {
		return kitex.Text("AccordionSummary must be used inside Accordion")
	}

	return Button(ButtonProps{
		Group:   props.Group,
		Variant: ButtonText,
		Style: style.S().
			Width(style.Percent(100)).
			JustifyContent(style.JustifyStart).
			Merge(props.Style),
		OnClick: func() {
			state.setExpanded(!state.expanded())
		},
		StartIcon: kitex.IfElse(state.expanded(), icon.ChevronDown, icon.ChevronRight),
	}, props.Children...)
})

// AccordionDetailsProps defines the properties for the Accordion content.
type AccordionDetailsProps struct {
	// Style allows passing additional style overrides.
	Style style.Style
	// Children is the content shown when the accordion is expanded.
	Children []kitex.Node
}

// AccordionDetails is the collapsible content area of an Accordion.
var AccordionDetails = kitex.FCC("AccordionDetails", func(props AccordionDetailsProps) kitex.Node {
	state := kitex.UseContext(accordionCtx)
	if state == nil {
		return kitex.Text("AccordionDetails must be used inside Accordion")
	}

	return kitex.If(state.expanded(), func() kitex.Node {
		return kitex.Box(kitex.BoxProps{
			Style: style.S().
				Padding(0, 1).
				Merge(props.Style),
		}, props.Children...)
	})
})
