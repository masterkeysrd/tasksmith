package widgets

import (
	"fmt"
	"image/color"

	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/tui/components/icon"
	"github.com/masterkeysrd/tasksmith/internal/tui/plugin/autocomplete"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

// AutocompleteMenuProps defines properties for the AutocompleteMenu widget.
type AutocompleteMenuProps struct {
	Items         []autocomplete.Item
	SelectedIndex int
	OnSelect      func(autocomplete.Item)
	Style         style.Style
	HideIcons     bool
	HideBadges    bool
}

var (
	overlayCardBaseStyle = style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexColumn).
				Width(style.Cells(58)). // Fixed width to keep overlay stable
				Padding(0, 1)

	menuTitleStyle = style.S().
			Bold(true).
			Margin(0, 0, 1, 0)

	menuListStyle = style.S().
			ListStyleType(style.ListStyleNone).
			Padding(0).
			Margin(0)

	menuRowStyle = style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexRow).
			AlignItems(style.AlignCenter).
			Padding(0, 1).
			Gap(1).
			Height(style.Cells(1))

	// Fixed-width column styles to achieve a clean tabular alignment
	menuIconColStyle = style.S().
				Width(style.Cells(2)).
				MarginRight(1)

	menuBadgeColStyle = style.S().
				Width(style.Cells(8)).
				Bold(true).
				MarginRight(1)

	menuLabelColStyle = style.S().
				Width(style.Cells(22)).
				Bold(true).
				Overflow(style.OverflowHidden)

	menuDetailColStyle = style.S().
				Flex(1, 1, style.Cells(0)).
				Overflow(style.OverflowHidden)
)

// AutocompleteMenu renders the floating dropdown list of completion suggestions in a tabular format.
var AutocompleteMenu = kitex.FC("AutocompleteMenu", func(props AutocompleteMenuProps) kitex.Node {
	t := theme.UseTheme()

	var bg, borderColor, textCol, titleCol, detailCol color.Color
	var activeBg, activeFg, activeDetailCol color.Color

	if t != nil {
		bg = t.Color.Surface.BaseFocus
		borderColor = t.Color.Border.Primary
		textCol = t.Color.Text.Secondary
		titleCol = t.Color.Text.Secondary
		detailCol = t.Color.Text.Tertiary

		// Use BaseHover for a subtle selection block to avoid washing out text
		activeBg = t.Color.Surface.BaseHover
		activeFg = t.Color.Text.Primary
		activeDetailCol = t.Color.Text.Secondary
	} else {
		// Fallbacks from demo styles
		bg = color.RGBA{R: 24, G: 28, B: 38, A: 255}
		borderColor = color.RGBA{R: 108, G: 124, B: 171, A: 255}
		textCol = color.RGBA{R: 200, G: 210, B: 230, A: 255}
		titleCol = color.RGBA{R: 176, G: 188, B: 220, A: 255}
		detailCol = color.RGBA{R: 142, G: 151, B: 178, A: 255}
		activeBg = color.RGBA{R: 63, G: 84, B: 145, A: 255}
		activeFg = color.RGBA{R: 255, G: 255, B: 255, A: 255}
		activeDetailCol = color.RGBA{R: 210, G: 220, B: 240, A: 255}
	}

	containerStyle := overlayCardBaseStyle.
		Background(bg).
		Border(style.SingleBorder().Color(borderColor)).
		Merge(props.Style)

	return kitex.Box(kitex.BoxProps{
		Style: containerStyle,
	},
		kitex.Box(kitex.BoxProps{
			Style: menuTitleStyle.Foreground(titleCol),
		}, kitex.Text("Autocomplete")),
		kitex.IfElse(len(props.Items) == 0,
			kitex.Box(kitex.BoxProps{
				Style: style.S().Foreground(detailCol).Padding(0, 1),
			}, kitex.Text("No matches")),
			kitex.UL(kitex.ULProps{
				Style: menuListStyle,
			},
				kitex.Map(props.Items, func(item autocomplete.Item, idx int) kitex.Node {
					isSelected := idx == props.SelectedIndex

					rowStyle := menuRowStyle
					var currentLabelCol, currentDetailCol color.Color
					if isSelected {
						rowStyle = rowStyle.Background(activeBg).Foreground(activeFg)
						currentLabelCol = activeFg
						currentDetailCol = activeDetailCol
					} else {
						rowStyle = rowStyle.Foreground(textCol)
						currentLabelCol = textCol
						currentDetailCol = detailCol
					}

					// 1. Kind Icon Column (Fixed Width: 2)
					var iconNode kitex.Node
					if !props.HideIcons && t != nil {
						var iNode kitex.Node
						if item.Kind != "" {
							iNode, _ = icon.LspKindIcon(item.Kind, t)
						}
						// Always render the Box to align subsequent columns
						iconNode = kitex.Box(kitex.BoxProps{
							Style: menuIconColStyle,
						}, kitex.IfElse(iNode != nil, iNode, kitex.Text(" ")))
					}

					// 2. Category Badge Column (Fixed Width: 8)
					var badgeNode kitex.Node
					if !props.HideBadges {
						var bText string
						var badgeCol color.Color
						if item.Badge != "" {
							bText = item.Badge
							if t != nil {
								switch item.Badge {
								case "FILE":
									badgeCol = t.Color.Surface.Success
								case "LSP":
									badgeCol = t.Color.Surface.Info
								case "CMD":
									badgeCol = t.Color.Surface.Tertiary
								default:
									badgeCol = t.Color.Surface.Secondary
								}
							} else {
								badgeCol = color.RGBA{R: 123, G: 205, B: 165, A: 255}
							}
						}
						// Always render the Box to align subsequent columns
						badgeNode = kitex.Box(kitex.BoxProps{
							Style: menuBadgeColStyle.Foreground(badgeCol),
						}, kitex.Text(bText))
					}

					// 3. Name / Label Column (Fixed Width: 22)
					labelNode := kitex.Box(kitex.BoxProps{
						Style: menuLabelColStyle.Foreground(currentLabelCol),
					}, kitex.Text(item.Label))

					// 4. Sublabel / Detail Column (Flex: 1)
					detailNode := kitex.Box(kitex.BoxProps{
						Style: menuDetailColStyle.Foreground(currentDetailCol),
					}, kitex.Text(item.Sublabel))

					return kitex.LI(kitex.LIProps{
						Key:   fmt.Sprintf("item-%s-%d", item.ID, idx),
						Style: rowStyle,
						OnClick: func(e event.Event) {
							if props.OnSelect != nil {
								props.OnSelect(item)
							}
						},
					},
						// Icon
						kitex.If(iconNode != nil, func() kitex.Node {
							return iconNode
						}),
						// Category Badge
						kitex.If(badgeNode != nil, func() kitex.Node {
							return badgeNode
						}),
						// Label (Symbol/Filename)
						labelNode,
						// Detail description or directory path
						detailNode,
					)
				}),
			),
		),
	)
})
