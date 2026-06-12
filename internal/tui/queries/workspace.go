package queries

import (
	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/kite/promise"
	"github.com/masterkeysrd/tasksmith/internal/api"
	tuiapi "github.com/masterkeysrd/tasksmith/internal/tui/api"
)

// UseListProjects returns a query result for the list of projects.
func UseListProjects() wind.Result[*api.ListProjectsResponse] {
	client := tuiapi.UseClient()
	return wind.Use(api.ListProjectsRequest{}, promise.WrapWithProps(client.ListProjects))
}

// UseListAgents returns a query result for the list of agents.
func UseListAgents() wind.Result[*api.ListAgentsResponse] {
	client := tuiapi.UseClient()
	return wind.Use(api.ListAgentsRequest{}, promise.WrapWithProps(client.ListAgents))
}

// UseListProviders returns a query result for the list of model providers.
func UseListProviders() wind.Result[*api.ListProvidersResponse] {
	client := tuiapi.UseClient()
	return wind.Use(api.ListProvidersRequest{}, promise.WrapWithProps(client.ListProviders))
}

// UseListProvidersPresets returns a query result for the list of model provider presets.
func UseListProvidersPresets() wind.Result[*api.ListProvidersPresetsResponse] {
	client := tuiapi.UseClient()
	return wind.Use(api.ListProvidersPresetsRequest{}, promise.WrapWithProps(client.ListProvidersPresets))
}

// UseListToolsPresets returns a query result for the list of tool presets.
func UseListToolsPresets() wind.Result[*api.ListToolsPresetsResponse] {
	client := tuiapi.UseClient()
	return wind.Use(api.ListToolsPresetsRequest{}, promise.WrapWithProps(client.ListToolsPresets))
}
