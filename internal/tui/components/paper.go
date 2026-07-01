package components

import (
	"image/color"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

// PaperVariant defines visual styles for the Paper component.
type PaperVariant string

const (
	// PaperDefault is a standard container with no extra decoration.
	PaperDefault PaperVariant = "default"
	// PaperOutlined is a container with a single-line border.
	PaperOutlined PaperVariant = "outlined"
)

// PaperColor defines color variants for the Paper component.
type PaperColor string

const (
	PaperBase       PaperColor = "base"
	PaperSurface    PaperColor = "surface"
	PaperHover      PaperColor = "hover"
	PaperContent    PaperColor = "content"
	PaperContentAlt PaperColor = "content_alt"
	PaperFooter     PaperColor = "footer"
	PaperPrimary    PaperColor = "primary"
	PaperSecondary  PaperColor = "secondary"
	PaperTertiary   PaperColor = "tertiary"
	PaperSuccess    PaperColor = "success"
	PaperInfo       PaperColor = "info"
	PaperError      PaperColor = "error"
)

// PaperProps defines the properties for the Paper component.
type PaperProps struct {
	// Color specifies the color variant of the paper background.
	Color PaperColor
	// Variant specifies the visual variant of the paper.
	Variant PaperVariant
	// Style allows passing additional style overrides (e.g., padding, margin).
	Style style.Style
	// Attributes contains custom DOM attributes.
	Attributes map[string]string
	// Children is the list of child nodes to render inside the paper.
	Children []kitex.Node
}

// Paper is a base layout component that provides a styled container.
// It integrates with the theme system to resolve semantic colors.
var Paper = kitex.FCC("Paper", func(props PaperProps) kitex.Node {
	t := theme.UseTheme()

	if t == nil {
		return kitex.Box(kitex.BoxProps{
			Style:      props.Style,
			Attributes: props.Attributes,
		}, props.Children...)
	}

	var bgColor color.Color
	var fgColor color.Color

	switch props.Color {
	case PaperHover:
		bgColor = t.Color.Surface.BaseHover
		fgColor = t.Color.Text.Primary
	case PaperSurface:
		bgColor = t.Color.Surface.BaseHover
		fgColor = t.Color.Text.Primary
	case PaperContent:
		bgColor = t.Color.Surface.BasePressed
		fgColor = t.Color.Text.Primary
	case PaperContentAlt:
		bgColor = t.Color.Surface.BaseFocus
		fgColor = t.Color.Text.Primary
	case PaperFooter:
		bgColor = t.Color.Surface.BaseDisabled
		fgColor = t.Color.Text.Primary
	case PaperPrimary:
		bgColor = t.Color.Surface.Primary
		fgColor = t.Color.Text.InversePrimary
	case PaperSecondary:
		bgColor = t.Color.Surface.Secondary
		fgColor = t.Color.Text.InversePrimary
	case PaperTertiary:
		bgColor = t.Color.Surface.Tertiary
		fgColor = t.Color.Text.InversePrimary
	case PaperSuccess:
		bgColor = t.Color.Surface.Success
		fgColor = t.Color.Text.InversePrimary
	case PaperInfo:
		bgColor = t.Color.Surface.Info
		fgColor = t.Color.Text.InversePrimary
	case PaperError:
		bgColor = t.Color.Surface.Error
		fgColor = t.Color.Text.InversePrimary
	default:
		bgColor = t.Color.Surface.Base
		fgColor = t.Color.Text.Primary
	}

	s := style.S().Background(bgColor).Foreground(fgColor)

	// Apply variant-specific styles
	if props.Variant == PaperOutlined {
		borderColor := fgColor
		if props.Color == "" || props.Color == PaperBase {
			borderColor = t.Color.Border.Primary
		}
		s = s.Border(style.SingleBorder().Color(borderColor))
	}

	// Merge with explicit style overrides
	s = s.Merge(props.Style)

	return kitex.Box(kitex.BoxProps{
		Style:      s,
		Attributes: props.Attributes,
	}, props.Children...)
})
