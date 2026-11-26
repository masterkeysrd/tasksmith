package command

import (
	"context"
	"strings"
	"testing"

	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/components"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/tui/testutils"
	"github.com/masterkeysrd/tasksmith/internal/ui/autocomplete"
)

func TestWorkspaceCommandRegistration(t *testing.T) {
	r := NewRegistry()

	if cmd, ok := r.Lookup("workspace"); !ok || cmd.Name != "workspace" {
		t.Fatalf("expected /workspace to be registered, ok=%v cmd=%+v", ok, cmd)
	}
	if cmd, ok := r.Lookup("/workspace"); !ok || cmd.Name != "workspace" {
		t.Fatalf("expected /workspace with slash prefix to resolve, ok=%v", ok)
	}
	if cmd, ok := r.Lookup("ws"); !ok || cmd.Name != "workspace" {
		t.Fatalf("expected alias /ws to resolve to workspace, ok=%v cmd=%+v", ok, cmd)
	}
}

func TestWorkspaceUnknownSubcommand(t *testing.T) {
	r := NewRegistry()

	for _, input := range []string{"/workspace bogus", "/ws --unknown", "/workspace reload extra", "/ws reload -y extra", "/ws -y --force"} {
		if err := r.Execute(input); err == nil || !strings.Contains(err.Error(), "usage: "+workspaceUsage) {
			t.Errorf("%q: expected usage error, got %v", input, err)
		}
	}
}

func TestWorkspaceReloadWithoutClient(t *testing.T) {
	r := NewRegistry()

	// No active client: the reload must fail gracefully and report an error
	// to the transcript instead of panicking.
	store.ResetSession()
	store.ClearTranscript()
	defer store.ResetSession()
	defer store.ClearTranscript()

	if err := r.Execute("/workspace reload"); err != nil {
		t.Fatalf("Execute /workspace reload: %v", err)
	}

	items := store.GetTranscript().Items
	if items == nil || len(*items) == 0 {
		t.Fatalf("expected an error transcript item after /workspace reload without a client")
	}
	last := (*items)[len(*items)-1]
	if last.Kind != store.TranscriptKindError {
		t.Fatalf("expected TranscriptKindError, got %v", last.Kind)
	}
	if d, ok := last.Data.(store.ErrorData); !ok || !strings.Contains(d.Message, "Client unavailable") {
		t.Fatalf("expected 'Client unavailable' error, got %+v", last.Data)
	}
}

func TestWorkspaceNoArgReloads(t *testing.T) {
	r := NewRegistry()

	// With no subcommand, /workspace defaults to reload and reports the same
	// client-unavailable error when no client is active.
	store.ResetSession()
	store.ClearTranscript()
	defer store.ResetSession()
	defer store.ClearTranscript()

	if err := r.Execute("/workspace"); err != nil {
		t.Fatalf("Execute /workspace: %v", err)
	}

	items := store.GetTranscript().Items
	if items == nil || len(*items) == 0 {
		t.Fatalf("expected an error transcript item after /workspace without a client")
	}
	last := (*items)[len(*items)-1]
	if last.Kind != store.TranscriptKindError {
		t.Fatalf("expected TranscriptKindError, got %v", last.Kind)
	}
}

func TestWorkspaceCompleter(t *testing.T) {
	candidates, err := completeWorkspace(context.Background(), autocomplete.Request{})
	if err != nil {
		t.Fatalf("completeWorkspace: %v", err)
	}
	seen := map[string]bool{}
	for _, c := range candidates {
		seen[c.Value] = true
	}
	for _, want := range []string{"reload", "refresh", "--force", "-y"} {
		if !seen[want] {
			t.Errorf("missing %q from %+v", want, candidates)
		}
	}
}

func assertWorkspaceReloaded(t *testing.T, fixture *mcpFixture) {
	t.Helper()
	if !waitFor(t, func() bool { return strings.Contains(transcriptText(), "Workspace reloaded successfully.") }) {
		t.Fatalf("reload did not finish: %s", transcriptText())
	}
	if n := len(fixture.pathCalls("/workspace/reload")); n != 1 || store.GetSlot().Active != nil {
		t.Fatalf("expected one reload and no slot; calls=%d slot=%+v", n, store.GetSlot().Active)
	}
}

func TestWorkspaceImmediateReload(t *testing.T) {
	for _, cmd := range []string{"/workspace", "/ws refresh", "/ws --force", "/ws -y", "/workspace --force", "/workspace -y", "/workspace reload --force", "/workspace reload -y", "/ws reload -y", "/ws reload --force"} {
		t.Run(cmd, func(t *testing.T) {
			fixture := newMCPFixture(t, nil)
			switch cmd {
			case "/workspace":
				store.SetSession(nil)
			case "/ws refresh":
				store.SetSession(&api.Session{ID: "empty", Context: api.ContextState{UsedTokens: 100, Breakdown: api.ContextBreakdown{SystemPrompt: 100}}})
			default:
				store.AppendMessage(store.Message{ID: "first"})
			}
			store.AppendNotice("Local notice")
			if err := NewRegistry().Execute(cmd); err != nil {
				t.Fatal(err)
			}
			assertWorkspaceReloaded(t, fixture)
		})
	}
}

func TestWorkspaceReloadConfirmation(t *testing.T) {
	for _, action := range []string{"reload", "cancel", "escape"} {
		t.Run(action, func(t *testing.T) {
			fixture := newMCPFixture(t, nil)
			if action == "escape" {
				store.SetSession(&api.Session{ID: "resumed", Context: api.ContextState{Breakdown: api.ContextBreakdown{UserMessages: 10}}})
			} else {
				store.AppendMessage(store.Message{ID: "first"})
			}
			if err := NewRegistry().Execute("/ws"); err != nil {
				t.Fatal(err)
			}
			app := testutils.NewApp(t, components.Slot(), 80, 24)
			text := strings.Join(strings.Fields(app.Text()), " ")
			for _, want := range []string{"AGENTS.md", "system prompt or tool definitions", "invalidate the provider prompt cache", "uncached rate on the next turn", "Reload workspace", "Cancel", "↑/↓", "enter", "esc"} {
				if !strings.Contains(text, want) {
					t.Errorf("missing %q from prompt:\n%s", want, text)
				}
			}
			if len(fixture.pathCalls("/workspace/reload")) != 0 || strings.Contains(transcriptText(), "Reloading workspace") {
				t.Fatal("reload started before confirmation")
			}
			switch action {
			case "reload":
				app.Press(key.Key{Code: key.KeyDown})
				app.Press(key.Key{Code: key.KeyEnter})
				assertWorkspaceReloaded(t, fixture)
				return
			case "cancel":
				app.Press(key.Key{Code: key.KeyEnter})
			case "escape":
				app.Press(key.Key{Code: key.KeyEscape})
			}
			if store.GetSlot().Active != nil || len(fixture.pathCalls("/workspace/reload")) != 0 || !strings.Contains(transcriptText(), "Workspace reload canceled.") {
				t.Fatalf("expected dismissal without reload: %s", transcriptText())
			}
		})
	}

	t.Run("active first turn before transcript or context populates", func(t *testing.T) {
		fixture := newMCPFixture(t, nil)
		t.Cleanup(store.ResetStream)
		store.SetSession(&api.Session{ID: "first-turn", Status: api.StatusThinking})
		if err := NewRegistry().Execute("/ws"); err != nil {
			t.Fatal(err)
		}
		if store.GetSlot().Active == nil {
			t.Fatal("expected confirmation slot during active first turn")
		}
		if len(fixture.pathCalls("/workspace/reload")) != 0 {
			t.Fatal("reload started before confirmation during active first turn")
		}
	})
}
