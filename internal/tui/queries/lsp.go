package queries

import (
	"time"

	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/kite/promise"
	"github.com/masterkeysrd/tasksmith/internal/api"
	tuiapi "github.com/masterkeysrd/tasksmith/internal/tui/api"
)

// UseGetLspStatus retrieves a reactive status of all LSP servers.
func UseGetLspStatus() wind.Result[*api.GetLspStatusResponse] {
	client := tuiapi.UseClient()
	return wind.Use(api.GetLspStatusRequest{}, promise.WrapWithProps(client.GetLspStatus))
}

// UseGetLspDiagnosticCounts retrieves a reactive count of LSP diagnostics.
func UseGetLspDiagnosticCounts() wind.Result[*api.GetLspDiagnosticCountsResponse] {
	client := tuiapi.UseClient()
	return wind.Use(api.GetLspDiagnosticCountsRequest{}, promise.WrapWithProps(client.GetLspDiagnosticCounts), wind.Options{
		Enabled:   true,
		StaleTime: 5 * time.Second,
	})
}

// UseGetLspDiagnostics retrieves detailed LSP diagnostics.
func UseGetLspDiagnostics(req api.GetLspDiagnosticsRequest) wind.Result[*api.GetLspDiagnosticsResponse] {
	client := tuiapi.UseClient()
	return wind.Use(req, promise.WrapWithProps(client.GetLspDiagnostics))
}

// UseLspSearch performs an LSP symbol search.
func UseLspSearch(req api.LspSearchRequest) wind.Result[*api.LspSearchResponse] {
	client := tuiapi.UseClient()
	return wind.Use(req, promise.WrapWithProps(client.LspSearch), wind.Options{
		Enabled: req.Query != "",
	})
}
