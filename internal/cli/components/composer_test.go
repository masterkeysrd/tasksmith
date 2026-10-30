package components

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/masterkeysrd/kite/backend"
	"github.com/masterkeysrd/kite/backend/mock"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/ui/autocomplete"
)

func createComposerInstance(t *testing.T, props ComposerProps) (*trail.Instance, *mock.Backend) {
	t.Helper()
	b := mock.New(80, 24)
	qc := wind.NewClient()
	app := trail.SimpleFC("TestApp", func() trail.Node {
		return wind.Provider(wind.ProviderProps{Client: qc}, Composer(props))
	})
	inst, err := trail.New(app(), trail.WithBackend(b))
	if err != nil {
		t.Fatalf("trail.New: %v", err)
	}
	t.Cleanup(inst.Unmount)
	inst.Frame()
	return inst, b
}

func typeText(inst *trail.Instance, text string) {
	for _, r := range text {
		inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: r, Text: string(r)}})
	}
	inst.Frame()
}

func pressKey(inst *trail.Instance, code rune, mod key.Mod) {
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: code, Mod: mod}})
	inst.Frame()
}

func TestComposerEnterSubmitsAndClears(t *testing.T) {
	xdg.SetTestEnv(t.TempDir(), "tasksmith")
	defer xdg.ClearCache()

	store.ClearSlot()
	store.ResetApp()
	defer store.ClearSlot()
	defer store.ResetApp()

	var submitted string
	inst, b := createComposerInstance(t, ComposerProps{
		OnSubmit: func(text string) {
			submitted = text
		},
	})

	typeText(inst, "hello world")
	if !strings.Contains(surfaceText(b), "> hello world") {
		t.Fatalf("expected typed text on surface, got:\n%s", surfaceText(b))
	}

	pressKey(inst, key.KeyEnter, 0)
	if submitted != "hello world" {
		t.Fatalf("expected submitted 'hello world', got %q", submitted)
	}
	if strings.Contains(surfaceText(b), "hello world") {
		t.Fatalf("expected input to be cleared after submit, got:\n%s", surfaceText(b))
	}
}

func TestComposerShiftEnterInsertsNewline(t *testing.T) {
	store.ClearSlot()
	store.ResetApp()
	defer store.ClearSlot()
	defer store.ResetApp()

	var submitted string
	inst, b := createComposerInstance(t, ComposerProps{
		OnSubmit: func(text string) {
			submitted = text
		},
	})

	typeText(inst, "line 1")
	pressKey(inst, key.KeyEnter, key.ModShift)
	typeText(inst, "line 2")

	if submitted != "" {
		t.Fatalf("shift+enter should not submit, got %q", submitted)
	}

	out := surfaceText(b)
	if !strings.Contains(out, "line 1") || !strings.Contains(out, "line 2") {
		t.Fatalf("expected multiline input on surface, got:\n%s", out)
	}
}

func TestComposerHistoryNavigation(t *testing.T) {
	xdg.SetTestEnv(t.TempDir(), "tasksmith")
	defer xdg.ClearCache()

	store.ClearSlot()
	store.ResetApp()
	defer store.ClearSlot()
	defer store.ResetApp()

	var submitted []string
	inst, b := createComposerInstance(t, ComposerProps{
		OnSubmit: func(text string) {
			submitted = append(submitted, text)
		},
	})

	// Submit two commands
	typeText(inst, "cmd 1")
	pressKey(inst, key.KeyEnter, 0)

	typeText(inst, "cmd 2")
	pressKey(inst, key.KeyEnter, 0)

	if len(submitted) != 2 {
		t.Fatalf("expected 2 submissions, got %v", submitted)
	}

	// Type an unsent draft
	typeText(inst, "unsent draft")

	// Press Up -> should show "cmd 2"
	pressKey(inst, key.KeyUp, 0)
	if !strings.Contains(surfaceText(b), "> cmd 2") {
		t.Fatalf("expected 'cmd 2' on Up, got:\n%s", surfaceText(b))
	}

	// Press Up -> should show "cmd 1"
	pressKey(inst, key.KeyUp, 0)
	if !strings.Contains(surfaceText(b), "> cmd 1") {
		t.Fatalf("expected 'cmd 1' on second Up, got:\n%s", surfaceText(b))
	}

	// Press Down -> should show "cmd 2"
	pressKey(inst, key.KeyDown, 0)
	if !strings.Contains(surfaceText(b), "> cmd 2") {
		t.Fatalf("expected 'cmd 2' on Down, got:\n%s", surfaceText(b))
	}

	// Press Down -> should restore "unsent draft"
	pressKey(inst, key.KeyDown, 0)
	if !strings.Contains(surfaceText(b), "> unsent draft") {
		t.Fatalf("expected 'unsent draft' restored, got:\n%s", surfaceText(b))
	}
}

func TestComposerShiftTabCyclesMode(t *testing.T) {
	store.ClearSlot()
	store.ResetApp()
	store.ResetSession()
	defer store.ClearSlot()
	defer store.ResetApp()
	defer store.ResetSession()

	// 1. Default cycling via store.CycleMode
	inst, _ := createComposerInstance(t, ComposerProps{})

	if store.GetSession().Config.Mode != api.ModeDefault {
		t.Fatalf("expected initial ModeDefault, got %q", store.GetSession().Config.Mode)
	}

	pressKey(inst, key.KeyTab, key.ModShift)
	if store.GetSession().Config.Mode != api.ModeAcceptEdits {
		t.Fatalf("expected ModeAcceptEdits after Shift+Tab, got %q", store.GetSession().Config.Mode)
	}

	pressKey(inst, key.KeyTab, key.ModShift)
	if store.GetSession().Config.Mode != api.ModePlan {
		t.Fatalf("expected ModePlan after Shift+Tab, got %q", store.GetSession().Config.Mode)
	}

	// 2. Custom OnCycleMode override
	customCalled := false
	inst2, _ := createComposerInstance(t, ComposerProps{
		OnCycleMode: func() {
			customCalled = true
		},
	})
	pressKey(inst2, key.KeyTab, key.ModShift)
	if !customCalled {
		t.Fatal("expected custom OnCycleMode to be called")
	}
}

func TestComposerEscCancels(t *testing.T) {
	store.ClearSlot()
	store.ResetApp()
	defer store.ClearSlot()
	defer store.ResetApp()

	canceled := false
	inst, _ := createComposerInstance(t, ComposerProps{
		OnCancel: func() {
			canceled = true
		},
	})

	pressKey(inst, key.KeyEsc, 0)
	if !canceled {
		t.Fatal("expected OnCancel to be called on Esc")
	}
}

func TestComposerHidesWhenSlotEnqueuedAndRestoresDraft(t *testing.T) {
	store.ClearSlot()
	store.ResetApp()
	defer store.ClearSlot()
	defer store.ResetApp()

	inst, b := createComposerInstance(t, ComposerProps{})

	// Type draft
	typeText(inst, "preserve this draft")
	if !strings.Contains(surfaceText(b), "> preserve this draft") {
		t.Fatalf("expected draft on surface, got:\n%s", surfaceText(b))
	}

	// Enqueue a slot item that hides the composer (e.g. Permission)
	store.EnqueueSlot(store.SlotItem{
		ID:   "perm-1",
		Kind: store.SlotKindPermission,
		Data: "permission data",
	})
	inst.Frame()

	// Composer should now be hidden (DisplayNone)
	if strings.Contains(surfaceText(b), "preserve this draft") {
		t.Fatalf("expected composer to be hidden when slot active, got:\n%s", surfaceText(b))
	}

	// Dequeue slot -> Composer should reappear with draft intact
	store.DequeueSlot()
	inst.Frame()

	if !strings.Contains(surfaceText(b), "> preserve this draft") {
		t.Fatalf("expected preserved draft after slot dequeue, got:\n%s", surfaceText(b))
	}
}

func TestComposerCtrlCClearsDraft(t *testing.T) {
	store.ClearSlot()
	store.ResetApp()
	defer store.ClearSlot()
	defer store.ResetApp()

	inst, b := createComposerInstance(t, ComposerProps{})

	typeText(inst, "abandon this draft")
	if !strings.Contains(surfaceText(b), "> abandon this draft") {
		t.Fatalf("expected draft on surface, got:\n%s", surfaceText(b))
	}

	// Set CtrlCPending -> clears draft
	store.SetCtrlCPending(true)
	inst.Frame()

	if strings.Contains(surfaceText(b), "abandon this draft") {
		t.Fatalf("expected draft to be cleared on CtrlCPending, got:\n%s", surfaceText(b))
	}
}

func TestComposerHistoryPersistsToDisk(t *testing.T) {
	xdg.SetTestEnv(t.TempDir(), "tasksmith")
	defer xdg.ClearCache()
	store.ClearSlot()
	store.ResetApp()
	defer store.ClearSlot()
	defer store.ResetApp()

	// Instance 1: submit a prompt which is persisted to disk.
	var submitted []string
	inst1, _ := createComposerInstance(t, ComposerProps{
		OnSubmit: func(text string) { submitted = append(submitted, text) },
	})
	typeText(inst1, "persisted prompt")
	pressKey(inst1, key.KeyEnter, 0)
	if len(submitted) != 1 || submitted[0] != "persisted prompt" {
		t.Fatalf("expected submission, got %v", submitted)
	}
	inst1.Unmount()

	// Instance 2: should load "persisted prompt" from disk on mount.
	inst2, b2 := createComposerInstance(t, ComposerProps{})
	pressKey(inst2, key.KeyUp, 0)
	if !strings.Contains(surfaceText(b2), "persisted prompt") {
		t.Fatalf("expected 'persisted prompt' in new instance, got:\n%s", surfaceText(b2))
	}
}

func TestComposerHistoryMultiLineRoundTrip(t *testing.T) {
	xdg.SetTestEnv(t.TempDir(), "tasksmith")
	defer xdg.ClearCache()
	store.ClearSlot()
	store.ResetApp()
	defer store.ClearSlot()
	defer store.ResetApp()

	// Instance 1: submit a multi-line prompt via Shift+Enter.
	var submitted string
	inst1, _ := createComposerInstance(t, ComposerProps{
		OnSubmit: func(text string) { submitted = text },
	})
	typeText(inst1, "first line")
	pressKey(inst1, key.KeyEnter, key.ModShift)
	typeText(inst1, "second line")
	pressKey(inst1, key.KeyEnter, 0)

	if submitted != "first line\nsecond line" {
		t.Fatalf("expected multi-line submission, got %q", submitted)
	}
	inst1.Unmount()

	// Instance 2: navigate to find the multi-line prompt intact.
	inst2, b2 := createComposerInstance(t, ComposerProps{})
	pressKey(inst2, key.KeyUp, 0)
	out := surfaceText(b2)
	if !strings.Contains(out, "first line") || !strings.Contains(out, "second line") {
		t.Fatalf("expected multi-line prompt in new instance, got:\n%s", out)
	}
}

func TestComposerHistoryDeduplication(t *testing.T) {
	xdg.SetTestEnv(t.TempDir(), "tasksmith")
	defer xdg.ClearCache()
	store.ClearSlot()
	store.ResetApp()
	defer store.ClearSlot()
	defer store.ResetApp()

	// Submit "hello" in instance 1.
	inst1, _ := createComposerInstance(t, ComposerProps{})
	typeText(inst1, "hello")
	pressKey(inst1, key.KeyEnter, 0)
	inst1.Unmount()

	// Instance 2 loads "hello" from disk; submitting "hello" again should be deduped.
	inst2, _ := createComposerInstance(t, ComposerProps{})
	typeText(inst2, "hello")
	pressKey(inst2, key.KeyEnter, 0)
	inst2.Unmount()

	// Verify only one entry on disk.
	entries := loadHistoryFromFile()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry after dedup, got %d: %v", len(entries), entries)
	}
	if entries[0] != "hello" {
		t.Fatalf("expected 'hello', got %q", entries[0])
	}
}

func TestComposerHistoryCap500(t *testing.T) {
	xdg.SetTestEnv(t.TempDir(), "tasksmith")
	defer xdg.ClearCache()

	// Write 600 entries directly, then verify loadHistoryFromFile caps at 500.
	entries := make([]string, 0, 600)
	for i := range 600 {
		entries = append(entries, fmt.Sprintf("entry-%d", i))
	}
	if err := persistHistory(entries); err != nil {
		t.Fatalf("persistHistory: %v", err)
	}

	loaded := loadHistoryFromFile()
	if len(loaded) != 500 {
		t.Fatalf("expected 500 entries after cap, got %d", len(loaded))
	}
	// The most recent 500 are entries[100:600].
	if loaded[0] != "entry-100" {
		t.Fatalf("expected first entry to be 'entry-100', got %q", loaded[0])
	}
	if loaded[499] != "entry-599" {
		t.Fatalf("expected last entry to be 'entry-599', got %q", loaded[499])
	}
}

func TestComposerBashModeActivationAndCancellation(t *testing.T) {
	xdg.SetTestEnv(t.TempDir(), "tasksmith")
	defer xdg.ClearCache()

	store.ClearSlot()
	store.ResetApp()
	defer store.ClearSlot()
	defer store.ResetApp()

	inst, b := createComposerInstance(t, ComposerProps{})

	// Initially, prompt is ">" and hint is DefaultHint.
	if !strings.Contains(surfaceText(b), ">") {
		t.Fatalf("expected initial prompt '>', got:\n%s", surfaceText(b))
	}
	if store.GetApp().Hint != store.DefaultHint {
		t.Fatalf("expected hint %q, got %q", store.DefaultHint, store.GetApp().Hint)
	}

	// 1. Press '!' on empty draft -> activates bash mode.
	typeText(inst, "!")
	out := surfaceText(b)
	if !strings.Contains(out, "!") {
		t.Fatalf("expected prompt '!' in bash mode, got:\n%s", out)
	}
	if strings.Contains(out, "! !") {
		t.Fatalf("expected draft to remain empty in bash mode, got:\n%s", out)
	}
	if store.GetApp().Hint != BashModeHint {
		t.Fatalf("expected hint %q in bash mode, got %q", BashModeHint, store.GetApp().Hint)
	}

	// 2. Press backspace on empty draft in bash mode -> exits bash mode.
	pressKey(inst, key.KeyBackspace, 0)
	out = surfaceText(b)
	if !strings.Contains(out, ">") {
		t.Fatalf("expected prompt restored to '>', got:\n%s", out)
	}
	if store.GetApp().Hint != store.DefaultHint {
		t.Fatalf("expected hint restored to %q, got %q", store.DefaultHint, store.GetApp().Hint)
	}

	// 3. Reactivate with '!' and cancel with esc.
	typeText(inst, "!")
	if store.GetApp().Hint != BashModeHint {
		t.Fatalf("expected hint %q after reactivating bash mode, got %q", BashModeHint, store.GetApp().Hint)
	}
	pressKey(inst, key.KeyEscape, 0)
	out = surfaceText(b)
	if !strings.Contains(out, ">") {
		t.Fatalf("expected prompt restored to '>' after esc, got:\n%s", out)
	}
	if store.GetApp().Hint != store.DefaultHint {
		t.Fatalf("expected hint restored to %q after esc, got %q", store.DefaultHint, store.GetApp().Hint)
	}
}

func TestComposerBashModeSubmitAndHistory(t *testing.T) {
	xdg.SetTestEnv(t.TempDir(), "tasksmith")
	defer xdg.ClearCache()

	store.ClearSlot()
	store.ResetApp()
	defer store.ClearSlot()
	defer store.ResetApp()

	var submitted string
	inst, b := createComposerInstance(t, ComposerProps{
		OnSubmit: func(text string) {
			submitted = text
		},
	})

	// Activate bash mode and type command
	typeText(inst, "!")
	typeText(inst, "git status")

	out := surfaceText(b)
	if !strings.Contains(out, "! git status") {
		t.Fatalf("expected '! git status' on surface in bash mode, got:\n%s", out)
	}

	// Submit via enter
	pressKey(inst, key.KeyEnter, 0)

	if submitted != "!git status" {
		t.Fatalf("expected submitted '!git status', got %q", submitted)
	}

	// Exits bash mode and restores prompt and default hint
	out = surfaceText(b)
	if strings.Contains(out, "git status") {
		t.Fatalf("expected input cleared after submit, got:\n%s", out)
	}
	if !strings.Contains(out, ">") {
		t.Fatalf("expected prompt restored to '>', got:\n%s", out)
	}
	if store.GetApp().Hint != store.DefaultHint {
		t.Fatalf("expected hint restored to %q, got %q", store.DefaultHint, store.GetApp().Hint)
	}

	// Up arrow should recall "!git status" and put composer back in bash mode
	pressKey(inst, key.KeyUp, 0)
	out = surfaceText(b)
	if !strings.Contains(out, "! git status") {
		t.Fatalf("expected recalled '! git status' on Up, got:\n%s", out)
	}
	if store.GetApp().Hint != BashModeHint {
		t.Fatalf("expected hint %q when recalling bash command, got %q", BashModeHint, store.GetApp().Hint)
	}
}

// waitForAutocomplete drives frames until cond holds or the deadline elapses.
func waitForAutocomplete(t *testing.T, inst *trail.Instance, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		inst.Frame()
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", msg)
}

func newAutocompleteComposer(t *testing.T, b *mock.Backend) *trail.Instance {
	t.Helper()
	engine := autocomplete.New()
	if err := engine.Register(autocomplete.Source{
		Name:    "cmds",
		Trigger: "@",
		Complete: autocomplete.Static(
			autocomplete.Candidate{Value: "alpha"},
			autocomplete.Candidate{Value: "alpine"},
			autocomplete.Candidate{Value: "apex"},
		),
	}); err != nil {
		t.Fatal(err)
	}
	qc := wind.NewClient()
	app := trail.SimpleFC("AutocompleteComposerTest", func() trail.Node {
		return wind.Provider(wind.ProviderProps{Client: qc}, Composer(ComposerProps{
			Autocomplete: engine,
		}))
	})
	inst, err := trail.New(app(), trail.WithBackend(b))
	if err != nil {
		t.Fatalf("trail.New: %v", err)
	}
	t.Cleanup(inst.Unmount)
	inst.Frame()
	return inst
}

func TestComposerAutocompleteShowsKeyboardHintAndCtrlN(t *testing.T) {
	store.ClearSlot()
	store.ResetApp()
	defer store.ClearSlot()
	defer store.ResetApp()

	b := mock.New(80, 24)
	inst := newAutocompleteComposer(t, b)

	typeText(inst, "@")
	waitForAutocomplete(t, inst, func() bool {
		out := surfaceText(b)
		return strings.Contains(out, "alpha") && strings.Contains(out, "Keyboard:")
	}, "autocomplete menu with keyboard hint")

	// The keyboard hint is rendered in the autocomplete and advertises Ctrl+P/Ctrl+N.
	if out := surfaceText(b); !strings.Contains(out, "Ctrl+P/Ctrl+N") {
		t.Fatalf("expected Ctrl+P/Ctrl+N in the keyboard hint, got:\n%s", out)
	}

	// Ctrl+N navigates down to the second candidate; Tab accepts it.
	pressKey(inst, 'n', key.ModCtrl)
	pressKey(inst, key.KeyTab, 0)
	if out := surfaceText(b); !strings.Contains(out, "@alpine") {
		t.Fatalf("expected Ctrl+N to move to 'alpine', got:\n%s", out)
	}
}

func TestComposerAutocompleteCtrlPNavigatesUp(t *testing.T) {
	store.ClearSlot()
	store.ResetApp()
	defer store.ClearSlot()
	defer store.ResetApp()

	b := mock.New(80, 24)
	inst := newAutocompleteComposer(t, b)

	typeText(inst, "@")
	waitForAutocomplete(t, inst, func() bool {
		out := surfaceText(b)
		return strings.Contains(out, "alpha") && strings.Contains(out, "Keyboard:")
	}, "autocomplete menu with keyboard hint")

	// Navigate down twice to "apex", then Ctrl+P back up to "alpine"; Tab accepts it.
	pressKey(inst, 'n', key.ModCtrl)
	pressKey(inst, 'n', key.ModCtrl)
	pressKey(inst, 'p', key.ModCtrl)
	pressKey(inst, key.KeyTab, 0)
	if out := surfaceText(b); !strings.Contains(out, "@alpine") {
		t.Fatalf("expected Ctrl+P to move back to 'alpine', got:\n%s", out)
	}
}
