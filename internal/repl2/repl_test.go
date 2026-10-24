package repl2

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/masterkeysrd/kite/backend"
	"github.com/masterkeysrd/kite/backend/mock"
	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/tasksmith/client"
	"github.com/masterkeysrd/tasksmith/internal/repl2/store"
)

func surfaceText(b *mock.Backend) string {
	frame := b.LastFrame().Surface
	if frame == nil {
		return ""
	}
	var sb strings.Builder
	for y := 0; y < frame.Height; y++ {
		var line strings.Builder
		for x := 0; x < frame.Width; x++ {
			c := frame.CellAt(x, y)
			if c.Content != "" {
				line.WriteString(c.Content)
			} else {
				line.WriteByte(' ')
			}
		}
		trimmed := strings.TrimRight(line.String(), " ")
		if trimmed != "" {
			sb.WriteString(trimmed)
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func waitFor(t *testing.T, condition func() bool, timeout time.Duration, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", msg)
}

func TestOptionsValidate(t *testing.T) {
	c, err := client.New("/tmp/mock.sock")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	tests := []struct {
		name    string
		opts    Options
		wantErr bool
	}{
		{
			name:    "nil client",
			opts:    Options{Client: nil, Workspace: "/tmp/ws"},
			wantErr: true,
		},
		{
			name:    "empty workspace",
			opts:    Options{Client: c, Workspace: ""},
			wantErr: true,
		},
		{
			name:    "whitespace workspace",
			opts:    Options{Client: c, Workspace: "   "},
			wantErr: true,
		},
		{
			name:    "valid options",
			opts:    Options{Client: c, Workspace: "/tmp/ws"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.opts.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && !errors.Is(err, ErrInvalidOptions) {
				t.Fatalf("expected error wrapping ErrInvalidOptions, got: %v", err)
			}
		})
	}

	// Verify Run also returns validation error
	if err := Run(context.Background(), Options{}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("Run() error = %v, want ErrInvalidOptions", err)
	}
}

func TestRunCtrlCDoublePressExits(t *testing.T) {
	b := mock.New(80, 24)
	c, err := client.New("/tmp/mock.sock")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := t.Context()

	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(ctx, Options{
			Client:    c,
			Workspace: "/tmp/ws",
			Backend:   b,
		})
	}()

	waitFor(t, func() bool {
		return b.LastFrame().Surface != nil
	}, 2*time.Second, "initial frame render")

	if store.GetApp().CtrlCPending {
		t.Fatal("expected CtrlCPending to be false initially")
	}

	// Send first Ctrl+C
	b.PostEvent(&backend.RawKeyEvent{
		Key: key.Key{Code: 'c', Mod: key.ModCtrl},
	})

	// Verify store.GetApp().CtrlCPending becomes true and screen contains "Press Ctrl+C again to exit"
	waitFor(t, func() bool {
		return store.GetApp().CtrlCPending && strings.Contains(surfaceText(b), "Press Ctrl+C again to exit")
	}, 2*time.Second, "CtrlCPending and screen update on first Ctrl+C")

	// Verify Run is still running
	select {
	case err := <-errCh:
		t.Fatalf("Run exited prematurely after single Ctrl+C: %v", err)
	default:
	}

	// Send second Ctrl+C
	b.PostEvent(&backend.RawKeyEvent{
		Key: key.Key{Code: 'c', Mod: key.ModCtrl},
	})

	// Verify Run exits cleanly with nil error
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("expected nil error on double Ctrl+C exit, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Run to exit after second Ctrl+C")
	}
}

func TestRunCtrlCDisarmedByOtherKey(t *testing.T) {
	b := mock.New(80, 24)
	c, err := client.New("/tmp/mock.sock")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := t.Context()

	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(ctx, Options{
			Client:    c,
			Workspace: "/tmp/ws",
			Backend:   b,
		})
	}()

	waitFor(t, func() bool {
		return b.LastFrame().Surface != nil
	}, 2*time.Second, "initial frame render")

	// 1. Send first Ctrl+C
	b.PostEvent(&backend.RawKeyEvent{
		Key: key.Key{Code: key.CtrlC},
	})

	waitFor(t, func() bool {
		return store.GetApp().CtrlCPending
	}, 2*time.Second, "CtrlCPending to become true")

	// 2. Send 'a'
	b.PostEvent(&backend.RawKeyEvent{
		Key: key.Key{Code: 'a', Text: "a"},
	})

	waitFor(t, func() bool {
		return !store.GetApp().CtrlCPending
	}, 2*time.Second, "CtrlCPending to be disarmed by other key")

	if strings.Contains(surfaceText(b), "Press Ctrl+C again to exit") {
		t.Fatalf("screen should no longer contain exit warning, got:\n%s", surfaceText(b))
	}

	// 3. Send third Ctrl+C (only re-arms)
	b.PostEvent(&backend.RawKeyEvent{
		Key: key.Key{Code: key.CtrlC},
	})

	waitFor(t, func() bool {
		return store.GetApp().CtrlCPending && strings.Contains(surfaceText(b), "Press Ctrl+C again to exit")
	}, 2*time.Second, "CtrlCPending to re-arm")

	// Verify Run is still running
	select {
	case err := <-errCh:
		t.Fatalf("Run exited prematurely after third Ctrl+C: %v", err)
	default:
	}

	// 4. Send fourth Ctrl+C (exits)
	b.PostEvent(&backend.RawKeyEvent{
		Key: key.Key{Code: key.CtrlC},
	})

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("expected nil error on fourth Ctrl+C exit, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Run to exit after fourth Ctrl+C")
	}
}

func TestRunContextCanceled(t *testing.T) {
	b := mock.New(80, 24)
	c, err := client.New("/tmp/mock.sock")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(ctx, Options{
			Client:    c,
			Workspace: "/tmp/ws",
			Backend:   b,
		})
	}()

	waitFor(t, func() bool {
		return b.LastFrame().Surface != nil
	}, 2*time.Second, "initial frame render")

	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("expected nil error on context cancel, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Run to exit after context cancel")
	}
}

func TestRunWithCWD(t *testing.T) {
	b := mock.New(80, 24)
	c, err := client.New("/tmp/mock.sock")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	if c.CWD() != "" {
		t.Fatalf("expected client CWD to be empty, got: %q", c.CWD())
	}

	ctx, cancel := context.WithCancel(context.Background())
	opts := Options{
		Client:    c,
		Workspace: "/tmp/custom-ws",
		Backend:   b,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(ctx, opts)
	}()

	waitFor(t, func() bool {
		return b.LastFrame().Surface != nil
	}, 2*time.Second, "initial frame render")

	cancel()
	<-errCh
}

func TestRunStartupHeader(t *testing.T) {
	b := mock.New(80, 24)
	c, err := client.New("/tmp/mock.sock")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	opts := Options{
		Client:    c,
		Workspace: "/tmp/ws",
		Version:   "0.2.0",
		Model:     "test-model",
		Backend:   b,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(ctx, opts)
	}()

	waitFor(t, func() bool {
		for _, buf := range b.InsertedAbove {
			for y := 0; y < buf.Height; y++ {
				var line strings.Builder
				for x := 0; x < buf.Width; x++ {
					c := buf.CellAt(x, y)
					if c.Content != "" {
						line.WriteString(c.Content)
					}
				}
				if strings.Contains(line.String(), "TaskSmith v0.2.0") {
					return true
				}
			}
		}
		return false
	}, 2*time.Second, "startup banner in inserted above scrollback")

	waitFor(t, func() bool {
		return strings.Contains(surfaceText(b), "shift+tab cycle mode")
	}, 2*time.Second, "default hint on status line")
}
