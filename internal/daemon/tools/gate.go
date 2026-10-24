package tools

import (
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/masterkeysrd/loom/tool"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
)

// Gate manages session permission modes, session-scoped grants, workspace tool
// policy enforcement, and per-path mutex locking for file mutations.
type Gate struct {
	mu          sync.RWMutex
	mode        PermissionMode
	grants      []Grant
	policies    *workspace.Policies
	workspace   *workspace.Workspace
	annotations map[string]tool.Annotation

	locksMu   sync.Mutex
	pathLocks map[string]*sync.Mutex
}

// NewGate constructs a new [Gate] initialized in [ModeDefault] with policies
// loaded from ws (when non-nil).
func NewGate(ws *workspace.Workspace) *Gate {
	var policies *workspace.Policies
	if ws != nil {
		policies = ws.Policies()
	}
	return &Gate{
		mode:        ModeDefault,
		policies:    policies,
		workspace:   ws,
		annotations: make(map[string]tool.Annotation),
		pathLocks:   make(map[string]*sync.Mutex),
	}
}

func (g *Gate) registerAnnotation(name string, ann tool.Annotation) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.annotations == nil {
		g.annotations = make(map[string]tool.Annotation)
	}
	g.annotations[name] = ann
}

// Mode returns the active [PermissionMode].
func (g *Gate) Mode() PermissionMode {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if g.mode == "" {
		return ModeDefault
	}
	return g.mode
}

// SetMode updates the active [PermissionMode].
func (g *Gate) SetMode(mode PermissionMode) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if mode == "" {
		mode = ModeDefault
	}
	g.mode = mode
}

// AddGrant appends a session-scoped [Grant] if an identical grant is not
// already present.
func (g *Gate) AddGrant(grant Grant) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if slices.Contains(g.grants, grant) {
		return
	}
	g.grants = append(g.grants, grant)
}

// Grants returns a defensive copy of all active session grants.
func (g *Gate) Grants() []Grant {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return slices.Clone(g.grants)
}

// ClearGrants removes all active session grants.
func (g *Gate) ClearGrants() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.grants = nil
}

// Policies returns the active workspace policies configured on the Gate.
func (g *Gate) Policies() *workspace.Policies {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if g.policies != nil {
		return g.policies
	}
	if g.workspace != nil {
		return g.workspace.Policies()
	}
	return nil
}

// SetPolicies overrides the workspace policies evaluated by the Gate.
func (g *Gate) SetPolicies(policies *workspace.Policies) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.policies = policies
}

// ApplyDecision processes a user's [PermissionDecision] for req, recording a
// session grant when Kind is [DecisionAllowSession] or returning
// [ErrPermissionDenied] when Kind is [DecisionReject].
func (g *Gate) ApplyDecision(req *PermissionRequest, decision PermissionDecision) error {
	switch decision.Kind {
	case DecisionAllowOnce:
		return nil
	case DecisionAllowSession:
		if req != nil {
			pattern := strings.TrimSpace(decision.GrantPattern)
			if pattern == "" {
				if len(req.GrantPatterns) > 0 {
					pattern = req.GrantPatterns[0]
				} else if req.Target != "" {
					pattern = req.Target
				} else {
					pattern = "*"
				}
			}
			g.AddGrant(Grant{
				ToolName: req.ToolName,
				Action:   req.Action,
				Pattern:  pattern,
			})
		}
		return nil
	case DecisionReject:
		if strings.TrimSpace(decision.Feedback) != "" {
			return fmt.Errorf("%w: %s", ErrPermissionDenied, strings.TrimSpace(decision.Feedback))
		}
		return ErrPermissionDenied
	default:
		return fmt.Errorf("%w: unknown decision kind %q", ErrPermissionDenied, decision.Kind)
	}
}

// Evaluate checks plan against workspace policies, the active [PermissionMode],
// safety classification, and session grants.
func (g *Gate) Evaluate(plan *Plan) (AccessDecision, *PermissionRequest, error) {
	if plan == nil {
		return AccessDeny, nil, fmt.Errorf("%w: plan cannot be nil", ErrPermissionDenied)
	}

	g.mu.RLock()
	mode := g.mode
	if mode == "" {
		mode = ModeDefault
	}
	grants := slices.Clone(g.grants)
	policies := g.policies
	ws := g.workspace
	ann, hasAnn := g.annotations[plan.ToolName]
	g.mu.RUnlock()

	if policies == nil && ws != nil {
		policies = ws.Policies()
	}

	// 1. Check workspace tool policies (Exclude / Include globs, AllowDangerous, AllowOpenWorld).
	if !g.isAllowedByPolicy(plan, policies, ws, ann, hasAnn) {
		return AccessDeny, nil, nil
	}

	switch mode {
	case ModePlan:
		if !plan.ReadOnly {
			return AccessDeny, nil, nil
		}
		if plan.Safety == SafetySafe {
			return AccessAllow, nil, nil
		}
		if plan.Safety != SafetyUnsafe && hasMatchingGrant(grants, plan) {
			return AccessAllow, nil, nil
		}
	case ModeAsk:
		if plan.Safety != SafetyUnsafe && hasMatchingGrant(grants, plan) {
			return AccessAllow, nil, nil
		}
	case ModeAuto:
		return AccessAllow, nil, nil
	default: // ModeDefault, ModeAcceptEdits
		if plan.ReadOnly && plan.Safety == SafetySafe {
			return AccessAllow, nil, nil
		}
		if mode == ModeAcceptEdits && plan.Action == ActionWrite && plan.Safety != SafetyUnsafe {
			return AccessAllow, nil, nil
		}
		if plan.Safety != SafetyUnsafe && hasMatchingGrant(grants, plan) {
			return AccessAllow, nil, nil
		}
	}

	// Mint a uuid.NewV7() ID and return AccessAsk, &PermissionRequest{...}, nil.
	id, err := uuid.NewV7()
	if err != nil {
		return AccessDeny, nil, fmt.Errorf("tools: generate permission request id: %w", err)
	}

	req := &PermissionRequest{
		ID:            id.String(),
		ToolCallID:    plan.ToolCallID,
		ToolName:      plan.ToolName,
		Action:        plan.Action,
		Safety:        plan.Safety,
		Target:        plan.Target,
		Summary:       plan.Summary,
		Diff:          plan.Diff,
		GrantPatterns: deriveGrantPatterns(plan.Action, plan.Target),
	}
	return AccessAsk, req, nil
}

func deriveGrantPatterns(action ActionType, target string) []string {
	target = strings.TrimSpace(target)
	if target == "" {
		return []string{"*"}
	}

	switch action {
	case ActionRead, ActionWrite, ActionDelete:
		patterns := []string{target}
		dir := path.Dir(target)
		if dir != "." && dir != "" && dir != "/" {
			patterns = append(patterns, dir+"/**")
		}
		patterns = append(patterns, "*")
		return patterns
	case ActionExec:
		patterns := []string{target}
		if fields := strings.Fields(target); len(fields) > 0 {
			patterns = append(patterns, fields[0]+" *")
		}
		patterns = append(patterns, "*")
		return patterns
	case ActionNet:
		if u, err := url.Parse(target); err == nil && u.Host != "" {
			return []string{u.Host, target, "*"}
		}
		return []string{target, "*"}
	default:
		return []string{target, "*"}
	}
}

func hasMatchingGrant(grants []Grant, plan *Plan) bool {
	for _, gr := range grants {
		if matchGrant(gr, plan) {
			return true
		}
	}
	return false
}

// lockPath acquires the per-path mutex for targetPath and returns an unlock function.
func (g *Gate) lockPath(targetPath string) func() {
	key := filepath.ToSlash(filepath.Clean(strings.TrimSpace(targetPath)))
	g.locksMu.Lock()
	if g.pathLocks == nil {
		g.pathLocks = make(map[string]*sync.Mutex)
	}
	mu, ok := g.pathLocks[key]
	if !ok {
		mu = &sync.Mutex{}
		g.pathLocks[key] = mu
	}
	g.locksMu.Unlock()

	mu.Lock()
	return mu.Unlock
}

func (g *Gate) isAllowedByPolicy(plan *Plan, policies *workspace.Policies, ws *workspace.Workspace, regAnn tool.Annotation, hasRegAnn bool) bool {
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

	ann := regAnn
	if ws != nil {
		if wsTool, ok := ws.GetTool(plan.ToolName); ok && wsTool.Annotations != nil {
			ann = tool.Annotation{
				IsOpenWorld:  wsTool.Annotations.IsOpenWorld,
				IsDangerous:  wsTool.Annotations.IsDangerous,
				IsReadOnly:   wsTool.Annotations.IsReadOnly,
				IsIdempotent: wsTool.Annotations.IsIdempotent,
				UserHint:     wsTool.Annotations.UserHint,
			}
			hasRegAnn = true
		}
	}

	if tp.AllowDangerous != nil && !*tp.AllowDangerous {
		isDangerous := plan.Safety == SafetyUnsafe || plan.Action == ActionDelete
		if hasRegAnn && ann.IsDangerous {
			isDangerous = true
		}
		if isDangerous {
			return false
		}
	}

	if tp.AllowOpenWorld != nil && !*tp.AllowOpenWorld {
		isOpenWorld := plan.Action == ActionNet
		if hasRegAnn && ann.IsOpenWorld {
			isOpenWorld = true
		}
		if isOpenWorld {
			return false
		}
	}

	return true
}

func matchGrant(gr Grant, plan *Plan) bool {
	toolPattern := strings.TrimSpace(gr.ToolName)
	if toolPattern != "" && toolPattern != "*" && !matchPattern(toolPattern, plan.ToolName) {
		return false
	}

	if gr.Action != "" && gr.Action != "*" && gr.Action != plan.Action {
		return false
	}

	pattern := strings.TrimSpace(gr.Pattern)
	if pattern == "" || pattern == "*" {
		return true
	}

	target := plan.Target
	if pattern == target {
		return true
	}

	// URL host matching for network tools.
	if u, err := url.Parse(target); err == nil && u.Host != "" {
		if pattern == u.Host || pattern == u.Hostname() || matchPattern(pattern, u.Host) || matchPattern(pattern, u.Hostname()) {
			return true
		}
	}

	// Directory prefix / /** glob matching.
	if before, ok := strings.CutSuffix(pattern, "/**"); ok {
		prefix := before
		prefix = strings.TrimSuffix(prefix, "/")
		if prefix == "" || prefix == "." {
			return true
		}
		if target == prefix || strings.HasPrefix(target, prefix+"/") {
			return true
		}
	}

	// Command / string prefix matching (e.g. "go test*", "mkdir *").
	if strings.HasSuffix(pattern, "*") && !strings.Contains(pattern, "**") {
		prefix := strings.TrimSuffix(pattern, "*")
		if strings.HasPrefix(target, prefix) || strings.TrimSpace(prefix) == target {
			return true
		}
	}

	return matchPattern(pattern, target)
}

func matchPattern(pattern, target string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "*" {
		return true
	}
	if pattern == target {
		return true
	}
	if ok, err := path.Match(pattern, target); err == nil && ok {
		return true
	}
	reStr := globToRegex(pattern)
	if re, err := regexp.Compile(reStr); err == nil {
		return re.MatchString(target)
	}
	return false
}

func globToRegex(glob string) string {
	var sb strings.Builder
	sb.WriteString("^")
	for i := 0; i < len(glob); i++ {
		ch := glob[i]
		switch ch {
		case '*':
			if i+1 < len(glob) && glob[i+1] == '*' {
				sb.WriteString(".*")
				i++
				if i+1 < len(glob) && glob[i+1] == '/' {
					i++
				}
			} else {
				sb.WriteString("[^/]*")
			}
		case '?':
			sb.WriteString("[^/]")
		case '.', '+', '(', ')', '|', '^', '$', '{', '}', '\\':
			sb.WriteByte('\\')
			sb.WriteByte(ch)
		default:
			sb.WriteByte(ch)
		}
	}
	sb.WriteString("$")
	return sb.String()
}
