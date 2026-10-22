// Package hooks provides Wind queries and mutations for the daemon client.
// Hooks require both a UI client.Provider and a wind.Provider above the calling
// component.
package hooks

import (
	"context"

	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/kite/promise"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
	uiclient "github.com/masterkeysrd/tasksmith/internal/ui/client"
)

type sessionsKey struct {
	SocketPath string
	CWD        string
}

func sessionListKey(c *client.Client) sessionsKey {
	return sessionsKey{SocketPath: c.SocketPath(), CWD: c.CWD()}
}

// UseSessions queries the sessions in the provided client's workspace.
func UseSessions(opts ...wind.Options) wind.Result[[]api.Session] {
	c := uiclient.UseClient()
	fetch := promise.Wrap(c.ListSessions)
	return wind.Use(sessionListKey(c), func(ctx context.Context, _ sessionsKey) *promise.Promise[[]api.Session] {
		return fetch(ctx)
	}, opts...)
}

// RenameSessionInput identifies the session to rename independently of the
// session currently selected by the UI.
type RenameSessionInput struct {
	SessionID string
	Title     string
}

// UseRenameSession renames a session and invalidates its workspace's session list.
func UseRenameSession(opts ...wind.MutationOptions[RenameSessionInput, *api.Session]) wind.MutationResult[RenameSessionInput] {
	c := uiclient.UseClient()
	mutate := promise.WrapWithProps(func(ctx context.Context, input RenameSessionInput) (*api.Session, error) {
		return c.UpdateSession(ctx, input.SessionID, api.UpdateSessionRequest{Title: input.Title})
	})
	options := append([]wind.MutationOptions[RenameSessionInput, *api.Session]{{
		OnSuccess: func(_ *api.Session, _ RenameSessionInput, ctx wind.MutationContext) {
			ctx.Client.InvalidateQueries(sessionListKey(c))
		},
	}}, opts...)
	return wind.UseMutation(mutate, options...)
}

// UseDeleteSession deletes the supplied session ID and invalidates its
// workspace's session list.
func UseDeleteSession(opts ...wind.MutationOptions[string, struct{}]) wind.MutationResult[string] {
	c := uiclient.UseClient()
	mutate := promise.WrapWithProps(func(ctx context.Context, sessionID string) (struct{}, error) {
		return struct{}{}, c.DeleteSession(ctx, sessionID)
	})
	options := append([]wind.MutationOptions[string, struct{}]{{
		OnSuccess: func(_ struct{}, _ string, ctx wind.MutationContext) {
			ctx.Client.InvalidateQueries(sessionListKey(c))
		},
	}}, opts...)
	return wind.UseMutation(mutate, options...)
}
