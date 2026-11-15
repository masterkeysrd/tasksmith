package store

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"iter"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/masterkeysrd/kite/extras/kites"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
	"github.com/masterkeysrd/tasksmith/internal/cli/mentions"
	"github.com/masterkeysrd/tasksmith/internal/daemon"
)

// SessionState holds the active session and its configuration, permission mode,
// display model label, background tasks, and context window state.
type SessionState struct {
	Session    *api.Session
	Config     api.SessionConfig
	Context    api.ContextState
	Usage      api.SessionUsage
	ModelLabel string
	// ModelID is the identifier of the model ModelLabel was derived from. A
	// session with no explicit model selection still resolves to one (agent or
	// workspace default), so this is what identifies the active model when
	// Config.Model is empty or an alias.
	ModelID   string
	Thinking  string
	Tasks     *[]api.Task
	Subagents *[]api.Session
	Ready     bool

	// SandboxMode is the sandbox isolation backend the active or draft session
	// runs under ("native" or "local").
	SandboxMode api.SandboxMode
}

var session = kites.Create(SessionState{Config: api.SessionConfig{Mode: api.ModeDefault}})

// ActiveModelID reports which model the session actually runs on. A draft
// session selects none explicitly — the daemon resolves it to the agent or
// workspace default — and an explicit selection may be an alias, so the
// resolved identifier takes precedence over the raw configuration values.
func (s SessionState) ActiveModelID() string {
	if id := strings.TrimSpace(s.ModelID); id != "" {
		return id
	}
	if s.Session != nil {
		if id := modelIdentifier(s.Session.Model); id != "" {
			return id
		}
		return strings.TrimSpace(s.Session.Config.Model)
	}
	return strings.TrimSpace(s.Config.Model)
}

var (
	sessMu               sync.Mutex
	activeClient         *client.Client
	activeOpts           InitSessionOptions
	activeCtx            context.Context
	activeCancel         context.CancelFunc
	streamCancel         context.CancelFunc
	modeDebounceTimer    *time.Timer
	modeDebounceDelay    = 500 * time.Millisecond
	workspaceSandboxMode api.SandboxMode
	diagnosticSession    string
	diagnosticConfig     api.SessionConfig
	displayedDiagnostics []api.SessionDiagnostic
)

func cancelModeDebounceLocked() {
	if modeDebounceTimer != nil {
		modeDebounceTimer.Stop()
		modeDebounceTimer = nil
	}
}

// InitSessionOptions specifies options for initializing or connecting to a session.
type InitSessionOptions struct {
	SessionID string
	Agent     string
	Model     string
	Title     string

	// Sandbox is the requested sandbox isolation backend ("native" or
	// "local") for sessions this REPL creates; empty lets the daemon
	// resolve the host default.
	Sandbox     api.SandboxMode
	StartDaemon func(ctx context.Context, socketPath string) error
}

// InitSession initializes or attaches to a session. It starts background watchers
// for session metadata and stream events.
func InitSession(ctx context.Context, c *client.Client, opts InitSessionOptions) {
	sessMu.Lock()
	cancelModeDebounceLocked()
	if activeCancel != nil {
		activeCancel()
	}
	if streamCancel != nil {
		streamCancel()
		streamCancel = nil
	}
	subCtx, cancel := context.WithCancel(ctx)
	activeCtx = subCtx
	activeCancel = cancel
	activeClient = c
	activeOpts = opts
	workspaceSandboxMode = ""
	sessMu.Unlock()

	var sess *api.Session
	sid := strings.TrimSpace(opts.SessionID)
	if sid != "" {
		if loaded, err := c.GetSession(subCtx, sid); err == nil && loaded != nil {
			sess = loaded
			title := strings.TrimSpace(opts.Title)
			agent := strings.TrimSpace(opts.Agent)
			model := strings.TrimSpace(opts.Model)
			if title != "" || agent != "" || model != "" {
				if updated, err := c.UpdateSession(subCtx, sid, api.UpdateSessionRequest{
					Title: title,
					Config: api.SessionConfig{
						Agent: agent,
						Model: model,
					},
				}); err == nil && updated != nil {
					sess = updated
				}
			}
			requested := api.SandboxMode(strings.ToLower(strings.TrimSpace(string(opts.Sandbox))))
			active := api.SandboxMode(strings.ToLower(string(sess.SandboxMode)))
			if requested != "" && active != "" && requested != active {
				AppendWarning(fmt.Sprintf("Session %s already runs under the %s sandbox backend; the --sandbox %s request cannot be applied to an existing session.", sid, sess.SandboxMode, requested))
			}
			SetSession(sess)
			globalConversation.reset()
			globalConversation.setSessionID(sid)
		}
	}

	var ws *api.Workspace
	if loaded, err := c.GetWorkspace(subCtx); err == nil {
		ws = loaded
		SetWorkspaceWarnings(ws.Warnings)
		sessMu.Lock()
		workspaceSandboxMode = api.SandboxMode(strings.ToLower(strings.TrimSpace(string(ws.SandboxMode))))
		sessMu.Unlock()
		SetDefaultAgent(strings.TrimSpace(ws.DefaultAgent))
	}
	// MCP servers connect in the background when the workspace opens, so the
	// workspace payload fetched above can still describe them as connecting. Cache
	// the listing now and keep watching briefly so a server that needs a login or
	// failed to start reports itself in the timeline instead of staying invisible.
	if servers, err := c.ListMCPs(subCtx); err == nil {
		SetWorkspaceMCPServers(servers)
		watchMCPSettlement(subCtx, c, servers)
	}
	if cmds, err := c.ListCommands(subCtx); err == nil {
		SetWorkspaceCommands(cmds)
	}
	if skills, err := c.ListSkills(subCtx); err == nil {
		SetWorkspaceSkills(skills)
	}
	if rules, err := c.ListRules(subCtx); err == nil {
		SetWorkspaceRules(rules)
	}
	var agents []api.Agent
	if ags, err := c.ListAgents(subCtx); err == nil {
		agents = ags
		SetWorkspaceAgents(ags)
	}

	if sess == nil {
		cfg := GetSession().Config
		cfg.Agent, cfg.Model = opts.Agent, opts.Model
		if agent := strings.TrimSpace(opts.Agent); agent != "" {
			for _, ag := range agents {
				if ag.Name == agent {
					if !ag.CanHumanTrigger() {
						AppendError("Error", fmt.Sprintf("Agent %q cannot be triggered by human (triggers: %s)", ag.Name, strings.Join(ag.Triggers, ", ")))
						cfg.Agent = ""
					}
					break
				}
			}
		}
		session.Set(func(s SessionState) SessionState {
			s.Config = cfg
			s.SandboxMode = draftSandboxMode()
			return s
		})

		if ws != nil && strings.TrimSpace(cfg.Agent) == "" && strings.TrimSpace(cfg.Model) == "" {
			SetSessionModelInfo(ws.DefaultModel, cfg.Thinking)
		} else {
			updateDraftModel(subCtx, c, cfg)
		}
	}

	// Tool statuses depend on the effective agent, which is only known once the
	// session config and workspace default agent are resolved.
	refreshWorkspaceTools(subCtx, c, effectiveToolAgent())

	if sid != "" && sess != nil {
		FreezeTranscript()
		if tl, err := c.Timeline(subCtx, sid); err == nil {
			globalConversation.loadTimeline(tl, sess.PendingQuestions)
		}
		if len(sess.PendingPermissions) > 0 {
			globalConversation.SeedPermissions(sess.PendingPermissions)
		}
		globalConversation.SeedQuestions(sess.PendingQuestions)
		if len(sess.Queue) > 0 {
			SeedQueue(sess.Queue)
		}
		if len(sess.Todos) > 0 {
			SeedTodos(sess.Todos)
		}
		showSessionDiagnostics(sess, true)
		startSessionStreamWatcher(subCtx, sid)
	}

	startSessionsWatcher(subCtx)
}

func startSessionStreamWatcher(ctx context.Context, sessionID string) {
	sessMu.Lock()
	if streamCancel != nil {
		streamCancel()
	}
	watchCtx, cancel := context.WithCancel(ctx)
	streamCancel = cancel
	c := activeClient
	sessMu.Unlock()

	if c == nil || sessionID == "" {
		return
	}

	go loadSessionUsage(watchCtx, c, sessionID)

	go func() {
		var since string
		for {
			if watchCtx.Err() != nil {
				return
			}
			for ev, err := range c.Watch(watchCtx, since, api.SessionStreamTopic(sessionID)) {
				if err != nil {
					if client.IsNeedSync(err) {
						loadSessionUsage(watchCtx, c, sessionID)
						sess, sessErr := c.GetSession(watchCtx, sessionID)
						if tl, tlErr := c.Timeline(watchCtx, sessionID); tlErr == nil {
							if sessErr == nil && sess != nil {
								globalConversation.loadTimeline(tl, sess.PendingQuestions)
							} else {
								globalConversation.loadTimeline(tl)
							}
						}
						if sessErr == nil && sess != nil {
							globalConversation.reconcilePermissions(sess.PendingPermissions)
							globalConversation.SeedQuestions(sess.PendingQuestions)
						}
						since = ""
					}
					break
				}
				if ev.ID != "" {
					since = ev.ID
				}
				globalConversation.applyEvent(ev)
			}
			select {
			case <-watchCtx.Done():
				return
			case <-time.After(100 * time.Millisecond):
			}
		}
	}()
}

func startSessionsWatcher(ctx context.Context) {
	sessMu.Lock()
	c := activeClient
	sessMu.Unlock()

	if c == nil {
		return
	}

	go func() {
		var since string
		for {
			if ctx.Err() != nil {
				return
			}
			for ev, err := range c.Watch(ctx, since, api.TopicSessions) {
				if err != nil {
					if ctx.Err() != nil {
						return
					}
					if client.IsNeedSync(err) {
						since = ""
						break
					}
					if !isDaemonHealthy(ctx, c) {
						if GetApp().DaemonStatus == DaemonConnected {
							SetDaemonStatus(DaemonDisconnected)
							st := GetStream()
							if st.Streaming || st.WaitingPermission || st.WaitingQuestion || st.PendingCommand != "" {
								CancelTurn()
							}
						}

						ticker := time.NewTicker(250 * time.Millisecond)
						for !isDaemonHealthy(ctx, c) {
							select {
							case <-ctx.Done():
								ticker.Stop()
								return
							case <-ticker.C:
							}
						}
						ticker.Stop()

						since = ""
						resyncDaemonState(ctx, c)
						SetDaemonStatus(DaemonConnected)
					}
					break
				}
				if ev.ID != "" {
					since = ev.ID
				}
				if p, ok := ev.Payload.(api.SessionUpdatedEvent); ok {
					sessMu.Lock()
					curr := GetSession().Session
					sessMu.Unlock()
					if curr != nil {
						if curr.ID == p.Session.ID {
							sess := p.Session
							SetSession(&sess)
							showSessionDiagnostics(&sess, false)
						} else if p.Session.ParentID != "" && p.Session.ParentID == curr.ID {
							UpsertSubagent(p.Session)
							refreshSubagentState()
						}
					}
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(100 * time.Millisecond):
			}
		}
	}()
}

// draftSandboxMode predicts the sandbox backend a draft session will get:
// an explicit local request is always honored, otherwise the workspace's
// host default (native when the kernel backend is available).
func draftSandboxMode() api.SandboxMode {
	sessMu.Lock()
	requested := api.SandboxMode(strings.ToLower(strings.TrimSpace(string(activeOpts.Sandbox))))
	wsMode := workspaceSandboxMode
	sessMu.Unlock()
	if requested == api.SandboxLocal {
		return api.SandboxLocal
	}
	if wsMode != "" {
		return wsMode
	}
	return api.SandboxNative
}

func getOrCreateSession(ctx context.Context) (*api.Session, error) {
	sessMu.Lock()
	sess := GetSession().Session
	if sess != nil && sess.ID != "" {
		sessMu.Unlock()
		return sess, nil
	}
	if activeClient == nil {
		sessMu.Unlock()
		return nil, errors.New("repl: no active client to create session")
	}
	title := strings.TrimSpace(activeOpts.Title)
	c := activeClient
	agent := activeOpts.Agent
	model := activeOpts.Model
	sandbox := activeOpts.Sandbox
	cfg := GetSession().Config
	mode := cfg.Mode
	sessMu.Unlock()

	created, err := c.CreateSession(ctx, api.CreateSessionRequest{
		Title: title,
		Config: api.SessionConfig{
			Agent:    cmp.Or(cfg.Agent, agent),
			Model:    cmp.Or(cfg.Model, model),
			Thinking: cfg.Thinking,
			Mode:     mode,
			Sandbox:  sandbox,
		},
	})
	if err != nil {
		return nil, err
	}
	SetSession(created)
	globalConversation.setSessionID(created.ID)
	showSessionDiagnostics(created, false)
	startSessionStreamWatcher(ctx, created.ID)
	return created, nil
}

// Send sends a user prompt message to the active session. If no session exists yet,
// one is created lazily.
func Send(text string, attachments ...api.Attachment) {
	SendWithUserText(text, text, attachments...)
}

// SendWithUserText sends text to the active session while scanning only userText
// (rather than any server-rendered prompt or MCP resource body in text) for
// structured @-mention reconciliation and 3.4 fallback lookup.
func SendWithUserText(text, userText string, attachments ...api.Attachment) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return
	}
	reconciled := mentions.Resolve(userText, attachments, nil)
	FreezeTranscript()
	msgID := uuid.Must(uuid.NewV7()).String()
	globalConversation.addPendingUser(msgID, text, reconciled)

	if status := GetApp().DaemonStatus; status == DaemonDisconnected || status == DaemonRestarting {
		globalConversation.cancelPendingUser(msgID, errors.New("daemon is not running (run /daemon start or /daemon restart)"))
		return
	}

	sessMu.Lock()
	c := activeClient
	ctx := activeCtx
	sessMu.Unlock()

	if c == nil {
		globalConversation.cancelPendingUser(msgID, errors.New("repl: no active client"))
		return
	}

	go func() {
		if ctx == nil || ctx.Err() != nil {
			ctx = context.Background()
		}

		sess, err := getOrCreateSession(ctx)
		if err != nil {
			globalConversation.cancelPendingUser(msgID, err)
			return
		}

		finalAttachments := mentions.Resolve(userText, reconciled, func(cand string) (string, bool) {
			q := strings.TrimSuffix(cand, "/")
			files, err := c.ListWorkspaceFiles(ctx, api.ListFilesRequest{
				Query:     q,
				SessionID: sess.ID,
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
		if len(finalAttachments) > len(reconciled) {
			globalConversation.setPendingUserAttachments(msgID, finalAttachments)
		}

		err = c.Send(ctx, sess.ID, api.SendRequest{
			ID:          msgID,
			Content:     text,
			Attachments: finalAttachments,
		})
		if err != nil {
			globalConversation.cancelPendingUser(msgID, err)
		}
	}()
}

// SendCommand executes a direct user shell command (!) in the active session sandbox
// without triggering an LLM turn. If no session exists yet, one is created lazily.
func SendCommand(cmd string) {
	trimmed := strings.TrimSpace(cmd)
	if trimmed == "" {
		return
	}
	FreezeTranscript()
	msgID := uuid.Must(uuid.NewV7()).String()
	globalConversation.addPendingCommand(msgID, trimmed)

	if status := GetApp().DaemonStatus; status == DaemonDisconnected || status == DaemonRestarting {
		globalConversation.cancelPendingUser(msgID, errors.New("daemon is not running (run /daemon start or /daemon restart)"))
		return
	}

	sessMu.Lock()
	c := activeClient
	ctx := activeCtx
	sessMu.Unlock()

	go func() {
		if ctx == nil || ctx.Err() != nil {
			ctx = context.Background()
		}

		sess, err := getOrCreateSession(ctx)
		if err != nil {
			globalConversation.cancelPendingUser(msgID, err)
			return
		}

		err = c.Send(ctx, sess.ID, api.SendRequest{
			ID:      msgID,
			Command: trimmed,
		})
		if err != nil {
			globalConversation.cancelPendingUser(msgID, err)
		}
	}()
}

// SendCompact triggers manual context compaction for the active session.
func SendCompact(focus string) {
	if status := GetApp().DaemonStatus; status == DaemonDisconnected || status == DaemonRestarting {
		AppendError("Error", "daemon is not running (run /daemon start or /daemon restart)")
		return
	}

	sessMu.Lock()
	sess := GetSession().Session
	c := activeClient
	ctx := activeCtx
	sessMu.Unlock()

	if sess == nil || sess.ID == "" {
		AppendNotice("Nothing to compact")
		return
	}

	FreezeTranscript()
	msgID := uuid.Must(uuid.NewV7()).String()

	if c == nil {
		AppendError("Error", "repl: no active client")
		return
	}

	go func() {
		if ctx == nil || ctx.Err() != nil {
			ctx = context.Background()
		}
		err := c.Send(ctx, sess.ID, api.SendRequest{
			ID:      msgID,
			Compact: &api.CompactRequest{Focus: strings.TrimSpace(focus)},
		})
		if err != nil {
			AppendError("Error", err.Error())
		}
	}()
}

// Cancel aborts the active turn if streaming.
func Cancel() {
	if !GetStream().Streaming {
		return
	}
	sessMu.Lock()
	c := activeClient
	ctx := activeCtx
	sess := GetSession().Session
	sessMu.Unlock()

	if sess == nil || sess.ID == "" {
		globalConversation.cancelTurn()
		return
	}

	go func() {
		if ctx == nil || ctx.Err() != nil {
			ctx = context.Background()
		}
		_ = c.Cancel(ctx, sess.ID)
		globalConversation.cancelTurn()
	}()
}

func SubmitQuestion(req api.QuestionRequest, response api.QuestionResponse, done func(error)) {
	sessMu.Lock()
	c, ctx := activeClient, activeCtx
	sess := GetSession().Session
	sessMu.Unlock()
	if sess == nil || sess.ID == "" || req.ID == "" {
		if done != nil {
			done(fmt.Errorf("no active session for question"))
		}
		return
	}
	response.RequestID, response.ToolCallID = req.ID, req.ToolCallID
	sessionID := sess.ID
	go func() {
		if ctx == nil || ctx.Err() != nil {
			ctx = context.Background()
		}
		err := c.Send(ctx, sessionID, api.SendRequest{Question: &response})
		if err == nil {
			globalConversation.mu.Lock()
			if globalConversation.sessionID == sessionID {
				globalConversation.removeQuestionLocked(req.ID)
				globalConversation.publishLocked()
			}
			globalConversation.mu.Unlock()
		}
		if done != nil {
			done(err)
		}
	}()
}

// DecidePermission submits a user decision on a pending permission request.
func DecidePermission(req api.PermissionRequest, dec api.PermissionDecision) {
	if req.ID == "" {
		return
	}
	dec.RequestID = req.ID
	dec.ToolCallID = req.ToolCallID

	RemoveSlot("perm-" + req.ID)
	globalConversation.removePermission(req.ID)

	sessMu.Lock()
	c := activeClient
	ctx := activeCtx
	sess := GetSession().Session
	sessMu.Unlock()

	if c == nil || sess == nil || sess.ID == "" {
		return
	}

	go func() {
		if ctx == nil || ctx.Err() != nil {
			ctx = context.Background()
		}
		err := c.Send(ctx, sess.ID, api.SendRequest{Decision: &dec})
		if err != nil {
			AppendError("Error", err.Error())
			globalConversation.SeedPermissions([]api.PermissionRequest{req})
		}
	}()
}

// UseSession subscribes a component to a comparable slice of SessionState.
func UseSession[U comparable](selector func(SessionState) U) U {
	return kites.Use(session, selector)
}

// GetSession returns the current SessionState snapshot.
func GetSession() SessionState {
	return session.Get()
}

// UseContext subscribes a component to the active session's ContextState.
func UseContext() api.ContextState {
	return UseSession(func(s SessionState) api.ContextState {
		return s.Context
	})
}

// GetContext returns the current ContextState snapshot.
func GetContext() api.ContextState {
	return GetSession().Context
}

// UseMode returns the active permission mode, defaulting to api.ModeDefault if unset.
func UseMode() api.PermissionMode {
	return UseSession(func(s SessionState) api.PermissionMode {
		return cmp.Or(s.Config.Mode, api.ModeDefault)
	})
}

func showSessionDiagnostics(sess *api.Session, force bool) {
	if sess == nil {
		return
	}
	sessMu.Lock()
	changed := force || diagnosticSession != sess.ID || diagnosticConfig.Agent != sess.Config.Agent || diagnosticConfig.Model != sess.Config.Model || !reflect.DeepEqual(diagnosticConfig.Thinking, sess.Config.Thinking)
	previous := displayedDiagnostics
	if changed {
		previous = nil
	}
	var notices []string
	seen := slices.Clone(previous)
	for _, diagnostic := range sess.Diagnostics {
		if !slices.Contains(seen, diagnostic) {
			notices = append(notices, diagnostic.Message)
			seen = append(seen, diagnostic)
		}
	}
	diagnosticSession = sess.ID
	diagnosticConfig = sess.Config
	if sess.Config.Thinking != nil {
		thinking := *sess.Config.Thinking
		diagnosticConfig.Thinking = &thinking
	}
	displayedDiagnostics = slices.Clone(sess.Diagnostics)
	sessMu.Unlock()
	for _, notice := range notices {
		AppendWarning("Warning: " + notice)
	}
}

// SetSession updates the session and its configuration. If sess is non-nil,
// Mode defaults to api.ModeDefault if empty.
func SetSession(sess *api.Session) {
	sessMu.Lock()
	cancelModeDebounceLocked()
	sessMu.Unlock()

	session.Set(func(s SessionState) SessionState {
		if sess == nil || s.Session == nil || s.Session.ID != sess.ID {
			s.Usage = api.SessionUsage{}
		}
		s.Session = sess
		if sess != nil {
			s.ModelID = modelIdentifier(sess.Model)
			s.ModelLabel, s.Thinking = modelDisplay(sess.Model, sess.Config.Thinking)
			s.SandboxMode = sess.SandboxMode
			s.Ready = true
			s.Context = sess.Context
			s.Config = sess.Config
			if s.Config.Mode == "" {
				s.Config.Mode = api.ModeDefault
			}
			if s.Session.Config.Mode == "" {
				s.Session.Config.Mode = api.ModeDefault
			}
			if sess.Tasks != nil {
				tasksCopy := make([]api.Task, len(sess.Tasks))
				copy(tasksCopy, sess.Tasks)
				s.Tasks = &tasksCopy
			} else {
				s.Tasks = nil
			}
			if sess.Subagents != nil {
				subagentsCopy := make([]api.Session, len(sess.Subagents))
				copy(subagentsCopy, sess.Subagents)
				s.Subagents = &subagentsCopy
			} else {
				s.Subagents = nil
			}
		} else {
			s.Tasks = nil
			s.Subagents = nil
			s.SandboxMode = ""
		}
		return s
	})
}

// loadSessionUsage fetches the persisted usage of a session so that totals
// are available before the first usage_updated event arrives.
func loadSessionUsage(parent context.Context, c *client.Client, sessionID string) {
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	usage, err := c.SessionUsage(ctx, sessionID)
	if err != nil {
		return
	}
	SetUsage(usage)
}

// SetUsage stores usage as the active session's usage when it belongs to that session.
func SetUsage(usage api.SessionUsage) {
	session.Set(func(s SessionState) SessionState {
		if s.Session != nil && s.Session.ID == usage.SessionID {
			s.Usage = usage
		}
		return s
	})
}

// UsageTotals is the comparable subset of session usage shown in the status line.
type UsageTotals struct {
	Calls         int64
	UnpricedCalls int64
	CostNano      int64
	InputTokens   int64
	OutputTokens  int64
}

// UseUsageTotals subscribes a component to the active session's usage totals.
func UseUsageTotals() UsageTotals {
	return UseSession(func(s SessionState) UsageTotals {
		t := s.Usage.Totals
		return UsageTotals{Calls: t.Calls, UnpricedCalls: t.UnpricedCalls, CostNano: t.CostNano, InputTokens: t.Tokens.Input, OutputTokens: t.Tokens.Output}
	})
}

// GetUsage returns the active session's usage snapshot.
func GetUsage() api.SessionUsage {
	return GetSession().Usage
}

// UpsertTask updates an existing task in SessionState by ID, or appends it.
func UpsertTask(task api.Task) {
	session.Set(func(s SessionState) SessionState {
		var current []api.Task
		if s.Tasks != nil {
			current = make([]api.Task, len(*s.Tasks))
			copy(current, *s.Tasks)
		}
		found := false
		for i, t := range current {
			if t.ID == task.ID {
				current[i] = task
				found = true
				break
			}
		}
		if !found {
			current = append(current, task)
		}
		s.Tasks = &current
		if s.Session != nil {
			s.Session.Tasks = current
		}
		return s
	})
}

// UseTasks subscribes a component to the active session's tasks.
func UseTasks() []api.Task {
	tasks := UseSession(func(s SessionState) *[]api.Task {
		return s.Tasks
	})
	if tasks == nil {
		return nil
	}
	return *tasks
}

// UseRunningTasks subscribes a component to the active session's running and pending tasks.
func UseRunningTasks() []api.Task {
	all := UseTasks()
	var running []api.Task
	for _, t := range all {
		if t.Status == api.TaskStatusRunning || t.Status == api.TaskStatusPending {
			running = append(running, t)
		}
	}
	return running
}

// GetTasks returns a snapshot of background tasks for the active session.
func GetTasks() []api.Task {
	st := GetSession()
	if st.Tasks == nil {
		return nil
	}
	return *st.Tasks
}

// GetRunningTasks returns a snapshot of active running and pending tasks for the active session.
func GetRunningTasks() []api.Task {
	all := GetTasks()
	var running []api.Task
	for _, t := range all {
		if t.Status == api.TaskStatusRunning || t.Status == api.TaskStatusPending {
			running = append(running, t)
		}
	}
	return running
}

// KillTask sends a kill request for an active background task in the active session.
func KillTask(taskID string) error {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return errors.New("repl: task ID must not be empty")
	}
	sessMu.Lock()
	c := activeClient
	ctx := activeCtx
	sess := GetSession().Session
	sessMu.Unlock()

	if sess == nil || sess.ID == "" {
		return errors.New("repl: no active session")
	}
	if ctx == nil || ctx.Err() != nil {
		ctx = context.Background()
	}
	return c.KillTask(ctx, sess.ID, taskID)
}

// StreamTaskOutput streams live output chunks for a background task.
func StreamTaskOutput(ctx context.Context, taskID string) iter.Seq2[string, error] {
	sessMu.Lock()
	c := activeClient
	sess := GetSession().Session
	sessMu.Unlock()

	if sess == nil || sess.ID == "" {
		return func(yield func(string, error) bool) {
			yield("", errors.New("repl: no active session"))
		}
	}
	return c.StreamTaskOutput(ctx, sess.ID, taskID)
}

// NextPermissionMode returns the next permission mode in the cycle sequence:
// default -> accept_edits -> plan -> ask -> auto -> default.
func NextPermissionMode(current api.PermissionMode) api.PermissionMode {
	switch current {
	case "", api.ModeDefault:
		return api.ModeAcceptEdits
	case api.ModeAcceptEdits:
		return api.ModePlan
	case api.ModePlan:
		return api.ModeAsk
	case api.ModeAsk:
		return api.ModeAuto
	default:
		return api.ModeDefault
	}
}

// CycleMode atomically advances to the next permission mode in sequence and returns the new mode.
func CycleMode() api.PermissionMode {
	next := NextPermissionMode(GetSession().Config.Mode)
	SetMode(next)
	return next
}

// SetSessionMode updates the permission mode in SessionState without syncing to the daemon.
func SetSessionMode(mode api.PermissionMode) {
	if mode == "" {
		mode = api.ModeDefault
	}
	session.Set(func(s SessionState) SessionState {
		s.Config.Mode = mode
		if s.Session != nil {
			s.Session.Config.Mode = mode
		}
		return s
	})
}

// SetMode updates the active permission mode immediately and debounces syncing to the daemon.
func SetMode(mode api.PermissionMode) {
	if mode == "" {
		mode = api.ModeDefault
	}
	var sessID string
	session.Set(func(s SessionState) SessionState {
		s.Config.Mode = mode
		if s.Session != nil {
			sessID = s.Session.ID
		}
		return s
	})
	if sessID != "" {
		sessMu.Lock()
		defer sessMu.Unlock()
		cancelModeDebounceLocked()
		c := activeClient
		if c == nil {
			return
		}
		targetSessID := sessID
		modeDebounceTimer = time.AfterFunc(modeDebounceDelay, func() {
			sessMu.Lock()
			cl := activeClient
			ctx := activeCtx
			sessMu.Unlock()
			if cl == nil {
				return
			}
			current := GetSession()
			if current.Session == nil || current.Session.ID != targetSessID {
				return
			}
			currentMode := cmp.Or(current.Config.Mode, api.ModeDefault)
			persistedMode := cmp.Or(current.Session.Config.Mode, api.ModeDefault)
			if currentMode == persistedMode {
				return
			}
			if ctx == nil || ctx.Err() != nil {
				ctx = context.Background()
			}
			updated, err := cl.UpdateSession(ctx, targetSessID, api.UpdateSessionRequest{
				Config: api.SessionConfig{Mode: currentMode},
			})
			if err == nil && updated != nil {
				session.Set(func(s SessionState) SessionState {
					if s.Session != nil && s.Session.ID == targetSessID {
						s.Session.Config.Mode = currentMode
					}
					return s
				})
			}
		})
	}
}

// SetModelInfo sets the display model label and reasoning effort.
func SetModelInfo(label, thinking string) {
	session.Set(func(s SessionState) SessionState {
		s.ModelLabel = label
		s.Thinking = thinking
		s.Ready = true
		return s
	})
}

// SetSessionModelInfo records a resolved model: its identifier, display label,
// and reasoning effort. The identifier is what lets `/model` highlight the
// active row even when no model was selected explicitly.
func SetSessionModelInfo(model *api.Model, setting *api.ThinkingSetting) {
	label, thinking := modelDisplay(model, setting)
	session.Set(func(s SessionState) SessionState {
		s.ModelID = modelIdentifier(model)
		s.ModelLabel = label
		s.Thinking = thinking
		s.Ready = true
		return s
	})
}

// SetActiveModelID records the identifier of the model the current label refers
// to, for updates that carry an identifier but no model description.
func SetActiveModelID(id string) {
	session.Set(func(s SessionState) SessionState {
		s.ModelID = strings.TrimSpace(id)
		return s
	})
}

func modelIdentifier(model *api.Model) string {
	if model == nil {
		return ""
	}
	return strings.TrimSpace(model.ID)
}

// SetSessionAgent updates the active agent name in SessionState.
func SetSessionAgent(agent string) {
	session.Set(func(s SessionState) SessionState {
		s.Config.Agent = agent
		if s.Session != nil {
			s.Session.Config.Agent = agent
		}
		return s
	})
}

// SetClient sets the active daemon client in the session store.
func SetClient(c *client.Client) {
	sessMu.Lock()
	defer sessMu.Unlock()
	activeClient = c
}

// Client returns the currently active daemon client, if any.
func Client() *client.Client {
	sessMu.Lock()
	defer sessMu.Unlock()
	return activeClient
}

// SocketPath returns the Unix domain socket path of the active client, if any.
func SocketPath() string {
	sessMu.Lock()
	defer sessMu.Unlock()
	if activeClient != nil {
		return activeClient.SocketPath()
	}
	return ""
}

func isDaemonHealthy(ctx context.Context, c *client.Client) bool {
	if c == nil {
		return false
	}
	pingCtx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	_, err := c.Health(pingCtx)
	return err == nil
}

// effectiveToolAgent returns the agent whose tool statuses should be cached:
// the active session's agent, falling back to the draft config agent and then
// the workspace default agent.
func effectiveToolAgent() string {
	st := GetSession()
	if agent := strings.TrimSpace(st.Config.Agent); agent != "" {
		return agent
	}
	return strings.TrimSpace(GetApp().DefaultAgent)
}

// RefreshWorkspaceTools fetches the workspace tools classified for agent using
// the active client and caches them in AppState. It returns the number of
// cached tools, and is a no-op when the cache already holds tools for that
// agent.
func RefreshWorkspaceTools(ctx context.Context, agent string) (int, error) {
	return refreshWorkspaceToolsForce(ctx, Client(), agent, false)
}

// refreshWorkspaceTools fetches the workspace tools classified for agent and
// caches them in AppState. The fetch is skipped when the cache already holds
// tools for the same agent, unless force is true.
func refreshWorkspaceTools(ctx context.Context, c *client.Client, agent string) {
	_, _ = refreshWorkspaceToolsForce(ctx, c, agent, false)
}

func refreshWorkspaceToolsForce(ctx context.Context, c *client.Client, agent string, force bool) (int, error) {
	if c == nil {
		return 0, errors.New("no daemon connection")
	}
	// The recorded agent plus the fetched flag form the freshness key: an empty
	// toolset is a valid cached result and must not trigger a re-fetch.
	if appState := GetApp(); !force && appState.ToolsFetched && appState.ToolsAgent == agent {
		return 0, nil
	}
	tools, err := c.ListTools(ctx, agent)
	if err != nil {
		if agent == "" {
			return 0, err
		}
		// An agent the daemon no longer resolves would otherwise leave the tool
		// browser empty, so fall back to the unclassified workspace listing while
		// still marking the cache as belonging to the requested agent.
		unscoped, fallbackErr := c.ListTools(ctx, "")
		if fallbackErr != nil {
			return 0, err
		}
		tools = unscoped
	}
	SetWorkspaceTools(tools, agent)
	return len(tools), nil
}

func resyncDaemonState(ctx context.Context, c *client.Client) {
	if c == nil {
		return
	}
	if ws, err := c.GetWorkspace(ctx); err == nil && ws != nil {
		SetWorkspaceWarnings(ws.Warnings)
		sessMu.Lock()
		workspaceSandboxMode = api.SandboxMode(strings.ToLower(strings.TrimSpace(string(ws.SandboxMode))))
		sessMu.Unlock()
		SetDefaultAgent(strings.TrimSpace(ws.DefaultAgent))
	}
	// A resync follows a daemon restart, so MCP connections are starting over and
	// the cached health is stale; re-list and keep watching while they settle.
	if servers, err := c.ListMCPs(ctx); err == nil {
		SetWorkspaceMCPServers(servers)
		watchMCPSettlement(ctx, c, servers)
	}
	if cmds, err := c.ListCommands(ctx); err == nil {
		SetWorkspaceCommands(cmds)
	}
	if skills, err := c.ListSkills(ctx); err == nil {
		SetWorkspaceSkills(skills)
	}
	if rules, err := c.ListRules(ctx); err == nil {
		SetWorkspaceRules(rules)
	}
	if agents, err := c.ListAgents(ctx); err == nil {
		SetWorkspaceAgents(agents)
	}
	refreshWorkspaceTools(ctx, c, effectiveToolAgent())

	sessMu.Lock()
	sess := GetSession().Session
	opts := activeOpts
	pCtx := activeCtx
	sessMu.Unlock()

	if pCtx == nil || pCtx.Err() != nil {
		pCtx = context.Background()
	}

	if sess == nil {
		cfg := GetSession().Config
		cfg.Agent, cfg.Model = opts.Agent, opts.Model
		session.Set(func(s SessionState) SessionState {
			s.Config = cfg
			s.SandboxMode = draftSandboxMode()
			return s
		})

		ws, _ := c.GetWorkspace(ctx)
		if ws != nil && strings.TrimSpace(cfg.Agent) == "" && strings.TrimSpace(cfg.Model) == "" {
			SetSessionModelInfo(ws.DefaultModel, cfg.Thinking)
		} else {
			updateDraftModel(ctx, c, cfg)
		}
	} else {
		if loadedSess, err := c.GetSession(ctx, sess.ID); err == nil && loadedSess != nil {
			SetSession(loadedSess)
			sess = loadedSess
		}
		if tl, err := c.Timeline(ctx, sess.ID); err == nil {
			globalConversation.loadTimeline(tl, sess.PendingQuestions)
		}
		loadSessionUsage(ctx, c, sess.ID)
		if len(sess.PendingPermissions) > 0 {
			globalConversation.SeedPermissions(sess.PendingPermissions)
		}
		globalConversation.SeedQuestions(sess.PendingQuestions)
		if len(sess.Queue) > 0 {
			SeedQueue(sess.Queue)
		}
		if len(sess.Todos) > 0 {
			SeedTodos(sess.Todos)
		}
		showSessionDiagnostics(sess, false)
		startSessionStreamWatcher(pCtx, sess.ID)
	}
}

// mcpSettleInterval and mcpSettleTimeout bound the background poll that waits for
// MCP servers still connecting when a session initializes. They are variables so
// tests can watch a whole settle without waiting out a real connect budget.
var (
	mcpSettleInterval = 400 * time.Millisecond
	mcpSettleTimeout  = 10 * time.Second
)

// watchMCPSettlement refreshes the cached MCP listing in the background until no
// server is still connecting, the wait expires, or the session context ends. A
// server that settles into auth_required or error is announced in the timeline
// only once its status is known, so this is what makes the announcement appear
// for servers that were still connecting when the CLI started.
func watchMCPSettlement(ctx context.Context, c *client.Client, servers []api.MCPServer) {
	if !hasConnectingMCP(servers) {
		return
	}
	go func() {
		deadline := time.Now().Add(mcpSettleTimeout)
		ticker := time.NewTicker(mcpSettleInterval)
		defer ticker.Stop()
		for time.Now().Before(deadline) {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			updated, err := c.ListMCPs(ctx)
			if err != nil {
				return
			}
			SetWorkspaceMCPServers(updated)
			if !hasConnectingMCP(updated) {
				return
			}
		}
	}()
}

// hasConnectingMCP reports whether any server in the listing is still connecting.
func hasConnectingMCP(servers []api.MCPServer) bool {
	return slices.ContainsFunc(servers, func(server api.MCPServer) bool {
		return server.Status == api.MCPStatusConnecting
	})
}

// StartDaemon ensures the background Brain Daemon is running.
func StartDaemon(ctx context.Context) (*api.HealthResponse, error) {
	sessMu.Lock()
	c := activeClient
	starter := activeOpts.StartDaemon
	sessMu.Unlock()

	if c == nil {
		return nil, errors.New("repl: no active client")
	}

	if h, err := c.Health(ctx); err == nil {
		SetDaemonStatus(DaemonConnected)
		return h, nil
	}

	SetDaemonStatus(DaemonRestarting)

	var spawnErr error
	if starter != nil {
		spawnErr = starter(ctx, c.SocketPath())
	} else {
		spawnErr = daemon.SpawnDetached(c.SocketPath())
	}
	if spawnErr != nil {
		SetDaemonStatus(DaemonDisconnected)
		return nil, fmt.Errorf("spawn daemon: %w", spawnErr)
	}

	pollCtx := ctx
	var cancelPoll context.CancelFunc
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		pollCtx, cancelPoll = context.WithTimeout(ctx, 5*time.Second)
		defer cancelPoll()
	}

	ticker := time.NewTicker(40 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-pollCtx.Done():
			SetDaemonStatus(DaemonDisconnected)
			return nil, fmt.Errorf("timed out waiting for daemon to start: %w", pollCtx.Err())
		case <-ticker.C:
			h, err := c.Health(pollCtx)
			if err == nil {
				resyncDaemonState(pollCtx, c)
				SetDaemonStatus(DaemonConnected)
				return h, nil
			}
		}
	}
}

// StopDaemon gracefully stops the running Brain Daemon.
func StopDaemon(ctx context.Context) error {
	sessMu.Lock()
	c := activeClient
	sessMu.Unlock()

	if c == nil {
		return errors.New("repl: no active client")
	}

	_ = c.Shutdown(ctx)

	stopCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	ticker := time.NewTicker(40 * time.Millisecond)
	defer ticker.Stop()

loop:
	for isDaemonHealthy(stopCtx, c) {
		select {
		case <-stopCtx.Done():
			break loop
		case <-ticker.C:
		}
	}

	CancelTurn()
	SetDaemonStatus(DaemonDisconnected)
	return nil
}

// RestartDaemon stops any running daemon, spawns a new background process,
// waits for it to become healthy, and resyncs state.
func RestartDaemon(ctx context.Context) (*api.HealthResponse, error) {
	sessMu.Lock()
	c := activeClient
	starter := activeOpts.StartDaemon
	sessMu.Unlock()

	if c == nil {
		return nil, errors.New("repl: no active client")
	}

	SetDaemonStatus(DaemonRestarting)
	CancelTurn()

	if isDaemonHealthy(ctx, c) {
		_ = c.Shutdown(ctx)
		stopCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		ticker := time.NewTicker(40 * time.Millisecond)
	loop:
		for isDaemonHealthy(stopCtx, c) {
			select {
			case <-stopCtx.Done():
				break loop
			case <-ticker.C:
			}
		}
		ticker.Stop()
		cancel()
	}

	var spawnErr error
	if starter != nil {
		spawnErr = starter(ctx, c.SocketPath())
	} else {
		spawnErr = daemon.SpawnDetached(c.SocketPath())
	}
	if spawnErr != nil {
		SetDaemonStatus(DaemonDisconnected)
		return nil, fmt.Errorf("spawn daemon: %w", spawnErr)
	}

	pollCtx := ctx
	var cancelPoll context.CancelFunc
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		pollCtx, cancelPoll = context.WithTimeout(ctx, 10*time.Second)
		defer cancelPoll()
	}

	ticker := time.NewTicker(40 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-pollCtx.Done():
			SetDaemonStatus(DaemonDisconnected)
			return nil, fmt.Errorf("timed out waiting for daemon to restart: %w", pollCtx.Err())
		case <-ticker.C:
			h, err := c.Health(pollCtx)
			if err == nil {
				resyncDaemonState(pollCtx, c)
				SetDaemonStatus(DaemonConnected)
				return h, nil
			}
		}
	}
}

// DefaultAgent returns the default agent configured at startup.
func DefaultAgent() string {
	sessMu.Lock()
	defer sessMu.Unlock()
	return activeOpts.Agent
}

// DefaultModel returns the default model configured at startup.
func DefaultModel() string {
	sessMu.Lock()
	defer sessMu.Unlock()
	return activeOpts.Model
}

// NewDraftSession resets the REPL to a new draft session, preserving mode.
func NewDraftSession(modelOverride string) {
	sessMu.Lock()
	cancelModeDebounceLocked()
	if streamCancel != nil {
		streamCancel()
		streamCancel = nil
	}
	c := activeClient
	ctx := activeCtx
	defAgent := activeOpts.Agent
	defModel := activeOpts.Model
	sessMu.Unlock()

	ResetStream()

	currentMode := GetSession().Config.Mode
	if currentMode == "" {
		currentMode = api.ModeDefault
	}
	targetModel := cmp.Or(strings.TrimSpace(modelOverride), defModel)
	cfg := api.SessionConfig{
		Mode:  currentMode,
		Agent: defAgent,
		Model: targetModel,
	}

	session.Set(func(s SessionState) SessionState {
		s.Session = nil
		s.Config = cfg
		s.Tasks = nil
		s.Subagents = nil
		s.Usage = api.SessionUsage{}
		s.SandboxMode = draftSandboxMode()
		return s
	})

	if ctx == nil || ctx.Err() != nil {
		ctx = context.Background()
	}
	updateDraftModel(ctx, c, cfg)
	ResetToDraft()
}

// SwitchSession loads an existing session and switches to it asynchronously.
func SwitchSession(sessionID string) {
	sessMu.Lock()
	cancelModeDebounceLocked()
	c := activeClient
	ctx := activeCtx
	sessMu.Unlock()

	if strings.TrimSpace(sessionID) == "" {
		return
	}

	go func() {
		if ctx == nil || ctx.Err() != nil {
			ctx = context.Background()
		}
		loaded, err := c.GetSession(ctx, sessionID)
		if err != nil {
			AppendError("Error", err.Error())
			return
		}

		sessMu.Lock()
		if activeClient != c {
			sessMu.Unlock()
			return
		}
		if streamCancel != nil {
			streamCancel()
			streamCancel = nil
		}
		sessMu.Unlock()

		SetSession(loaded)
		globalConversation.reset()
		globalConversation.setSessionID(loaded.ID)
		ResetForSessionSwitch()

		if tl, err := c.Timeline(ctx, loaded.ID); err == nil {
			globalConversation.loadTimeline(tl, loaded.PendingQuestions)
		}
		if len(loaded.PendingPermissions) > 0 {
			globalConversation.SeedPermissions(loaded.PendingPermissions)
		}
		globalConversation.SeedQuestions(loaded.PendingQuestions)
		if len(loaded.Queue) > 0 {
			SeedQueue(loaded.Queue)
		}
		if len(loaded.Todos) > 0 {
			SeedTodos(loaded.Todos)
		}
		showSessionDiagnostics(loaded, true)
		startSessionStreamWatcher(ctx, loaded.ID)
	}()
}

// SetModel updates the model for the session, either via daemon API or in draft config.
func SetModel(modelID string) {
	st := GetSession()
	sessMu.Lock()
	c := activeClient
	ctx := activeCtx
	sessMu.Unlock()

	if st.Session != nil && st.Session.ID != "" {
		if c == nil {
			return
		}
		go func() {
			if ctx == nil || ctx.Err() != nil {
				ctx = context.Background()
			}
			updated, err := c.UpdateSession(ctx, st.Session.ID, api.UpdateSessionRequest{
				Config: api.SessionConfig{Model: modelID},
			})
			if err != nil {
				AppendError("Error", err.Error())
				return
			}
			SetSession(updated)
			showSessionDiagnostics(updated, false)
		}()
		return
	}

	if st.Config.Model != modelID && st.Config.Thinking != nil {
		AppendNotice("Thinking override cleared after changing the model.")
	}
	session.Set(func(s SessionState) SessionState {
		if s.Config.Model != modelID && s.Config.Thinking != nil {
			s.Config.Thinking = nil
		}
		s.Config.Model = modelID
		return s
	})
	if ctx == nil || ctx.Err() != nil {
		ctx = context.Background()
	}
	cfg := GetSession().Config
	updateDraftModel(ctx, c, cfg)
	AppendCommandOutput("/model "+modelID, "Model set to "+modelID)
}

// SetAgent updates the agent for the session, either via daemon API or in draft config.
func SetAgent(agentName string) {
	st := GetSession()
	sessMu.Lock()
	c := activeClient
	ctx := activeCtx
	sessMu.Unlock()

	if st.Session != nil && st.Session.ID != "" {
		if c == nil {
			return
		}
		go func() {
			if ctx == nil || ctx.Err() != nil {
				ctx = context.Background()
			}
			updated, err := c.UpdateSession(ctx, st.Session.ID, api.UpdateSessionRequest{
				Config: api.SessionConfig{Agent: agentName},
			})
			if err != nil {
				AppendError("Error", err.Error())
				return
			}
			SetSession(updated)
			showSessionDiagnostics(updated, false)
		}()
		return
	}

	session.Set(func(s SessionState) SessionState {
		s.Config.Agent = agentName
		return s
	})
	if ctx == nil || ctx.Err() != nil {
		ctx = context.Background()
	}
	cfg := GetSession().Config
	updateDraftModel(ctx, c, cfg)
	AppendCommandOutput("/agent "+agentName, "Agent set to "+agentName)
}

// ResetSession resets the session state to its initial default mode and cancels any active watchers.
func ResetSession() {
	sessMu.Lock()
	cancelModeDebounceLocked()
	if activeCancel != nil {
		activeCancel()
		activeCancel = nil
	}
	if streamCancel != nil {
		streamCancel()
		streamCancel = nil
	}
	activeClient = nil
	activeOpts = InitSessionOptions{}
	activeCtx = nil
	workspaceSandboxMode = ""
	diagnosticSession = ""
	diagnosticConfig = api.SessionConfig{}
	displayedDiagnostics = nil
	sessMu.Unlock()

	session.Set(func(SessionState) SessionState {
		return SessionState{Config: api.SessionConfig{Mode: api.ModeDefault}}
	})
}

func modelDisplay(model *api.Model, setting *api.ThinkingSetting) (string, string) {
	label, thinking := "default", ThinkingProviderDefault
	if model != nil {
		label = cmp.Or(strings.TrimSpace(model.Label), strings.TrimSpace(model.Name), strings.TrimSpace(model.ID), label)
		if resolved := ModelThinkingDefault(model); resolved != nil {
			thinking = thinkingDisplay(resolved)
		}
	}
	if setting != nil {
		thinking = thinkingDisplay(setting)
	}
	return label, thinking
}

// ModelThinkingDefault reports the thinking a model applies without any session
// or agent override. The daemon resolves this into ThinkingDefault; the scan of
// declared capabilities keeps the status line correct when it does not.
func ModelThinkingDefault(model *api.Model) *api.ThinkingSetting {
	if model.ThinkingDefault != nil {
		return model.ThinkingDefault
	}
	for _, option := range model.Thinking {
		if option.IsDefault {
			return thinkingCapabilityDefault(option)
		}
	}
	for _, option := range model.Thinking {
		if option.Type == "effort" && option.Default != "" {
			return &api.ThinkingSetting{Type: "effort", Effort: option.Default}
		}
	}
	return nil
}

func thinkingCapabilityDefault(option api.ThinkingCapability) *api.ThinkingSetting {
	switch option.Type {
	case "effort":
		if option.Default == "" {
			return nil
		}
		return &api.ThinkingSetting{Type: "effort", Effort: option.Default}
	case "budget", "budget_tokens":
		budget, err := strconv.Atoi(option.Default)
		if err != nil || budget <= 0 {
			budget = option.Min
		}
		if budget <= 0 {
			return nil
		}
		return &api.ThinkingSetting{Type: "budget", Budget: budget}
	case "toggle":
		if option.Default == "false" {
			return nil
		}
		return &api.ThinkingSetting{Type: "toggle", Enabled: true}
	case "adaptive":
		return &api.ThinkingSetting{Type: "adaptive"}
	}
	return nil
}

func updateDraftModel(ctx context.Context, c *client.Client, cfg api.SessionConfig) {
	var model *api.Model
	if c != nil {
		if preview, _ := c.PreviewSession(ctx, api.PreviewSessionRequest{Config: cfg}); preview != nil {
			model = preview.Model
		}
	}
	label, thinking := modelDisplay(model, cfg.Thinking)
	session.Set(func(s SessionState) SessionState {
		if s.Session == nil && s.Config.Agent == cfg.Agent && s.Config.Model == cfg.Model && reflect.DeepEqual(s.Config.Thinking, cfg.Thinking) {
			s.ModelID = modelIdentifier(model)
			s.ModelLabel, s.Thinking = label, thinking
			s.Context = api.ContextState{}
		}
		return s
	})
}

// ReloadWorkspace instructs the daemon to reload workspace resources from disk
// and refreshes AppState with updated skills and commands.
func ReloadWorkspace(ctx context.Context) error {
	c := Client()
	if err := c.ReloadWorkspace(ctx); err != nil {
		return err
	}
	if sks, err := c.ListSkills(ctx); err == nil {
		SetWorkspaceSkills(sks)
	}
	if rules, err := c.ListRules(ctx); err == nil {
		SetWorkspaceRules(rules)
	}
	if ags, err := c.ListAgents(ctx); err == nil {
		SetWorkspaceAgents(ags)
	}
	if cmds, err := c.ListCommands(ctx); err == nil {
		SetWorkspaceCommands(cmds)
	}
	if ws, err := c.GetWorkspace(ctx); err == nil {
		SetWorkspaceWarnings(ws.Warnings)
		sessMu.Lock()
		workspaceSandboxMode = api.SandboxMode(strings.ToLower(strings.TrimSpace(string(ws.SandboxMode))))
		sessMu.Unlock()
		SetDefaultAgent(strings.TrimSpace(ws.DefaultAgent))
	}
	// A reload re-syncs MCP connections, so health captured before it is stale.
	if servers, err := c.ListMCPs(ctx); err == nil {
		SetWorkspaceMCPServers(servers)
		watchMCPSettlement(ctx, c, servers)
	}
	state := GetSession()
	if state.Session == nil {
		updateDraftModel(ctx, c, state.Config)
	} else if updated, err := c.GetSession(ctx, state.Session.ID); err == nil && updated != nil {
		SetSession(updated)
	}
	_, _ = refreshWorkspaceToolsForce(ctx, c, effectiveToolAgent(), true)
	return nil
}
