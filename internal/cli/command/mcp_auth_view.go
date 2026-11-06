package command

import (
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
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	uicomponents "github.com/masterkeysrd/tasksmith/internal/ui/components"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
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

// mcpCopyToClipboard publishes text to the system clipboard and reports whether it
// was accepted. It is a variable so a test can capture what a copy produced instead
// of racing a clipboard that headless environments do not provide.
var mcpCopyToClipboard = func(doc dom.Document, text string) bool {
	if doc == nil || strings.TrimSpace(text) == "" {
		return false
	}
	clipboard := doc.Clipboard()
	if clipboard == nil {
		return false
	}
	// The write is asynchronous and the terminal gives no signal of its own, so the
	// screen confirms it optimistically; a clipboard the terminal cannot reach would
	// fail silently for every other copy in the app too.
	clipboard.WriteText(text)
	return true
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
	server string
	state  *kites.Store[mcpAuthState]
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

// mcpAuthServerName names the server an authorization screen is about. The flow
// knows it, and the browser's pane state knows it, but neither is guaranteed to be
// populated when the other is, since the pane opens before the flow starts.
func mcpAuthServerName(server *api.MCPServer, state mcpAuthState) string {
	if server != nil && strings.TrimSpace(server.Name) != "" {
		return server.Name
	}
	return state.server
}

// mcpAuthPane shows an authorization in progress: the page to open, a way to copy it
// when no browser appeared, and a field to paste the redirect back when the browser
// could not reach the daemon's callback listener.
var mcpAuthPane = trail.FC("MCPServerAuth", func(props mcpAuthPaneProps) trail.Node {
	scheme := theme.UseTheme()
	doc := kitex.UseDocument()
	winSize := trail.UseWindowSize()
	value, setValue := kitex.UseState("")
	inputRef := kitex.UseRef[dom.Element](nil)
	trail.UseFocus(inputRef, trail.FocusOptions{AutoFocus: true, IsActive: true})

	state := kites.Use(props.state, func(s mcpAuthState) mcpAuthState { return s })
	server := strings.TrimSpace(props.server)
	if server == "" {
		server = state.server
	}

	trail.UseInput(func(_ string, k event.KeyEvent) {
		switch {
		case k.MatchString("esc", "escape"):
			if props.onBack != nil {
				props.onBack()
			}
		case k.MatchString("enter"):
			pasted := strings.TrimSpace(value())
			if pasted == "" || props.onSubmit == nil {
				return
			}
			setValue("")
			props.onSubmit(pasted)
		case mcpAuthCopyKey(k, value()):
			if !mcpCopyToClipboard(doc(), state.authURL) {
				return
			}
			// The paste field has focus, so the "c" that copies can also land in
			// it. Dropping it keeps the field ready for a pasted URL.
			if strings.TrimSpace(value()) == "c" {
				setValue("")
			}
			if props.onCopy != nil {
				props.onCopy()
			}
		}
	})

	titleStyle := style.S().Bold(true).Foreground(scheme.Color.Text.Primary)
	reportStyle := style.S().Foreground(scheme.Color.Text.Warning)
	labelStyle := style.S().Foreground(scheme.Color.Text.Secondary)
	urlStyle := style.S().Foreground(scheme.Color.Text.Accent)
	mutedStyle := style.S().Foreground(scheme.Color.Text.Muted)

	width := winSize.Width - mcpCardIndent
	rows := []trail.Node{
		mcpDetailLine(trail.Span(trail.SpanProps{Style: titleStyle}, trail.Text("Authenticating with "+server+"…"))),
		blankRow(),
		mcpDetailLine(trail.Span(trail.SpanProps{Style: labelStyle},
			trail.Text(mcpAuthSpinner+" "+mcpAuthNotice))),
	}

	report := strings.TrimSpace(props.notice)
	if report == "" {
		report = strings.TrimSpace(state.report)
	}
	if report != "" {
		rows = append(rows, blankRow())
		for _, line := range mcpWrapText(report, width, 0) {
			rows = append(rows, mcpDetailLine(trail.Span(trail.SpanProps{
				Style: reportStyle.WhiteSpace(style.WhiteSpacePre),
			}, trail.Text(line))))
		}
	}

	rows = append(rows,
		blankRow(),
		mcpDetailLine(trail.Span(trail.SpanProps{Style: labelStyle}, trail.Text(mcpAuthCopyHint))),
	)
	if state.authURL == "" {
		rows = append(rows, mcpDetailLine(trail.Span(trail.SpanProps{Style: mutedStyle},
			trail.Text("Waiting for the authorization URL…"))))
	} else {
		for _, line := range mcpWrapText(state.authURL, width, 0) {
			rows = append(rows, mcpDetailLine(trail.Span(trail.SpanProps{
				Style: urlStyle.WhiteSpace(style.WhiteSpacePre),
			}, trail.Text(line))))
		}
	}

	rows = append(rows,
		blankRow(),
		mcpDetailLine(trail.Span(trail.SpanProps{Style: labelStyle}, trail.Text(mcpAuthPasteHint))),
		trail.Box(trail.BoxProps{Style: style.S().FlexDirection(style.FlexRow)},
			trail.Span(trail.SpanProps{Style: labelStyle}, trail.Text("URL > ")),
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
		blankRow(),
		mcpDetailLine(trail.Span(trail.SpanProps{Style: mutedStyle}, trail.Text(mcpAuthReturnHint))),
	)

	border := style.SingleBorder().Top(true).Bottom(false).Left(false).Right(false).Color(scheme.Color.Border.Default)
	return trail.Box(trail.BoxProps{
		Style: style.S().
			Border(border).
			Width(style.Percent(100)).
			FlexDirection(style.FlexColumn),
	},
		trail.Box(trail.BoxProps{
			Style: style.S().MarginLeft(mcpCardIndent).FlexDirection(style.FlexColumn),
		}, rows...),
		uicomponents.KeyboardHint(uicomponents.KeyboardHintProps{
			Bindings: []uicomponents.KeyBinding{
				{Key: "c", Label: "Copy URL"},
				{Key: "enter", Label: "Submit pasted URL"},
				{Key: "esc", Label: "Back"},
			},
		}),
	)
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
