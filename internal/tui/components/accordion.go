package components

import (
	"image/color"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/tui/components/icon"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

type accordionState struct {
	expanded    func() bool
	setExpanded func(bool)
	color       PaperColor
}

var accordionCtx = kitex.CreateContext[*accordionState](nil)

// AccordionProps defines the properties for the Accordion component.
type AccordionProps struct {
	// Color specifies the color variant of the accordion.
	Color PaperColor
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

var (
	// AccordionBaseStyle is the base style for the accordion container.
	AccordionBaseStyle = style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexColumn).
		Width(style.Percent(100))
)

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
		color:       props.Color,
	}

	// Lookup summary and details in children to organize layout.
	var summary, details kitex.Node
	var unpack func(n kitex.Node)
	unpack = func(n kitex.Node) {
		if n == nil {
			return
		}
		switch n.TagName() {
		case "AccordionSummary":
			summary = n
		case "AccordionDetails":
			details = n
		case "Fragment", "Map", "If", "Else":
			for _, c := range n.Children() {
				unpack(c)
			}
		}
	}

	for _, child := range props.Children {
		unpack(child)
	}

	return accordionCtx.Provider(state,
		Paper(PaperProps{
			Color:   props.Color,
			Variant: props.Variant,
			Style:   AccordionBaseStyle.Merge(props.Style),
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
	// HideExpandIcon disables rendering the default chevron icon.
	HideExpandIcon bool
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

	t := theme.UseTheme()
	btnStyle := style.S().
		Width(style.Percent(100)).
		JustifyContent(style.JustifyStart)

	if t != nil {
		var fgColor color.Color
		switch state.color {
		case PaperPrimary, PaperSecondary, PaperTertiary, PaperSuccess, PaperInfo, PaperError:
			fgColor = t.Color.Text.InversePrimary
		default:
			fgColor = t.Color.Text.Primary
		}
		btnStyle = btnStyle.Foreground(fgColor)
	}

	btnStyle = btnStyle.Merge(props.Style)

	var startIcon kitex.Node
	if !props.HideExpandIcon {
		startIcon = kitex.IfElse(state.expanded(), icon.ChevronDown, icon.ChevronRight)
	}

	return Button(ButtonProps{
		Variant: ButtonText,
		Style:   btnStyle,
		OnClick: func() {
			state.setExpanded(!state.expanded())
		},
		StartIcon: startIcon,
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
