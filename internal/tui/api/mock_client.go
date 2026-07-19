package api

import (
	"context"
	"iter"

	"github.com/masterkeysrd/tasksmith/internal/api"
)

// MockClient is a centralized mock implementation of the Client interface.
// Individual tests can instantiate it and set only the function pointers they need.
type MockClient struct {
	ListProjectsFunc                func(ctx context.Context, req api.ListProjectsRequest) (*api.ListProjectsResponse, error)
	ListAgentsFunc                  func(ctx context.Context, req api.ListAgentsRequest) (*api.ListAgentsResponse, error)
	ListSkillsFunc                  func(ctx context.Context, req api.ListSkillsRequest) (*api.ListSkillsResponse, error)
	ListProvidersFunc               func(ctx context.Context, req api.ListProvidersRequest) (*api.ListProvidersResponse, error)
	ListProvidersPresetsFunc        func(ctx context.Context, req api.ListProvidersPresetsRequest) (*api.ListProvidersPresetsResponse, error)
	ListToolsPresetsFunc            func(ctx context.Context, req api.ListToolsPresetsRequest) (*api.ListToolsPresetsResponse, error)
	InitializeWorkspaceFunc         func(ctx context.Context, req api.InitializeWorkspaceRequest) (*api.InitializeWorkspaceResponse, error)
	GetWorkspaceConfigFunc          func(ctx context.Context, req api.GetWorkspaceConfigRequest) (*api.GetWorkspaceConfigResponse, error)
	AuthorizeWorkspaceToolsFunc     func(ctx context.Context, req api.AuthorizeWorkspaceToolsRequest) (*api.AuthorizeWorkspaceToolsResponse, error)
	ListSessionsFunc                func(ctx context.Context, req api.ListSessionsRequest) (*api.ListSessionsResponse, error)
	CreateSessionFunc               func(ctx context.Context, req api.CreateSessionRequest) (*api.CreateSessionResponse, error)
	ConfigureSessionFunc            func(ctx context.Context, req api.ConfigureSessionRequest) (*api.ConfigureSessionResponse, error)
	RenameSessionFunc               func(ctx context.Context, req api.RenameSessionRequest) (*api.RenameSessionResponse, error)
	ArchiveSessionFunc              func(ctx context.Context, req api.ArchiveSessionRequest) (*api.ArchiveSessionResponse, error)
	DeleteSessionFunc               func(ctx context.Context, req api.DeleteSessionRequest) (*api.DeleteSessionResponse, error)
	SendMessageFunc                 func(ctx context.Context, req api.SendMessageRequest) (*api.SendMessageResponse, error)
	CancelTurnFunc                  func(ctx context.Context, req api.CancelTurnRequest) (*api.CancelTurnResponse, error)
	RetryTurnFunc                   func(ctx context.Context, req api.RetryTurnRequest) (*api.RetryTurnResponse, error)
	ForceCompactionFunc             func(ctx context.Context, req api.ForceCompactionRequest) (*api.ForceCompactionResponse, error)
	GetSessionMessagesFunc          func(ctx context.Context, req api.GetSessionMessagesRequest) (*api.GetSessionMessagesResponse, error)
	WatchSessionMessagesFunc        func(ctx context.Context, req api.GetSessionMessagesRequest) iter.Seq2[*api.GetSessionMessagesResponse, error]
	GetInputHistoryFunc             func(ctx context.Context, req api.GetInputHistoryRequest) (*api.GetInputHistoryResponse, error)
	GetSessionStateFunc             func(ctx context.Context, req api.GetSessionStateRequest) (*api.GetSessionStateResponse, error)
	SubmitAuthorizationDecisionFunc func(ctx context.Context, req api.SubmitAuthorizationDecisionRequest) (*api.SubmitAuthorizationDecisionResponse, error)
	SubmitQuestionAnswersFunc       func(ctx context.Context, req api.SubmitQuestionAnswersRequest) (*api.SubmitQuestionAnswersResponse, error)
	ResolveMcpRequestFunc           func(ctx context.Context, req api.ResolveMcpRequest) (*api.ResolveMcpResponse, error)
	SetPermissionModeFunc           func(ctx context.Context, req api.SetPermissionModeRequest) (*api.SetPermissionModeResponse, error)
	GetPermissionsFunc              func(ctx context.Context, req api.GetPermissionsRequest) (*api.GetPermissionsResponse, error)
	DeletePermissionFunc            func(ctx context.Context, req api.DeletePermissionRequest) (*api.DeletePermissionResponse, error)
	GetTokenAnalyticsFunc           func(ctx context.Context, req api.GetTokenAnalyticsRequest) (*api.GetTokenAnalyticsResponse, error)
	ConfigureLspFunc                func(ctx context.Context, req api.ConfigureLspRequest) (*api.ConfigureLspResponse, error)
	DismissLspSuggestionFunc        func(ctx context.Context, req api.DismissLspSuggestionRequest) (*api.DismissLspSuggestionResponse, error)
	GetLspStatusFunc                func(ctx context.Context, req api.GetLspStatusRequest) (*api.GetLspStatusResponse, error)
	GetMcpStatusFunc                func(ctx context.Context, req api.GetMcpStatusRequest) (*api.GetMcpStatusResponse, error)
	GetLspDiagnosticCountsFunc      func(ctx context.Context, req api.GetLspDiagnosticCountsRequest) (*api.GetLspDiagnosticCountsResponse, error)
	RestartLspFunc                  func(ctx context.Context, req api.RestartLspRequest) (*api.RestartLspResponse, error)
	RestartMcpFunc                  func(ctx context.Context, req api.RestartMcpRequest) (*api.RestartMcpResponse, error)
	GetLspDiagnosticsFunc           func(ctx context.Context, req api.GetLspDiagnosticsRequest) (*api.GetLspDiagnosticsResponse, error)
	LspSymbolsFunc                  func(ctx context.Context, req api.LspSymbolsRequest) (*api.LspSymbolsResponse, error)
	GetFileChangesFunc              func(ctx context.Context, req api.GetFileChangesRequest) (*api.GetFileChangesResponse, error)
	GetFileJournalFunc              func(ctx context.Context, req api.GetFileJournalRequest) (*api.GetFileJournalResponse, error)
	RevertFileFunc                  func(ctx context.Context, req api.RevertFileRequest) (*api.RevertFileResponse, error)
	GetCachedFileFunc               func(ctx context.Context, req api.GetCachedFileRequest) (*api.GetCachedFileResponse, error)
	DequeueFromFunc                 func(ctx context.Context, req api.DequeueFromRequest) (*api.DequeueFromResponse, error)
	EnqueueMessagesFunc             func(ctx context.Context, req api.EnqueueMessagesRequest) (*api.EnqueueMessagesResponse, error)
	ClearQueueFunc                  func(ctx context.Context, req api.ClearQueueRequest) (*api.ClearQueueResponse, error)
	RemoveQueuedMessageFunc         func(ctx context.Context, req api.RemoveQueuedMessageRequest) (*api.RemoveQueuedMessageResponse, error)
	SendQueuedFunc                  func(ctx context.Context, req api.SendQueuedRequest) (*api.SendQueuedResponse, error)
}

// Verify that MockClient implements Client
var _ Client = (*MockClient)(nil)

func (m *MockClient) ListProjects(ctx context.Context, req api.ListProjectsRequest) (*api.ListProjectsResponse, error) {
	if m.ListProjectsFunc != nil {
		return m.ListProjectsFunc(ctx, req)
	}
	return &api.ListProjectsResponse{}, nil
}

func (m *MockClient) ListAgents(ctx context.Context, req api.ListAgentsRequest) (*api.ListAgentsResponse, error) {
	if m.ListAgentsFunc != nil {
		return m.ListAgentsFunc(ctx, req)
	}
	return &api.ListAgentsResponse{}, nil
}

func (m *MockClient) ListSkills(ctx context.Context, req api.ListSkillsRequest) (*api.ListSkillsResponse, error) {
	if m.ListSkillsFunc != nil {
		return m.ListSkillsFunc(ctx, req)
	}
	return &api.ListSkillsResponse{}, nil
}

func (m *MockClient) ListProviders(ctx context.Context, req api.ListProvidersRequest) (*api.ListProvidersResponse, error) {
	if m.ListProvidersFunc != nil {
		return m.ListProvidersFunc(ctx, req)
	}
	return &api.ListProvidersResponse{}, nil
}

func (m *MockClient) ListProvidersPresets(ctx context.Context, req api.ListProvidersPresetsRequest) (*api.ListProvidersPresetsResponse, error) {
	if m.ListProvidersPresetsFunc != nil {
		return m.ListProvidersPresetsFunc(ctx, req)
	}
	return &api.ListProvidersPresetsResponse{}, nil
}

func (m *MockClient) ListToolsPresets(ctx context.Context, req api.ListToolsPresetsRequest) (*api.ListToolsPresetsResponse, error) {
	if m.ListToolsPresetsFunc != nil {
		return m.ListToolsPresetsFunc(ctx, req)
	}
	return &api.ListToolsPresetsResponse{}, nil
}

func (m *MockClient) InitializeWorkspace(ctx context.Context, req api.InitializeWorkspaceRequest) (*api.InitializeWorkspaceResponse, error) {
	if m.InitializeWorkspaceFunc != nil {
		return m.InitializeWorkspaceFunc(ctx, req)
	}
	return &api.InitializeWorkspaceResponse{}, nil
}

func (m *MockClient) GetWorkspaceConfig(ctx context.Context, req api.GetWorkspaceConfigRequest) (*api.GetWorkspaceConfigResponse, error) {
	if m.GetWorkspaceConfigFunc != nil {
		return m.GetWorkspaceConfigFunc(ctx, req)
	}
	return &api.GetWorkspaceConfigResponse{}, nil
}

func (m *MockClient) AuthorizeWorkspaceTools(ctx context.Context, req api.AuthorizeWorkspaceToolsRequest) (*api.AuthorizeWorkspaceToolsResponse, error) {
	if m.AuthorizeWorkspaceToolsFunc != nil {
		return m.AuthorizeWorkspaceToolsFunc(ctx, req)
	}
	return &api.AuthorizeWorkspaceToolsResponse{}, nil
}

func (m *MockClient) ListSessions(ctx context.Context, req api.ListSessionsRequest) (*api.ListSessionsResponse, error) {
	if m.ListSessionsFunc != nil {
		return m.ListSessionsFunc(ctx, req)
	}
	return &api.ListSessionsResponse{}, nil
}

func (m *MockClient) CreateSession(ctx context.Context, req api.CreateSessionRequest) (*api.CreateSessionResponse, error) {
	if m.CreateSessionFunc != nil {
		return m.CreateSessionFunc(ctx, req)
	}
	return &api.CreateSessionResponse{}, nil
}

func (m *MockClient) ConfigureSession(ctx context.Context, req api.ConfigureSessionRequest) (*api.ConfigureSessionResponse, error) {
	if m.ConfigureSessionFunc != nil {
		return m.ConfigureSessionFunc(ctx, req)
	}
	return &api.ConfigureSessionResponse{}, nil
}

func (m *MockClient) RenameSession(ctx context.Context, req api.RenameSessionRequest) (*api.RenameSessionResponse, error) {
	if m.RenameSessionFunc != nil {
		return m.RenameSessionFunc(ctx, req)
	}
	return &api.RenameSessionResponse{}, nil
}

func (m *MockClient) ArchiveSession(ctx context.Context, req api.ArchiveSessionRequest) (*api.ArchiveSessionResponse, error) {
	if m.ArchiveSessionFunc != nil {
		return m.ArchiveSessionFunc(ctx, req)
	}
	return &api.ArchiveSessionResponse{}, nil
}

func (m *MockClient) DeleteSession(ctx context.Context, req api.DeleteSessionRequest) (*api.DeleteSessionResponse, error) {
	if m.DeleteSessionFunc != nil {
		return m.DeleteSessionFunc(ctx, req)
	}
	return &api.DeleteSessionResponse{}, nil
}

func (m *MockClient) SendMessage(ctx context.Context, req api.SendMessageRequest) (*api.SendMessageResponse, error) {
	if m.SendMessageFunc != nil {
		return m.SendMessageFunc(ctx, req)
	}
	return &api.SendMessageResponse{}, nil
}

func (m *MockClient) CancelTurn(ctx context.Context, req api.CancelTurnRequest) (*api.CancelTurnResponse, error) {
	if m.CancelTurnFunc != nil {
		return m.CancelTurnFunc(ctx, req)
	}
	return &api.CancelTurnResponse{}, nil
}

func (m *MockClient) RetryTurn(ctx context.Context, req api.RetryTurnRequest) (*api.RetryTurnResponse, error) {
	if m.RetryTurnFunc != nil {
		return m.RetryTurnFunc(ctx, req)
	}
	return &api.RetryTurnResponse{}, nil
}

func (m *MockClient) ForceCompaction(ctx context.Context, req api.ForceCompactionRequest) (*api.ForceCompactionResponse, error) {
	if m.ForceCompactionFunc != nil {
		return m.ForceCompactionFunc(ctx, req)
	}
	return &api.ForceCompactionResponse{}, nil
}

func (m *MockClient) GetSessionMessages(ctx context.Context, req api.GetSessionMessagesRequest) (*api.GetSessionMessagesResponse, error) {
	if m.GetSessionMessagesFunc != nil {
		return m.GetSessionMessagesFunc(ctx, req)
	}
	return &api.GetSessionMessagesResponse{}, nil
}

func (m *MockClient) GetInputHistory(ctx context.Context, req api.GetInputHistoryRequest) (*api.GetInputHistoryResponse, error) {
	if m.GetInputHistoryFunc != nil {
		return m.GetInputHistoryFunc(ctx, req)
	}
	return &api.GetInputHistoryResponse{}, nil
}

func (m *MockClient) WatchSessionMessages(ctx context.Context, req api.GetSessionMessagesRequest) iter.Seq2[*api.GetSessionMessagesResponse, error] {
	if m.WatchSessionMessagesFunc != nil {
		return m.WatchSessionMessagesFunc(ctx, req)
	}
	return func(yield func(*api.GetSessionMessagesResponse, error) bool) {}
}

func (m *MockClient) GetSessionState(ctx context.Context, req api.GetSessionStateRequest) (*api.GetSessionStateResponse, error) {
	if m.GetSessionStateFunc != nil {
		return m.GetSessionStateFunc(ctx, req)
	}
	return &api.GetSessionStateResponse{}, nil
}

func (m *MockClient) SubmitAuthorizationDecision(ctx context.Context, req api.SubmitAuthorizationDecisionRequest) (*api.SubmitAuthorizationDecisionResponse, error) {
	if m.SubmitAuthorizationDecisionFunc != nil {
		return m.SubmitAuthorizationDecisionFunc(ctx, req)
	}
	return &api.SubmitAuthorizationDecisionResponse{}, nil
}

func (m *MockClient) SubmitQuestionAnswers(ctx context.Context, req api.SubmitQuestionAnswersRequest) (*api.SubmitQuestionAnswersResponse, error) {
	if m.SubmitQuestionAnswersFunc != nil {
		return m.SubmitQuestionAnswersFunc(ctx, req)
	}
	return &api.SubmitQuestionAnswersResponse{Success: true}, nil
}

func (m *MockClient) ResolveMcpRequest(ctx context.Context, req api.ResolveMcpRequest) (*api.ResolveMcpResponse, error) {
	if m.ResolveMcpRequestFunc != nil {
		return m.ResolveMcpRequestFunc(ctx, req)
	}
	return &api.ResolveMcpResponse{}, nil
}

func (m *MockClient) SetPermissionMode(ctx context.Context, req api.SetPermissionModeRequest) (*api.SetPermissionModeResponse, error) {
	if m.SetPermissionModeFunc != nil {
		return m.SetPermissionModeFunc(ctx, req)
	}
	return &api.SetPermissionModeResponse{}, nil
}

func (m *MockClient) GetPermissions(ctx context.Context, req api.GetPermissionsRequest) (*api.GetPermissionsResponse, error) {
	if m.GetPermissionsFunc != nil {
		return m.GetPermissionsFunc(ctx, req)
	}
	return &api.GetPermissionsResponse{}, nil
}

func (m *MockClient) DeletePermission(ctx context.Context, req api.DeletePermissionRequest) (*api.DeletePermissionResponse, error) {
	if m.DeletePermissionFunc != nil {
		return m.DeletePermissionFunc(ctx, req)
	}
	return &api.DeletePermissionResponse{}, nil
}

func (m *MockClient) GetTokenAnalytics(ctx context.Context, req api.GetTokenAnalyticsRequest) (*api.GetTokenAnalyticsResponse, error) {
	if m.GetTokenAnalyticsFunc != nil {
		return m.GetTokenAnalyticsFunc(ctx, req)
	}
	return &api.GetTokenAnalyticsResponse{}, nil
}

func (m *MockClient) ConfigureLsp(ctx context.Context, req api.ConfigureLspRequest) (*api.ConfigureLspResponse, error) {
	if m.ConfigureLspFunc != nil {
		return m.ConfigureLspFunc(ctx, req)
	}
	return &api.ConfigureLspResponse{}, nil
}

func (m *MockClient) DismissLspSuggestion(ctx context.Context, req api.DismissLspSuggestionRequest) (*api.DismissLspSuggestionResponse, error) {
	if m.DismissLspSuggestionFunc != nil {
		return m.DismissLspSuggestionFunc(ctx, req)
	}
	return &api.DismissLspSuggestionResponse{}, nil
}

func (m *MockClient) GetLspStatus(ctx context.Context, req api.GetLspStatusRequest) (*api.GetLspStatusResponse, error) {
	if m.GetLspStatusFunc != nil {
		return m.GetLspStatusFunc(ctx, req)
	}
	return &api.GetLspStatusResponse{}, nil
}

func (m *MockClient) GetMcpStatus(ctx context.Context, req api.GetMcpStatusRequest) (*api.GetMcpStatusResponse, error) {
	if m.GetMcpStatusFunc != nil {
		return m.GetMcpStatusFunc(ctx, req)
	}
	return &api.GetMcpStatusResponse{}, nil
}

func (m *MockClient) GetLspDiagnosticCounts(ctx context.Context, req api.GetLspDiagnosticCountsRequest) (*api.GetLspDiagnosticCountsResponse, error) {
	if m.GetLspDiagnosticCountsFunc != nil {
		return m.GetLspDiagnosticCountsFunc(ctx, req)
	}
	return &api.GetLspDiagnosticCountsResponse{}, nil
}

func (m *MockClient) RestartLsp(ctx context.Context, req api.RestartLspRequest) (*api.RestartLspResponse, error) {
	if m.RestartLspFunc != nil {
		return m.RestartLspFunc(ctx, req)
	}
	return &api.RestartLspResponse{}, nil
}

func (m *MockClient) RestartMcp(ctx context.Context, req api.RestartMcpRequest) (*api.RestartMcpResponse, error) {
	if m.RestartMcpFunc != nil {
		return m.RestartMcpFunc(ctx, req)
	}
	return &api.RestartMcpResponse{}, nil
}

func (m *MockClient) GetLspDiagnostics(ctx context.Context, req api.GetLspDiagnosticsRequest) (*api.GetLspDiagnosticsResponse, error) {
	if m.GetLspDiagnosticsFunc != nil {
		return m.GetLspDiagnosticsFunc(ctx, req)
	}
	return &api.GetLspDiagnosticsResponse{}, nil
}

func (m *MockClient) LspSymbols(ctx context.Context, req api.LspSymbolsRequest) (*api.LspSymbolsResponse, error) {
	if m.LspSymbolsFunc != nil {
		return m.LspSymbolsFunc(ctx, req)
	}
	return &api.LspSymbolsResponse{}, nil
}

func (m *MockClient) GetFileChanges(ctx context.Context, req api.GetFileChangesRequest) (*api.GetFileChangesResponse, error) {
	if m.GetFileChangesFunc != nil {
		return m.GetFileChangesFunc(ctx, req)
	}
	return &api.GetFileChangesResponse{}, nil
}

func (m *MockClient) GetFileJournal(ctx context.Context, req api.GetFileJournalRequest) (*api.GetFileJournalResponse, error) {
	if m.GetFileJournalFunc != nil {
		return m.GetFileJournalFunc(ctx, req)
	}
	return &api.GetFileJournalResponse{}, nil
}

func (m *MockClient) RevertFile(ctx context.Context, req api.RevertFileRequest) (*api.RevertFileResponse, error) {
	if m.RevertFileFunc != nil {
		return m.RevertFileFunc(ctx, req)
	}
	return &api.RevertFileResponse{}, nil
}

func (m *MockClient) GetCachedFile(ctx context.Context, req api.GetCachedFileRequest) (*api.GetCachedFileResponse, error) {
	if m.GetCachedFileFunc != nil {
		return m.GetCachedFileFunc(ctx, req)
	}
	return &api.GetCachedFileResponse{}, nil
}

func (m *MockClient) DequeueFrom(ctx context.Context, req api.DequeueFromRequest) (*api.DequeueFromResponse, error) {
	if m.DequeueFromFunc != nil {
		return m.DequeueFromFunc(ctx, req)
	}
	return &api.DequeueFromResponse{}, nil
}

func (m *MockClient) EnqueueMessages(ctx context.Context, req api.EnqueueMessagesRequest) (*api.EnqueueMessagesResponse, error) {
	if m.EnqueueMessagesFunc != nil {
		return m.EnqueueMessagesFunc(ctx, req)
	}
	return &api.EnqueueMessagesResponse{}, nil
}

func (m *MockClient) ClearQueue(ctx context.Context, req api.ClearQueueRequest) (*api.ClearQueueResponse, error) {
	if m.ClearQueueFunc != nil {
		return m.ClearQueueFunc(ctx, req)
	}
	return &api.ClearQueueResponse{}, nil
}

func (m *MockClient) RemoveQueuedMessage(ctx context.Context, req api.RemoveQueuedMessageRequest) (*api.RemoveQueuedMessageResponse, error) {
	if m.RemoveQueuedMessageFunc != nil {
		return m.RemoveQueuedMessageFunc(ctx, req)
	}
	return &api.RemoveQueuedMessageResponse{}, nil
}

func (m *MockClient) SendQueued(ctx context.Context, req api.SendQueuedRequest) (*api.SendQueuedResponse, error) {
	if m.SendQueuedFunc != nil {
		return m.SendQueuedFunc(ctx, req)
	}
	return &api.SendQueuedResponse{}, nil
}
