package permissions

import (
	"context"
	"fmt"
	"sync"

	"github.com/masterkeysrd/tasksmith/internal/core/preview"
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

// PermissionGrantRequest represents a single requirement within a tool call.
type PermissionGrantRequest struct {
	ID               string             `json:"id"`                // Unique ID for this specific grant request
	Description      string             `json:"description"`       // e.g., "Permission required for: git commit"
	Options          []PermissionOption `json:"options"`           // Granular options (exact, prefix, wildcard)
	DirectoryOptions []PermissionOption `json:"directory_options"` // Optional directory constraints (Restrict vs Anywhere)
	AllowedScopes    []PermissionScope  `json:"allowed_scopes"`    // Scopes allowed for this specific grant request
}

// AuthorizationRequest is returned by tools and saved to PendingAuthorizations in the graph state.
type AuthorizationRequest struct {
	ToolCallID    string                   `json:"tool_call_id"`
	ToolName      string                   `json:"tool_name"`
	Description   string                   `json:"description"`
	Payload       map[string]any           `json:"payload"`
	Preview       any                      `json:"preview,omitempty"`
	SystemHints   []string                 `json:"system_hints,omitempty"`
	GrantRequests []PermissionGrantRequest `json:"grant_requests"` // Replaces flat []PermissionOption
}

type GrantDecision struct {
	RequestID        string          `json:"request_id"`
	SelectedTarget   string          `json:"selected_target"`
	AllowedDirectory string          `json:"allowed_directory"`
	Scope            PermissionScope `json:"scope"`
}

// AuthorizationDecision is what the UI returns to the graph to resume execution.
type AuthorizationDecision struct {
	ToolCallID      string          `json:"tool_call_id"`
	Approved        bool            `json:"approved"` // Overall approval; false aborts the execution
	CancelExecution bool            `json:"cancel_execution,omitempty"`
	Scope           PermissionScope `json:"scope"`
	GrantDecisions  []GrantDecision `json:"grant_decisions,omitempty"`
	ModifiedPayload map[string]any  `json:"modified_payload,omitempty"`
	Reason          string          `json:"reason,omitempty"`
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
	Group            string           `json:"group"`
	Target           string           `json:"target"`
	MatchMethod      string           `json:"match_method"`      // "exact", "prefix", "path", "wildcard"
	Action           PermissionAction `json:"action"`            // "allow" or "deny"
	AllowedDirectory string           `json:"allowed_directory"` // e.g., "/var/www/frontend", or "*" for anywhere
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

	// GetPreview returns a generic structured preview for the action.
	// The orchestrator only calls this if Evaluate returns StateRequiresAuth.
	GetPreview(ctx context.Context, req ToolCallRequest) (preview.ToolPreview, error)
}

// PermissionManager handles reading and storing permissions across scopes.
type PermissionManager interface {
	// GetGrants retrieves all saved permissions for a specific group across all active scopes.
	GetGrants(ctx context.Context, group string) []Permission

	// GetMode returns the current operating mode of the permission system.
	GetMode(ctx context.Context) PermissionMode

	// SavePermission persists a new grant/deny to the specified scope.
	SavePermission(ctx context.Context, scope PermissionScope, perm Permission) error

	// GetAllPermissions retrieves all stored permissions across all active scopes.
	GetAllPermissions(ctx context.Context) (map[PermissionScope][]Permission, error)

	// DeletePermission removes the specified permission from the given scope.
	DeletePermission(ctx context.Context, scope PermissionScope, perm Permission) error
}

// ToolCallRequest represents a request to evaluate permissions for a tool call.
type ToolCallRequest struct {
	ToolName    string
	Args        map[string]any
	Description string
	UserHint    string
	IsDangerous bool
	IsOpenWorld bool
	IsReadOnly  bool
}
