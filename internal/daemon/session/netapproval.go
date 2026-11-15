package session

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/permissions"
	"github.com/masterkeysrd/tasksmith/internal/daemon/timeline"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
)

// hostApprovalTimeout bounds how long the sandbox network proxy waits for a
// user decision on an unlisted host before refusing the connection.
const hostApprovalTimeout = 2 * time.Minute

// hostApprovalSettleTimeout bounds how long a client decision call waits for
// the owning proxy goroutine to persist the grant and finalize the prompt.
const hostApprovalSettleTimeout = 30 * time.Second

// hostApprovalNetTool names synthetic permission plans minted by the
// sandbox network proxy for unlisted hosts. Session-scope grants land as
// net(host) rules in the regular permission rule namespace; workspace- and
// user-scope grants persist to the corresponding sandbox.json network
// allowedDomains instead (see persistHostGrant).
const hostApprovalNetTool = "net"

// hostApproval is one in-flight user prompt for an unlisted network host.
// The first proxy connection for a host owns the prompt (requestsStatus /
// decided); later connections to the same host ride along on done.
type hostApproval struct {
	request api.PermissionRequest
	hostKey string
	decided chan hostApprovalOutcome

	done    chan struct{}
	outcome hostApprovalOutcome
}

type hostApprovalOutcome struct {
	decision api.PermissionDecision
	err      error
}

// topicObserver is an optional Publisher capability: whether any client is
// currently subscribed to a topic. The REPL streams every session it drives,
// so a session with no subscribers has nobody to answer a prompt.
type topicObserver interface {
	HasSubscribers(topic string) bool
}

// interactive reports whether a live client could answer permission prompts
// for this session right now.
func (s *Session) interactive() bool {
	if ob, ok := s.publisher.(topicObserver); ok {
		return ob.HasSubscribers(api.SessionStreamTopic(s.ID))
	}
	return true
}

// requestHostApproval is the sandbox proxy's OnUnlistedHost callback. It runs
// the synthetic net(host) plan through the session permission engine: rules
// and mode decide silently where they can (an existing net(host) allow rule,
// auto mode, plan mode, deny rules), and only a genuine ask reaches the user.
// With no interactive subscriber — or after the approval timeout — the host
// is refused immediately.
//
// Host approvals run in the middle of an active turn (the harness is blocked
// inside a sandboxed subprocess), so the prompt status must never touch
// Running: the turn goroutine still owns the turn lifecycle and restores the
// session status when it finishes.
func (s *Session) requestHostApproval(ctx context.Context, host string, port int) (bool, error) {
	s.Lock()
	perm := s.PermManager
	s.Unlock()
	if perm == nil {
		return false, errors.New("session: permission manager unavailable for host approval")
	}

	plan := &tools.Plan{
		ToolName: hostApprovalNetTool,
		Action:   api.ActionNet,
		Safety:   api.SafetyOutside,
		Target:   host,
		Summary:  fmt.Sprintf("Sandboxed command wants to reach %s (port %d), which is not in the network allowlist.", host, port),
	}

	access, req, err := perm.Evaluate(plan)
	if err != nil {
		return false, fmt.Errorf("session: evaluate host approval for %s: %w", host, err)
	}
	switch access {
	case tools.AccessAllow:
		return true, nil
	case tools.AccessDeny:
		return false, nil
	}
	if req == nil {
		return false, fmt.Errorf("session: host approval ask for %s produced no request", host)
	}

	if !s.interactive() {
		return false, fmt.Errorf("sandbox: host %q is not in the network allowlist and no client is attached to approve it", host)
	}

	s.Lock()
	prior := s.Status
	turnToken := s.done
	s.Unlock()

	owner, shared := s.registerHostApproval(*req, host, port)
	if owner == nil {
		if shared == nil {
			return false, errors.New("sandbox: session is closing")
		}
		return awaitSharedApproval(ctx, shared)
	}
	s.beginHostApprovalPrompt(prior)
	s.publisher.Publish(api.SessionStreamTopic(s.ID), api.PermissionRequestedEvent{
		SessionID:   s.ID,
		Permissions: []api.PermissionRequest{owner.request},
	})

	allowed, finishErr := s.ownHostApproval(ctx, owner)
	s.finishHostApprovalPrompt(prior, turnToken)
	return allowed, finishErr
}

// registerHostApproval records the pending approval. It returns the new owner
// waiter, or an existing waiter for the same host:port (single-flight) so a
// burst of parallel connections produces at most one prompt. Both returns are
// nil once the session is closing.
func (s *Session) registerHostApproval(req api.PermissionRequest, host string, port int) (*hostApproval, *hostApproval) {
	s.Lock()
	defer s.Unlock()
	if s.closing {
		return nil, nil
	}
	key := fmt.Sprintf("%s|%d", host, port)
	for _, w := range s.hostApprovals {
		if w.hostKey == key {
			return nil, w
		}
	}
	if s.hostApprovals == nil {
		s.hostApprovals = make(map[string]*hostApproval)
	}
	if id, err := uuid.NewV7(); err == nil {
		req.ID = id.String()
	}
	w := &hostApproval{
		request: req,
		hostKey: key,
		decided: make(chan hostApprovalOutcome, 1),
		done:    make(chan struct{}),
	}
	s.hostApprovals[req.ID] = w
	s.hostApprovalActive++
	return w, nil
}

// beginHostApprovalPrompt switches the session to a permission-waiting status
// without disturbing Running: an in-flight turn keeps executing (the harness
// is blocked inside the sandboxed subprocess) and must remain visible as such
// to Enqueue, startTurn, and reconnecting clients.
func (s *Session) beginHostApprovalPrompt(prior api.SessionStatus) {
	if prior == api.StatusWaitingPermission || prior == api.StatusWaitingQuestion {
		return
	}
	s.Lock()
	s.Status = api.StatusWaitingPermission
	s.Unlock()
	s.publishHostApprovalStatus(api.StatusWaitingPermission)
}

// finishHostApprovalPrompt decrements the active-prompt count and, once the
// last prompt resolves, restores the pre-prompt status. If the turn that was
// live when the prompt started has since finished, its teardown preserved the
// waiting status, so settle the session to idle here.
func (s *Session) finishHostApprovalPrompt(prior api.SessionStatus, turnToken chan struct{}) {
	s.Lock()
	if s.hostApprovalActive > 0 {
		s.hostApprovalActive--
	}
	if s.hostApprovalActive > 0 {
		s.Unlock()
		return
	}
	next := prior
	if next == api.StatusWaitingPermission || next == api.StatusWaitingQuestion {
		// Another prompt predates ours; leave the waiting posture intact.
	} else if len(s.hostApprovals) > 0 || len(s.PendingPermissions) > 0 {
		next = api.StatusWaitingPermission
	} else if len(s.PendingQuestions) > 0 {
		next = api.StatusWaitingQuestion
	} else if turnToken == nil || turnToken != s.done {
		// The prompting turn ended (its teardown preserved the waiting
		// status); settle the session back to idle.
		next = api.StatusIdle
	}
	// Otherwise the prompting turn is still live and prior (e.g. executing)
	// is the correct posture to restore: the subprocess continues running.
	s.Status = next
	s.Unlock()
	s.publishHostApprovalStatus(next)
}

// publishHostApprovalStatus notifies subscribers of a host-approval-driven
// status transition without going through setStatus (which derives Running).
func (s *Session) publishHostApprovalStatus(status api.SessionStatus) {
	s.publisher.Publish(api.SessionStreamTopic(s.ID), api.StatusChangedEvent{
		SessionID: s.ID,
		Status:    status,
	})
	s.notifyParentUpdate()
}

// ownHostApproval waits for the user's decision (or the approval timeout),
// applies any persistent grant, records the timeline entry, and publishes the
// outcome for shared waiters.
func (s *Session) ownHostApproval(ctx context.Context, w *hostApproval) (bool, error) {
	timer := time.NewTimer(hostApprovalTimeout)
	defer timer.Stop()

	var outcome hostApprovalOutcome
	select {
	case outcome = <-w.decided:
	case <-timer.C:
		outcome = hostApprovalOutcome{err: fmt.Errorf("sandbox: host approval for %s timed out", w.request.Target)}
	case <-ctx.Done():
		outcome = hostApprovalOutcome{err: ctx.Err()}
	}
	return s.finishHostApproval(w, outcome)
}

// finishHostApproval finalizes the approval exactly once: it removes the
// waiter from the pending map, persists the grant, records the timeline
// entry, then releases shared waiters with the same outcome.
func (s *Session) finishHostApproval(w *hostApproval, outcome hostApprovalOutcome) (bool, error) {
	s.Lock()
	alive := false
	if s.hostApprovals != nil {
		_, alive = s.hostApprovals[w.request.ID]
		delete(s.hostApprovals, w.request.ID)
	}
	perm := s.PermManager
	s.Unlock()
	if !alive {
		<-w.done
		return approvalVerdict(w.outcome)
	}

	if outcome.err == nil {
		dec := outcome.decision
		dec.RequestID = w.request.ID
		if dec.Kind == api.DecisionAllowSession && dec.Scope == "" {
			dec.Scope = api.ScopeSession
		}
		outcome.decision = dec
		if err := s.applyHostGrant(perm, w, dec); err != nil {
			outcome = hostApprovalOutcome{err: err}
		}
		if outcome.err == nil && s.Timeline != nil {
			entry := timeline.NewPermissionEntry(api.PermissionRecord{
				Request:  w.request,
				Decision: outcome.decision,
			})
			if err := s.appendTimeline(s.bgCtx, entry); err != nil {
				outcome = hostApprovalOutcome{err: err}
			}
		}
	}

	w.outcome = outcome
	close(w.done)
	return approvalVerdict(outcome)
}

// applyHostGrant persists the user's verdict. Workspace- and user-scope
// grants extend the corresponding sandbox.json network.allowedDomains (the
// spec's "Allow for workspace" semantics) and live-refresh the proxy policy;
// one-shot and session-scope grants stay in-memory as net(host) permission
// rules so they evaporate with the session.
func (s *Session) applyHostGrant(perm permissions.SessionManager, w *hostApproval, dec api.PermissionDecision) error {
	if dec.Kind == api.DecisionAllowSession && (dec.Scope == api.ScopeProject || dec.Scope == api.ScopeGlobal) {
		if err := s.persistHostGrant(w.request.Target, dec.Scope); err != nil {
			return fmt.Errorf("session: persist host grant for %s: %w", w.request.Target, err)
		}
		s.refreshSandboxPolicy(s.bgCtx)
		return nil
	}
	if perm != nil {
		if err := perm.ApplyDecision(&w.request, dec); err != nil {
			return fmt.Errorf("session: apply host approval decision: %w", err)
		}
	}
	return nil
}

// persistHostGrant appends one host to the given scope's sandbox.json
// network.allowedDomains, leaving the document untouched when the entry
// already exists.
func (s *Session) persistHostGrant(host string, scope api.PermissionScope) error {
	s.Lock()
	cfgMgr := s.sandboxCfg
	s.Unlock()
	if cfgMgr == nil {
		return errors.New("sandbox settings manager unavailable")
	}
	h := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	if h == "" || strings.ContainsAny(h, " \t/@\\") {
		return fmt.Errorf("invalid host %q", host)
	}
	if strings.HasPrefix(h, "[") || strings.Contains(h, ":") {
		literal := strings.TrimSuffix(strings.TrimPrefix(h, "["), "]")
		if strings.HasPrefix(h, "[") && !strings.HasSuffix(h, "]") {
			return fmt.Errorf("invalid IP literal %q", host)
		}
		if net.ParseIP(literal) == nil {
			return fmt.Errorf("invalid IP literal %q", host)
		}
		h = literal
	}

	settings, err := cfgMgr.Get(scope)
	if err != nil {
		return err
	}
	for _, d := range settings.Network.AllowedDomains {
		if strings.EqualFold(strings.TrimSuffix(strings.TrimSpace(d), "."), h) {
			return nil
		}
	}
	settings.Network.AllowedDomains = append(slices.Clone(settings.Network.AllowedDomains), h)
	return cfgMgr.Set(scope, settings)
}

// cancelHostApprovals releases every in-flight host approval with an error:
// owners wake from their wait, resolve through the normal finalize path, and
// the session status settles back from the prompt posture.
func (s *Session) cancelHostApprovals(reason error) {
	s.Lock()
	waiters := make([]*hostApproval, 0, len(s.hostApprovals))
	for _, w := range s.hostApprovals {
		waiters = append(waiters, w)
	}
	s.Unlock()
	for _, w := range waiters {
		select {
		case w.decided <- hostApprovalOutcome{err: reason}:
		default:
		}
	}
}

// approvalVerdict maps a resolved outcome to the proxy's allow/refuse answer.
func approvalVerdict(out hostApprovalOutcome) (bool, error) {
	if out.err != nil {
		return false, out.err
	}
	return out.decision.Kind == api.DecisionAllowOnce || out.decision.Kind == api.DecisionAllowSession, nil
}

// awaitSharedApproval waits for the owner of a same-host prompt to publish
// its outcome.
func awaitSharedApproval(ctx context.Context, w *hostApproval) (bool, error) {
	timer := time.NewTimer(hostApprovalTimeout)
	defer timer.Stop()
	select {
	case <-w.done:
		return approvalVerdict(w.outcome)
	case <-timer.C:
		return false, fmt.Errorf("sandbox: host approval for %s timed out", w.request.Target)
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

// resolveHostApproval routes a client decision to a pending host approval and
// waits for the owning proxy goroutine to finalize it, so ApplyDecision
// failures reach the client instead of being swallowed. Handled reports
// whether the decision belonged to a host approval at all; unhandled
// decisions flow to the harness-backed permission resume path.
func (s *Session) resolveHostApproval(decision api.PermissionDecision) (bool, error) {
	s.Lock()
	var waiter *hostApproval
	if s.hostApprovals != nil {
		if decision.RequestID != "" {
			waiter = s.hostApprovals[decision.RequestID]
		} else if decision.ToolCallID != "" {
			for _, w := range s.hostApprovals {
				if w.request.ToolCallID == decision.ToolCallID {
					waiter = w
					break
				}
			}
		}
	}
	s.Unlock()
	if waiter == nil {
		return false, nil
	}
	select {
	case waiter.decided <- hostApprovalOutcome{decision: decision}:
	default:
		// Already answered.
	}

	timer := time.NewTimer(hostApprovalSettleTimeout)
	defer timer.Stop()
	select {
	case <-waiter.done:
		if waiter.outcome.err != nil {
			return true, waiter.outcome.err
		}
		return true, nil
	case <-timer.C:
		return true, fmt.Errorf("session: host approval decision was recorded but did not finalize in time")
	}
}
