package queries

import (
	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/kite/promise"
	"github.com/masterkeysrd/tasksmith/internal/api"
	tuiapi "github.com/masterkeysrd/tasksmith/internal/tui/api"
)

// UseGetTokenAnalytics returns a query result for token analytics.
func UseGetTokenAnalytics(req api.GetTokenAnalyticsRequest) wind.Result[*api.GetTokenAnalyticsResponse] {
	client := tuiapi.UseClient()
	return wind.Use(req, promise.WrapWithProps(client.GetTokenAnalytics))
}
