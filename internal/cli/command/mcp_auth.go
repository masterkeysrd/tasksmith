package command

import (
	"context"
	"strings"
	"time"

	"github.com/masterkeysrd/kite/extras/kites"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
)

// mcpAuthState is the observable state of one interactive authorization. It lives in
// a [kites.Store] rather than in component state because the flow runs on a
// background goroutine while two components care about it: the browser, which closes
// the slot and writes the timeline when the flow settles, and the pane, which renders
// it and accepts a pasted callback.
type mcpAuthState struct {
	server  string
	authURL string
	status  string
	outcome string
	settled bool
	failed  bool
}

// mcpAuthFlow drives one authorization and publishes its progress.
type mcpAuthFlow struct {
	state  *kites.Store[mcpAuthState]
	server string
	stop   context.CancelFunc
}

// newMCPAuthStore creates the store one browser instance uses for its flow.
func newMCPAuthStore() *kites.Store[mcpAuthState] {
	return kites.Create(mcpAuthState{})
}

// startMCPAuth begins authorization for the named server and reports its progress on
// a background goroutine. Cancelling the returned flow stops this client from
// reporting on the authorization; it does not abort it, because the daemon holds the
// callback open on a timer of its own.
func startMCPAuth(server string, state *kites.Store[mcpAuthState]) *mcpAuthFlow {
	if state == nil {
		panic("startMCPAuth requires a state store")
	}
	ctx, cancel := context.WithCancel(context.Background())
	flow := &mcpAuthFlow{state: state, server: server, stop: cancel}

	flow.update(func(next *mcpAuthState) { *next = mcpAuthState{server: server} })
	goMCPBackground(func() {
		startCtx, cancel := context.WithTimeout(ctx, mcpAuthPollTimeout)
		outcome := mcpStartAuth(startCtx, server)
		cancel()
		if outcome.Settled {
			flow.finish(outcome)
			return
		}
		flow.update(func(next *mcpAuthState) {
			next.authURL = outcome.AuthURL
			next.status = outcome.Message
		})
		flow.watch(ctx)
	})
	return flow
}

// watch polls the listing until the server reports its new state or the reporting
// budget runs out. Running out is deliberately not a failure: the daemon keeps its
// callback open far longer than the terminal is willing to chatter, and someone who
// finishes a slow login still needs this screen to hold the URL and the paste field.
func (f *mcpAuthFlow) watch(ctx context.Context) {
	deadline := time.Now().Add(mcpAuthPollTimeout)
	for {
		if !mcpAuthWait(ctx, mcpAuthPollInterval) {
			return
		}
		// A pasted callback can settle the flow while this loop is between polls,
		// and a settled flow has nothing left to watch for.
		if f.state.Get().settled {
			return
		}
		outcome := mcpCheckAuth(ctx, f.server)
		if outcome.Settled {
			f.finish(outcome)
			return
		}
		if outcome.Message != "" {
			f.update(func(next *mcpAuthState) { next.status = outcome.Message })
		}
		if !time.Now().Before(deadline) {
			f.update(func(next *mcpAuthState) {
				next.status = mcpAuthTimeoutMessage(f.server)
			})
			return
		}
	}
}

// mcpAuthWait pauses between polls and reports whether the full interval elapsed. A
// wait cut short means the user left, so reporting stops without a parting message.
func mcpAuthWait(ctx context.Context, interval time.Duration) bool {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// update publishes one change. Every writer goes through here so a pasted callback
// cannot clobber the progress a poll just reported.
func (f *mcpAuthFlow) update(mutate func(*mcpAuthState)) {
	f.state.Set(func(current mcpAuthState) mcpAuthState {
		next := current
		mutate(&next)
		return next
	})
}

// finish publishes the terminal state of a flow.
func (f *mcpAuthFlow) finish(outcome mcpAuthOutcome) {
	f.update(func(next *mcpAuthState) {
		if outcome.Message != "" {
			next.outcome = outcome.Message
			next.status = outcome.Message
		}
		if outcome.AuthURL != "" {
			next.authURL = outcome.AuthURL
		}
		next.settled = true
		next.failed = outcome.Failed
	})
}

// cancel stops reporting on the flow.
func (f *mcpAuthFlow) cancel() {
	f.stop()
}

// submit hands a pasted redirect URL to the daemon so the flow it still holds open
// can finish without the browser ever reaching this machine, then keeps watching,
// because the token exchange has to complete before the server reports connected.
func (f *mcpAuthFlow) submit(ctx context.Context, callbackURL string) {
	f.update(func(next *mcpAuthState) {
		next.status = "Submitting the callback for " + f.server + "…"
	})
	goMCPBackground(func() {
		outcome := mcpSubmitAuthCallback(ctx, f.server, callbackURL)
		if outcome.Settled {
			f.finish(outcome)
			return
		}
		f.update(func(next *mcpAuthState) {
			next.status = outcome.Message
		})
		if !f.state.Get().settled {
			f.watch(ctx)
		}
	})
}

// mcpAuthTimelineReport records how an authorization ended in the conversation
// timeline, where it can still be read and copied after the browser closes. A notice
// in the browser's title disappears with it, which is no use to someone retrying a
// failed login.
func mcpAuthTimelineReport(state mcpAuthState) {
	if !state.settled {
		return
	}
	message := strings.TrimSpace(state.outcome)
	if message == "" {
		return
	}
	if state.failed {
		store.AppendError("MCP", message)
		return
	}
	store.AppendCommandOutput("/mcp", message)
}
