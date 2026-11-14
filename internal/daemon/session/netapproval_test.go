package session

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/daemon/broker"
	"github.com/masterkeysrd/tasksmith/internal/daemon/harness"
	"github.com/masterkeysrd/tasksmith/internal/daemon/permissions"
	"github.com/masterkeysrd/tasksmith/internal/daemon/sandboxcfg"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
	"github.com/masterkeysrd/warp"
)

// askingPermManager always returns a genuine ask so host approvals reach the
// interactive prompt path.
type askingPermManager struct {
	mu       sync.Mutex
	applied  []api.PermissionDecision
	requests []*api.PermissionRequest
}

func (m *askingPermManager) Mode() api.PermissionMode        { return api.ModeDefault }
func (m *askingPermManager) SetMode(api.PermissionMode)      {}
func (m *askingPermManager) Permissions() api.Permissions    { return api.Permissions{} }
func (m *askingPermManager) Policies() *warp.Policies        { return nil }
func (m *askingPermManager) SetPolicies(*warp.Policies)      {}
func (m *askingPermManager) AgentPolicies() *warp.Policies   { return nil }
func (m *askingPermManager) SetAgentPolicies(*warp.Policies) {}
func (m *askingPermManager) SetRuntimePolicy(*permissions.RuntimePolicy) {
}
func (m *askingPermManager) RuntimePolicy() *permissions.RuntimePolicy { return nil }
func (m *askingPermManager) ForSubagent() permissions.SessionManager   { return m }
func (m *askingPermManager) AddAllow(string) error                     { return nil }
func (m *askingPermManager) AddDeny(string) error                      { return nil }
func (m *askingPermManager) Clear() error                              { return nil }

func (m *askingPermManager) ApplyDecision(req *api.PermissionRequest, decision api.PermissionDecision) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.applied = append(m.applied, decision)
	m.requests = append(m.requests, req)
	return nil
}

func (m *askingPermManager) appliedDecisions() []api.PermissionDecision {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]api.PermissionDecision, len(m.applied))
	copy(out, m.applied)
	return out
}

func (m *askingPermManager) Evaluate(plan *tools.Plan) (tools.AccessDecision, *api.PermissionRequest, error) {
	return tools.AccessAsk, &api.PermissionRequest{
		ToolName:   plan.ToolName,
		ToolCallID: "call-" + plan.Target,
		Summary:    plan.Summary,
		Target:     plan.Target,
	}, nil
}

// denyingPermManager silently denies every plan.
type denyingPermManager struct {
	askingPermManager
}

func (m *denyingPermManager) Evaluate(plan *tools.Plan) (tools.AccessDecision, *api.PermissionRequest, error) {
	return tools.AccessDeny, nil, nil
}

// observingPublisher records events and controls subscriber presence so the
// interactive() gate can be exercised.
type observingPublisher struct {
	testPublisher
	mu          sync.Mutex
	subscribers bool
}

func (p *observingPublisher) HasSubscribers(string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.subscribers
}

func (p *observingPublisher) setSubscribers(v bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.subscribers = v
}

// newHostApprovalSession builds a minimal session (no LLM, DB, or sandbox)
// sufficient to drive the host-approval code path.
func newHostApprovalSession(t *testing.T, perm permissions.SessionManager, pub *observingPublisher) *Session {
	t.Helper()
	if pub == nil {
		pub = &observingPublisher{}
		pub.setSubscribers(true)
	}
	if perm == nil {
		perm = &askingPermManager{}
	}
	return &Session{
		State:       NewState(),
		ID:          "netapproval-test",
		WorkDir:     t.TempDir(),
		PermManager: perm,
		publisher:   pub,
		logger:      log.Default(),
		bgCtx:       context.Background(),
	}
}

func permissionRequested(ev broker.Event) (*api.PermissionRequestedEvent, bool) {
	if e, ok := ev.(api.PermissionRequestedEvent); ok {
		return &e, true
	}
	return nil, false
}

func TestHostApprovalPromptAllowOnce(t *testing.T) {
	pub := &observingPublisher{}
	pub.setSubscribers(true)
	perm := &askingPermManager{}
	sess := newHostApprovalSession(t, perm, pub)

	type verdict struct {
		allow bool
		err   error
	}
	result := make(chan verdict, 1)
	go func() {
		allow, err := sess.requestHostApproval(context.Background(), "api.example.com", 443)
		result <- verdict{allow, err}
	}()

	var reqID string
	deadline := time.Now().Add(2 * time.Second)
	for reqID == "" && time.Now().Before(deadline) {
		for _, ev := range pub.recorded() {
			if e, ok := permissionRequested(ev); ok && len(e.Permissions) > 0 {
				reqID = e.Permissions[0].ID
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	if reqID == "" {
		t.Fatal("no PermissionRequestedEvent published for unlisted host")
	}

	// While pending, the prompt must be visible to reconnecting clients.
	pending := sess.Clone().PendingPermissions
	if len(pending) != 1 || pending[0].ID != reqID {
		t.Fatalf("Clone PendingPermissions = %+v, want the host prompt", pending)
	}

	handled, err := sess.resolveHostApproval(api.PermissionDecision{RequestID: reqID, Kind: api.DecisionAllowOnce})
	if !handled {
		t.Fatal("resolveHostApproval did not handle the host decision")
	}
	if err != nil {
		t.Fatalf("resolveHostApproval: %v", err)
	}

	select {
	case v := <-result:
		if v.err != nil || !v.allow {
			t.Fatalf("requestHostApproval = (%v, %v), want (true, nil)", v.allow, v.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("requestHostApproval did not return after decision")
	}

	applied := perm.appliedDecisions()
	if len(applied) != 1 || applied[0].Kind != api.DecisionAllowOnce {
		t.Fatalf("ApplyDecision calls = %+v, want one AllowOnce", applied)
	}
	sess.Lock()
	leftover := len(sess.hostApprovals) + sess.hostApprovalActive
	status := sess.Status
	sess.Unlock()
	if leftover != 0 {
		t.Fatalf("pending host approvals after resolve = %d, want 0", leftover)
	}
	if status != api.StatusIdle {
		t.Fatalf("status after resolve = %s, want idle", status)
	}
}

func TestHostApprovalSingleFlight(t *testing.T) {
	pub := &observingPublisher{}
	pub.setSubscribers(true)
	sess := newHostApprovalSession(t, &askingPermManager{}, pub)

	const racers = 4
	results := make(chan bool, racers)
	for range racers {
		go func() {
			allow, err := sess.requestHostApproval(context.Background(), "shared.example.com", 443)
			if err != nil {
				results <- false
				return
			}
			results <- allow
		}()
	}

	// Exactly one prompt should surface; answer it once.
	var reqID string
	deadline := time.Now().Add(2 * time.Second)
	prompts := 0
	for time.Now().Before(deadline) {
		prompts = 0
		reqID = ""
		for _, ev := range pub.recorded() {
			if e, ok := permissionRequested(ev); ok {
				prompts++
				reqID = e.Permissions[0].ID
			}
		}
		if prompts >= 1 && reqID != "" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if reqID == "" {
		t.Fatal("no prompt published")
	}
	if _, err := sess.resolveHostApproval(api.PermissionDecision{RequestID: reqID, Kind: api.DecisionAllowSession}); err != nil {
		t.Fatalf("resolveHostApproval: %v", err)
	}

	for range racers {
		select {
		case ok := <-results:
			if !ok {
				t.Fatal("racer refused despite approval")
			}
		case <-time.After(3 * time.Second):
			t.Fatal("racer did not resolve")
		}
	}
	// Give the owner goroutine a moment to finish before counting.
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		prompts = 0
		for _, ev := range pub.recorded() {
			if _, ok := permissionRequested(ev); ok {
				prompts++
			}
		}
		if prompts > 1 {
			t.Fatalf("single-flight violated: %d prompts for one host", prompts)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestHostApprovalNonInteractiveRefuses(t *testing.T) {
	pub := &observingPublisher{} // no subscribers
	sess := newHostApprovalSession(t, &askingPermManager{}, pub)

	allow, err := sess.requestHostApproval(context.Background(), "quiet.example.com", 443)
	if allow || err == nil {
		t.Fatalf("requestHostApproval = (%v, %v), want refusal with error", allow, err)
	}
	if !strings.Contains(err.Error(), "no client is attached") {
		t.Fatalf("error = %v, want no-client-attached refusal", err)
	}
	sess.Lock()
	pending := len(sess.hostApprovals)
	sess.Unlock()
	if pending != 0 {
		t.Fatalf("pending approvals = %d, want 0", pending)
	}
}

func TestHostApprovalSilentDeny(t *testing.T) {
	sess := newHostApprovalSession(t, &denyingPermManager{}, nil)
	allow, err := sess.requestHostApproval(context.Background(), "blocked.example.com", 443)
	if allow || err != nil {
		t.Fatalf("requestHostApproval = (%v, %v), want (false, nil) silent deny", allow, err)
	}
}

// TestHostApprovalKeepsTurnRunning proves the mid-turn prompt never clears
// Running, blocks starting a second turn, and restores the executing posture
// once answered.
func TestHostApprovalKeepsTurnRunning(t *testing.T) {
	pub := &observingPublisher{}
	pub.setSubscribers(true)
	sess := newHostApprovalSession(t, &askingPermManager{}, pub)

	sess.Lock()
	sess.Status = api.StatusExecuting
	sess.Running = true
	done := make(chan struct{})
	sess.done = done
	sess.Unlock()

	result := make(chan error, 1)
	go func() {
		_, err := sess.requestHostApproval(context.Background(), "turn.example.com", 443)
		result <- err
	}()

	var reqID string
	deadline := time.Now().Add(2 * time.Second)
	for reqID == "" && time.Now().Before(deadline) {
		for _, ev := range pub.recorded() {
			if e, ok := permissionRequested(ev); ok && len(e.Permissions) > 0 {
				reqID = e.Permissions[0].ID
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	if reqID == "" {
		t.Fatal("no prompt published")
	}

	sess.Lock()
	running := sess.Running
	status := sess.Status
	waiting := sess.waitingForInteractionLocked()
	sess.Unlock()
	if !running {
		t.Fatal("Running cleared while host approval pending mid-turn")
	}
	if status != api.StatusWaitingPermission {
		t.Fatalf("status during prompt = %s, want waiting_permission", status)
	}
	if !waiting {
		t.Fatal("waitingForInteractionLocked false while host approval pending")
	}
	// A plain user turn must not start on top of the pending approval.
	if startErr := sess.startTurn(harness.Input{Message: message.NewUser(&message.TextBlock{Text: "hello"})}); startErr == nil {
		t.Fatal("startTurn accepted a new user turn while a host approval is pending")
	}

	if _, err := sess.resolveHostApproval(api.PermissionDecision{RequestID: reqID, Kind: api.DecisionAllowOnce}); err != nil {
		t.Fatalf("resolveHostApproval: %v", err)
	}
	if err := <-result; err != nil {
		t.Fatalf("requestHostApproval: %v", err)
	}

	sess.Lock()
	running = sess.Running
	status = sess.Status
	sess.Unlock()
	if !running {
		t.Fatal("Running cleared after resolving a mid-turn approval")
	}
	if status != api.StatusExecuting {
		t.Fatalf("status after resolve = %s, want executing (prompting turn still live)", status)
	}

	// Simulate the turn goroutine finishing while nothing else is pending:
	// teardown preserves non-idle statuses, so it must land on idle itself.
	sess.Lock()
	sess.done = nil
	sess.Running = false
	sess.Unlock()
}

// TestHostApprovalSettlesIdleWhenTurnEnded covers the interleaving where the
// prompting turn ends before the user answers: teardown keeps the waiting
// status, and the last approval must settle the session to idle.
func TestHostApprovalSettlesIdleWhenTurnEnded(t *testing.T) {
	pub := &observingPublisher{}
	pub.setSubscribers(true)
	sess := newHostApprovalSession(t, &askingPermManager{}, pub)

	sess.Lock()
	sess.Status = api.StatusExecuting
	sess.Running = true
	done := make(chan struct{})
	sess.done = done
	sess.Unlock()

	result := make(chan error, 1)
	go func() {
		_, err := sess.requestHostApproval(context.Background(), "ended.example.com", 443)
		result <- err
	}()

	var reqID string
	deadline := time.Now().Add(2 * time.Second)
	for reqID == "" && time.Now().Before(deadline) {
		for _, ev := range pub.recorded() {
			if e, ok := permissionRequested(ev); ok && len(e.Permissions) > 0 {
				reqID = e.Permissions[0].ID
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	if reqID == "" {
		t.Fatal("no prompt published")
	}

	// The turn ends: mimic the turn goroutine defer, which keeps a waiting
	// status intact and clears done.
	sess.Lock()
	sess.Running = false
	sess.done = nil
	sess.Unlock()
	close(done)

	if _, err := sess.resolveHostApproval(api.PermissionDecision{RequestID: reqID, Kind: api.DecisionReject}); err != nil {
		t.Fatalf("resolveHostApproval: %v", err)
	}
	if err := <-result; err != nil {
		t.Fatalf("requestHostApproval: %v", err)
	}

	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		sess.Lock()
		status := sess.Status
		sess.Unlock()
		if status == api.StatusIdle {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	sess.Lock()
	status := sess.Status
	sess.Unlock()
	t.Fatalf("status after turn ended + approval resolved = %s, want idle", status)
}

func TestHostApprovalCloseReleasesWaiters(t *testing.T) {
	pub := &observingPublisher{}
	pub.setSubscribers(true)
	sess := newHostApprovalSession(t, &askingPermManager{}, pub)

	result := make(chan error, 1)
	go func() {
		_, err := sess.requestHostApproval(context.Background(), "closing.example.com", 443)
		result <- err
	}()

	var reqID string
	deadline := time.Now().Add(2 * time.Second)
	for reqID == "" && time.Now().Before(deadline) {
		for _, ev := range pub.recorded() {
			if e, ok := permissionRequested(ev); ok && len(e.Permissions) > 0 {
				reqID = e.Permissions[0].ID
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	if reqID == "" {
		t.Fatal("no prompt published")
	}

	_ = sess.Close()

	select {
	case err := <-result:
		if err == nil {
			t.Fatal("requestHostApproval returned success after Close")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not release the pending host approval")
	}
}

// TestHostApprovalWorkspaceGrantPersists verifies the "Allow for workspace"
// path: the host lands in the project sandbox.json allowedDomains and the
// session policy refresh picks the entry up.
func TestHostApprovalWorkspaceGrantPersists(t *testing.T) {
	workDir := t.TempDir()
	mgr, err := sandboxcfg.New(sandboxcfg.Config{
		WorkDir:     workDir,
		GlobalPath:  filepath.Join(t.TempDir(), "global-sandbox.json"),
		ProjectPath: filepath.Join(workDir, "sandbox.json"),
	})
	if err != nil {
		t.Fatal(err)
	}
	perm := &askingPermManager{}
	sess := newHostApprovalSession(t, perm, nil)
	sess.sandboxCfg = mgr

	result := make(chan error, 1)
	go func() {
		_, err := sess.requestHostApproval(context.Background(), "grant.example.com", 443)
		result <- err
	}()

	var reqID string
	deadline := time.Now().Add(2 * time.Second)
	pub := sess.publisher.(*observingPublisher)
	for reqID == "" && time.Now().Before(deadline) {
		for _, ev := range pub.recorded() {
			if e, ok := permissionRequested(ev); ok && len(e.Permissions) > 0 {
				reqID = e.Permissions[0].ID
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	if reqID == "" {
		t.Fatal("no prompt published")
	}

	handled, err := sess.resolveHostApproval(api.PermissionDecision{
		RequestID: reqID,
		Kind:      api.DecisionAllowSession,
		Scope:     api.ScopeProject,
	})
	if !handled {
		t.Fatal("decision not routed to host approval")
	}
	if err != nil {
		t.Fatalf("resolveHostApproval: %v", err)
	}
	if err := <-result; err != nil {
		t.Fatalf("requestHostApproval: %v", err)
	}

	got, err := mgr.Get(api.ScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range got.Network.AllowedDomains {
		if d == "grant.example.com" {
			found = true
		}
	}
	if !found {
		t.Fatalf("project sandbox.json allowedDomains = %v, want grant.example.com", got.Network.AllowedDomains)
	}

	// The permission engine must NOT receive the workspace grant (it went to
	// sandbox.json instead), keeping the two stores consistent.
	if applied := perm.appliedDecisions(); len(applied) != 0 {
		t.Fatalf("ApplyDecision calls = %+v, want none for workspace-scope grant", applied)
	}
}

func TestHostApprovalInvalidDecisionSurfaces(t *testing.T) {
	perm := &failingApplyPermManager{}
	sess := newHostApprovalSession(t, perm, nil)

	result := make(chan error, 1)
	go func() {
		_, err := sess.requestHostApproval(context.Background(), "fail.example.com", 443)
		result <- err
	}()

	pub := sess.publisher.(*observingPublisher)
	var reqID string
	deadline := time.Now().Add(2 * time.Second)
	for reqID == "" && time.Now().Before(deadline) {
		for _, ev := range pub.recorded() {
			if e, ok := permissionRequested(ev); ok && len(e.Permissions) > 0 {
				reqID = e.Permissions[0].ID
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	if reqID == "" {
		t.Fatal("no prompt published")
	}

	_, err := sess.resolveHostApproval(api.PermissionDecision{RequestID: reqID, Kind: api.DecisionAllowSession})
	if err == nil || !strings.Contains(err.Error(), "apply boom") {
		t.Fatalf("resolveHostApproval error = %v, want wrapped ApplyDecision failure", err)
	}
	if proxyErr := <-result; proxyErr == nil || !strings.Contains(proxyErr.Error(), "apply boom") {
		t.Fatalf("proxy callback error = %v, want wrapped ApplyDecision failure", proxyErr)
	}
}

// failingApplyPermManager asks, then fails to persist the decision.
type failingApplyPermManager struct {
	askingPermManager
}

func (m *failingApplyPermManager) ApplyDecision(req *api.PermissionRequest, decision api.PermissionDecision) error {
	return errors.New("apply boom")
}
