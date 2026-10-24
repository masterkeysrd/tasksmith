package hooks

import (
	"context"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/kite/promise"
	"github.com/masterkeysrd/tasksmith/api"
	uiclient "github.com/masterkeysrd/tasksmith/internal/ui/client"
)

// PermissionModeControl reads and changes the active session's permission mode.
type PermissionModeControl struct {
	Mode api.PermissionMode
	// Set changes the mode. For a draft the mode is only held locally; for a
	// persisted session it is also sent to the daemon.
	Set func(api.PermissionMode)
	// Cycle moves to the next mode.
	Cycle func()
}

type permissionModeInput struct {
	SessionID string
	Mode      api.PermissionMode
}

// NextPermissionMode returns the mode that follows current in the cycle.
func NextPermissionMode(current api.PermissionMode) api.PermissionMode {
	switch current {
	case api.ModeDefault:
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

// UsePermissionMode returns the permission mode of the active session and the
// means to change it.
func UsePermissionMode() PermissionModeControl {
	scope := useSessionScope()
	c := uiclient.UseClient()
	mutation := wind.UseMutation(promise.WrapWithProps(func(ctx context.Context, in permissionModeInput) (*api.Session, error) {
		return c.UpdateSession(ctx, in.SessionID, api.UpdateSessionRequest{
			Config: api.SessionConfig{Mode: in.Mode},
		})
	}))

	set := func(mode api.PermissionMode) {
		scope.setModeFn(mode)
		if session := scope.state.Session; !session.Draft {
			mutation.Mutate(permissionModeInput{SessionID: session.ID, Mode: mode})
		}
	}
	return PermissionModeControl{
		Mode:  scope.mode,
		Set:   set,
		Cycle: func() { set(NextPermissionMode(scope.mode)) },
	}
}

var conversationContext = kitex.CreateContext[*Conversation](nil)

// ConversationProviderProps configures [ConversationProvider].
type ConversationProviderProps struct {
	Children []kitex.Node // injected by kitex.FCC
}

// ConversationProvider loads the active session's conversation once and shares
// it with the components below. A draft has no conversation on the daemon, so
// nothing is requested until its first message creates the session. It
// requires [SessionProvider] above it.
var ConversationProvider = kitex.FCC("ConversationProvider", func(props ConversationProviderProps) kitex.Node {
	session := UseSession()
	id := session.ID
	if session.Draft {
		id = ""
	}
	conversation := UseConversation(id)
	return conversationContext.Provider(&conversation, props.Children...)
})

// UseCurrentConversation returns the conversation of the active session.
func UseCurrentConversation() Conversation {
	conversation := kitex.UseContext(conversationContext)
	if conversation == nil {
		panic("ui/hooks: UseCurrentConversation must be used within a ConversationProvider")
	}
	return *conversation
}

// UseIsStreaming reports whether the active session is producing a response:
// a message is being sent, or the conversation is running or waiting for a
// permission decision.
func UseIsStreaming() bool {
	conversation := UseCurrentConversation()
	return UseSending() ||
		conversation.Stream.Status == StreamRunning ||
		conversation.Stream.Status == StreamWaitingPermission
}
