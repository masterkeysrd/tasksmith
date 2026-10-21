package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/masterkeysrd/loom/decision"
	"github.com/masterkeysrd/loom/embedding"
	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/loom/profile"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/gateway"
)

type fakeProvider struct {
	mu           sync.Mutex
	chunkHandler func(ctx context.Context, req *llm.Request) (llm.StreamResponse, error)
}

func (p *fakeProvider) Name() string                              { return "fake" }
func (p *fakeProvider) ListProfiles() []profile.Profile           { return nil }
func (p *fakeProvider) GetProfile(string) (profile.Profile, bool) { return profile.Profile{}, false }
func (p *fakeProvider) SearchProfiles(string) []profile.Profile   { return nil }
func (p *fakeProvider) OverrideProfile(string, profile.Profile)   {}

func (p *fakeProvider) setHandler(fn func(ctx context.Context, req *llm.Request) (llm.StreamResponse, error)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.chunkHandler = fn
}

func (p *fakeProvider) Stream(ctx context.Context, req *llm.Request) (llm.StreamResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	p.mu.Lock()
	fn := p.chunkHandler
	p.mu.Unlock()

	if fn != nil {
		return fn(ctx, req)
	}

	chunks := []string{"Hello", " from ", "daemon!"}
	respID := uuid.NewString()
	return func(yield func(message.AssistantChunk, error) bool) {
		for i, text := range chunks {
			if err := ctx.Err(); err != nil {
				yield(message.AssistantChunk{}, err)
				return
			}
			ch := message.AssistantChunk{
				BaseChunk: message.BaseChunk{ID: respID},
				Model:     req.Model,
				Content: []message.Block{
					&message.TextBlock{Text: text},
				},
				Done: i == len(chunks)-1,
			}
			if !yield(ch, nil) {
				return
			}
		}
	}, nil
}

type fakeGateway struct {
	provider *fakeProvider
}

func (g *fakeGateway) ResolveLLM(ctx context.Context, explicit string, _ ...gateway.LLMOption) (*llm.Model, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	modelName := strings.TrimSpace(explicit)
	if modelName == "" {
		modelName = "fake-default-model"
	}
	return llm.NewModel(g.provider, modelName, nil)
}

func (g *fakeGateway) ResolveDecision(context.Context, string, ...gateway.DecisionOption) (*decision.Model, error) {
	return nil, gateway.ErrUnsupportedCapability
}

func (g *fakeGateway) ResolveEmbedding(context.Context, string, ...gateway.EmbeddingOption) (*embedding.Model, error) {
	return nil, gateway.ErrUnsupportedCapability
}

func (g *fakeGateway) ListProviders() []gateway.ProviderInfo { return nil }
func (g *fakeGateway) Close() error                          { return nil }

type fakeGatewayPool struct {
	gw *fakeGateway
}

func newFakeGatewayPool(prov *fakeProvider) *fakeGatewayPool {
	if prov == nil {
		prov = &fakeProvider{}
	}
	return &fakeGatewayPool{
		gw: &fakeGateway{provider: prov},
	}
}

func (p *fakeGatewayPool) Acquire(ctx context.Context, _ gateway.Config) (gateway.Gateway, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return p.gw, nil
}

func (p *fakeGatewayPool) Close() error {
	return nil
}

func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "tsd-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(dir)
	})
	return dir
}

func setupDaemonTest(t *testing.T, prov *fakeProvider) (*Server, *client.Client, string) {
	t.Helper()

	xdgRoot := t.TempDir()
	xdg.SetTestEnv(xdgRoot, "tasksmith-daemon-test")
	t.Cleanup(func() {
		xdg.ClearCache()
	})

	runDir := shortTempDir(t)
	sockPath := filepath.Join(runDir, "b.sock")
	lockPath := filepath.Join(runDir, "b.lock")

	srv, err := New(Config{
		SocketPath:  sockPath,
		LockPath:    lockPath,
		GatewayPool: newFakeGatewayPool(prov),
		Logger:      log.Default(),
	})
	if err != nil {
		t.Fatalf("daemon.New: %v", err)
	}

	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("srv.Start: %v", err)
	}
	t.Cleanup(func() {
		_ = srv.Close()
	})

	cli, err := client.New(sockPath)
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}

	wsDir := shortTempDir(t)
	return srv, cli, wsDir
}

func extractAPIMessageText(msg api.Message) string {
	var sb strings.Builder
	for _, b := range msg.Content {
		if b.Type == api.BlockText {
			sb.WriteString(b.Text)
		}
	}
	return sb.String()
}

func TestConfigValidateAndDefaultPaths(t *testing.T) {
	xdgRoot := t.TempDir()
	xdg.SetTestEnv(xdgRoot, "tasksmith-daemon-cfg-test")
	t.Cleanup(func() {
		xdg.ClearCache()
	})

	sockPath, err := DefaultSocketPath()
	if err != nil || sockPath == "" {
		t.Fatalf("DefaultSocketPath() = (%q, %v)", sockPath, err)
	}
	lockPath, err := DefaultLockPath()
	if err != nil || lockPath == "" {
		t.Fatalf("DefaultLockPath() = (%q, %v)", lockPath, err)
	}

	pool := newFakeGatewayPool(nil)
	logger := log.Default()

	tests := []struct {
		name string
		cfg  Config
	}{
		{
			name: "missing SocketPath",
			cfg: Config{
				LockPath:    lockPath,
				GatewayPool: pool,
				Logger:      logger,
			},
		},
		{
			name: "missing LockPath",
			cfg: Config{
				SocketPath:  sockPath,
				GatewayPool: pool,
				Logger:      logger,
			},
		},
		{
			name: "missing GatewayPool",
			cfg: Config{
				SocketPath: sockPath,
				LockPath:   lockPath,
				Logger:     logger,
			},
		},
		{
			name: "missing Logger",
			cfg: Config{
				SocketPath:  sockPath,
				LockPath:    lockPath,
				GatewayPool: pool,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.cfg.Validate(); !errors.Is(err, ErrInvalidConfig) {
				t.Errorf("Validate() error = %v, want %v", err, ErrInvalidConfig)
			}
			if _, err := New(tc.cfg); !errors.Is(err, ErrInvalidConfig) {
				t.Errorf("New() error = %v, want %v", err, ErrInvalidConfig)
			}
		})
	}
}

func TestLockfileSingleInstanceProtection(t *testing.T) {
	runDir := shortTempDir(t)
	sock1 := filepath.Join(runDir, "1.sock")
	sock2 := filepath.Join(runDir, "2.sock")
	lockPath := filepath.Join(runDir, "shared.lock")

	srv1, err := New(Config{
		SocketPath:  sock1,
		LockPath:    lockPath,
		GatewayPool: newFakeGatewayPool(nil),
		Logger:      log.Default(),
	})
	if err != nil {
		t.Fatalf("New srv1: %v", err)
	}
	if err := srv1.Start(context.Background()); err != nil {
		t.Fatalf("srv1.Start: %v", err)
	}
	defer func() {
		_ = srv1.Close()
	}()

	srv2, err := New(Config{
		SocketPath:  sock2,
		LockPath:    lockPath,
		GatewayPool: newFakeGatewayPool(nil),
		Logger:      log.Default(),
	})
	if err != nil {
		t.Fatalf("New srv2: %v", err)
	}

	if err := srv2.Start(context.Background()); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("srv2.Start error = %v, want %v", err, ErrAlreadyRunning)
	}

	// After closing srv1, srv2 can acquire the lock and start cleanly.
	if err := srv1.Close(); err != nil {
		t.Fatalf("srv1.Close: %v", err)
	}
	if err := srv2.Start(context.Background()); err != nil {
		t.Fatalf("srv2.Start after srv1 closed: %v", err)
	}
	_ = srv2.Close()
}

func TestHealthAndSessionCRUD(t *testing.T) {
	ctx := context.Background()
	_, cli, wsDir := setupDaemonTest(t, nil)

	// 1. Health before any workspace is opened
	health1, err := cli.Health(ctx)
	if err != nil {
		t.Fatalf("Health 1: %v", err)
	}
	if health1.PID != os.Getpid() {
		t.Errorf("Health PID = %d, want %d", health1.PID, os.Getpid())
	}
	if len(health1.Workspaces) != 0 {
		t.Errorf("expected 0 open workspaces initially, got %v", health1.Workspaces)
	}

	// 2. CreateSession
	s1, err := cli.CreateSession(ctx, wsDir, api.CreateSessionRequest{
		Title: "Session One",
	})
	if err != nil {
		t.Fatalf("CreateSession s1: %v", err)
	}
	if s1.ID == "" || s1.Title != "Session One" || s1.Status != api.StatusIdle {
		t.Errorf("unexpected s1: %+v", s1)
	}

	// Health now lists wsDir
	health2, err := cli.Health(ctx)
	if err != nil {
		t.Fatalf("Health 2: %v", err)
	}
	if len(health2.Workspaces) != 1 {
		t.Fatalf("expected 1 open workspace, got %v", health2.Workspaces)
	}

	time.Sleep(2 * time.Millisecond)

	s2, err := cli.CreateSession(ctx, wsDir, api.CreateSessionRequest{
		Title: "Session Two",
		Model: "anthropic/claude-4-sonnet",
	})
	if err != nil {
		t.Fatalf("CreateSession s2: %v", err)
	}

	// 3. GetSession
	got1, err := cli.GetSession(ctx, wsDir, s1.ID)
	if err != nil {
		t.Fatalf("GetSession s1: %v", err)
	}
	if got1.ID != s1.ID || got1.Title != "Session One" || got1.Status != api.StatusIdle {
		t.Errorf("GetSession s1 = %+v", got1)
	}

	// 4. ListSessions (ordered by updated_at DESC -> [s2, s1])
	list, err := cli.ListSessions(ctx, wsDir)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(list) != 2 || list[0].ID != s2.ID || list[1].ID != s1.ID {
		t.Fatalf("ListSessions = %+v, want [%s, %s]", list, s2.ID, s1.ID)
	}

	// 5. DeleteSession
	if err := cli.DeleteSession(ctx, wsDir, s1.ID); err != nil {
		t.Fatalf("DeleteSession s1: %v", err)
	}

	var apiErr *api.APIError
	if _, err := cli.GetSession(ctx, wsDir, s1.ID); !errors.As(err, &apiErr) || apiErr.StatusCode != 404 {
		t.Errorf("GetSession deleted s1 error = %v, want 404 APIError", err)
	}
}

func TestSSESubscribeAndStreamingTurn(t *testing.T) {
	ctx := context.Background()
	prov := &fakeProvider{
		chunkHandler: func(_ context.Context, req *llm.Request) (llm.StreamResponse, error) {
			parts := []string{"Alpha", " Beta", " Gamma"}
			respID := "chunk-stream-1"
			return func(yield func(message.AssistantChunk, error) bool) {
				for i, text := range parts {
					if !yield(message.AssistantChunk{
						BaseChunk: message.BaseChunk{ID: respID},
						Model:     req.Model,
						Content:   []message.Block{&message.TextBlock{Text: text}},
						Done:      i == len(parts)-1,
					}, nil) {
						return
					}
				}
			}, nil
		},
	}

	_, cli, wsDir := setupDaemonTest(t, prov)

	sess, err := cli.CreateSession(ctx, wsDir, api.CreateSessionRequest{Title: "SSE Session"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	subCtx, cancelSub := context.WithCancel(ctx)
	defer cancelSub()

	eventsCh := make(chan api.Event, 32)
	subReady := make(chan struct{})
	go func() {
		first := true
		for ev, err := range cli.Subscribe(subCtx, wsDir, sess.ID) {
			if err != nil {
				return
			}
			if first {
				first = false
			}
			eventsCh <- ev
			if ev.Type == api.EventCompleted {
				return
			}
		}
	}()
	// Give Subscribe a brief moment to connect and flush `: connected\n\n`
	close(subReady)
	<-subReady
	time.Sleep(30 * time.Millisecond)

	if err := cli.Send(ctx, wsDir, sess.ID, api.SendRequest{
		Content: "Stream me",
		Wait:    true,
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	var received []api.Event
	timeout := time.After(3 * time.Second)
collectLoop:
	for {
		select {
		case ev := <-eventsCh:
			received = append(received, ev)
			if ev.Type == api.EventCompleted {
				break collectLoop
			}
		case <-timeout:
			t.Fatalf("timed out waiting for SSE events; got so far: %+v", received)
		}
	}

	// Expected sequence:
	// 1. message_added (user)
	// 2..4. chunk ("Alpha", " Beta", " Gamma")
	// 5. message_added (assistant)
	// 6. completed
	if len(received) != 6 {
		t.Fatalf("expected 6 SSE events, got %d: %+v", len(received), received)
	}
	if received[0].Type != api.EventMessageAdded || received[0].Message == nil || received[0].Message.Role != api.RoleUser {
		t.Errorf("received[0] = %+v, want user message_added", received[0])
	}
	var streamed strings.Builder
	for i := 1; i <= 3; i++ {
		if received[i].Type != api.EventChunk || received[i].Chunk == nil {
			t.Errorf("received[%d] = %+v, want chunk", i, received[i])
			continue
		}
		for _, b := range received[i].Chunk.Content {
			streamed.WriteString(b.Text)
		}
	}
	if streamed.String() != "Alpha Beta Gamma" {
		t.Errorf("streamed chunks = %q, want \"Alpha Beta Gamma\"", streamed.String())
	}
	if received[4].Type != api.EventMessageAdded || received[4].Message == nil || received[4].Message.Role != api.RoleAssistant {
		t.Errorf("received[4] = %+v, want assistant message_added", received[4])
	}
	if received[5].Type != api.EventCompleted {
		t.Errorf("received[5] = %+v, want completed", received[5])
	}
}

func TestBackgroundTurnSurvivesClientDisconnect(t *testing.T) {
	ctx := context.Background()

	turnStarted := make(chan struct{})
	releaseTurn := make(chan struct{})

	prov := &fakeProvider{
		chunkHandler: func(runCtx context.Context, req *llm.Request) (llm.StreamResponse, error) {
			close(turnStarted)
			select {
			case <-releaseTurn:
			case <-runCtx.Done():
				return nil, runCtx.Err()
			}
			return func(yield func(message.AssistantChunk, error) bool) {
				yield(message.AssistantChunk{
					BaseChunk: message.BaseChunk{ID: "bg-reply-1"},
					Model:     req.Model,
					Content:   []message.Block{&message.TextBlock{Text: "Completed in background!"}},
					Done:      true,
				}, nil)
			}, nil
		},
	}

	_, cli, wsDir := setupDaemonTest(t, prov)

	sess, err := cli.CreateSession(ctx, wsDir, api.CreateSessionRequest{Title: "Background Turn"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// Start a turn with a short-lived client context that disconnects while the turn is running!
	shortCtx, cancelShort := context.WithCancel(ctx)
	sendDone := make(chan error, 1)
	go func() {
		sendDone <- cli.Send(shortCtx, wsDir, sess.ID, api.SendRequest{
			Content: "Run in background",
			Wait:    true,
		})
	}()

	select {
	case <-turnStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for turn to start")
	}

	// Verify GetSession reports StatusRunning while turn is active
	runningSess, err := cli.GetSession(ctx, wsDir, sess.ID)
	if err != nil {
		t.Fatalf("GetSession while running: %v", err)
	}
	if runningSess.Status != api.StatusRunning {
		t.Errorf("GetSession.Status = %q, want %q", runningSess.Status, api.StatusRunning)
	}

	// Concurrent Send while running must return 409 Conflict
	var apiErr *api.APIError
	if err := cli.Send(ctx, wsDir, sess.ID, api.SendRequest{Content: "concurrent"}); !errors.As(err, &apiErr) || apiErr.StatusCode != 409 {
		t.Errorf("concurrent Send error = %v, want 409 Conflict", err)
	}

	// Disconnect the client HTTP request mid-turn!
	cancelShort()
	<-sendDone

	// Release the LLM turn and verify it completes and persists to SQLite even after client disconnect!
	close(releaseTurn)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		st, err := cli.GetSession(ctx, wsDir, sess.ID)
		if err == nil && st.Status == api.StatusIdle {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	afterSess, err := cli.GetSession(ctx, wsDir, sess.ID)
	if err != nil {
		t.Fatalf("GetSession after completion: %v", err)
	}
	if afterSess.Status != api.StatusIdle {
		t.Fatalf("afterSess.Status = %q, want %q", afterSess.Status, api.StatusIdle)
	}

	history, err := cli.History(ctx, wsDir, sess.ID)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("expected 2 persisted messages in History after client disconnect, got %d: %+v", len(history), history)
	}
	if history[1].Role != api.RoleAssistant || extractAPIMessageText(history[1]) != "Completed in background!" {
		t.Errorf("history[1] = %+v, want 'Completed in background!'", history[1])
	}
}

func TestCancelRunningTurn(t *testing.T) {
	ctx := context.Background()

	turnStarted := make(chan struct{})
	prov := &fakeProvider{
		chunkHandler: func(runCtx context.Context, _ *llm.Request) (llm.StreamResponse, error) {
			close(turnStarted)
			<-runCtx.Done()
			return nil, runCtx.Err()
		},
	}

	_, cli, wsDir := setupDaemonTest(t, prov)

	sess, err := cli.CreateSession(ctx, wsDir, api.CreateSessionRequest{Title: "Cancelable Turn"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if err := cli.Send(ctx, wsDir, sess.ID, api.SendRequest{
		Content: "Hang forever",
		Wait:    false,
	}); err != nil {
		t.Fatalf("Send(Wait: false): %v", err)
	}

	select {
	case <-turnStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for turn to start")
	}

	if err := cli.Cancel(ctx, wsDir, sess.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	afterSess, err := cli.GetSession(ctx, wsDir, sess.ID)
	if err != nil {
		t.Fatalf("GetSession after Cancel: %v", err)
	}
	if afterSess.Status != api.StatusIdle {
		t.Errorf("Status after Cancel = %q, want %q", afterSess.Status, api.StatusIdle)
	}
}

func TestPermissionsModesAndFileChangesRevert(t *testing.T) {
	ctx := context.Background()

	var step int
	prov := &fakeProvider{
		chunkHandler: func(_ context.Context, req *llm.Request) (llm.StreamResponse, error) {
			step++
			if step == 1 {
				return func(yield func(message.AssistantChunk, error) bool) {
					yield(message.AssistantChunk{
						BaseChunk: message.BaseChunk{ID: uuid.NewString()},
						Model:     req.Model,
						Content: []message.Block{
							&message.TextBlock{Text: "I will write hello.txt now."},
							&message.ToolCall{
								ID:   "tc-write-hello",
								Name: "write",
								Args: map[string]any{
									"path":    "hello.txt",
									"content": "updated content\n",
									"force":   true,
								},
							},
						},
						Done: true,
					}, nil)
				}, nil
			}
			return func(yield func(message.AssistantChunk, error) bool) {
				yield(message.AssistantChunk{
					BaseChunk: message.BaseChunk{ID: uuid.NewString()},
					Model:     req.Model,
					Content: []message.Block{
						&message.TextBlock{Text: "Successfully wrote hello.txt!"},
					},
					Done: true,
				}, nil)
			}, nil
		},
	}

	_, cli, wsDir := setupDaemonTest(t, prov)

	// Create an initial baseline file on disk so we can test diff and revert
	targetFile := filepath.Join(wsDir, "hello.txt")
	if err := os.WriteFile(targetFile, []byte("initial baseline\n"), 0o644); err != nil {
		t.Fatalf("WriteFile baseline: %v", err)
	}

	sess, err := cli.CreateSession(ctx, wsDir, api.CreateSessionRequest{Title: "Permissions & Revert"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// 1. Test PermissionMode get and set
	mode, err := cli.PermissionMode(ctx, wsDir, sess.ID)
	if err != nil {
		t.Fatalf("PermissionMode: %v", err)
	}
	if mode != api.ModeDefault {
		t.Errorf("initial mode = %q, want %q", mode, api.ModeDefault)
	}

	if err := cli.SetPermissionMode(ctx, wsDir, sess.ID, api.ModeAsk); err != nil {
		t.Fatalf("SetPermissionMode(ModeAsk): %v", err)
	}
	if got, err := cli.PermissionMode(ctx, wsDir, sess.ID); err != nil || got != api.ModeAsk {
		t.Errorf("PermissionMode after Set = (%q, %v), want %q", got, err, api.ModeAsk)
	}
	if err := cli.SetPermissionMode(ctx, wsDir, sess.ID, api.ModeDefault); err != nil {
		t.Fatalf("SetPermissionMode(ModeDefault): %v", err)
	}

	// Subscribe to SSE to verify permission_requested event
	subCtx, cancelSub := context.WithCancel(ctx)
	defer cancelSub()
	permEvents := make(chan api.Event, 16)
	go func() {
		for ev, err := range cli.Subscribe(subCtx, wsDir, sess.ID) {
			if err != nil {
				return
			}
			if ev.Type == api.EventPermissionRequested {
				permEvents <- ev
			}
		}
	}()
	time.Sleep(30 * time.Millisecond)

	// 2. Send triggers write tool call -> pauses on permission gate
	if err := cli.Send(ctx, wsDir, sess.ID, api.SendRequest{
		Content: "Please update hello.txt",
		Wait:    true,
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	select {
	case ev := <-permEvents:
		if len(ev.Permissions) != 1 || ev.Permissions[0].ToolName != "write" {
			t.Errorf("unexpected permission_requested event: %+v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for permission_requested SSE event")
	}

	// Session status must be StatusWaitingPermission
	waitingSess, err := cli.GetSession(ctx, wsDir, sess.ID)
	if err != nil {
		t.Fatalf("GetSession waiting: %v", err)
	}
	if waitingSess.Status != api.StatusWaitingPermission {
		t.Errorf("waitingSess.Status = %q, want %q", waitingSess.Status, api.StatusWaitingPermission)
	}

	perms, err := cli.PendingPermissions(ctx, wsDir, sess.ID)
	if err != nil {
		t.Fatalf("PendingPermissions: %v", err)
	}
	if len(perms) != 1 || perms[0].ToolName != "write" {
		t.Fatalf("PendingPermissions = %+v, want 1 write permission", perms)
	}

	// 3. Approve permission via RespondPermission
	if err := cli.RespondPermission(ctx, wsDir, sess.ID, api.PermissionDecision{
		RequestID: perms[0].ID,
		Kind:      api.DecisionAllowOnce,
		Wait:      true,
	}); err != nil {
		t.Fatalf("RespondPermission: %v", err)
	}

	// Verify file on disk was updated
	diskBytes, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatalf("ReadFile targetFile: %v", err)
	}
	if string(diskBytes) != "updated content\n" {
		t.Errorf("disk content = %q, want \"updated content\\n\"", string(diskBytes))
	}

	// 4. Inspect Changes and DiffChange
	changes, err := cli.Changes(ctx, wsDir, sess.ID)
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}
	if len(changes) != 1 || changes[0].Path != "hello.txt" || changes[0].Tool != "write" {
		t.Fatalf("Changes = %+v, want 1 write change on hello.txt", changes)
	}

	diffText, err := cli.DiffChange(ctx, wsDir, sess.ID, "hello.txt", changes[0].ID)
	if err != nil {
		t.Fatalf("DiffChange: %v", err)
	}
	if !strings.Contains(diffText, "-initial baseline") || !strings.Contains(diffText, "+updated content") {
		t.Errorf("unexpected DiffChange output:\n%s", diffText)
	}

	// 5. Revert session back to baseline via client.Revert
	if err := cli.Revert(ctx, wsDir, sess.ID, api.RevertRequest{
		Force: true,
	}); err != nil {
		t.Fatalf("Revert session: %v", err)
	}

	revertedBytes, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatalf("ReadFile after Revert: %v", err)
	}
	if string(revertedBytes) != "initial baseline\n" {
		t.Errorf("content after Revert = %q, want \"initial baseline\\n\"", string(revertedBytes))
	}

	// 6. RecoverAt the first change entry (before=false -> "updated content\n")
	if err := cli.Revert(ctx, wsDir, sess.ID, api.RevertRequest{
		Path:    "hello.txt",
		EntryID: changes[0].ID,
		Before:  false,
	}); err != nil {
		t.Fatalf("Revert RecoverAt: %v", err)
	}
	recoveredBytes, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatalf("ReadFile after RecoverAt: %v", err)
	}
	if string(recoveredBytes) != "updated content\n" {
		t.Errorf("content after RecoverAt = %q, want \"updated content\\n\"", string(recoveredBytes))
	}

	// 7. Revert single file to baseline (Path="hello.txt", EntryID="")
	if err := cli.Revert(ctx, wsDir, sess.ID, api.RevertRequest{
		Path:  "hello.txt",
		Force: true,
	}); err != nil {
		t.Fatalf("Revert file to baseline: %v", err)
	}
	finalBytes, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatalf("ReadFile after file Revert: %v", err)
	}
	if string(finalBytes) != "initial baseline\n" {
		t.Errorf("content after file Revert = %q, want \"initial baseline\\n\"", string(finalBytes))
	}
}

func TestServerShutdownEndpoint(t *testing.T) {
	ctx := context.Background()
	_, cli, _ := setupDaemonTest(t, nil)

	if _, err := cli.Health(ctx); err != nil {
		t.Fatalf("cli.Health: %v", err)
	}

	if err := cli.Shutdown(ctx); err != nil {
		t.Fatalf("cli.Shutdown: %v", err)
	}
}

func TestConversionsAndErrorPublishing(t *testing.T) {
	ctx := context.Background()

	// Test block conversions (ThinkingBlock, ToolCall, ToolCallChunk, fromSendRequest)
	reqMsg, err := fromSendRequest(api.SendRequest{
		Content: "Lead text",
		Blocks: []api.Block{
			{Type: api.BlockReasoning, Text: "thought", Signature: "sig1"},
			{Type: api.BlockToolCall, ToolCall: &api.ToolCall{ID: "tc1", Name: "view", Args: map[string]any{"path": "x.go"}}},
		},
	})
	if err != nil {
		t.Fatalf("fromSendRequest: %v", err)
	}
	apiMsg := toAPIMessage(reqMsg)
	if apiMsg == nil || len(apiMsg.Content) != 3 {
		t.Fatalf("toAPIMessage = %+v, want 3 blocks", apiMsg)
	}

	if _, err := fromSendRequest(api.SendRequest{}); !errors.Is(err, ErrEmptyMessage) {
		t.Errorf("fromSendRequest empty error = %v, want %v", err, ErrEmptyMessage)
	}

	// Test SSE EventError when LLM turn fails in background
	prov := &fakeProvider{
		chunkHandler: func(_ context.Context, _ *llm.Request) (llm.StreamResponse, error) {
			return nil, errors.New("upstream llm failure")
		},
	}
	_, cli, wsDir := setupDaemonTest(t, prov)

	sess, err := cli.CreateSession(ctx, wsDir, api.CreateSessionRequest{Title: "Error Session"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	subCtx, cancelSub := context.WithCancel(ctx)
	defer cancelSub()

	errEvents := make(chan api.Event, 4)
	go func() {
		for ev, sErr := range cli.Subscribe(subCtx, wsDir, sess.ID) {
			if sErr != nil {
				return
			}
			if ev.Type == api.EventError {
				errEvents <- ev
				return
			}
		}
	}()
	time.Sleep(30 * time.Millisecond)

	if err := cli.Send(ctx, wsDir, sess.ID, api.SendRequest{Content: "trigger error", Wait: false}); err != nil {
		t.Fatalf("Send(Wait: false): %v", err)
	}

	select {
	case ev := <-errEvents:
		if !strings.Contains(ev.Error, "upstream llm failure") {
			t.Errorf("EventError = %q, want to contain 'upstream llm failure'", ev.Error)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for EventError on SSE stream")
	}
}

func TestSubdirectorySharesCanonicalWorkspaceRuntime(t *testing.T) {
	ctx := context.Background()
	_, cli, wsDir := setupDaemonTest(t, nil)

	// Write a WORKSPACE.md sentinel in wsDir and create a nested subdirectory
	wsManifest := "---\napiVersion: warp/v1alpha1\nkind: Workspace\nmetadata:\n  name: root-ws\n---\n"
	if err := os.WriteFile(filepath.Join(wsDir, "WORKSPACE.md"), []byte(wsManifest), 0o644); err != nil {
		t.Fatalf("WriteFile WORKSPACE.md: %v", err)
	}
	subDir := filepath.Join(wsDir, "cmd", "subapp")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatalf("MkdirAll subDir: %v", err)
	}

	// Create session using wsDir
	sess, err := cli.CreateSession(ctx, wsDir, api.CreateSessionRequest{Title: "Root Session"})
	if err != nil {
		t.Fatalf("CreateSession at root: %v", err)
	}

	// Query ListSessions and GetSession using subDir -> must resolve to the same workspace runtime!
	list, err := cli.ListSessions(ctx, subDir)
	if err != nil {
		t.Fatalf("ListSessions from subDir: %v", err)
	}
	if len(list) != 1 || list[0].ID != sess.ID {
		t.Fatalf("ListSessions from subDir = %+v, want [%s]", list, sess.ID)
	}

	got, err := cli.GetSession(ctx, subDir, sess.ID)
	if err != nil || got.ID != sess.ID {
		t.Fatalf("GetSession from subDir = (%+v, %v)", got, err)
	}

	// Verify Health reports only 1 open workspace (the canonical root)
	health, err := cli.Health(ctx)
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if len(health.Workspaces) != 1 {
		t.Fatalf("expected 1 canonical workspace in Health, got %v", health.Workspaces)
	}

	// Subscribe using subDir and Send using wsDir -> subscriber must receive events!
	subCtx, cancelSub := context.WithCancel(ctx)
	defer cancelSub()

	doneCh := make(chan struct{}, 1)
	go func() {
		for ev, sErr := range cli.Subscribe(subCtx, subDir, sess.ID) {
			if sErr != nil {
				return
			}
			if ev.Type == api.EventCompleted {
				doneCh <- struct{}{}
				return
			}
		}
	}()
	time.Sleep(30 * time.Millisecond)

	if err := cli.Send(ctx, wsDir, sess.ID, api.SendRequest{Content: "hello from root", Wait: true}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	select {
	case <-doneCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for completed SSE event when subscribed via subDir")
	}
}


