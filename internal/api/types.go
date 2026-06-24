package api

import (
	"github.com/masterkeysrd/tasksmith/internal/agent/permissions"
	"github.com/masterkeysrd/tasksmith/internal/metrics"
)

type ListProjectsRequest struct {
}

type ListProjectsResponse struct {
	Projects []Project `json:"projects"`
}

type Project struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Path        string `json:"path"`
}

type ListAgentsRequest struct {
}

type ListAgentsResponse struct {
	Agents []Agent `json:"agents"`
}

type Agent struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type ListProvidersRequest struct {
}

type ListProvidersResponse struct {
	Providers       []Provider `json:"providers"`
	DefaultProvider string     `json:"default_provider,omitempty"`
}

type Provider struct {
	Name         string  `json:"name"`
	DisplayName  string  `json:"display_name"`
	Description  string  `json:"description"`
	DefaultModel string  `json:"default_model"`
	Endpoint     string  `json:"endpoint"`
	AuthEnv      string  `json:"auth_env"`
	APIKey       string  `json:"api_key"`
	Models       []Model `json:"models"`
}

type Model struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Label           string `json:"label"`
	ContextWindow   int    `json:"context_window"`
	MaxOutputTokens int    `json:"max_output_tokens"`
}

type ListProvidersPresetsRequest struct {
}

type ListProvidersPresetsResponse struct {
	Providers []Provider `json:"providers"`
}

type ListToolsPresetsRequest struct {
}

type ListToolsPresetsResponse struct {
	Tools []Tool `json:"tools"`
}

type Tool struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Category    string            `json:"category"`
	Labels      map[string]string `json:"labels"`
}

type InitializeWorkspaceRequest struct {
	ProjectName      string          `json:"project_name"`
	SelectedProvider string          `json:"selected_provider"`
	APIKey           string          `json:"api_key"`
	Endpoint         string          `json:"endpoint"`
	DefaultModel     string          `json:"default_model"`
	Theme            string          `json:"theme"`
	AuthorizedTools  map[string]bool `json:"authorized_tools"`
}

type InitializeWorkspaceResponse struct {
	Success bool `json:"success"`
}

type GetWorkspaceConfigRequest struct {
}

type GetWorkspaceConfigResponse struct {
	Name            string          `json:"name"`
	DefaultProvider string          `json:"default_provider"`
	AuthorizedTools map[string]bool `json:"authorized_tools"`
	IsConfigured    bool            `json:"is_configured"`
	CWD             string          `json:"cwd"`
}

type ListSessionsRequest struct {
}

type ListSessionsResponse struct {
	Sessions []Session `json:"sessions"`
}

type Session struct {
	ID              string          `json:"id"`
	Title           string          `json:"title"`
	AgentName       string          `json:"agent_name"`
	ProviderName    string          `json:"provider_name"`
	ModelName       string          `json:"model_name"`
	LastTurnMetrics *SessionMetrics `json:"last_turn_metrics,omitempty"`
	CreatedAt       string          `json:"created_at"`
	UpdatedAt       string          `json:"updated_at"`
}

type SessionMetrics struct {
	SystemTokens               int     `json:"system_tokens"`
	ToolsTokens                int     `json:"tools_tokens"`
	ToolResultTokens           int     `json:"tool_result_tokens"`
	WorkspaceFileTokens        int     `json:"workspace_file_tokens"`
	ChatTokens                 int     `json:"chat_tokens"`
	PromptTokens               int     `json:"prompt_tokens"`
	CompletionTokens           int     `json:"completion_tokens"`
	TotalTokens                int     `json:"total_tokens"`
	EstimatedCostUSD           float64 `json:"estimated_cost_usd"`
	CumulativePromptTokens     int     `json:"cumulative_prompt_tokens"`
	CumulativeCompletionTokens int     `json:"cumulative_completion_tokens"`
	CumulativeTotalTokens      int     `json:"cumulative_total_tokens"`
	CumulativeCostUSD          float64 `json:"cumulative_cost_usd"`
}

type CreateSessionRequest struct {
	Title string `json:"title"`
}

type CreateSessionResponse struct {
	Session Session `json:"session"`
}

type DeleteSessionRequest struct {
	ID string `json:"id"`
}

type DeleteSessionResponse struct {
	Success bool `json:"success"`
}

type RenameSessionRequest struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type RenameSessionResponse struct {
	Success bool `json:"success"`
}

type ArchiveSessionRequest struct {
	ID string `json:"id"`
}

type ArchiveSessionResponse struct {
	Success bool `json:"success"`
}

type SendMessageRequest struct {
	SessionID string `json:"session_id"`
	Text      string `json:"text"`
}

type SendMessageResponse struct {
	Success bool `json:"success"`
}

type GetSessionMessagesRequest struct {
	SessionID string `json:"session_id"`
}

type GetSessionMessagesResponse struct {
	Messages       []string `json:"messages"`                  // Serialized JSON messages
	QueuedMessages []string `json:"queued_messages,omitempty"` // Serialized JSON queued messages
}

type GetSessionStateRequest struct {
	SessionID string `json:"session_id"`
}

type RunningTaskInfo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Details string `json:"details,omitempty"`
}

type GetSessionStateResponse struct {
	Status                string                             `json:"status"`
	Error                 string                             `json:"error,omitempty"`
	IsGenerating          bool                               `json:"is_generating"`
	RunningTasks          []RunningTaskInfo                  `json:"running_tasks,omitempty"`
	Todos                 []Todo                             `json:"todos,omitempty"`
	PendingAuthorizations []permissions.AuthorizationRequest `json:"pending_authorizations,omitempty"`
}

type SubmitAuthorizationDecisionRequest struct {
	SessionID string                              `json:"session_id"`
	Decision  permissions.AuthorizationDecision   `json:"decision"`
	Decisions []permissions.AuthorizationDecision `json:"decisions,omitempty"`
}

type SubmitAuthorizationDecisionResponse struct {
	Success bool `json:"success"`
}

type Todo struct {
	Description string `json:"description"`
	Status      string `json:"status"`
	ActiveText  string `json:"active_text,omitempty"`
}

type GetTokenAnalyticsRequest struct {
	Timeframe      string `json:"timeframe"`       // "today", "7days", "30days"
	ProviderFilter string `json:"provider_filter"` // optional provider instance name
}

type TokenAnalyticsStats = metrics.TokenAnalyticsStats
type DailyActivity = metrics.DailyActivity
type AnalyticsSummaryBreakdown = metrics.AnalyticsSummaryBreakdown
type ByProjectStats = metrics.ByProjectStats
type ByModelStats = metrics.ByModelStats
type ByAgentStats = metrics.ByAgentStats
type ToolAnalytics = metrics.ToolAnalytics

type GetTokenAnalyticsResponse struct {
	GlobalStats      TokenAnalyticsStats       `json:"global_stats"`
	DailyActivity    []DailyActivity           `json:"daily_activity"`
	SummaryBreakdown AnalyticsSummaryBreakdown `json:"summary_breakdown"`
	ByProject        []ByProjectStats          `json:"by_project"`
	ByModel          []ByModelStats            `json:"by_model"`
	ByAgent          []ByAgentStats            `json:"by_agent"`
	Tools            []ToolAnalytics           `json:"tools"`
	ProvidersList    []string                  `json:"providers_list"`
}
