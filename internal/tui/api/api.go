// Package api provides a client for interacting with the TaskSmith API. It defines the
// Client interface and a Provider component for dependency injection.
package api

import (
	"context"
	"iter"

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
	WatchSessionMessages(ctx context.Context, req api.GetSessionMessagesRequest) iter.Seq2[*api.GetSessionMessagesResponse, error]
	GetSessionState(ctx context.Context, req api.GetSessionStateRequest) (*api.GetSessionStateResponse, error)
	SubmitAuthorizationDecision(ctx context.Context, req api.SubmitAuthorizationDecisionRequest) (*api.SubmitAuthorizationDecisionResponse, error)
	ResolveMcpRequest(ctx context.Context, req api.ResolveMcpRequest) (*api.ResolveMcpResponse, error)
	GetTokenAnalytics(ctx context.Context, req api.GetTokenAnalyticsRequest) (*api.GetTokenAnalyticsResponse, error)
	ConfigureLsp(ctx context.Context, req api.ConfigureLspRequest) (*api.ConfigureLspResponse, error)
	DismissLspSuggestion(ctx context.Context, req api.DismissLspSuggestionRequest) (*api.DismissLspSuggestionResponse, error)
	GetLspStatus(ctx context.Context, req api.GetLspStatusRequest) (*api.GetLspStatusResponse, error)
	GetMcpStatus(ctx context.Context, req api.GetMcpStatusRequest) (*api.GetMcpStatusResponse, error)
	GetLspDiagnosticCounts(ctx context.Context, req api.GetLspDiagnosticCountsRequest) (*api.GetLspDiagnosticCountsResponse, error)
	RestartLsp(ctx context.Context, req api.RestartLspRequest) (*api.RestartLspResponse, error)
	RestartMcp(ctx context.Context, req api.RestartMcpRequest) (*api.RestartMcpResponse, error)
	GetLspDiagnostics(ctx context.Context, req api.GetLspDiagnosticsRequest) (*api.GetLspDiagnosticsResponse, error)
	LspSymbols(ctx context.Context, req api.LspSymbolsRequest) (*api.LspSymbolsResponse, error)
	GetFileChanges(ctx context.Context, req api.GetFileChangesRequest) (*api.GetFileChangesResponse, error)
	GetFileJournal(ctx context.Context, req api.GetFileJournalRequest) (*api.GetFileJournalResponse, error)
	RevertFile(ctx context.Context, req api.RevertFileRequest) (*api.RevertFileResponse, error)
	GetCachedFile(ctx context.Context, req api.GetCachedFileRequest) (*api.GetCachedFileResponse, error)
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
