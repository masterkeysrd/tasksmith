package components

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/terminal"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/core/strutils"
)

// terminalAppName is the stable machine-readable program name reported through
// the Program Status Protocol (OSC 7501).
const terminalAppName = "tasksmith"

// terminalTitleMaxCells bounds the window/tab title so a long session title
// never floods the tab bar.
const terminalTitleMaxCells = 60

// statusMessageMaxCells bounds the one-line activity message so a long daemon
// activity line cannot bloat every escape sequence written.
const statusMessageMaxCells = 80

// statusHold is how long a terminal-only transient status (a failed turn or a
// just-completed turn) stays visible after the session itself has settled back
// to idle. Without a hold the state would flash for a single frame. It is a
// var so tests can shorten it.
var statusHold = 5 * time.Second

// terminalState is the REPL's session state projected onto what terminals can
// display: a window title, a native progress bar (OSC 9;4), and a structured
// Program Status record (OSC 7501).
type terminalState string

const (
	stateIdle    terminalState = "idle"
	stateWorking terminalState = "working"
	stateBlocked terminalState = "blocked"
	stateDone    terminalState = "done"
	stateError   terminalState = "error"
)

// streamKey is the compact comparable slice of stream state the terminal
// status projection needs. Subscribing to this instead of the whole
// StreamState keeps a headless component from re-rendering on every token.
type streamKey struct {
	Streaming         bool
	WaitingPermission bool
	WaitingQuestion   bool
	TurnFailed        bool
}

// liveTerminalState projects stream and onboarding state onto a terminalState.
// An onboarding wizard is treated as blocked-on-auth because that is exactly
// what it usually is: the REPL cannot proceed until credentials are provided.
// A turn failure is not projected here: it arrives on the same publish that
// returns the stream to idle, so it rides the stream's TurnFailed flag through
// a rising-edge hold instead.
func liveTerminalState(stream streamKey, onboardingActive bool) terminalState {
	switch {
	case onboardingActive:
		return stateBlocked
	case stream.WaitingPermission:
		return stateBlocked
	case stream.WaitingQuestion:
		return stateBlocked
	case stream.Streaming:
		return stateWorking
	default:
		return stateIdle
	}
}

// blockedKind says what a blocked session is waiting for, per the Program
// Status Protocol's Kind field.
func blockedKind(stream streamKey, onboardingActive bool) terminal.ProgramStatusKind {
	switch {
	case stream.WaitingPermission:
		return terminal.ProgramStatusKindPermission
	case stream.WaitingQuestion:
		return terminal.ProgramStatusKindQuestion
	case onboardingActive:
		return terminal.ProgramStatusKindAuth
	default:
		return ""
	}
}

// progressBarState maps a terminalState onto the native progress bar (OSC 9;4).
// Idle and done hide the bar; done is deliberately not a full bar, since the
// session has no measurable progress toward anything.
func progressBarState(state terminalState) terminal.ProgressBarState {
	switch state {
	case stateWorking:
		return terminal.ProgressBarIndeterminate
	case stateBlocked:
		return terminal.ProgressBarPaused
	case stateError:
		return terminal.ProgressBarError
	default:
		return terminal.ProgressBarHide
	}
}

// programState maps a terminalState onto the Program Status Protocol states.
func programState(state terminalState) terminal.ProgramState {
	switch state {
	case stateWorking:
		return terminal.ProgramStateWorking
	case stateBlocked:
		return terminal.ProgramStateBlocked
	case stateDone:
		return terminal.ProgramStateDone
	case stateError:
		return terminal.ProgramStateError
	default:
		return terminal.ProgramStateIdle
	}
}

// isHeldState reports whether a state is a transient one that deserves a short
// hold after the underlying stream has already moved on.
func isHeldState(state terminalState) bool {
	return state == stateError || state == stateDone
}

// sanitizeStatusText collapses runs of whitespace and strips control
// characters from daemon-provided text before it is embedded in an escape
// sequence.
func sanitizeStatusText(s string) string {
	return strings.Join(strings.Fields(strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)), " ")
}

// terminalTitleText builds the window/tab title: the generated session title
// when the daemon has produced one, else the workspace directory name. No
// application prefix is added — tab bars are narrow and the Program Status
// record already carries the app identity. Control characters are stripped
// because the title is derived from daemon data and feeds escape sequences
// directly.
func terminalTitleText(sessionTitle, workspace string) string {
	label := sanitizeStatusText(sessionTitle)
	if label == "" {
		label = filepath.Base(strings.TrimSpace(workspace))
	}
	if label == "" || label == "." || label == string(filepath.Separator) {
		label = "TaskSmith"
	}
	label, _ = strutils.TruncateLine(label, terminalTitleMaxCells, "…")
	return label
}

// statusMessageText is the one-line human message carried by the Program
// Status record. While working it prefers the daemon's live activity line.
func statusMessageText(state terminalState, activity string) string {
	if state == stateWorking {
		if line := sanitizeStatusText(activity); line != "" {
			line, _ = strutils.TruncateLine(line, statusMessageMaxCells, "…")
			return line
		}
		return "Working"
	}
	switch state {
	case stateBlocked:
		return "Waiting for input"
	case stateDone:
		return "Turn complete"
	case stateError:
		return "Turn failed"
	default:
		return "Idle"
	}
}

// TerminalStatus is a headless component that mirrors the session's live state
// onto the terminal itself: the window/tab title (OSC 2), the native progress
// bar (OSC 9;4), and the Program Status Protocol (OSC 7501), so tabs, taskbars,
// and multiplexers show when TaskSmith is working, blocked on the user, done,
// or errored. It renders nothing, and every write is gated by the engine's
// terminal capability probe, so unsupported terminals see nothing at all.
var TerminalStatus = trail.SimpleFC("TerminalStatus", func() trail.Node {
	workspace := store.UseApp(func(s store.AppState) string { return s.Workspace })
	onboardingActive := store.UseOnboarding(func(s store.OnboardingState) bool { return s.Active })
	sessionTitle := store.UseSession(func(s store.SessionState) string {
		if s.Session == nil {
			return ""
		}
		return s.Session.Title
	})
	activity := store.UseSession(func(s store.SessionState) string {
		if s.Session == nil {
			return ""
		}
		return s.Session.Activity
	})
	stream := store.UseStream(func(s store.StreamState) streamKey {
		return streamKey{
			Streaming:         s.Streaming,
			WaitingPermission: s.WaitingPermission,
			WaitingQuestion:   s.WaitingQuestion,
			TurnFailed:        s.TurnFailed,
		}
	})

	live := liveTerminalState(stream, onboardingActive)
	held, setHeld := kitex.UseState(stateIdle)
	display := live
	if held() != stateIdle && live == stateIdle {
		display = held()
	}

	titleText := terminalTitleText(sessionTitle, workspace)
	messageText := statusMessageText(display, activity)
	kind := terminal.ProgramStatusKind("")
	if display == stateBlocked {
		kind = blockedKind(stream, onboardingActive)
	}

	// The latest projection, readable from callbacks that outlive the render
	// that created them.
	type statusSnapshot struct {
		state   terminalState
		title   string
		message string
		kind    terminal.ProgramStatusKind
	}
	current := kitex.UseRef(statusSnapshot{})
	kitex.UseLayoutEffect(func() {
		current.Current = statusSnapshot{
			state:   display,
			title:   titleText,
			message: messageText,
			kind:    kind,
		}
	}, []any{display, titleText, messageText, kind})

	getTerm := kitex.UseTerminal()

	// Title (OSC 2). kitex.UseTitle re-applies the title whenever it changes;
	// the engine drops the write when the terminal does not support titles.
	_, setTitle := kitex.UseTitle(titleText)
	kitex.UseEffect(func() {
		setTitle(titleText)
	}, []any{titleText})

	// Progress bar (OSC 9;4) and Program Status (OSC 7501) follow the
	// displayed state. Neither is ever written before the terminal answers
	// the Program Status capability probe, so a terminal that does not speak
	// the protocol sees no records at all.
	reportStatus := kitex.UseCallback(func() {
		term := getTerm()
		if term == nil {
			return
		}
		cur := current.Current
		term.SetProgressBar(progressBarState(cur.state), 0)
		term.SetProgramStatus(&terminal.ProgramStatus{
			State:   programState(cur.state),
			App:     terminalAppName,
			Kind:    cur.kind,
			Title:   cur.title,
			Message: cur.message,
		})
	}, []any{display, titleText, messageText, kind})

	kitex.UseEffect(func() {
		reportStatus()
	}, []any{display, titleText, messageText, kind, reportStatus})

	// Terminals that answer the capability probe confirm Program Status
	// support at runtime; re-report once so the first real record lands after
	// the engine has enabled the capability.
	doc := kitex.UseDocument()
	kitex.UseEffectCleanup(func() func() {
		d := doc()
		if d == nil {
			return nil
		}
		sub := d.AddEventListener(event.EventProgramStatusSupport, func(event.Event) {
			kitex.PostMacro(reportStatus)
		})
		return sub.Cancel
	}, []any{reportStatus})

	// A failed turn or a finished one settles back to idle immediately, so
	// hold the transient state briefly before clearing it. A turn that ends
	// without an error projects as done — the point of the Program Status
	// done state is that attention was requested, the user answered, and the
	// work finished — while a session that never streamed stays idle.
	// TurnFailed stays set until the next turn starts, so the failure is
	// caught on its rising edge rather than as a level.
	wasBusy := kitex.UseRef(false)
	prevFailed := kitex.UseRef(false)
	kitex.UseEffect(func() {
		failed := stream.TurnFailed
		switch {
		case failed && !prevFailed.Current:
			setHeld(stateError)
		case live == stateIdle && wasBusy.Current:
			setHeld(stateDone)
		case live != stateIdle:
			setHeld(stateIdle)
		}
		prevFailed.Current = failed
		wasBusy.Current = live == stateWorking || live == stateBlocked || stream.Streaming
	}, []any{live, stream.TurnFailed})
	kitex.UseTimeout(func() {
		if isHeldState(held()) {
			setHeld(stateIdle)
		}
	}, statusHold, []any{held()})

	// Clear everything when the engine stops so the user's shell tab does not
	// keep a stale TaskSmith status. OnClose also fires immediately if the
	// engine has already stopped, and the writes are idempotent.
	kitex.UseEffectCleanup(func() func() {
		term := getTerm()
		if term == nil {
			return nil
		}
		term.OnClose(func() {
			term.SetTitle("")
			term.SetProgressBar(terminal.ProgressBarHide, 0)
			term.SetProgramStatus(nil)
		})
		return nil
	}, []any{})

	return nil
})
