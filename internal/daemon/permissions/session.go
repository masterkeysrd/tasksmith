package permissions

import (
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/masterkeysrd/loom/tool"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
)

// SessionManager manages permission evaluation, permission modes, and
// session-scoped permission rules.
type SessionManager interface {
	Mode() tools.PermissionMode
	SetMode(mode tools.PermissionMode)
	Permissions() Permissions
	Policies() *workspace.Policies
	SetPolicies(p *workspace.Policies)
	AddAllow(rule string) error
	AddDeny(rule string) error
	Clear() error
	ApplyDecision(req *tools.PermissionRequest, decision tools.PermissionDecision) error
	Evaluate(plan *tools.Plan) (tools.AccessDecision, *tools.PermissionRequest, error)
}

type sessionManager struct {
	mu          sync.RWMutex
	manager     Manager
	ws          workspace.Workspace
	mode        tools.PermissionMode
	permissions Permissions
	policies    *workspace.Policies
	onUpdate    func(Permissions) error
}

var _ SessionManager = (*sessionManager)(nil)

// NewSession constructs a detached [SessionManager] without an underlying root manager.
func NewSession(mode tools.PermissionMode, initial Permissions, onUpdate func(Permissions) error) SessionManager {
	if mode == "" {
		mode = tools.ModeDefault
	}
	return &sessionManager{
		mode:        mode,
		permissions: initial.Clone(),
		onUpdate:    onUpdate,
	}
}

// Mode returns the active [tools.PermissionMode].
func (s *sessionManager) Mode() tools.PermissionMode {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.mode == "" {
		return tools.ModeDefault
	}
	return s.mode
}

// SetMode updates the active [tools.PermissionMode].
func (s *sessionManager) SetMode(mode tools.PermissionMode) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if mode == "" {
		mode = tools.ModeDefault
	}
	s.mode = mode
}

// Permissions returns a deep copy of the active session [Permissions].
func (s *sessionManager) Permissions() Permissions {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.permissions.Clone()
}

// Policies returns the active workspace policies or override configured on s.
func (s *sessionManager) Policies() *workspace.Policies {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.policies != nil {
		return s.policies
	}
	if s.ws != nil {
		return s.ws.Policies()
	}
	return nil
}

// SetPolicies overrides the workspace policies evaluated by s.
func (s *sessionManager) SetPolicies(p *workspace.Policies) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.policies = p
}

// AddAllow validates rule and adds it to the session allow rules.
func (s *sessionManager) AddAllow(rule string) error {
	if _, _, err := ParseRule(rule); err != nil {
		return err
	}

	s.mu.Lock()
	if slices.Contains(s.permissions.Allow, rule) {
		s.mu.Unlock()
		return nil
	}
	s.permissions.Allow = append(s.permissions.Allow, rule)
	updated := s.permissions.Clone()
	onUpdate := s.onUpdate
	s.mu.Unlock()

	if onUpdate != nil {
		return onUpdate(updated)
	}
	return nil
}

// AddDeny validates rule and adds it to the session deny rules.
func (s *sessionManager) AddDeny(rule string) error {
	if _, _, err := ParseRule(rule); err != nil {
		return err
	}

	s.mu.Lock()
	if slices.Contains(s.permissions.Deny, rule) {
		s.mu.Unlock()
		return nil
	}
	s.permissions.Deny = append(s.permissions.Deny, rule)
	updated := s.permissions.Clone()
	onUpdate := s.onUpdate
	s.mu.Unlock()

	if onUpdate != nil {
		return onUpdate(updated)
	}
	return nil
}

// Clear removes all session-scoped allow and deny rules.
func (s *sessionManager) Clear() error {
	s.mu.Lock()
	s.permissions = Permissions{}
	updated := s.permissions.Clone()
	onUpdate := s.onUpdate
	s.mu.Unlock()

	if onUpdate != nil {
		return onUpdate(updated)
	}
	return nil
}

// ApplyDecision processes a user's [tools.PermissionDecision] for req.
func (s *sessionManager) ApplyDecision(req *tools.PermissionRequest, decision tools.PermissionDecision) error {
	switch decision.Kind {
	case tools.DecisionAllowOnce:
		return nil
	case tools.DecisionAllowSession:
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
			scope := Scope(strings.TrimSpace(decision.Scope))
			if scope == "" {
				scope = ScopeSession
			}
			switch scope {
			case ScopeSession:
				if err := s.AddAllow(rule); err != nil {
					return err
				}
			case ScopeProject:
				if s.manager == nil {
					return ErrInvalidConfig
				}
				if err := s.manager.AddRule(ScopeProject, EffectAllow, rule); err != nil {
					return err
				}
			case ScopeGlobal:
				if s.manager == nil {
					return ErrInvalidConfig
				}
				if err := s.manager.AddRule(ScopeGlobal, EffectAllow, rule); err != nil {
					return err
				}
			default:
				return fmt.Errorf("%w: %q", ErrInvalidScope, decision.Scope)
			}
		}
		return nil
	case tools.DecisionReject:
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
func (s *sessionManager) Evaluate(plan *tools.Plan) (tools.AccessDecision, *tools.PermissionRequest, error) {
	if plan == nil {
		return tools.AccessDeny, nil, fmt.Errorf("%w: plan cannot be nil", tools.ErrPermissionDenied)
	}

	// 1. Check workspace tool policies.
	policies := s.Policies()
	if !isAllowedByPolicy(plan, policies, s.ws) {
		return tools.AccessDeny, nil, nil
	}

	// 2. Load current rules for Session (in-memory), Project, and Global.
	sessPerms := s.Permissions()
	var projPerms, globPerms Permissions
	if s.manager != nil {
		var err error
		projPerms, err = s.manager.Get(ScopeProject)
		if err != nil {
			return tools.AccessDeny, nil, err
		}
		globPerms, err = s.manager.Get(ScopeGlobal)
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
	switch mode {
	case tools.ModePlan:
		if !plan.ReadOnly {
			return tools.AccessDeny, nil, nil
		}
		if plan.Safety == tools.SafetySafe {
			return tools.AccessAllow, nil, nil
		}
		if plan.Safety != tools.SafetyUnsafe && hasMatchingAllow(plan) {
			return tools.AccessAllow, nil, nil
		}
	case tools.ModeAsk:
		if plan.Safety != tools.SafetyUnsafe && hasMatchingAllow(plan) {
			return tools.AccessAllow, nil, nil
		}
	case tools.ModeAuto:
		return tools.AccessAllow, nil, nil
	default: // ModeDefault, ModeAcceptEdits
		if plan.ReadOnly && plan.Safety == tools.SafetySafe {
			return tools.AccessAllow, nil, nil
		}
		if mode == tools.ModeAcceptEdits && plan.Action == tools.ActionWrite && plan.Safety != tools.SafetyUnsafe {
			return tools.AccessAllow, nil, nil
		}
		if plan.Safety != tools.SafetyUnsafe && hasMatchingAllow(plan) {
			return tools.AccessAllow, nil, nil
		}
	}

	// 5. Mint UUIDv7 and return AccessAsk with PermissionRequest.
	id, err := uuid.NewV7()
	if err != nil {
		return tools.AccessDeny, nil, fmt.Errorf("permissions: generate request id: %w", err)
	}

	req := &tools.PermissionRequest{
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

func isAllowedByPolicy(plan *tools.Plan, policies *workspace.Policies, ws workspace.Workspace) bool {
	if policies == nil || policies.Tools == nil {
		return true
	}
	tp := policies.Tools

	// Exclude globs take precedence.
	for _, pattern := range tp.Exclude {
		if matchPattern(pattern, plan.ToolName) {
			return false
		}
	}

	// If Include is non-empty, ToolName must match at least one pattern.
	if len(tp.Include) > 0 {
		matched := false
		for _, pattern := range tp.Include {
			if matchPattern(pattern, plan.ToolName) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	var ann tool.Annotation
	hasAnn := false
	if ws != nil {
		if wsTool, ok := ws.GetTool(plan.ToolName); ok && wsTool.Annotations != nil {
			ann = tool.Annotation{
				IsOpenWorld:  wsTool.Annotations.IsOpenWorld,
				IsDangerous:  wsTool.Annotations.IsDangerous,
				IsReadOnly:   wsTool.Annotations.IsReadOnly,
				IsIdempotent: wsTool.Annotations.IsIdempotent,
				UserHint:     wsTool.Annotations.UserHint,
			}
			hasAnn = true
		}
	}

	if tp.AllowDangerous != nil && !*tp.AllowDangerous {
		isDangerous := plan.Safety == tools.SafetyUnsafe || plan.Action == tools.ActionDelete
		if hasAnn && ann.IsDangerous {
			isDangerous = true
		}
		if isDangerous {
			return false
		}
	}

	if tp.AllowOpenWorld != nil && !*tp.AllowOpenWorld {
		isOpenWorld := plan.Action == tools.ActionNet
		if hasAnn && ann.IsOpenWorld {
			isOpenWorld = true
		}
		if isOpenWorld {
			return false
		}
	}

	return true
}
