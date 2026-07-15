package queries

import (
	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/kite/promise"
	"github.com/masterkeysrd/tasksmith/internal/api"
	tuiapi "github.com/masterkeysrd/tasksmith/internal/tui/api"
)

// UseGetPermissions retrieves all stored permissions for the given session.
func UseGetPermissions(sessionID string) wind.Result[*api.GetPermissionsResponse] {
	client := tuiapi.UseClient()
	return wind.Use(api.GetPermissionsRequest{SessionID: sessionID}, promise.WrapWithProps(client.GetPermissions))
}
