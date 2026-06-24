package permissions

import (
	"context"
	"fmt"
	"sync"
)

var (
	registry   = make(map[string]ToolPermissionHandler)
	registryMu sync.RWMutex
)

// RegisterHandler registers a tool permission handler for a tool.
func RegisterHandler(toolName string, handler ToolPermissionHandler) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[toolName] = handler
}

// GetHandler retrieves a registered tool permission handler.
func GetHandler(toolName string) (ToolPermissionHandler, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	h, ok := registry[toolName]
	return h, ok
}

type contextKey string

const workspaceCWDKey contextKey = "workspace_cwd"

// ContextWithWorkspaceCWD injects the workspace CWD into the context.
func ContextWithWorkspaceCWD(ctx context.Context, cwd string) context.Context {
	return context.WithValue(ctx, workspaceCWDKey, cwd)
}

// GetWorkspaceCWD retrieves the workspace CWD from the context.
func GetWorkspaceCWD(ctx context.Context) string {
	if val, ok := ctx.Value(workspaceCWDKey).(string); ok {
		return val
	}
	return ""
}

// PermissionScope defines where the granted permission is stored.
type PermissionScope string

const (
	// ScopeSession is stored in ~/.local/share/tasksmith/sessions/<session-id>/permissions.json
	ScopeSession PermissionScope = "session"
	// ScopeWorkspace is stored in ~/.local/share/tasksmith/<workspace-id>/permissions.json
	ScopeWorkspace PermissionScope = "workspace"
	// ScopeGlobal is stored in ~/.local/share/tasksmith/permissions.json
	ScopeGlobal PermissionScope = "global"
	// ScopeOnce is a special scope indicating a one-time approval, not persisted.
	ScopeOnce PermissionScope = "once"
)

// PermissionMode dictates how the PermissionManager behaves when evaluating requests.
type PermissionMode string

const (
	ModeAuto    PermissionMode = "auto"    // Auto-approve everything (YOLO mode)
	ModeDefault PermissionMode = "default" // Follow granted rules, prompt otherwise
	ModeStrict  PermissionMode = "strict"  // Always prompt for sensitive tools, ignore broad grants
)

// PermissionOption is a specific rule the tool proposes to the user.
type PermissionOption struct {
	Label       string           `json:"label"`        // e.g., "Allow specific command"
	Target      string           `json:"target"`       // e.g., "npm install"
	MatchMethod string           `json:"match_method"` // e.g., "exact", "prefix", "path", "wildcard"
	Action      PermissionAction `json:"action"`       // "allow" or "deny"
	IsDanger    bool             `json:"is_danger"`    // UI hint for dangerous options
}

// AuthorizationRequest is returned by tools and saved to PendingAuthorizations in the graph state.
type AuthorizationRequest struct {
	ToolCallID  string         `json:"tool_call_id"`
	ToolName    string         `json:"tool_name"`
	Description string         `json:"description"`
	Payload     map[string]any `json:"payload"`
	// Preview contains a human-readable preview of the action (e.g. a Unified Diff for file edits)
	Preview     string             `json:"preview,omitempty"`
	SystemHints []string           `json:"system_hints,omitempty"`
	Options     []PermissionOption `json:"options"`
}

// AuthorizationDecision is what the UI returns to the graph to resume execution.
type AuthorizationDecision struct {
	ToolCallID     string          `json:"tool_call_id"`
	Approved       bool            `json:"approved"`
	Scope          PermissionScope `json:"scope"`
	SelectedTarget string          `json:"selected_target,omitempty"`
	// ModifiedPayload contains the tool arguments if the user edited them.
	// If nil, the graph uses the original arguments.
	ModifiedPayload map[string]any `json:"modified_payload,omitempty"`
}

// AuthorizationRequiredError is the error returned by tools when they lack permission.
type AuthorizationRequiredError struct {
	Request AuthorizationRequest
}

func (e *AuthorizationRequiredError) Error() string {
	return fmt.Sprintf("authorization required for tool %q", e.Request.ToolName)
}

// PermissionAction defines whether the grant is allowing or denying the action.
type PermissionAction string

const (
	ActionAllow PermissionAction = "allow"
	ActionDeny  PermissionAction = "deny"
)

// Permission represents an active grant or block.
type Permission struct {
	Group       string           `json:"group"`
	Target      string           `json:"target"`
	MatchMethod string           `json:"match_method"` // "exact", "prefix", "path", "wildcard"
	Action      PermissionAction `json:"action"`       // "allow" or "deny"
}

// PermissionState is returned by the PermissionManager to tell the tool how to proceed.
type PermissionState string

const (
	StateExplicitAllow PermissionState = "explicit_allow"
	StateExplicitDeny  PermissionState = "explicit_deny"
	StateAuto          PermissionState = "auto"          // Tool must run its own safety filters
	StateRequiresAuth  PermissionState = "requires_auth" // Force a prompt
)

// EvaluationResult holds the outcome of a permission check.
type EvaluationResult struct {
	State PermissionState
	Hints []string // e.g., "⚠️ Auto mode blocked: touching .git is dangerous"
}

// ToolPermissionHandler defines the domain-specific security rules for a single tool.
// These handlers are evaluated by the orchestrator BEFORE the tool is executed.
type ToolPermissionHandler interface {
	// GetPermissionGroup returns the abstract capability group this tool belongs to
	// (e.g. "write_file", "read_file", "command", "web"). Tools sharing a group share grants.
	GetPermissionGroup() string

	// Evaluate takes the tool request, the active mode, and the user's saved grants for this group,
	// and decides the final PermissionState. It returns Hints if it forces a prompt.
	Evaluate(ctx context.Context, req ToolCallRequest, mode PermissionMode, grants []Permission) EvaluationResult

	// GetOptions returns the granular UI choices (exact, prefix, wildcard)
	// to present if authorization is required.
	GetOptions(req ToolCallRequest) []PermissionOption

	// GetPreview returns a human-readable summary of the action (e.g., a unified diff).
	// The orchestrator only calls this if Evaluate returns StateRequiresAuth.
	GetPreview(ctx context.Context, req ToolCallRequest) (string, error)
}

// PermissionManager handles reading and storing permissions across scopes.
type PermissionManager interface {
	// GetGrants retrieves all saved permissions for a specific group across all active scopes.
	GetGrants(ctx context.Context, group string) []Permission

	// GetMode returns the current operating mode of the permission system.
	GetMode(ctx context.Context) PermissionMode

	// SavePermission persists a new grant/deny to the specified scope.
	SavePermission(ctx context.Context, scope PermissionScope, perm Permission) error
}

// ToolCallRequest represents a request to evaluate permissions for a tool call.
type ToolCallRequest struct {
	ToolName    string
	Args        map[string]any
	Description string
	IsDangerous bool
	IsOpenWorld bool
	IsReadOnly  bool
}
