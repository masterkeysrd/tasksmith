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
	if c != nil && sid != "" {
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

	modelLabel, thinking := ResolveModelInfo(subCtx, c, opts.Agent, opts.Model, sess)
	SetModelInfo(modelLabel, thinking)

	if c != nil && sid != "" && sess != nil {
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

	if c != nil {
		startSessionsWatcher(subCtx)
	}
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
		var since string
		for {
			if watchCtx.Err() != nil {
				return
			}
			for ev, err := range c.Watch(watchCtx, since, api.SessionStreamTopic(sessionID)) {
				if err != nil {
					if client.IsNeedSync(err) {
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

	if c == nil || sess == nil || sess.ID == "" {
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
	if c == nil || sess == nil || sess.ID == "" || req.ID == "" {
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
		if s.Config.Mode == "" {
			return api.ModeDefault
		}
		return s.Config.Mode
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
		AppendNotice("Warning: " + notice)
	}
}

// SetSession updates the session and its configuration. If sess is non-nil,
// Mode defaults to api.ModeDefault if empty.
func SetSession(sess *api.Session) {
	sessMu.Lock()
	cancelModeDebounceLocked()
	sessMu.Unlock()

	session.Set(func(s SessionState) SessionState {
		s.Session = sess
		if sess != nil {
			if sess.Context.MaxTokens > 0 {
				s.Context = sess.Context
			}
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
	ptr := UseSession(func(s SessionState) *[]api.Task {
		return s.Tasks
	})
	if ptr == nil {
		return nil
	}
	return *ptr
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

	if c == nil || sess == nil || sess.ID == "" {
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

	if c == nil || sess == nil || sess.ID == "" {
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
		return s
	})

	if ctx == nil || ctx.Err() != nil {
		ctx = context.Background()
	}
	modelLabel, thinking := ResolveModelInfo(ctx, c, cfg.Agent, cfg.Model, nil)
	SetModelInfo(modelLabel, thinking)
	ResetToDraft()
}

// SwitchSession loads an existing session and switches to it asynchronously.
func SwitchSession(sessionID string) {
	sessMu.Lock()
	cancelModeDebounceLocked()
	c := activeClient
	ctx := activeCtx
	defAgent := activeOpts.Agent
	defModel := activeOpts.Model
	sessMu.Unlock()

	if c == nil || strings.TrimSpace(sessionID) == "" {
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
		modelLabel, thinking := ResolveModelInfo(ctx, c, defAgent, defModel, loaded)
		SetModelInfo(modelLabel, thinking)
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
	defAgent := activeOpts.Agent
	defModel := activeOpts.Model
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
			modelLabel, thinking := ResolveModelInfo(ctx, c, defAgent, defModel, updated)
			SetModelInfo(modelLabel, thinking)
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
	modelLabel, thinking := ResolveModelInfo(ctx, c, cfg.Agent, cfg.Model, &api.Session{Config: cfg})
	SetModelInfo(modelLabel, thinking)
	AppendCommandOutput("/model "+modelID, "Model set to "+modelID)
}

// SetAgent updates the agent for the session, either via daemon API or in draft config.
func SetAgent(agentName string) {
	st := GetSession()
	sessMu.Lock()
	c := activeClient
	ctx := activeCtx
	defAgent := activeOpts.Agent
	defModel := activeOpts.Model
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
			modelLabel, thinking := ResolveModelInfo(ctx, c, defAgent, defModel, updated)
			SetModelInfo(modelLabel, thinking)
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
	modelLabel, thinking := ResolveModelInfo(ctx, c, cfg.Agent, cfg.Model, &api.Session{Config: cfg})
	SetModelInfo(modelLabel, thinking)
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

// ResolveModelInfo returns the display label and reasoning effort for the
// given agent and model, falling back to the workspace and agent defaults for
// blank values. When sess is set its agent and model take precedence.
func ResolveModelInfo(ctx context.Context, c *client.Client, cfgAgent, cfgModel string, sess *api.Session) (modelLabel, thinking string) {
	agentName := strings.TrimSpace(cfgAgent)
	modelID := strings.TrimSpace(cfgModel)

	if sess != nil {
		agentName = strings.TrimSpace(sess.Config.Agent)
		modelID = strings.TrimSpace(sess.Config.Model)
	}

	var ws *api.Workspace
	if c != nil {
		ws, _ = c.GetWorkspace(ctx)
		if ws != nil {
			SetWorkspaceWarnings(ws.Warnings)
			if agentName == "" {
				agentName = strings.TrimSpace(ws.DefaultAgent)
			}
		}
		if cmds, err := c.ListCommands(ctx); err == nil {
			SetWorkspaceCommands(cmds)
		}
		if sks, err := c.ListSkills(ctx); err == nil {
			SetWorkspaceSkills(sks)
		}
	}

	var ag *api.Agent
	if c != nil && agentName != "" {
		ag, _ = c.GetAgent(ctx, agentName)
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
	if c != nil && modelID != "" {
		mdl, _ = c.GetModel(ctx, modelID)
	}

	var mdlLabel string
	thinking = ThinkingProviderDefault
	if mdl != nil {
		mdlLabel = cmp.Or(strings.TrimSpace(mdl.Label), strings.TrimSpace(mdl.Name), strings.TrimSpace(mdl.ID))
		for _, option := range mdl.Thinking {
			if option.Type == "effort" && option.Default != "" {
				thinking = option.Default
				break
			}
		}
	}
	if ag != nil && ag.Thinking != nil {
		thinking = thinkingDisplay(ag.Thinking)
	}

	modelLabel = cmp.Or(mdlLabel, modelID, "default")
	if sess != nil && sess.Config.Thinking != nil {
		thinking = thinkingDisplay(sess.Config.Thinking)
	}

	var initContext api.ContextState
	if sess != nil && sess.Context.MaxTokens > 0 {
		initContext = sess.Context
	} else if ag != nil && mdl != nil {
		ctxLimit := mdl.ContextLimit
		outLimit := mdl.OutputLimit
		if ctxLimit <= 0 {
			ctxLimit = 128000
		}
		if outLimit <= 0 {
			outLimit = 16384
		}
		reserve, trigger := computeCompactionThresholds(ctxLimit, outLimit)
		used := ag.Context.SystemPrompt + ag.Context.ContextFiles + ag.Context.SystemTools + ag.Context.CustomTools + ag.Context.MCPTools + ag.Context.Skills + ag.Context.Subagents
		free := max(0, trigger-used)
		initContext = api.ContextState{
			UsedTokens:    used,
			MaxTokens:     ctxLimit,
			OutputLimit:   outLimit,
			ReserveTokens: reserve,
			TriggerTokens: trigger,
			FreeTokens:    free,
			Breakdown:     ag.Context,
		}
	}
	if initContext.MaxTokens > 0 {
		session.Set(func(s SessionState) SessionState {
			s.Context = initContext
			return s
		})
	}

	return modelLabel, thinking
}

func computeCompactionThresholds(contextLimit, outputLimit int) (reserveTokens, triggerTokens int) {
	if contextLimit <= 0 {
		contextLimit = 128000
	}
	if outputLimit <= 0 {
		outputLimit = 16384
	}

	outputReserve := min(outputLimit, 32000)
	margin := int(float64(contextLimit) * 0.03)
	if margin < 8000 {
		margin = 8000
	} else if margin > 32000 {
		margin = 32000
	}

	reserveTokens = outputReserve + margin
	triggerTokens = contextLimit - reserveTokens
	return reserveTokens, triggerTokens
}

// ReloadWorkspace instructs the daemon to reload workspace resources from disk
// and refreshes AppState with updated skills and commands.
func ReloadWorkspace(ctx context.Context) error {
	c := Client()
	if c == nil {
		return errors.New("client unavailable")
	}
	if err := c.ReloadWorkspace(ctx); err != nil {
		return err
	}
	if sks, err := c.ListSkills(ctx); err == nil {
		SetWorkspaceSkills(sks)
	}
	if cmds, err := c.ListCommands(ctx); err == nil {
		SetWorkspaceCommands(cmds)
	}
	return nil
}
