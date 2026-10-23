package repl

import (
	"context"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/kite/promise"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/repl/components"
	"github.com/masterkeysrd/tasksmith/internal/repl/ui"
	uiclient "github.com/masterkeysrd/tasksmith/internal/ui/client"
	uihooks "github.com/masterkeysrd/tasksmith/internal/ui/hooks"
)

// The providers in this file own mutations whose results must be handled even
// while the prompt or a modal is unmounted, because wind drops the callbacks of
// a mutation whose component has unmounted.

func appendError(transcript TranscriptControl, err error) {
	transcript.Append(components.ErrorEntry(ui.ErrorMessageProps{Text: err.Error()}))
}

// SendControl sends messages and cancels the running turn.
type SendControl struct {
	// Send creates the session first when it is still a draft, then sends.
	Send func(text string)
	// Cancel stops the running turn. It does nothing when nothing is running.
	Cancel func()
}

var sendContext = kitex.CreateContext[*SendControl](nil)

// UseSend returns the means to send a message and cancel the running turn.
func UseSend() SendControl {
	control := kitex.UseContext(sendContext)
	if control == nil {
		panic("repl: UseSend must be used within a SendProvider")
	}
	return *control
}

// SendProviderProps configures [SendProvider].
type SendProviderProps struct {
	Children []trail.Node // injected by trail.FCC
}

type createInput struct {
	Session api.Session
	Text    string
	Token   int64
}

type sendInput struct {
	SessionID string
	Text      string
	Token     int64
}

// SendProvider sends messages for the active session. The first message of a
// draft creates it under its client-generated ID, selects it, then sends.
var SendProvider = trail.FCC("SendProvider", func(props SendProviderProps) trail.Node {
	c := uiclient.UseClient()
	session := uihooks.UseSession()
	selection := uihooks.UseSessionSelection()
	setSending := uihooks.UseSetSending()
	streaming := uihooks.UseIsStreaming()
	transcript := UseTranscript()

	// token identifies the latest send; results of older ones are ignored, so
	// cancelling or switching away cannot revive a stale turn.
	token := kitex.UseRef(int64(0))

	send := wind.UseMutation(promise.WrapWithProps(func(ctx context.Context, in sendInput) (struct{}, error) {
		return struct{}{}, c.Send(ctx, in.SessionID, api.SendRequest{Content: in.Text})
	}), wind.MutationOptions[sendInput, struct{}]{
		OnSuccess: func(_ struct{}, in sendInput, _ wind.MutationContext) {
			if in.Token == token.Current {
				setSending("")
			}
		},
		OnError: func(err error, in sendInput, _ wind.MutationContext) {
			if in.Token == token.Current {
				setSending("")
				appendError(transcript, err)
			}
		},
	})

	create := wind.UseMutation(promise.WrapWithProps(func(ctx context.Context, in createInput) (*api.Session, error) {
		return c.CreateSession(ctx, api.CreateSessionRequest{
			ID:    in.Session.ID,
			Title: in.Text,
			Agent: in.Session.Agent,
			Model: in.Session.Model,
		})
	}), wind.MutationOptions[createInput, *api.Session]{
		OnSuccess: func(created *api.Session, in createInput, _ wind.MutationContext) {
			if in.Token != token.Current {
				return
			}
			selection.Persisted(*created)
			send.Mutate(sendInput{SessionID: created.ID, Text: in.Text, Token: in.Token})
		},
		OnError: func(err error, in createInput, _ wind.MutationContext) {
			if in.Token == token.Current {
				setSending("")
				appendError(transcript, err)
			}
		},
	})

	cancel := wind.UseMutation(promise.WrapWithProps(func(ctx context.Context, id string) (struct{}, error) {
		return struct{}{}, c.Cancel(ctx, id)
	}), wind.MutationOptions[string, struct{}]{
		OnError: func(err error, _ string, _ wind.MutationContext) {
			appendError(transcript, err)
		},
	})

	control := &SendControl{
		Send: func(text string) {
			token.Current++
			id := token.Current
			setSending(session.ID)
			if session.Draft {
				create.Mutate(createInput{Session: session.Session, Text: text, Token: id})
				return
			}
			send.Mutate(sendInput{SessionID: session.ID, Text: text, Token: id})
		},
		Cancel: func() {
			if !streaming {
				return
			}
			if !session.Draft {
				cancel.Mutate(session.ID)
			}
			token.Current++
			setSending("")
		},
	}
	return sendContext.Provider(control, props.Children...)
})

// SessionActions switches, renames and deletes sessions.
type SessionActions struct {
	Switch func(sessionID string)
	Rename func(sessionID, title string)
	Delete func(sessionID string)

	IsRenaming bool
	IsDeleting bool
}

var sessionActionsContext = kitex.CreateContext[*SessionActions](nil)

// UseSessionActions returns the actions that change the workspace's sessions.
func UseSessionActions() SessionActions {
	actions := kitex.UseContext(sessionActionsContext)
	if actions == nil {
		panic("repl: UseSessionActions must be used within a SessionActionsProvider")
	}
	return *actions
}

// SessionActionsProviderProps configures [SessionActionsProvider].
type SessionActionsProviderProps struct {
	Children []trail.Node // injected by trail.FCC
}

type switchedSession struct {
	Session api.Session
	Mode    api.PermissionMode
}

// SessionActionsProvider runs the session mutations. At most one rename or
// delete runs at a time; asking for another reports it in the status hint.
var SessionActionsProvider = trail.FCC("SessionActionsProvider", func(props SessionActionsProviderProps) trail.Node {
	c := uiclient.UseClient()
	session := uihooks.UseSession()
	selection := uihooks.UseSessionSelection()
	overlay := UseOverlay()
	transcript := UseTranscript()
	busy := kitex.UseRef(false)

	finish := func() {
		busy.Current = false
		overlay.SetHint("")
	}

	rename := uihooks.UseRenameSession(wind.MutationOptions[uihooks.RenameSessionInput, *api.Session]{
		OnSuccess: func(updated *api.Session, _ uihooks.RenameSessionInput, _ wind.MutationContext) {
			finish()
			selection.Update(*updated)
		},
		OnError: func(err error, _ uihooks.RenameSessionInput, _ wind.MutationContext) {
			finish()
			appendError(transcript, err)
		},
	})
	remove := uihooks.UseDeleteSession(wind.MutationOptions[string, struct{}]{
		OnSuccess: func(_ struct{}, deleted string, _ wind.MutationContext) {
			finish()
			if !session.Draft && session.ID == deleted {
				selection.StartDraft("")
			}
		},
		OnError: func(err error, _ string, _ wind.MutationContext) {
			finish()
			appendError(transcript, err)
		},
	})
	switchTo := wind.UseMutation(promise.WrapWithProps(func(ctx context.Context, id string) (switchedSession, error) {
		target, err := c.GetSession(ctx, id)
		if err != nil {
			return switchedSession{}, err
		}
		mode, _ := c.PermissionMode(ctx, target.ID)
		return switchedSession{Session: *target, Mode: mode}, nil
	}), wind.MutationOptions[string, switchedSession]{
		OnSuccess: func(target switchedSession, _ string, _ wind.MutationContext) {
			selection.Select(target.Session, target.Mode)
		},
		OnError: func(err error, _ string, _ wind.MutationContext) {
			appendError(transcript, err)
		},
	})

	begin := func() bool {
		if busy.Current {
			overlay.SetHint("A session update is already in progress.")
			return false
		}
		busy.Current = true
		overlay.SetHint("")
		return true
	}
	actions := &SessionActions{
		Switch: func(id string) { switchTo.Mutate(id) },
		Rename: func(id, title string) {
			if begin() {
				rename.Mutate(uihooks.RenameSessionInput{SessionID: id, Title: title})
			}
		},
		Delete: func(id string) {
			if begin() {
				remove.Mutate(id)
			}
		},
		IsRenaming: rename.IsPending,
		IsDeleting: remove.IsPending,
	}
	return sessionActionsContext.Provider(actions, props.Children...)
})
