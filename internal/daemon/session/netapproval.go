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
	"github.com/masterkeysrd/tasksmith/internal/daemon/permreview"
	"github.com/masterkeysrd/tasksmith/internal/daemon/scope"
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

// hostReviewFlight coalesces concurrent Phase-1 auto-mode decision-model
// reviews for the same host:port before any human prompt is created. It never
// enters s.hostApprovals or surfaces in State.Clone.
type hostReviewFlight struct {
	hostKey string
	done    chan struct{}
	outcome hostReviewOutcome
}

type hostReviewOutcome struct {
	approved bool
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

// hostApprovalReviewTimeout bounds the silent decision-model review that
// precedes a human host prompt in auto mode. The full hostApprovalTimeout
// still applies to the human prompt after a deferral.
const hostApprovalReviewTimeout = 5 * time.Second

// requestHostApproval is the sandbox proxy's OnUnlistedHost callback. It runs
// the synthetic net(host) plan through the session permission engine: rules
// and mode decide silently where they can (an existing net(host) allow rule,
// auto mode, plan mode, deny rules), and only a genuine ask reaches the user.
// In auto mode a risky ask first gets one silent decision-model review; only
// a deferral prompts the human. With no interactive subscriber — or after the
// approval timeout — the host is refused immediately.
//
// Host approvals run in the middle of an active turn (the harness is blocked
// inside a sandboxed subprocess), so the prompt status must never touch
// Running: the turn goroutine still owns the turn lifecycle and restores the
// session status when it finishes.
func (s *Session) requestHostApproval(ctx context.Context, host string, port int) (bool, error) {
	s.Lock()
	perm := s.PermManager
	prior := s.Status
	turnToken := s.done
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

	// Phase 1: in auto mode, coalesce concurrent same-host connections on a
	// silent decision-model review without touching s.hostApprovals or status.
	if perm.Mode() == api.ModeAuto && s.permReviewer != nil {
		if handled, allow, reviewErr := s.tryAutoHostReview(ctx, perm, *req, plan, host, port); handled {
			return allow, reviewErr
		}
	}

	// Phase 2: human prompt flow.
	if !s.interactive() {
		return false, fmt.Errorf("sandbox: host %q is not in the network allowlist and no client is attached to approve it", host)
	}

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

// tryAutoHostReview runs or joins a silent Phase-1 auto-mode review for
// host:port. It returns handled=true when the verdict is final (approved,
// canceled, closing, or errored); when deferred, it returns handled=false so
// callers proceed into the human prompt path.
func (s *Session) tryAutoHostReview(ctx context.Context, perm permissions.SessionManager, req api.PermissionRequest, plan *tools.Plan, host string, port int) (handled bool, allow bool, err error) {
	key := fmt.Sprintf("%s|%d", host, port)
	s.Lock()
	if s.closing {
		s.Unlock()
		return true, false, errors.New("sandbox: session is closing")
	}
	for _, w := range s.hostApprovals {
		if w.hostKey == key {
			s.Unlock()
			return false, false, nil
		}
	}
	if s.hostReviews != nil {
		if existing := s.hostReviews[key]; existing != nil {
			s.Unlock()
			select {
			case <-existing.done:
				if existing.outcome.err != nil {
					return true, false, existing.outcome.err
				}
				if existing.outcome.approved {
					return true, true, nil
				}
				return false, false, nil
			case <-ctx.Done():
				return true, false, ctx.Err()
			}
		}
	}
	if s.hostReviews == nil {
		s.hostReviews = make(map[string]*hostReviewFlight)
	}
	if id, idErr := uuid.NewV7(); idErr == nil {
		req.ID = id.String()
	}
	flight := &hostReviewFlight{
		hostKey: key,
		done:    make(chan struct{}),
	}
	s.hostReviews[key] = flight
	s.Unlock()

	receipt := s.reviewHostApproval(ctx, perm, req, plan)
	if ctxErr := ctx.Err(); ctxErr != nil {
		s.finishHostReviewFlight(key, flight, hostReviewOutcome{err: ctxErr})
		return true, false, ctxErr
	}
	s.Lock()
	closing := s.closing
	s.Unlock()
	if closing {
		closeErr := errors.New("sandbox: session is closing")
		s.finishHostReviewFlight(key, flight, hostReviewOutcome{err: closeErr})
		return true, false, closeErr
	}
	if receipt == nil {
		s.finishHostReviewFlight(key, flight, hostReviewOutcome{approved: false})
		return false, false, nil
	}

	dec := api.PermissionDecision{RequestID: req.ID, Kind: api.DecisionAllowOnce}
	if applyErr := perm.ApplyDecision(&req, dec); applyErr != nil {
		wrapErr := fmt.Errorf("session: apply host approval decision: %w", applyErr)
		s.finishHostReviewFlight(key, flight, hostReviewOutcome{err: wrapErr})
		return true, false, wrapErr
	}
	if s.Timeline != nil {
		entry := timeline.NewPermissionEntry(api.PermissionRecord{
			Request:  req,
			Decision: dec,
			Review:   receipt,
		})
		if tlErr := s.appendTimeline(s.bgCtx, entry); tlErr != nil {
			s.finishHostReviewFlight(key, flight, hostReviewOutcome{err: tlErr})
			return true, false, tlErr
		}
	}
	s.finishHostReviewFlight(key, flight, hostReviewOutcome{approved: true})
	return true, true, nil
}

func (s *Session) finishHostReviewFlight(key string, flight *hostReviewFlight, outcome hostReviewOutcome) {
	s.Lock()
	if s.hostReviews != nil && s.hostReviews[key] == flight {
		delete(s.hostReviews, key)
	}
	s.Unlock()
	flight.outcome = outcome
	close(flight.done)
}

// reviewHostApproval runs the auto-mode decision-model review for a pending
// host card. It returns the receipt only when the verdict survives post-
// inference liveness and policy re-checks; every other path returns nil so
// the caller defers to the human flow. The reviewer calls the gateway
// in-process, never the sandbox proxy, so reviews cannot self-recurse.
func (s *Session) reviewHostApproval(ctx context.Context, perm permissions.SessionManager, req api.PermissionRequest, plan *tools.Plan) *api.PermissionReview {
	if s.permReviewer == nil || perm.Mode() != api.ModeAuto {
		return nil
	}
	s.Lock()
	agentName := s.Config.Agent
	var wsRoot string
	if s.workspace != nil {
		wsRoot = s.workspace.RootDir()
	}
	s.Unlock()
	scopedCtx := scope.With(ctx, scope.Scope{
		SessionID:       s.ID,
		ParentSessionID: s.ParentID,
		WorkspaceRoot:   wsRoot,
		Agent:           agentName,
	})
	reviewCtx, cancel := context.WithTimeout(scopedCtx, hostApprovalReviewTimeout)
	defer cancel()
	res := s.permReviewer.Review(reviewCtx, permreview.ReviewRequest{
		Request:         req,
		InsideWorkspace: plan.InsideWorkspace,
		ProtectedPath:   plan.ProtectedPath,
		ScriptPaths:     plan.ScriptPaths,
		UserIntent:      s.lastTurnUserIntent(),
	})
	if res.WarnOnce != "" {
		s.logger.Warn(res.WarnOnce)
	}
	if res.Verdict != permreview.VerdictApproveOnce || res.Receipt == nil {
		return nil
	}
	s.Lock()
	closing := s.closing
	s.Unlock()
	if closing || ctx.Err() != nil || perm.Mode() != api.ModeAuto {
		return nil
	}
	if recheck, _, err := perm.Evaluate(plan); err != nil || recheck == tools.AccessDeny {
		return nil
	}
	return res.Receipt
}

// lastTurnUserIntent returns the initiating user prompt text of the active
// turn from the harness checkpoint, for the reviewer's user_intent field.
func (s *Session) lastTurnUserIntent() string {
	if s.Harness == nil {
		return ""
	}
	state, err := s.Harness.LoadState(s.bgCtx)
	if err != nil {
		return ""
	}
	return state.LastUserIntentText()
}

// registerHostApproval records the pending approval. It returns the new owner
// waiter, or an existing waiter for the same host:port (single-flight) so a
// burst of parallel connections produces at most one review and, after a
// deferral, at most one prompt. Both returns are nil once the session is
// closing. Registered entries are silent until beginHostApprovalPrompt flips
// them to the human-prompt phase.
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
	return w, nil
}

// beginHostApprovalPrompt counts the prompt as active and switches the session
// to a permission-waiting status without disturbing Running — an in-flight
// turn keeps executing (the harness is blocked inside the sandboxed
// subprocess) and must remain visible as such to Enqueue, startTurn, and
// reconnecting clients.
func (s *Session) beginHostApprovalPrompt(prior api.SessionStatus) {
	s.Lock()
	s.hostApprovalActive++
	s.Unlock()
	if prior == api.StatusWaitingPermission || prior == api.StatusWaitingQuestion || prior == api.StatusWaitingPlanReview {
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
	if next == api.StatusWaitingPermission || next == api.StatusWaitingQuestion || next == api.StatusWaitingPlanReview {
		// Another prompt predates ours; leave the waiting posture intact.
	} else if len(s.hostApprovals) > 0 || len(s.PendingPermissions) > 0 {
		next = api.StatusWaitingPermission
	} else if len(s.PendingQuestions) > 0 {
		next = api.StatusWaitingQuestion
	} else if len(s.PendingPlanReviews) > 0 {
		next = api.StatusWaitingPlanReview
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
