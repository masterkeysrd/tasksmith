package api

import (
	"time"

	"github.com/masterkeysrd/tasksmith/internal/agent/model"
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
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	Label           string            `json:"label"`
	ContextWindow   int               `json:"context_window"`
	MaxOutputTokens int               `json:"max_output_tokens"`
	Family          string            `json:"family,omitempty"`
	OpenWeights     bool              `json:"open_weights,omitempty"`
	Capabilities    ModelCapabilities `json:"capabilities"`
	Pricing         ModelPricing      `json:"pricing"`
	Modalities      ModelModalities   `json:"modalities"`
	IsDefault       bool              `json:"is_default,omitempty"`
	KnowledgeCutoff string            `json:"knowledge_cutoff,omitempty"`
	LastUpdated     string            `json:"last_updated,omitempty"`
}

type ModelCapabilities struct {
	Attachment       bool                   `json:"attachment"`
	Reasoning        bool                   `json:"reasoning"`
	ToolCall         bool                   `json:"tool_call"`
	Temperature      bool                   `json:"temperature"`
	ReasoningOptions []ModelReasoningOption `json:"reasoning_options,omitempty"`
}

type ModelReasoningOption struct {
	Type   string   `json:"type"`
	Values []string `json:"values,omitempty"`
}

type ModelPricing struct {
	Input        float64       `json:"input"`
	Output       float64       `json:"output"`
	CacheRead    float64       `json:"cache_read,omitempty"`
	CacheWrite   float64       `json:"cache_write,omitempty"`
	Reasoning    float64       `json:"reasoning,omitempty"`
	TieredLimits []TierPricing `json:"tiered_limits,omitempty"`
}

type TierPricing struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cache_read,omitempty"`
	CacheWrite float64 `json:"cache_write,omitempty"`
	Reasoning  float64 `json:"reasoning,omitempty"`
	TierLimit  int     `json:"limit"`
}

type ModelModalities struct {
	Inputs  []string `json:"inputs,omitempty"`
	Outputs []string `json:"outputs,omitempty"`
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
	ID              string                `json:"id"`
	Title           string                `json:"title"`
	Settings        model.SessionSettings `json:"settings"`
	LastTurnMetrics *SessionMetrics       `json:"last_turn_metrics,omitempty"`
	CreatedAt       string                `json:"created_at"`
	UpdatedAt       string                `json:"updated_at"`
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

type ConfigureSessionRequest struct {
	SessionID    string                 `json:"session_id"`
	AgentName    string                 `json:"agent_name,omitempty"`
	ProviderName string                 `json:"provider_name,omitempty"`
	ModelName    string                 `json:"model_name,omitempty"`
	Settings     *model.SessionSettings `json:"settings,omitempty"`
}

type ConfigureSessionResponse struct {
	Success bool `json:"success"`
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

type PendingMcpRequest struct {
	ID         string      `json:"id"`
	Type       string      `json:"type"` // "oauth" or "elicitation"
	ServerName string      `json:"server_name"`
	Message    string      `json:"message"`
	URL        string      `json:"url,omitempty"`
	Schema     interface{} `json:"schema,omitempty"`
}

type GetSessionStateResponse struct {
	Status                string                             `json:"status"`
	Error                 string                             `json:"error,omitempty"`
	IsGenerating          bool                               `json:"is_generating"`
	ThinkingDuration      int64                              `json:"thinking_duration,omitempty"` // seconds elapsed since agent started thinking
	RunningTasks          []RunningTaskInfo                  `json:"running_tasks,omitempty"`
	Todos                 []Todo                             `json:"todos,omitempty"`
	PendingAuthorizations []permissions.AuthorizationRequest `json:"pending_authorizations,omitempty"`
	PendingLspSuggestions []LspSuggestion                    `json:"pending_lsp_suggestions,omitempty"`
	PendingMcpRequests    []PendingMcpRequest                `json:"pending_mcp_requests,omitempty"`
}

type ResolveMcpRequest struct {
	RequestID string                 `json:"request_id"`
	Action    string                 `json:"action"`            // "accept", "reject" (for elicitation) or "cancel"
	Content   map[string]interface{} `json:"content,omitempty"` // Form content for elicitation
	Code      string                 `json:"code,omitempty"`    // For OAuth fallback code
	State     string                 `json:"state,omitempty"`   // For OAuth fallback state
}

type ResolveMcpResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

type LspSuggestion struct {
	Language   string   `json:"language"`
	ServerName string   `json:"server_name"`
	Command    []string `json:"command"`
}

type ConfigureLspRequest struct {
	Language string `json:"language"`
}

type ConfigureLspResponse struct {
	Success bool `json:"success"`
}

type DismissLspSuggestionRequest struct {
	Language string `json:"language"`
}

type DismissLspSuggestionResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type GetLspStatusRequest struct{}

type LspServerInfo struct {
	Name      string   `json:"name"`
	Command   []string `json:"command"`
	FileTypes []string `json:"file_types"`
	IsRunning bool     `json:"is_running"`
}

type GetLspStatusResponse struct {
	Servers []LspServerInfo `json:"servers"`
}

type GetLspDiagnosticCountsRequest struct{}

type GetLspDiagnosticCountsResponse struct {
	Errors   int `json:"errors"`
	Warnings int `json:"warnings"`
	Infos    int `json:"infos"`
}

type RestartLspRequest struct{}

type RestartLspResponse struct {
	Success bool `json:"success"`
}

type GetMcpStatusRequest struct{}

type McpCapabilities struct {
	Completions bool `json:"completions"`
	Logging     bool `json:"logging"`
	Prompts     bool `json:"prompts"`
	Resources   bool `json:"resources"`
	Tools       bool `json:"tools"`
}

type McpTool struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	IsDangerous  bool   `json:"is_dangerous,omitempty"`
	IsReadOnly   bool   `json:"is_read_only,omitempty"`
	IsOpenWorld  bool   `json:"is_open_world,omitempty"`
	IsIdempotent bool   `json:"is_idempotent,omitempty"`
	UserHint     string `json:"user_hint,omitempty"`
}

type McpPromptArgument struct {
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required"`
}

type McpPrompt struct {
	Name        string              `json:"name"`
	Title       string              `json:"title,omitempty"`
	Description string              `json:"description,omitempty"`
	Arguments   []McpPromptArgument `json:"arguments,omitempty"`
}

type McpResource struct {
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	MIMEType    string `json:"mime_type,omitempty"`
	URI         string `json:"uri"`
}

type McpResourceTemplate struct {
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	MIMEType    string `json:"mime_type,omitempty"`
	URITemplate string `json:"uri_template"`
}

type McpServerInfo struct {
	Name              string                `json:"name"`
	Type              string                `json:"type"`
	Command           []string              `json:"command,omitempty"`
	URL               string                `json:"url,omitempty"`
	IsRunning         bool                  `json:"is_running"`
	Tools             []McpTool             `json:"tools,omitempty"`
	Prompts           []McpPrompt           `json:"prompts,omitempty"`
	Resources         []McpResource         `json:"resources,omitempty"`
	ResourceTemplates []McpResourceTemplate `json:"resource_templates,omitempty"`
	Error             string                `json:"error,omitempty"`
	Title             string                `json:"title,omitempty"`
	Version           string                `json:"version,omitempty"`
	WebsiteURL        string                `json:"website_url,omitempty"`
	Instructions      string                `json:"instructions,omitempty"`
	Capabilities      McpCapabilities       `json:"capabilities,omitempty"`
	EnvKeys           []string              `json:"env_keys,omitempty"`
	Description       string                `json:"description,omitempty"`
	IsDangerous       bool                  `json:"is_dangerous,omitempty"`
	IsReadOnly        bool                  `json:"is_read_only,omitempty"`
	IsOpenWorld       bool                  `json:"is_open_world,omitempty"`
	IsIdempotent      bool                  `json:"is_idempotent,omitempty"`
	UserHint          string                `json:"user_hint,omitempty"`
	Config            string                `json:"config,omitempty"`
}

type GetMcpStatusResponse struct {
	Servers []McpServerInfo `json:"servers"`
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

type GetLspDiagnosticsRequest struct {
	Path string `json:"path"`
}

type LspDiagnosticItem struct {
	Path     string `json:"path"`
	Message  string `json:"message"`
	Severity string `json:"severity"`
	Line     int    `json:"line"`
	Char     int    `json:"character"`
}

type GetLspDiagnosticsResponse struct {
	Diagnostics []LspDiagnosticItem `json:"diagnostics"`
}

type LspSymbolsRequest struct {
	Query string `json:"query"`
}

type LspSymbolsItem struct {
	Name          string `json:"name"`
	Kind          string `json:"kind"`
	Path          string `json:"path"`
	Line          int    `json:"line"`
	Char          int    `json:"character"`
	ContainerName string `json:"container_name,omitempty"`
}

type LspSymbolsResponse struct {
	Results []LspSymbolsItem `json:"results"`
}

type FileChangeSummary struct {
	Path          string    `json:"path"`
	Kind          string    `json:"kind"`
	TotalEdits    int       `json:"total_edits"`
	NetAdditions  int       `json:"net_additions"`
	NetDeletions  int       `json:"net_deletions"`
	LastChangedAt time.Time `json:"last_changed_at"`
}

type GetFileChangesRequest struct {
	SessionID string `json:"session_id"`
}

type GetFileChangesResponse struct {
	Changes []FileChangeSummary `json:"changes"`
}

type JournalEntryItem struct {
	Timestamp time.Time `json:"ts"`
	ToolName  string    `json:"tool,omitempty"`
	Kind      string    `json:"kind"`
	Content   string    `json:"content,omitempty"`
	Additions int       `json:"additions,omitempty"`
	Deletions int       `json:"deletions,omitempty"`
	Diff      string    `json:"diff,omitempty"`
}

type GetFileJournalRequest struct {
	SessionID string `json:"session_id"`
	Path      string `json:"path"`
}

type GetFileJournalResponse struct {
	Entries []JournalEntryItem `json:"entries"`
}

type RevertFileRequest struct {
	SessionID string `json:"session_id"`
	Path      string `json:"path"`
	Force     bool   `json:"force,omitempty"`
}

type RevertFileResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

type GetCachedFileRequest struct {
	SessionID string `json:"session_id"`
	Path      string `json:"path"`
}

type GetCachedFileResponse struct {
	Content string `json:"content"`
}

type RestartMcpRequest struct {
	ServerName string `json:"server_name"`
}

type RestartMcpResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}
