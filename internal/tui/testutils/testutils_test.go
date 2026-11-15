package testutils

import (
	"strings"
	"testing"

	"github.com/masterkeysrd/kite/backend"
	"github.com/masterkeysrd/kite/backend/mock"
	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/kite/style"
)

type labelProps struct{ Text string }

var label = trail.FC("Label", func(props labelProps) trail.Node {
	return trail.Span(trail.SpanProps{
		Style: style.S().WhiteSpace(style.WhiteSpacePre),
	}, trail.Text(props.Text))
})

type keyProps struct {
	onX func()
}

var keyEcho = trail.FC("KeyEcho", func(props keyProps) trail.Node {
	text, setText := kitex.UseState("idle")
	trail.UseInput(func(_ string, k event.KeyEvent) {
		if k.MatchString("x") {
			if props.onX != nil {
				props.onX()
			}
			setText("pressed")
		}
	})
	return label(labelProps{text()})
})

func TestRenderShowsComponentText(t *testing.T) {
	got := Render(t, label(labelProps{"hello"}), 40, 5)
	if got != "hello" {
		t.Fatalf("Render() = %q, want %q", got, "hello")
	}
}

func TestEnvReRendersSameContainer(t *testing.T) {
	env := NewEnv(t, 40, 5)
	if got := env.Render(label(labelProps{"first"})); got != "first" {
		t.Fatalf("first render = %q", got)
	}
	if got := env.Render(label(labelProps{"second"})); got != "second" {
		t.Fatalf("second render = %q, want the container updated in place", got)
	}
}

func TestEnvPressFlushesBeforeReturning(t *testing.T) {
	var fired int
	env := NewEnv(t, 40, 5)
	if got := env.Render(keyEcho(keyProps{onX: func() { fired++ }})); got != "idle" {
		t.Fatalf("initial render = %q, want %q", got, "idle")
	}
	if got := env.Press(key.Key{Code: 'x', Text: "x"}); got != "pressed" {
		t.Fatalf("after Press = %q, want the post-key frame %q", got, "pressed")
	}
	if fired != 1 {
		t.Fatalf("handler fired %d times, want 1", fired)
	}
}

func TestEnvPressIgnoresUnboundKey(t *testing.T) {
	var fired int
	env := NewEnv(t, 40, 5)
	env.Render(keyEcho(keyProps{onX: func() { fired++ }}))
	if got := env.Press(key.Key{Code: 'z', Text: "z"}); got != "idle" {
		t.Fatalf("after unbound key = %q, want %q", got, "idle")
	}
	if fired != 0 {
		t.Fatalf("handler fired %d times for an unbound key, want 0", fired)
	}
}

func TestAppRendersAndAcceptsKeys(t *testing.T) {
	var fired int
	app := NewApp(t, keyEcho(keyProps{onX: func() { fired++ }}), 40, 5)
	if got := app.Text(); got != "idle" {
		t.Fatalf("App.Text() = %q, want %q", got, "idle")
	}
	if got := app.Press(key.Key{Code: 'x', Text: "x"}); !strings.Contains(got, "pressed") {
		t.Fatalf("after Press = %q, want it to contain %q", got, "pressed")
	}
	if fired != 1 {
		t.Fatalf("handler fired %d times, want 1", fired)
	}
}

func TestAppWaitForResolvesAsynchronousUpdate(t *testing.T) {
	app := NewApp(t, label(labelProps{"eventually"}), 40, 5)
	got := app.WaitFor(func(text string) bool {
		return strings.Contains(text, "eventually")
	})
	if !strings.Contains(got, "eventually") {
		t.Fatalf("WaitFor returned %q", got)
	}
}

// drawFrame writes rows into a mock backend as one completed frame, so the surface
// readers can be tested without depending on how the layout engine positions nodes.
func drawFrame(t *testing.T, rows ...string) *mock.Backend {
	t.Helper()
	height := len(rows)
	width := 1
	for _, r := range rows {
		if len(r) > width {
			width = len(r)
		}
	}
	b := mock.New(width, height)
	surface := b.BeginFrame()
	for y, row := range rows {
		for x, r := range row {
			surface.Set(x, y, backend.Cell{Content: string(r), Width: 1})
		}
	}
	if err := b.EndFrame(); err != nil {
		t.Fatalf("EndFrame: %v", err)
	}
	return b
}

func TestSurfaceTextOnEmptyBackend(t *testing.T) {
	b := mock.New(10, 5)
	if got := SurfaceText(b); got != "" {
		t.Fatalf("SurfaceText with no frames = %q, want empty", got)
	}
	if got := SurfaceTextKeepBlanks(b); got != "" {
		t.Fatalf("SurfaceTextKeepBlanks with no frames = %q, want empty", got)
	}
	if got := SurfaceLines(b); len(got) != 0 {
		t.Fatalf("SurfaceLines with no frames = %q, want empty", got)
	}
}

// TestSurfaceTextVariantsDifferOnBlankLines pins the distinction between the two
// surface readers, since assertions depend on vertical spacing either being
// collapsed or preserved.
func TestSurfaceTextVariantsDifferOnBlankLines(t *testing.T) {
	b := drawFrame(t, "top", "", "bottom")

	collapsed := SurfaceText(b)
	kept := SurfaceTextKeepBlanks(b)

	if want := "top\nbottom\n"; collapsed != want {
		t.Errorf("SurfaceText() = %q, want %q (blank row should be dropped)", collapsed, want)
	}
	if want := "top\n\nbottom\n"; kept != want {
		t.Errorf("SurfaceTextKeepBlanks() = %q, want %q (blank row should be kept)", kept, want)
	}
	if got := SurfaceLines(b); len(got) != 2 || got[0] != "top" || got[1] != "bottom" {
		t.Errorf("SurfaceLines() = %q, want [top bottom]", got)
	}
}

// TestRenderKeepsInteriorPadding pins the exact text shape the repo's tests assert
// against. testenv pads every line to the full terminal width, and the existing
// helpers only TrimSpace the whole dump, so interior lines keep their padding and
// column-aligned output stays aligned. A helper that trimmed each line instead
// would silently change what an existing assertion matches.
func TestRenderKeepsInteriorPadding(t *testing.T) {
	env := NewEnv(t, 40, 5)
	got := env.Render(label(labelProps{"alpha\nbeta"}))

	if want := "alpha                                   \nbeta"; got != want {
		t.Fatalf("Render() = %q, want %q", got, want)
	}
	// The per-line trimmed variant is available when trailing padding is unwanted.
	if want := "alpha\nbeta"; TrimmedLinesText(env.DumpText()) != want {
		t.Fatalf("TrimmedLinesText() = %q, want %q", TrimmedLinesText(env.DumpText()), want)
	}
}

func TestLineHelpers(t *testing.T) {
	if got := TrimmedLines("a  \nb"); len(got) != 2 || got[0] != "a" {
		t.Fatalf("TrimmedLines = %q", got)
	}
	if got := TrimmedLinesText("  a  \n  \n"); got != "a" {
		t.Fatalf("TrimmedLinesText = %q, want %q", got, "a")
	}
	if got := Lines("a\nb"); len(got) != 2 {
		t.Fatalf("Lines = %q", got)
	}
}

// TestRenderDoesNotMutateInput guards the helper's contract that text is returned,
// not written anywhere shared.
func TestRenderIsRepeatable(t *testing.T) {
	node := label(labelProps{"same"})
	first := Render(t, node, 40, 5)
	second := Render(t, node, 40, 5)
	if first != second {
		t.Fatalf("rendering the same node twice differed: %q vs %q", first, second)
	}
}
