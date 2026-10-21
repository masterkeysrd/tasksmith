package repl

import (
	"context"
	"fmt"
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

	state     ui.State
	composer  ui.Composer
	approval  ui.Approval
	picker    ui.Picker
	argCursor int
	spinner   spinner.Model

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

// PickerCursor returns the currently highlighted option index in [ui.StatePicker].
func (m Model) PickerCursor() int {
	return m.picker.Cursor()
}

// ArgCursor returns the currently highlighted index in inline argument suggestions.
func (m Model) ArgCursor() int {
	return m.argCursor
}

// InputValue returns the current text in the prompt input field.
func (m Model) InputValue() string {
	return m.composer.Value()
}

// SetInputValue sets the prompt input field value and moves the cursor to the end.
func (m *Model) SetInputValue(val string) {
	m.composer.SetValue(val)
}

func (m Model) commandEnv() command.Env {
	return command.Env{
		Mode:         m.mode,
		Session:      m.session,
		DefaultAgent: m.defaultAgent,
		DefaultModel: m.defaultModel,
	}
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

	case command.OutputMsg:
		if typed.Text != "" {
			return m, tea.Println(typed.Text)
		}
		return m, nil

	case command.OpenPickerMsg:
		m.picker = typed.Picker
		m.state = ui.StatePicker
		return m, nil

	case ui.PickerCanceledMsg:
		m.state = ui.StateIdle
		return m, nil

	case command.ChangeModeMsg:
		return m.handleChangeMode(typed)

	case command.NewSessionMsg:
		return m.handleNewSession(typed)

	case command.SwitchSessionMsg:
		return m.handleSwitchSession(typed)

	case command.ListSessionsMsg:
		return m.handleListSessions()

	case command.ShowHistoryMsg:
		return m.handleShowHistory()

	case command.ListChangesMsg:
		return m.handleListChanges()

	case command.ShowDiffMsg:
		return m.handleShowDiff(typed)

	case command.RevertMsg:
		return m.handleRevert(typed)

	case command.ClearScreenMsg:
		return m, tea.ClearScreen

	case command.QuitMsg:
		m.quitting = true
		return m, tea.Quit
	}

	if m.state == ui.StateIdle {
		var cmd tea.Cmd
		m.composer, cmd = m.composer.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m Model) handleChangeMode(msg command.ChangeModeMsg) (tea.Model, tea.Cmd) {
	m.state = ui.StateIdle
	if m.client != nil && m.session != nil && strings.TrimSpace(m.session.ID) != "" {
		if err := m.client.SetPermissionMode(m.ctx, m.session.ID, msg.Mode); err != nil {
			return m, tea.Println(ui.FormatError(err.Error()))
		}
	}
	m.mode = msg.Mode
	if !msg.Silent {
		return m, tea.Println(fmt.Sprintf("Permission mode set to: %s", msg.Mode))
	}
	return m, nil
}

func (m Model) handleNewSession(msg command.NewSessionMsg) (tea.Model, tea.Cmd) {
	m.session = nil
	if msg.Model != "" {
		m.defaultModel = msg.Model
	}
	return m, tea.Println(fmt.Sprintf("Switched to new session (agent=%s model=%s)", m.defaultAgent, m.defaultModel))
}

func (m Model) handleSwitchSession(msg command.SwitchSessionMsg) (tea.Model, tea.Cmd) {
	if m.client == nil {
		return m, tea.Println(ui.FormatError("repl: client is required"))
	}
	s, err := m.client.GetSession(m.ctx, msg.SessionID)
	if err != nil {
		return m, tea.Println(ui.FormatError(fmt.Sprintf("error switching session: %v", err)))
	}
	m.session = s
	return m, tea.Println(fmt.Sprintf("Switched to session %s (agent=%s model=%s)", s.ID, s.Agent, s.Model))
}

func (m Model) handleListSessions() (tea.Model, tea.Cmd) {
	if m.client == nil {
		return m, tea.Println(ui.FormatError("repl: client is required"))
	}
	sessions, err := m.client.ListSessions(m.ctx)
	if err != nil {
		return m, tea.Println(ui.FormatError(fmt.Sprintf("error listing sessions: %v", err)))
	}
	var activeID string
	if m.session != nil {
		activeID = m.session.ID
	}
	return m, tea.Println(formatSessionList(sessions, activeID))
}

func (m Model) handleShowHistory() (tea.Model, tea.Cmd) {
	if m.session == nil || strings.TrimSpace(m.session.ID) == "" {
		return m, tea.Println("No messages in session history.")
	}
	if m.client == nil {
		return m, tea.Println(ui.FormatError("repl: client is required"))
	}
	history, err := m.client.History(m.ctx, m.session.ID)
	if err != nil {
		return m, tea.Println(ui.FormatError(fmt.Sprintf("error loading history: %v", err)))
	}
	return m, tea.Println(formatHistory(history))
}

func (m Model) handleListChanges() (tea.Model, tea.Cmd) {
	if m.session == nil || strings.TrimSpace(m.session.ID) == "" {
		return m, tea.Println("No tracked file changes.")
	}
	if m.client == nil {
		return m, tea.Println(ui.FormatError("repl: client is required"))
	}
	changes, err := m.client.Changes(m.ctx, m.session.ID)
	if err != nil {
		return m, tea.Println(ui.FormatError(fmt.Sprintf("error listing file changes: %v", err)))
	}
	return m, tea.Println(formatChanges(changes))
}

func (m Model) handleShowDiff(msg command.ShowDiffMsg) (tea.Model, tea.Cmd) {
	if m.session == nil || strings.TrimSpace(m.session.ID) == "" {
		return m, tea.Println(ui.FormatError("repl: active session is required"))
	}
	if m.client == nil {
		return m, tea.Println(ui.FormatError("repl: client is required"))
	}
	diffText, err := m.client.DiffChange(m.ctx, m.session.ID, msg.Path, msg.EntryID)
	if err != nil {
		return m, tea.Println(ui.FormatError(fmt.Sprintf("error fetching diff: %v", err)))
	}
	if strings.TrimSpace(diffText) == "" {
		return m, tea.Println("No diff changes.")
	}
	return m, tea.Println(ui.FormatDiff(strings.TrimRight(diffText, "\n")))
}

func (m Model) handleRevert(msg command.RevertMsg) (tea.Model, tea.Cmd) {
	if m.session == nil || strings.TrimSpace(m.session.ID) == "" {
		return m, tea.Println(ui.FormatError("repl: active session is required"))
	}
	if m.client == nil {
		return m, tea.Println(ui.FormatError("repl: client is required"))
	}
	if err := m.client.Revert(m.ctx, m.session.ID, api.RevertRequest{
		Path:  msg.Path,
		Force: true,
	}); err != nil {
		return m, tea.Println(ui.FormatError(fmt.Sprintf("error reverting changes: %v", err)))
	}
	if msg.Path == "" {
		return m, tea.Println("Reverted all session file changes.")
	}
	return m, tea.Println(fmt.Sprintf("Reverted %s.", msg.Path))
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
	case ui.StatePicker:
		return tea.NewView(m.renderPickerView())
	default:
		return tea.NewView(m.renderIdleView())
	}
}

func (m Model) renderIdleView() string {
	var sections []string
	sections = append(sections, m.composer.View())

	raw := m.composer.Value()
	if _, argOpts, ok := m.registry.SuggestArgs(m.commandEnv(), raw); ok && len(argOpts) > 0 {
		cursor := m.argCursor
		if cursor < 0 {
			cursor = 0
		}
		if cursor >= len(argOpts) {
			cursor = len(argOpts) - 1
		}
		if optsBlock := ui.FormatOptions(argOpts, cursor); optsBlock != "" {
			sections = append(sections, optsBlock)
		}
	} else {
		trimmed := strings.TrimSpace(raw)
		if strings.HasPrefix(trimmed, "/") && !strings.Contains(trimmed, " ") {
			suggestions := m.registry.Suggest(trimmed)
			suggs := make([]ui.Suggestion, len(suggestions))
			for i, s := range suggestions {
				suggs[i] = ui.Suggestion{Usage: s.Usage, Description: s.Description}
			}
			if hint := ui.FormatSuggestions(suggs); hint != "" {
				sections = append(sections, hint)
			}
		}
	}

	sections = append(sections, ui.FormatStatusBar(m.state, m.session, m.mode))
	return strings.Join(sections, "\n")
}

func (m Model) renderPickerView() string {
	return m.picker.Render()
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
	return m.approval.Render()
}

func isEnterKey(msg tea.KeyPressMsg) bool {
	keyStr := msg.String()
	return keyStr == "enter" || msg.Code == tea.KeyEnter || keyStr == "ctrl+j" || msg.Code == '\n' || msg.Code == 10 || msg.Code == 13
}

func (m Model) handleKeyPress(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch m.state {
	case ui.StateStreaming:
		return m.handleStreamingKey(msg)
	case ui.StateApproval:
		return m.handleApprovalKey(msg)
	case ui.StatePicker:
		var cmd tea.Cmd
		m.picker, cmd = m.picker.Update(msg)
		if cmd != nil {
			ev := cmd()
			if ev != nil {
				return m.Update(ev)
			}
		}
		return m, nil
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
		return m.Update(command.ChangeModeMsg{
			Mode:   nextPermissionMode(m.mode),
			Silent: true,
		})
	}

	inHist := m.composer.InHistory()
	argCmd, argOpts, hasArgSuggs := m.registry.SuggestArgs(m.commandEnv(), m.composer.Value())
	if !inHist && hasArgSuggs && len(argOpts) > 0 {
		if keyStr == "up" || msg.Code == tea.KeyUp {
			if m.argCursor > 0 {
				m.argCursor--
			}
			return m, nil
		}
		if keyStr == "down" || msg.Code == tea.KeyDown {
			if m.argCursor < len(argOpts)-1 {
				m.argCursor++
			}
			return m, nil
		}
		if isEnterKey(msg) {
			idx := max(m.argCursor, 0)
			if idx >= len(argOpts) {
				idx = len(argOpts) - 1
			}
			opt := argOpts[idx]
			_ = m.composer.Submit()
			m.argCursor = 0
			if argCmd.OnSelect != nil {
				return m.Update(argCmd.OnSelect(opt))
			}
			return m.executeSlashCommand("/" + argCmd.Name + " " + opt.Value)
		}
	}

	if keyStr == "up" || msg.Code == tea.KeyUp {
		m.composer = m.composer.NavigateHistory(-1)
		return m, nil
	}

	if keyStr == "down" || msg.Code == tea.KeyDown {
		m.composer = m.composer.NavigateHistory(1)
		return m, nil
	}

	if isEnterKey(msg) {
		return m.submitIdleInput()
	}

	oldVal := m.composer.Value()
	var cmd tea.Cmd
	m.composer, cmd = m.composer.Update(msg)
	if m.composer.Value() != oldVal {
		m.argCursor = 0
	}
	return m, cmd
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
	m.argCursor = 0
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
	ev, err := m.registry.Execute(m.commandEnv(), line)
	if err != nil {
		return m, tea.Println(ui.FormatError(err.Error()))
	}
	if ev == nil {
		return m, nil
	}
	return m.Update(ev)
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

	case isEnterKey(msg):
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
	if isEnterKey(msg) {
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
