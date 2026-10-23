package hooks

import (
	"cmp"
	"strings"

	"github.com/google/uuid"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/tasksmith/api"
	uiclient "github.com/masterkeysrd/tasksmith/internal/ui/client"
)

// Session is the session the UI is working with. A draft has a real,
// client-generated ID but does not exist on the daemon until its first message
// is sent, so it must not be queried, subscribed to or mutated before then.
type Session struct {
	api.Session
	Draft bool
}

// IsDraft reports whether the session has not been created on the daemon yet.
func (s Session) IsDraft() bool { return s.Draft }

// NewDraft returns a draft session with a fresh UUIDv7 ID.
func NewDraft(agent, model string) Session {
	return Session{
		Session: api.Session{ID: uuid.Must(uuid.NewV7()).String(), Agent: agent, Model: model},
		Draft:   true,
	}
}

type sessionState struct {
	Session   Session
	Epoch     int
	SendingID string
}

// sessionScope is the value shared by [SessionProvider]. It is a pointer so it
// satisfies the context's comparable constraint.
type sessionScope struct {
	state sessionState
	mode  api.PermissionMode

	defaults     api.Session // Agent and Model for new drafts
	initialKey   modelInfoKey
	initialInfo  ModelInfo
	selectFn     func(api.Session, api.PermissionMode)
	startDraftFn func(model string)
	persistedFn  func(api.Session)
	updateFn     func(api.Session)
	setModeFn    func(api.PermissionMode)
	setSendingFn func(sessionID string)
}

var sessionContext = kitex.CreateContext[*sessionScope](nil)

func useSessionScope() *sessionScope {
	scope := kitex.UseContext(sessionContext)
	if scope == nil {
		panic("ui/hooks: session hooks must be used within a SessionProvider")
	}
	return scope
}

// SessionProviderProps configures [SessionProvider].
type SessionProviderProps struct {
	// Initial is a persisted session to resume. Nil starts a draft.
	Initial *api.Session
	// Agent and Model are the defaults for drafts.
	Agent string
	Model string
	// Mode is the initial permission mode.
	Mode api.PermissionMode
	// InitialModel and InitialThinking are the already-resolved model label and
	// effort of the initial session. When set, the first model lookup is skipped.
	InitialModel    string
	InitialThinking string

	Children []kitex.Node // injected by kitex.FCC
}

// SessionProvider owns which session is active and the state tied to it: the
// permission mode and which session is currently sending. It requires
// client.Provider above it.
var SessionProvider = kitex.FCC("SessionProvider", func(props SessionProviderProps) kitex.Node {
	c := uiclient.UseClient()
	initial := kitex.UseMemo(func() Session {
		if props.Initial != nil {
			return Session{Session: *props.Initial}
		}
		return NewDraft(props.Agent, props.Model)
	}, []any{})
	state, setState := kitex.UseState(sessionState{Session: initial})
	mode, setMode := kitex.UseState(cmp.Or(props.Mode, api.ModeDefault))
	initialKey := kitex.UseMemo(func() modelInfoKey {
		return newModelInfoKey(c, initial.Session)
	}, []any{})

	scope := &sessionScope{
		state:       state(),
		mode:        mode(),
		defaults:    api.Session{Agent: props.Agent, Model: props.Model},
		initialKey:  initialKey,
		initialInfo: ModelInfo{Label: props.InitialModel, Thinking: props.InitialThinking, Ready: props.InitialModel != ""},
		selectFn: func(s api.Session, m api.PermissionMode) {
			setState(sessionState{Session: Session{Session: s}, Epoch: state().Epoch + 1})
			setMode(cmp.Or(m, api.ModeDefault))
		},
		startDraftFn: func(model string) {
			model = cmp.Or(strings.TrimSpace(model), props.Model)
			setState(sessionState{Session: NewDraft(props.Agent, model), Epoch: state().Epoch + 1})
			setMode(api.ModeDefault)
		},
		persistedFn: func(s api.Session) {
			current := state()
			if current.Session.ID != s.ID {
				return
			}
			current.Session = Session{Session: s}
			setState(current)
		},
		updateFn: func(s api.Session) {
			current := state()
			if current.Session.ID != s.ID || current.Session.Draft {
				return
			}
			current.Session = Session{Session: s}
			setState(current)
		},
		setModeFn: func(m api.PermissionMode) { setMode(m) },
		setSendingFn: func(sessionID string) {
			current := state()
			current.SendingID = sessionID
			setState(current)
		},
	}
	return sessionContext.Provider(scope, props.Children...)
})

// UseSession returns the active session: a persisted one, or a draft with a
// client-generated ID and the default agent and model.
func UseSession() Session {
	return useSessionScope().state.Session
}

// UseSessionEpoch returns a counter that changes whenever the UI switches to a
// different session or starts a new draft. It does not change when a draft is
// persisted by its first message.
func UseSessionEpoch() int {
	return useSessionScope().state.Epoch
}

// UseSessionSelection returns the transitions between sessions.
func UseSessionSelection() SessionSelection {
	scope := useSessionScope()
	return SessionSelection{
		Select:     scope.selectFn,
		StartDraft: scope.startDraftFn,
		Persisted:  scope.persistedFn,
		Update:     scope.updateFn,
	}
}

// SessionSelection changes the active session.
type SessionSelection struct {
	// Select switches to an existing session and its permission mode.
	Select func(session api.Session, mode api.PermissionMode)
	// StartDraft starts a new draft. An empty model keeps the default.
	StartDraft func(model string)
	// Persisted marks the active draft as created on the daemon.
	Persisted func(session api.Session)
	// Update replaces the data of the active persisted session, for example
	// after a rename.
	Update func(session api.Session)
}

// UseSending reports whether the active session has a message being sent.
func UseSending() bool {
	scope := useSessionScope()
	return scope.state.SendingID != "" && scope.state.SendingID == scope.state.Session.ID
}

// UseSetSending returns a function that records which session is sending a
// message. An empty ID clears it.
func UseSetSending() func(sessionID string) {
	return useSessionScope().setSendingFn
}
