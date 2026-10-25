package components

import (
	"strings"
	"testing"

	"github.com/masterkeysrd/kite/backend"
	"github.com/masterkeysrd/kite/backend/mock"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/repl2/store"
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
