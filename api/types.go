package api

import (
	"fmt"
	"time"
)

// HeaderCWD is the HTTP request header carrying the client's working directory.
const HeaderCWD = "X-TaskSmith-CWD"

// Workspace describes the resolved configuration metadata for a workspace directory.
type Workspace struct {
	Name         string `json:"name"`
	RootDir      string `json:"root_dir"`
	CWD          string `json:"cwd,omitempty"`
	DefaultAgent string `json:"default_agent"`
	DefaultModel string `json:"default_model,omitempty"`
}

// Agent describes an effective agent configuration in a workspace.
type Agent struct {
	Name        string   `json:"name"`
	DisplayName string   `json:"display_name,omitempty"`
	Description string   `json:"description,omitempty"`
	Models      []string `json:"models,omitempty"`
	Effort      string   `json:"effort,omitempty"`
}

// Model describes a resolved model and its capabilities from a workspace provider.
type Model struct {
	ID             string   `json:"id"`
	Provider       string   `json:"provider,omitempty"`
	Name           string   `json:"name"`
	Label          string   `json:"label,omitempty"`
	DefaultEffort  string   `json:"default_effort,omitempty"`
	AllowedEfforts []string `json:"allowed_efforts,omitempty"`
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
	StatusRunning           SessionStatus = "running"
	StatusWaitingPermission SessionStatus = "waiting_permission"
)

// Session represents a persistent conversational session managed by the daemon.
type Session struct {
	ID        string        `json:"id"`
	Title     string        `json:"title"`
	Agent     string        `json:"agent"`
	Model     string        `json:"model"`
	Status    SessionStatus `json:"status"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
}

// CreateSessionRequest holds optional parameters for creating a new session.
type CreateSessionRequest struct {
	Title string `json:"title,omitempty"`
	Agent string `json:"agent,omitempty"`
	Model string `json:"model,omitempty"`
}

// UpdateSessionRequest holds optional fields to update on an existing session.
type UpdateSessionRequest struct {
	Title string `json:"title,omitempty"`
	Agent string `json:"agent,omitempty"`
	Model string `json:"model,omitempty"`
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
	ID         string    `json:"id"`
	Role       Role      `json:"role"`
	Content    []Block   `json:"content,omitempty"`
	ToolCallID string    `json:"tool_call_id,omitempty"`
	Name       string    `json:"name,omitempty"`
	IsError    bool      `json:"is_error,omitempty"`
	CreatedAt  time.Time `json:"created_at,omitempty"`
}

// Chunk represents an incremental streaming assistant delta.
type Chunk struct {
	ID      string  `json:"id"`
	Content []Block `json:"content,omitempty"`
	Done    bool    `json:"done,omitempty"`
}

// SendRequest specifies the user message payload and whether the HTTP request
// should block until the turn finishes or pauses for permissions.
type SendRequest struct {
	Content string  `json:"content,omitempty"`
	Blocks  []Block `json:"blocks,omitempty"`
	Wait    bool    `json:"wait,omitempty"`
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
	RequestID    string       `json:"request_id,omitempty"`
	ToolCallID   string       `json:"tool_call_id,omitempty"`
	Kind         DecisionKind `json:"kind"`
	GrantPattern string       `json:"grant_pattern,omitempty"`
	Feedback     string       `json:"feedback,omitempty"`
	Wait         bool         `json:"wait,omitempty"`
}

// ModeResponse reports the active [PermissionMode] of a session.
type ModeResponse struct {
	Mode PermissionMode `json:"mode"`
}

// SetModeRequest updates the active [PermissionMode] of a session.
type SetModeRequest struct {
	Mode PermissionMode `json:"mode"`
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

// EventType identifies the kind of real-time Server-Sent Event emitted by the daemon.
type EventType string

const (
	EventMessageAdded        EventType = "message_added"
	EventChunk               EventType = "chunk"
	EventPermissionRequested EventType = "permission_requested"
	EventCompleted           EventType = "completed"
	EventError               EventType = "error"
)

// Event is the real-time notification payload streamed over `/v1/events`.
type Event struct {
	Type        EventType           `json:"type"`
	Workspace   string              `json:"workspace,omitempty"`
	SessionID   string              `json:"session_id,omitempty"`
	Message     *Message            `json:"message,omitempty"`
	Chunk       *Chunk              `json:"chunk,omitempty"`
	Permissions []PermissionRequest `json:"permissions,omitempty"`
	Error       string              `json:"error,omitempty"`
}
