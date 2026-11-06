package command

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/masterkeysrd/kite/backend"
	"github.com/masterkeysrd/kite/backend/mock"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/tasksmith/client"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/tui/testutils"
	"github.com/masterkeysrd/tasksmith/internal/ui/autocomplete"
)

type mockDaemonRoundTripper struct {
	handler func(req *http.Request) (*http.Response, error)
}

func (m *mockDaemonRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.handler(req)
}

func TestDaemonCommandRegistration(t *testing.T) {
	r := NewRegistry()

	cmd, ok := r.Lookup("daemon")
	if !ok || cmd.Name != "daemon" {
		t.Fatalf("expected /daemon to be registered, ok=%v cmd=%+v", ok, cmd)
	}
	if cmd.Usage != "/daemon [status|restart|start|stop]" {
		t.Errorf("unexpected usage: %q", cmd.Usage)
	}
	if cmd.Description == "" {
		t.Error("expected non-empty description")
	}
	if cmd.Complete == nil {
		t.Error("expected non-nil Complete")
	}
}

func TestDaemonCompleter(t *testing.T) {
	candidates, err := completeDaemon(context.Background(), autocomplete.Request{})
	if err != nil {
		t.Fatalf("completeDaemon error: %v", err)
	}
	expected := []string{"status", "restart", "start", "stop"}
	found := make(map[string]bool)
	for _, c := range candidates {
		found[c.Value] = true
	}
	for _, exp := range expected {
		if !found[exp] {
			t.Errorf("expected autocomplete candidate %q, got %+v", exp, candidates)
		}
	}
}

func TestDaemonSubcommands(t *testing.T) {
	store.ClearSlot()
	store.ClearTranscript()
	store.ResetApp()
	store.ResetSession()
	defer store.ClearSlot()
	defer store.ClearTranscript()
	defer store.ResetApp()
	defer store.ResetSession()

	r := NewRegistry()

	// 1. Unknown subcommand
	err := r.Execute("/daemon bogus")
	if err == nil || !strings.Contains(err.Error(), `unknown daemon subcommand "bogus"`) {
		t.Fatalf("expected unknown daemon subcommand error, got %v", err)
	}

	// 2. /daemon (status) enqueues slot
	store.ClearSlot()
	if err := r.Execute("/daemon"); err != nil {
		t.Fatalf("Execute /daemon: %v", err)
	}
	if store.GetSlot().Active == nil {
		t.Fatal("expected /daemon to open slot")
	}

	// 3. /daemon status enqueues slot
	store.ClearSlot()
	if err := r.Execute("/daemon status"); err != nil {
		t.Fatalf("Execute /daemon status: %v", err)
	}
	if store.GetSlot().Active == nil {
		t.Fatal("expected /daemon status to open slot")
	}

	// 4. /daemon restart
	store.ClearTranscript()
	if err := r.Execute("/daemon restart"); err != nil {
		t.Fatalf("Execute /daemon restart: %v", err)
	}
	items := store.GetTranscript().Items
	if items == nil || len(*items) == 0 {
		t.Fatal("expected transcript item after /daemon restart")
	}

	// 5. /daemon start
	store.ClearTranscript()
	if err := r.Execute("/daemon start"); err != nil {
		t.Fatalf("Execute /daemon start: %v", err)
	}
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		items = store.GetTranscript().Items
		if items != nil && len(*items) > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if items == nil || len(*items) == 0 {
		t.Fatal("expected transcript item after /daemon start")
	}

	// 6. /daemon stop
	store.ClearTranscript()
	if err := r.Execute("/daemon stop"); err != nil {
		t.Fatalf("Execute /daemon stop: %v", err)
	}
	deadline = time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		items = store.GetTranscript().Items
		if items != nil && len(*items) > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if items == nil || len(*items) == 0 {
		t.Fatal("expected transcript item after /daemon stop")
	}
}

func TestDaemonBrowserRenderingOfflineAndRestarting(t *testing.T) {
	store.ResetApp()
	store.ClearSlot()
	defer store.ResetApp()
	defer store.ClearSlot()

	// 1. Disconnected state
	store.SetDaemonStatus(store.DaemonDisconnected)
	out := renderTestNode(t, DaemonBrowser("slot-1"))
	if !strings.Contains(out, "Brain Daemon") {
		t.Fatalf("expected Brain Daemon title, got:\n%s", out)
	}
	if !strings.Contains(out, "○ Offline") {
		t.Fatalf("expected ○ Offline status, got:\n%s", out)
	}
	if !strings.Contains(out, "The background daemon is not running. Press s or r to start it.") {
		t.Fatalf("expected offline hint, got:\n%s", out)
	}
	if !strings.Contains(out, "s/r") || !strings.Contains(out, "Start") {
		t.Fatalf("expected s/r Start keybinding, got:\n%s", out)
	}

	// 2. Restarting state
	store.SetDaemonStatus(store.DaemonRestarting)
	out = renderTestNode(t, DaemonBrowser("slot-2"))
	if !strings.Contains(out, "↻ Restarting...") {
		t.Fatalf("expected ↻ Restarting... status, got:\n%s", out)
	}
	if !strings.Contains(out, "Stopping existing daemon and spawning a new background process...") {
		t.Fatalf("expected restarting hint, got:\n%s", out)
	}
	if !strings.Contains(out, "Close") {
		t.Fatalf("expected Close keybinding, got:\n%s", out)
	}
}

func TestDaemonBrowserRenderingRunning(t *testing.T) {
	store.ResetApp()
	store.ResetSession()
	store.ClearSlot()
	defer store.ResetApp()
	defer store.ResetSession()
	defer store.ClearSlot()

	rt := &mockDaemonRoundTripper{
		handler: func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == "/v1/health" {
				resp := `{"pid":4242,"started_at":"2026-01-01T00:00:00Z","workspaces":["/test-workspace"]}`
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(resp)),
					Header:     make(http.Header),
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{}`)),
				Header:     make(http.Header),
			}, nil
		},
	}

	c, err := client.New("/tmp/test-browser.sock", client.WithCWD("/test-workspace"), client.WithHTTPClient(&http.Client{Transport: rt}))
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}

	store.InitSession(context.Background(), c, store.InitSessionOptions{})
	store.SetDaemonStatus(store.DaemonConnected)

	out := renderTestNode(t, DaemonBrowser("slot-3"))
	if !strings.Contains(out, "Brain Daemon") {
		t.Fatalf("expected Brain Daemon title, got:\n%s", out)
	}
	if !strings.Contains(out, "● Running") {
		t.Fatalf("expected ● Running status, got:\n%s", out)
	}
	if !strings.Contains(out, "4242") {
		t.Fatalf("expected PID 4242, got:\n%s", out)
	}
	if !strings.Contains(out, "Uptime:") {
		t.Fatalf("expected Uptime row, got:\n%s", out)
	}
	if !strings.Contains(out, "1 active") || !strings.Contains(out, "/test-workspace") {
		t.Fatalf("expected workspace count and path, got:\n%s", out)
	}
	if !strings.Contains(out, "Restart") || !strings.Contains(out, "Stop") || !strings.Contains(out, "Refresh") {
		t.Fatalf("expected Running keybindings, got:\n%s", out)
	}
}

func TestDaemonBrowserKeybindingsClose(t *testing.T) {
	store.ResetApp()
	store.ClearSlot()
	defer store.ResetApp()
	defer store.ClearSlot()

	store.EnqueueSlot(store.SlotItem{
		ID:   "slot-close-test",
		Kind: store.SlotKindPicker,
		Data: DaemonBrowser,
	})

	if store.GetSlot().Active == nil {
		t.Fatal("expected slot active")
	}

	b := mock.New(100, 24)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go func() {
		_ = trail.Run(ctx, DaemonBrowser("slot-close-test"), trail.WithBackend(b))
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(testutils.SurfaceTextKeepBlanks(b), "Brain Daemon") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	b.PostEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyEscape}})

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if store.GetSlot().Active == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	if store.GetSlot().Active != nil {
		t.Fatal("expected slot removed after Escape")
	}
}
