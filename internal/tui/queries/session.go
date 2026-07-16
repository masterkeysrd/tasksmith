package queries

import (
	"context"
	"iter"

	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/kite/promise"
	"github.com/masterkeysrd/tasksmith/internal/api"
	tuiapi "github.com/masterkeysrd/tasksmith/internal/tui/api"
)

// UseListSessions retrieves a reactive list of user sessions.
func UseListSessions(req ...api.ListSessionsRequest) wind.Result[*api.ListSessionsResponse] {
	client := tuiapi.UseClient()
	r := api.ListSessionsRequest{}
	if len(req) > 0 {
		r = req[0]
	}
	return wind.Use(r, promise.WrapWithProps(client.ListSessions))
}

// UseGetSessionMessages retrieves a reactive message log for the given session.
func UseGetSessionMessages(sessionID string) wind.StreamResult[*api.GetSessionMessagesResponse] {
	client := tuiapi.UseClient()
	return wind.UseStream(api.GetSessionMessagesRequest{SessionID: sessionID}, func(ctx context.Context, req api.GetSessionMessagesRequest) iter.Seq2[*api.GetSessionMessagesResponse, error] {
		return client.WatchSessionMessages(ctx, req)
	})
}

// UseGetSessionState retrieves the active execution status of the session agent.
func UseGetSessionState(sessionID string) wind.Result[*api.GetSessionStateResponse] {
	client := tuiapi.UseClient()
	return wind.Use(api.GetSessionStateRequest{SessionID: sessionID}, promise.WrapWithProps(client.GetSessionState))
}

// UseGetInputHistory retrieves the user prompt history.
func UseGetInputHistory(req api.GetInputHistoryRequest) wind.Result[*api.GetInputHistoryResponse] {
	client := tuiapi.UseClient()
	return wind.Use(req, promise.WrapWithProps(client.GetInputHistory))
}

// UseGetFileChanges retrieves the list of file changes for the active session.
func UseGetFileChanges(sessionID string) wind.Result[*api.GetFileChangesResponse] {
	client := tuiapi.UseClient()
	return wind.Use(api.GetFileChangesRequest{SessionID: sessionID}, promise.WrapWithProps(client.GetFileChanges))
}
