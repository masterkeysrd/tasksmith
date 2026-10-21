package ui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/repl/theme"
)

// Approval manages pending tool permission requests, navigation cursor, and feedback input.
type Approval struct {
	pending       []api.PermissionRequest
	cursor        int
	feedbackMode  bool
	feedbackInput textinput.Model
}

// NewApproval constructs an initialized [Approval] component.
func NewApproval() Approval {
	fi := textinput.New()
	fi.Prompt = ""
	fi.Placeholder = "Reason for rejection..."

	return Approval{
		feedbackInput: fi,
	}
}

// Pending returns the current list of pending permission requests.
func (a Approval) Pending() []api.PermissionRequest {
	return a.pending
}

// HasPending reports whether any permission requests are waiting for decision.
func (a Approval) HasPending() bool {
	return len(a.pending) > 0
}

// SetPending replaces the queue of pending permission requests.
func (a *Approval) SetPending(perms []api.PermissionRequest) {
	a.pending = perms
	a.cursor = 0
	a.feedbackMode = false
	a.feedbackInput.Reset()
}

// Append adds permission requests to the end of the queue.
func (a *Approval) Append(perms ...api.PermissionRequest) {
	a.pending = append(a.pending, perms...)
}

// Clear clears all pending permission requests and resets state.
func (a *Approval) Clear() {
	a.pending = nil
	a.cursor = 0
	a.feedbackMode = false
	a.feedbackInput.Reset()
}

// Cursor returns the currently highlighted decision option index (0..2).
func (a Approval) Cursor() int {
	return a.cursor
}

// SetCursor explicitly sets the cursor index, clamping it to 0..2.
func (a *Approval) SetCursor(c int) {
	if c < 0 {
		c = 0
	}
	if c > 2 {
		c = 2
	}
	a.cursor = c
}

// CursorUp moves the selection cursor up.
func (a *Approval) CursorUp() {
	if a.cursor > 0 {
		a.cursor--
	}
}

// CursorDown moves the selection cursor down.
func (a *Approval) CursorDown() {
	if a.cursor < 2 {
		a.cursor++
	}
}

// FeedbackMode reports whether rejection feedback text input is active.
func (a Approval) FeedbackMode() bool {
	return a.feedbackMode
}

// FeedbackValue returns the current text in the feedback input field.
func (a Approval) FeedbackValue() string {
	return a.feedbackInput.Value()
}

// EnterFeedback switches the component into rejection feedback input mode.
func (a *Approval) EnterFeedback() {
	a.feedbackMode = true
	a.cursor = 2
	a.feedbackInput.Reset()
	a.feedbackInput.Focus()
}

// ExitFeedback cancels rejection feedback input mode.
func (a *Approval) ExitFeedback() {
	a.feedbackMode = false
	a.feedbackInput.Reset()
}

// UpdateFeedback forwards Bubble Tea messages to the rejection feedback input field.
func (a Approval) UpdateFeedback(msg tea.Msg) (Approval, tea.Cmd) {
	var cmd tea.Cmd
	a.feedbackInput, cmd = a.feedbackInput.Update(msg)
	return a, cmd
}

// PopDecision creates a [api.PermissionDecision] for the head request, removes it from the queue,
// and returns the resolved request and decision.
func (a *Approval) PopDecision(kind api.DecisionKind, feedback string) (api.PermissionRequest, api.PermissionDecision, bool) {
	if len(a.pending) == 0 {
		return api.PermissionRequest{}, api.PermissionDecision{}, false
	}

	perm := a.pending[0]
	a.pending = a.pending[1:]
	a.cursor = 0
	a.feedbackMode = false
	a.feedbackInput.Reset()

	decision := api.PermissionDecision{
		RequestID:  perm.ID,
		ToolCallID: perm.ToolCallID,
		Kind:       kind,
		Feedback:   feedback,
		Wait:       false,
	}
	if kind == api.DecisionAllowSession && len(perm.GrantPatterns) > 0 {
		decision.GrantPattern = perm.GrantPatterns[0]
	}

	return perm, decision, true
}

// PopSelected creates a decision based on the currently selected cursor option.
func (a *Approval) PopSelected() (api.PermissionRequest, api.PermissionDecision, bool) {
	switch a.cursor {
	case 0:
		return a.PopDecision(api.DecisionAllowOnce, "")
	case 1:
		return a.PopDecision(api.DecisionAllowSession, "")
	default:
		return a.PopDecision(api.DecisionReject, "")
	}
}

// Render formats the active permission request card as a styled terminal view.
func (a Approval) Render() string {
	if len(a.pending) == 0 {
		return ""
	}
	return FormatPermissionCard(a.pending[0], a.cursor, a.feedbackMode, a.feedbackInput.Value())
}

// FormatPermissionCard renders a bordered approval card for a pending [api.PermissionRequest].
func FormatPermissionCard(perm api.PermissionRequest, cursor int, feedbackMode bool, feedbackText string) string {
	var rows []string

	actionBadge := theme.ActionBadgeStyle.Render(strings.ToUpper(string(perm.Action)))
	safetyBadge := theme.SafetyBadgeStyle(perm.Safety).Render(strings.ToUpper(string(perm.Safety)))
	header := theme.PermHeaderStyle.Render("[Permission Required]") + " " + actionBadge + " • " + safetyBadge
	if perm.ToolName != "" {
		header += " • " + theme.ToolCallStyle.Render(perm.ToolName)
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
			rows = append(rows, theme.OptionSelectedStyle.Render("❯ "+opt))
			continue
		}
		rows = append(rows, theme.OptionNormalStyle.Render("  "+opt))
	}

	rows = append(rows, formatPermissionFooter(feedbackMode, feedbackText))

	return theme.PermCardBorderStyle.Render(strings.Join(rows, "\n"))
}

func formatPermissionFooter(feedbackMode bool, feedbackText string) string {
	if feedbackMode {
		return theme.PermHeaderStyle.Render("Feedback: ") + feedbackText + "█ " + theme.StatusBarStyle.Render("(enter to submit, esc to cancel)")
	}
	return theme.StatusBarStyle.Render("↑/↓ or j/k to select • enter to confirm • r to reject with feedback")
}

// FormatDecisionSummary renders a 1-line summary of a resolved permission request.
func FormatDecisionSummary(perm api.PermissionRequest, decision api.PermissionDecision) string {
	target := perm.Summary
	if target == "" {
		target = perm.ToolName
	}
	switch decision.Kind {
	case api.DecisionAllowOnce:
		return theme.AllowDecisionStyle.Render(fmt.Sprintf("✓ Allowed once: %s", target))
	case api.DecisionAllowSession:
		return theme.AllowDecisionStyle.Render(fmt.Sprintf("✓ Allowed for session: %s", target))
	default:
		if decision.Feedback != "" {
			return theme.RejectDecisionStyle.Render(fmt.Sprintf("✗ Rejected: %s (%s)", target, decision.Feedback))
		}
		return theme.RejectDecisionStyle.Render(fmt.Sprintf("✗ Rejected: %s", target))
	}
}
