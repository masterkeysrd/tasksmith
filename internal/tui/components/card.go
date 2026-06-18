package components

import (
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
)

// CardVariant defines visual styles for the Card component.
type CardVariant string

const (
	// CardDefault is a standard card container.
	CardDefault CardVariant = "default"
	// CardOutlined is a card container with a border.
	CardOutlined CardVariant = "outlined"
)

// CardProps defines the properties for the Card component.
type CardProps struct {
	// Color specifies the color variant of the card background.
	Color PaperColor
	// Variant specifies the visual variant of the card.
	Variant CardVariant
	// Style allows passing additional style overrides.
	Style style.Style
	// Children should contain CardHeader, CardContent, or CardActions.
	Children []kitex.Node
}

var (
	// CardBaseStyle is the base style for the card container.
	CardBaseStyle = style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexColumn).
		Width(style.Percent(100))
)

// Card is a surface-level container that groups related content and actions.
// It organizes its children (Header, Content, Actions) into a consistent layout.
var Card = kitex.FCC("Card", func(props CardProps) kitex.Node {
	var header, content, actions kitex.Node

	var unpack func(n kitex.Node)
	unpack = func(n kitex.Node) {
		if n == nil {
			return
		}
		switch n.TagName() {
		case "CardHeader":
			header = n
		case "CardContent":
			content = n
		case "CardActions":
			actions = n
		case "Fragment", "Map", "If", "Else":
			for _, c := range n.Children() {
				unpack(c)
			}
		}
	}

	for _, child := range props.Children {
		unpack(child)
	}

	paperVariant := PaperDefault
	if props.Variant == CardOutlined {
		paperVariant = PaperOutlined
	}

	return Paper(PaperProps{
		Color:   props.Color,
		Variant: paperVariant,
		Style:   CardBaseStyle.Merge(props.Style),
	},
		kitex.If(header != nil, func() kitex.Node { return header }),
		kitex.If(content != nil, func() kitex.Node { return content }),
		kitex.If(actions != nil, func() kitex.Node { return actions }),
	)
})

// CardHeaderProps defines the properties for the CardHeader component.
type CardHeaderProps struct {
	// Title is the main heading of the card.
	Title kitex.Node
	// Subheader is the secondary text below the title.
	Subheader kitex.Node
	// Avatar is an optional icon or image at the start.
	Avatar kitex.Node
	// Action is an optional node (e.g. IconButton) at the end.
	Action kitex.Node
	// Style allows passing additional style overrides.
	Style style.Style
}

var (
	CardHeaderStyle = style.S().
			Display(style.DisplayFlex).
			AlignItems(style.AlignCenter).
			PaddingHorizontal(1).
			Gap(1)

	CardHeaderTextStyle = style.S().
				Flex(1).
				Display(style.DisplayFlex).
				FlexDirection(style.FlexColumn)

	CardHeaderTitleStyle = style.S().
				Bold(true)

	CardHeaderSubheaderStyle = style.S() // TODO: Add dimming style?
)

// CardHeader displays a title and optional subheader, avatar, and actions.
var CardHeader = kitex.FC("CardHeader", func(props CardHeaderProps) kitex.Node {
	return kitex.Box(kitex.BoxProps{
		Style: CardHeaderStyle.Merge(props.Style),
	},
		kitex.If(props.Avatar != nil, func() kitex.Node { return props.Avatar }),
		kitex.Box(kitex.BoxProps{Style: CardHeaderTextStyle},
			kitex.If(props.Title != nil, func() kitex.Node {
				return kitex.Box(kitex.BoxProps{Style: CardHeaderTitleStyle}, props.Title)
			}),
			kitex.If(props.Subheader != nil, func() kitex.Node {
				return kitex.Box(kitex.BoxProps{Style: CardHeaderSubheaderStyle}, props.Subheader)
			}),
		),
		kitex.If(props.Action != nil, func() kitex.Node { return props.Action }),
	)
})

// CardContentProps defines the properties for the CardContent component.
type CardContentProps struct {
	// Style allows passing additional style overrides.
	Style style.Style
	// Children is the main body content.
	Children []kitex.Node
}

var (
	CardContentStyle = style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexColumn)
)

// CardContent is the primary area for a card's content.
var CardContent = kitex.FCC("CardContent", func(props CardContentProps) kitex.Node {
	return kitex.Box(kitex.BoxProps{
		Style: CardContentStyle.Merge(props.Style),
	}, props.Children...)
})

// CardActionsProps defines the properties for the CardActions component.
type CardActionsProps struct {
	// Style allows passing additional style overrides.
	Style style.Style
	// Children are the action buttons.
	Children []kitex.Node
}

var (
	CardActionsStyle = style.S().
		Display(style.DisplayFlex).
		AlignItems(style.AlignCenter).
		Gap(1)
)

// CardActions provides a horizontal row for buttons or other interactive elements.
var CardActions = kitex.FCC("CardActions", func(props CardActionsProps) kitex.Node {
	var children []kitex.Node
	var unpack func(n kitex.Node)
	unpack = func(n kitex.Node) {
		if n == nil {
			return
		}
		switch n.TagName() {
		case "Fragment", "Map", "If", "Else":
			for _, c := range n.Children() {
				unpack(c)
			}
		default:
			children = append(children, n)
		}
	}

	for _, child := range props.Children {
		unpack(child)
	}

	return kitex.Box(kitex.BoxProps{
		Style: CardActionsStyle.Merge(props.Style),
	}, children...)
})
