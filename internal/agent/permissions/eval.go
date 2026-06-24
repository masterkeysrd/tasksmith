package permissions

import (
	"context"
	"encoding/json"
	"fmt"
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
				Hints: []string{fmt.Sprintf("⚠️ Auto mode prompt: tool %q is sensitive", h.ToolName)},
			}
		}
		return EvaluationResult{State: StateExplicitAllow}

	case ModeStrict:
		return EvaluationResult{
			State: StateRequiresAuth,
			Hints: []string{fmt.Sprintf("🔒 Strict mode: authorization required for %q", h.ToolName)},
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
func (h *GenericPermissionHandler) GetPreview(ctx context.Context, req ToolCallRequest) (string, error) {
	prettyArgs, err := json.MarshalIndent(req.Args, "", "  ")
	if err != nil {
		return fmt.Sprintf("Execute tool %q with arguments: %v", h.ToolName, req.Args), nil
	}
	return fmt.Sprintf("Execute tool %q with arguments:\n%s", h.ToolName, string(prettyArgs)), nil
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
		if decision.Approved {
			if decision.Scope != ScopeOnce {
				matchMethod := "exact"
				if decision.SelectedTarget == "*" {
					matchMethod = "wildcard"
				}
				action := ActionAllow

				handler, _ := GetHandler(req.ToolName)
				if handler == nil {
					handler = &GenericPermissionHandler{
						ToolName:    req.ToolName,
						IsDangerous: req.IsDangerous,
						IsOpenWorld: req.IsOpenWorld,
						IsReadOnly:  req.IsReadOnly,
					}
				}

				options := handler.GetOptions(req)
				for _, opt := range options {
					if opt.Target == decision.SelectedTarget {
						matchMethod = opt.MatchMethod
						action = opt.Action
						break
					}
				}

				perm := Permission{
					Group:       handler.GetPermissionGroup(),
					Target:      decision.SelectedTarget,
					MatchMethod: matchMethod,
					Action:      action,
				}
				_ = pm.SavePermission(ctx, decision.Scope, perm)
			}
			return StateExplicitAllow, finalArgs, nil, nil
		} else {
			if decision.Scope != ScopeOnce {
				matchMethod := "exact"
				if decision.SelectedTarget == "*" {
					matchMethod = "wildcard"
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

				perm := Permission{
					Group:       handler.GetPermissionGroup(),
					Target:      decision.SelectedTarget,
					MatchMethod: matchMethod,
					Action:      ActionDeny,
				}
				_ = pm.SavePermission(ctx, decision.Scope, perm)
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

	res := handler.Evaluate(ctx, req, mode, grants)
	if res.State == StateRequiresAuth {
		var preview string
		if resPreview, err := handler.GetPreview(ctx, req); err == nil {
			preview = resPreview
		}
		return StateRequiresAuth, req.Args, res.Hints, &AuthorizationRequest{
			ToolName:    req.ToolName,
			Description: req.Description,
			Payload:     req.Args,
			Preview:     preview,
			SystemHints: res.Hints,
			Options:     handler.GetOptions(req),
		}
	}
	return res.State, req.Args, nil, nil
}
