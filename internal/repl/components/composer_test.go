package components

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/masterkeysrd/kite/backend"
	"github.com/masterkeysrd/kite/backend/mock"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/tasksmith/internal/ui/autocomplete"
	uicomponents "github.com/masterkeysrd/tasksmith/internal/ui/components"
)

func composerForTest(props ComposerProps) trail.Node {
	cache := kitex.UseMemo(func() *wind.Client {
		cache := wind.NewClient()
		cache.GcTime = time.Millisecond
		return cache
	}, []any{})
	return wind.Provider(wind.ProviderProps{Client: cache}, Composer(props))
}

func composerCompletionEngine(t *testing.T, sources ...autocomplete.Source) *autocomplete.Engine {
	t.Helper()
	engine := autocomplete.New()
	for _, source := range sources {
		if err := engine.Register(source); err != nil {
			t.Fatal(err)
		}
	}
	return engine
}

func composerCommandSource() autocomplete.Source {
	return autocomplete.Source{
		Name: "commands", Trigger: "/", StartOnly: true,
		Complete: autocomplete.Static(
			autocomplete.Candidate{Value: "help", Label: "/help", Description: "Show commands"},
			autocomplete.Candidate{Value: "mode", Label: "/mode [name]", Description: "Change mode", InsertText: "mode "},
			autocomplete.Candidate{Value: "sessions", Label: "/sessions", Description: "List sessions"},
		),
	}
}

func awaitComposer(t *testing.T, inst *trail.Instance, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		inst.Frame()
		if ready() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for composer")
}

func typeComposer(inst *trail.Instance, text string) {
	for _, ch := range text {
		inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: ch, Text: string(ch)}})
	}
}

func pressComposer(inst *trail.Instance, code rune) {
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: code}})
	inst.Frame()
}

func TestComposerCompletesAtUnicodeCursor(t *testing.T) {
	engine := composerCompletionEngine(t, autocomplete.Source{
		Name: "files", Trigger: "@", Complete: autocomplete.Static(
			autocomplete.Candidate{Value: "source.go"},
			autocomplete.Candidate{Value: "source_test.go"},
		),
	})
	backend := mock.New(80, 24)
	var submitted string
	component := trail.SimpleFC("CursorComposer", func() trail.Node {
		return composerForTest(ComposerProps{Autocomplete: engine, OnSubmit: func(text string) { submitted = text }})
	})
	inst, err := trail.New(component(), trail.WithBackend(backend))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(inst.Unmount)
	inst.Frame()
	typeComposer(inst, "\u6587 @sourest tail")
	inst.Frame()
	for range "rest tail" {
		pressComposer(inst, key.KeyLeft)
	}
	awaitComposer(t, inst, func() bool { return strings.Contains(surfaceText(backend), "source_test.go") })
	pressComposer(inst, key.KeyTab)
	if submitted != "" || !strings.Contains(surfaceText(backend), "@source.go tail") {
		t.Fatalf("completion submitted %q or replaced surrounding text:\n%s", submitted, surfaceText(backend))
	}
	typeComposer(inst, "!")
	inst.Frame()
	if !strings.Contains(surfaceText(backend), "@source.go! tail") {
		t.Fatalf("caret was not restored after completion:\n%s", surfaceText(backend))
	}
	pressComposer(inst, key.KeyEscape)
	pressComposer(inst, key.KeyEnter)
	if submitted != "\u6587 @source.go! tail" {
		t.Errorf("submitted = %q, want Unicode prefix and untouched suffix", submitted)
	}
}

func TestComposerCommandToArgumentCompletion(t *testing.T) {
	engine := composerCompletionEngine(t, composerCommandSource(), autocomplete.Source{
		Name: "modes", Trigger: "/mode ", StartOnly: true,
		Complete: autocomplete.Static(autocomplete.Candidate{Value: "plan", Label: "Plan mode"}),
	})
	backend := mock.New(80, 24)
	var submitted string
	component := trail.SimpleFC("CascadeComposer", func() trail.Node {
		return composerForTest(ComposerProps{Autocomplete: engine, OnSubmit: func(text string) { submitted = text }})
	})
	inst, err := trail.New(component(), trail.WithBackend(backend))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(inst.Unmount)
	inst.Frame()
	typeComposer(inst, "/mo")
	awaitComposer(t, inst, func() bool { return strings.Contains(surfaceText(backend), "\u276f /mode [name]") })
	pressComposer(inst, key.KeyTab)
	awaitComposer(t, inst, func() bool { return strings.Contains(surfaceText(backend), "Plan mode") })
	pressComposer(inst, key.KeyEnter)
	if submitted != "" || !strings.Contains(surfaceText(backend), "> /mode plan") {
		t.Fatalf("argument acceptance submitted %q or lost the draft:\n%s", submitted, surfaceText(backend))
	}
	pressComposer(inst, key.KeyEnter)
	if submitted != "/mode plan" {
		t.Errorf("submitted = %q, want /mode plan", submitted)
	}
}

func TestComposerEscapeAndHistory(t *testing.T) {
	engine := composerCompletionEngine(t, composerCommandSource())
	backend := mock.New(80, 24)
	canceled := 0
	var submitted []string
	component := trail.SimpleFC("DismissComposer", func() trail.Node {
		return composerForTest(ComposerProps{
			Autocomplete: engine,
			OnCancel:     func() { canceled++ },
			OnSubmit:     func(text string) { submitted = append(submitted, text) },
		})
	})
	inst, err := trail.New(component(), trail.WithBackend(backend))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(inst.Unmount)
	inst.Frame()
	typeComposer(inst, "/h")
	awaitComposer(t, inst, func() bool { return strings.Contains(surfaceText(backend), "Show commands") })
	pressComposer(inst, key.KeyEscape)
	if canceled != 0 || strings.Contains(surfaceText(backend), "Show commands") {
		t.Fatal("Escape did not dismiss suggestions before canceling")
	}
	pressComposer(inst, key.KeyEscape)
	if canceled != 1 {
		t.Fatal("second Escape did not reach OnCancel")
	}
	pressComposer(inst, key.KeyEnter)
	if len(submitted) != 1 || submitted[0] != "/h" {
		t.Fatalf("dismissal changed the submitted draft: %v", submitted)
	}
	typeComposer(inst, "draft")
	inst.Frame()
	pressComposer(inst, key.KeyUp)
	if !strings.Contains(surfaceText(backend), "> /h") {
		t.Fatal("Up did not restore prompt history")
	}
	// Dismiss the recalled command's suggestions before using history again.
	pressComposer(inst, key.KeyEscape)
	pressComposer(inst, key.KeyDown)
	if !strings.Contains(surfaceText(backend), "> draft") {
		t.Fatalf("Down did not restore the draft:\n%s", surfaceText(backend))
	}
}

func TestComposerFunctionalComponent(t *testing.T) {
	cycleCalled := false
	cancelCalled := false

	b := mock.New(80, 24)
	app := trail.SimpleFC("TestApp", func() kitex.Node {
		return composerForTest(ComposerProps{
			OnCycleMode: func() {
				cycleCalled = true
			},
			OnCancel: func() {
				cancelCalled = true
			},
		})
	})

	inst, err := trail.New(app(), trail.WithBackend(b))
	if err != nil {
		t.Fatalf("trail.New: %v", err)
	}
	inst.Frame()

	// Initial render contains prompt symbol and has height 3 (top border + 1 row + bottom border)
	out := surfaceText(b)
	if !strings.Contains(out, ">") {
		t.Errorf("composer missing '>':\n%s", out)
	}
	if h := b.LastFrame().Surface.Height; h != 3 {
		t.Errorf("expected initial composer height 3, got %d:\n%s", h, out)
	}

	// Space and Shift+A must NOT trigger OnCycleMode
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{
		Key: key.Key{Code: ' ', Text: " "},
	})
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{
		Key: key.Key{Code: 'a', Text: "A", Mod: key.ModShift},
	})
	if cycleCalled {
		t.Errorf("OnCycleMode should not be called on space or Shift+A")
	}

	// Shift+Enter inserts a newline and grows the composer height to 4
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{
		Key: key.Key{Code: key.KeyEnter, Mod: key.ModShift},
	})
	inst.Frame()
	if h := b.LastFrame().Surface.Height; h != 4 {
		t.Errorf("expected composer height 4 after Shift+Enter newline, got %d:\n%s", h, surfaceText(b))
	}

	// Send Shift+Tab key event
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{
		Key: key.Key{Code: key.KeyTab, Mod: key.ModShift},
	})
	if !cycleCalled {
		t.Errorf("expected OnCycleMode to be called on Shift+Tab")
	}

	// Send Escape key event
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{
		Key: key.Key{Code: key.KeyEscape},
	})
	if !cancelCalled {
		t.Errorf("expected OnCancel to be called on Escape")
	}
}

func TestComposerSuggestArgsAndSelection(t *testing.T) {
	opts := []uicomponents.Option{
		{Value: "default", Label: "default", Description: "Allow safe reads", Current: true},
		{Value: "accept_edits", Label: "accept_edits", Description: "Allow safe reads and workspace file edits"},
		{Value: "plan", Label: "plan", Description: "Read-only mode"},
		{Value: "ask", Label: "ask", Description: "Ask for confirmation"},
		{Value: "auto", Label: "auto", Description: "Allow all actions"},
	}

	var submittedText string
	engine := composerCompletionEngine(t, autocomplete.Source{
		Name: "modes", Trigger: "/mode ", StartOnly: true,
		Complete: func(context.Context, autocomplete.Request) ([]autocomplete.Candidate, error) {
			candidates := make([]autocomplete.Candidate, len(opts))
			for i, opt := range opts {
				candidates[i] = autocomplete.Candidate{
					Value: opt.Value, Label: opt.Label, Description: opt.Description,
				}
			}
			return candidates, nil
		},
	})
	b := mock.New(80, 24)
	app := trail.SimpleFC("TestApp", func() kitex.Node {
		return composerForTest(ComposerProps{
			Autocomplete: engine,
			OnSubmit: func(text string) {
				submittedText = text
			},
		})
	})

	inst, err := trail.New(app(), trail.WithBackend(b))
	if err != nil {
		t.Fatalf("trail.New: %v", err)
	}
	inst.Frame()

	// 1. Type "/mode " into composer
	for _, ch := range "/mode " {
		inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{
			Key: key.Key{Code: ch, Text: string(ch)},
		})
	}
	awaitComposer(t, inst, func() bool { return strings.Contains(surfaceText(b), "accept_edits") })

	out := surfaceText(b)
	if !strings.Contains(out, "default") || !strings.Contains(out, "accept_edits") || !strings.Contains(out, "plan") {
		t.Fatalf("expected mode options table below composer, got:\n%s", out)
	}

	// Verify alignment: row 0 has "❯ default", row 1 has "  accept_edits"
	// Both label texts must start at the exact same X column
	frame := b.LastFrame().Surface
	if frame == nil {
		t.Fatalf("expected rendered frame")
	}

	var row0Y, row1Y int
	for y := 0; y < frame.Height; y++ {
		rowStr := ""
		for x := 0; x < frame.Width; x++ {
			rowStr += frame.CellAt(x, y).Content
		}
		if strings.Contains(rowStr, "default") && strings.Contains(rowStr, "❯") {
			row0Y = y
		}
		if strings.Contains(rowStr, "accept_edits") {
			row1Y = y
		}
	}

	// In row0, "default" starts at X=2 (after "❯ ")
	if frame.CellAt(0, row0Y).Content != "❯" || frame.CellAt(1, row0Y).Content != " " || frame.CellAt(2, row0Y).Content != "d" {
		t.Errorf("row0 cursor/label mismatch: [%q, %q, %q]",
			frame.CellAt(0, row0Y).Content, frame.CellAt(1, row0Y).Content, frame.CellAt(2, row0Y).Content)
	}
	// In row1, "accept_edits" starts at X=2 (after "  ")
	if frame.CellAt(0, row1Y).Content != " " || frame.CellAt(1, row1Y).Content != " " || frame.CellAt(2, row1Y).Content != "a" {
		t.Errorf("row1 cursor/label mismatch: [%q, %q, %q] - leading spaces must not be stripped",
			frame.CellAt(0, row1Y).Content, frame.CellAt(1, row1Y).Content, frame.CellAt(2, row1Y).Content)
	}

	// 2. Press Down -> moves selection to accept_edits
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{
		Key: key.Key{Code: key.KeyDown},
	})
	inst.Frame()

	frameDown := b.LastFrame().Surface
	// Now row0 has unselected "  default" starting at X=2
	if frameDown.CellAt(0, row0Y).Content != " " || frameDown.CellAt(1, row0Y).Content != " " || frameDown.CellAt(2, row0Y).Content != "d" {
		t.Errorf("after Down, row0 mismatch: [%q, %q, %q]",
			frameDown.CellAt(0, row0Y).Content, frameDown.CellAt(1, row0Y).Content, frameDown.CellAt(2, row0Y).Content)
	}
	// And row1 has selected "❯ accept_edits" starting at X=2
	if frameDown.CellAt(0, row1Y).Content != "❯" || frameDown.CellAt(1, row1Y).Content != " " || frameDown.CellAt(2, row1Y).Content != "a" {
		t.Errorf("after Down, row1 mismatch: [%q, %q, %q]",
			frameDown.CellAt(0, row1Y).Content, frameDown.CellAt(1, row1Y).Content, frameDown.CellAt(2, row1Y).Content)
	}

	// Accepting a suggestion updates the draft without submitting it.
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{
		Key: key.Key{Code: key.KeyEnter},
	})
	inst.Frame()
	if submittedText != "" || !strings.Contains(surfaceText(b), "> /mode accept_edits") {
		t.Fatalf("accepting a suggestion submitted %q or lost the draft:\n%s", submittedText, surfaceText(b))
	}
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyEnter}})
	inst.Frame()
	if submittedText != "/mode accept_edits" {
		t.Errorf("submittedText = %q, want '/mode accept_edits'", submittedText)
	}
}

func TestComposerSlashCommandNavigationAndSelection(t *testing.T) {
	engine := composerCompletionEngine(t, composerCommandSource())

	var submittedText string
	b := mock.New(80, 24)
	app := trail.SimpleFC("TestApp", func() kitex.Node {
		return composerForTest(ComposerProps{
			Autocomplete: engine,
			OnSubmit: func(text string) {
				submittedText = text
			},
		})
	})

	inst, err := trail.New(app(), trail.WithBackend(b))
	if err != nil {
		t.Fatalf("trail.New: %v", err)
	}
	inst.Frame()

	// 1. Type "/"
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{
		Key: key.Key{Code: '/', Text: "/"},
	})
	awaitComposer(t, inst, func() bool { return strings.Contains(surfaceText(b), "❯ /help") })

	out := surfaceText(b)
	if !strings.Contains(out, "❯ /help") {
		t.Fatalf("expected row 0 highlighted with '❯ /help', got:\n%s", out)
	}
	if !strings.Contains(out, "  /mode [name]") {
		t.Errorf("expected row 1 unselected '  /mode', got:\n%s", out)
	}

	// 2. Press Down -> moves selection to row 1 (/mode)
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{
		Key: key.Key{Code: key.KeyDown},
	})
	inst.Frame()

	outDown := surfaceText(b)
	if !strings.Contains(outDown, "  /help") || !strings.Contains(outDown, "❯ /mode [name]") {
		t.Fatalf("expected row 1 highlighted with '❯ /mode [name]', got:\n%s", outDown)
	}

	// Enter accepts the highlighted command; a second Enter submits it.
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{
		Key: key.Key{Code: key.KeyEnter},
	})
	inst.Frame()

	if submittedText != "" {
		t.Fatalf("accepting a command submitted %q", submittedText)
	}
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyEnter}})
	inst.Frame()
	if submittedText != "/mode" {
		t.Errorf("submittedText = %q, want '/mode'", submittedText)
	}
}

func TestComposerSlashCommandTabCompletion(t *testing.T) {
	engine := composerCompletionEngine(t, composerCommandSource())

	b := mock.New(80, 24)
	app := trail.SimpleFC("TestApp", func() kitex.Node {
		return composerForTest(ComposerProps{
			Autocomplete: engine,
		})
	})

	inst, err := trail.New(app(), trail.WithBackend(b))
	if err != nil {
		t.Fatalf("trail.New: %v", err)
	}
	inst.Frame()

	// 1. Type "/"
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{
		Key: key.Key{Code: '/', Text: "/"},
	})
	awaitComposer(t, inst, func() bool { return strings.Contains(surfaceText(b), "❯ /help") })

	// 2. Press Down -> selects /mode
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{
		Key: key.Key{Code: key.KeyDown},
	})
	inst.Frame()

	// 3. Press Tab -> completes "/mode "
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{
		Key: key.Key{Code: key.KeyTab},
	})
	inst.Frame()

	// 4. Type 'x' to verify the trailing space after tab completion
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{
		Key: key.Key{Code: 'x', Text: "x"},
	})
	inst.Frame()

	outTab := surfaceText(b)
	if !strings.Contains(outTab, "> /mode x") {
		t.Errorf("expected input to contain '> /mode x' after Tab and typing 'x', got:\n%s", outTab)
	}
}

func TestComposerEnterSubmitsCompleteCommandAndCompletesPartial(t *testing.T) {
	engine := composerCompletionEngine(t, composerCommandSource())

	var submitted []string
	b := mock.New(80, 24)
	app := trail.SimpleFC("TestApp", func() kitex.Node {
		return composerForTest(ComposerProps{
			Autocomplete: engine,
			OnSubmit:     func(text string) { submitted = append(submitted, text) },
		})
	})
	inst, err := trail.New(app(), trail.WithBackend(b))
	if err != nil {
		t.Fatalf("trail.New: %v", err)
	}
	inst.Frame()

	typeText := func(text string) {
		for _, r := range text {
			inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: r, Text: string(r)}})
		}
	}
	enter := func() {
		inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyEnter}})
		inst.Frame()
	}

	// A partial command name completes without submitting.
	typeText("/hel")
	awaitComposer(t, inst, func() bool { return strings.Contains(surfaceText(b), "❯ /help") })
	enter()
	if len(submitted) != 0 {
		t.Fatalf("completing a partial command submitted %v", submitted)
	}
	if !strings.Contains(surfaceText(b), "> /help") {
		t.Fatalf("partial command was not completed:\n%s", surfaceText(b))
	}

	// The complete command name submits on the next Enter.
	enter()
	if len(submitted) != 1 || submitted[0] != "/help" {
		t.Fatalf("submitted = %v, want [/help]", submitted)
	}

	// Typing the full name submits with a single Enter.
	typeText("/help")
	awaitComposer(t, inst, func() bool { return strings.Contains(surfaceText(b), "❯ /help") })
	enter()
	if len(submitted) != 2 || submitted[1] != "/help" {
		t.Fatalf("submitted = %v, want a second /help after one Enter", submitted)
	}
}
