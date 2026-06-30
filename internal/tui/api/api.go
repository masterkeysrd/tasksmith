// Package api provides a client for interacting with the TaskSmith API. It defines the
// Client interface and a Provider component for dependency injection.
package api

import (
	"context"
	stdErrs "errors"
	"iter"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/tasksmith/internal/api"
	tserrors "github.com/masterkeysrd/tasksmith/internal/core/errors"
	"github.com/masterkeysrd/tasksmith/internal/tui/toast"
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
	ConfigureSession(ctx context.Context, req api.ConfigureSessionRequest) (*api.ConfigureSessionResponse, error)
	RenameSession(ctx context.Context, req api.RenameSessionRequest) (*api.RenameSessionResponse, error)
	ArchiveSession(ctx context.Context, req api.ArchiveSessionRequest) (*api.ArchiveSessionResponse, error)
	DeleteSession(ctx context.Context, req api.DeleteSessionRequest) (*api.DeleteSessionResponse, error)
	SendMessage(ctx context.Context, req api.SendMessageRequest) (*api.SendMessageResponse, error)
	GetSessionMessages(ctx context.Context, req api.GetSessionMessagesRequest) (*api.GetSessionMessagesResponse, error)
	WatchSessionMessages(ctx context.Context, req api.GetSessionMessagesRequest) iter.Seq2[*api.GetSessionMessagesResponse, error]
	GetSessionState(ctx context.Context, req api.GetSessionStateRequest) (*api.GetSessionStateResponse, error)
	SubmitAuthorizationDecision(ctx context.Context, req api.SubmitAuthorizationDecisionRequest) (*api.SubmitAuthorizationDecisionResponse, error)
	ResolveMcpRequest(ctx context.Context, req api.ResolveMcpRequest) (*api.ResolveMcpResponse, error)
	SetPermissionMode(ctx context.Context, req api.SetPermissionModeRequest) (*api.SetPermissionModeResponse, error)
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
	wrappedClient := &toastClient{delegate: props.Client}
	return clientCtx.Provider(wrappedClient, children...)
}

func UseClient() Client {
	client := kitex.UseContext(clientCtx)
	return client
}

// toastError displays a toast message if an error is non-nil. It parses custom tserrors
// to display user-friendly translated titles and descriptions.
func toastError(defaultTitle string, err error) {
	if err == nil {
		return
	}
	title := defaultTitle
	msg := err.Error()

	var appErr *tserrors.Error
	if stdErrs.As(err, &appErr) {
		title = appErr.GetTitle()
		msg = appErr.GetDescription()
	}

	toast.AddErrorMessage(title, msg)
}

// toastClient wraps a Client and triggers toast notifications on method errors.
type toastClient struct {
	delegate Client
}

func (w *toastClient) ListProjects(ctx context.Context, req api.ListProjectsRequest) (*api.ListProjectsResponse, error) {
	res, err := w.delegate.ListProjects(ctx, req)
	toastError("Failed to List Projects", err)
	return res, err
}

func (w *toastClient) ListAgents(ctx context.Context, req api.ListAgentsRequest) (*api.ListAgentsResponse, error) {
	res, err := w.delegate.ListAgents(ctx, req)
	toastError("Failed to List Agents", err)
	return res, err
}

func (w *toastClient) ListProviders(ctx context.Context, req api.ListProvidersRequest) (*api.ListProvidersResponse, error) {
	res, err := w.delegate.ListProviders(ctx, req)
	toastError("Failed to List Providers", err)
	return res, err
}

func (w *toastClient) ListProvidersPresets(ctx context.Context, req api.ListProvidersPresetsRequest) (*api.ListProvidersPresetsResponse, error) {
	res, err := w.delegate.ListProvidersPresets(ctx, req)
	toastError("Failed to Load Provider Presets", err)
	return res, err
}

func (w *toastClient) ListToolsPresets(ctx context.Context, req api.ListToolsPresetsRequest) (*api.ListToolsPresetsResponse, error) {
	res, err := w.delegate.ListToolsPresets(ctx, req)
	toastError("Failed to Load Tool Presets", err)
	return res, err
}

func (w *toastClient) InitializeWorkspace(ctx context.Context, req api.InitializeWorkspaceRequest) (*api.InitializeWorkspaceResponse, error) {
	res, err := w.delegate.InitializeWorkspace(ctx, req)
	toastError("Failed to Initialize Workspace", err)
	return res, err
}

func (w *toastClient) GetWorkspaceConfig(ctx context.Context, req api.GetWorkspaceConfigRequest) (*api.GetWorkspaceConfigResponse, error) {
	res, err := w.delegate.GetWorkspaceConfig(ctx, req)
	toastError("Failed to Get Workspace Config", err)
	return res, err
}

func (w *toastClient) ListSessions(ctx context.Context, req api.ListSessionsRequest) (*api.ListSessionsResponse, error) {
	res, err := w.delegate.ListSessions(ctx, req)
	toastError("Failed to List Sessions", err)
	return res, err
}

func (w *toastClient) CreateSession(ctx context.Context, req api.CreateSessionRequest) (*api.CreateSessionResponse, error) {
	res, err := w.delegate.CreateSession(ctx, req)
	toastError("Failed to Create Session", err)
	return res, err
}

func (w *toastClient) ConfigureSession(ctx context.Context, req api.ConfigureSessionRequest) (*api.ConfigureSessionResponse, error) {
	res, err := w.delegate.ConfigureSession(ctx, req)
	toastError("Failed to Configure Session", err)
	return res, err
}

func (w *toastClient) RenameSession(ctx context.Context, req api.RenameSessionRequest) (*api.RenameSessionResponse, error) {
	res, err := w.delegate.RenameSession(ctx, req)
	toastError("Failed to Rename Session", err)
	return res, err
}

func (w *toastClient) ArchiveSession(ctx context.Context, req api.ArchiveSessionRequest) (*api.ArchiveSessionResponse, error) {
	res, err := w.delegate.ArchiveSession(ctx, req)
	toastError("Failed to Archive Session", err)
	return res, err
}

func (w *toastClient) DeleteSession(ctx context.Context, req api.DeleteSessionRequest) (*api.DeleteSessionResponse, error) {
	res, err := w.delegate.DeleteSession(ctx, req)
	toastError("Failed to Delete Session", err)
	return res, err
}

func (w *toastClient) SendMessage(ctx context.Context, req api.SendMessageRequest) (*api.SendMessageResponse, error) {
	res, err := w.delegate.SendMessage(ctx, req)
	toastError("Failed to Send Message", err)
	return res, err
}

func (w *toastClient) GetSessionMessages(ctx context.Context, req api.GetSessionMessagesRequest) (*api.GetSessionMessagesResponse, error) {
	res, err := w.delegate.GetSessionMessages(ctx, req)
	toastError("Failed to Get Session Messages", err)
	return res, err
}

func (w *toastClient) WatchSessionMessages(ctx context.Context, req api.GetSessionMessagesRequest) iter.Seq2[*api.GetSessionMessagesResponse, error] {
	return func(yield func(*api.GetSessionMessagesResponse, error) bool) {
		for resp, err := range w.delegate.WatchSessionMessages(ctx, req) {
			toastError("Session Stream Disconnected", err)
			if !yield(resp, err) {
				return
			}
		}
	}
}

func (w *toastClient) GetSessionState(ctx context.Context, req api.GetSessionStateRequest) (*api.GetSessionStateResponse, error) {
	res, err := w.delegate.GetSessionState(ctx, req)
	toastError("Failed to Get Session State", err)
	return res, err
}

func (w *toastClient) SubmitAuthorizationDecision(ctx context.Context, req api.SubmitAuthorizationDecisionRequest) (*api.SubmitAuthorizationDecisionResponse, error) {
	res, err := w.delegate.SubmitAuthorizationDecision(ctx, req)
	toastError("Authorization Submission Failed", err)
	return res, err
}

func (w *toastClient) ResolveMcpRequest(ctx context.Context, req api.ResolveMcpRequest) (*api.ResolveMcpResponse, error) {
	res, err := w.delegate.ResolveMcpRequest(ctx, req)
	toastError("Failed to Resolve MCP Request", err)
	return res, err
}

func (w *toastClient) GetTokenAnalytics(ctx context.Context, req api.GetTokenAnalyticsRequest) (*api.GetTokenAnalyticsResponse, error) {
	res, err := w.delegate.GetTokenAnalytics(ctx, req)
	toastError("Failed to Load Token Analytics", err)
	return res, err
}

func (w *toastClient) ConfigureLsp(ctx context.Context, req api.ConfigureLspRequest) (*api.ConfigureLspResponse, error) {
	res, err := w.delegate.ConfigureLsp(ctx, req)
	toastError("Failed to Configure LSP", err)
	return res, err
}

func (w *toastClient) DismissLspSuggestion(ctx context.Context, req api.DismissLspSuggestionRequest) (*api.DismissLspSuggestionResponse, error) {
	res, err := w.delegate.DismissLspSuggestion(ctx, req)
	toastError("Failed to Dismiss LSP Suggestion", err)
	return res, err
}

func (w *toastClient) GetLspStatus(ctx context.Context, req api.GetLspStatusRequest) (*api.GetLspStatusResponse, error) {
	res, err := w.delegate.GetLspStatus(ctx, req)
	toastError("Failed to Get LSP Status", err)
	return res, err
}

func (w *toastClient) GetMcpStatus(ctx context.Context, req api.GetMcpStatusRequest) (*api.GetMcpStatusResponse, error) {
	res, err := w.delegate.GetMcpStatus(ctx, req)
	toastError("Failed to Get MCP Status", err)
	return res, err
}

func (w *toastClient) SetPermissionMode(ctx context.Context, req api.SetPermissionModeRequest) (*api.SetPermissionModeResponse, error) {
	res, err := w.delegate.SetPermissionMode(ctx, req)
	toastError("Failed to Set Permission Mode", err)
	return res, err
}

func (w *toastClient) GetLspDiagnosticCounts(ctx context.Context, req api.GetLspDiagnosticCountsRequest) (*api.GetLspDiagnosticCountsResponse, error) {
	res, err := w.delegate.GetLspDiagnosticCounts(ctx, req)
	toastError("Failed to Get LSP Diagnostics Count", err)
	return res, err
}

func (w *toastClient) RestartLsp(ctx context.Context, req api.RestartLspRequest) (*api.RestartLspResponse, error) {
	res, err := w.delegate.RestartLsp(ctx, req)
	toastError("Failed to Restart LSP", err)
	return res, err
}

func (w *toastClient) RestartMcp(ctx context.Context, req api.RestartMcpRequest) (*api.RestartMcpResponse, error) {
	res, err := w.delegate.RestartMcp(ctx, req)
	toastError("Failed to Restart MCP Server", err)
	return res, err
}

func (w *toastClient) GetLspDiagnostics(ctx context.Context, req api.GetLspDiagnosticsRequest) (*api.GetLspDiagnosticsResponse, error) {
	res, err := w.delegate.GetLspDiagnostics(ctx, req)
	toastError("Failed to Get LSP Diagnostics", err)
	return res, err
}

func (w *toastClient) LspSymbols(ctx context.Context, req api.LspSymbolsRequest) (*api.LspSymbolsResponse, error) {
	res, err := w.delegate.LspSymbols(ctx, req)
	toastError("Failed to Load LSP Symbols", err)
	return res, err
}

func (w *toastClient) GetFileChanges(ctx context.Context, req api.GetFileChangesRequest) (*api.GetFileChangesResponse, error) {
	res, err := w.delegate.GetFileChanges(ctx, req)
	toastError("Failed to Get File Changes", err)
	return res, err
}

func (w *toastClient) GetFileJournal(ctx context.Context, req api.GetFileJournalRequest) (*api.GetFileJournalResponse, error) {
	res, err := w.delegate.GetFileJournal(ctx, req)
	toastError("Failed to Get File Journal", err)
	return res, err
}

func (w *toastClient) RevertFile(ctx context.Context, req api.RevertFileRequest) (*api.RevertFileResponse, error) {
	res, err := w.delegate.RevertFile(ctx, req)
	toastError("Failed to Revert File", err)
	return res, err
}

func (w *toastClient) GetCachedFile(ctx context.Context, req api.GetCachedFileRequest) (*api.GetCachedFileResponse, error) {
	res, err := w.delegate.GetCachedFile(ctx, req)
	toastError("Failed to Get Cached File", err)
	return res, err
}
