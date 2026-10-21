package repl

import (
	"context"
	"strings"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
	"github.com/masterkeysrd/tasksmith/internal/repl/command"
	"github.com/masterkeysrd/tasksmith/internal/repl/theme"
	"github.com/masterkeysrd/tasksmith/internal/repl/ui"
)

type sendErrorMsg struct {
	err error
}

type sseClosedMsg struct{}

// Model implements the inline Bubble Tea v2 [tea.Model] for the TaskSmith REPL,
// delegating UI rendering and component state to [ui.Composer], [ui.Approval],
// and [command.Registry].
type Model struct {
	ctx          context.Context
	client       *client.Client
	workspace    string
	session      *api.Session
	mode         api.PermissionMode
	defaultAgent string
	defaultModel string
	defaultTitle string
	registry     *command.Registry

	state    ui.State
	composer ui.Composer
	approval ui.Approval
	spinner  spinner.Model

	streamText      string
	streamReasoning string

	eventCh  <-chan api.Event
	quitting bool
}

// NewModel constructs an initialized [Model] in [ui.StateIdle].
func NewModel(ctx context.Context, cfg Config, reg *command.Registry, sess *api.Session, mode api.PermissionMode) Model {
	if ctx == nil {
		ctx = context.Background()
	}
	if reg == nil {
		reg = command.NewRegistry()
	}
	if mode == "" {
		mode = api.ModeDefault
	}

	sp := spinner.New(
		spinner.WithSpinner(spinner.Dot),
		spinner.WithStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("#7C3AED"))),
	)

	c := cfg.Client
	if c != nil && c.CWD() == "" && cfg.Workspace != "" {
		c = c.WithCWD(cfg.Workspace)
	}

	return Model{
		ctx:          ctx,
		client:       c,
		workspace:    cfg.Workspace,
		session:      sess,
		mode:         mode,
		defaultAgent: cfg.Agent,
		defaultModel: cfg.Model,
		defaultTitle: cfg.Title,
		registry:     reg,
		state:        ui.StateIdle,
		composer:     ui.NewComposer(),
		approval:     ui.NewApproval(),
		spinner:      sp,
	}
}

// State returns the current lifecycle [ui.State] of the model.
func (m Model) State() ui.State {
	return m.state
}

// Mode returns the active [api.PermissionMode] tracked by the model.
func (m Model) Mode() api.PermissionMode {
	return m.mode
}

// Session returns the active [api.Session] tracked by the model.
func (m Model) Session() *api.Session {
	return m.session
}

// StreamText returns the currently buffered streaming assistant text.
func (m Model) StreamText() string {
	return m.streamText
}

// StreamReasoning returns the currently buffered streaming reasoning text.
func (m Model) StreamReasoning() string {
	return m.streamReasoning
}

// PendingPermissions returns the queue of unresolved [api.PermissionRequest] items.
func (m Model) PendingPermissions() []api.PermissionRequest {
	return m.approval.Pending()
}

// PermCursor returns the currently highlighted option index in [ui.StateApproval].
func (m Model) PermCursor() int {
	return m.approval.Cursor()
}

// SetPermCursor overrides the selection cursor index for testing.
func (m *Model) SetPermCursor(c int) {
	m.approval.SetCursor(c)
}

// FeedbackMode reports whether inline rejection feedback input is active in [ui.StateApproval].
func (m Model) FeedbackMode() bool {
	return m.approval.FeedbackMode()
}

// InputValue returns the current text in the prompt input field.
func (m Model) InputValue() string {
	return m.composer.Value()
}

// SetInputValue sets the prompt input field value and moves the cursor to the end.
func (m *Model) SetInputValue(val string) {
	m.composer.SetValue(val)
}

// Init implements [tea.Model].
func (m Model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, m.waitForSSECmd())
}

// Update implements [tea.Model].
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch typed := msg.(type) {
	case tea.KeyPressMsg:
		return m.handleKeyPress(typed)

	case api.Event:
		model, cmd := m.handleSSEEvent(typed)
		return model, tea.Batch(cmd, m.waitForSSECmd())

	case sendErrorMsg:
		m.state = ui.StateIdle
		m.streamText = ""
		m.streamReasoning = ""
		return m, tea.Println(ui.FormatError(typed.err.Error()))

	case spinner.TickMsg:
		if m.state != ui.StateStreaming {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(typed)
		return m, cmd

	case sseClosedMsg:
		return m, nil
	}

	if m.state == ui.StateIdle {
		var cmd tea.Cmd
		m.composer, cmd = m.composer.Update(msg)
		return m, cmd
	}
	return m, nil
}

// View implements [tea.Model] in inline mode (AltScreen is never enabled).
func (m Model) View() tea.View {
	if m.quitting {
		return tea.NewView("")
	}

	switch m.state {
	case ui.StateStreaming:
		return tea.NewView(m.renderStreamingView())
	case ui.StateApproval:
		return tea.NewView(m.renderApprovalView())
	default:
		return tea.NewView(m.renderIdleView())
	}
}

func (m Model) renderIdleView() string {
	var sections []string
	sections = append(sections, m.composer.View())

	raw := strings.TrimSpace(m.composer.Value())
	if strings.HasPrefix(raw, "/") && !strings.Contains(raw, " ") {
		suggestions := m.registry.Suggest(raw)
		suggs := make([]ui.Suggestion, len(suggestions))
		for i, s := range suggestions {
			suggs[i] = ui.Suggestion{Usage: s.Usage, Description: s.Description}
		}
		if hint := ui.FormatSuggestions(suggs); hint != "" {
			sections = append(sections, hint)
		}
	}

	sections = append(sections, ui.FormatStatusBar(m.state, m.session, m.mode))
	return strings.Join(sections, "\n")
}

func (m Model) renderStreamingView() string {
	var sections []string
	if r := strings.TrimSpace(m.streamReasoning); r != "" {
		sections = append(sections, theme.ReasoningStyle.Render(r))
	}

	streamLine := m.spinner.View() + " Thinking..."
	if m.streamText != "" {
		streamLine = m.spinner.View() + " " + m.streamText
	}
	sections = append(sections, streamLine)
	sections = append(sections, ui.FormatStatusBar(m.state, m.session, m.mode))
	return strings.Join(sections, "\n")
}

func (m Model) renderApprovalView() string {
	var sections []string
	if card := m.approval.Render(); card != "" {
		sections = append(sections, card)
	}
	sections = append(sections, ui.FormatStatusBar(m.state, m.session, m.mode))
	return strings.Join(sections, "\n")
}

func (m Model) handleKeyPress(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch m.state {
	case ui.StateStreaming:
		return m.handleStreamingKey(msg)
	case ui.StateApproval:
		return m.handleApprovalKey(msg)
	default:
		return m.handleIdleKey(msg)
	}
}

func (m Model) handleIdleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	keyStr := msg.String()

	if keyStr == "ctrl+c" || keyStr == "ctrl+d" {
		m.quitting = true
		return m, tea.Quit
	}

	if keyStr == "shift+tab" || (msg.Code == tea.KeyTab && msg.Mod&tea.ModShift != 0) {
		return m.cycleMode()
	}

	if keyStr == "up" || msg.Code == tea.KeyUp {
		m.composer = m.composer.NavigateHistory(-1)
		return m, nil
	}

	if keyStr == "down" || msg.Code == tea.KeyDown {
		m.composer = m.composer.NavigateHistory(1)
		return m, nil
	}

	if keyStr == "enter" || msg.Code == tea.KeyEnter {
		return m.submitIdleInput()
	}

	var cmd tea.Cmd
	m.composer, cmd = m.composer.Update(msg)
	return m, cmd
}

func (m Model) cycleMode() (tea.Model, tea.Cmd) {
	next := nextPermissionMode(m.mode)
	if m.client != nil && m.session != nil && strings.TrimSpace(m.session.ID) != "" {
		if err := m.client.SetPermissionMode(m.ctx, m.session.ID, next); err != nil {
			return m, tea.Println(ui.FormatError(err.Error()))
		}
	}
	m.mode = next
	return m, nil
}

func nextPermissionMode(curr api.PermissionMode) api.PermissionMode {
	switch curr {
	case api.ModeDefault, "":
		return api.ModeAcceptEdits
	case api.ModeAcceptEdits:
		return api.ModePlan
	case api.ModePlan:
		return api.ModeAsk
	case api.ModeAsk:
		return api.ModeAuto
	case api.ModeAuto:
		return api.ModeDefault
	default:
		return api.ModeDefault
	}
}

func (m Model) submitIdleInput() (tea.Model, tea.Cmd) {
	text := m.composer.Submit()
	if text == "" {
		return m, nil
	}

	if strings.HasPrefix(text, "/") {
		return m.executeSlashCommand(text)
	}

	if (m.session == nil || strings.TrimSpace(m.session.ID) == "") && m.client != nil {
		sess, err := ensureSession(m.ctx, m.client, m.defaultTitle, m.defaultAgent, m.defaultModel, m.mode)
		if err != nil {
			return m, tea.Println(ui.FormatError(err.Error()))
		}
		m.session = sess
	}

	m.state = ui.StateStreaming
	m.streamText = ""
	m.streamReasoning = ""

	return m, tea.Batch(
		tea.Println(ui.FormatUserPrompt(text)),
		m.spinner.Tick,
		m.sendPromptCmd(text),
	)
}

func (m Model) executeSlashCommand(line string) (tea.Model, tea.Cmd) {
	env := &command.Env{
		Client:       m.client,
		Workspace:    m.workspace,
		Session:      m.session,
		Mode:         m.mode,
		DefaultAgent: m.defaultAgent,
		DefaultModel: m.defaultModel,
		DefaultTitle: m.defaultTitle,
	}

	res, err := m.registry.Execute(m.ctx, env, line)
	if err != nil {
		return m, tea.Println(ui.FormatError(err.Error()))
	}

	if res.ResetSession {
		m.session = nil
		m.defaultModel = res.ModelOverride
	}
	if res.SwitchTo != nil {
		m.session = res.SwitchTo
	}
	if res.ModeChange != "" {
		m.mode = res.ModeChange
	}

	var cmds []tea.Cmd
	if res.Clear {
		cmds = append(cmds, tea.ClearScreen)
	}
	if res.Output != "" {
		cmds = append(cmds, tea.Println(res.Output))
	}
	if res.Quit {
		m.quitting = true
		cmds = append(cmds, tea.Quit)
	}
	return m, tea.Batch(cmds...)
}

func (m Model) handleStreamingKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	keyStr := msg.String()
	if keyStr == "esc" || keyStr == "ctrl+c" || msg.Code == tea.KeyEscape {
		if m.client != nil && m.session != nil && strings.TrimSpace(m.session.ID) != "" {
			_ = m.client.Cancel(m.ctx, m.session.ID)
		}
		m.state = ui.StateIdle
		m.streamText = ""
		m.streamReasoning = ""
		return m, nil
	}
	return m, nil
}

func (m Model) handleApprovalKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if !m.approval.HasPending() {
		m.state = ui.StateIdle
		return m, nil
	}

	if m.approval.FeedbackMode() {
		return m.handleApprovalFeedbackKey(msg)
	}

	keyStr := msg.String()
	switch {
	case keyStr == "ctrl+c" || keyStr == "esc" || msg.Code == tea.KeyEscape:
		if m.client != nil && m.session != nil && strings.TrimSpace(m.session.ID) != "" {
			_ = m.client.Cancel(m.ctx, m.session.ID)
		}
		m.approval.Clear()
		m.state = ui.StateIdle
		return m, nil

	case keyStr == "up" || keyStr == "k" || msg.Code == tea.KeyUp:
		m.approval.CursorUp()
		return m, nil

	case keyStr == "down" || keyStr == "j" || msg.Code == tea.KeyDown:
		m.approval.CursorDown()
		return m, nil

	case keyStr == "y":
		return m.submitPermissionDecision(api.DecisionAllowOnce, "")

	case keyStr == "a":
		return m.submitPermissionDecision(api.DecisionAllowSession, "")

	case keyStr == "n":
		return m.submitPermissionDecision(api.DecisionReject, "")

	case keyStr == "r":
		m.approval.EnterFeedback()
		return m, nil

	case keyStr == "enter" || msg.Code == tea.KeyEnter:
		return m.submitSelectedPermission()
	}

	return m, nil
}

func (m Model) handleApprovalFeedbackKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	keyStr := msg.String()
	if keyStr == "esc" || msg.Code == tea.KeyEscape {
		m.approval.ExitFeedback()
		return m, nil
	}
	if keyStr == "enter" || msg.Code == tea.KeyEnter {
		feedback := strings.TrimSpace(m.approval.FeedbackValue())
		m.approval.ExitFeedback()
		return m.submitPermissionDecision(api.DecisionReject, feedback)
	}

	var cmd tea.Cmd
	m.approval, cmd = m.approval.UpdateFeedback(msg)
	return m, cmd
}

func (m Model) submitSelectedPermission() (tea.Model, tea.Cmd) {
	perm, decision, ok := m.approval.PopSelected()
	if !ok {
		m.state = ui.StateIdle
		return m, nil
	}
	return m.applyDecision(perm, decision)
}

func (m Model) submitPermissionDecision(kind api.DecisionKind, feedback string) (tea.Model, tea.Cmd) {
	perm, decision, ok := m.approval.PopDecision(kind, feedback)
	if !ok {
		m.state = ui.StateIdle
		return m, nil
	}
	return m.applyDecision(perm, decision)
}

func (m Model) applyDecision(perm api.PermissionRequest, decision api.PermissionDecision) (tea.Model, tea.Cmd) {
	if m.client != nil && m.session != nil && strings.TrimSpace(m.session.ID) != "" {
		if err := m.client.RespondPermission(m.ctx, m.session.ID, decision); err != nil {
			m.state = ui.StateIdle
			return m, tea.Println(ui.FormatError(err.Error()))
		}
	}

	summary := ui.FormatDecisionSummary(perm, decision)
	if m.approval.HasPending() {
		m.state = ui.StateApproval
		return m, tea.Println(summary)
	}

	m.state = ui.StateStreaming
	return m, tea.Batch(tea.Println(summary), m.spinner.Tick)
}

func (m Model) handleSSEEvent(ev api.Event) (Model, tea.Cmd) {
	if ev.SessionID != "" && m.session != nil && ev.SessionID != m.session.ID {
		return m, nil
	}

	switch ev.Type {
	case api.EventChunk:
		return m.handleChunkEvent(ev), nil

	case api.EventMessageAdded:
		return m.handleMessageAddedEvent(ev)

	case api.EventPermissionRequested:
		return m.handlePermissionRequestedEvent(ev)

	case api.EventCompleted:
		return m.handleCompletedEvent()

	case api.EventError:
		return m.handleErrorEvent(ev)
	}

	return m, nil
}

func (m Model) handleChunkEvent(ev api.Event) Model {
	if ev.Chunk == nil {
		return m
	}
	for _, b := range ev.Chunk.Content {
		if b.Type == api.BlockText {
			m.streamText += b.Text
		}
		if b.Type == api.BlockReasoning {
			m.streamReasoning += b.Text
		}
	}
	return m
}

func (m Model) handleMessageAddedEvent(ev api.Event) (Model, tea.Cmd) {
	if ev.Message == nil {
		return m, nil
	}
	if ev.Message.Role == api.RoleUser {
		return m, nil
	}
	if ev.Message.Role == api.RoleTool {
		return m, tea.Println(ui.FormatMessage(*ev.Message))
	}
	if ev.Message.Role == api.RoleAssistant {
		if hasToolCalls(ev.Message.Content) {
			m.streamText = ""
			m.streamReasoning = ""
			return m, tea.Println(ui.FormatMessage(*ev.Message))
		}
		if m.streamText == "" {
			m.streamText = ui.ExtractTextBlocks(ev.Message.Content)
		}
	}
	return m, nil
}

func hasToolCalls(blocks []api.Block) bool {
	for _, b := range blocks {
		if b.Type == api.BlockToolCall && b.ToolCall != nil {
			return true
		}
	}
	return false
}

func (m Model) handlePermissionRequestedEvent(ev api.Event) (Model, tea.Cmd) {
	var cmds []tea.Cmd
	if flushed := m.flushStreamBuffer(); flushed != "" {
		cmds = append(cmds, tea.Println(flushed))
	}

	if len(ev.Permissions) > 0 {
		m.approval.Append(ev.Permissions...)
	}
	if !m.approval.HasPending() && m.client != nil && m.session != nil && strings.TrimSpace(m.session.ID) != "" {
		if perms, err := m.client.PendingPermissions(m.ctx, m.session.ID); err == nil {
			m.approval.Append(perms...)
		}
	}

	m.state = ui.StateApproval
	return m, tea.Batch(cmds...)
}

func (m Model) handleCompletedEvent() (Model, tea.Cmd) {
	flushed := m.flushStreamBuffer()
	m.state = ui.StateIdle
	m.approval.Clear()
	if flushed != "" {
		return m, tea.Println(flushed)
	}
	return m, nil
}

func (m Model) handleErrorEvent(ev api.Event) (Model, tea.Cmd) {
	m.streamText = ""
	m.streamReasoning = ""
	m.approval.Clear()
	m.state = ui.StateIdle
	errText := ev.Error
	if errText == "" {
		errText = "turn failed"
	}
	return m, tea.Println(ui.FormatError(errText))
}

func (m *Model) flushStreamBuffer() string {
	var blocks []api.Block
	if r := strings.TrimSpace(m.streamReasoning); r != "" {
		blocks = append(blocks, api.Block{Type: api.BlockReasoning, Text: r})
	}
	if m.streamText != "" {
		blocks = append(blocks, api.Block{Type: api.BlockText, Text: m.streamText})
	}
	m.streamReasoning = ""
	m.streamText = ""
	if len(blocks) == 0 {
		return ""
	}
	return ui.FormatAssistantMessage(blocks)
}

func (m Model) sendPromptCmd(text string) tea.Cmd {
	c := m.client
	ctx := m.ctx
	var sid string
	if m.session != nil {
		sid = m.session.ID
	}
	return func() tea.Msg {
		if c == nil || sid == "" {
			return nil
		}
		if err := c.Send(ctx, sid, api.SendRequest{Content: text, Wait: false}); err != nil {
			return sendErrorMsg{err: err}
		}
		return nil
	}
}

func (m Model) waitForSSECmd() tea.Cmd {
	ch := m.eventCh
	ctx := m.ctx
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		select {
		case <-ctx.Done():
			return sseClosedMsg{}
		case ev, ok := <-ch:
			if !ok {
				return sseClosedMsg{}
			}
			return ev
		}
	}
}
