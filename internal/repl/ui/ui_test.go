package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/repl/theme"
)

func TestHeaderRenderingAndNormalization(t *testing.T) {
	h := Header{
		Version:  "0.1.0",
		Model:    "Claude 3.7 Sonnet",
		Thinking: "high",
		Path:     "/workspace/project",
	}
	rendered := h.Render()
	lines := strings.Split(rendered, "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d:\n%s", len(lines), rendered)
	}
	if !strings.Contains(lines[0], "TaskSmith") || !strings.Contains(lines[0], "v0.1.0") {
		t.Errorf("line 1 mismatch: %q", lines[0])
	}
	if !strings.Contains(lines[1], "Claude 3.7 Sonnet with high effort") {
		t.Errorf("line 2 mismatch: %q", lines[1])
	}
	if !strings.Contains(lines[2], "/workspace/project") {
		t.Errorf("line 3 mismatch: %q", lines[2])
	}

	// Home path shortening
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		shortened := ShortenHomePath(filepath.Join(home, "src", "tasksmith"))
		if !strings.HasPrefix(shortened, "~") {
			t.Errorf("expected ~ prefix for home path, got %q", shortened)
		}
		if ShortenHomePath(home) != "~" {
			t.Errorf("expected ~ for exact home, got %q", ShortenHomePath(home))
		}
	}

	// Empty defaults
	emptyH := Header{}
	emptyRendered := FormatHeader(emptyH)
	if !strings.Contains(emptyRendered, "v0.1.0") || !strings.Contains(emptyRendered, "default with auto effort") {
		t.Errorf("empty defaults mismatch:\n%s", emptyRendered)
	}
}

func TestStatusBarRenderingAndStates(t *testing.T) {
	// 1. Default mode: shows "<Model> · <Thinking>" without "default · "
	sbDefault := StatusBar{
		State:    StateIdle,
		Mode:     api.ModeDefault,
		Model:    "Claude 3.7 Sonnet",
		Thinking: "high",
		Width:    100,
	}
	outDefault := sbDefault.Render()
	if !strings.Contains(outDefault, "Claude 3.7 Sonnet · high") {
		t.Errorf("expected model · thinking on right, got: %s", outDefault)
	}
	if strings.Contains(outDefault, "default · ") {
		t.Errorf("default mode should not prefix with 'default · ': %s", outDefault)
	}
	if !strings.Contains(outDefault, "shift+tab cycle mode") {
		t.Errorf("idle state hint missing on left: %s", outDefault)
	}

	// 2. Non-default modes: shows "<mode> · <Model> · <Thinking>"
	for _, mode := range []api.PermissionMode{api.ModeAcceptEdits, api.ModePlan, api.ModeAsk, api.ModeAuto} {
		sb := StatusBar{
			State:    StateIdle,
			Mode:     mode,
			Model:    "Gemini 2.5 Flash",
			Thinking: "auto",
			Width:    120,
		}
		out := sb.Render()
		wantRight := theme.ModeBadgeStyle(mode).Render(string(mode)) + theme.StatusBarStyle.Render(" · ") + theme.StatusBarStyle.Render("Gemini 2.5 Flash · auto")
		if !strings.Contains(out, wantRight) {
			t.Errorf("mode %s right part mismatch, want %q in %q", mode, wantRight, out)
		}
	}

	// 3. Streaming state hint on left
	sbStreaming := StatusBar{
		State:    StateStreaming,
		Mode:     api.ModeDefault,
		Model:    "Claude 3.7 Sonnet",
		Thinking: "auto",
		Width:    80,
	}
	outStreaming := sbStreaming.Render()
	if !strings.Contains(outStreaming, "streaming • esc to cancel") {
		t.Errorf("streaming hint missing on left: %s", outStreaming)
	}

	// 4. Width gap separation
	sbWide := StatusBar{
		State:    StateIdle,
		Mode:     api.ModeDefault,
		Model:    "M",
		Thinking: "T",
		Width:    100,
	}
	outWide := sbWide.Render()
	left := theme.StatusBarStyle.Render("shift+tab cycle mode • /help commands")
	right := theme.StatusBarStyle.Render("M · T")
	expectedSpaces := 100 - lipgloss.Width(left) - lipgloss.Width(right)
	if !strings.Contains(outWide, strings.Repeat(" ", expectedSpaces)) {
		t.Errorf("status bar width gap mismatch: %q", outWide)
	}

	// 5. Fallback defaults when fields are empty
	sbEmpty := StatusBar{}
	outEmpty := sbEmpty.Render()
	if !strings.Contains(outEmpty, "default · auto") {
		t.Errorf("empty status bar missing fallback 'default · auto': %s", outEmpty)
	}

	if (State(99)).String() != "unknown" {
		t.Errorf("unknown state string = %q", (State(99)).String())
	}
	if FormatStatusBar(sbDefault) == "" {
		t.Errorf("FormatStatusBar returned empty")
	}
}

func TestComposerLifecycleAndHistory(t *testing.T) {
	c := NewComposer()
	if c.Value() != "" {
		t.Errorf("initial value = %q, want empty", c.Value())
	}

	c.SetValue("hello")
	if c.Value() != "hello" {
		t.Errorf("SetValue failed, got %q", c.Value())
	}
	c.Reset()
	if c.Value() != "" {
		t.Errorf("Reset failed, got %q", c.Value())
	}

	// Submit history
	c.SetValue("cmd 1")
	if submitted := c.Submit(); submitted != "cmd 1" {
		t.Errorf("Submit = %q, want 'cmd 1'", submitted)
	}
	c.SetValue("cmd 2")
	_ = c.Submit()

	// History navigation
	c.SetValue("draft")
	c = c.NavigateHistory(-1)
	if c.Value() != "cmd 2" {
		t.Errorf("NavigateHistory(-1) = %q, want 'cmd 2'", c.Value())
	}
	c = c.NavigateHistory(-1)
	if c.Value() != "cmd 1" {
		t.Errorf("NavigateHistory(-1) = %q, want 'cmd 1'", c.Value())
	}
	// At top of history
	c = c.NavigateHistory(-1)
	if c.Value() != "cmd 1" {
		t.Errorf("NavigateHistory(-1) at top = %q, want 'cmd 1'", c.Value())
	}

	// Navigate down
	c = c.NavigateHistory(1)
	if c.Value() != "cmd 2" {
		t.Errorf("NavigateHistory(1) = %q, want 'cmd 2'", c.Value())
	}
	c = c.NavigateHistory(1)
	if c.Value() != "draft" {
		t.Errorf("NavigateHistory(1) restored draft = %q, want 'draft'", c.Value())
	}

	_ = c.Focus()
	c.Blur()
	if c.View() == "" {
		t.Errorf("View() returned empty")
	}

	// Width, WindowSizeMsg, and top/bottom horizontal border rendering
	if c.Width() != 80 {
		t.Errorf("default width = %d, want 80", c.Width())
	}
	c.SetWidth(100)
	if c.Width() != 100 {
		t.Errorf("width after SetWidth = %d, want 100", c.Width())
	}
	c, _ = c.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if c.Width() != 120 {
		t.Errorf("width after WindowSizeMsg = %d, want 120", c.Width())
	}
	view := c.View()
	if !strings.Contains(view, "─") {
		t.Errorf("View() missing horizontal border lines:\n%s", view)
	}

	suggestions := []Suggestion{
		{Usage: "/help", Description: "Show help"},
		{Usage: "/exit", Description: "Exit REPL"},
	}
	formatted := FormatSuggestions(suggestions)
	if !strings.Contains(formatted, "/help") || !strings.Contains(formatted, "Show help") {
		t.Errorf("FormatSuggestions = %q", formatted)
	}
	if FormatSuggestions(nil) != "" {
		t.Errorf("FormatSuggestions(nil) should be empty")
	}
}

func TestApprovalComponentAndDecisions(t *testing.T) {
	app := NewApproval()
	if app.HasPending() {
		t.Fatalf("expected no pending on new approval")
	}

	req1 := api.PermissionRequest{
		ID:            "p1",
		ToolName:      "write_file",
		Action:        api.ActionWrite,
		Safety:        api.SafetyUnsafe,
		Summary:       "Write foo.go",
		Target:        "/tmp/foo.go",
		Diff:          "+added line\n-removed line\n",
		GrantPatterns: []string{"write_file:*"},
	}
	req2 := api.PermissionRequest{
		ID:       "p2",
		ToolName: "shell",
		Action:   api.ActionExec,
		Safety:   api.SafetyUnsafe,
		Summary:  "Execute ls",
	}

	app.SetPending([]api.PermissionRequest{req1})
	app.Append(req2)
	if len(app.Pending()) != 2 {
		t.Fatalf("expected 2 pending, got %d", len(app.Pending()))
	}

	// Cursor navigation
	if app.Cursor() != 0 {
		t.Errorf("initial cursor = %d, want 0", app.Cursor())
	}
	app.CursorDown()
	if app.Cursor() != 1 {
		t.Errorf("cursor after down = %d, want 1", app.Cursor())
	}
	app.CursorDown()
	if app.Cursor() != 2 {
		t.Errorf("cursor after down = %d, want 2", app.Cursor())
	}
	app.CursorDown() // capped at 2
	if app.Cursor() != 2 {
		t.Errorf("cursor after capped down = %d, want 2", app.Cursor())
	}
	app.CursorUp()
	if app.Cursor() != 1 {
		t.Errorf("cursor after up = %d, want 1", app.Cursor())
	}

	// Render card
	card := app.Render()
	if !strings.Contains(card, "write_file") || !strings.Contains(card, "Write foo.go") {
		t.Errorf("rendered card missing details:\n%s", card)
	}

	// Feedback mode
	app.EnterFeedback()
	if !app.FeedbackMode() {
		t.Errorf("expected FeedbackMode true")
	}
	app.ExitFeedback()
	if app.FeedbackMode() {
		t.Errorf("expected FeedbackMode false after ExitFeedback")
	}
	app.CursorUp() // cursor goes from 2 back to 1 (AllowSession)

	// Pop selected (cursor at 1 -> AllowSession)
	poppedReq, decision, ok := app.PopSelected()
	if !ok || poppedReq.ID != "p1" {
		t.Fatalf("PopSelected failed: ok=%v, req=%+v", ok, poppedReq)
	}
	if decision.Kind != api.DecisionAllowSession || decision.GrantPattern != "write_file:*" {
		t.Errorf("decision = %+v, want AllowSession with pattern", decision)
	}

	// Format decision summary
	sumAllow := FormatDecisionSummary(poppedReq, decision)
	if !strings.Contains(sumAllow, "Allowed for session") {
		t.Errorf("summary mismatch: %s", sumAllow)
	}

	// Pop second as Reject with feedback
	popped2, dec2, ok2 := app.PopDecision(api.DecisionReject, "no shell commands")
	if !ok2 || popped2.ID != "p2" {
		t.Fatalf("PopDecision p2 failed: %v", popped2)
	}
	if dec2.Kind != api.DecisionReject || dec2.Feedback != "no shell commands" {
		t.Errorf("dec2 mismatch: %+v", dec2)
	}
	sumReject := FormatDecisionSummary(popped2, dec2)
	if !strings.Contains(sumReject, "Rejected") || !strings.Contains(sumReject, "no shell commands") {
		t.Errorf("summary reject mismatch: %s", sumReject)
	}
}

func TestMessageAndDiffFormatting(t *testing.T) {
	// User prompt
	prompt := FormatUserPrompt("hello world")
	if !strings.Contains(prompt, "hello world") {
		t.Errorf("FormatUserPrompt missing text: %s", prompt)
	}

	// User message
	userMsg := FormatMessage(api.Message{
		Role:    api.RoleUser,
		Content: []api.Block{{Type: api.BlockText, Text: "howdy"}},
	})
	if !strings.Contains(userMsg, "[user] howdy") {
		t.Errorf("userMsg = %q", userMsg)
	}

	// Assistant message with reasoning and tool call
	asstMsg := FormatMessage(api.Message{
		Role: api.RoleAssistant,
		Content: []api.Block{
			{Type: api.BlockReasoning, Text: "deep thought"},
			{Type: api.BlockText, Text: "greetings"},
			{Type: api.BlockToolCall, ToolCall: &api.ToolCall{ID: "t1", Name: "read_file", Args: map[string]any{"path": "x"}}},
		},
	})
	if !strings.Contains(asstMsg, "[reasoning] deep thought") ||
		!strings.Contains(asstMsg, "[assistant] greetings") ||
		!strings.Contains(asstMsg, "read_file") {
		t.Errorf("asstMsg = %q", asstMsg)
	}

	// Tool message
	toolMsg := FormatMessage(api.Message{
		Role:       api.RoleTool,
		ToolCallID: "t1",
		Name:       "read_file",
		Content:    []api.Block{{Type: api.BlockText, Text: "file content"}},
	})
	if !strings.Contains(toolMsg, "[tool] read_file: file content") {
		t.Errorf("toolMsg = %q", toolMsg)
	}

	// Diff format
	rawDiff := "--- a/file.go\n+++ b/file.go\n@@ -1,1 +1,2 @@\n-old\n+new\n"
	diffOut := FormatDiff(rawDiff)
	if !strings.Contains(diffOut, "--- a/file.go") || !strings.Contains(diffOut, "+new") {
		t.Errorf("FormatDiff output missing details:\n%s", diffOut)
	}
	if FormatDiff("") != "" {
		t.Errorf("FormatDiff empty should return empty")
	}

	// Error formatting
	errFormatted := FormatError("fatal failure")
	if !strings.Contains(errFormatted, "error: fatal failure") {
		t.Errorf("FormatError = %q", errFormatted)
	}
}

func TestPickerComponent(t *testing.T) {
	opts := []Option{
		{Value: "default", Label: "Default", Description: "Safe reads"},
		{Value: "accept_edits", Label: "Accept Edits", Description: "Auto edits", Current: true},
		{Value: "plan", Label: "Plan", Description: "Read-only"},
	}

	type dummyMsg struct{ val string }
	p := NewPicker("Choose Mode:", opts, func(opt Option) tea.Msg {
		return dummyMsg{val: opt.Value}
	})
	if p.Cursor() != 1 {
		t.Fatalf("expected cursor at 1 (Current option), got %d", p.Cursor())
	}

	sel, ok := p.Selected()
	if !ok || sel.Value != "accept_edits" {
		t.Errorf("Selected = %+v, ok=%v", sel, ok)
	}

	selectedMsg := p.Select()
	if dm, ok := selectedMsg.(dummyMsg); !ok || dm.val != "accept_edits" {
		t.Errorf("Select() = %+v, want dummyMsg 'accept_edits'", selectedMsg)
	}

	p.CursorDown()
	if p.Cursor() != 2 {
		t.Errorf("CursorDown = %d, want 2", p.Cursor())
	}
	p.CursorDown() // clamped
	if p.Cursor() != 2 {
		t.Errorf("CursorDown clamped = %d, want 2", p.Cursor())
	}

	p.CursorUp()
	if p.Cursor() != 1 {
		t.Errorf("CursorUp = %d, want 1", p.Cursor())
	}

	p.SetCursor(0)
	if p.Cursor() != 0 {
		t.Errorf("SetCursor(0) = %d, want 0", p.Cursor())
	}

	rendered := p.Render()
	if !strings.Contains(rendered, "Choose Mode:") ||
		!strings.Contains(rendered, "Accept Edits") ||
		!strings.Contains(rendered, "current") ||
		!strings.Contains(rendered, "esc to cancel") {
		t.Errorf("rendered picker mismatch:\n%s", rendered)
	}
	if !strings.Contains(rendered, "─") {
		t.Errorf("rendered picker missing top border separator line:\n%s", rendered)
	}
	// Verify blank line before status bar
	lines := strings.Split(strings.TrimRight(rendered, "\n"), "\n")
	if len(lines) < 3 || strings.TrimSpace(lines[len(lines)-2]) != "" {
		t.Errorf("rendered picker missing blank line before status bar:\n%s", rendered)
	}

	// Width and WindowSizeMsg handling
	if p.Width() != 80 {
		t.Errorf("p.Width() default = %d, want 80", p.Width())
	}
	p.SetWidth(100)
	if p.Width() != 100 {
		t.Errorf("p.Width() after SetWidth = %d, want 100", p.Width())
	}
	p, _ = p.Update(tea.WindowSizeMsg{Width: 110, Height: 35})
	if p.Width() != 110 {
		t.Errorf("p.Width() after WindowSizeMsg = %d, want 110", p.Width())
	}

	// Empty picker
	emptyP := NewPicker("", nil, nil)
	if _, ok := emptyP.Selected(); ok {
		t.Errorf("empty picker Selected should be false")
	}
	if emptyP.Select() != nil {
		t.Errorf("empty picker Select should be nil")
	}
	if emptyP.Render() == "" {
		t.Errorf("empty picker Render should still render footer")
	}

	// Custom status bar
	customP := NewPicker("Custom", opts, nil)
	customP.StatusBar = "custom navigation hints"
	if !strings.Contains(customP.Render(), "custom navigation hints") {
		t.Errorf("custom status bar missing in render")
	}
	// Fallback to DefaultPickerStatusBar when cleared
	customP.StatusBar = ""
	if !strings.Contains(customP.Render(), DefaultPickerStatusBar) {
		t.Errorf("default status bar missing when StatusBar cleared")
	}
}
