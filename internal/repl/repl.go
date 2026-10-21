package repl

import (
	"bufio"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
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

// Run starts the REPL using the inline Bubble Tea v2 TUI when cfg.In is a
// terminal TTY, or the deterministic line-driven loop when cfg.In is not a TTY.
func Run(ctx context.Context, cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}

	cfg.Version = strings.TrimSpace(cfg.Version)
	if cfg.Version == "" {
		cfg.Version = "v0.1.0"
	} else if !strings.HasPrefix(cfg.Version, "v") {
		cfg.Version = "v" + cfg.Version
	}

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

	if isTTY(cfg.In) {
		return runTUI(ctx, cfg, sess, mode)
	}
	return runLineLoop(ctx, cfg, sess, mode)
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

func isTTY(r io.Reader) bool {
	f, ok := r.(*os.File)
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

func runTUI(ctx context.Context, cfg Config, sess *api.Session, mode api.PermissionMode) error {
	hdr := buildHeader(ctx, cfg, sess)
	_, _ = fmt.Fprintln(cfg.Out, FormatHeader(hdr))

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

	model := NewModel(ctx, cfg, NewRegistry(), sess, mode)
	model.eventCh = eventCh

	p := tea.NewProgram(
		model,
		tea.WithInput(cfg.In),
		tea.WithOutput(cfg.Out),
		tea.WithContext(ctx),
	)
	_, err := p.Run()
	if errors.Is(err, tea.ErrInterrupted) || errors.Is(err, context.Canceled) {
		return nil
	}
	return err
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

type lineRunner struct {
	ctx             context.Context
	cfg             Config
	reg             *Registry
	in              *bufio.Scanner
	out             io.Writer
	errOut          io.Writer
	activeSession   *api.Session
	activeSessionID atomic.Value
	mode            api.PermissionMode
	turnSettledCh   chan struct{}
}

func runLineLoop(ctx context.Context, cfg Config, sess *api.Session, mode api.PermissionMode) error {
	r := &lineRunner{
		ctx:           ctx,
		cfg:           cfg,
		reg:           NewRegistry(),
		in:            bufio.NewScanner(cfg.In),
		out:           &syncWriter{w: cfg.Out},
		errOut:        &syncWriter{w: cfg.ErrOut},
		activeSession: sess,
		mode:          mode,
		turnSettledCh: make(chan struct{}, 1),
	}
	var initialID string
	if sess != nil {
		initialID = sess.ID
	}
	r.activeSessionID.Store(initialID)

	subCtx, cancelSub := context.WithCancel(ctx)
	subDone := r.startEventSubscription(subCtx)
	defer func() {
		cancelSub()
		<-subDone
	}()

	hdr := buildHeader(ctx, cfg, sess)
	_, _ = fmt.Fprintln(r.out, FormatHeader(hdr))

	return r.loop()
}

func (r *lineRunner) startEventSubscription(subCtx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ev, err := range r.cfg.Client.Subscribe(subCtx, "") {
			if err != nil {
				return
			}
			currID, _ := r.activeSessionID.Load().(string)
			if currID == "" || ev.SessionID != currID {
				continue
			}
			if ev.Type == api.EventChunk && ev.Chunk != nil {
				for _, b := range ev.Chunk.Content {
					if b.Type == api.BlockText && b.Text != "" {
						_, _ = fmt.Fprint(r.out, b.Text)
					}
				}
			}
			if ev.Type == api.EventCompleted || ev.Type == api.EventPermissionRequested || ev.Type == api.EventError {
				select {
				case r.turnSettledCh <- struct{}{}:
				default:
				}
			}
		}
	}()
	time.Sleep(15 * time.Millisecond)
	return done
}

func (r *lineRunner) drainTurnSettled() {
	select {
	case <-r.turnSettledCh:
	default:
	}
}

func (r *lineRunner) waitTurnSettled() {
	select {
	case <-r.turnSettledCh:
	case <-time.After(250 * time.Millisecond):
	case <-r.ctx.Done():
	}
}

func (r *lineRunner) loop() error {
	for {
		_, _ = fmt.Fprint(r.out, "> ")
		if !r.in.Scan() {
			_, _ = fmt.Fprintln(r.out)
			break
		}

		line := strings.TrimSpace(r.in.Text())
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "/") {
			if quit := r.handleSlashCommand(line); quit {
				return nil
			}
			continue
		}

		r.handlePrompt(line)
	}
	return r.in.Err()
}

func (r *lineRunner) handleSlashCommand(line string) bool {
	env := &CommandEnv{
		Client:       r.cfg.Client,
		Workspace:    r.cfg.Workspace,
		Session:      r.activeSession,
		Mode:         r.mode,
		DefaultAgent: r.cfg.Agent,
		DefaultModel: r.cfg.Model,
		DefaultTitle: r.cfg.Title,
	}

	res, err := r.reg.Execute(r.ctx, env, line)
	if err != nil {
		_, _ = fmt.Fprintf(r.errOut, "%v\n", err)
		return false
	}

	if res.ResetSession {
		r.activeSession = nil
		r.activeSessionID.Store("")
		r.cfg.Model = res.ModelOverride
	}
	if res.SwitchTo != nil {
		r.activeSession = res.SwitchTo
		r.activeSessionID.Store(res.SwitchTo.ID)
	}
	if res.ModeChange != "" {
		r.mode = res.ModeChange
	}
	if res.Output != "" {
		_, _ = fmt.Fprintln(r.out, res.Output)
	}
	return res.Quit
}

func (r *lineRunner) handlePrompt(line string) {
	if r.activeSession == nil || strings.TrimSpace(r.activeSession.ID) == "" {
		sess, err := ensureSession(r.ctx, r.cfg.Client, r.cfg.Title, r.cfg.Agent, r.cfg.Model, r.mode)
		if err != nil {
			_, _ = fmt.Fprintf(r.errOut, "error: %v\n", err)
			return
		}
		r.activeSession = sess
		r.activeSessionID.Store(sess.ID)
	}

	r.drainTurnSettled()
	err := r.cfg.Client.Send(r.ctx, r.activeSession.ID, api.SendRequest{
		Content: line,
		Wait:    true,
	})
	if err == nil {
		r.waitTurnSettled()
	}
	_, _ = fmt.Fprintln(r.out)
	if err != nil {
		_, _ = fmt.Fprintf(r.errOut, "error: %v\n", err)
		return
	}
	if err := r.promptPendingPermissions(); err != nil {
		_, _ = fmt.Fprintf(r.errOut, "error: %v\n", err)
	}
}

func (r *lineRunner) promptPendingPermissions() error {
	for {
		perms, err := r.cfg.Client.PendingPermissions(r.ctx, r.activeSession.ID)
		if err != nil {
			return err
		}
		if len(perms) == 0 {
			return nil
		}

		perm := perms[0]
		_, _ = fmt.Fprintln(r.out)
		_, _ = fmt.Fprintln(r.out, FormatPermissionCard(perm, 0, false, ""))
		_, _ = fmt.Fprint(r.out, "Allow? [y]es once / [a]llow session / [n]o (reject): ")
		if !r.in.Scan() {
			_, _ = fmt.Fprintln(r.out)
			return r.in.Err()
		}

		decision := ParsePermissionInput(perm, strings.TrimSpace(r.in.Text()))

		r.drainTurnSettled()
		if err := r.cfg.Client.RespondPermission(r.ctx, r.activeSession.ID, decision); err != nil {
			return err
		}
		if len(perms) == 1 {
			r.waitTurnSettled()
		}
		_, _ = fmt.Fprintln(r.out)
	}
}

// ParsePermissionInput parses a line-mode permission response ("y", "a", "n [feedback]")
// into a structured [api.PermissionDecision].
func ParsePermissionInput(perm api.PermissionRequest, raw string) api.PermissionDecision {
	lower := strings.ToLower(strings.TrimSpace(raw))
	decision := api.PermissionDecision{
		RequestID:  perm.ID,
		ToolCallID: perm.ToolCallID,
		Wait:       true,
	}

	if lower == "y" || lower == "yes" {
		decision.Kind = api.DecisionAllowOnce
		return decision
	}

	if lower == "a" || lower == "allow" || lower == "always" {
		decision.Kind = api.DecisionAllowSession
		if len(perm.GrantPatterns) > 0 {
			decision.GrantPattern = perm.GrantPatterns[0]
		}
		return decision
	}

	decision.Kind = api.DecisionReject
	trimmed := strings.TrimSpace(raw)
	if len(trimmed) > 2 && strings.HasPrefix(lower, "n ") {
		decision.Feedback = strings.TrimSpace(trimmed[2:])
		return decision
	}
	if len(trimmed) > 3 && strings.HasPrefix(lower, "no ") {
		decision.Feedback = strings.TrimSpace(trimmed[3:])
	}
	return decision
}
