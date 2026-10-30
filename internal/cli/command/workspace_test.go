package command

import (
	"context"
	"strings"
	"testing"

	"github.com/masterkeysrd/tasksmith/internal/cli/store"
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

	err := r.Execute("/workspace bogus")
	if err == nil || !strings.Contains(err.Error(), "unknown workspace subcommand") {
		t.Fatalf("expected unknown subcommand error, got: %v", err)
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
	if !seen["reload"] {
		t.Errorf("expected 'reload' candidate, got %+v", candidates)
	}
}
