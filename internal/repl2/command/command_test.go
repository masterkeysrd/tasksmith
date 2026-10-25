package command

import (
	"context"
	"testing"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
	"github.com/masterkeysrd/tasksmith/internal/repl2/store"
)

func TestRegistryBuiltinsAndLookup(t *testing.T) {
	r := NewRegistry()

	cmds := []string{"help", "mode", "model", "agent", "effort", "theme", "new", "sessions", "changes", "diff", "revert", "clear", "exit", "permissions"}
	for _, name := range cmds {
		cmd, ok := r.Lookup(name)
		if !ok {
			t.Fatalf("expected command /%s to be registered", name)
		}
		if cmd.Name != name {
			t.Errorf("expected cmd.Name=%q, got %q", name, cmd.Name)
		}
	}

	// Test aliases
	aliases := map[string]string{
		"h":       "help",
		"?":       "help",
		"session": "sessions",
		"ls":      "sessions",
		"quit":    "exit",
		"q":       "exit",
		"perm":    "permissions",
		"perms":   "permissions",
	}
	for alias, expectedName := range aliases {
		cmd, ok := r.Lookup(alias)
		if !ok {
			t.Errorf("expected alias /%s to be registered", alias)
			continue
		}
		if cmd.Name != expectedName {
			t.Errorf("alias %q resolved to %q, expected %q", alias, cmd.Name, expectedName)
		}
	}
}

func TestCommandExecute(t *testing.T) {
	r := NewRegistry()

	t.Run("exit", func(t *testing.T) {
		store.ResetApp()
		defer store.ResetApp()

		if err := r.Execute("/exit"); err != nil {
			t.Fatalf("Execute /exit: %v", err)
		}
		if !store.GetApp().ExitRequested {
			t.Errorf("expected ExitRequested=true")
		}
	})

	t.Run("clear", func(t *testing.T) {
		store.ClearTranscript()
		defer store.ClearTranscript()

		store.SetHeaderMeta("v1.0.0", "/ws")

		// 1. /clear in draft mode clears draft items without incrementing epoch
		store.AppendNotice("hello")
		epochBefore := store.GetTranscript().Epoch

		if err := r.Execute("/clear"); err != nil {
			t.Fatalf("Execute /clear: %v", err)
		}
		epochAfter := store.GetTranscript().Epoch
		if epochAfter != epochBefore {
			t.Errorf("expected epoch NOT to increment on /clear in draft mode, was %d now %d", epochBefore, epochAfter)
		}
		if store.GetTranscript().Items != nil {
			t.Errorf("expected nil items (live header) remaining after /clear in draft, got %v", store.GetTranscript().Items)
		}

		// 2. /clear in frozen mode increments epoch and resets items to [Header]
		store.FreezeTranscript()
		store.AppendNotice("frozen notice")
		epochFrozenBefore := store.GetTranscript().Epoch

		if err := r.Execute("/clear"); err != nil {
			t.Fatalf("Execute /clear in frozen mode: %v", err)
		}
		epochFrozenAfter := store.GetTranscript().Epoch
		if epochFrozenAfter != epochFrozenBefore+1 {
			t.Errorf("expected epoch to increment on /clear in frozen mode, was %d now %d", epochFrozenBefore, epochFrozenAfter)
		}
		if store.GetTranscript().Items == nil || len(*store.GetTranscript().Items) != 1 {
			t.Errorf("expected [Header] remaining after /clear in frozen mode, got %v", store.GetTranscript().Items)
		}

		// 3. /clear on clean frozen transcript should NOT increment epoch
		if err := r.Execute("/clear"); err != nil {
			t.Fatalf("Execute /clear on clean frozen transcript: %v", err)
		}
		epochClean := store.GetTranscript().Epoch
		if epochClean != epochFrozenAfter {
			t.Errorf("expected epoch NOT to increment on clean frozen transcript, was %d now %d", epochFrozenAfter, epochClean)
		}
	})

	t.Run("mode with arg", func(t *testing.T) {
		store.ResetSession()
		defer store.ResetSession()

		if err := r.Execute("/mode auto"); err != nil {
			t.Fatalf("Execute /mode auto: %v", err)
		}
		if store.GetSession().Config.Mode != api.ModeAuto {
			t.Errorf("expected mode to be auto, got %q", store.GetSession().Config.Mode)
		}

		// Invalid mode returns error
		if err := r.Execute("/mode invalid_mode"); err == nil {
			t.Errorf("expected error for invalid mode")
		}
	})

	t.Run("help enqueues slot", func(t *testing.T) {
		store.ClearSlot()
		defer store.ClearSlot()

		if err := r.Execute("/help"); err != nil {
			t.Fatalf("Execute /help: %v", err)
		}
		if store.GetSlot().Active == nil {
			t.Fatalf("expected slot to be enqueued for /help")
		}
	})

	t.Run("permissions", func(t *testing.T) {
		store.ClearSlot()
		defer store.ClearSlot()
		defer store.ResetSession()

		if err := r.Execute("/permissions"); err != nil {
			t.Fatalf("Execute /permissions without client: %v", err)
		}
		if store.GetSlot().Active != nil {
			t.Errorf("expected no active slot when client is nil")
		}

		cli, _ := client.New("/tmp/dummy.sock")
		store.InitSession(context.Background(), cli, store.InitSessionOptions{})

		if err := r.Execute("/permissions"); err != nil {
			t.Fatalf("Execute /permissions: %v", err)
		}
		if store.GetSlot().Active == nil || store.GetSlot().Active.Kind != store.SlotKindPicker {
			t.Fatalf("expected SlotKindPicker active slot after /permissions, got %+v", store.GetSlot().Active)
		}
		store.ClearSlot()

		if err := r.Execute("/perm"); err != nil {
			t.Fatalf("Execute /perm: %v", err)
		}
		if store.GetSlot().Active == nil || store.GetSlot().Active.Kind != store.SlotKindPicker {
			t.Fatalf("expected SlotKindPicker active slot after /perm, got %+v", store.GetSlot().Active)
		}
		store.ClearSlot()

		if err := r.Execute("/perms"); err != nil {
			t.Fatalf("Execute /perms: %v", err)
		}
		if store.GetSlot().Active == nil || store.GetSlot().Active.Kind != store.SlotKindPicker {
			t.Fatalf("expected SlotKindPicker active slot after /perms, got %+v", store.GetSlot().Active)
		}
	})

	t.Run("new session", func(t *testing.T) {
		store.ResetSession()
		store.ClearTranscript()
		defer store.ResetSession()
		defer store.ClearTranscript()

		store.SetHeaderMeta("v1.0.0", "/ws")
		store.SetModelInfo("test-model-1", "auto")

		epoch0 := store.GetTranscript().Epoch

		// 1. /new on clean transcript with same model should NOT increment epoch
		if err := r.Execute("/new test-model-1"); err != nil {
			t.Fatalf("Execute /new: %v", err)
		}
		if store.GetTranscript().Epoch != epoch0 {
			t.Errorf("expected epoch to remain %d on clean transcript with same model, got %d", epoch0, store.GetTranscript().Epoch)
		}
		if store.GetSession().Config.Model != "test-model-1" {
			t.Errorf("expected model test-model-1, got %q", store.GetSession().Config.Model)
		}

		// 2. /new on draft transcript clears items without incrementing epoch
		store.AppendNotice("dirty notice")
		if err := r.Execute("/new test-model-1"); err != nil {
			t.Fatalf("Execute /new on draft transcript: %v", err)
		}
		if store.GetTranscript().Epoch != epoch0 {
			t.Errorf("expected epoch to remain %d on draft transcript, got %d", epoch0, store.GetTranscript().Epoch)
		}
		if store.GetTranscript().Items != nil {
			t.Errorf("expected nil items after /new in draft, got %v", store.GetTranscript().Items)
		}

		// 3. /new on frozen transcript increments epoch and transitions to draft
		store.FreezeTranscript()
		store.AppendNotice("frozen notice")
		epochFrozenBefore := store.GetTranscript().Epoch
		if err := r.Execute("/new test-model-1"); err != nil {
			t.Fatalf("Execute /new on frozen transcript: %v", err)
		}
		if store.GetTranscript().Epoch != epochFrozenBefore+1 {
			t.Errorf("expected epoch to increment on frozen transcript, was %d now %d", epochFrozenBefore, store.GetTranscript().Epoch)
		}
		if store.GetTranscript().Frozen {
			t.Errorf("expected Frozen=false after /new")
		}
		if store.GetTranscript().Items != nil {
			t.Errorf("expected nil items after /new, got %v", store.GetTranscript().Items)
		}

		// 4. /new with different model updates live header without incrementing epoch
		epoch1 := store.GetTranscript().Epoch
		if err := r.Execute("/new diff-model"); err != nil {
			t.Fatalf("Execute /new diff-model: %v", err)
		}
		if store.GetTranscript().Epoch != epoch1 {
			t.Errorf("expected epoch to remain %d on clean transcript, got %d", epoch1, store.GetTranscript().Epoch)
		}
		if store.GetSession().Config.Model != "diff-model" {
			t.Errorf("expected model diff-model, got %q", store.GetSession().Config.Model)
		}
		if store.CurrentHeaderData().Model != "diff-model" {
			t.Errorf("expected live header model diff-model, got %q", store.CurrentHeaderData().Model)
		}
	})
}

func TestSources(t *testing.T) {
	r := NewRegistry()
	sources := r.Sources()
	if len(sources) == 0 {
		t.Fatalf("expected sources to be non-empty")
	}
	// First source should be root "/"
	if sources[0].Trigger != "/" {
		t.Errorf("expected first source trigger '/', got %q", sources[0].Trigger)
	}

	// Should have sub-sources for mode, model, agent, effort, theme
	foundMode := false
	for _, s := range sources {
		if s.Trigger == "/mode " {
			foundMode = true
			break
		}
	}
	if !foundMode {
		t.Errorf("expected /mode sub-source")
	}
}
