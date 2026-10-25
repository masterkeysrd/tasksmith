package store

import (
	"cmp"
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/masterkeysrd/kite/extras/kites"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
)

// SessionState holds the active session and its configuration, permission mode,
// and display model label.
type SessionState struct {
	Session    *api.Session
	Config     api.SessionConfig
	ModelLabel string
	Thinking   string
	Ready      bool
}

var session = kites.Create(SessionState{Config: api.SessionConfig{Mode: api.ModeDefault}})

var (
	sessMu       sync.Mutex
	activeClient *client.Client
	activeOpts   InitSessionOptions
	activeCtx    context.Context
	activeCancel context.CancelFunc
	streamCancel context.CancelFunc
)

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
			globalConversation.setSessionID(sid)
		}
	}

	modelLabel, thinking := ResolveModelInfo(subCtx, c, opts.Agent, opts.Model, sess)
	SetModelInfo(modelLabel, thinking)

	if c != nil && sid != "" && sess != nil {
		FreezeTranscript()
		if tl, err := c.Timeline(subCtx, sid); err == nil {
			globalConversation.loadTimeline(tl)
		}
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
						if tl, tlErr := c.Timeline(watchCtx, sessionID); tlErr == nil {
							globalConversation.loadTimeline(tl)
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

func getOrCreateSession(ctx context.Context, initialTitle string) (*api.Session, error) {
	sessMu.Lock()
	sess := GetSession().Session
	if sess != nil && sess.ID != "" {
		sessMu.Unlock()
		return sess, nil
	}
	if activeClient == nil {
		sessMu.Unlock()
		return nil, errors.New("repl2: no active client to create session")
	}
	title := strings.TrimSpace(activeOpts.Title)
	if title == "" {
		title = strings.TrimSpace(initialTitle)
	}
	if title == "" {
		title = "New Session"
	}
	c := activeClient
	agent := activeOpts.Agent
	model := activeOpts.Model
	cfg := GetSession().Config
	mode := cfg.Mode
	sessMu.Unlock()

	created, err := c.CreateSession(ctx, api.CreateSessionRequest{
		Title: title,
		Config: api.SessionConfig{
			Agent:  cmp.Or(cfg.Agent, agent),
			Model:  cmp.Or(cfg.Model, model),
			Effort: cfg.Effort,
			Mode:   mode,
		},
	})
	if err != nil {
		return nil, err
	}
	SetSession(created)
	globalConversation.setSessionID(created.ID)
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
		globalConversation.cancelPendingUser(msgID, errors.New("repl2: no active client"))
		return
	}

	go func() {
		if ctx == nil || ctx.Err() != nil {
			ctx = context.Background()
		}

		sess, err := getOrCreateSession(ctx, trimmed)
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

// UseSession subscribes a component to a comparable slice of SessionState.
func UseSession[U comparable](selector func(SessionState) U) U {
	return kites.Use(session, selector)
}

// GetSession returns the current SessionState snapshot.
func GetSession() SessionState {
	return session.Get()
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

// SetSession updates the session and its configuration. If sess is non-nil,
// Mode defaults to api.ModeDefault if empty.
func SetSession(sess *api.Session) {
	session.Set(func(s SessionState) SessionState {
		s.Session = sess
		if sess != nil {
			s.Config = sess.Config
			if s.Config.Mode == "" {
				s.Config.Mode = api.ModeDefault
			}
		}
		return s
	})
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

// SetMode updates the active permission mode.
func SetMode(mode api.PermissionMode) {
	if mode == "" {
		mode = api.ModeDefault
	}
	var sessID string
	session.Set(func(s SessionState) SessionState {
		s.Config.Mode = mode
		if s.Session != nil {
			s.Session.Config.Mode = mode
			sessID = s.Session.ID
		}
		return s
	})
	if sessID != "" {
		sessMu.Lock()
		c := activeClient
		ctx := activeCtx
		sessMu.Unlock()
		if c != nil {
			go func() {
				if ctx == nil || ctx.Err() != nil {
					ctx = context.Background()
				}
				_, _ = c.UpdateSession(ctx, sessID, api.UpdateSessionRequest{
					Config: api.SessionConfig{Mode: mode},
				})
			}()
		}
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
	if streamCancel != nil {
		streamCancel()
		streamCancel = nil
	}
	c := activeClient
	ctx := activeCtx
	defAgent := activeOpts.Agent
	defModel := activeOpts.Model
	sessMu.Unlock()

	globalConversation.reset()

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
		globalConversation.setSessionID(loaded.ID)
		modelLabel, thinking := ResolveModelInfo(ctx, c, defAgent, defModel, loaded)
		SetModelInfo(modelLabel, thinking)
		ResetForSessionSwitch()

		if tl, err := c.Timeline(ctx, loaded.ID); err == nil {
			globalConversation.loadTimeline(tl)
		}
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
			modelLabel, thinking := ResolveModelInfo(ctx, c, defAgent, defModel, updated)
			SetModelInfo(modelLabel, thinking)
		}()
		return
	}

	session.Set(func(s SessionState) SessionState {
		s.Config.Model = modelID
		return s
	})
	if ctx == nil || ctx.Err() != nil {
		ctx = context.Background()
	}
	cfg := GetSession().Config
	modelLabel, thinking := ResolveModelInfo(ctx, c, cfg.Agent, cfg.Model, &api.Session{Config: cfg})
	if cfg.Effort != "" {
		thinking = cfg.Effort
	}
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
	if cfg.Effort != "" {
		thinking = cfg.Effort
	}
	SetModelInfo(modelLabel, thinking)
	AppendCommandOutput("/agent "+agentName, "Agent set to "+agentName)
}

// SetEffort updates the reasoning effort for the session, either via daemon API or in draft config.
func SetEffort(effort string) {
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
				Config: api.SessionConfig{Effort: effort},
			})
			if err != nil {
				AppendError("Error", err.Error())
				return
			}
			SetSession(updated)
			modelLabel, thinking := ResolveModelInfo(ctx, c, defAgent, defModel, updated)
			SetModelInfo(modelLabel, thinking)
		}()
		return
	}

	session.Set(func(s SessionState) SessionState {
		s.Config.Effort = effort
		return s
	})
	if ctx == nil || ctx.Err() != nil {
		ctx = context.Background()
	}
	cfg := GetSession().Config
	modelLabel, thinking := ResolveModelInfo(ctx, c, cfg.Agent, cfg.Model, &api.Session{Config: cfg})
	if cfg.Effort != "" {
		thinking = cfg.Effort
	}
	SetModelInfo(modelLabel, thinking)
	AppendCommandOutput("/effort "+effort, "Effort set to "+effort)
}

// ResetSession resets the session state to its initial default mode and cancels any active watchers.
func ResetSession() {
	sessMu.Lock()
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
	if c != nil && (agentName == "" || modelID == "") {
		ws, _ = c.GetWorkspace(ctx)
		if ws != nil && agentName == "" {
			agentName = strings.TrimSpace(ws.DefaultAgent)
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

	var agEffort, mdlLabel, mdlEffort, explicitEffort string
	if sess != nil && strings.TrimSpace(sess.Config.Effort) != "" {
		explicitEffort = strings.TrimSpace(sess.Config.Effort)
	}
	if ag != nil {
		agEffort = strings.TrimSpace(ag.Effort)
	}
	if mdl != nil {
		mdlLabel = cmp.Or(strings.TrimSpace(mdl.Label), strings.TrimSpace(mdl.Name), strings.TrimSpace(mdl.ID))
		mdlEffort = strings.TrimSpace(mdl.DefaultEffort)
	}

	modelLabel = cmp.Or(mdlLabel, modelID, "default")
	thinking = cmp.Or(explicitEffort, agEffort, mdlEffort, "auto")
	return modelLabel, thinking
}
