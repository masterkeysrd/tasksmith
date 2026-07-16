package permissions

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/masterkeysrd/tasksmith/internal/core/preview"
)

// GenericPermissionHandler evaluates permissions for tools that do not have custom handlers.
type GenericPermissionHandler struct {
	ToolName    string
	IsDangerous bool
	IsOpenWorld bool
	IsReadOnly  bool
}

// GetPermissionGroup returns the generic tool name as its group.
func (h *GenericPermissionHandler) GetPermissionGroup() string {
	return h.ToolName
}

// Evaluate performs the standard evaluation based on tool settings and permission mode.
func (h *GenericPermissionHandler) Evaluate(ctx context.Context, req ToolCallRequest, mode PermissionMode, grants []Permission) EvaluationResult {
	if mode == ModeStrict {
		return EvaluationResult{
			State: StateRequiresAuth,
			Hints: nil,
		}
	}

	if state, found := EvaluateGrants(grants, func(p Permission) bool {
		return p.Target == "*"
	}); found {
		return EvaluationResult{State: state}
	}

	switch mode {
	case ModeAuto:
		if h.IsDangerous || h.IsOpenWorld {
			return EvaluationResult{
				State: StateRequiresAuth,
				Hints: []string{fmt.Sprintf("Auto mode prompt: tool %q is sensitive", h.ToolName)},
			}
		}
		return EvaluationResult{State: StateAuto}
	case ModeStrict:
		return EvaluationResult{
			State: StateRequiresAuth,
			Hints: nil,
		}

	case ModeDefault:
		fallthrough
	default:
		if h.IsReadOnly && !h.IsOpenWorld {
			return EvaluationResult{State: StateExplicitAllow}
		}
		return EvaluationResult{
			State: StateRequiresAuth,
			Hints: []string{fmt.Sprintf("Authorization required for tool %q", h.ToolName)},
		}
	}
}

// GetOptions returns generic permission options.
func (h *GenericPermissionHandler) GetOptions(req ToolCallRequest) []PermissionOption {
	return []PermissionOption{
		{
			Label:       fmt.Sprintf("Allow %s globally", h.ToolName),
			Target:      "*",
			MatchMethod: "wildcard",
			Action:      ActionAllow,
		},
	}
}

// GetPreview generates a generic arguments preview.
func (h *GenericPermissionHandler) GetPreview(ctx context.Context, req ToolCallRequest) (preview.ToolPreview, error) {
	prettyArgs, err := json.MarshalIndent(req.Args, "", "  ")
	if err != nil {
		return preview.DefaultTextPreview{Text: fmt.Sprintf("Execute tool %q with arguments: %v", h.ToolName, req.Args)}, nil
	}
	return preview.DefaultTextPreview{Text: fmt.Sprintf("Execute tool %q with arguments:\n%s", h.ToolName, string(prettyArgs))}, nil
}

// EvaluateGrants checks if any saved grants match using the provided match function.
func EvaluateGrants(grants []Permission, match func(Permission) bool) (PermissionState, bool) {
	for _, grant := range grants {
		if match(grant) {
			if grant.Action == ActionAllow {
				return StateExplicitAllow, true
			}
			return StateExplicitDeny, true
		}
	}
	return "", false
}

// EvaluateToolCall encapsulates all permission check logic, selecting the correct handler or falling back to a generic one,
// and returns the decision (State, finalArgs, Hints, AuthorizationRequest).
func EvaluateToolCall(
	ctx context.Context,
	pm PermissionManager,
	req ToolCallRequest,
	decision *AuthorizationDecision,
) (PermissionState, map[string]any, []string, *AuthorizationRequest) {
	if decision != nil {
		finalArgs := req.Args
		if decision.ModifiedPayload != nil {
			finalArgs = decision.ModifiedPayload
		}

		handler, _ := GetHandler(req.ToolName)
		if handler == nil {
			handler = &GenericPermissionHandler{
				ToolName:    req.ToolName,
				IsDangerous: req.IsDangerous,
				IsOpenWorld: req.IsOpenWorld,
				IsReadOnly:  req.IsReadOnly,
			}
		}

		if decision.Approved {
			desc := req.UserHint
			if desc == "" {
				desc = req.Description
			}
			if idx := strings.Index(desc, "\n"); idx != -1 {
				desc = strings.TrimSpace(desc[:idx])
			}

			var grantRequests []PermissionGrantRequest
			if multiHandler, ok := handler.(interface {
				GetGrantRequests(ctx context.Context, req ToolCallRequest, mode PermissionMode, grants []Permission) []PermissionGrantRequest
			}); ok {
				grantRequests = multiHandler.GetGrantRequests(ctx, req, pm.GetMode(ctx), pm.GetGrants(ctx, handler.GetPermissionGroup()))
			} else {
				grantRequests = []PermissionGrantRequest{
					{
						ID:          "default",
						Description: desc,
						Options:     handler.GetOptions(req),
					},
				}
			}

			for _, dec := range decision.GrantDecisions {
				if dec.Scope == ScopeOnce {
					continue
				}

				matchMethod := "exact"
				if dec.SelectedTarget == "*" {
					matchMethod = "wildcard"
				}
				action := ActionAllow

				found := false
				for _, gr := range grantRequests {
					if gr.ID == dec.RequestID {
						for _, opt := range gr.Options {
							if opt.Target == dec.SelectedTarget {
								matchMethod = opt.MatchMethod
								action = opt.Action
								found = true
								break
							}
						}
					}
					if found {
						break
					}
				}

				perm := Permission{
					Group:            handler.GetPermissionGroup(),
					Target:           dec.SelectedTarget,
					MatchMethod:      matchMethod,
					Action:           action,
					AllowedDirectory: dec.AllowedDirectory,
				}
				_ = pm.SavePermission(ctx, dec.Scope, perm)
			}
			return StateExplicitAllow, finalArgs, nil, nil
		} else {
			if len(decision.GrantDecisions) > 0 {
				for _, dec := range decision.GrantDecisions {
					if dec.Scope == ScopeOnce {
						continue
					}
					matchMethod := "exact"
					if dec.SelectedTarget == "*" {
						matchMethod = "wildcard"
					}
					perm := Permission{
						Group:            handler.GetPermissionGroup(),
						Target:           dec.SelectedTarget,
						MatchMethod:      matchMethod,
						Action:           ActionDeny,
						AllowedDirectory: dec.AllowedDirectory,
					}
					_ = pm.SavePermission(ctx, dec.Scope, perm)
				}
			}
			return StateExplicitDeny, finalArgs, nil, nil
		}
	}

	mode := pm.GetMode(ctx)

	handler, ok := GetHandler(req.ToolName)
	if !ok {
		handler = &GenericPermissionHandler{
			ToolName:    req.ToolName,
			IsDangerous: req.IsDangerous,
			IsOpenWorld: req.IsOpenWorld,
			IsReadOnly:  req.IsReadOnly,
		}
	}

	groupName := handler.GetPermissionGroup()
	grants := pm.GetGrants(ctx, groupName)
	if mode == ModeStrict {
		grants = nil
	}

	res := handler.Evaluate(ctx, req, mode, grants)
	if res.State == StateRequiresAuth {
		var preview preview.ToolPreview
		if resPreview, err := handler.GetPreview(ctx, req); err == nil {
			preview = resPreview
		}

		desc := req.UserHint
		if desc == "" {
			desc = req.Description
		}

		// Ensure we only show the first line of the description/hint
		if idx := strings.Index(desc, "\n"); idx != -1 {
			desc = strings.TrimSpace(desc[:idx])
		}

		// Determine allowed scopes based on mode
		allowedScopes := []PermissionScope{ScopeOnce, ScopeSession, ScopeWorkspace, ScopeGlobal}
		if mode == ModeStrict {
			allowedScopes = []PermissionScope{ScopeOnce}
		}

		var grantRequests []PermissionGrantRequest
		if multiHandler, ok := handler.(interface {
			GetGrantRequests(ctx context.Context, req ToolCallRequest, mode PermissionMode, grants []Permission) []PermissionGrantRequest
		}); ok {
			grantRequests = multiHandler.GetGrantRequests(ctx, req, mode, grants)
			for i := range grantRequests {
				if len(grantRequests[i].AllowedScopes) == 0 {
					grantRequests[i].AllowedScopes = allowedScopes
				}
			}
		} else {
			grantRequests = []PermissionGrantRequest{
				{
					ID:            "default",
					Description:   getFallbackActionDescription(req),
					Options:       handler.GetOptions(req),
					AllowedScopes: allowedScopes,
				},
			}
		}

		// Adapt options based on mode (e.g. Strict Mode omits "Allow Always" or "Allow Globally" / wildcard targets)
		if mode == ModeStrict {
			for i := range grantRequests {
				var filteredOpts []PermissionOption
				for _, opt := range grantRequests[i].Options {
					if opt.Target == "*" || opt.MatchMethod == "wildcard" {
						continue
					}
					filteredOpts = append(filteredOpts, opt)
				}
				grantRequests[i].Options = filteredOpts
			}
		}

		// Adapt hints based on mode
		var hints []string
		if mode == ModeStrict {
			hints = append(hints, "Strict Mode is active. Broad grants are ignored.")
		} else if mode == ModeAuto && (req.IsDangerous || req.IsOpenWorld) {
			hints = append(hints, "Auto Mode Alert: Intercepted a dangerous action.")
		}
		hints = append(hints, res.Hints...)

		return StateRequiresAuth, req.Args, hints, &AuthorizationRequest{
			ToolName:      req.ToolName,
			Description:   desc,
			Payload:       req.Args,
			Preview:       preview,
			SystemHints:   hints,
			GrantRequests: grantRequests,
		}
	}
	return res.State, req.Args, nil, nil
}

func getFallbackActionDescription(req ToolCallRequest) string {
	switch req.ToolName {
	case "edit_file", "write_to_file", "edit", "multi_edit":
		if path, ok := req.Args["path"].(string); ok {
			return fmt.Sprintf("Modify file: %s", path)
		}
		return "Modify file"
	case "write":
		if path, ok := req.Args["path"].(string); ok {
			return fmt.Sprintf("Write: %s", path)
		}
		return "Write file"
	case "read_file", "view_file", "view":
		if path, ok := req.Args["path"].(string); ok {
			return fmt.Sprintf("Read file: %s", path)
		}
		if path, ok := req.Args["AbsolutePath"].(string); ok {
			return fmt.Sprintf("Read file: %s", path)
		}
		return "Read file"
	case "grep_search", "grep":
		if path, ok := req.Args["SearchPath"].(string); ok {
			return fmt.Sprintf("Search path: %s", path)
		}
		if path, ok := req.Args["path"].(string); ok {
			return fmt.Sprintf("Search path: %s", path)
		}
		return "Search files"
	case "list_dir", "ls":
		if path, ok := req.Args["DirectoryPath"].(string); ok {
			return fmt.Sprintf("List directory: %s", path)
		}
		if path, ok := req.Args["path"].(string); ok {
			return fmt.Sprintf("List directory: %s", path)
		}
		return "List directory"
	case "glob":
		if pattern, ok := req.Args["pattern"].(string); ok {
			return fmt.Sprintf("Glob pattern: %s", pattern)
		}
		return "Glob files"
	case "tasks":
		action, _ := req.Args["action"].(string)
		taskId, _ := req.Args["taskId"].(string)
		switch action {
		case "list":
			return "List background tasks"
		case "status":
			if taskId != "" {
				return fmt.Sprintf("Retrieve status of task %s", taskId)
			}
			return "Retrieve task status"
		case "kill":
			if taskId != "" {
				return fmt.Sprintf("Terminate task %s", taskId)
			}
			return "Terminate task"
		case "send_input":
			if taskId != "" {
				return fmt.Sprintf("Send input to task %s", taskId)
			}
			return "Send input to task"
		default:
			if action != "" {
				if taskId != "" {
					return fmt.Sprintf("Perform task action %q on task %s", action, taskId)
				}
				return fmt.Sprintf("Perform task action %q", action)
			}
			return "Manage background tasks"
		}
	default:
		return fmt.Sprintf("Call tool: %s", req.ToolName)
	}
}
