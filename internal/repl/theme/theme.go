package theme

import (
	"image/color"

	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
)

// Semantic color palette constants for the TaskSmith REPL.
var (
	ColorPurple      = color.RGBA{R: 0x7C, G: 0x3A, B: 0xED, A: 0xFF}
	ColorMuted       = color.RGBA{R: 0x94, G: 0xA3, B: 0xB8, A: 0xFF}
	ColorStatus      = color.RGBA{R: 0x64, G: 0x74, B: 0x8B, A: 0xFF}
	ColorDimmed      = color.RGBA{R: 0x4B, G: 0x55, B: 0x63, A: 0xFF}
	ColorCyan        = color.RGBA{R: 0x38, G: 0xBD, B: 0xF8, A: 0xFF}
	ColorGreen       = color.RGBA{R: 0x34, G: 0xD3, B: 0x99, A: 0xFF}
	ColorYellow      = color.RGBA{R: 0xFB, G: 0xBF, B: 0x24, A: 0xFF}
	ColorOrange      = color.RGBA{R: 0xFB, G: 0x92, B: 0x3C, A: 0xFF}
	ColorRed         = color.RGBA{R: 0xF8, G: 0x71, B: 0x71, A: 0xFF}
	ColorTextUser    = color.RGBA{R: 0xE2, G: 0xE8, B: 0xF0, A: 0xFF}
	ColorTextAsst    = color.RGBA{R: 0xF8, G: 0xFA, B: 0xFC, A: 0xFF}
	ColorToolCall    = color.RGBA{R: 0xA7, G: 0x8B, B: 0xFA, A: 0xFF}
	ColorDiffAdd     = color.RGBA{R: 0x4A, G: 0xDE, B: 0x80, A: 0xFF}
	ColorDiffDel     = color.RGBA{R: 0xF8, G: 0x71, B: 0x71, A: 0xFF}
	ColorDiffHunk    = color.RGBA{R: 0x22, G: 0xD3, B: 0xEE, A: 0xFF}
	ColorDiffHdr     = color.RGBA{R: 0xCB, G: 0xD5, B: 0xE1, A: 0xFF}
	ColorBorderAmber = color.RGBA{R: 0xF5, G: 0x9E, B: 0x0B, A: 0xFF}
	ColorBorderGray  = color.RGBA{R: 0x4B, G: 0x55, B: 0x63, A: 0xFF}
)

// Semantic Kite styles for the TaskSmith REPL interface.
var (
	BannerTitleStyle = style.S().Bold(true).Foreground(ColorPurple)
	BannerMetaStyle  = style.S().Foreground(ColorMuted)
	StatusBarStyle   = style.S().Foreground(ColorStatus)
	DimmedStyle      = style.S().Foreground(ColorDimmed)

	UserBadgeStyle   = style.S().Bold(true).Foreground(ColorCyan)
	AgentBadgeStyle  = style.S().Bold(true).Foreground(ColorPurple)
	ToolBadgeStyle   = style.S().Foreground(ColorToolCall)
	ToolSuccessStyle = style.S().Foreground(ColorGreen)
	ToolErrorStyle   = style.S().Bold(true).Foreground(ColorRed)
	ErrorBadgeStyle  = style.S().Bold(true).Foreground(ColorRed)

	PickerTitleStyle     = style.S().Bold(true).Foreground(ColorPurple)
	PickerSelectedStyle  = style.S().Bold(true).Foreground(ColorCyan)
	PickerOptionStyle    = style.S().Foreground(ColorMuted)
	PickerTopBorderStyle = style.S().Border(style.SingleBorder().Top(true).Bottom(false).Left(false).Right(false).Color(ColorBorderGray))
	ComposerBorderStyle  = style.S().Border(style.SingleBorder().Top(true).Bottom(true).Left(false).Right(false).Color(ColorBorderGray))

	ApprovalBoxStyle   = style.S().Border(style.RoundedBorder().Color(ColorBorderAmber)).Padding(0, 1)
	ApprovalTitleStyle = style.S().Bold(true).Foreground(ColorYellow)

	ModeBadgeDefaultStyle     = style.S().Bold(true).Foreground(ColorCyan)
	ModeBadgeAcceptEditsStyle = style.S().Bold(true).Foreground(ColorGreen)
	ModeBadgePlanStyle        = style.S().Bold(true).Foreground(ColorYellow)
	ModeBadgeAskStyle         = style.S().Bold(true).Foreground(ColorOrange)
	ModeBadgeAutoStyle        = style.S().Bold(true).Foreground(ColorRed)

	UserPromptStyle   = style.S().Bold(true).Foreground(ColorCyan)
	UserMsgStyle      = style.S().Bold(true).Foreground(ColorTextUser)
	AssistantMsgStyle = style.S().Foreground(ColorTextAsst)
	ReasoningStyle    = style.S().Italic(true).Foreground(ColorMuted)
	ToolCallStyle     = style.S().Foreground(ColorToolCall)
	ToolResultStyle   = style.S().Foreground(ColorMuted)
	ErrorStyle        = style.S().Bold(true).Foreground(ColorRed)

	DiffAddStyle    = style.S().Foreground(ColorDiffAdd)
	DiffDelStyle    = style.S().Foreground(ColorDiffDel)
	DiffHunkStyle   = style.S().Foreground(ColorDiffHunk)
	DiffHeaderStyle = style.S().Bold(true).Foreground(ColorDiffHdr)

	PermCardBorderStyle = style.S().Border(style.RoundedBorder().Color(ColorBorderAmber)).Padding(0, 1)
	PermHeaderStyle     = style.S().Bold(true).Foreground(ColorYellow)
	ActionBadgeStyle    = style.S().Bold(true).Foreground(ColorCyan)
	SafetySafeStyle     = style.S().Bold(true).Foreground(ColorDiffAdd)
	SafetyWarnStyle     = style.S().Bold(true).Foreground(ColorYellow)
	SafetyDangerStyle   = style.S().Bold(true).Foreground(ColorRed)

	OptionSelectedStyle = style.S().Bold(true).Foreground(ColorCyan)
	OptionNormalStyle   = style.S().Foreground(ColorMuted)

	AllowDecisionStyle  = style.S().Bold(true).Foreground(ColorDiffAdd)
	RejectDecisionStyle = style.S().Bold(true).Foreground(ColorRed)
)

// ModeBadgeStyle returns the badge style corresponding to a permission mode.
func ModeBadgeStyle(mode api.PermissionMode) style.Style {
	switch mode {
	case api.ModeAcceptEdits:
		return ModeBadgeAcceptEditsStyle
	case api.ModePlan:
		return ModeBadgePlanStyle
	case api.ModeAsk:
		return ModeBadgeAskStyle
	case api.ModeAuto:
		return ModeBadgeAutoStyle
	default:
		return ModeBadgeDefaultStyle
	}
}

// SafetyBadgeStyle returns the badge style corresponding to a safety level.
func SafetyBadgeStyle(safety api.SafetyLevel) style.Style {
	switch safety {
	case api.SafetySafe:
		return SafetySafeStyle
	case api.SafetyUnsafe:
		return SafetyDangerStyle
	default:
		return SafetyWarnStyle
	}
}
