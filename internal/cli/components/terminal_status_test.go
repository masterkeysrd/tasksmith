package components

import (
	"strings"
	"testing"
	"time"

	"github.com/masterkeysrd/kite/backend"
	"github.com/masterkeysrd/kite/backend/mock"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/terminal"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
)

// statusBackend starts TerminalStatus on a mock backend whose capabilities
// advertise title, progress bar, and program status support, since the engine
// silently drops every one of those writes when the caps say otherwise.
func statusBackend(t *testing.T) (*mock.Backend, *trail.Instance) {
	t.Helper()
	b := mock.NewWithCaps(80, 12, backend.Caps{Title: true, ProgressBar: true, ProgramStatus: true})
	inst, err := trail.New(TerminalStatus(), trail.WithBackend(b))
	if err != nil {
		t.Fatalf("trail.New: %v", err)
	}
	t.Cleanup(inst.Unmount)
	inst.Frame()
	return b, inst
}

// waitForStatus pumps frames until cond accepts the current program status or
// the deadline passes, then hands back the last status seen. Status writes
// land through macrotasks, which Frame drains, so pumping frames is how the
// test gives effects time to run.
func waitForStatus(b *mock.Backend, inst *trail.Instance, cond func(*terminal.ProgramStatus) bool, timeout time.Duration) *terminal.ProgramStatus {
	deadline := time.Now().Add(timeout)
	var last *terminal.ProgramStatus
	for time.Now().Before(deadline) {
		inst.Frame()
		last = b.CurrentProgramStatus()
		if cond(last) {
			return last
		}
		time.Sleep(5 * time.Millisecond)
	}
	return last
}

func resetStatusStores(t *testing.T) {
	t.Helper()
	store.ResetApp()
	store.ResetSession()
	store.ResetStream()
	store.CloseOnboarding()
	t.Cleanup(func() {
		store.ResetApp()
		store.ResetSession()
		store.ResetStream()
		store.CloseOnboarding()
	})
}

func TestTerminalTitleText(t *testing.T) {
	tests := []struct {
		name      string
		title     string
		workspace string
		want      string
	}{
		{"session title wins", "Fix the flaky status line test", "/home/dev/Projects/tasksmith", "Fix the flaky status line test"},
		{"workspace basename fallback", "", "/home/dev/Projects/tasksmith", "tasksmith"},
		{"control characters collapse", "bad\n\ttitle\b", "/w", "bad title"},
		{"empty everything", "", "", "TaskSmith"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := terminalTitleText(tc.title, tc.workspace); got != tc.want {
				t.Errorf("terminalTitleText(%q, %q) = %q, want %q", tc.title, tc.workspace, got, tc.want)
			}
		})
	}
}

func TestTerminalTitleTruncatesLongSessionTitle(t *testing.T) {
	got := terminalTitleText(strings.Repeat("x", 200), "/w")
	if len(got) > terminalTitleMaxCells+len("…") {
		t.Fatalf("title not truncated to the cell budget: %d bytes: %q", len(got), got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("truncated title should end with the ellipsis, got %q", got)
	}
}

func TestTerminalStatusIdleWritesTitleAndIdleRecord(t *testing.T) {
	resetStatusStores(t)
	store.SetHeaderMeta("1.2.3", "/home/dev/Projects/tasksmith")

	b, inst := statusBackend(t)

	waitForStatus(b, inst, func(ps *terminal.ProgramStatus) bool { return ps != nil }, time.Second)

	if got := b.Title(); got != "tasksmith" {
		t.Errorf("expected workspace title, got %q", got)
	}
	ps := b.CurrentProgramStatus()
	if ps == nil {
		t.Fatalf("expected an idle program status record, got none\noutput: %q", b.Output.String())
	}
	if ps.State != terminal.ProgramStateIdle || ps.App != "tasksmith" {
		t.Errorf("expected idle record for app tasksmith, got %+v", ps)
	}
	if !strings.Contains(b.Output.String(), "\x1b]9;4;0;0\x07") {
		t.Errorf("expected a hidden progress bar write, got output %q", b.Output.String())
	}
}

func TestTerminalStatusWorkingWhileStreaming(t *testing.T) {
	resetStatusStores(t)
	store.SetHeaderMeta("1.2.3", "/w")
	store.SetSession(&api.Session{ID: "s-1", Activity: "Reading\n\tfiles\x07 in internal/"})
	b, inst := statusBackend(t)

	store.ApplyEvent(api.Event{
		Type:    api.EventChunk,
		Payload: api.ChunkEvent{Chunk: api.Chunk{ID: "asst-1", Content: []api.Block{{Type: api.BlockText, Text: "Working on it"}}}},
	})

	ps := waitForStatus(b, inst, func(ps *terminal.ProgramStatus) bool {
		return ps != nil && ps.State == terminal.ProgramStateWorking
	}, time.Second)
	if ps == nil || ps.State != terminal.ProgramStateWorking {
		t.Fatalf("expected working program status while streaming, got %+v", ps)
	}
	if ps.Message != "Reading files in internal/" {
		t.Errorf("activity message should be sanitized onto one line, got %q", ps.Message)
	}
	if !strings.Contains(b.Output.String(), "\x1b]9;4;3;0\x07") {
		t.Errorf("expected indeterminate progress bar write, got output %q", b.Output.String())
	}
}

func TestTerminalStatusBlockedOnPermission(t *testing.T) {
	resetStatusStores(t)
	store.SetHeaderMeta("1.2.3", "/w")
	b, inst := statusBackend(t)

	store.ApplyEvent(api.Event{
		Type: api.EventPermissionRequested,
		Payload: api.PermissionRequestedEvent{Permissions: []api.PermissionRequest{
			{ID: "p1", ToolCallID: "t1", ToolName: "bash", Action: api.ActionExec, Safety: api.SafetyUnsafe, Summary: "run rm"},
		}},
	})

	ps := waitForStatus(b, inst, func(ps *terminal.ProgramStatus) bool {
		return ps != nil && ps.State == terminal.ProgramStateBlocked
	}, time.Second)
	if ps == nil || ps.State != terminal.ProgramStateBlocked {
		t.Fatalf("expected blocked program status, got %+v", ps)
	}
	if ps.Kind != terminal.ProgramStatusKindPermission {
		t.Errorf("expected blocked kind permission, got %q", ps.Kind)
	}
	if !strings.Contains(b.Output.String(), "\x1b]9;4;4;0\x07") {
		t.Errorf("expected paused progress bar write, got output %q", b.Output.String())
	}
}

func TestTerminalStatusBlockedOnAuthDuringOnboarding(t *testing.T) {
	resetStatusStores(t)
	store.StartOnboarding("/home/dev/Projects/tasksmith", api.WorkspaceStatus{NeedsOnboarding: true}, false)

	b, inst := statusBackend(t)

	ps := waitForStatus(b, inst, func(ps *terminal.ProgramStatus) bool {
		return ps != nil && ps.State == terminal.ProgramStateBlocked
	}, time.Second)
	if ps == nil || ps.State != terminal.ProgramStateBlocked {
		t.Fatalf("expected blocked program status during onboarding, got %+v", ps)
	}
	if ps.Kind != terminal.ProgramStatusKindAuth {
		t.Errorf("expected blocked kind auth, got %q", ps.Kind)
	}
}

func TestTerminalStatusDoneAfterTurnCompletes(t *testing.T) {
	resetStatusStores(t)
	prevHold := statusHold
	statusHold = 40 * time.Millisecond
	t.Cleanup(func() { statusHold = prevHold })

	store.SetHeaderMeta("1.2.3", "/w")
	b, inst := statusBackend(t)

	store.ApplyEvent(api.Event{
		Type:    api.EventChunk,
		Payload: api.ChunkEvent{Chunk: api.Chunk{ID: "asst-1", Content: []api.Block{{Type: api.BlockText, Text: "done"}}}},
	})
	if ps := waitForStatus(b, inst, func(ps *terminal.ProgramStatus) bool {
		return ps != nil && ps.State == terminal.ProgramStateWorking
	}, time.Second); ps == nil {
		t.Fatalf("expected working status before completion, got %+v", ps)
	}

	store.ApplyEvent(api.Event{Type: api.EventCompleted, Payload: api.CompletedEvent{}})

	ps := waitForStatus(b, inst, func(ps *terminal.ProgramStatus) bool {
		return ps != nil && ps.State == terminal.ProgramStateDone
	}, time.Second)
	if ps == nil || ps.State != terminal.ProgramStateDone {
		t.Fatalf("expected done program status after completion, got %+v", ps)
	}

	ps = waitForStatus(b, inst, func(ps *terminal.ProgramStatus) bool {
		return ps != nil && ps.State == terminal.ProgramStateIdle
	}, time.Second)
	if ps == nil || ps.State != terminal.ProgramStateIdle {
		t.Fatalf("expected the done hold to expire back to idle, got %+v", ps)
	}
}

func TestTerminalStatusErrorAfterTurnFails(t *testing.T) {
	resetStatusStores(t)
	prevHold := statusHold
	statusHold = 40 * time.Millisecond
	t.Cleanup(func() { statusHold = prevHold })

	store.SetHeaderMeta("1.2.3", "/w")
	b, inst := statusBackend(t)

	store.ApplyEvent(api.Event{
		Type:    api.EventChunk,
		Payload: api.ChunkEvent{Chunk: api.Chunk{ID: "asst-1", Content: []api.Block{{Type: api.BlockText, Text: "partial"}}}},
	})
	waitForStatus(b, inst, func(ps *terminal.ProgramStatus) bool {
		return ps != nil && ps.State == terminal.ProgramStateWorking
	}, time.Second)

	store.ApplyEvent(api.Event{Type: api.EventError, Payload: api.ErrorEvent{Error: "provider exploded"}})

	ps := waitForStatus(b, inst, func(ps *terminal.ProgramStatus) bool {
		return ps != nil && ps.State == terminal.ProgramStateError
	}, time.Second)
	if ps == nil || ps.State != terminal.ProgramStateError {
		t.Fatalf("expected error program status after a failed turn, got %+v", ps)
	}
	if !strings.Contains(b.Output.String(), "\x1b]9;4;2;0\x07") {
		t.Errorf("expected error progress bar write, got output %q", b.Output.String())
	}

	ps = waitForStatus(b, inst, func(ps *terminal.ProgramStatus) bool {
		return ps != nil && ps.State == terminal.ProgramStateIdle
	}, time.Second)
	if ps == nil || ps.State != terminal.ProgramStateIdle {
		t.Fatalf("expected the error hold to expire back to idle, got %+v", ps)
	}
}

func TestTerminalStatusTitleFollowsSessionTitle(t *testing.T) {
	resetStatusStores(t)
	store.SetHeaderMeta("1.2.3", "/home/dev/Projects/tasksmith")
	b, inst := statusBackend(t)

	store.SetSession(&api.Session{ID: "s-1", Title: "Refactor the gateway"})
	inst.Frame()

	waitForStatus(b, inst, func(ps *terminal.ProgramStatus) bool { return ps != nil }, time.Second)
	if got := b.Title(); got != "Refactor the gateway" {
		t.Errorf("expected title to follow the session title, got %q", got)
	}
}

func TestTerminalStatusUnsupportedTerminalsSeeNoWrites(t *testing.T) {
	resetStatusStores(t)
	store.SetHeaderMeta("1.2.3", "/w")

	b := mock.New(80, 12)
	inst, err := trail.New(TerminalStatus(), trail.WithBackend(b))
	if err != nil {
		t.Fatalf("trail.New: %v", err)
	}
	t.Cleanup(inst.Unmount)
	inst.Frame()

	store.ApplyEvent(api.Event{
		Type:    api.EventChunk,
		Payload: api.ChunkEvent{Chunk: api.Chunk{ID: "asst-1", Content: []api.Block{{Type: api.BlockText, Text: "hi"}}}},
	})
	inst.Frame()

	if out := b.Output.String(); strings.Contains(out, "\x1b]2;") || strings.Contains(out, "\x1b]9;4;") {
		t.Errorf("capability-gated writes leaked on a terminal without caps: %q", out)
	}
}

func TestTerminalStatusReportsAfterSupportProbe(t *testing.T) {
	resetStatusStores(t)
	store.SetHeaderMeta("1.2.3", "/w")

	// A terminal that has not answered the Program Status probe yet: the
	// engine drops OSC 7501 writes until the confirmation event arrives.
	b := mock.NewWithCaps(80, 12, backend.Caps{Title: true, ProgressBar: true})
	inst, err := trail.New(TerminalStatus(), trail.WithBackend(b))
	if err != nil {
		t.Fatalf("trail.New: %v", err)
	}
	t.Cleanup(inst.Unmount)
	inst.Frame()

	if ps := b.CurrentProgramStatus(); ps != nil {
		t.Fatalf("no program status should be written before support is confirmed, got %+v", ps)
	}

	inst.Engine().ProcessRawEvent(&backend.RawProgramStatusSupportEvent{})
	var ps *terminal.ProgramStatus
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		inst.Frame()
		if ps = b.CurrentProgramStatus(); ps != nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if ps == nil || ps.State != terminal.ProgramStateIdle {
		t.Fatalf("expected the idle record to be re-reported after the support probe, got %+v", ps)
	}
}

func TestTerminalStatusClearsOnEngineStop(t *testing.T) {
	resetStatusStores(t)
	store.SetHeaderMeta("1.2.3", "/w")

	b, inst := statusBackend(t)
	waitForStatus(b, inst, func(ps *terminal.ProgramStatus) bool { return ps != nil }, time.Second)
	if b.Title() == "" {
		t.Fatalf("expected a title before exit")
	}

	inst.Engine().Stop()

	if got := b.Title(); got != "" {
		t.Errorf("expected the title to be cleared on exit, got %q", got)
	}
	if ps := b.CurrentProgramStatus(); ps != nil {
		t.Errorf("expected the program status to be cleared on exit, got %+v", ps)
	}
	out := b.Output.String()
	if !strings.Contains(out, "\x1b]2;\x07") {
		t.Errorf("expected an empty-title write on exit, output ends %q", out[max(0, len(out)-80):])
	}
}
