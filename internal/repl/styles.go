package repl

import (
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/masterkeysrd/tasksmith/api"
)

// State represents the interactive lifecycle phase of the REPL model.
type State int

const (
	// StateIdle indicates the REPL is waiting for user prompt or slash command input.
	StateIdle State = iota
	// StateStreaming indicates the REPL is streaming an active assistant turn.
	StateStreaming
	// StateApproval indicates the REPL is paused awaiting user resolution of a tool permission request.
	StateApproval
)

// String returns the human-readable name of the [State].
func (s State) String() string {
	switch s {
	case StateIdle:
		return "idle"
	case StateStreaming:
		return "streaming"
	case StateApproval:
		return "approval"
	default:
		return "unknown"
	}
}

var (
	bannerTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#7C3AED"))

	bannerMetaStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#94A3B8"))

	statusBarStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#64748B"))

	modeBadgeDefaultStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#38BDF8"))

	modeBadgeAcceptEditsStyle = lipgloss.NewStyle().
					Bold(true).
					Foreground(lipgloss.Color("#34D399"))

	modeBadgePlanStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#FBBF24"))

	modeBadgeAskStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#FB923C"))

	modeBadgeAutoStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#F87171"))

	userPromptStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#38BDF8"))

	userMsgStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#E2E8F0"))

	assistantMsgStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#F8FAFC"))

	reasoningStyle = lipgloss.NewStyle().
			Italic(true).
			Foreground(lipgloss.Color("#94A3B8"))

	toolCallStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#A78BFA"))

	toolResultStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#94A3B8"))

	errorStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#F87171"))

	diffAddStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#4ADE80"))

	diffDelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F87171"))

	diffHunkStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#22D3EE"))

	diffHeaderStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#CBD5E1"))

	permCardBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("#F59E0B")).
				Padding(0, 1)

	permHeaderStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FBBF24"))

	actionBadgeStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#38BDF8"))

	safetySafeStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#4ADE80"))

	safetyWarnStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FBBF24"))

	safetyDangerStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#F87171"))

	optionSelectedStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#38BDF8"))

	optionNormalStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#94A3B8"))

	allowDecisionStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#4ADE80"))

	rejectDecisionStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#F87171"))
)

func shortenHomePath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	home = filepath.Clean(home)
	cleaned := filepath.Clean(path)
	if cleaned == home {
		return "~"
	}
	if strings.HasPrefix(cleaned, home+string(filepath.Separator)) {
		return "~" + cleaned[len(home):]
	}
	return path
}

// Header holds the view-model fields for the 3-line REPL startup header.
type Header struct {
	Version  string
	Model    string
	Thinking string
	Path     string
}

func normalizeVersion(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		return "v0.1.0"
	}
	if !strings.HasPrefix(version, "v") {
		return "v" + version
	}
	return version
}

// FormatHeader renders the 3-line startup header banner for a REPL session.
func FormatHeader(h Header) string {
	version := normalizeVersion(h.Version)
	model := cmp.Or(strings.TrimSpace(h.Model), "default")
	thinking := cmp.Or(strings.TrimSpace(h.Thinking), "auto")
	path := shortenHomePath(h.Path)

	line1 := bannerTitleStyle.Render("TaskSmith") + " " + bannerMetaStyle.Render(version)
	line2 := bannerMetaStyle.Render(fmt.Sprintf("%s with %s effort", model, thinking))
	line3 := statusBarStyle.Render(path)

	return line1 + "\n" + line2 + "\n" + line3
}

// FormatStatusBar renders the contextual status footer for the active REPL state.
func FormatStatusBar(state State, sess *api.Session, mode api.PermissionMode) string {
	if mode == "" {
		mode = api.ModeDefault
	}

	modeBadge := styleForMode(mode).Render(fmt.Sprintf("mode: %s", mode))

	var sessionInfo string
	if sess != nil {
		parts := make([]string, 0, 3)
		if sess.ID != "" {
			parts = append(parts, fmt.Sprintf("session: %s", sess.ID))
		}
		if sess.Agent != "" {
			parts = append(parts, fmt.Sprintf("agent: %s", sess.Agent))
		}
		if sess.Model != "" {
			parts = append(parts, fmt.Sprintf("model: %s", sess.Model))
		}
		sessionInfo = strings.Join(parts, " • ")
	}

	hint := stateHint(state)
	segments := []string{modeBadge}
	if sessionInfo != "" {
		segments = append(segments, statusBarStyle.Render(sessionInfo))
	}
	segments = append(segments, statusBarStyle.Render(hint))

	return strings.Join(segments, statusBarStyle.Render(" │ "))
}

func styleForMode(mode api.PermissionMode) lipgloss.Style {
	switch mode {
	case api.ModeAcceptEdits:
		return modeBadgeAcceptEditsStyle
	case api.ModePlan:
		return modeBadgePlanStyle
	case api.ModeAsk:
		return modeBadgeAskStyle
	case api.ModeAuto:
		return modeBadgeAutoStyle
	default:
		return modeBadgeDefaultStyle
	}
}

func stateHint(state State) string {
	switch state {
	case StateStreaming:
		return "streaming • esc to cancel"
	case StateApproval:
		return "permission required • ↑/↓ select • enter confirm • r feedback"
	default:
		return "shift+tab cycle mode • /help commands"
	}
}

// FormatUserPrompt formats a submitted user prompt line for scrollback output.
func FormatUserPrompt(text string) string {
	return userPromptStyle.Render("❯ " + text)
}

// FormatMessage renders a persisted or streamed [api.Message] including user text,
// assistant reasoning/text, tool calls, and tool results.
func FormatMessage(msg api.Message) string {
	switch msg.Role {
	case api.RoleUser:
		text := extractTextBlocks(msg.Content)
		return userMsgStyle.Render(fmt.Sprintf("[user] %s", text))

	case api.RoleAssistant:
		return formatAssistantMessage(msg.Content)

	case api.RoleTool:
		return formatToolMessage(msg)

	default:
		text := extractTextBlocks(msg.Content)
		return statusBarStyle.Render(fmt.Sprintf("[%s] %s", msg.Role, text))
	}
}

func formatAssistantMessage(blocks []api.Block) string {
	var parts []string
	var reasoning strings.Builder
	var text strings.Builder

	for _, b := range blocks {
		switch b.Type {
		case api.BlockReasoning:
			reasoning.WriteString(b.Text)
		case api.BlockText:
			text.WriteString(b.Text)
		case api.BlockToolCall:
			if b.ToolCall != nil {
				parts = append(parts, formatToolCallBlock(*b.ToolCall))
			}
		}
	}

	var out []string
	if r := strings.TrimSpace(reasoning.String()); r != "" {
		out = append(out, reasoningStyle.Render(fmt.Sprintf("[reasoning] %s", r)))
	}
	if t := text.String(); t != "" {
		out = append(out, assistantMsgStyle.Render(fmt.Sprintf("[assistant] %s", t)))
	}
	out = append(out, parts...)

	if len(out) == 0 {
		return assistantMsgStyle.Render("[assistant] ")
	}
	return strings.Join(out, "\n")
}

func formatToolCallBlock(tc api.ToolCall) string {
	if len(tc.Args) == 0 {
		return toolCallStyle.Render(fmt.Sprintf("[tool_call] %s (%s)", tc.Name, tc.ID))
	}
	raw, err := json.Marshal(tc.Args)
	if err != nil {
		return toolCallStyle.Render(fmt.Sprintf("[tool_call] %s (%s)", tc.Name, tc.ID))
	}
	return toolCallStyle.Render(fmt.Sprintf("[tool_call] %s (%s) %s", tc.Name, tc.ID, string(raw)))
}

func formatToolMessage(msg api.Message) string {
	text := extractTextBlocks(msg.Content)
	name := msg.Name
	if name == "" {
		name = msg.ToolCallID
	}
	prefix := "[tool]"
	if msg.IsError {
		prefix = "[tool:error]"
	}
	if name != "" {
		line := fmt.Sprintf("%s %s: %s", prefix, name, text)
		if msg.IsError {
			return errorStyle.Render(line)
		}
		return toolResultStyle.Render(line)
	}
	line := fmt.Sprintf("%s %s", prefix, text)
	if msg.IsError {
		return errorStyle.Render(line)
	}
	return toolResultStyle.Render(line)
}

func extractTextBlocks(blocks []api.Block) string {
	var sb strings.Builder
	for _, b := range blocks {
		if b.Type == api.BlockText {
			sb.WriteString(b.Text)
		}
	}
	return sb.String()
}

// FormatDiff styles a unified diff string with green additions, red deletions,
// and cyan hunk headers.
func FormatDiff(diffText string) string {
	if diffText == "" {
		return ""
	}
	lines := strings.Split(diffText, "\n")
	styled := make([]string, len(lines))
	for i, line := range lines {
		styled[i] = styleDiffLine(line)
	}
	return strings.Join(styled, "\n")
}

func styleDiffLine(line string) string {
	if strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") {
		return diffHeaderStyle.Render(line)
	}
	if strings.HasPrefix(line, "@@") {
		return diffHunkStyle.Render(line)
	}
	if strings.HasPrefix(line, "+") {
		return diffAddStyle.Render(line)
	}
	if strings.HasPrefix(line, "-") {
		return diffDelStyle.Render(line)
	}
	return line
}

// FormatPermissionCard renders a bordered approval card for a pending [api.PermissionRequest].
func FormatPermissionCard(perm api.PermissionRequest, cursor int, feedbackMode bool, feedbackText string) string {
	var rows []string

	actionBadge := actionBadgeStyle.Render(strings.ToUpper(string(perm.Action)))
	safetyBadge := styleForSafety(perm.Safety).Render(strings.ToUpper(string(perm.Safety)))
	header := permHeaderStyle.Render("[Permission Required]") + " " + actionBadge + " • " + safetyBadge
	if perm.ToolName != "" {
		header += " • " + toolCallStyle.Render(perm.ToolName)
	}
	rows = append(rows, header)

	if perm.Summary != "" {
		rows = append(rows, fmt.Sprintf("Summary: %s", perm.Summary))
	}
	if perm.Target != "" {
		rows = append(rows, fmt.Sprintf("Target:  %s", perm.Target))
	}
	if perm.Diff != "" {
		rows = append(rows, FormatDiff(strings.TrimRight(perm.Diff, "\n")))
	}

	options := []string{
		"[y] Allow once",
		"[a] Allow for session",
		"[n] Reject",
	}
	for i, opt := range options {
		if i == cursor {
			rows = append(rows, optionSelectedStyle.Render("❯ "+opt))
			continue
		}
		rows = append(rows, optionNormalStyle.Render("  "+opt))
	}

	rows = append(rows, formatPermissionFooter(feedbackMode, feedbackText))

	return permCardBorderStyle.Render(strings.Join(rows, "\n"))
}

func formatPermissionFooter(feedbackMode bool, feedbackText string) string {
	if feedbackMode {
		return permHeaderStyle.Render("Feedback: ") + feedbackText + "█ " + statusBarStyle.Render("(enter to submit, esc to cancel)")
	}
	return statusBarStyle.Render("↑/↓ or j/k to select • enter to confirm • r to reject with feedback")
}

func styleForSafety(safety api.SafetyLevel) lipgloss.Style {
	switch safety {
	case api.SafetySafe:
		return safetySafeStyle
	case api.SafetyUnsafe:
		return safetyDangerStyle
	default:
		return safetyWarnStyle
	}
}

// FormatError styles an error message for scrollback display.
func FormatError(msg string) string {
	return errorStyle.Render("error: " + msg)
}

// FormatDecisionSummary renders a 1-line summary of a resolved permission request.
func FormatDecisionSummary(perm api.PermissionRequest, decision api.PermissionDecision) string {
	target := perm.Summary
	if target == "" {
		target = perm.ToolName
	}
	switch decision.Kind {
	case api.DecisionAllowOnce:
		return allowDecisionStyle.Render(fmt.Sprintf("✓ Allowed once: %s", target))
	case api.DecisionAllowSession:
		return allowDecisionStyle.Render(fmt.Sprintf("✓ Allowed for session: %s", target))
	default:
		if decision.Feedback != "" {
			return rejectDecisionStyle.Render(fmt.Sprintf("✗ Rejected: %s (%s)", target, decision.Feedback))
		}
		return rejectDecisionStyle.Render(fmt.Sprintf("✗ Rejected: %s", target))
	}
}

// FormatCommandSuggestions renders inline slash-command autocomplete hints.
func FormatCommandSuggestions(cmds []Command) string {
	if len(cmds) == 0 {
		return ""
	}
	var lines []string
	for _, cmd := range cmds {
		usage := cmd.Usage
		if usage == "" {
			usage = "/" + cmd.Name
		}
		lines = append(lines, statusBarStyle.Render(fmt.Sprintf("  %-38s %s", usage, cmd.Description)))
	}
	return strings.Join(lines, "\n")
}
