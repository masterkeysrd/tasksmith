package command

import (
	"testing"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/repl2/store"
)

func TestRegistryBuiltinsAndLookup(t *testing.T) {
	r := NewRegistry()

	cmds := []string{"help", "mode", "model", "agent", "effort", "theme", "new", "sessions", "changes", "diff", "revert", "clear", "exit"}
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

		store.AppendHeader(store.HeaderData{Version: "v1.0.0"})
		store.AppendNotice("hello")
		epochBefore := store.GetTranscript().Epoch

		if err := r.Execute("/clear"); err != nil {
			t.Fatalf("Execute /clear: %v", err)
		}
		epochAfter := store.GetTranscript().Epoch
		if epochAfter != epochBefore+1 {
			t.Errorf("expected epoch to increment on /clear when dirty, was %d now %d", epochBefore, epochAfter)
		}
		if len(*store.GetTranscript().Items) != 1 {
			t.Errorf("expected 1 item (header) remaining after /clear")
		}

		// /clear on clean transcript should NOT increment epoch
		if err := r.Execute("/clear"); err != nil {
			t.Fatalf("Execute /clear on clean transcript: %v", err)
		}
		epochClean := store.GetTranscript().Epoch
		if epochClean != epochAfter {
			t.Errorf("expected epoch NOT to increment on clean transcript, was %d now %d", epochAfter, epochClean)
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

	t.Run("new session", func(t *testing.T) {
		store.ResetSession()
		store.ClearTranscript()
		defer store.ResetSession()
		defer store.ClearTranscript()

		// Setup clean transcript with header matching the model
		store.AppendHeader(store.HeaderData{
			Version:  "v1.0.0",
			Model:    "test-model-1",
			Thinking: "auto",
		})
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

		// 2. /new on dirty transcript should increment epoch
		store.AppendNotice("dirty notice")
		if err := r.Execute("/new test-model-1"); err != nil {
			t.Fatalf("Execute /new on dirty transcript: %v", err)
		}
		if store.GetTranscript().Epoch != epoch0+1 {
			t.Errorf("expected epoch to increment on dirty transcript, was %d now %d", epoch0, store.GetTranscript().Epoch)
		}

		// 3. /new with different model should increment epoch
		epoch1 := store.GetTranscript().Epoch
		if err := r.Execute("/new diff-model"); err != nil {
			t.Fatalf("Execute /new diff-model: %v", err)
		}
		if store.GetTranscript().Epoch != epoch1+1 {
			t.Errorf("expected epoch to increment on model change, was %d now %d", epoch1, store.GetTranscript().Epoch)
		}
		if store.GetSession().Config.Model != "diff-model" {
			t.Errorf("expected model diff-model, got %q", store.GetSession().Config.Model)
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
