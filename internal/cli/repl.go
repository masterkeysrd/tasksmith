package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
	"github.com/spf13/cobra"
)

type syncWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

type replOptions struct {
	sessionID string
	agent     string
	model     string
	title     string
}

func bindREPLFlags(cmd *cobra.Command, opts *replOptions) {
	cmd.Flags().StringVar(&opts.sessionID, "session", "", "Existing session ID to resume")
	cmd.Flags().StringVar(&opts.agent, "agent", "", "Agent name when creating a new session")
	cmd.Flags().StringVar(&opts.model, "model", "", "Model identifier when creating a new session")
	cmd.Flags().StringVar(&opts.title, "title", "Terminal Session", "Session title when creating a new session")
}

func newREPLCmd(rootOpts *rootOptions) *cobra.Command {
	opts := &replOptions{}
	cmd := &cobra.Command{
		Use:   "repl",
		Short: "Start an interactive REPL session attached to the Brain Daemon",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runREPL(cmd, rootOpts, opts)
		},
	}
	bindREPLFlags(cmd, opts)
	return cmd
}

type replRunner struct {
	ctx             context.Context
	client          *client.Client
	workspace       string
	opts            *replOptions
	in              *bufio.Scanner
	out             io.Writer
	errOut          io.Writer
	activeSession   *api.Session
	activeSessionID atomic.Value
	turnSettledCh   chan struct{}
}

func runREPL(cmd *cobra.Command, rootOpts *rootOptions, opts *replOptions) error {
	ctx := cmd.Context()

	wsDir, err := rootOpts.resolveWorkspace()
	if err != nil {
		return err
	}

	sockPath, err := rootOpts.resolveSocket()
	if err != nil {
		return err
	}

	c, err := connectOrStart(ctx, sockPath)
	if err != nil {
		return fmt.Errorf("connect to daemon: %w", err)
	}

	activeSession, err := resolveInitialSession(ctx, c, wsDir, opts)
	if err != nil {
		return err
	}

	r := &replRunner{
		ctx:           ctx,
		client:        c,
		workspace:     wsDir,
		opts:          opts,
		in:            bufio.NewScanner(cmd.InOrStdin()),
		out:           &syncWriter{w: cmd.OutOrStdout()},
		errOut:        &syncWriter{w: cmd.ErrOrStderr()},
		activeSession: activeSession,
		turnSettledCh: make(chan struct{}, 1),
	}
	r.activeSessionID.Store(activeSession.ID)

	subCtx, cancelSub := context.WithCancel(ctx)
	subDone := r.startEventSubscription(subCtx)
	defer func() {
		cancelSub()
		<-subDone
	}()

	_, _ = fmt.Fprintf(r.out, "TaskSmith REPL (session=%s agent=%s model=%s)\n",
		r.activeSession.ID, r.activeSession.Agent, r.activeSession.Model)
	_, _ = fmt.Fprintln(r.out, "Commands: /new [model], /sessions, /switch <id>, /history, /mode [default|accept_edits|plan|ask|auto], /changes, /revert [path], /exit, /quit")

	return r.loop()
}

func resolveInitialSession(ctx context.Context, c *client.Client, wsDir string, opts *replOptions) (*api.Session, error) {
	if sid := strings.TrimSpace(opts.sessionID); sid != "" {
		sess, err := c.GetSession(ctx, wsDir, sid)
		if err != nil {
			return nil, fmt.Errorf("get session %q: %w", sid, err)
		}
		return sess, nil
	}

	title := strings.TrimSpace(opts.title)
	if title == "" {
		title = "Terminal Session"
	}

	sess, err := c.CreateSession(ctx, wsDir, api.CreateSessionRequest{
		Title: title,
		Agent: strings.TrimSpace(opts.agent),
		Model: strings.TrimSpace(opts.model),
	})
	if err != nil {
		return nil, fmt.Errorf("create initial session: %w", err)
	}
	return sess, nil
}

func (r *replRunner) startEventSubscription(subCtx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ev, err := range r.client.Subscribe(subCtx, r.workspace, "") {
			if err != nil {
				return
			}
			currID, _ := r.activeSessionID.Load().(string)
			if ev.SessionID != currID {
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
	// Allow the local Unix socket SSE subscription to register before first Send.
	time.Sleep(15 * time.Millisecond)
	return done
}

func (r *replRunner) drainTurnSettled() {
	select {
	case <-r.turnSettledCh:
	default:
	}
}

func (r *replRunner) waitTurnSettled() {
	select {
	case <-r.turnSettledCh:
	case <-time.After(250 * time.Millisecond):
	case <-r.ctx.Done():
	}
}

func (r *replRunner) loop() error {
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

		if line == "/exit" || line == "/quit" {
			return nil
		}

		if strings.HasPrefix(line, "/") {
			r.handleSlashCommand(line)
			continue
		}

		r.handlePrompt(line)
	}
	return r.in.Err()
}

func (r *replRunner) handleSlashCommand(line string) {
	switch {
	case line == "/new" || strings.HasPrefix(line, "/new "):
		r.cmdNew(strings.TrimSpace(strings.TrimPrefix(line, "/new")))

	case line == "/sessions":
		r.cmdSessions()

	case line == "/switch" || strings.HasPrefix(line, "/switch "):
		r.cmdSwitch(strings.TrimSpace(strings.TrimPrefix(line, "/switch")))

	case line == "/history":
		r.cmdHistory()

	case line == "/mode" || strings.HasPrefix(line, "/mode "):
		r.cmdMode(strings.TrimSpace(strings.TrimPrefix(line, "/mode")))

	case line == "/changes":
		r.cmdChanges()

	case line == "/revert" || strings.HasPrefix(line, "/revert "):
		r.cmdRevert(strings.TrimSpace(strings.TrimPrefix(line, "/revert")))

	default:
		_, _ = fmt.Fprintf(r.errOut, "unknown command: %s\n", line)
	}
}

func (r *replRunner) cmdNew(modelOverride string) {
	if modelOverride == "" {
		modelOverride = strings.TrimSpace(r.opts.model)
	}
	title := strings.TrimSpace(r.opts.title)
	if title == "" {
		title = "Terminal Session"
	}

	s, err := r.client.CreateSession(r.ctx, r.workspace, api.CreateSessionRequest{
		Title: title,
		Agent: strings.TrimSpace(r.opts.agent),
		Model: modelOverride,
	})
	if err != nil {
		_, _ = fmt.Fprintf(r.errOut, "error creating session: %v\n", err)
		return
	}
	r.activeSession = s
	r.activeSessionID.Store(s.ID)
	_, _ = fmt.Fprintf(r.out, "Switched to new session %s (agent=%s model=%s)\n", s.ID, s.Agent, s.Model)
}

func (r *replRunner) cmdSessions() {
	sessions, err := r.client.ListSessions(r.ctx, r.workspace)
	if err != nil {
		_, _ = fmt.Fprintf(r.errOut, "error listing sessions: %v\n", err)
		return
	}
	for _, s := range sessions {
		marker := " "
		if s.ID == r.activeSession.ID {
			marker = "*"
		}
		_, _ = fmt.Fprintf(r.out, "%s %s\tstatus=%s\tagent=%s\tmodel=%s\tupdated_at=%s\n",
			marker, s.ID, s.Status, s.Agent, s.Model, s.UpdatedAt.Format(time.RFC3339))
	}
}

func (r *replRunner) cmdSwitch(targetID string) {
	if targetID == "" {
		_, _ = fmt.Fprintln(r.errOut, "usage: /switch <session_id>")
		return
	}
	s, err := r.client.GetSession(r.ctx, r.workspace, targetID)
	if err != nil {
		_, _ = fmt.Fprintf(r.errOut, "error switching session: %v\n", err)
		return
	}
	r.activeSession = s
	r.activeSessionID.Store(s.ID)
	_, _ = fmt.Fprintf(r.out, "Switched to session %s (agent=%s model=%s)\n", s.ID, s.Agent, s.Model)
}

func (r *replRunner) cmdHistory() {
	history, err := r.client.History(r.ctx, r.workspace, r.activeSession.ID)
	if err != nil {
		_, _ = fmt.Fprintf(r.errOut, "error loading history: %v\n", err)
		return
	}
	for _, msg := range history {
		_, _ = fmt.Fprintf(r.out, "[%s] %s\n", msg.Role, extractMessageText(msg))
	}
}

func (r *replRunner) cmdMode(arg string) {
	if arg == "" {
		mode, err := r.client.PermissionMode(r.ctx, r.workspace, r.activeSession.ID)
		if err != nil {
			_, _ = fmt.Fprintf(r.errOut, "error reading permission mode: %v\n", err)
			return
		}
		_, _ = fmt.Fprintf(r.out, "Permission mode: %s\n", mode)
		return
	}

	nextMode := api.PermissionMode(arg)
	if err := r.client.SetPermissionMode(r.ctx, r.workspace, r.activeSession.ID, nextMode); err != nil {
		_, _ = fmt.Fprintf(r.errOut, "error setting permission mode: %v (valid: default, accept_edits, plan, ask, auto)\n", err)
		return
	}
	_, _ = fmt.Fprintf(r.out, "Permission mode set to: %s\n", nextMode)
}

func (r *replRunner) cmdChanges() {
	changes, err := r.client.Changes(r.ctx, r.workspace, r.activeSession.ID)
	if err != nil {
		_, _ = fmt.Fprintf(r.errOut, "error listing file changes: %v\n", err)
		return
	}
	if len(changes) == 0 {
		_, _ = fmt.Fprintln(r.out, "No tracked file changes.")
		return
	}
	for _, c := range changes {
		_, _ = fmt.Fprintf(r.out, "%s\t%s\t%s\t+%d/-%d\n", c.ID, c.Kind, c.Path, c.Additions, c.Deletions)
	}
}

func (r *replRunner) cmdRevert(path string) {
	if err := r.client.Revert(r.ctx, r.workspace, r.activeSession.ID, api.RevertRequest{
		Path:  path,
		Force: true,
	}); err != nil {
		_, _ = fmt.Fprintf(r.errOut, "error reverting changes: %v\n", err)
		return
	}
	if path == "" {
		_, _ = fmt.Fprintln(r.out, "Reverted all session file changes.")
		return
	}
	_, _ = fmt.Fprintf(r.out, "Reverted %s.\n", path)
}

func (r *replRunner) handlePrompt(line string) {
	r.drainTurnSettled()
	err := r.client.Send(r.ctx, r.workspace, r.activeSession.ID, api.SendRequest{
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

func (r *replRunner) promptPendingPermissions() error {
	for {
		perms, err := r.client.PendingPermissions(r.ctx, r.workspace, r.activeSession.ID)
		if err != nil {
			return err
		}
		if len(perms) == 0 {
			return nil
		}

		perm := perms[0]
		_, _ = fmt.Fprintf(r.out, "\n[Permission Required] %s\n", perm.Summary)
		if perm.Diff != "" {
			_, _ = fmt.Fprintln(r.out, perm.Diff)
		}
		_, _ = fmt.Fprint(r.out, "Allow? [y]es once / [a]llow session / [n]o (reject): ")
		if !r.in.Scan() {
			_, _ = fmt.Fprintln(r.out)
			return r.in.Err()
		}

		decision := parsePermissionInput(perm, strings.TrimSpace(r.in.Text()))

		r.drainTurnSettled()
		if err := r.client.RespondPermission(r.ctx, r.workspace, r.activeSession.ID, decision); err != nil {
			return err
		}
		if len(perms) == 1 {
			r.waitTurnSettled()
		}
		_, _ = fmt.Fprintln(r.out)
	}
}

func parsePermissionInput(perm api.PermissionRequest, raw string) api.PermissionDecision {
	lower := strings.ToLower(raw)
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
	if len(raw) > 2 && strings.HasPrefix(lower, "n ") {
		decision.Feedback = strings.TrimSpace(raw[2:])
		return decision
	}
	if len(raw) > 3 && strings.HasPrefix(lower, "no ") {
		decision.Feedback = strings.TrimSpace(raw[3:])
	}
	return decision
}

func extractMessageText(msg api.Message) string {
	var sb strings.Builder
	for _, b := range msg.Content {
		if b.Type == api.BlockText {
			sb.WriteString(b.Text)
		}
	}
	return sb.String()
}
