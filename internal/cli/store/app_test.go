package store

import (
	"context"
	"testing"
	"time"

	"github.com/masterkeysrd/kite/backend/mock"
	"github.com/masterkeysrd/kite/extras/trail"
)

func TestStoreApp(t *testing.T) {
	ResetApp()
	defer ResetApp()

	if GetApp().CtrlCPending {
		t.Fatal("expected CtrlCPending to be false after ResetApp")
	}
	if GetApp().Hint != DefaultHint {
		t.Fatalf("expected Hint to be %q after ResetApp, got %q", DefaultHint, GetApp().Hint)
	}

	SetCtrlCPending(true)
	if !GetApp().CtrlCPending {
		t.Fatal("expected CtrlCPending to be true after SetCtrlCPending(true)")
	}

	SetCtrlCPending(false)
	if GetApp().CtrlCPending {
		t.Fatal("expected CtrlCPending to be false after SetCtrlCPending(false)")
	}

	SetHint("custom hint")
	if GetApp().Hint != "custom hint" {
		t.Fatalf("expected Hint to be %q, got %q", "custom hint", GetApp().Hint)
	}

	SetCtrlCPending(true)
	ResetApp()
	if GetApp().CtrlCPending {
		t.Fatal("expected CtrlCPending to be false after ResetApp")
	}
	if GetApp().Hint != DefaultHint {
		t.Fatalf("expected Hint to reset to %q, got %q", DefaultHint, GetApp().Hint)
	}
}

func TestUseHintMountUnmount(t *testing.T) {
	ResetApp()
	defer ResetApp()

	Child := trail.SimpleFC("Child", func() trail.Node {
		UseHint("child hint")
		return trail.Text("child")
	})

	Parent := trail.SimpleFC("Parent", func() trail.Node {
		ctrlC := UseApp(func(s AppState) bool {
			return s.CtrlCPending
		})
		if !ctrlC {
			return Child()
		}
		return trail.Text("no child")
	})

	b := mock.New(80, 24)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go func() {
		_ = trail.Run(ctx, Parent(), trail.WithBackend(b))
	}()

	// Wait for child to mount and set hint
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if GetApp().Hint == "child hint" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	if GetApp().Hint != "child hint" {
		t.Fatalf("expected hint to be %q while child mounted, got %q", "child hint", GetApp().Hint)
	}

	// Trigger unmount of child by changing CtrlCPending
	SetCtrlCPending(true)

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if GetApp().Hint == DefaultHint {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	if GetApp().Hint != DefaultHint {
		t.Fatalf("expected hint to be restored to %q on child unmount, got %q", DefaultHint, GetApp().Hint)
	}
}
