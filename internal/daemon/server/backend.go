package server

import (
	"context"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/broker"
)

type SystemBackend interface {
	Health(ctx context.Context) (*api.HealthResponse, error)
	Shutdown(ctx context.Context) error
}

type EventBackend interface {
	AddSubscriber(ctx context.Context, subscriberID, since string, topics ...string) (*broker.Subscriber, []string, error)
	RemoveSubscriber(sub *broker.Subscriber)
	Subscribe(ctx context.Context, subscriberID, since string, topics ...string) ([]string, error)
	Unsubscribe(ctx context.Context, subscriberID string, topics ...string) ([]string, error)
	FormatEvent(msg broker.Message) (api.Event, error)
	Done() <-chan struct{}
}

type WorkspaceBackend interface {
	GetWorkspace(ctx context.Context, workspace string) (*api.Workspace, error)
	ReloadWorkspace(ctx context.Context, workspace string) error
	ListAgents(ctx context.Context, workspace string) ([]api.Agent, error)
	GetAgent(ctx context.Context, workspace, name string) (*api.Agent, error)
	ListCommands(ctx context.Context, workspace string) ([]api.Command, error)
	GetCommand(ctx context.Context, workspace, name string) (*api.Command, error)
	ListSkills(ctx context.Context, workspace string) ([]api.Skill, error)
	GetSkill(ctx context.Context, workspace, name string) (*api.Skill, error)
	ListRules(ctx context.Context, workspace string) ([]api.Rule, error)
	ListTools(ctx context.Context, workspace, agent string) ([]api.Tool, error)
	ListModels(ctx context.Context, workspace string) ([]api.Model, error)
	GetModel(ctx context.Context, workspace, id string) (*api.Model, error)
	GetPermissions(ctx context.Context, workspace string) (*api.PermissionsResponse, error)
	AddPermissionRule(ctx context.Context, workspace string, req api.PermissionRuleRequest) (*api.PermissionsResponse, error)
	RemovePermissionRule(ctx context.Context, workspace string, req api.PermissionRuleRequest) (*api.PermissionsResponse, error)
	GetTrustStatus(ctx context.Context, workspace string) (*api.TrustStatus, error)
	TrustWorkspace(ctx context.Context, req api.TrustFolderRequest) error
	RevokeTrust(ctx context.Context, workspace string) error
	GetWorkspaceStatus(ctx context.Context, workspace string) (*api.WorkspaceStatus, error)
	InitWorkspace(ctx context.Context, req api.InitWorkspaceRequest) error
	VerifyProvider(ctx context.Context, req api.VerifyProviderRequest) (*api.VerifyProviderResponse, error)
	StartProviderAuth(ctx context.Context, provider string) (*api.ProviderAuthResponse, error)
	GetProviderAuthStatus(ctx context.Context, provider string) (*api.ProviderAuthResponse, error)
}

// MCPBackend serves the workspace's configured MCP servers: their discovered
// tools, prompts, and resources, plus restart, interactive authorization,
// prompt rendering, resource reads, and manifest policy updates.
type MCPBackend interface {
	ListMCPs(ctx context.Context, workspace string) ([]api.MCPServer, error)
	RestartMCP(ctx context.Context, workspace, name string) (api.MCPServer, error)
	AuthenticateMCP(ctx context.Context, workspace, name string) (api.MCPAuthResponse, error)
	// CompleteMCPAuth feeds a redirect URL the user copied from their browser to
	// the authorization flow in progress for a server.
	CompleteMCPAuth(ctx context.Context, workspace, name string, req api.MCPAuthCallbackRequest) error
	GetMCPPrompt(ctx context.Context, workspace, name, prompt string, req api.MCPPromptRequest) (api.MCPPromptResponse, error)
	ReadMCPResource(ctx context.Context, workspace, name string, req api.MCPResourceRequest) (api.MCPResourceResponse, error)
	AllowMCPPolicy(ctx context.Context, workspace, name string, req api.MCPAllowPolicyRequest) (api.MCPServer, error)
}

type SessionBackend interface {
	CreateSession(ctx context.Context, workspace string, req api.CreateSessionRequest) (*api.Session, bool, error)
	PreviewSession(ctx context.Context, workspace string, req api.PreviewSessionRequest) (*api.SessionPreview, error)
	ListSessions(ctx context.Context, workspace string) ([]api.Session, error)
	GetSession(ctx context.Context, workspace, id string) (*api.Session, error)
	UpdateSession(ctx context.Context, workspace, id string, req api.UpdateSessionRequest) (*api.Session, error)
	DeleteSession(ctx context.Context, workspace, id string) error
	Timeline(ctx context.Context, workspace, id string) ([]api.TimelineEntry, error)
	Send(ctx context.Context, workspace, id string, req api.SendRequest) error
	Cancel(ctx context.Context, workspace, id string) error

	Changes(ctx context.Context, workspace, id string) ([]api.FileChange, error)
	Diff(ctx context.Context, workspace, id, path, entryID string) (*api.DiffResponse, error)
	Revert(ctx context.Context, workspace, id string, req api.RevertRequest) error

	Tasks(ctx context.Context, workspace, id string) ([]api.Task, error)
	KillTask(ctx context.Context, workspace, id, taskID string) error
	StreamTaskOutput(ctx context.Context, workspace, id, taskID string) (<-chan string, string, func(), error)
}

// MetricsBackend serves read-only usage and metrics queries.
type MetricsBackend interface {
	MetricsQuery(ctx context.Context, req api.MetricsQuery) (*api.MetricsResult, error)
	SessionUsage(ctx context.Context, workspace, id string) (*api.SessionUsage, error)
	Usage(ctx context.Context, workspace string, req api.UsageRequest) (*api.UsageSummary, error)
}

type Backend interface {
	SystemBackend
	EventBackend
	WorkspaceBackend
	MCPBackend
	SessionBackend
	MetricsBackend
}
