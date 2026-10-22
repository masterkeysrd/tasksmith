package ui

import (
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
