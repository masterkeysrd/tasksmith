// Package api provides a client for interacting with the TaskSmith API. It defines the
// Client interface and a Provider component for dependency injection.
package api

import (
	"context"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/tasksmith/internal/api"
)

type Client interface {
	ListProjects(ctx context.Context, req api.ListProjectsRequest) (*api.ListProjectsResponse, error)
	ListAgents(ctx context.Context, req api.ListAgentsRequest) (*api.ListAgentsResponse, error)
	ListProviders(ctx context.Context, req api.ListProvidersRequest) (*api.ListProvidersResponse, error)
	ListProvidersPresets(ctx context.Context, req api.ListProvidersPresetsRequest) (*api.ListProvidersPresetsResponse, error)
	ListToolsPresets(ctx context.Context, req api.ListToolsPresetsRequest) (*api.ListToolsPresetsResponse, error)
}

var clientCtx = kitex.CreateContext[Client](nil)

type Props struct {
	Client Client
}

func Provider(props Props, children ...kitex.Node) kitex.Node {
	return clientCtx.Provider(props.Client, children...)
}

func UseClient() Client {
	client := kitex.UseContext(clientCtx)
	return client
}
