package components

import (
	"strings"
	"testing"

	"github.com/masterkeysrd/tasksmith/internal/cli/store"
)

func TestDaemonAlert(t *testing.T) {
	store.ResetApp()
	store.ClearSlot()
	defer store.ResetApp()
	defer store.ClearSlot()

	render := renderComponent(t, 100)

	// 1. DaemonConnected -> empty
	store.SetDaemonStatus(store.DaemonConnected)
	if got := render(DaemonAlert()); got != "" {
		t.Fatalf("expected empty alert when connected, got %q", got)
	}

	// 2. DaemonDisconnected -> shows offline message
	store.SetDaemonStatus(store.DaemonDisconnected)
	got := render(DaemonAlert())
	if !strings.Contains(got, "Brain Daemon is offline") {
		t.Fatalf("expected offline alert title, got %q", got)
	}
	if !strings.Contains(got, "Waiting to reconnect automatically, or run /daemon restart") {
		t.Fatalf("expected offline alert help text, got %q", got)
	}

	// 3. DaemonRestarting -> shows restarting message
	store.SetDaemonStatus(store.DaemonRestarting)
	got = render(DaemonAlert())
	if !strings.Contains(got, "Restarting Brain Daemon...") {
		t.Fatalf("expected restarting alert title, got %q", got)
	}
	if !strings.Contains(got, "Spawning background process and syncing session state") {
		t.Fatalf("expected restarting alert help text, got %q", got)
	}

	// 4. Hidden when slot is active
	store.SetDaemonStatus(store.DaemonDisconnected)
	store.EnqueueSlot(store.SlotItem{
		ID:   "test-slot",
		Kind: store.SlotKindPicker,
	})
	if got := render(DaemonAlert()); got != "" {
		t.Fatalf("expected empty alert when slot is active, got %q", got)
	}
}
