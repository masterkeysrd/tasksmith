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
	variant     PaperVariant
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
		Width(style.Percent(100)).
		MinWidth(style.Percent(0))
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
		variant:     props.Variant,
	}

	// Lookup summary, preview and details in children to organize layout.
	var summary, preview, details kitex.Node
	var unpack func(n kitex.Node)
	unpack = func(n kitex.Node) {
		if n == nil {
			return
		}
		switch n.TagName() {
		case "AccordionSummary":
			summary = n
		case "AccordionPreview":
			preview = n
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
			kitex.If(preview != nil, func() kitex.Node {
				return preview
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
	// EndContent is optional content rendered on the right side of the header (only in PaperOutlined).
	EndContent kitex.Node
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
		if state.variant == PaperOutlined {
			btnStyle = btnStyle.
				Background(t.Color.Surface.BaseFocus).
				Padding(0, 1)
			// JustifyBetween only when there is end content to push to the right.
			if props.EndContent != nil {
				btnStyle = btnStyle.JustifyContent(style.JustifyBetween)
			}
		}
	}

	btnStyle = btnStyle.Merge(props.Style)

	var startIcon kitex.Node
	if !props.HideExpandIcon {
		startIcon = kitex.IfElse(state.expanded(), icon.ChevronDown, icon.ChevronRight)
	}

	children := make([]kitex.Node, 0, len(props.Children)+1)
	if startIcon != nil {
		children = append(children, startIcon)
	}
	children = append(children, props.Children...)

	return Button(ButtonProps{
		Variant: ButtonText,
		Style:   btnStyle,
		// StartIcon: startIcon,
		EndIcon: props.EndContent,
		OnClick: func() {
			state.setExpanded(!state.expanded())
		},
	}, kitex.Box(kitex.BoxProps{
		Style: style.S().Display(style.DisplayFlex).AlignItems(style.AlignCenter).Gap(1),
	}, children...))
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

// AccordionPreviewProps defines the properties for the always-visible preview area.
type AccordionPreviewProps struct {
	// Style allows passing additional style overrides.
	Style style.Style
	// Children is the content always shown regardless of expanded state.
	Children []kitex.Node
}

// AccordionPreview is always rendered — even when the accordion is collapsed.
// Use it to show a preview of the content alongside AccordionDetails for the overflow.
var AccordionPreview = kitex.FCC("AccordionPreview", func(props AccordionPreviewProps) kitex.Node {
	if kitex.UseContext(accordionCtx) == nil {
		return kitex.Text("AccordionPreview must be used inside Accordion")
	}

	return kitex.Box(kitex.BoxProps{
		Style: style.S().Padding(0, 1).Merge(props.Style),
	}, props.Children...)
})
