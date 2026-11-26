package components

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/masterkeysrd/kite/backend"
	"github.com/masterkeysrd/kite/backend/mock"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/geom"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/tui/testutils"
)

func startTranscript(t *testing.T, b backend.Backend) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	inst, err := trail.New(Transcript(), trail.WithBackend(b))
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		defer close(done)
		defer inst.Unmount()
		_ = inst.Run(ctx)
	}()
	stop := func() {
		cancel()
		<-done
	}
	t.Cleanup(stop)
	return stop
}

func TestTranscriptRendersHeaderAndRemountsOnReset(t *testing.T) {
	store.ClearTranscript()
	t.Cleanup(store.ClearTranscript)

	store.AppendHeader(store.HeaderData{
		Version:   "1.0.0",
		Model:     "TestModel",
		Thinking:  "medium",
		Workspace: "/workspace/initial",
	})
	store.FreezeTranscript()

	b := mock.New(80, 24)
	startTranscript(t, b)

	// Wait for header to be committed to InsertedAbove static scrollback
	var initialText string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		initialText = testutils.ScrollbackText(b)
		if strings.Contains(initialText, "TaskSmith v1.0.0") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	for _, want := range []string{
		"TaskSmith v1.0.0",
		"█▜▛█",
		"TestModel with medium effort",
		"/workspace/initial",
	} {
		if !strings.Contains(initialText, want) {
			t.Fatalf("static scrollback missing %q, got:\n%s", want, initialText)
		}
	}

	// Reset transcript with a new header, which increments Epoch
	store.ResetTranscript(store.TranscriptItem{
		ID:   "header",
		Kind: store.TranscriptKindHeader,
		Data: store.HeaderData{
			Version:   "2.0.0",
			Model:     "NewModel",
			Thinking:  "high",
			Workspace: "/workspace/reset",
		},
	})

	// Wait for new header to be rendered and committed on remount
	var postResetText string
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		postResetText = testutils.ScrollbackText(b)
		if strings.Contains(postResetText, "TaskSmith v2.0.0") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	for _, want := range []string{
		"TaskSmith v2.0.0",
		"NewModel with high effort",
		"/workspace/reset",
	} {
		if !strings.Contains(postResetText, want) {
			t.Fatalf("static scrollback missing reset item %q, got:\n%s", want, postResetText)
		}
	}
}

func TestTranscriptFreezesMascotInReadyPose(t *testing.T) {
	store.ClearTranscript()
	t.Cleanup(store.ClearTranscript)

	app := testutils.NewApp(t, Transcript(), 80, 24)
	if text := app.Text(); !strings.Contains(text, "◥██████████◣") {
		t.Fatalf("missing live startup T:\n%s", text)
	}

	store.FreezeTranscript()
	app.WaitFor(func(string) bool { return strings.Contains(testutils.ScrollbackText(app.Backend), "█▜▛█") })
	got := testutils.ScrollbackText(app.Backend)
	if strings.Count(got, "█▜▛█") != 1 || strings.Contains(got, "◥██████████◣") {
		t.Fatalf("expected exactly one static mascot and no startup T in scrollback:\n%s", got)
	}
	if text := app.Text(); strings.Contains(text, "▜████████▛") {
		t.Fatalf("mascot still visible in the live region after freeze:\n%s", text)
	}
}

func TestTranscriptPlaceholderKinds(t *testing.T) {
	store.ClearTranscript()
	t.Cleanup(store.ClearTranscript)

	// Append other kinds; should not panic and return valid nodes
	store.AppendTimelineEntry(api.TimelineEntry{ID: "tl-1"})
	store.AppendNotice("notice text")
	store.AppendError("err title", "err msg")
	store.AppendCommandOutput("/cmd", "output")

	b := mock.New(80, 24)
	startTranscript(t, b)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if b.LastFrame().Surface != nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Verify nothing broke and surface is clean
	if b.LastFrame().Surface == nil {
		t.Fatal("expected rendered surface")
	}
}

func TestTranscriptClearReprintHeader(t *testing.T) {
	store.ClearTranscript()
	t.Cleanup(store.ClearTranscript)

	store.SetHeaderMeta("1.0.0", "/workspace/clear-test")
	store.SetModelInfo("TestModel", "medium")
	store.FreezeTranscript()
	store.AppendWarning("old message that should be cleared")

	b := testutils.NewClearingBackend(mock.New(80, 24))
	startTranscript(t, b)

	// Wait for initial header and notice to be committed to scrollback
	var initialText string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		initialText = b.ScrollbackText()
		if strings.Contains(initialText, "TaskSmith v1.0.0") && strings.Contains(initialText, "old message that should be cleared") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	if !strings.Contains(initialText, "TaskSmith v1.0.0") || !strings.Contains(initialText, "old message that should be cleared") {
		t.Fatalf("expected initial header and notice in scrollback, got:\n%s", initialText)
	}

	// Trigger ClearScreen on frozen dirty transcript
	store.ClearScreen()

	// Wait for clear to take effect and snapshot header to be reprinted in scrollback
	var postClearText string
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if b.ClearCount() > 0 {
			postClearText = b.AllText()
			if strings.Contains(postClearText, "TaskSmith v1.0.0") {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
	}

	if b.ClearCount() < 1 {
		t.Fatalf("expected backend Clear() to have been called at least once, got %d", b.ClearCount())
	}
	if !strings.Contains(postClearText, "TaskSmith v1.0.0") {
		t.Fatalf("expected Header after clear on screen, got:\n%s", postClearText)
	}
	if strings.Contains(postClearText, "old message that should be cleared") {
		t.Fatalf("expected old messages to be cleared from scrollback, got:\n%s", postClearText)
	}
}

func TestTranscriptLiveDraftAndFreezeOnSubmit(t *testing.T) {
	store.ClearTranscript()
	t.Cleanup(store.ClearTranscript)

	store.SetHeaderMeta("1.0.0", "/workspace/live-test")
	store.SetModelInfo("model-alpha", "low")

	b := mock.New(80, 24)
	startTranscript(t, b)

	// 1. Initial render: Header is live on surface, scrollback (InsertedAbove) is empty
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(testutils.SurfaceText(b), "model-alpha with low effort") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !strings.Contains(testutils.SurfaceText(b), "model-alpha with low effort") {
		t.Fatalf("expected live header on surface, got:\n%s", testutils.SurfaceText(b))
	}
	if b.InsertedAboveCount() != 0 {
		t.Fatalf("expected InsertedAbove to be empty in draft mode, got %d buffers", b.InsertedAboveCount())
	}

	// 2. Append command output in draft mode: remains live on surface, NOT in InsertedAbove
	store.AppendCommandOutput("/model model-alpha", "Model set to model-alpha")
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(testutils.SurfaceText(b), "Model set to model-alpha") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !strings.Contains(testutils.SurfaceText(b), "Model set to model-alpha") {
		t.Fatalf("expected command output live on surface, got:\n%s", testutils.SurfaceText(b))
	}
	if b.InsertedAboveCount() != 0 {
		t.Fatalf("expected InsertedAbove to remain empty while in draft mode, got %d buffers", b.InsertedAboveCount())
	}

	// 3. Reactively update model info in place: live header updates even after command outputs
	store.SetModelInfo("model-beta", "high")
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(testutils.SurfaceText(b), "model-beta with high effort") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !strings.Contains(testutils.SurfaceText(b), "model-beta with high effort") {
		t.Fatalf("expected live header to update model info in place, got:\n%s", testutils.SurfaceText(b))
	}
	if b.InsertedAboveCount() != 0 {
		t.Fatalf("expected InsertedAbove to remain empty after model update, got %d buffers", b.InsertedAboveCount())
	}

	// 4. Freeze transcript (e.g. on first prompt send): freezes snapshot header and draft items into InsertedAbove
	store.FreezeTranscript()
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(testutils.ScrollbackText(b), "Model set to model-alpha") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	aboveText := testutils.ScrollbackText(b)
	if !strings.Contains(aboveText, "model-beta with high effort") {
		t.Fatalf("expected frozen header in InsertedAbove after FreezeTranscript, got:\n%s", aboveText)
	}
	if !strings.Contains(aboveText, "Model set to model-alpha") {
		t.Fatalf("expected command output in InsertedAbove after FreezeTranscript, got:\n%s", aboveText)
	}
}

type fakeResizeTimer struct {
	fn      func()
	stopped atomic.Bool
}

type fakeResizeClock struct {
	mu     sync.Mutex
	timers []*fakeResizeTimer
}

func (c *fakeResizeClock) schedule(_ time.Duration, fn func()) func() {
	timer := &fakeResizeTimer{fn: fn}
	c.mu.Lock()
	c.timers = append(c.timers, timer)
	c.mu.Unlock()
	return func() { timer.stopped.Store(true) }
}

func (c *fakeResizeClock) snapshot() []*fakeResizeTimer {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]*fakeResizeTimer, len(c.timers))
	copy(out, c.timers)
	return out
}

func TestTranscriptResize(t *testing.T) {
	wait := func(t *testing.T, b *testutils.ClearingBackend, ready func() bool) {
		t.Helper()
		deadline := time.Now().Add(testutils.DefaultWaitTimeout)
		for !ready() {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for condition:\n%s", b.AllText())
			}
			time.Sleep(time.Millisecond)
		}
	}

	syncEngine := func(t *testing.T, inst *trail.Instance) {
		t.Helper()
		done := make(chan struct{})
		inst.Engine().Post(func() { close(done) })
		select {
		case <-done:
		case <-time.After(testutils.DefaultWaitTimeout):
			t.Fatal("timed out synchronizing with engine")
		}
	}

	for _, tc := range []struct {
		name                string
		widths              []int
		draft, empty, reset bool
		wantTimers          int
		clears              int
	}{
		{name: "mount"},
		{name: "height only", widths: []int{80}},
		{name: "draft", widths: []int{50}, draft: true},
		{name: "empty frozen", widths: []int{50}, empty: true},
		{name: "width", widths: []int{50}, wantTimers: 1, clears: 1},
		{name: "drag", widths: []int{70, 60, 50}, wantTimers: 3, clears: 1},
		{name: "return", widths: []int{60, 80}, wantTimers: 1},
		{name: "session reset", widths: []int{60}, reset: true, wantTimers: 1, clears: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clock := &fakeResizeClock{}
			old := afterTranscriptResize
			afterTranscriptResize = clock.schedule
			t.Cleanup(func() { afterTranscriptResize = old })

			store.ClearTranscript()
			t.Cleanup(store.ClearTranscript)
			store.SetHeaderMeta("resize", "workspace")
			wide := strings.Repeat("123456789 ", 6) + "end"
			store.AppendNotice(wide)
			if !tc.draft {
				store.FreezeTranscript()
			}
			if tc.empty {
				store.ResetTranscript()
			}
			b := testutils.NewClearingBackend(mock.New(80, 24))
			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan struct{})
			inst, err := trail.New(Transcript(), trail.WithBackend(b))
			if err != nil {
				t.Fatal(err)
			}
			go func() {
				defer close(done)
				defer inst.Unmount()
				_ = inst.Run(ctx)
			}()
			t.Cleanup(func() {
				cancel()
				<-done
			})

			wait(t, b, func() bool { return b.FrameCount() > 0 && (tc.empty || strings.Contains(b.AllText(), wide)) })
			for i, w := range tc.widths {
				size := geom.Size{Width: w, Height: 25 + i}
				frames := b.FrameCount()
				b.Resize(size)
				b.PostEvent(&backend.RawResizeEvent{Width: w, Height: size.Height})
				wait(t, b, func() bool { return b.FrameCount() > frames && b.LastFrame().Surface.Width == w })
				syncEngine(t, inst)
			}
			if got := b.ClearCount(); got != 0 {
				t.Fatalf("cleared before debounce settled: %d", got)
			}
			if tc.reset {
				store.ResetForSessionSwitch()
				wait(t, b, func() bool { return b.ClearCount() == 1 })
				syncEngine(t, inst)
			}

			timers := clock.snapshot()
			if len(timers) != tc.wantTimers {
				t.Fatalf("scheduled timers = %d, want %d", len(timers), tc.wantTimers)
			}

			// Every superseded timer (or all timers when returning to original width / resetting)
			// must have been stopped by cleanup, and firing stale callbacks must be a no-op.
			for i, timer := range timers {
				isLastActive := !tc.reset && tc.clears == 1 && i == len(timers)-1
				if !isLastActive {
					if !timer.stopped.Load() {
						t.Fatalf("timer %d was not stopped", i)
					}
					timer.fn()
					syncEngine(t, inst)
				}
			}
			if !tc.reset && tc.clears == 1 {
				last := timers[len(timers)-1]
				if last.stopped.Load() {
					t.Fatal("active resize timer was unexpectedly stopped")
				}
				wrapped := strings.TrimSpace(strings.Repeat("123456789 ", 5)) + "\n123456789 end"
				last.fn()
				wait(t, b, func() bool {
					return b.ClearCount() == 1 &&
						strings.Contains(b.ScrollbackText(), "TaskSmith vresize") &&
						strings.Contains(b.ScrollbackText(), wrapped)
				})
			}

			syncEngine(t, inst)
			if got := b.ClearCount(); got != tc.clears {
				t.Fatalf("got %d clears, want %d:\n%s", got, tc.clears, b.AllText())
			}
		})
	}

	t.Run("unmount keeps engine alive and ignores stale callback", func(t *testing.T) {
		clock := &fakeResizeClock{}
		old := afterTranscriptResize
		afterTranscriptResize = clock.schedule
		t.Cleanup(func() { afterTranscriptResize = old })

		store.ClearTranscript()
		t.Cleanup(store.ClearTranscript)
		store.SetHeaderMeta("resize", "workspace")
		store.AppendNotice("hello")
		store.FreezeTranscript()

		var setShow func(bool)
		host := trail.SimpleFC("ResizeHost", func() trail.Node {
			show, update := kitex.UseState(true)
			setShow = update
			if !show() {
				return trail.Text("unmounted")
			}
			return Transcript()
		})

		b := testutils.NewClearingBackend(mock.New(80, 24))
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		inst, err := trail.New(host(), trail.WithBackend(b))
		if err != nil {
			t.Fatal(err)
		}
		go func() {
			defer close(done)
			defer inst.Unmount()
			_ = inst.Run(ctx)
		}()
		t.Cleanup(func() {
			cancel()
			<-done
		})

		wait(t, b, func() bool { return b.FrameCount() > 0 && strings.Contains(b.AllText(), "hello") })

		frames := b.FrameCount()
		b.Resize(geom.Size{Width: 60, Height: 24})
		b.PostEvent(&backend.RawResizeEvent{Width: 60, Height: 24})
		wait(t, b, func() bool { return b.FrameCount() > frames && b.LastFrame().Surface.Width == 60 })
		syncEngine(t, inst)

		timers := clock.snapshot()
		if len(timers) != 1 {
			t.Fatalf("scheduled timers = %d, want 1", len(timers))
		}

		// Unmount Transcript while keeping the engine running.
		inst.Engine().Post(func() { setShow(false) })
		wait(t, b, func() bool { return strings.Contains(testutils.SurfaceText(b.Backend), "unmounted") })
		syncEngine(t, inst)

		if !timers[0].stopped.Load() {
			t.Fatal("pending resize timer was not stopped on unmount")
		}
		// Fire the stale callback while the engine is still running; it must be a no-op.
		timers[0].fn()
		syncEngine(t, inst)
		if got := b.ClearCount(); got != 0 {
			t.Fatalf("stale callback after unmount triggered %d clears", got)
		}
	})
}

func TestTranscriptSessionSwitch(t *testing.T) {
	store.ClearTranscript()
	t.Cleanup(store.ClearTranscript)

	store.SetHeaderMeta("1.0.0", "/workspace/resumed")
	store.SetModelInfo("resumed-model", "high")

	b := mock.New(80, 24)
	startTranscript(t, b)

	// Switch session: snapshot header and enter frozen mode
	store.ResetForSessionSwitch()
	store.AppendTimelineEntry(api.TimelineEntry{
		ID:        "tl-entry-1",
		SessionID: "s-resumed",
	})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(testutils.ScrollbackText(b), "resumed-model with high effort") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	aboveText := testutils.ScrollbackText(b)
	if !strings.Contains(aboveText, "TaskSmith v1.0.0") || !strings.Contains(aboveText, "resumed-model with high effort") {
		t.Fatalf("expected resumed session header in InsertedAbove, got:\n%s", aboveText)
	}
}
