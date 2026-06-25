package queries

import (
	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/kite/promise"
	"github.com/masterkeysrd/tasksmith/internal/api"
	tuiapi "github.com/masterkeysrd/tasksmith/internal/tui/api"
)

// UseGetMcpStatus retrieves a reactive status of all MCP servers.
func UseGetMcpStatus() wind.Result[*api.GetMcpStatusResponse] {
	client := tuiapi.UseClient()
	return wind.Use(api.GetMcpStatusRequest{}, promise.WrapWithProps(client.GetMcpStatus))
}
