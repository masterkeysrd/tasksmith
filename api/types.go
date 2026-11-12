package api

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
)

// HeaderCWD is the HTTP request header carrying the client's working directory.
const HeaderCWD = "X-TaskSmith-CWD"

// Workspace describes the resolved configuration metadata for a workspace directory.
type Workspace struct {
	Name         string   `json:"name"`
	RootDir      string   `json:"root_dir"`
	CWD          string   `json:"cwd,omitempty"`
	DefaultAgent string   `json:"default_agent"`
	DefaultModel *Model   `json:"default_model,omitempty"`
	Warnings     []string `json:"warnings,omitempty"`
	IsRestricted bool     `json:"is_restricted,omitempty"`
}

// WorkspaceFile describes a file or directory in the workspace available for context mention.
type WorkspaceFile struct {
	Path      string `json:"path"`                 // Workspace-relative slash path; directories do NOT need trailing slash here (note: @file:<dir>/ uses trailing slash in prompt tokens!)
	ShortPath string `json:"short_path,omitempty"` // Shortest unique slash-delimited suffix across the workspace (directories end with "/")
	Name      string `json:"name"`                 // Basename
	Dir       string `json:"dir,omitempty"`        // Parent directory ("" for root items)
	IsDir     bool   `json:"is_dir,omitempty"`     // True when entry is a directory
	Recent    bool   `json:"recent,omitempty"`     // True when recently read/modified in the session
	Allowed   bool   `json:"allowed"`              // False when blocked by explicit workspace/agent policies or deny rules
}

// AttachmentKind discriminates the resource kind of an [Attachment].
type AttachmentKind string

const (
	AttachmentFile   AttachmentKind = "file"
	AttachmentRule   AttachmentKind = "rule"
	AttachmentMCP    AttachmentKind = "mcp"
	AttachmentSymbol AttachmentKind = "symbol"
	AttachmentSkill  AttachmentKind = "skill"
)

// Attachment binds an inline reference token in a user message to its canonical workspace target.
type Attachment struct {
	Kind            AttachmentKind `json:"kind"`
	Ref             string         `json:"ref,omitempty"`              // Token in user text, e.g. "@composer.go", "@rule:go-verification-gate", "@mcp:notes:notes://spec-1"
	Target          string         `json:"target"`                     // Canonical workspace-relative path (trailing "/" for dirs), rule name, "<server>:<uri>", etc.
	StartLine       int            `json:"start_line,omitempty"`       // Optional 1-based start line for file/symbol slices
	EndLine         int            `json:"end_line,omitempty"`         // Optional 1-based end line for file/symbol slices
	AllowProhibited bool           `json:"allow_prohibited,omitempty"` // True when an interactive user explicitly confirmed a prohibited MCP resource once
}

// ListFilesRequest configures a workspace file search or listing.
type ListFilesRequest struct {
	Query         string `json:"query,omitempty"`
	SessionID     string `json:"session_id,omitempty"`
	Limit         int    `json:"limit,omitempty"`
	IncludeHidden *bool  `json:"include_hidden,omitempty"` // Defaults to true (excluding .git and .env secrets)
}

// TrustStatus summarizes trust and executable configuration status for a directory.
type TrustStatus struct {
	Path               string                  `json:"path"`
	IsTrusted          bool                    `json:"is_trusted"`
	FingerprintChanged bool                    `json:"fingerprint_changed,omitempty"`
	Executables        ExecutableConfigSummary `json:"executables"`
}

// ExecutableConfigSummary summarizes the executable configurations in a project.
type ExecutableConfigSummary struct {
	MCPServers  []string `json:"mcp_servers,omitempty"`
	CustomTools []string `json:"custom_tools,omitempty"`
	HasEnv      bool     `json:"has_env,omitempty"`
}

// TrustFolderRequest configures a directory trust action.
type TrustFolderRequest struct {
	Path  string `json:"path"`
	Scope string `json:"scope,omitempty"` // "folder" or "parent"
}

// WorkspaceStatus summarizes the onboarding and configuration state of a workspace directory.
type WorkspaceStatus struct {
	Path               string                  `json:"path"`
	IsTrusted          bool                    `json:"is_trusted"`
	FingerprintChanged bool                    `json:"fingerprint_changed,omitempty"`
	IsConfigured       bool                    `json:"is_configured"`
	HasAgentsMD        bool                    `json:"has_agents_md"`
	Executables        ExecutableConfigSummary `json:"executables"`
	GlobalProviders    []ProviderSummary       `json:"global_providers,omitempty"`
	GlobalMCPs         []string                `json:"global_mcps,omitempty"`
	MissingCredentials []string                `json:"missing_credentials,omitempty"`
	NeedsOnboarding    bool                    `json:"needs_onboarding"`
}

// ProviderSummary summarizes an available or configured model provider.
type ProviderSummary struct {
	Name         string   `json:"name"`
	DisplayName  string   `json:"display_name,omitempty"`
	Status       string   `json:"status,omitempty"` // e.g. "✓ key", "✓ local", "needs key", "sign-in"
	HasKey       bool     `json:"has_key"`
	AuthType     string   `json:"auth_type,omitempty"` // "env", "bearer", "oauth2", etc.
	AuthEnv      string   `json:"auth_env,omitempty"`
	Account      string   `json:"account,omitempty"` // Authenticated account email if OAuth
	DefaultModel string   `json:"default_model,omitempty"`
	Models       []string `json:"models,omitempty"`
	Scope        string   `json:"scope,omitempty"` // "system", "user", "workspace", "project"
}

// ProviderAuthRequest initiates interactive OAuth for a provider.
type ProviderAuthRequest struct {
	Provider string `json:"provider"`
}

// ProviderAuthResponse returns the authorization URL and status for an interactive OAuth flow.
type ProviderAuthResponse struct {
	Provider string `json:"provider"`
	AuthURL  string `json:"auth_url,omitempty"`
	Status   string `json:"status"` // "idle", "authorizing", "authenticated", "error"
	Account  string `json:"account,omitempty"`
	Error    string `json:"error,omitempty"`
}

// VerifyProviderRequest requests verification of a provider's credentials or endpoint.
type VerifyProviderRequest struct {
	Provider string `json:"provider"`
	APIKey   string `json:"api_key,omitempty"`
	Endpoint string `json:"endpoint,omitempty"`
	AuthType string `json:"auth_type,omitempty"`
	AuthEnv  string `json:"auth_env,omitempty"`
}

// VerifyProviderResponse returns the verification outcome for a provider.
type VerifyProviderResponse struct {
	Valid        bool   `json:"valid"`
	ModelsCount  int    `json:"models_count,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
	Warning      string `json:"warning,omitempty"`
}

// InitWorkspaceRequest represents the payload for POST /v1/workspace/init.
type InitWorkspaceRequest struct {
	CWD           string            `json:"cwd"`
	Scope         string            `json:"scope,omitempty"` // "user", "workspace", "project"
	ProjectName   string            `json:"project_name,omitempty"`
	Provider      string            `json:"provider,omitempty"`
	APIKey        string            `json:"api_key,omitempty"`
	Endpoint      string            `json:"endpoint,omitempty"`
	AuthType      string            `json:"auth_type,omitempty"`
	AuthEnv       string            `json:"auth_env,omitempty"`
	DefaultModels map[string]string `json:"default_models,omitempty"`
	DefaultMode   PermissionMode    `json:"default_mode,omitempty"`
	DisabledTools []string          `json:"disabled_tools,omitempty"`
	DisabledMCPs  []string          `json:"disabled_mcps,omitempty"`
}

// ContextBreakdown categorizes token usage within the context window.
type ContextBreakdown struct {
	SystemPrompt   int `json:"system_prompt"`
	ContextFiles   int `json:"context_files,omitempty"`
	SystemTools    int `json:"system_tools,omitempty"`
	CustomTools    int `json:"custom_tools,omitempty"`
	MCPTools       int `json:"mcp_tools,omitempty"`
	Skills         int `json:"skills,omitempty"`
	Rules          int `json:"rules,omitempty"`
	Subagents      int `json:"subagents,omitempty"`
	Summary        int `json:"summary,omitempty"`
	UserMessages   int `json:"user_messages,omitempty"`
	AgentResponses int `json:"agent_responses,omitempty"`
	ToolCalls      int `json:"tool_calls,omitempty"`
}

// ContextState tracks token usage, limits, and breakdown for a session context window.
type ContextState struct {
	UsedTokens    int              `json:"used_tokens"`
	MaxTokens     int              `json:"max_tokens"`
	OutputLimit   int              `json:"output_limit,omitempty"`
	ReserveTokens int              `json:"reserve_tokens"`
	TriggerTokens int              `json:"trigger_tokens"`
	FreeTokens    int              `json:"free_tokens"`
	Breakdown     ContextBreakdown `json:"breakdown"`
	InputTokens   int              `json:"input_tokens,omitempty"`
	OutputTokens  int              `json:"output_tokens,omitempty"`
	CachedTokens  int              `json:"cached_tokens,omitempty"`
	Compactions   int              `json:"compactions,omitempty"`
	LastCompacted time.Time        `json:"last_compacted,omitzero"`
}

// Agent describes an effective agent configuration in a workspace.
type Agent struct {
	Name        string           `json:"name"`
	DisplayName string           `json:"display_name,omitempty"`
	Description string           `json:"description,omitempty"`
	Triggers    []string         `json:"triggers,omitempty"`
	Models      []string         `json:"models,omitempty"`
	Thinking    *ThinkingSetting `json:"thinking,omitempty"`
	Context     ContextBreakdown `json:"context"`
}

// CanHumanTrigger reports whether a human caller (the --agent flag, the /agent
// command, or human-initiated session creation) is allowed to invoke this agent.
// An empty Triggers list means the agent is unrestricted and may be invoked by
// any caller, including humans.
func (a Agent) CanHumanTrigger() bool {
	if len(a.Triggers) == 0 {
		return true
	}
	for _, t := range a.Triggers {
		if strings.EqualFold(t, "human") {
			return true
		}
	}
	return false
}

// Argument defines a structured parameter accepted by a command or skill.
type Argument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Default     string `json:"default,omitempty"`
}

// Command describes an executable slash command prompt template in the workspace.
type Command struct {
	Name         string     `json:"name"`
	DisplayName  string     `json:"display_name,omitempty"`
	Description  string     `json:"description,omitempty"`
	Agent        string     `json:"agent,omitempty"`
	Context      string     `json:"context,omitempty"`
	ArgumentHint string     `json:"argument_hint,omitempty"`
	Arguments    []Argument `json:"arguments,omitempty"`
	Models       []string   `json:"models,omitempty"`
	Tools        []string   `json:"tools,omitempty"`
	Skills       []string   `json:"skills,omitempty"`
	Rules        []string   `json:"rules,omitempty"`
	Namespace    string     `json:"namespace,omitempty"`
}

// Skill describes an expertise module or workflow in the workspace.
type Skill struct {
	Name         string     `json:"name"`
	DisplayName  string     `json:"display_name,omitempty"`
	Description  string     `json:"description,omitempty"`
	UseWhen      string     `json:"use_when,omitempty"`
	Keywords     []string   `json:"keywords,omitempty"`
	ArgumentHint string     `json:"argument_hint,omitempty"`
	Arguments    []Argument `json:"arguments,omitempty"`
	Tools        []string   `json:"tools,omitempty"`
	Namespace    string     `json:"namespace,omitempty"`
	Scope        string     `json:"scope,omitempty"`
	Instructions string     `json:"instructions,omitempty"`
	Tokens       int        `json:"tokens,omitempty"`
}

// RuleTriggers defines the activation conditions for a workspace rule.
type RuleTriggers struct {
	Always   bool     `json:"always,omitempty"`
	Globs    []string `json:"globs,omitempty"`
	UseWhen  string   `json:"use_when,omitempty"`
	Keywords []string `json:"keywords,omitempty"`
	Decision string   `json:"decision,omitempty"`
	Tools    []string `json:"tools,omitempty"`
}

// Rule describes an ambient coding standard, constraint, or guardrail in the workspace.
type Rule struct {
	Name         string       `json:"name"`
	DisplayName  string       `json:"display_name,omitempty"`
	Description  string       `json:"description,omitempty"`
	Priority     int          `json:"priority,omitempty"`
	Triggers     RuleTriggers `json:"triggers"`
	Namespace    string       `json:"namespace,omitempty"`
	Scope        string       `json:"scope,omitempty"`
	Path         string       `json:"path,omitempty"`
	Instructions string       `json:"instructions,omitempty"`
	Tokens       int          `json:"tokens,omitempty"`
}

// ToolAnnotation defines the safety profile and behavioral constraints for a tool.
type ToolAnnotation struct {
	IsOpenWorld  bool   `json:"is_open_world,omitempty"`
	IsDangerous  bool   `json:"is_dangerous,omitempty"`
	IsReadOnly   bool   `json:"is_read_only,omitempty"`
	IsIdempotent bool   `json:"is_idempotent,omitempty"`
	UserHint     string `json:"user_hint,omitempty"`
}

// ToolStatus classifies a tool's availability in an agent's active toolset.
type ToolStatus string

const (
	// ToolStatusEager marks a tool loaded on the agent's first request.
	ToolStatusEager ToolStatus = "eager"
	// ToolStatusDeferred marks a policy-allowed tool held back from the initial
	// context and discoverable via tool_search.
	ToolStatusDeferred ToolStatus = "deferred"
	// ToolStatusDenied marks a tool blocked by the workspace or agent tool policy.
	ToolStatusDenied ToolStatus = "denied"
	// ToolStatusInactive marks a tool that is not applicable to the agent at all
	// (for example the subagent tools for an agent with no subagents), so it is
	// neither loaded, deferred, nor explicitly denied.
	ToolStatusInactive ToolStatus = "inactive"
)

// Tool describes an available built-in or custom executable tool in the workspace.
type Tool struct {
	Name         string         `json:"name"`
	DisplayName  string         `json:"display_name,omitempty"`
	Description  string         `json:"description,omitempty"`
	Namespace    string         `json:"namespace,omitempty"`
	Scope        string         `json:"scope,omitempty"`
	Command      []string       `json:"command,omitempty"`
	InputSchema  map[string]any `json:"input_schema,omitempty"`
	OutputSchema map[string]any `json:"output_schema,omitempty"`
	Annotations  ToolAnnotation `json:"annotations"`
	Tokens       int            `json:"tokens,omitempty"`
	// Status is the tool's availability for the agent the request was scoped to.
	// It is empty when the listing was not scoped to an agent.
	Status ToolStatus `json:"status,omitempty"`
}

type ThinkingValue struct {
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
}

type ThinkingCapability struct {
	Type      string          `json:"type"`
	Values    []ThinkingValue `json:"values,omitempty"`
	Default   string          `json:"default,omitempty"`
	Min       int             `json:"min,omitempty"`
	Max       int             `json:"max,omitempty"`
	IsDefault bool            `json:"is_default,omitempty"`
}

type ThinkingSetting struct {
	Type    string `json:"type"`
	Effort  string `json:"effort,omitempty"`
	Budget  int    `json:"budget,omitempty"`
	Enabled bool   `json:"enabled,omitempty"`
}

type ModelCapabilities struct {
	Completion       *bool    `json:"completion,omitempty"`
	Decision         *bool    `json:"decision,omitempty"`
	Embedding        *bool    `json:"embedding,omitempty"`
	Tools            *bool    `json:"tools,omitempty"`
	Attachment       *bool    `json:"attachment,omitempty"`
	StructuredOutput *bool    `json:"structured_output,omitempty"`
	Temperature      *bool    `json:"temperature,omitempty"`
	Dimensions       []int    `json:"dimensions,omitempty"`
	InputModalities  []string `json:"input_modalities,omitempty"`
	OutputModalities []string `json:"output_modalities,omitempty"`
}

// SupportsCompletion reports whether a model can serve chat completion. A nil
// flag means the capability was never reported, which counts as supported, the
// same default the daemon applies. Clients use it to keep embedding-only models
// out of model pickers.
func (c ModelCapabilities) SupportsCompletion() bool {
	return c.Completion == nil || *c.Completion
}

type ModelPricing struct {
	Input      float64            `json:"input"`
	Output     float64            `json:"output"`
	CacheRead  float64            `json:"cache_read"`
	CacheWrite float64            `json:"cache_write"`
	Reasoning  float64            `json:"reasoning"`
	Tiers      []ModelPricingTier `json:"tiers,omitempty"`
}

type ModelPricingTier struct {
	Label         string  `json:"label,omitempty"`
	ConditionType string  `json:"condition_type,omitempty"`
	ConditionSize int     `json:"condition_size,omitempty"`
	Input         float64 `json:"input"`
	Output        float64 `json:"output"`
	CacheRead     float64 `json:"cache_read"`
	CacheWrite    float64 `json:"cache_write"`
	Reasoning     float64 `json:"reasoning"`
}

// Model describes a resolved model and its capabilities from a workspace provider.
type Model struct {
	ID              string               `json:"id"`
	Provider        string               `json:"provider,omitempty"`
	Name            string               `json:"name"`
	Label           string               `json:"label,omitempty"`
	Aliases         []string             `json:"aliases,omitempty"`
	Thinking        []ThinkingCapability `json:"thinking,omitempty"`
	ThinkingDefault *ThinkingSetting     `json:"thinking_default,omitempty"`
	ContextLimit    int                  `json:"context_limit,omitempty"`
	OutputLimit     int                  `json:"output_limit,omitempty"`
	Capabilities    ModelCapabilities    `json:"capabilities"`
	Pricing         *ModelPricing        `json:"pricing,omitempty"`
}

// HealthResponse reports the daemon's process identity, startup timestamp,
// and currently initialized workspace directories.
type HealthResponse struct {
	PID        int       `json:"pid"`
	StartedAt  time.Time `json:"started_at"`
	Workspaces []string  `json:"workspaces,omitempty"`
}

// ErrorResponse is the JSON wire envelope returned on non-2xx HTTP responses.
type ErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// APIError represents a structured HTTP error returned by the Brain Daemon.
type APIError struct {
	StatusCode int    `json:"status_code"`
	Code       string `json:"code"`
	Message    string `json:"message"`
}

// Error implements the standard error interface.
func (e *APIError) Error() string {
	if e == nil {
		return ""
	}
	if e.Code != "" {
		return fmt.Sprintf("api: %s (%d): %s", e.Code, e.StatusCode, e.Message)
	}
	return fmt.Sprintf("api: status %d: %s", e.StatusCode, e.Message)
}

// SessionStatus describes the live execution state of a session.
type SessionStatus string

const (
	StatusIdle              SessionStatus = "idle"
	StatusThinking          SessionStatus = "thinking"
	StatusGenerating        SessionStatus = "generating"
	StatusExecuting         SessionStatus = "executing"
	StatusCompacting        SessionStatus = "compacting"
	StatusWaitingPermission SessionStatus = "waiting_permission"
	StatusWaitingQuestion   SessionStatus = "waiting_question"
)

// SessionConfig holds configurable runtime settings for a [Session].
type SessionConfig struct {
	Agent    string           `json:"agent,omitempty"`
	Model    string           `json:"model,omitempty"`
	Thinking *ThinkingSetting `json:"thinking,omitempty"`
	Mode     PermissionMode   `json:"mode,omitempty"`
}

// TaskStatus describes the operational state of a background task.
type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
	TaskStatusCanceled  TaskStatus = "canceled"
)

// Task describes a background task running or finished within a session.
type Task struct {
	ID           string     `json:"id"`
	Kind         string     `json:"kind"`
	Title        string     `json:"title"`
	Command      string     `json:"command,omitempty"`
	Status       TaskStatus `json:"status"`
	ExitCode     int        `json:"exit_code"`
	Error        string     `json:"error,omitempty"`
	LogPath      string     `json:"log_path,omitempty"`
	StartedAt    time.Time  `json:"started_at"`
	FinishedAt   time.Time  `json:"finished_at,omitzero"`
	DurationMs   int64      `json:"duration_ms"`
	Backgrounded bool       `json:"backgrounded"`
}

// TasksResponse returns the list of background tasks in a session.
type TasksResponse struct {
	Tasks []Task `json:"tasks"`
}

type QueueKind string

const (
	QueueKindMessage  QueueKind = "message"
	QueueKindCommand  QueueKind = "command"
	QueueKindCompact  QueueKind = "compact"
	QueueKindTask     QueueKind = "task"
	QueueKindSubagent QueueKind = "subagent"
)

type QueueItem struct {
	ID        string          `json:"id"`
	Kind      QueueKind       `json:"kind"`
	Message   *Message        `json:"message,omitempty"`
	Command   string          `json:"command,omitempty"`
	Compact   *CompactRequest `json:"compact,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

type QuestionType string

const (
	QuestionTypeSingle  QuestionType = "single"
	QuestionTypeMulti   QuestionType = "multi"
	QuestionTypeText    QuestionType = "text"
	QuestionTypeConfirm QuestionType = "confirm"
)

type QuestionRequest struct {
	ID          string     `json:"id"`
	ToolCallID  string     `json:"tool_call_id"`
	Title       string     `json:"title,omitempty"`
	Description string     `json:"description,omitempty"`
	Questions   []Question `json:"questions"`
}

// Clone returns a deep copy of QuestionRequest with cloned slices.
func (r *QuestionRequest) Clone() *QuestionRequest {
	if r == nil {
		return nil
	}
	cp := &QuestionRequest{
		ID:          r.ID,
		ToolCallID:  r.ToolCallID,
		Title:       r.Title,
		Description: r.Description,
		Questions:   make([]Question, len(r.Questions)),
	}
	for i, q := range r.Questions {
		o := make([]QuestionOption, len(q.Options))
		copy(o, q.Options)
		cp.Questions[i] = Question{
			Type:        q.Type,
			Question:    q.Question,
			Description: q.Description,
			Label:       q.Label,
			Options:     o,
		}
	}
	return cp
}

type QuestionResponse struct {
	RequestID  string           `json:"request_id"`
	ToolCallID string           `json:"tool_call_id"`
	Canceled   bool             `json:"canceled,omitempty"`
	Answers    []QuestionAnswer `json:"answers,omitempty"`
}

// Clone returns a deep copy of QuestionResponse with cloned slices.
func (r *QuestionResponse) Clone() *QuestionResponse {
	if r == nil {
		return nil
	}
	cp := &QuestionResponse{
		RequestID:  r.RequestID,
		ToolCallID: r.ToolCallID,
		Canceled:   r.Canceled,
		Answers:    make([]QuestionAnswer, len(r.Answers)),
	}
	for i, a := range r.Answers {
		var confirmed *bool
		if a.Confirmed != nil {
			v := *a.Confirmed
			confirmed = &v
		}
		cp.Answers[i] = QuestionAnswer{
			Question:  a.Question,
			Selected:  slices.Clone(a.Selected),
			Text:      a.Text,
			Confirmed: confirmed,
		}
	}
	return cp
}

type SessionDiagnostic struct {
	Code    string `json:"code"`
	Setting string `json:"setting"`
	Message string `json:"message"`
}

// Session represents a persistent conversational session managed by the daemon.
type Session struct {
	ID                 string              `json:"id"`
	Title              string              `json:"title"`
	Config             SessionConfig       `json:"config"`
	Model              *Model              `json:"model,omitempty"`
	Diagnostics        []SessionDiagnostic `json:"diagnostics,omitempty"`
	Status             SessionStatus       `json:"status"`
	Context            ContextState        `json:"context"`
	PendingPermissions []PermissionRequest `json:"pending_permissions,omitempty"`
	PendingQuestions   []QuestionRequest   `json:"pending_questions,omitempty"`
	Tasks              []Task              `json:"tasks,omitempty"`
	Todos              []TodoItem          `json:"todos,omitempty"`
	Queue              []QueueItem         `json:"queue,omitempty"`
	ParentID           string              `json:"parent_id,omitempty"`
	WorkDir            string              `json:"work_dir,omitempty"`
	Subagents          []Session           `json:"subagents,omitempty"`
	Activity           string              `json:"activity,omitempty"`
	CreatedAt          time.Time           `json:"created_at"`
	UpdatedAt          time.Time           `json:"updated_at"`
}

// CreateSessionRequest holds optional parameters for creating a new session.
//
// ID is optional. When set it must be a canonical lowercase UUIDv7 and makes
// the request idempotent: creating an ID that already exists returns the
// existing session when every supplied field matches, and fails with a
// conflict otherwise.
type CreateSessionRequest struct {
	ID     string        `json:"id,omitempty"`
	Title  string        `json:"title,omitempty"`
	Config SessionConfig `json:"config"`
}

// PreviewSessionRequest holds parameters for previewing draft session resolution.
type PreviewSessionRequest struct {
	Config SessionConfig `json:"config"`
}

// SessionPreview holds resolved model, configuration, and diagnostics for a draft session.
type SessionPreview struct {
	Config      SessionConfig       `json:"config"`
	Model       *Model              `json:"model,omitempty"`
	Diagnostics []SessionDiagnostic `json:"diagnostics,omitempty"`
}

// UpdateSessionRequest holds optional fields to update on an existing session.
type UpdateSessionRequest struct {
	Title  string        `json:"title,omitempty"`
	Config SessionConfig `json:"config"`
}

// Role identifies the author of a conversation message.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
	RoleSystem    Role = "system"
)

// BlockType discriminates the content kind of a [Block].
type BlockType string

const (
	BlockText      BlockType = "text"
	BlockReasoning BlockType = "reasoning"
	BlockToolCall  BlockType = "tool_call"
)

// ToolCall represents a structured tool invocation block within an assistant message.
type ToolCall struct {
	ID   string         `json:"id"`
	Name string         `json:"name"`
	Args map[string]any `json:"args,omitempty"`
}

// Block represents a single typed unit of message content.
type Block struct {
	Type      BlockType `json:"type"`
	Text      string    `json:"text,omitempty"`
	Signature string    `json:"signature,omitempty"`
	ToolCall  *ToolCall `json:"tool_call,omitempty"`
}

// Message represents a persisted turn in a session's UI conversation history.
type Message struct {
	ID                string          `json:"id"`
	Role              Role            `json:"role"`
	Content           []Block         `json:"content,omitempty"`
	StructuredContent json.RawMessage `json:"structured_content,omitempty"`
	ToolCallID        string          `json:"tool_call_id,omitempty"`
	Name              string          `json:"name,omitempty"`
	IsError           bool            `json:"is_error,omitempty"`
	CreatedAt         time.Time       `json:"created_at,omitzero"`
	Attachments       []Attachment    `json:"attachments,omitempty"`
}

// Chunk represents an incremental streaming assistant delta.
type Chunk struct {
	ID      string  `json:"id"`
	Content []Block `json:"content,omitempty"`
	Done    bool    `json:"done,omitempty"`
	// Usage is the cumulative input and output token count of the LLM call
	// this chunk belongs to, when the provider reported it. Each chunk
	// carries the running total, not a delta.
	Usage *ChunkUsage `json:"usage,omitempty"`
}

// ChunkUsage is the cumulative token usage of an in-flight LLM call.
type ChunkUsage struct {
	Input  int `json:"input,omitempty"`
	Output int `json:"output,omitempty"`
}

// CompactRequest specifies optional focus parameters for manual context compaction.
type CompactRequest struct {
	Focus string `json:"focus,omitempty"`
}

// SendRequest specifies the user message payload or permission decision to submit to the session.
type SendRequest struct {
	ID          string              `json:"id,omitempty"`
	Content     string              `json:"content,omitempty"`
	Blocks      []Block             `json:"blocks,omitempty"`
	Command     string              `json:"command,omitempty"`
	Decision    *PermissionDecision `json:"decision,omitempty"`
	Question    *QuestionResponse   `json:"question,omitempty"`
	Compact     *CompactRequest     `json:"compact,omitempty"`
	Attachments []Attachment        `json:"attachments,omitempty"`
}

// PermissionMode controls the session-wide tool approval policy.
type PermissionMode string

const (
	ModeDefault     PermissionMode = "default"
	ModeAcceptEdits PermissionMode = "accept_edits"
	ModePlan        PermissionMode = "plan"
	ModeAsk         PermissionMode = "ask"
	ModeAuto        PermissionMode = "auto"
)

func (m PermissionMode) String() string {
	return string(m)
}

func (m PermissionMode) IsValid() bool {
	switch m {
	case ModeDefault, ModeAcceptEdits, ModePlan, ModeAsk, ModeAuto:
		return true
	default:
		return false
	}
}

// TodoStatus describes the completion status of a task checklist item.
type TodoStatus string

const (
	TodoStatusPending    TodoStatus = "pending"
	TodoStatusInProgress TodoStatus = "in_progress"
	TodoStatusCompleted  TodoStatus = "completed"
)

func (s TodoStatus) IsValid() bool {
	switch s {
	case TodoStatusPending, TodoStatusInProgress, TodoStatusCompleted:
		return true
	default:
		return false
	}
}

// ActionType classifies the category of a tool operation.
type ActionType string

const (
	ActionRead    ActionType = "read"
	ActionUnknown ActionType = "unknown"
	ActionNet     ActionType = "net"
	ActionWrite   ActionType = "write"
	ActionExec    ActionType = "exec"
	ActionDelete  ActionType = "delete"
)

// String returns the canonical lowercase name of the action type.
func (a ActionType) String() string {
	switch a {
	case ActionRead, ActionUnknown, ActionNet, ActionWrite, ActionExec, ActionDelete:
		return string(a)
	default:
		return string(ActionUnknown)
	}
}

// Severity returns the numeric severity rank of the action type, ordered from
// lowest (ActionRead = 0) to highest (ActionDelete = 5).
func (a ActionType) Severity() int {
	switch a {
	case ActionRead:
		return 0
	case ActionUnknown:
		return 1
	case ActionNet:
		return 2
	case ActionWrite:
		return 3
	case ActionExec:
		return 4
	case ActionDelete:
		return 5
	default:
		return 1
	}
}

// SafetyLevel classifies the risk profile of a planned tool operation.
type SafetyLevel string

const (
	SafetySafe    SafetyLevel = "safe"
	SafetyOutside SafetyLevel = "outside"
	SafetyUnknown SafetyLevel = "unknown"
	SafetyUnsafe  SafetyLevel = "unsafe"
)

// String returns the canonical lowercase name of the safety level.
func (s SafetyLevel) String() string {
	switch s {
	case SafetySafe, SafetyOutside, SafetyUnknown, SafetyUnsafe:
		return string(s)
	default:
		return string(SafetyUnknown)
	}
}

// Severity returns the numeric risk rank of the safety level, ordered from
// safest (SafetySafe = 0) to most unsafe (SafetyUnsafe = 3).
func (s SafetyLevel) Severity() int {
	switch s {
	case SafetySafe:
		return 0
	case SafetyOutside:
		return 1
	case SafetyUnknown:
		return 2
	case SafetyUnsafe:
		return 3
	default:
		return 2
	}
}

// PermissionScope specifies the boundary tier for an allowed permission rule.
type PermissionScope string

const (
	ScopeSession PermissionScope = "session"
	ScopeProject PermissionScope = "project"
	ScopeGlobal  PermissionScope = "global"
)

// PermissionEffect defines whether a permission rule allows or denies matching tool calls.
type PermissionEffect string

const (
	EffectAllow PermissionEffect = "allow"
	EffectDeny  PermissionEffect = "deny"
)

// Permissions holds allow and deny rules, default mode, and disabled tools/mcps for a single scope.
type Permissions struct {
	Schema        int            `json:"schema,omitempty"`
	DefaultMode   PermissionMode `json:"defaultMode,omitempty"`
	DisabledTools []string       `json:"disabledTools,omitempty"`
	DisabledMCPs  []string       `json:"disabledMcps,omitempty"`
	Allow         []string       `json:"allow,omitempty"`
	Deny          []string       `json:"deny,omitempty"`
}

// Clone returns a deep copy of Permissions.
func (p Permissions) Clone() Permissions {
	return Permissions{
		Schema:        p.Schema,
		DefaultMode:   p.DefaultMode,
		DisabledTools: slices.Clone(p.DisabledTools),
		DisabledMCPs:  slices.Clone(p.DisabledMCPs),
		Allow:         slices.Clone(p.Allow),
		Deny:          slices.Clone(p.Deny),
	}
}

// PermissionsResponse returns the configured project and global permissions.
type PermissionsResponse struct {
	Project Permissions `json:"project"`
	Global  Permissions `json:"global"`
}

// PermissionRuleRequest specifies a rule mutation on project or global permissions.
type PermissionRuleRequest struct {
	Scope  PermissionScope  `json:"scope"`
	Effect PermissionEffect `json:"effect"`
	Rule   string           `json:"rule"`
}

// DecisionKind represents a user's approval or rejection choice for a permission request.
type DecisionKind string

const (
	DecisionAllowOnce    DecisionKind = "allow_once"
	DecisionAllowSession DecisionKind = "allow_session"
	DecisionReject       DecisionKind = "reject"
)

// PermissionRequest describes a pending tool operation awaiting user approval.
type PermissionRequest struct {
	ID            string      `json:"id"`
	ToolCallID    string      `json:"tool_call_id"`
	ToolName      string      `json:"tool_name"`
	Action        ActionType  `json:"action"`
	Safety        SafetyLevel `json:"safety"`
	Target        string      `json:"target,omitempty"`
	Summary       string      `json:"summary"`
	Diff          string      `json:"diff,omitempty"`
	GrantPatterns []string    `json:"grant_patterns,omitempty"`
}

// PermissionDecision submits a user's resolution for a pending [PermissionRequest].
type PermissionDecision struct {
	RequestID    string          `json:"request_id,omitempty"`
	ToolCallID   string          `json:"tool_call_id,omitempty"`
	Kind         DecisionKind    `json:"kind"`
	GrantPattern string          `json:"grant_pattern,omitempty"`
	Scope        PermissionScope `json:"scope,omitempty"`
	Feedback     string          `json:"feedback,omitempty"`
}

// ChangeSource identifies whether a file change was made by the agent or externally.
type ChangeSource string

const (
	SourceAgent    ChangeSource = "agent"
	SourceExternal ChangeSource = "external"
)

// ChangeKind classifies the type of file modification.
type ChangeKind string

const (
	KindCreated  ChangeKind = "created"
	KindModified ChangeKind = "modified"
	KindDeleted  ChangeKind = "deleted"
)

// FileChange represents a single recorded file mutation entry in a session's change journal.
type FileChange struct {
	ID         string       `json:"id"`
	Path       string       `json:"path"`
	Source     ChangeSource `json:"source"`
	Tool       string       `json:"tool,omitempty"`
	Kind       ChangeKind   `json:"kind"`
	BeforeHash string       `json:"before_hash,omitempty"`
	AfterHash  string       `json:"after_hash,omitempty"`
	TurnID     string       `json:"turn_id,omitempty"`
	MessageID  string       `json:"message_id,omitempty"`
	ToolCallID string       `json:"tool_call_id,omitempty"`
	Additions  int          `json:"additions,omitempty"`
	Deletions  int          `json:"deletions,omitempty"`
	Timestamp  time.Time    `json:"timestamp"`
}

// DiffResponse carries the unified diff for a specific journal entry.
type DiffResponse struct {
	Path    string `json:"path"`
	EntryID string `json:"entry_id"`
	Diff    string `json:"diff"`
}

// RevertRequest specifies the scope of a file or session revert operation.
type RevertRequest struct {
	Path    string `json:"path,omitempty"`
	EntryID string `json:"entry_id,omitempty"`
	Before  bool   `json:"before,omitempty"`
	Force   bool   `json:"force,omitempty"`
}

// MCPStatus describes the live connection state of an MCP server.
type MCPStatus string

const (
	// MCPStatusDisconnected marks a server with no active connection.
	MCPStatusDisconnected MCPStatus = "disconnected"
	// MCPStatusConnecting marks a server whose connection is in progress.
	MCPStatusConnecting MCPStatus = "connecting"
	// MCPStatusConnected marks a server that is connected and discovered.
	MCPStatusConnected MCPStatus = "connected"
	// MCPStatusAuthRequired marks a server waiting for the user to finish an
	// interactive authorization flow.
	MCPStatusAuthRequired MCPStatus = "auth_required"
	// MCPStatusError marks a server whose last connection or discovery failed.
	MCPStatusError MCPStatus = "error"
)

// MCPToolSummary describes one tool discovered on an MCP server.
type MCPToolSummary struct {
	Name         string         `json:"name"`    // "<server>__<rawName>"
	RawName      string         `json:"rawName"` // "<rawName>"
	Description  string         `json:"description,omitempty"`
	Allowed      bool           `json:"allowed"`
	Annotations  ToolAnnotation `json:"annotations"`
	InputSchema  map[string]any `json:"inputSchema,omitempty"`
	OutputSchema map[string]any `json:"outputSchema,omitempty"`
}

// MCPPromptArgument describes one argument accepted by an MCP prompt.
type MCPPromptArgument struct {
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// MCPPromptSummary describes one prompt discovered on an MCP server.
type MCPPromptSummary struct {
	Name        string              `json:"name"`
	Title       string              `json:"title,omitempty"`
	Description string              `json:"description,omitempty"`
	Arguments   []MCPPromptArgument `json:"arguments,omitempty"`
	Allowed     bool                `json:"allowed"`
}

// MCPResourceSummary describes one resource discovered on an MCP server.
type MCPResourceSummary struct {
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
	URI         string `json:"uri"`
	Description string `json:"description,omitempty"`
	MIMEType    string `json:"mimeType,omitempty"`
	Size        int64  `json:"size,omitempty"`
	Allowed     bool   `json:"allowed"`
}

// MCPServer describes a configured MCP server, its resolved transport and
// authentication configuration, its live connection state, and everything
// discovered on it. Items excluded by the server manifest's policies are still
// listed with Allowed set to false so callers can show what a server offers and
// why an item is restricted.
type MCPServer struct {
	Name        string               `json:"name"`
	DisplayName string               `json:"displayName,omitempty"`
	Description string               `json:"description,omitempty"`
	Namespace   string               `json:"namespace,omitempty"`
	Scope       string               `json:"scope,omitempty"`
	Path        string               `json:"path,omitempty"`
	Transport   string               `json:"transport"`
	Command     []string             `json:"command,omitempty"`
	Endpoint    string               `json:"endpoint,omitempty"`
	AuthType    string               `json:"authType,omitempty"`
	Status      MCPStatus            `json:"status"`
	AuthURL     string               `json:"authUrl,omitempty"`
	Error       string               `json:"error,omitempty"`
	Tools       []MCPToolSummary     `json:"tools,omitempty"`
	Prompts     []MCPPromptSummary   `json:"prompts,omitempty"`
	Resources   []MCPResourceSummary `json:"resources,omitempty"`
}

// MCPAuthResponse reports how a user should authorize an MCP server.
type MCPAuthResponse struct {
	AuthURL string    `json:"authUrl,omitempty"`
	Status  MCPStatus `json:"status"`
}

// MCPAuthCallbackRequest carries a redirect URL the user pasted by hand so a
// waiting authorization flow can complete without the browser ever reaching the
// daemon's loopback listener.
type MCPAuthCallbackRequest struct {
	CallbackURL string `json:"callbackUrl"`
}

// MCPPromptRequest renders a prompt discovered on an MCP server. Prompts
// blocked by the server manifest's policies are rejected unless the caller is a
// user explicitly authorizing one prohibited use with AllowProhibited.
type MCPPromptRequest struct {
	Args            map[string]string `json:"args,omitempty"`
	AllowProhibited bool              `json:"allowProhibited,omitempty"`
}

// MCPPromptMessage is one rendered message of an MCP prompt.
type MCPPromptMessage struct {
	Role Role   `json:"role"`
	Text string `json:"text,omitempty"`
}

// MCPPromptResponse carries the rendered messages of an MCP prompt.
type MCPPromptResponse struct {
	Description string             `json:"description,omitempty"`
	Messages    []MCPPromptMessage `json:"messages,omitempty"`
}

// MCPResourceRequest reads one resource from an MCP server. Resources blocked by
// the server manifest's policies are rejected unless the caller is a user
// explicitly authorizing one prohibited read with AllowProhibited.
type MCPResourceRequest struct {
	URI             string `json:"uri"`
	AllowProhibited bool   `json:"allowProhibited,omitempty"`
}

// MCPResourceResponse carries the text content of a read MCP resource.
type MCPResourceResponse struct {
	URI      string `json:"uri"`
	MIMEType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
}

// MCP policy item kinds accepted by MCPAllowPolicyRequest and named in policy
// rejection messages.
const (
	MCPAllowPolicyKindTool     = "tool"
	MCPAllowPolicyKindPrompt   = "prompt"
	MCPAllowPolicyKindResource = "resource"
)

// MCPAllowPolicyRequest names one MCP item to permit in the server's manifest.
// Kind is one of [MCPAllowPolicyKindTool], [MCPAllowPolicyKindPrompt], or
// [MCPAllowPolicyKindResource], and Value is the raw tool name, prompt name, or
// resource URI.
type MCPAllowPolicyRequest struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}
