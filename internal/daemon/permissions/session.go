package permissions

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/fsutils"
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
	// SetPlansDir registers the session-scoped safe plans directory
	// (<session-dir>/plans/) where agent write/edit/remove operations are
	// auto-allowed and remain writable even in restricted workspaces.
	SetPlansDir(dir string)
	// SetRuntimePolicy installs the live sandbox posture consulted while
	// evaluating bash plans (autoAllowBash). A nil policy clears it, which
	// disables autoAllowBash widening.
	SetRuntimePolicy(p *RuntimePolicy)
	// RuntimePolicy returns the live sandbox posture, or nil when unset.
	RuntimePolicy() *RuntimePolicy
	// SetWorkspace binds permission and restriction checks to ws.
	SetWorkspace(ws *workspace.Workspace)
	ForSubagent() SessionManager
	AddAllow(rule string) error
	AddDeny(rule string) error
	Clear() error
	ApplyDecision(req *api.PermissionRequest, decision api.PermissionDecision) error
	Evaluate(plan *tools.Plan) (tools.AccessDecision, *api.PermissionRequest, error)
}

// RuntimePolicy carries the live, per-session posture the permission engine
// consults beyond static permission files. It is refreshed whenever sandbox
// settings change so edits apply to running sessions without recreation.
type RuntimePolicy struct {
	// AutoAllowBash auto-approves safe, sandboxed bash plans in default and
	// accept_edits modes. It never widens plan/ask modes, unsandboxed plans,
	// or anything unsafe, outside, denied, or rule-asked.
	AutoAllowBash bool
	// SandboxNative reports whether the session's enforced backend is the
	// kernel sandbox; autoAllowBash only applies when it is.
	SandboxNative bool
}

type sessionState struct {
	mu          sync.RWMutex
	manager     Manager
	ws          *workspace.Workspace
	plansDir    string
	mode        api.PermissionMode
	permissions api.Permissions
	policies    *warp.Policies
	runtime     *RuntimePolicy
	onUpdate    func(api.Permissions) error
}

type sessionManager struct {
	*sessionState
	agentMu       sync.RWMutex
	agentPolicies *warp.Policies
}

var _ SessionManager = (*sessionManager)(nil)

// NewSession constructs a detached [SessionManager] without an underlying root manager.
func NewSession(mode api.PermissionMode, initial api.Permissions, onUpdate func(api.Permissions) error) SessionManager {
	if mode == "" {
		mode = api.ModeDefault
	}
	return &sessionManager{
		sessionState: &sessionState{
			mode:        mode,
			permissions: initial.Clone(),
			onUpdate:    onUpdate,
		},
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
	s.agentMu.RLock()
	defer s.agentMu.RUnlock()
	return s.agentPolicies
}

// SetAgentPolicies overrides the active agent's tool policies evaluated by s.
func (s *sessionManager) SetAgentPolicies(p *warp.Policies) {
	s.agentMu.Lock()
	defer s.agentMu.Unlock()
	s.agentPolicies = p
}

// SetPlansDir registers the session-scoped safe plans directory where agent
// write, edit, and remove operations are auto-allowed.
func (s *sessionManager) SetPlansDir(dir string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	dir = strings.TrimSpace(dir)
	if dir == "" {
		s.plansDir = ""
		return
	}
	s.plansDir = filepath.Clean(dir)
}

func (s *sessionState) isPlanDirMutation(plan *tools.Plan) bool {
	if plan == nil || plan.Safety != api.SafetySafe || plan.Target == "" {
		return false
	}
	if plan.Action != api.ActionWrite && plan.Action != api.ActionDelete {
		return false
	}
	s.mu.RLock()
	plansDir := s.plansDir
	s.mu.RUnlock()
	if plansDir == "" {
		return false
	}
	return fsutils.IsInsideDir(filepath.Clean(plan.Target), plansDir)
}

// SetRuntimePolicy installs the live sandbox posture consulted while
// evaluating bash plans. Subagents share the session state, so the posture
// applies to the whole session tree.
func (s *sessionManager) SetRuntimePolicy(p *RuntimePolicy) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p == nil {
		s.runtime = nil
		return
	}
	copied := *p
	s.runtime = &copied
}

// runtimePolicy returns a copy of the live runtime posture, or nil when unset.
func (s *sessionState) runtimePolicy() *RuntimePolicy {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.runtime == nil {
		return nil
	}
	copied := *s.runtime
	return &copied
}

// RuntimePolicy returns a copy of the live sandbox posture consulted while
// evaluating bash plans, or nil when unset.
func (s *sessionManager) RuntimePolicy() *RuntimePolicy {
	return s.runtimePolicy()
}

// SetWorkspace binds permission and restriction checks to ws.
func (s *sessionManager) SetWorkspace(ws *workspace.Workspace) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ws = ws
}

func (s *sessionState) workspace() *workspace.Workspace {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ws
}

// ForSubagent returns a child [SessionManager] that shares the receiver's
// permission mode, workspace policies, and session-scoped allow/deny rules
// while isolating its active agent tool policy ceiling.
func (s *sessionManager) ForSubagent() SessionManager {
	return &sessionManager{
		sessionState: s.sessionState,
	}
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
			// Only an explicit bypass request grants into the
			// tool_unsandboxed(...) namespace; an excludedCommands match
			// keeps granting ordinary tool(...) rules.
			grantTool := req.ToolName
			if req.BypassSandbox {
				grantTool = UnsandboxedToolName(req.ToolName)
			}
			raw := strings.TrimSpace(decision.GrantPattern)
			var rule string
			if raw == "" {
				if len(req.GrantPatterns) > 0 {
					rule = req.GrantPatterns[0]
				} else if req.Target != "" {
					rule = FormatRule(grantTool, req.Target)
				} else {
					rule = FormatRule(grantTool, "*")
				}
			} else if toolName, pattern, err := ParseRule(raw); err != nil {
				rule = FormatRule(grantTool, raw)
			} else if req.BypassSandbox {
				// A bypass grant must never widen the sandboxed namespace:
				// keep wildcard/unsandboxed rules verbatim, re-key plain
				// same-tool rules into the unsandboxed namespace, and reject
				// patterns for unrelated tools.
				switch toolName {
				case "*", UnsandboxedToolName(req.ToolName):
					rule = raw
				case req.ToolName:
					rule = FormatRule(grantTool, pattern)
				default:
					return fmt.Errorf("%w: grant pattern %q does not apply to unsandboxed %s requests", ErrInvalidRule, raw, req.ToolName)
				}
			} else if IsUnsandboxedToolName(toolName) {
				// A sandboxed or excludedCommands request must never mint an
				// unsandboxed-namespace rule behind the user's back.
				return fmt.Errorf("%w: grant pattern %q requires an unsandboxed bypass request", ErrInvalidRule, raw)
			} else {
				rule = raw
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

// matchDenyRule checks whether a deny rule covers plan. Unlike allow
// matching, deny applies across both rule namespaces: bash(git *) denies
// sandboxed and unsandboxed git commands alike, while bash_unsandboxed(git *)
// denies only the variant that skips the kernel sandbox (an explicit bypass or
// an excluded-command match).
func matchDenyRule(rule string, plan *tools.Plan) bool {
	if plan == nil {
		return false
	}
	if !plan.Unsandboxed && !plan.BypassSandbox {
		return MatchRule(rule, plan)
	}
	toolName, _, err := ParseRule(rule)
	if err != nil {
		return false
	}
	if IsUnsandboxedToolName(toolName) {
		probe := *plan
		probe.BypassSandbox = true
		return MatchRule(rule, &probe)
	}
	probe := *plan
	probe.BypassSandbox = false
	probe.Unsandboxed = false
	return MatchRule(rule, &probe)
}

// Evaluate checks plan against workspace policies, active permission mode,
// safety classification, and deny/allow permission rules.
func (s *sessionManager) Evaluate(plan *tools.Plan) (tools.AccessDecision, *api.PermissionRequest, error) {
	if plan == nil {
		return tools.AccessDeny, nil, fmt.Errorf("%w: plan cannot be nil", tools.ErrPermissionDenied)
	}

	// 1. Evaluate the workspace and active-agent tool policy ceiling.
	ws := s.workspace()
	policyAction := evaluatePolicy(plan, s.Policies(), s.AgentPolicies(), ws)
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
	// Deny spans both namespaces (see matchDenyRule).
	for _, r := range sessPerms.Deny {
		if matchDenyRule(r, plan) {
			return tools.AccessDeny, nil, nil
		}
	}
	for _, r := range projPerms.Deny {
		if matchDenyRule(r, plan) {
			return tools.AccessDeny, nil, nil
		}
	}
	for _, r := range globPerms.Deny {
		if matchDenyRule(r, plan) {
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

	// Restricted (untrusted) workspaces deny all mutating or side-effecting
	// actions regardless of mode, while keeping the session-scoped plans
	// directory writable.
	if ws != nil && ws.IsRestricted() && !plan.ReadOnly && !s.isPlanDirMutation(plan) {
		return tools.AccessDeny, nil, fmt.Errorf("%w: workspace is in restricted (read-only) mode because it is untrusted "+
			"or its executable configuration changed; approve workspace trust to allow writes", tools.ErrPermissionDenied)
	}

	// Agent write, edit, and remove operations inside <session-dir>/plans/ are
	// session-scoped and safe, so they auto-allow without prompting (unless
	// blocked above by policy or an explicit deny rule).
	if s.isPlanDirMutation(plan) {
		return tools.AccessAllow, nil, nil
	}

	// A policy "ask" ceiling forces user confirmation unless the mode is auto or
	// an explicit allow rule already matches the plan.
	if policyAction == warp.ToolPolicyAsk && mode != api.ModeAuto && !hasMatchingAllow(plan) {
		// Fall through to minting an AccessAsk request.
	} else if plan.BypassSandbox {
		// An unsandboxed bypass retry lives in its own rule namespace: only a
		// matching bash_unsandboxed(...) (or wildcard) allow rule can approve
		// it, and every other mode — including auto and accept_edits — still
		// prompts, because the kernel boundary disappears for this command.
		if hasMatchingAllow(plan) {
			return tools.AccessAllow, nil, nil
		}
	} else if plan.Action == api.ActionNet && plan.ToolName == "net" {
		// A synthetic host-approval plan from the sandbox network proxy:
		// sandbox.json's allowlist is the boundary, so no permission mode,
		// including auto, may silently open an unlisted host. Only an explicit
		// net(host) (or wildcard) allow rule auto-approves; everything else
		// falls through to a prompt.
		if hasMatchingAllow(plan) {
			return tools.AccessAllow, nil, nil
		}
	} else {
		switch mode {
		case api.ModeAuto:
			// Auto mode auto-allows plans the deterministic engine can vouch for:
			// workspace-safe non-destructive operations, workspace-safe
			// reversible deletions (e.g. journal-snapshotted remove), or
			// non-unsafe plans covered by an explicit allow rule. Unsafe,
			// unmatched outside-workspace, and non-reversible delete plans mint
			// an AccessAsk for the calibrated reviewer.
			if (plan.Safety == api.SafetySafe && (plan.Action != api.ActionDelete || plan.Reversible)) ||
				(plan.Safety != api.SafetyUnsafe && hasMatchingAllow(plan)) {
				return tools.AccessAllow, nil, nil
			}
		default: // ModeDefault, ModeAcceptEdits, ModePlan, ModeAsk
			if plan.ReadOnly && plan.Safety == api.SafetySafe {
				return tools.AccessAllow, nil, nil
			}
			if mode == api.ModeAcceptEdits && plan.Action == api.ActionWrite && plan.Safety != api.SafetyUnsafe {
				return tools.AccessAllow, nil, nil
			}
			// autoAllowBash widens exactly one case: a safe, sandboxed bash
			// command inside a native kernel sandbox in default or accept_edits
			// modes. Unsafe, outside, unsandboxed, denied, and policy-asked plans
			// still prompt, and plan/ask modes keep standard prompting.
			if mode == api.ModeDefault || mode == api.ModeAcceptEdits {
				if rt := s.runtimePolicy(); rt != nil && rt.AutoAllowBash && rt.SandboxNative &&
					plan.ToolName == "bash" && !plan.Unsandboxed && plan.Safety == api.SafetySafe {
					return tools.AccessAllow, nil, nil
				}
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

	grantToolName := plan.ToolName
	if plan.BypassSandbox {
		// Granting an unsandboxed approval persists a bash_unsandboxed(...)
		// rule so the regular sandboxed namespace is never widened.
		grantToolName = UnsandboxedToolName(plan.ToolName)
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
		GrantPatterns: DeriveGrantRules(grantToolName, plan.Action, plan.Target),
		Unsandboxed:   plan.Unsandboxed,
		BypassSandbox: plan.BypassSandbox,
	}
	return tools.AccessAsk, req, nil
}

// EvaluatePolicy computes the effective [warp.ToolPolicyAction] for plan by
// combining the workspace policy ceiling with the active agent's policy
// ceiling, returning the stricter of the two (allow < ask < deny).
func EvaluatePolicy(plan *tools.Plan, wsPolicies, agentPolicies *warp.Policies, ws *workspace.Workspace) warp.ToolPolicyAction {
	return evaluatePolicy(plan, wsPolicies, agentPolicies, ws)
}

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
		// A synthesized plan has no catalog manifest. For network plans the
		// tool name is synthetic ("net"): it represents egress requested by an
		// already-approved tool such as bash, so an explicit Include allow-list
		// must not silently deny it just because "net" is not listed. Exclude,
		// Ask, Dangerous, and OpenWorld layers still apply unchanged.
		if wsTool == nil && plan.Action == api.ActionNet && len(tp.Include) > 0 {
			relaxed := *tp
			relaxed.Include = nil
			tp = &relaxed
		}
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
