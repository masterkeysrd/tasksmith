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
	InitializeWorkspace(ctx context.Context, req api.InitializeWorkspaceRequest) (*api.InitializeWorkspaceResponse, error)
	GetWorkspaceConfig(ctx context.Context, req api.GetWorkspaceConfigRequest) (*api.GetWorkspaceConfigResponse, error)

	ListSessions(ctx context.Context, req api.ListSessionsRequest) (*api.ListSessionsResponse, error)
	CreateSession(ctx context.Context, req api.CreateSessionRequest) (*api.CreateSessionResponse, error)
	RenameSession(ctx context.Context, req api.RenameSessionRequest) (*api.RenameSessionResponse, error)
	ArchiveSession(ctx context.Context, req api.ArchiveSessionRequest) (*api.ArchiveSessionResponse, error)
	DeleteSession(ctx context.Context, req api.DeleteSessionRequest) (*api.DeleteSessionResponse, error)
	SendMessage(ctx context.Context, req api.SendMessageRequest) (*api.SendMessageResponse, error)
	GetSessionMessages(ctx context.Context, req api.GetSessionMessagesRequest) (*api.GetSessionMessagesResponse, error)
	GetSessionState(ctx context.Context, req api.GetSessionStateRequest) (*api.GetSessionStateResponse, error)
	SubmitAuthorizationDecision(ctx context.Context, req api.SubmitAuthorizationDecisionRequest) (*api.SubmitAuthorizationDecisionResponse, error)
	GetTokenAnalytics(ctx context.Context, req api.GetTokenAnalyticsRequest) (*api.GetTokenAnalyticsResponse, error)
	ConfigureLsp(ctx context.Context, req api.ConfigureLspRequest) (*api.ConfigureLspResponse, error)
	DismissLspSuggestion(ctx context.Context, req api.DismissLspSuggestionRequest) (*api.DismissLspSuggestionResponse, error)
	GetLspStatus(ctx context.Context, req api.GetLspStatusRequest) (*api.GetLspStatusResponse, error)
	GetLspDiagnosticCounts(ctx context.Context, req api.GetLspDiagnosticCountsRequest) (*api.GetLspDiagnosticCountsResponse, error)
	RestartLsp(ctx context.Context, req api.RestartLspRequest) (*api.RestartLspResponse, error)
	GetLspDiagnostics(ctx context.Context, req api.GetLspDiagnosticsRequest) (*api.GetLspDiagnosticsResponse, error)
	LspSearch(ctx context.Context, req api.LspSearchRequest) (*api.LspSearchResponse, error)
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
