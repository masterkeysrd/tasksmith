package command

import (
	"cmp"
	"context"
	"strings"
	"time"

	"github.com/masterkeysrd/kite/dom"
	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kites"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/ui/clipboard"
	uicomponents "github.com/masterkeysrd/tasksmith/internal/ui/components"
	"github.com/masterkeysrd/tasksmith/internal/ui/styles"
)

// The authorization screen says a browser is about to open rather than that it has,
// because whether one opens is the desktop's decision, not this screen's.
const (
	mcpAuthNotice     = "A browser window will open for authentication"
	mcpAuthCopyHint   = "If your browser doesn't open automatically, copy this URL manually (c to copy)"
	mcpAuthPasteHint  = "If the redirect page shows a connection error, paste the URL from your browser's address bar:"
	mcpAuthReturnHint = "Return here after authenticating in your browser. Press Esc to go back."
	mcpAuthSpinner    = "✻"
)

// mcpAuthCopiedNotice confirms a copy, the one action on this screen the terminal
// gives no visible feedback for.
const mcpAuthCopiedNotice = "Copied authorization URL to clipboard."

// mcpAuthState is the observable state of one interactive authorization. It lives in
// a [kites.Store] rather than in component state because the flow runs on a
// background goroutine while two components care about it: the browser, which closes
// the slot and writes the timeline when the flow settles, and the pane, which renders
// it and accepts a pasted callback.
type mcpAuthState struct {
	server  string
	authURL string
	// notice carries the flow's progress. The screen has no room for it and it is
	// worthless once the browser closes, so the browser keeps it in the timeline.
	notice string
	// report is what the screen shows the user prominently: a copy confirmation, a
	// rejected paste, or the final result.
	report        string
	settled       bool
	failed        bool
	leavesBrowser bool
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

// mcpReadAuth reads a flow's current state from outside the render cycle.
func mcpReadAuth(state *kites.Store[mcpAuthState]) mcpAuthState {
	if state == nil {
		return mcpAuthState{}
	}
	return state.Get()
}

// startMCPAuth begins authorization for the named server and reports its progress on
// a background goroutine. Cancelling the returned flow stops this client from
// reporting on the authorization; it does not abort it, because the daemon holds the
// callback open on a timer of its own.
func startMCPAuth(server string, state *kites.Store[mcpAuthState]) *mcpAuthFlow {
	ctx, cancel := context.WithCancel(context.Background())
	flow := &mcpAuthFlow{state: state, server: server, stop: cancel}
	if state == nil {
		cancel()
		return flow
	}

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
			next.notice = outcome.Message
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
		if mcpReadAuth(f.state).settled {
			return
		}
		outcome := mcpCheckAuth(ctx, f.server)
		if outcome.Settled {
			f.finish(outcome)
			return
		}
		if outcome.Message != "" {
			f.update(func(next *mcpAuthState) { next.notice = outcome.Message })
		}
		if !time.Now().Before(deadline) {
			f.update(func(next *mcpAuthState) {
				next.report = mcpAuthTimeoutMessage(f.server)
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
	if f == nil || f.state == nil {
		return
	}
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
			next.notice = outcome.Message
			next.report = outcome.Message
		}
		if outcome.AuthURL != "" {
			next.authURL = outcome.AuthURL
		}
		next.settled = true
		next.failed = outcome.Failed
		next.leavesBrowser = outcome.LeavesBrowser
	})
}

// cancel stops reporting on the flow.
func (f *mcpAuthFlow) cancel() {
	if f == nil || f.stop == nil {
		return
	}
	f.stop()
}

// submit hands a pasted redirect URL to the daemon so the flow it still holds open
// can finish without the browser ever reaching this machine, then keeps watching,
// because the token exchange has to complete before the server reports connected.
func (f *mcpAuthFlow) submit(ctx context.Context, callbackURL string) {
	if f == nil || f.state == nil {
		return
	}
	f.update(func(next *mcpAuthState) {
		next.report = "Submitting the callback for " + f.server + "…"
	})
	goMCPBackground(func() {
		outcome := mcpSubmitAuthCallback(ctx, f.server, callbackURL)
		if outcome.Settled {
			f.finish(outcome)
			return
		}
		f.update(func(next *mcpAuthState) {
			next.notice = outcome.Message
			next.report = outcome.Message
		})
		if !mcpReadAuth(f.state).settled {
			f.watch(ctx)
		}
	})
}

// mcpAuthCopyKey reports whether a key press should copy the authorization URL. The
// ctrl+y chord always does. A bare `c` does only while the paste field holds nothing
// but that keystroke: the terminal writes a printable key into the focused field
// before any handler sees it, so the letter being typed is what an empty field looks
// like here, and a URL already being typed keeps its letters.
func mcpAuthCopyKey(k event.KeyEvent, pasted string) bool {
	if k.Mod&key.ModCtrl != 0 {
		return k.MatchString("ctrl+y")
	}
	if !k.MatchString("c") {
		return false
	}
	trimmed := strings.TrimSpace(pasted)
	return trimmed == "" || trimmed == "c"
}

type mcpAuthPaneProps struct {
	livePaneProps
	server      string
	title       string
	escLabel    string
	state       *kites.Store[mcpAuthState]
	browserOnly bool
	// notice is a report owned by the browser rather than the flow, such as the
	// confirmation of a copy. It shows in place of the flow's own report.
	notice string
	// onCopy reports a successful copy so the confirmation can live in the browser
	// rather than in this view, which unmounts as soon as the user leaves.
	onCopy func()
	// onSubmit hands a pasted callback URL to the waiting authorization flow.
	onSubmit func(callbackURL string)
	onBack   func()
}

// mcpAuthPane shows an authorization in progress: the page to open, a way to copy it
// when no browser appeared, and a field to paste the redirect back when the browser
// could not reach the daemon's callback listener.
var mcpAuthPane = trail.FC("MCPServerAuth", func(props mcpAuthPaneProps) trail.Node {
	common := styles.UseCommon()
	doc := kitex.UseDocument()
	winSize := trail.UseWindowSize()
	value, setValue := kitex.UseState("")
	inputRef := kitex.UseRef[dom.Element](nil)
	trail.UseFocus(inputRef, trail.FocusOptions{AutoFocus: true, IsActive: !props.browserOnly})

	state := kites.Use(props.state, func(s mcpAuthState) mcpAuthState { return s })
	server := cmp.Or(strings.TrimSpace(props.server), state.server)
	width := winSize.Width - mcpCardIndent
	title := cmp.Or(strings.TrimSpace(props.title), "Authenticating with "+server+"…")
	report := cmp.Or(strings.TrimSpace(props.notice), strings.TrimSpace(state.report))
	trail.UseInput(func(_ string, k event.KeyEvent) {
		if !mcpAuthCopyKey(k, value()) || !clipboard.WriteText(doc(), state.authURL) {
			return
		}
		if strings.TrimSpace(value()) == "c" {
			setValue("")
		}
		if props.onCopy != nil {
			props.onCopy()
		}
	}, trail.InputOptions{IsActive: !props.browserOnly})
	bindings := uicomponents.UseKeyMap([]uicomponents.KeyAction{
		{Hint: "c", Label: "Copy URL", Disabled: props.browserOnly},
		{Keys: []string{"enter"}, Label: "Submit pasted URL", Disabled: props.browserOnly, OnPress: func() {
			if pasted := strings.TrimSpace(value()); pasted != "" && props.onSubmit != nil {
				setValue("")
				props.onSubmit(pasted)
			}
		}},
		{Keys: []string{"esc", "escape"}, Label: cmp.Or(strings.TrimSpace(props.escLabel), "Back"), OnPress: props.onBack},
	}, true)
	return uicomponents.Panel(uicomponents.PanelProps{Bindings: bindings},
		trail.Box(trail.BoxProps{Style: common.Column.MarginLeft(mcpCardIndent)},
			sectionHeading(common.Title, title),
			blankRow(),
			sectionHeading(common.Secondary, mcpAuthSpinner+" "+mcpAuthNotice),
			kitex.If(report != "", func() trail.Node {
				return kitex.Fragment(blankRow(), mcpWrappedLines(common.Warning, report, width))
			}),
			kitex.If(!props.browserOnly, func() trail.Node {
				url := sectionHeading(common.Muted, "Waiting for the authorization URL…")
				if state.authURL != "" {
					url = mcpWrappedLines(common.Accent, state.authURL, width)
				}
				return kitex.Fragment(blankRow(), sectionHeading(common.Secondary, mcpAuthCopyHint), url,
					blankRow(), sectionHeading(common.Secondary, mcpAuthPasteHint),
					trail.Box(trail.BoxProps{Style: common.Row},
						trail.Span(trail.SpanProps{Style: common.Secondary}, trail.Text("URL > ")),
						kitex.Input(kitex.InputProps{
							Ref:   inputRef,
							Value: value(),
							Style: style.S().Flex(1),
							OnChange: func(e event.Event) {
								if ie, ok := e.(*event.InputEvent); ok {
									setValue(ie.Value)
								}
							},
						}),
					),
					blankRow(), sectionHeading(common.Muted, mcpAuthReturnHint))
			}),
		))
})

// mcpAuthTimelineReport records how an authorization ended in the conversation
// timeline, where it can still be read and copied after the browser closes. A notice
// in the browser's title disappears with it, which is no use to someone retrying a
// failed login.
func mcpAuthTimelineReport(state mcpAuthState) {
	if !state.settled {
		return
	}
	message := strings.TrimSpace(state.notice)
	if message == "" {
		message = strings.TrimSpace(state.report)
	}
	if message == "" {
		return
	}
	if state.failed {
		store.AppendError("MCP", message)
		return
	}
	store.AppendCommandOutput("/mcp", message)
}
