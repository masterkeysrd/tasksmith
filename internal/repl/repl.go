package repl

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
	"github.com/masterkeysrd/tasksmith/internal/repl/command"
	"github.com/masterkeysrd/tasksmith/internal/repl/ui"
)

// Forwarded types and constants from subpackages for caller ergonomics.
type (
	Header            = ui.Header
	State             = ui.State
	Option            = ui.Option
	Picker            = ui.Picker
	PickerCanceledMsg = ui.PickerCanceledMsg
	Registry          = command.Registry
	Command           = command.Command
	CommandEnv        = command.Env
	CommandResult     = tea.Msg
	CommandHandler    = command.Handler
)

const (
	StateIdle      = ui.StateIdle
	StateStreaming = ui.StateStreaming
	StateApproval  = ui.StateApproval
	StatePicker    = ui.StatePicker
)

// ErrInvalidConfig is returned when required [Config] fields are missing.
var ErrInvalidConfig = errors.New("repl: invalid configuration")

// Config holds the parameters for starting a REPL session.
// SessionID is optional; when empty, a session is created lazily on the first user prompt.
type Config struct {
	Client    *client.Client
	Workspace string
	Version   string
	SessionID string
	Agent     string
	Model     string
	Title     string
	In        io.Reader
	Out       io.Writer
	ErrOut    io.Writer
}

// Validate checks that all required fields in [Config] are provided.
func (c Config) Validate() error {
	if c.Client == nil {
		return fmt.Errorf("%w: client is required", ErrInvalidConfig)
	}
	if strings.TrimSpace(c.Workspace) == "" {
		return fmt.Errorf("%w: workspace is required", ErrInvalidConfig)
	}
	if c.In == nil {
		return fmt.Errorf("%w: input reader is required", ErrInvalidConfig)
	}
	if c.Out == nil {
		return fmt.Errorf("%w: output writer is required", ErrInvalidConfig)
	}
	if c.ErrOut == nil {
		return fmt.Errorf("%w: error output writer is required", ErrInvalidConfig)
	}
	return nil
}

// Run starts the REPL using the inline Bubble Tea v2 TUI.
func Run(ctx context.Context, cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}

	cfg.Version = ui.NormalizeVersion(cfg.Version)

	if cfg.Client.CWD() == "" && cfg.Workspace != "" {
		cfg.Client = cfg.Client.WithCWD(cfg.Workspace)
	}

	var sess *api.Session
	mode := api.ModeDefault

	if sid := strings.TrimSpace(cfg.SessionID); sid != "" {
		loaded, err := cfg.Client.GetSession(ctx, sid)
		if err != nil {
			return fmt.Errorf("get session %q: %w", sid, err)
		}
		sess = loaded

		title := strings.TrimSpace(cfg.Title)
		agent := strings.TrimSpace(cfg.Agent)
		model := strings.TrimSpace(cfg.Model)
		if title != "" || agent != "" || model != "" {
			updated, err := cfg.Client.UpdateSession(ctx, sid, api.UpdateSessionRequest{
				Title: title,
				Agent: agent,
				Model: model,
			})
			if err != nil {
				return fmt.Errorf("update session %q: %w", sid, err)
			}
			sess = updated
		}

		if m, mErr := cfg.Client.PermissionMode(ctx, sess.ID); mErr == nil && m != "" {
			mode = m
		}
	}

	return runTUI(ctx, cfg, sess, mode)
}

func buildHeader(ctx context.Context, cfg Config, sess *api.Session) Header {
	agentName := strings.TrimSpace(cfg.Agent)
	modelID := strings.TrimSpace(cfg.Model)

	if sess != nil {
		agentName = strings.TrimSpace(sess.Agent)
		modelID = strings.TrimSpace(sess.Model)
	}

	var ws *api.Workspace
	if agentName == "" || modelID == "" {
		ws, _ = cfg.Client.GetWorkspace(ctx)
		if ws != nil && agentName == "" {
			agentName = strings.TrimSpace(ws.DefaultAgent)
		}
	}

	var ag *api.Agent
	if agentName != "" {
		ag, _ = cfg.Client.GetAgent(ctx, agentName)
	}

	if modelID == "" {
		if ag != nil && len(ag.Models) > 0 {
			modelID = strings.TrimSpace(ag.Models[0])
		}
		if modelID == "" && ws != nil {
			modelID = strings.TrimSpace(ws.DefaultModel)
		}
	}

	var mdl *api.Model
	if modelID != "" {
		mdl, _ = cfg.Client.GetModel(ctx, modelID)
	}

	var agEffort, mdlLabel, mdlEffort string
	if ag != nil {
		agEffort = strings.TrimSpace(ag.Effort)
	}
	if mdl != nil {
		mdlLabel = cmp.Or(strings.TrimSpace(mdl.Label), strings.TrimSpace(mdl.Name), strings.TrimSpace(mdl.ID))
		mdlEffort = strings.TrimSpace(mdl.DefaultEffort)
	}

	return Header{
		Version:  cfg.Version,
		Model:    cmp.Or(mdlLabel, modelID),
		Thinking: cmp.Or(agEffort, mdlEffort, "auto"),
		Path:     cfg.Workspace,
	}
}

func ensureSession(ctx context.Context, c *client.Client, title, agent, model string, mode api.PermissionMode) (*api.Session, error) {
	resolvedTitle := strings.TrimSpace(title)
	if resolvedTitle == "" {
		resolvedTitle = "Terminal Session"
	}

	sess, err := c.CreateSession(ctx, api.CreateSessionRequest{
		Title: resolvedTitle,
		Agent: strings.TrimSpace(agent),
		Model: strings.TrimSpace(model),
	})
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}

	if mode != "" && mode != api.ModeDefault {
		if err := c.SetPermissionMode(ctx, sess.ID, mode); err != nil {
			return nil, fmt.Errorf("set permission mode: %w", err)
		}
	}
	return sess, nil
}

func isTTY(v any) bool {
	f, ok := v.(*os.File)
	if !ok || f == nil {
		return false
	}
	if f.Name() == os.DevNull {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

type syncWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

func (s *syncWriter) Fd() uintptr {
	if f, ok := s.w.(interface{ Fd() uintptr }); ok {
		return f.Fd()
	}
	return 0
}

func runTUI(ctx context.Context, cfg Config, sess *api.Session, mode api.PermissionMode) error {
	out := cfg.Out
	if !isTTY(out) {
		out = &syncWriter{w: out}
	}

	hdr := buildHeader(ctx, cfg, sess)
	_, _ = fmt.Fprintln(out, ui.FormatHeader(hdr))

	subCtx, cancelSub := context.WithCancel(ctx)
	eventCh := make(chan api.Event, 64)
	subDone := make(chan struct{})

	go func() {
		defer close(subDone)
		defer close(eventCh)
		for ev, err := range cfg.Client.Subscribe(subCtx, "") {
			if err != nil {
				return
			}
			select {
			case eventCh <- ev:
			case <-subCtx.Done():
				return
			}
		}
	}()

	defer func() {
		cancelSub()
		<-subDone
	}()

	model := NewModel(ctx, cfg, command.NewRegistry(), sess, mode)
	model.eventCh = eventCh

	opts := []tea.ProgramOption{
		tea.WithInput(cfg.In),
		tea.WithOutput(out),
		tea.WithContext(ctx),
	}
	if !isTTY(cfg.In) {
		opts = append(opts, tea.WithoutSignals())
	}

	p := tea.NewProgram(model, opts...)
	_, err := p.Run()
	if errors.Is(err, tea.ErrInterrupted) || errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

// Forwarding helper functions delegating to ui or command subpackages.

// NewRegistry constructs a [command.Registry] populated with built-in commands.
func NewRegistry() *Registry {
	return command.NewRegistry()
}

// FormatHeader renders the 3-line startup header banner.
func FormatHeader(h Header) string {
	return ui.FormatHeader(h)
}

// FormatBanner renders the 3-line startup header banner.
func FormatBanner(h Header) string {
	return ui.FormatHeader(h)
}

// FormatStatusBar renders the contextual status footer for the active REPL state.
func FormatStatusBar(state State, sess *api.Session, mode api.PermissionMode) string {
	return ui.FormatStatusBar(state, sess, mode)
}

// FormatUserPrompt formats a submitted user prompt line for scrollback output.
func FormatUserPrompt(text string) string {
	return ui.FormatUserPrompt(text)
}

// FormatMessage renders a persisted or streamed message for terminal display.
func FormatMessage(msg api.Message) string {
	return ui.FormatMessage(msg)
}

// FormatError styles an error message for scrollback display.
func FormatError(msg string) string {
	return ui.FormatError(msg)
}

// FormatDiff styles a unified diff string.
func FormatDiff(diffText string) string {
	return ui.FormatDiff(diffText)
}

// FormatPermissionCard renders a bordered approval card for a pending permission request.
func FormatPermissionCard(perm api.PermissionRequest, cursor int, feedbackMode bool, feedbackText string) string {
	return ui.FormatPermissionCard(perm, cursor, feedbackMode, feedbackText)
}

// FormatDecisionSummary renders a 1-line summary of a resolved permission request.
func FormatDecisionSummary(perm api.PermissionRequest, decision api.PermissionDecision) string {
	return ui.FormatDecisionSummary(perm, decision)
}

// FormatCommandSuggestions renders inline slash-command autocomplete hints.
func FormatCommandSuggestions(cmds []Command) string {
	suggestions := make([]ui.Suggestion, len(cmds))
	for i, c := range cmds {
		suggestions[i] = ui.Suggestion{Usage: c.Usage, Description: c.Description}
	}
	return ui.FormatSuggestions(suggestions)
}
