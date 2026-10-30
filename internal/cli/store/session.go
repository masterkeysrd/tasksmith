package store

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"iter"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/masterkeysrd/kite/extras/kites"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
)

// SessionState holds the active session and its configuration, permission mode,
// display model label, background tasks, and context window state.
type SessionState struct {
	Session    *api.Session
	Config     api.SessionConfig
	Context    api.ContextState
	Usage      api.SessionUsage
	ModelLabel string
	Thinking   string
	Tasks      *[]api.Task
	Ready      bool
}

var session = kites.Create(SessionState{Config: api.SessionConfig{Mode: api.ModeDefault}})

var (
	sessMu               sync.Mutex
	activeClient         *client.Client
	activeOpts           InitSessionOptions
	activeCtx            context.Context
	activeCancel         context.CancelFunc
	streamCancel         context.CancelFunc
	modeDebounceTimer    *time.Timer
	modeDebounceDelay    = 500 * time.Millisecond
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
			SetSession(sess)
			globalConversation.reset()
			globalConversation.setSessionID(sid)
		}
	}

	var ws *api.Workspace
	if loaded, err := c.GetWorkspace(subCtx); err == nil {
		ws = loaded
		SetWorkspaceWarnings(ws.Warnings)
	}
	if cmds, err := c.ListCommands(subCtx); err == nil {
		SetWorkspaceCommands(cmds)
	}
	if skills, err := c.ListSkills(subCtx); err == nil {
		SetWorkspaceSkills(skills)
	}

	if sess == nil {
		cfg := GetSession().Config
		cfg.Agent, cfg.Model = opts.Agent, opts.Model
		session.Set(func(s SessionState) SessionState {
			s.Config = cfg
			return s
		})

		if ws != nil && strings.TrimSpace(cfg.Agent) == "" && strings.TrimSpace(cfg.Model) == "" {
			label, thinking := modelDisplay(ws.DefaultModel, cfg.Thinking)
			SetModelInfo(label, thinking)
		} else {
			updateDraftModel(subCtx, c, cfg)
		}
	}

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

	go func() {
		loadSessionUsage(watchCtx, c, sessionID)
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
					break
				}
				if ev.ID != "" {
					since = ev.ID
				}
				if p, ok := ev.Payload.(api.SessionUpdatedEvent); ok {
					sessMu.Lock()
					curr := GetSession().Session
					sessMu.Unlock()
					if curr != nil && curr.ID == p.Session.ID {
						sess := p.Session
						SetSession(&sess)
						showSessionDiagnostics(&sess, false)
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
func Send(text string) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return
	}
	FreezeTranscript()
	msgID := uuid.Must(uuid.NewV7()).String()
	globalConversation.addPendingUser(msgID, text)

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

		err = c.Send(ctx, sess.ID, api.SendRequest{
			ID:      msgID,
			Content: text,
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

	if sess == nil || sess.ID == "" {
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
			s.ModelLabel, s.Thinking = modelDisplay(sess.Model, sess.Config.Thinking)
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
		} else {
			s.Tasks = nil
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

// Client returns the currently active daemon client, if any.
func Client() *client.Client {
	sessMu.Lock()
	defer sessMu.Unlock()
	return activeClient
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
		s.Usage = api.SessionUsage{}
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
		for _, option := range model.Thinking {
			if option.Type == "effort" && option.Default != "" {
				thinking = option.Default
				break
			}
		}
		if model.ThinkingDefault != nil {
			thinking = thinkingDisplay(model.ThinkingDefault)
		}
	}
	if setting != nil {
		thinking = thinkingDisplay(setting)
	}
	return label, thinking
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
	if ags, err := c.ListAgents(ctx); err == nil {
		SetWorkspaceAgents(ags)
	}
	if cmds, err := c.ListCommands(ctx); err == nil {
		SetWorkspaceCommands(cmds)
	}
	if ws, err := c.GetWorkspace(ctx); err == nil {
		SetWorkspaceWarnings(ws.Warnings)
	}
	state := GetSession()
	if state.Session == nil {
		updateDraftModel(ctx, c, state.Config)
	} else if updated, err := c.GetSession(ctx, state.Session.ID); err == nil && updated != nil {
		SetSession(updated)
	}
	return nil
}
