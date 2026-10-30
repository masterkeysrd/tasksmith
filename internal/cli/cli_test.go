package cli

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/masterkeysrd/kite/backend"
	"github.com/masterkeysrd/kite/backend/mock"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
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

func allText(b *mock.Backend) string {
	var sb strings.Builder
	for _, buf := range b.InsertedAbove {
		for y := 0; y < buf.Height; y++ {
			var line strings.Builder
			for x := 0; x < buf.Width; x++ {
				c := buf.CellAt(x, y)
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
	}
	sb.WriteString(surfaceText(b))
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

func TestBuiltinSkillAutocomplete(t *testing.T) {
	store.ResetApp()
	store.ResetSession()
	store.ClearSlot()
	t.Cleanup(func() {
		store.ResetApp()
		store.ResetSession()
		store.ClearSlot()
	})
	store.SetWorkspaceSkills([]api.Skill{{Name: "tasksmith-customizations", Namespace: "system", Description: "Built-in customization guidance"}})
	b := mock.New(100, 24)
	inst, err := trail.New(App(), trail.WithBackend(b))
	if err != nil {
		t.Fatal(err)
	}
	defer inst.Unmount()
	inst.Frame()
	for _, r := range "/" {
		inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: r, Text: string(r)}})
		inst.Frame()
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		inst.Frame()
		if strings.Contains(surfaceText(b), "/tasksmith-customizations") {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("built-in skill missing from autocomplete:\n%s", surfaceText(b))
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

	if active := ActiveSession(); active != nil {
		t.Errorf("expected nil ActiveSession() on draft REPL exit, got %+v", active)
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

	// The startup banner is initially rendered live on the surface (not in static scrollback)
	waitFor(t, func() bool {
		return strings.Contains(surfaceText(b), "TaskSmith v0.2.0") &&
			strings.Contains(surfaceText(b), "default · provider default") &&
			strings.Contains(surfaceText(b), "/tmp/ws")
	}, 2*time.Second, "live startup banner on screen surface")

	if len(b.InsertedAbove) != 0 {
		t.Fatalf("expected 0 scrollback buffers while transcript is clean, got %d", len(b.InsertedAbove))
	}

	waitFor(t, func() bool {
		return strings.Contains(surfaceText(b), "shift+tab cycle mode")
	}, 2*time.Second, "default hint on status line")
}

func TestRunShiftTabCyclesPermissionMode(t *testing.T) {
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
		Version:   "0.1.0",
		Model:     "test-model",
		Backend:   b,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(ctx, opts)
	}()

	// Wait for initial render where ModeDefault is hidden
	waitFor(t, func() bool {
		return b.LastFrame().Surface != nil && strings.Contains(surfaceText(b), ">")
	}, 2*time.Second, "initial composer render")

	if store.GetSession().Config.Mode != api.ModeDefault {
		t.Fatalf("expected ModeDefault initially, got %q", store.GetSession().Config.Mode)
	}

	// Press Shift+Tab -> cycles to accept_edits
	b.PostEvent(&backend.RawKeyEvent{
		Key: key.Key{Code: key.KeyTab, Mod: key.ModShift},
	})

	waitFor(t, func() bool {
		return store.GetSession().Config.Mode == api.ModeAcceptEdits &&
			strings.Contains(surfaceText(b), "accept_edits")
	}, 2*time.Second, "status line to show accept_edits mode")

	// Press Shift+Tab again -> cycles to plan
	b.PostEvent(&backend.RawKeyEvent{
		Key: key.Key{Code: key.KeyTab, Mod: key.ModShift},
	})

	waitFor(t, func() bool {
		return store.GetSession().Config.Mode == api.ModePlan &&
			strings.Contains(surfaceText(b), "plan")
	}, 2*time.Second, "status line to show plan mode")
}

func TestRunSubmitPromptAndActiveStream(t *testing.T) {
	b := mock.New(80, 24)
	c, st := startTestUnixDaemon(t)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	opts := Options{
		Client:    c,
		Workspace: "/workspace/project",
		Version:   "0.1.0",
		Model:     "claude-sonnet-4-6",
		Backend:   b,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(ctx, opts)
	}()

	waitFor(t, func() bool {
		return b.LastFrame().Surface != nil && strings.Contains(surfaceText(b), ">")
	}, 2*time.Second, "initial composer render")

	// Type "Hello Tasksmith" into Composer
	for _, ch := range "Hello Tasksmith" {
		b.PostEvent(&backend.RawKeyEvent{
			Key: key.Key{Code: ch, Text: string(ch)},
		})
	}

	waitFor(t, func() bool {
		return strings.Contains(surfaceText(b), "Hello Tasksmith")
	}, 2*time.Second, "typed text in composer")

	// Press Enter
	b.PostEvent(&backend.RawKeyEvent{
		Key: key.Key{Code: key.KeyEnter},
	})

	// Composer should clear, pending user message should appear in ActiveStream,
	// hint should become "streaming • esc to cancel", and message sent to daemon
	waitFor(t, func() bool {
		st.mu.Lock()
		sentCount := len(st.sent)
		st.mu.Unlock()
		txt := surfaceText(b)
		return sentCount > 0 &&
			strings.Contains(txt, "Hello Tasksmith") &&
			(strings.Contains(txt, "Thinking...") || strings.Contains(txt, "streaming • esc to cancel"))
	}, 2*time.Second, "active stream prompt submission and hint")

	// Verify client-supplied message ID was generated (UUIDv7)
	st.mu.Lock()
	if len(st.sent) == 0 {
		st.mu.Unlock()
		t.Fatal("expected message to be sent")
	}
	sentReq := st.sent[0]
	st.mu.Unlock()
	if sentReq.ID == "" {
		t.Fatal("expected client-generated UUIDv7 ID on SendRequest, got empty")
	}
	if sentReq.Content != "Hello Tasksmith" {
		t.Fatalf("expected SendRequest content 'Hello Tasksmith', got %q", sentReq.Content)
	}

	// Press Escape to cancel
	b.PostEvent(&backend.RawKeyEvent{
		Key: key.Key{Code: key.KeyEscape},
	})

	waitFor(t, func() bool {
		st.mu.Lock()
		defer st.mu.Unlock()
		return len(st.cancels) > 0
	}, 2*time.Second, "cancel request delivered to daemon")

	waitFor(t, func() bool {
		txt := surfaceText(b)
		return !store.GetStream().Streaming &&
			!strings.Contains(txt, "Thinking...") &&
			!strings.Contains(txt, "streaming • esc to cancel") &&
			strings.Contains(txt, "shift+tab cycle mode • /help commands")
	}, 2*time.Second, "UI state reset on cancel")
}

func TestRunSlashCommandModeAndExit(t *testing.T) {
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
		return b.LastFrame().Surface != nil && strings.Contains(surfaceText(b), ">")
	}, 2*time.Second, "initial composer render")

	// Type "/mode auto"
	for _, ch := range "/mode auto" {
		b.PostEvent(&backend.RawKeyEvent{
			Key: key.Key{Code: ch, Text: string(ch)},
		})
	}
	b.PostEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyEnter}})

	waitFor(t, func() bool {
		return store.GetSession().Config.Mode == api.ModeAuto
	}, 2*time.Second, "mode to switch to auto")

	// Type "/exit"
	for _, ch := range "/exit" {
		b.PostEvent(&backend.RawKeyEvent{
			Key: key.Key{Code: ch, Text: string(ch)},
		})
	}
	b.PostEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyEnter}})

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("expected nil error on /exit, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Run to exit after /exit")
	}
}

func TestAppSlashCommandHelpAndEscapeRestoresComposerFocus(t *testing.T) {
	store.ClearTranscript()
	store.ClearSlot()
	defer store.ClearTranscript()
	defer store.ClearSlot()

	b := mock.New(80, 24)
	c, err := client.New("/tmp/mock.sock")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(ctx, Options{
			Client:    c,
			Workspace: "/tmp/ws",
			Backend:   b,
		})
	}()

	waitFor(t, func() bool {
		return b.LastFrame().Surface != nil && strings.Contains(surfaceText(b), ">")
	}, 2*time.Second, "initial composer render")

	// 1. Type "/help" and press Enter
	for _, ch := range "/help" {
		b.PostEvent(&backend.RawKeyEvent{
			Key: key.Key{Code: ch, Text: string(ch)},
		})
	}
	b.PostEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyEnter}})

	waitFor(t, func() bool {
		txt := surfaceText(b)
		return store.GetSlot().Active != nil &&
			strings.Contains(txt, "Available commands:") &&
			strings.Contains(txt, "Filter:")
	}, 2*time.Second, "help picker to open in slot")

	// 2. Press Esc while /help picker is open
	b.PostEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyEscape}})

	waitFor(t, func() bool {
		txt := allText(b)
		return store.GetSlot().Active == nil &&
			strings.Contains(txt, "Exited /help command") &&
			strings.Contains(surfaceText(b), ">")
	}, 2*time.Second, "help picker to close and exit notice to appear")

	// 3. Immediately type "/mode" and press Enter
	for _, ch := range "/mode" {
		b.PostEvent(&backend.RawKeyEvent{
			Key: key.Key{Code: ch, Text: string(ch)},
		})
	}
	b.PostEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyEnter}})

	waitFor(t, func() bool {
		txt := surfaceText(b)
		return store.GetSlot().Active != nil &&
			strings.Contains(txt, "Select permission mode:")
	}, 2*time.Second, "mode picker to open in slot")

	// 4. Press Esc on /mode picker
	b.PostEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyEscape}})

	waitFor(t, func() bool {
		txt := allText(b)
		return store.GetSlot().Active == nil &&
			strings.Contains(txt, "Exited /mode command") &&
			strings.Contains(surfaceText(b), ">")
	}, 2*time.Second, "mode picker to close and exit notice to appear")

	// 5. Verify Composer can be typed into again
	for _, ch := range "echo test" {
		b.PostEvent(&backend.RawKeyEvent{
			Key: key.Key{Code: ch, Text: string(ch)},
		})
	}
	waitFor(t, func() bool {
		return strings.Contains(surfaceText(b), "echo test")
	}, 2*time.Second, "typed text in composer after mode picker closed")

	// 5. Verify Composer can be typed into again
	for _, ch := range "echo test" {
		b.PostEvent(&backend.RawKeyEvent{
			Key: key.Key{Code: ch, Text: string(ch)},
		})
	}
	waitFor(t, func() bool {
		return strings.Contains(surfaceText(b), "echo test")
	}, 2*time.Second, "typed text in composer after mode picker closed")
}

func TestRunResumeSessionHeaderIsItem0(t *testing.T) {
	store.ClearTranscript()
	store.ClearSlot()
	defer store.ClearTranscript()
	defer store.ClearSlot()

	b := mock.New(80, 24)
	c, st := startTestUnixDaemon(t)

	sess, err := c.CreateSession(t.Context(), api.CreateSessionRequest{
		Config: api.SessionConfig{
			Model: "test-model-42",
		},
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	st.mu.Lock()
	st.history[sess.ID] = []api.Message{
		{
			ID:   "msg-1",
			Role: api.RoleUser,
			Content: []api.Block{
				{Type: api.BlockText, Text: "Initial question"},
			},
		},
		{
			ID:   "msg-2",
			Role: api.RoleAssistant,
			Content: []api.Block{
				{Type: api.BlockText, Text: "Initial answer"},
			},
		},
	}
	st.mu.Unlock()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	opts := Options{
		Client:    c,
		Workspace: "/workspace/project",
		Version:   "0.1.0",
		SessionID: sess.ID,
		Backend:   b,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(ctx, opts)
	}()

	waitFor(t, func() bool {
		return b.LastFrame().Surface != nil && strings.Contains(surfaceText(b), ">")
	}, 2*time.Second, "initial composer render")

	waitFor(t, func() bool {
		items := store.GetTranscript().Items
		return items != nil && len(*items) >= 3
	}, 2*time.Second, "transcript to load header and timeline entries")

	items := *store.GetTranscript().Items
	if items[0].Kind != store.TranscriptKindHeader {
		t.Fatalf("expected items[0] to be header, got %s", items[0].Kind)
	}
	if items[1].Kind != store.TranscriptKindMessage {
		t.Fatalf("expected items[1] to be message, got %s", items[1].Kind)
	}
	if items[2].Kind != store.TranscriptKindMessage {
		t.Fatalf("expected items[2] to be message, got %s", items[2].Kind)
	}
	if active := ActiveSession(); active == nil || active.ID != sess.ID {
		t.Errorf("expected ActiveSession().ID = %q, got %+v", sess.ID, active)
	}
}

type clearMockBackend struct {
	*mock.Backend
}

func (c *clearMockBackend) Clear() {
	c.Backend.Clear()
	c.InsertedAbove = nil
}

func TestRunClearReprintHeader(t *testing.T) {
	store.ClearTranscript()
	store.ClearSlot()
	defer store.ClearTranscript()
	defer store.ClearSlot()

	rawBackend := mock.New(80, 24)
	b := &clearMockBackend{Backend: rawBackend}
	c, _ := startTestUnixDaemon(t)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	opts := Options{
		Client:    c,
		Workspace: "/workspace/clear-repl",
		Version:   "0.1.0",
		Backend:   b,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(ctx, opts)
	}()

	waitFor(t, func() bool {
		return b.LastFrame().Surface != nil && strings.Contains(surfaceText(b.Backend), ">")
	}, 2*time.Second, "initial composer render")

	// Wait for initial header to be printed into InsertedAbove
	waitFor(t, func() bool {
		return strings.Contains(allText(b.Backend), "TaskSmith v0.1.0")
	}, 2*time.Second, "initial header in InsertedAbove")

	// Append a notice to make transcript dirty
	store.AppendNotice("old notice that must be cleared")

	waitFor(t, func() bool {
		return strings.Contains(allText(b.Backend), "old notice that must be cleared")
	}, 2*time.Second, "dirty notice in InsertedAbove")

	// Type /clear in draft mode: clears draft items from surface without calling Clear()
	for _, ch := range "/clear" {
		b.PostEvent(&backend.RawKeyEvent{
			Key: key.Key{Code: ch, Text: string(ch)},
		})
	}
	b.PostEvent(&backend.RawKeyEvent{
		Key: key.Key{Code: key.KeyEnter},
	})

	waitFor(t, func() bool {
		return !strings.Contains(surfaceText(b.Backend), "old notice that must be cleared") && strings.Contains(surfaceText(b.Backend), "TaskSmith v0.1.0")
	}, 2*time.Second, "draft notice cleared from surface")

	// Now test frozen mode: freeze transcript, append notice, and /clear should wipe backend
	store.FreezeTranscript()
	store.AppendNotice("frozen notice to be cleared")

	waitFor(t, func() bool {
		return strings.Contains(allText(b.Backend), "frozen notice to be cleared")
	}, 2*time.Second, "frozen notice in scrollback")

	for _, ch := range "/clear" {
		b.PostEvent(&backend.RawKeyEvent{
			Key: key.Key{Code: ch, Text: string(ch)},
		})
	}
	b.PostEvent(&backend.RawKeyEvent{
		Key: key.Key{Code: key.KeyEnter},
	})

	// Wait for /clear to execute in frozen mode, clearing backend and reprinting Header
	waitFor(t, func() bool {
		return b.ClearCalls >= 1 && strings.Contains(allText(b.Backend), "TaskSmith v0.1.0")
	}, 2*time.Second, "Clear() called and header reprinted")

	txt := allText(b.Backend)
	if !strings.Contains(txt, "TaskSmith v0.1.0") {
		t.Fatalf("expected header to be reprinted after /clear, got:\n%s", txt)
	}
	if strings.Contains(txt, "frozen notice to be cleared") {
		t.Fatalf("expected frozen notice to be cleared from scrollback, got:\n%s", txt)
	}
}
