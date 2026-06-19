package queries

import (
	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/kite/promise"
	"github.com/masterkeysrd/tasksmith/internal/api"
	tuiapi "github.com/masterkeysrd/tasksmith/internal/tui/api"
)

// UseListSessions retrieves a reactive list of all user sessions.
func UseListSessions() wind.Result[*api.ListSessionsResponse] {
	client := tuiapi.UseClient()
	return wind.Use(api.ListSessionsRequest{}, promise.WrapWithProps(client.ListSessions))
}

// UseGetSessionMessages retrieves a reactive message log for the given session.
func UseGetSessionMessages(sessionID string) wind.Result[*api.GetSessionMessagesResponse] {
	client := tuiapi.UseClient()
	return wind.Use(api.GetSessionMessagesRequest{SessionID: sessionID}, promise.WrapWithProps(client.GetSessionMessages))
}

// UseGetSessionState retrieves the active execution status of the session agent.
func UseGetSessionState(sessionID string) wind.Result[*api.GetSessionStateResponse] {
	client := tuiapi.UseClient()
	return wind.Use(api.GetSessionStateRequest{SessionID: sessionID}, promise.WrapWithProps(client.GetSessionState))
}
