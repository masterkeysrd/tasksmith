package theme

import (
	"charm.land/lipgloss/v2"
	"github.com/masterkeysrd/tasksmith/api"
)

// Shared semantic Lip Gloss styles for the TaskSmith REPL interface.
var (
	BannerTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#7C3AED"))

	BannerMetaStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#94A3B8"))

	StatusBarStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#64748B"))

	ModeBadgeDefaultStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#38BDF8"))

	ModeBadgeAcceptEditsStyle = lipgloss.NewStyle().
					Bold(true).
					Foreground(lipgloss.Color("#34D399"))

	ModeBadgePlanStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#FBBF24"))

	ModeBadgeAskStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#FB923C"))

	ModeBadgeAutoStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#F87171"))

	UserPromptStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#38BDF8"))

	UserMsgStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#E2E8F0"))

	AssistantMsgStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#F8FAFC"))

	ReasoningStyle = lipgloss.NewStyle().
			Italic(true).
			Foreground(lipgloss.Color("#94A3B8"))

	ToolCallStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#A78BFA"))

	ToolResultStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#94A3B8"))

	ErrorStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#F87171"))

	DiffAddStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#4ADE80"))

	DiffDelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F87171"))

	DiffHunkStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#22D3EE"))

	DiffHeaderStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#CBD5E1"))

	PermCardBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("#F59E0B")).
				Padding(0, 1)

	PermHeaderStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FBBF24"))

	ActionBadgeStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#38BDF8"))

	SafetySafeStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#4ADE80"))

	SafetyWarnStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FBBF24"))

	SafetyDangerStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#F87171"))

	OptionSelectedStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#38BDF8"))

	OptionNormalStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#94A3B8"))

	AllowDecisionStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#4ADE80"))

	RejectDecisionStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#F87171"))
)

// ModeBadgeStyle returns the badge style corresponding to a permission mode.
func ModeBadgeStyle(mode api.PermissionMode) lipgloss.Style {
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
func SafetyBadgeStyle(safety api.SafetyLevel) lipgloss.Style {
	switch safety {
	case api.SafetySafe:
		return SafetySafeStyle
	case api.SafetyUnsafe:
		return SafetyDangerStyle
	default:
		return SafetyWarnStyle
	}
}
