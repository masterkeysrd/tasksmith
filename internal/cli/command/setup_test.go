package command

import (
	"testing"

	"github.com/masterkeysrd/tasksmith/internal/cli/store"
)

func TestSetupCommandRegistration(t *testing.T) {
	reg := NewRegistry()
	cmd, ok := reg.Lookup("setup")
	if !ok {
		t.Fatal("expected /setup command to be registered")
	}
	if cmd.Name != "setup" {
		t.Fatalf("expected command name setup, got %s", cmd.Name)
	}

	// Without client, reports error to store without panicking
	err := reg.Execute("/setup")
	if err != nil {
		t.Fatalf("unexpected error from Execute: %v", err)
	}

	items := store.GetTranscript().Items
	if items == nil || len(*items) == 0 {
		t.Fatal("expected transcript entry when client unavailable")
	}
}
