// Package testutils renders TaskSmith's terminal UI inside tests.
//
// Two rendering strategies exist because components have different needs:
//
//   - [Render] and [NewEnv] run headlessly through testenv and kitex.Render.
//     This is the default: fast, deterministic, and enough for anything that only
//     reads props and kitex state.
//   - [NewApp] boots a real trail runtime on a mock backend. Use it when the
//     component needs trail.UseApp, trail.UseInput, Static output, or anything else
//     that only exists inside a running instance.
//
// Both return rendered text rather than a node tree, so assertions read what a
// user would actually see. Widths are always explicit: a test should state the
// terminal size it assumes instead of inheriting a default that can change under it.
package testutils

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/masterkeysrd/kite/backend"
	"github.com/masterkeysrd/kite/backend/mock"
	"github.com/masterkeysrd/kite/element"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/kite/testenv"
)

// DefaultWaitTimeout bounds [App.WaitFor] when no other timeout is set.
const DefaultWaitTimeout = 2 * time.Second

// waitInterval is how often WaitFor re-renders while polling for a condition.
const waitInterval = 5 * time.Millisecond

// Env is a headless terminal with a mounted container, for tests that render the
// same component more than once or drive it with input.
//
// Rendering the same container repeatedly reconciles against the previous tree,
// which is what you want when asserting that a state change updated the screen.
type Env struct {
	t *testing.T
	*testenv.Environment
	container *element.BoxElement
}

// NewEnv returns a headless environment of the given size, closed with the test.
func NewEnv(t *testing.T, width, height int) *Env {
	t.Helper()
	env := testenv.Default(width, height)
	t.Cleanup(env.Close)
	container := element.NewBox(env.Document())
	env.Mount(container)
	return &Env{t: t, Environment: env, container: container}
}

// Render mounts or updates node in this environment and returns the visible text.
//
// The result is the terminal dump with surrounding whitespace removed, matching
// what the repo's tests have always asserted against: interior lines keep their
// right padding, so column-aligned output still lines up. Use [TrimmedLinesText]
// when trailing padding gets in the way instead.
func (e *Env) Render(node trail.Node) string {
	e.t.Helper()
	kitex.Render(node, e.container)
	e.Flush()
	return e.Text()
}

// Text returns the current visible text without rendering.
func (e *Env) Text() string {
	e.t.Helper()
	return strings.TrimSpace(e.DumpText())
}

// Press delivers one key, flushes pending work, and returns the resulting text.
// Flushing is not optional in practice: rendering is scheduled, so an assertion
// right after a key press would otherwise read the pre-key frame.
func (e *Env) Press(k key.Key) string {
	e.t.Helper()
	e.SendKey(k)
	e.Flush()
	return e.Text()
}

// Type types text, flushes, and returns the resulting text.
func (e *Env) Type(text string) string {
	e.t.Helper()
	// Qualified because this method's name shadows the embedded one.
	e.Environment.Type(text)
	e.Flush()
	return e.Text()
}

// KeyPress delivers a named key such as "esc", flushes, and returns the text.
func (e *Env) KeyPress(keyOrChar string, mods ...key.Mod) string {
	e.t.Helper()
	// Qualified because this method's name shadows the embedded one.
	e.Environment.KeyPress(keyOrChar, mods...)
	e.Flush()
	return e.Text()
}

// Render mounts node in a fresh headless terminal of the given size and returns
// the visible text. Use [NewEnv] instead when a test renders more than once.
func Render(t *testing.T, node trail.Node, width, height int) string {
	t.Helper()
	return NewEnv(t, width, height).Render(node)
}

// App is a running trail instance on a mock backend, for components that need the
// real runtime (trail.UseApp, trail.UseInput, Static).
//
// Instance is nil for an app started with [StartApp], whose loop paints frames on
// its own; guard before using it.
type App struct {
	t *testing.T
	*trail.Instance
	Backend *mock.Backend
}

// NewApp boots node in a trail instance of the given size, renders one frame, and
// unmounts with the test.
func NewApp(t *testing.T, node trail.Node, width, height int) *App {
	t.Helper()
	b := mock.New(width, height)
	inst, err := trail.New(node, trail.WithBackend(b))
	if err != nil {
		t.Fatalf("testutils.NewApp: %v", err)
	}
	t.Cleanup(inst.Unmount)
	inst.Frame()
	return &App{t: t, Instance: inst, Backend: b}
}

// Text renders a frame and returns it as text with trailing whitespace removed,
// matching the shape [Env.Text] returns so assertions read the same either way.
func (a *App) Text() string {
	a.t.Helper()
	if a.Instance != nil {
		a.Frame()
	}
	return strings.TrimSpace(SurfaceText(a.Backend))
}

// Press delivers one key and returns the resulting text. On a [NewApp] instance it
// also renders a frame, since nothing else drives the loop.
func (a *App) Press(k key.Key) string {
	a.t.Helper()
	event := &backend.RawKeyEvent{Key: k}
	if a.Instance != nil {
		a.Instance.Engine().ProcessRawEvent(event)
		return a.Text()
	}
	// A running loop consumes events from the backend's queue.
	a.Backend.PostEvent(event)
	return a.Text()
}

// StartApp boots node with a running trail loop in a background goroutine, which is
// what [trail.Run] does. Prefer [NewApp] unless the component depends on the loop
// actually running (timers, scheduled macro tasks, Static output).
//
// It returns immediately; the first frame may not be painted yet. Use [App.AwaitFrame]
// or [App.WaitUntil] before asserting.
func StartApp(t *testing.T, node trail.Node, width, height int) *App {
	t.Helper()
	b := mock.New(width, height)
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	go func() {
		_ = trail.Run(ctx, node, trail.WithBackend(b))
	}()
	return &App{t: t, Backend: b}
}

// HasFrame reports whether the backend has painted a frame yet.
func (a *App) HasFrame() bool {
	return a.Backend.LastFrame().Surface != nil
}

// AwaitFrame waits up to timeout for the first painted frame and returns its text.
func (a *App) AwaitFrame(timeout time.Duration) string {
	a.t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if a.HasFrame() {
			return SurfaceText(a.Backend)
		}
		time.Sleep(waitInterval)
	}
	return SurfaceText(a.Backend)
}

// WaitUntil polls frames until cond accepts the text or the timeout elapses, then
// returns the last text seen. It never fails the test, so callers can assert with
// their own message. Use [App.WaitFor] to fail fast with the last frame included.
func (a *App) WaitUntil(cond func(string) bool, timeout time.Duration) string {
	deadline := time.Now().Add(timeout)
	text := SurfaceText(a.Backend)
	for time.Now().Before(deadline) {
		if cond(text) {
			return text
		}
		time.Sleep(waitInterval)
		text = SurfaceText(a.Backend)
	}
	return text
}

// WaitFor renders frames until cond accepts the text, failing the test with the
// last frame if the timeout elapses first. Use it for work that lands
// asynchronously, such as a timer-driven update; prefer [App.Press] when a key
// press causes the change. To assert that something does *not* happen, use
// [App.Press] and assert on the text it returns instead.
func (a *App) WaitFor(cond func(string) bool) string {
	a.t.Helper()
	text := a.WaitUntil(cond, DefaultWaitTimeout)
	if !cond(text) {
		a.t.Fatalf("condition not met within %v, last frame:\n%s", DefaultWaitTimeout, text)
	}
	return text
}

// SurfaceText renders a mock frame as text with trailing spaces stripped and
// blank lines omitted, which is what most assertions want to read.
func SurfaceText(b *mock.Backend) string {
	frame := b.LastFrame().Surface
	if frame == nil {
		return ""
	}
	var sb strings.Builder
	for y := 0; y < frame.Height; y++ {
		line := strings.TrimRight(surfaceLine(frame, y), " ")
		if line == "" {
			continue
		}
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	return sb.String()
}

// SurfaceTextKeepBlanks is [SurfaceText] but preserves blank lines, for assertions
// that depend on vertical spacing.
func SurfaceTextKeepBlanks(b *mock.Backend) string {
	frame := b.LastFrame().Surface
	if frame == nil {
		return ""
	}
	var sb strings.Builder
	for y := 0; y < frame.Height; y++ {
		sb.WriteString(strings.TrimRight(surfaceLine(frame, y), " "))
		sb.WriteByte('\n')
	}
	return sb.String()
}

// SurfaceLines returns a mock frame as non-blank lines.
func SurfaceLines(b *mock.Backend) []string {
	text := strings.TrimSuffix(SurfaceText(b), "\n")
	if text == "" {
		return nil
	}
	return Lines(text)
}

// surfaceLine reads one row of a frame as a string.
func surfaceLine(frame *backend.Buffer, y int) string {
	var line strings.Builder
	for x := 0; x < frame.Width; x++ {
		c := frame.CellAt(x, y)
		if c.Content != "" {
			line.WriteString(c.Content)
		} else {
			line.WriteByte(' ')
		}
	}
	return line.String()
}

// Lines splits rendered text into lines.
func Lines(text string) []string {
	return strings.Split(text, "\n")
}

// TrimmedLines splits rendered text into lines with trailing spaces removed, so
// assertions can reason about indentation without the terminal's right padding.
func TrimmedLines(text string) []string {
	lines := strings.Split(text, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	return lines
}

// TrimmedLinesText trims trailing whitespace per line and the whole block, which
// is the shape most assertions compare against.
func TrimmedLinesText(text string) string {
	return strings.TrimSpace(strings.Join(TrimmedLines(text), "\n"))
}
