package permissions

import (
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
	"github.com/masterkeysrd/warp"
)

// SessionManager manages permission evaluation, permission modes, and
// session-scoped permission rules.
type SessionManager interface {
	Mode() api.PermissionMode
	SetMode(mode api.PermissionMode)
	Permissions() api.Permissions
	Policies() *warp.Policies
	SetPolicies(p *warp.Policies)
	AgentPolicies() *warp.Policies
	SetAgentPolicies(p *warp.Policies)
	AddAllow(rule string) error
	AddDeny(rule string) error
	Clear() error
	ApplyDecision(req *api.PermissionRequest, decision api.PermissionDecision) error
	Evaluate(plan *tools.Plan) (tools.AccessDecision, *api.PermissionRequest, error)
}

type sessionManager struct {
	mu            sync.RWMutex
	manager       Manager
	ws            *workspace.Workspace
	mode          api.PermissionMode
	permissions   api.Permissions
	policies      *warp.Policies
	agentPolicies *warp.Policies
	onUpdate      func(api.Permissions) error
}

var _ SessionManager = (*sessionManager)(nil)

// NewSession constructs a detached [SessionManager] without an underlying root manager.
func NewSession(mode api.PermissionMode, initial api.Permissions, onUpdate func(api.Permissions) error) SessionManager {
	if mode == "" {
		mode = api.ModeDefault
	}
	return &sessionManager{
		mode:        mode,
		permissions: initial.Clone(),
		onUpdate:    onUpdate,
	}
}

// Mode returns the active [api.PermissionMode].
func (s *sessionManager) Mode() api.PermissionMode {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.mode == "" {
		return api.ModeDefault
	}
	return s.mode
}

// SetMode updates the active [api.PermissionMode].
func (s *sessionManager) SetMode(mode api.PermissionMode) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if mode == "" {
		mode = api.ModeDefault
	}
	s.mode = mode
}

// Permissions returns a deep copy of the active session [api.Permissions].
func (s *sessionManager) Permissions() api.Permissions {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.permissions.Clone()
}

// Policies returns the active workspace policies or override configured on s.
func (s *sessionManager) Policies() *warp.Policies {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.policies != nil {
		return s.policies
	}
	if s.ws != nil && s.ws.Catalog() != nil {
		return s.ws.Catalog().Policies()
	}
	return nil
}

// SetPolicies overrides the workspace policies evaluated by s.
func (s *sessionManager) SetPolicies(p *warp.Policies) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.policies = p
}

// AgentPolicies returns the active agent's tool policies (ceiling) configured on s.
func (s *sessionManager) AgentPolicies() *warp.Policies {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.agentPolicies
}

// SetAgentPolicies overrides the active agent's tool policies evaluated by s.
func (s *sessionManager) SetAgentPolicies(p *warp.Policies) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.agentPolicies = p
}

func (s *sessionManager) addRule(effect api.PermissionEffect, rule string) error {
	if _, _, err := ParseRule(rule); err != nil {
		return err
	}

	s.mu.Lock()
	switch effect {
	case api.EffectAllow:
		if slices.Contains(s.permissions.Allow, rule) {
			s.mu.Unlock()
			return nil
		}
		s.permissions.Allow = append(s.permissions.Allow, rule)
	case api.EffectDeny:
		if slices.Contains(s.permissions.Deny, rule) {
			s.mu.Unlock()
			return nil
		}
		s.permissions.Deny = append(s.permissions.Deny, rule)
	default:
		s.mu.Unlock()
		return fmt.Errorf("%w: invalid effect %q", ErrInvalidRule, effect)
	}
	updated := s.permissions.Clone()
	onUpdate := s.onUpdate
	s.mu.Unlock()

	if onUpdate != nil {
		return onUpdate(updated)
	}
	return nil
}

// AddAllow validates rule and adds it to the session allow rules.
func (s *sessionManager) AddAllow(rule string) error {
	return s.addRule(api.EffectAllow, rule)
}

// AddDeny validates rule and adds it to the session deny rules.
func (s *sessionManager) AddDeny(rule string) error {
	return s.addRule(api.EffectDeny, rule)
}

// Clear removes all session-scoped allow and deny rules.
func (s *sessionManager) Clear() error {
	s.mu.Lock()
	s.permissions = api.Permissions{}
	updated := s.permissions.Clone()
	onUpdate := s.onUpdate
	s.mu.Unlock()

	if onUpdate != nil {
		return onUpdate(updated)
	}
	return nil
}

// ApplyDecision processes a user's [api.PermissionDecision] for req.
func (s *sessionManager) ApplyDecision(req *api.PermissionRequest, decision api.PermissionDecision) error {
	switch decision.Kind {
	case api.DecisionAllowOnce:
		return nil
	case api.DecisionAllowSession:
		if req != nil {
			raw := strings.TrimSpace(decision.GrantPattern)
			var rule string
			if raw == "" {
				if len(req.GrantPatterns) > 0 {
					rule = req.GrantPatterns[0]
				} else if req.Target != "" {
					rule = FormatRule(req.ToolName, req.Target)
				} else {
					rule = FormatRule(req.ToolName, "*")
				}
			} else {
				if _, _, err := ParseRule(raw); err != nil {
					rule = FormatRule(req.ToolName, raw)
				} else {
					rule = raw
				}
			}
			scope := api.PermissionScope(strings.TrimSpace(string(decision.Scope)))
			if scope == "" {
				scope = api.ScopeSession
			}
			switch scope {
			case api.ScopeSession:
				if err := s.AddAllow(rule); err != nil {
					return err
				}
			case api.ScopeProject:
				if s.manager == nil {
					return ErrInvalidConfig
				}
				if err := s.manager.AddRule(api.ScopeProject, api.EffectAllow, rule); err != nil {
					return err
				}
			case api.ScopeGlobal:
				if s.manager == nil {
					return ErrInvalidConfig
				}
				if err := s.manager.AddRule(api.ScopeGlobal, api.EffectAllow, rule); err != nil {
					return err
				}
			default:
				return fmt.Errorf("%w: %q", ErrInvalidScope, decision.Scope)
			}
		}
		return nil
	case api.DecisionReject:
		if strings.TrimSpace(decision.Feedback) != "" {
			return fmt.Errorf("%w: %s", tools.ErrPermissionDenied, strings.TrimSpace(decision.Feedback))
		}
		return tools.ErrPermissionDenied
	default:
		return fmt.Errorf("%w: unknown decision kind %q", tools.ErrPermissionDenied, decision.Kind)
	}
}

// Evaluate checks plan against workspace policies, active permission mode,
// safety classification, and deny/allow permission rules.
func (s *sessionManager) Evaluate(plan *tools.Plan) (tools.AccessDecision, *api.PermissionRequest, error) {
	if plan == nil {
		return tools.AccessDeny, nil, fmt.Errorf("%w: plan cannot be nil", tools.ErrPermissionDenied)
	}

	// 1. Evaluate the workspace and active-agent tool policy ceiling.
	policyAction := evaluatePolicy(plan, s.Policies(), s.AgentPolicies(), s.ws)
	if policyAction == warp.ToolPolicyDeny {
		return tools.AccessDeny, nil, nil
	}

	// 2. Load current rules for Session (in-memory), Project, and Global.
	sessPerms := s.Permissions()
	var projPerms, globPerms api.Permissions
	if s.manager != nil {
		var err error
		projPerms, err = s.manager.Get(api.ScopeProject)
		if err != nil {
			return tools.AccessDeny, nil, err
		}
		globPerms, err = s.manager.Get(api.ScopeGlobal)
		if err != nil {
			return tools.AccessDeny, nil, err
		}
	}

	// 3. Deny check first: if any deny rule matches, return AccessDeny.
	for _, r := range sessPerms.Deny {
		if MatchRule(r, plan) {
			return tools.AccessDeny, nil, nil
		}
	}
	for _, r := range projPerms.Deny {
		if MatchRule(r, plan) {
			return tools.AccessDeny, nil, nil
		}
	}
	for _, r := range globPerms.Deny {
		if MatchRule(r, plan) {
			return tools.AccessDeny, nil, nil
		}
	}

	// 4. Check mode and Allow rules.
	hasMatchingAllow := func(p *tools.Plan) bool {
		for _, r := range sessPerms.Allow {
			if MatchRule(r, p) {
				return true
			}
		}
		for _, r := range projPerms.Allow {
			if MatchRule(r, p) {
				return true
			}
		}
		for _, r := range globPerms.Allow {
			if MatchRule(r, p) {
				return true
			}
		}
		return false
	}

	mode := s.Mode()

	// Plan mode denies any plan that is not read-only.
	if mode == api.ModePlan && !plan.ReadOnly {
		return tools.AccessDeny, nil, nil
	}

	// A policy "ask" ceiling forces user confirmation unless the mode is auto or
	// an explicit allow rule already matches the plan.
	if policyAction == warp.ToolPolicyAsk && mode != api.ModeAuto && !hasMatchingAllow(plan) {
		// Fall through to minting an AccessAsk request.
	} else {
		switch mode {
		case api.ModePlan:
			if plan.Safety == api.SafetySafe {
				return tools.AccessAllow, nil, nil
			}
			if plan.Safety != api.SafetyUnsafe && hasMatchingAllow(plan) {
				return tools.AccessAllow, nil, nil
			}
		case api.ModeAsk:
			if plan.Safety != api.SafetyUnsafe && hasMatchingAllow(plan) {
				return tools.AccessAllow, nil, nil
			}
		case api.ModeAuto:
			return tools.AccessAllow, nil, nil
		default: // ModeDefault, ModeAcceptEdits
			if plan.ReadOnly && plan.Safety == api.SafetySafe {
				return tools.AccessAllow, nil, nil
			}
			if mode == api.ModeAcceptEdits && plan.Action == api.ActionWrite && plan.Safety != api.SafetyUnsafe {
				return tools.AccessAllow, nil, nil
			}
			if plan.Safety != api.SafetyUnsafe && hasMatchingAllow(plan) {
				return tools.AccessAllow, nil, nil
			}
		}
	}

	// 5. Mint UUIDv7 and return AccessAsk with PermissionRequest.
	id, err := uuid.NewV7()
	if err != nil {
		return tools.AccessDeny, nil, fmt.Errorf("permissions: generate request id: %w", err)
	}

	req := &api.PermissionRequest{
		ID:            id.String(),
		ToolCallID:    plan.ToolCallID,
		ToolName:      plan.ToolName,
		Action:        plan.Action,
		Safety:        plan.Safety,
		Target:        plan.Target,
		Summary:       plan.Summary,
		Diff:          plan.Diff,
		GrantPatterns: DeriveGrantRules(plan.ToolName, plan.Action, plan.Target),
	}
	return tools.AccessAsk, req, nil
}

// evaluatePolicy computes the effective [warp.ToolPolicyAction] for plan by
// combining the workspace policy ceiling with the active agent's policy
// ceiling, returning the stricter of the two (allow < ask < deny).
func evaluatePolicy(plan *tools.Plan, wsPolicies, agentPolicies *warp.Policies, ws *workspace.Workspace) warp.ToolPolicyAction {
	var wsTool *warp.Tool
	if ws != nil && ws.Catalog() != nil {
		if t, ok := ws.Catalog().GetTool(plan.ToolName); ok {
			wsTool = t
		}
	}

	wsAction := warp.ToolPolicyAllow
	if wsPolicies != nil {
		wsAction = evaluateToolPolicies(wsPolicies.Tools, plan, wsTool)
	}

	agentAction := warp.ToolPolicyAllow
	if agentPolicies != nil {
		agentAction = evaluateToolPolicies(agentPolicies.Tools, plan, wsTool)
	}

	return warp.StricterToolPolicyAction(wsAction, agentAction)
}

// evaluateToolPolicies evaluates a single [warp.ToolPolicies] against plan.
//
// When a catalog tool exists for plan.ToolName it is evaluated directly via
// [warp.ToolPolicies.EvaluateTool], which natively handles ReadOnly, Include,
// Exclude, Ask, Dangerous, OpenWorld, and qualified name patterns. Otherwise a
// minimal [warp.Tool] is synthesized from the runtime plan so that dynamic
// properties (which a static manifest may not capture, or may be absent) are
// still honored.
func evaluateToolPolicies(tp *warp.ToolPolicies, plan *tools.Plan, wsTool *warp.Tool) warp.ToolPolicyAction {
	tool := wsTool
	if tool == nil {
		tool = &warp.Tool{
			BaseResource: warp.BaseResource{
				Metadata: warp.Metadata{Name: plan.ToolName},
			},
			Spec: warp.ToolSpec{
				Annotations: &warp.ToolAnnotation{
					IsReadOnly:  plan.ReadOnly,
					IsDangerous: plan.Safety == api.SafetyUnsafe || plan.Action == api.ActionDelete,
					IsOpenWorld: plan.Action == api.ActionNet,
				},
			},
		}
	}

	action := warp.ToolPolicyAllow
	if tp != nil {
		action = tp.EvaluateTool(tool)
	}

	if tp == nil {
		return action
	}

	// Layer dynamic runtime plan properties that a static manifest may not
	// capture (or may not have at all).
	if tp.ReadOnly && !plan.ReadOnly {
		return warp.ToolPolicyDeny
	}
	if plan.Safety == api.SafetyUnsafe || plan.Action == api.ActionDelete {
		dangerous := tp.Dangerous
		if dangerous == "" {
			dangerous = warp.ToolPolicyAsk
		}
		action = warp.StricterToolPolicyAction(action, dangerous)
	}
	if plan.Action == api.ActionNet {
		if tp.OpenWorld == warp.ToolPolicyDeny || tp.OpenWorld == warp.ToolPolicyAsk {
			action = warp.StricterToolPolicyAction(action, tp.OpenWorld)
		}
	}

	return action
}
