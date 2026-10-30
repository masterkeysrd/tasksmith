package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
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
}

// ContextBreakdown categorizes token usage within the context window.
type ContextBreakdown struct {
	SystemPrompt   int `json:"system_prompt"`
	ContextFiles   int `json:"context_files,omitempty"`
	SystemTools    int `json:"system_tools,omitempty"`
	CustomTools    int `json:"custom_tools,omitempty"`
	MCPTools       int `json:"mcp_tools,omitempty"`
	Skills         int `json:"skills,omitempty"`
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
	Models      []string         `json:"models,omitempty"`
	Thinking    *ThinkingSetting `json:"thinking,omitempty"`
	Context     ContextBreakdown `json:"context"`
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
	ArgumentHint string     `json:"argument_hint,omitempty"`
	Arguments    []Argument `json:"arguments,omitempty"`
	Models       []string   `json:"models,omitempty"`
	Tools        []string   `json:"tools,omitempty"`
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
	Namespace    string     `json:"namespace,omitempty"`
	Scope        string     `json:"scope,omitempty"`
	Instructions string     `json:"instructions,omitempty"`
}

type ThinkingValue struct {
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
}

type ThinkingCapability struct {
	Type    string          `json:"type"`
	Values  []ThinkingValue `json:"values,omitempty"`
	Default string          `json:"default,omitempty"`
	Min     int             `json:"min,omitempty"`
	Max     int             `json:"max,omitempty"`
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
	QueueKindMessage QueueKind = "message"
	QueueKindCommand QueueKind = "command"
	QueueKindCompact QueueKind = "compact"
)

type QueueItemPayload interface {
	QueueKind() QueueKind
}

type QueueMessagePayload struct {
	Message Message `json:"message"`
}

func (QueueMessagePayload) QueueKind() QueueKind { return QueueKindMessage }

type QueueCommandPayload struct {
	Command string `json:"command"`
}

func (QueueCommandPayload) QueueKind() QueueKind { return QueueKindCommand }

type QueueCompactPayload struct {
	Focus string `json:"focus,omitempty"`
}

func (QueueCompactPayload) QueueKind() QueueKind { return QueueKindCompact }

type QueueItem struct {
	ID        string           `json:"id"`
	Kind      QueueKind        `json:"kind"`
	Payload   QueueItemPayload `json:"payload,omitempty"`
	CreatedAt time.Time        `json:"created_at"`
}

func (q *QueueItem) UnmarshalJSON(data []byte) error {
	type wire struct {
		ID        string          `json:"id"`
		Kind      QueueKind       `json:"kind"`
		Payload   json.RawMessage `json:"payload,omitempty"`
		CreatedAt time.Time       `json:"created_at"`
	}
	var w wire
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	q.ID = w.ID
	q.Kind = w.Kind
	q.CreatedAt = w.CreatedAt
	q.Payload = nil

	raw := bytes.TrimSpace(w.Payload)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil
	}

	switch w.Kind {
	case QueueKindMessage:
		var p QueueMessagePayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return fmt.Errorf("api: unmarshal queue message payload: %w", err)
		}
		q.Payload = p
	case QueueKindCommand:
		var p QueueCommandPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return fmt.Errorf("api: unmarshal queue command payload: %w", err)
		}
		q.Payload = p
	case QueueKindCompact:
		var p QueueCompactPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return fmt.Errorf("api: unmarshal queue compact payload: %w", err)
		}
		q.Payload = p
	default:
		return fmt.Errorf("api: unknown queue kind %q", w.Kind)
	}

	return nil
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
	ID       string              `json:"id,omitempty"`
	Content  string              `json:"content,omitempty"`
	Blocks   []Block             `json:"blocks,omitempty"`
	Command  string              `json:"command,omitempty"`
	Decision *PermissionDecision `json:"decision,omitempty"`
	Question *QuestionResponse   `json:"question,omitempty"`
	Compact  *CompactRequest     `json:"compact,omitempty"`
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

// SafetyLevel classifies the risk profile of a planned tool operation.
type SafetyLevel string

const (
	SafetySafe    SafetyLevel = "safe"
	SafetyOutside SafetyLevel = "outside"
	SafetyUnknown SafetyLevel = "unknown"
	SafetyUnsafe  SafetyLevel = "unsafe"
)

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

// Permissions holds allow and deny rules for a single scope.
type Permissions struct {
	Allow []string `json:"allow,omitempty"`
	Deny  []string `json:"deny,omitempty"`
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

// FileChange represents a single recorded file mutation entry in a session's change journal.
type FileChange struct {
	ID         string    `json:"id"`
	Path       string    `json:"path"`
	Source     string    `json:"source"`
	Tool       string    `json:"tool,omitempty"`
	Kind       string    `json:"kind"`
	BeforeHash string    `json:"before_hash,omitempty"`
	AfterHash  string    `json:"after_hash,omitempty"`
	TurnID     string    `json:"turn_id,omitempty"`
	MessageID  string    `json:"message_id,omitempty"`
	ToolCallID string    `json:"tool_call_id,omitempty"`
	Additions  int       `json:"additions,omitempty"`
	Deletions  int       `json:"deletions,omitempty"`
	Timestamp  time.Time `json:"timestamp"`
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
