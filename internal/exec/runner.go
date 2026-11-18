package exec

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
	"github.com/masterkeysrd/tasksmith/internal/cli/mentions"
)

// streamReconnectAttempts bounds how often the event watcher re-attaches after
// a transport hiccup before failing the run.
const streamReconnectAttempts = 5

// settleFetchTimeout bounds the post-turn usage and change lookups so a hung
// daemon cannot wedge the final result frame.
const settleFetchTimeout = 5 * time.Second

// Run executes a headless session end to end. The returned error carries the
// process exit code through [ExitCode]; stdout stays within the framing the
// selected [OutputFormat] promises and diagnostics go to opts.Stderr.
func Run(ctx context.Context, opts Options) error {
	r, err := newRun(ctx, &opts)
	if err != nil {
		return err
	}
	return r.execute()
}

// run holds one headless execution. All turn state is owned by the loop
// goroutine; the watcher, stdin reader, and signal goroutines communicate only
// through channels.
type run struct {
	ctx    context.Context
	cancel context.CancelFunc
	opts   *Options
	c      *client.Client
	w      writer
	sess   *api.Session

	events      chan api.Event
	streamErr   chan error
	actions     chan api.ExecInputFrame
	interrupt   chan struct{}
	quit        chan struct{}
	watchMu     sync.Mutex
	since       string
	startCursor string
	watchedSubs map[string]struct{}

	// watchCancel aborts the main session watcher's subscription context.
	// /fork cancels and replaces it so the watcher re-subscribes to the
	// fork's stream topic without reporting a lost stream.
	watchCancel context.CancelFunc

	// Loop-owned turn state.
	tracker        *turnTracker
	permOwners     map[string]string
	questionOwners map[string]string
	pendingSends   int
	turnsDone      int
	cancelPending  bool
	lastStatus     string
	lastError      string
	guardReason    string
}

func newRun(ctx context.Context, opts *Options) (*run, error) {
	if opts.InputFormat == "" {
		opts.InputFormat = InputText
	}
	if opts.OutputFormat == "" {
		opts.OutputFormat = OutputText
	}
	if opts.Stdin == nil {
		opts.Stdin = os.Stdin
	}
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}

	runCtx, cancel := context.WithCancel(ctx)
	r := &run{
		ctx:            runCtx,
		cancel:         cancel,
		opts:           opts,
		tracker:        newTurnTracker(),
		watchedSubs:    make(map[string]struct{}),
		permOwners:     make(map[string]string),
		questionOwners: make(map[string]string),
		lastStatus:     api.ExecResultCompleted,
		events:         make(chan api.Event),
		streamErr:      make(chan error, 1),
		actions:        make(chan api.ExecInputFrame, 16),
		interrupt:      make(chan struct{}, 1),
		quit:           make(chan struct{}, 1),
	}
	r.w = newTextWriter(opts.Stdout, opts.OutputFormat, opts.OutputFormat == OutputText)
	return r, nil
}

// execute drives the full lifecycle: connect, session, watch-before-send, the
// serialized event/input loop, and the terminal result.
func (r *run) execute() error {
	defer r.cancel()

	if err := r.connect(); err != nil {
		return r.fail(err)
	}
	if err := r.resolveSession(); err != nil {
		return r.fail(err)
	}
	if r.opts.InputFormat == InputJSONL {
		go r.readStdin()
	}
	if err := r.writeHeader(); err != nil {
		return r.fail(err)
	}

	stopSignals := r.watchSignals()
	defer stopSignals()

	r.startWatcher()

	if err := r.submitInitial(); err != nil {
		return r.fail(err)
	}
	if r.opts.InputFormat != InputJSONL && r.pendingSends == 0 && !r.cancelPending {
		// A one-shot prompt fully handled client-side (a local-only slash
		// command): nothing can settle on the daemon, so report immediately.
		return r.finish(ExitOK)
	}

	return r.finish(r.loop())
}

// connect connects to (or spawns) the daemon and binds the workspace.
func (r *run) connect() error {
	c, notices, err := client.ConnectOrStart(r.ctx, r.opts.Socket)
	if err != nil {
		return fmt.Errorf("connect to daemon: %w", err)
	}
	for _, n := range notices {
		r.warn(n)
	}
	if ws := strings.TrimSpace(r.opts.Workspace); ws != "" {
		c = c.WithCWD(ws)
	}
	r.c = c
	return nil
}

// resolveSession resumes --session or -c/--continue, branches them with
// --fork, overlays any flag config, or creates a fresh session with the
// requested configuration.
func (r *run) resolveSession() error {
	id := strings.TrimSpace(r.opts.SessionID)
	if id == "" && r.opts.Continue {
		latest, err := r.c.LatestSession(r.ctx)
		if err != nil {
			return fmt.Errorf("resolve latest session: %w", err)
		}
		if latest != nil {
			id = latest.ID
		}
	}
	if id != "" {
		var (
			sess *api.Session
			err  error
		)
		if r.opts.Fork {
			sess, err = r.c.ForkSession(r.ctx, id, api.ForkSessionRequest{Title: strings.TrimSpace(r.opts.Title)})
			if err != nil {
				return fmt.Errorf("fork session %q: %w", id, err)
			}
			id = sess.ID
		} else {
			sess, err = r.c.GetSession(r.ctx, id)
			if err != nil {
				return fmt.Errorf("resume session %q: %w", id, err)
			}
		}
		if req := r.opts.Config.Sandbox; req != "" && sess.SandboxMode != "" && req != sess.SandboxMode {
			r.warn(fmt.Sprintf("session %s already runs under the %s sandbox backend; --sandbox %s cannot be applied to an existing session", id, sess.SandboxMode, req))
		}
		if hasMutableConfig(r.opts.Config) {
			cfg := r.opts.Config
			cfg.Sandbox = ""
			updated, err := r.c.UpdateSession(r.ctx, id, api.UpdateSessionRequest{
				Config: cfg,
				Force:  r.opts.Force,
			})
			if err != nil {
				if apiErr, ok := client.IsCapabilityMismatch(err); ok {
					return fmt.Errorf("apply session config: %s; pass --force to omit unsupported attachments on future turns", apiErr.Message)
				}
				return fmt.Errorf("apply session config: %w", err)
			}
			sess = updated
		}
		r.sess = sess
		r.seedSessionPending(sess)
		return nil
	}
	if r.opts.Fork {
		return newUsageError("--fork needs a session to branch: combine it with --session or -c")
	}

	sess, err := r.c.CreateSession(r.ctx, api.CreateSessionRequest{
		Title:  strings.TrimSpace(r.opts.Title),
		Config: r.opts.Config,
	})
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	r.sess = sess
	r.seedSessionPending(sess)
	return nil
}

func hasMutableConfig(cfg api.SessionConfig) bool {
	return strings.TrimSpace(cfg.Agent) != "" || strings.TrimSpace(cfg.Model) != "" ||
		cfg.Mode != "" || cfg.Thinking != nil
}

// writeHeader announces the resolved session (jsonl framing; other writers
// ignore it).
func (r *run) writeHeader() error {
	header := api.ExecHeaderFrame{
		SessionID:   r.sess.ID,
		Workspace:   r.c.CWD(),
		Model:       r.sess.Model,
		Config:      r.sess.Config,
		SandboxMode: r.sess.SandboxMode,
	}
	if h, err := r.c.Health(r.ctx); err == nil && h != nil {
		header.DaemonVer = h.Version
	}
	return r.w.begin(header)
}

// startWatcher subscribes to the session stream before anything is sent and
// re-attaches with a fresh cursor on transport errors, mirroring the TUI.
func (r *run) startWatcher() {
	cursor := client.NewCursor()
	r.since = cursor
	r.startCursor = cursor
	r.rebindMainWatch()
	for i := range r.sess.Subagents {
		r.ensureSubagentWatcher(r.sess.Subagents[i].ID)
	}
}

// rebindMainWatch (re)starts the main session watcher goroutine subscribed to
// the current session's stream topic. Cancelling the previous subscription
// context makes the old watcher return without reporting a stream error, so a
// /fork rebind switches the watched topic cleanly.
func (r *run) rebindMainWatch() {
	if r.watchCancel != nil {
		r.watchCancel()
	}
	sessionID := r.sess.ID
	watchCtx, cancel := context.WithCancel(r.ctx)
	r.watchCancel = cancel
	go func() {
		for attempt := 0; ; attempt++ {
			err := r.consumeStream(watchCtx, sessionID)
			if watchCtx.Err() != nil || r.ctx.Err() != nil {
				return
			}
			if client.IsNeedSync(err) {
				r.setSince(client.NewCursor())
				attempt = -1
			} else if attempt >= streamReconnectAttempts {
				select {
				case r.streamErr <- err:
				case <-r.ctx.Done():
				}
				return
			}
			select {
			case <-r.ctx.Done():
				return
			case <-time.After(200 * time.Millisecond):
			}
		}
	}()
}

// ensureSubagentWatcher subscribes to a child subagent's turn stream once so
// subagent events (chunks, tool entries, permission/question prompts) flow
// onto the single NDJSON output stream keyed by session_id.
func (r *run) ensureSubagentWatcher(subID string) {
	if subID == "" || subID == r.sess.ID {
		return
	}
	r.watchMu.Lock()
	if _, exists := r.watchedSubs[subID]; exists {
		r.watchMu.Unlock()
		return
	}
	r.watchedSubs[subID] = struct{}{}
	since := r.startCursor
	r.watchMu.Unlock()

	topic := api.SessionStreamTopic(subID)
	go func() {
		for attempt := 0; ; attempt++ {
			err := r.consumeSubagentStream(topic, &since)
			if r.ctx.Err() != nil {
				return
			}
			if client.IsNeedSync(err) {
				since = client.NewCursor()
				attempt = 0
			} else if attempt >= streamReconnectAttempts {
				return
			}
			select {
			case <-r.ctx.Done():
				return
			case <-time.After(200 * time.Millisecond):
			}
		}
	}()
}

func (r *run) consumeSubagentStream(topic string, since *string) error {
	for ev, err := range r.c.Watch(r.ctx, *since, topic) {
		if err != nil {
			return err
		}
		if ev.ID != "" {
			*since = ev.ID
		}
		select {
		case r.events <- ev:
		case <-r.ctx.Done():
			return r.ctx.Err()
		}
	}
	return errors.New("client: subagent stream ended")
}

func (r *run) isWatchedSubagent(id string) bool {
	r.watchMu.Lock()
	_, ok := r.watchedSubs[id]
	r.watchMu.Unlock()
	return ok
}

// consumeStream iterates the session and session-metadata topics until the
// iterator ends. TopicSessions lets non-interactive runs detect child subagents
// waiting on permissions/questions without polluting the NDJSON output stream.
func (r *run) consumeStream(watchCtx context.Context, sessionID string) error {
	r.watchMu.Lock()
	since := r.since
	r.watchMu.Unlock()
	for ev, err := range r.c.Watch(watchCtx, since, api.SessionStreamTopic(sessionID), api.TopicSessions) {
		if err != nil {
			return err
		}
		if ev.ID != "" {
			r.setSince(ev.ID)
		}
		select {
		case r.events <- ev:
		case <-r.ctx.Done():
			return r.ctx.Err()
		}
	}
	return errors.New("client: session stream ended")
}

func (r *run) setSince(s string) {
	r.watchMu.Lock()
	r.since = s
	r.watchMu.Unlock()
}

// watchSignals forwards SIGINT/SIGTERM to the loop as a cancel request so
// Ctrl-C settles as a cancellation instead of killing the process mid-frame.
func (r *run) watchSignals() func() {
	sigCtx, stop := signal.NotifyContext(r.ctx, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case <-sigCtx.Done():
			select {
			case r.interrupt <- struct{}{}:
			default:
			}
		case <-r.ctx.Done():
		}
	}()
	return stop
}

// submitInitial sends the opening prompt (piped stdin becomes context, while
// only the user-authored prompt argument is scanned for @mentions).
func (r *run) submitInitial() error {
	if r.opts.InputFormat == InputJSONL {
		if strings.TrimSpace(r.opts.Prompt) == "" {
			return nil
		}
		return r.submit(r.opts.Prompt, r.opts.Prompt)
	}
	content := composePrompt(r.opts.StdinText, r.opts.Prompt)
	if strings.TrimSpace(content) == "" {
		return newUsageError("no prompt: pass a prompt, pipe stdin, or use --input-format jsonl")
	}
	return r.submit(content, r.opts.Prompt)
}

// composePrompt prepends piped stdin content to the prompt as context.
func composePrompt(stdinText, prompt string) string {
	stdinText = strings.TrimRight(stdinText, "\n")
	prompt = strings.TrimSpace(prompt)
	switch {
	case stdinText == "":
		return prompt
	case prompt == "":
		return stdinText
	default:
		return stdinText + "\n\n" + prompt
	}
}

// submit dispatches one input line: client-local slash commands resolve
// immediately, everything else (including `!` shell commands and unknown
// `/names`) becomes a daemon send with @mentions resolved from userText.
func (r *run) submit(text, userText string) error {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}
	if handled, err := r.localCommand(trimmed); handled {
		if err != nil {
			if r.opts.Interactive() || r.opts.OutputFormat == OutputJSONL {
				r.reportCommand(trimmed, "", err)
			}
			if !r.opts.Interactive() {
				return err
			}
		}
		return nil
	}
	req := api.SendRequest{Content: trimmed}
	if strings.HasPrefix(trimmed, "!") {
		req = api.SendRequest{Command: strings.TrimLeft(trimmed, "!")}
	} else if strings.TrimSpace(userText) != "" {
		req.Attachments = r.resolveMentions(userText, nil)
	}
	r.pendingSends++
	if err := r.c.Send(r.ctx, r.sess.ID, req); err != nil {
		r.pendingSends--
		if r.opts.InputFormat == InputJSONL {
			r.reportCommand(trimmed, "", err)
			return nil
		}
		return err
	}
	return nil
}

// resolveMentions extracts structured @file, @rule:, @skill:, and @mcp:
// attachments from userText, resolving bare @filename tokens against the
// workspace file index identically to the TUI composer.
func (r *run) resolveMentions(userText string, existing []api.Attachment) []api.Attachment {
	reconciled := mentions.Resolve(userText, existing, nil)
	return mentions.Resolve(userText, reconciled, func(cand string) (string, bool) {
		q := strings.TrimSuffix(cand, "/")
		files, err := r.c.ListWorkspaceFiles(r.ctx, api.ListFilesRequest{
			Query:     q,
			SessionID: r.sess.ID,
			Limit:     50,
		})
		if err != nil {
			return "", false
		}
		var matched *api.WorkspaceFile
		for i := range files {
			if files[i].ShortPath == cand || files[i].Path == q {
				if matched != nil {
					return "", false
				}
				matched = &files[i]
			}
		}
		if matched == nil {
			return "", false
		}
		if matched.IsDir && !strings.HasSuffix(matched.Path, "/") {
			return matched.Path + "/", true
		}
		return matched.Path, true
	})
}

// loop serializes daemon events, input frames, stream failures, and signals
// until the run settles (one-shot) or stdin closes with no work left (bidi).
func (r *run) loop() int {
	stdinOpen := r.opts.InputFormat == InputJSONL
	quitRequested := false
	var actions <-chan api.ExecInputFrame
	if stdinOpen {
		actions = r.actions
	}

	for {
		select {
		case <-r.ctx.Done():
			r.settleCanceled()
			return r.exitCode()

		case <-r.quit:
			if r.opts.Interactive() {
				// Let in-flight turns settle before the final result.
				if r.pendingSends == 0 && !r.cancelPending {
					return r.exitCode()
				}
				quitRequested = true
				continue
			}
			return r.exitCode()

		case err := <-r.streamErr:
			r.lastStatus = api.ExecResultFailed
			r.lastError = fmt.Sprintf("session stream lost: %v", err)
			return r.exitCode()

		case <-r.interrupt:
			r.requestCancel()

		case ev := <-r.events:
			if r.handleEvent(ev) {
				return r.exitCode()
			}
			if quitRequested && r.pendingSends == 0 && !r.cancelPending {
				return r.exitCode()
			}
			if !stdinOpen && r.pendingSends == 0 && !r.cancelPending {
				// Input is closed and the last turn settled: nothing left
				// to wait for.
				return r.exitCode()
			}

		case frame := <-actions:
			if frame.Type == api.ExecInputClose {
				stdinOpen = false
				actions = nil
				if r.pendingSends == 0 && !r.cancelPending {
					return r.exitCode()
				}
				continue
			}
			if err := r.applyFrame(frame); err != nil {
				r.warn(fmt.Sprintf("input frame error: %v", err))
			}
		}
	}
}

// handleEvent folds one daemon event into the writers and turn state,
// reporting whether the run should terminate now.
func (r *run) handleEvent(ev api.Event) bool {
	if ev.Topic == api.TopicSessions {
		return r.handleSessionLifecycleEvent(ev)
	}

	if err := r.w.event(ev); err != nil {
		r.warn(fmt.Sprintf("write event: %v", err))
	}

	isRoot := ev.Topic == api.SessionStreamTopic(r.sess.ID)
	if isRoot {
		r.tracker.observe(ev)
		if r.opts.OutputFormat == OutputText {
			switch p := ev.Payload.(type) {
			case api.ChunkEvent:
				for _, b := range p.Chunk.Content {
					if b.Type == api.BlockText && b.Text != "" {
						_ = r.w.delta(p.Chunk.ID, b.Text)
					}
				}
			case api.EntryAddedEvent:
				if p.Entry.Kind == api.TimelineKindCommand && p.Entry.Command != nil {
					if out := strings.TrimRight(p.Entry.Command.Output, "\n"); out != "" {
						_ = r.w.delta(p.Entry.ID, out)
					}
				}
			}
		}
	}

	switch p := ev.Payload.(type) {
	case api.PermissionRequestedEvent:
		r.recordPermissionOwners(p.SessionID, p.Permissions)
		if !r.opts.Interactive() && len(p.Permissions) > 0 {
			r.guardReason = permissionSummary(p.Permissions)
			r.settleNeedsInput()
			return true
		}
	case api.QuestionRequestedEvent:
		r.recordQuestionOwners(p.SessionID, p.Questions)
		if !r.opts.Interactive() && len(p.Questions) > 0 {
			r.guardReason = questionSummary(p.Questions)
			r.settleNeedsInput()
			return true
		}
	case api.ErrorEvent:
		if !isRoot {
			return false
		}
		r.pendingSends = 0
		r.cancelPending = false
		r.lastStatus = api.ExecResultFailed
		r.lastError = p.Error
		return !r.opts.Interactive()
	case api.CompletedEvent:
		if !isRoot {
			return false
		}
		// The daemon batches queued sends into one turn run, so a completed
		// event settles every send submitted before it — but only once the
		// session is truly idle (queue drained).
		if !r.quiescent() {
			return false
		}
		r.pendingSends = 0
		if r.cancelPending {
			r.cancelPending = false
			r.lastStatus = api.ExecResultCanceled
		} else {
			r.lastStatus = api.ExecResultCompleted
			r.lastError = ""
		}
		r.turnsDone++
		if r.opts.MaxTurns > 0 && r.turnsDone >= r.opts.MaxTurns {
			// The cap stops the run after this turn: no further input.
			select {
			case r.quit <- struct{}{}:
			default:
			}
		}
		return !r.opts.Interactive()
	case api.StatusChangedEvent:
		if !isRoot {
			return false
		}
		if r.cancelPending && p.Status == api.StatusIdle {
			// A canceled turn never emits completed; the idle transition is
			// its terminal signal.
			r.cancelPending = false
			r.pendingSends = 0
			r.lastStatus = api.ExecResultCanceled
			return !r.opts.Interactive()
		}
	}
	return false
}

// handleSessionLifecycleEvent filters workspace-wide session lifecycle events
// down to the active session and its direct subagents, forwarding matching
// frames to jsonl output and attaching child stream watchers on the fly.
func (r *run) handleSessionLifecycleEvent(ev api.Event) bool {
	switch p := ev.Payload.(type) {
	case api.SessionCreatedEvent:
		if p.Session.ID != r.sess.ID && p.Session.ParentID != r.sess.ID {
			return false
		}
		if err := r.w.event(ev); err != nil {
			r.warn(fmt.Sprintf("write event: %v", err))
		}
		r.seedSingleSessionPending(&p.Session)
		if p.Session.ParentID == r.sess.ID {
			r.ensureSubagentWatcher(p.Session.ID)
		}
		return r.checkNonInteractiveSession(&p.Session)

	case api.SessionUpdatedEvent:
		if p.Session.ID != r.sess.ID && p.Session.ParentID != r.sess.ID {
			return false
		}
		if err := r.w.event(ev); err != nil {
			r.warn(fmt.Sprintf("write event: %v", err))
		}
		if p.Session.ID == r.sess.ID {
			r.sess = &p.Session
			r.seedSessionPending(&p.Session)
			for i := range p.Session.Subagents {
				r.ensureSubagentWatcher(p.Session.Subagents[i].ID)
				if r.checkNonInteractiveSession(&p.Session.Subagents[i]) {
					return true
				}
			}
			return r.checkNonInteractiveSession(&p.Session)
		}
		r.seedSingleSessionPending(&p.Session)
		r.ensureSubagentWatcher(p.Session.ID)
		return r.checkNonInteractiveSession(&p.Session)

	case api.SessionDeletedEvent:
		if p.SessionID != r.sess.ID && !r.isWatchedSubagent(p.SessionID) {
			return false
		}
		if err := r.w.event(ev); err != nil {
			r.warn(fmt.Sprintf("write event: %v", err))
		}
	}
	return false
}

func (r *run) checkNonInteractiveSession(sess *api.Session) bool {
	if r.opts.Interactive() || sess == nil {
		return false
	}
	if len(sess.PendingPermissions) > 0 {
		r.guardReason = permissionSummary(sess.PendingPermissions)
		r.settleNeedsInput()
		return true
	}
	if len(sess.PendingQuestions) > 0 {
		r.guardReason = questionSummary(sess.PendingQuestions)
		r.settleNeedsInput()
		return true
	}
	return false
}

func (r *run) seedSessionPending(sess *api.Session) {
	if sess == nil {
		return
	}
	r.seedSingleSessionPending(sess)
	for i := range sess.Subagents {
		r.seedSingleSessionPending(&sess.Subagents[i])
	}
}

func (r *run) seedSingleSessionPending(sess *api.Session) {
	r.recordPermissionOwners(sess.ID, sess.PendingPermissions)
	r.recordQuestionOwners(sess.ID, sess.PendingQuestions)
}

func (r *run) recordPermissionOwners(sessionID string, reqs []api.PermissionRequest) {
	if sessionID == "" {
		return
	}
	for _, req := range reqs {
		if req.ID != "" {
			r.permOwners[req.ID] = sessionID
		}
		if req.ToolCallID != "" {
			r.permOwners["call:"+req.ToolCallID] = sessionID
		}
	}
}

func (r *run) recordQuestionOwners(sessionID string, reqs []api.QuestionRequest) {
	if sessionID == "" {
		return
	}
	for _, req := range reqs {
		if req.ID != "" {
			r.questionOwners[req.ID] = sessionID
		}
		if req.ToolCallID != "" {
			r.questionOwners["call:"+req.ToolCallID] = sessionID
		}
	}
}

func (r *run) permissionSessionID(dec *api.PermissionDecision) string {
	if dec.RequestID != "" {
		if sid, ok := r.permOwners[dec.RequestID]; ok {
			return sid
		}
	}
	if dec.ToolCallID != "" {
		if sid, ok := r.permOwners["call:"+dec.ToolCallID]; ok {
			return sid
		}
	}
	return ""
}

func (r *run) questionSessionID(resp *api.QuestionResponse) string {
	if resp.RequestID != "" {
		if sid, ok := r.questionOwners[resp.RequestID]; ok {
			return sid
		}
	}
	if resp.ToolCallID != "" {
		if sid, ok := r.questionOwners["call:"+resp.ToolCallID]; ok {
			return sid
		}
	}
	return ""
}

// quiescent reports whether the daemon session and its child subagents have no
// running or queued work. A failed lookup counts as quiescent: with an
// unreachable daemon there is nothing left to wait for, and the stream
// watchdog reports the loss.
func (r *run) quiescent() bool {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.ctx), settleFetchTimeout)
	defer cancel()
	sess, err := r.c.GetSession(ctx, r.sess.ID)
	if err != nil || sess == nil {
		return true
	}
	r.sess = sess
	return sess.Status == api.StatusIdle && !hasRunningSubagents(sess.Subagents)
}

func hasRunningSubagents(subs []api.Session) bool {
	for _, s := range subs {
		if s.Status != "" && s.Status != api.StatusIdle {
			return true
		}
	}
	return false
}

// requestCancel asks the daemon to abort the in-flight turn without blocking
// the loop. The daemon's idle status change finalizes the cancellation.
func (r *run) requestCancel() {
	if r.cancelPending {
		return
	}
	r.cancelPending = true
	sessionID := r.sess.ID
	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.ctx), 10*time.Second)
		defer cancel()
		if err := r.c.Cancel(ctx, sessionID); err != nil && !errors.Is(err, context.Canceled) {
			r.warn(fmt.Sprintf("cancel: %v", err))
		}
	}()
}

// settleNeedsInput records the non-interactive guard outcome: the turn is
// canceled in the background (it can never be answered here) and the run
// settles immediately with the needs-input status.
func (r *run) settleNeedsInput() {
	r.pendingSends = 0
	r.lastStatus = api.ExecResultNeedsInput
	r.requestCancel()
}

// settleCanceled records a context-driven cancellation.
func (r *run) settleCanceled() {
	r.pendingSends = 0
	r.cancelPending = false
	r.lastStatus = api.ExecResultCanceled
}

// exitCode maps the settled status onto a process exit code.
func (r *run) exitCode() int {
	switch r.lastStatus {
	case api.ExecResultNeedsInput:
		return ExitNeedsInput
	case api.ExecResultCanceled:
		return ExitCanceled
	case api.ExecResultFailed:
		return ExitError
	default:
		return ExitOK
	}
}

// finish fetches the settled usage and file changes, writes the terminal
// result, and applies --ephemeral cleanup.
func (r *run) finish(code int) error {
	res := api.ExecResult{
		SessionID: r.sess.ID,
		Status:    r.lastStatus,
		Text:      r.tracker.Text(),
		Error:     r.lastError,
	}
	if r.guardReason != "" && r.lastStatus == api.ExecResultNeedsInput {
		res.Error = r.guardReason
	}

	fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(r.ctx), settleFetchTimeout)
	usage, err := r.c.SessionUsage(fetchCtx, r.sess.ID)
	if err == nil {
		res.Usage = &usage
	}
	changes, err := r.c.Changes(fetchCtx, r.sess.ID)
	if err == nil {
		res.Changes = changes
	}
	cancel()

	if r.opts.Ephemeral {
		dCtx, dCancel := context.WithTimeout(context.WithoutCancel(r.ctx), settleFetchTimeout)
		if err := r.c.DeleteSession(dCtx, r.sess.ID); err != nil {
			r.warn(fmt.Sprintf("delete ephemeral session: %v", err))
		}
		dCancel()
	}

	if err := r.w.finish(res); err != nil {
		return err
	}

	switch code {
	case ExitOK:
		return nil
	case ExitNeedsInput:
		r.warn(fmt.Sprintf("%s\nrerun with --input-format jsonl to answer prompts interactively", res.Error))
		return &ExitCodeError{Code: ExitNeedsInput, Err: errors.New(res.Error)}
	case ExitCanceled:
		return &ExitCodeError{Code: ExitCanceled, Err: errors.New("run canceled")}
	default:
		if res.Error != "" {
			r.warn(res.Error)
			return &ExitCodeError{Code: ExitError, Err: errors.New(res.Error)}
		}
		return &ExitCodeError{Code: ExitError, Err: errors.New("run failed")}
	}
}

// fail reports a pre-loop failure in the run's framing and wraps it with the
// right exit code.
func (r *run) fail(err error) error {
	if _, ok := errors.AsType[*usageError](err); ok {
		r.warn(err.Error())
		return &ExitCodeError{Code: ExitUsage, Err: err}
	}
	if (r.opts.OutputFormat == OutputJSON || r.opts.OutputFormat == OutputJSONL) && r.c != nil && r.sess != nil {
		_ = r.w.finish(api.ExecResult{
			SessionID: r.sess.ID,
			Status:    api.ExecResultFailed,
			Text:      r.tracker.Text(),
			Error:     err.Error(),
		})
	}
	r.warn(err.Error())
	return &ExitCodeError{Code: ExitError, Err: err}
}

// warn writes a diagnostic line to stderr.
func (r *run) warn(msg string) {
	if strings.TrimSpace(msg) == "" {
		return
	}
	_, _ = fmt.Fprintf(r.opts.Stderr, "tasksmith exec: %s\n", msg)
}

// usageError marks an invalid invocation (missing prompt, bad flags).
type usageError struct{ msg string }

func (e *usageError) Error() string { return e.msg }

func newUsageError(msg string) error { return &usageError{msg: msg} }

// permissionSummary names the pending permissions for the guard message.
func permissionSummary(reqs []api.PermissionRequest) string {
	parts := make([]string, 0, len(reqs))
	for _, req := range reqs {
		label := req.Summary
		if label == "" {
			label = req.ToolName
		}
		parts = append(parts, label)
	}
	return fmt.Sprintf("run needs a permission decision it cannot answer: %s", strings.Join(parts, "; "))
}

// questionSummary names the pending question requests for the guard.
func questionSummary(reqs []api.QuestionRequest) string {
	parts := make([]string, 0, len(reqs))
	for _, req := range reqs {
		label := req.Title
		if label == "" {
			label = fmt.Sprintf("%d question(s)", len(req.Questions))
		}
		parts = append(parts, label)
	}
	return fmt.Sprintf("run needs an answer it cannot provide: %s", strings.Join(parts, "; "))
}
