package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
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
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway"
	"github.com/masterkeysrd/tasksmith/internal/daemon/timeline"
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
	if eval, err := filepath.EvalSymlinks(dir); err == nil {
		dir = eval
	}
	dir = filepath.Clean(dir)
	t.Cleanup(func() {
		_ = os.RemoveAll(dir)
	})
	return dir
}

func setupDaemonTest(t *testing.T, prov *fakeProvider) (*Daemon, *client.Client, string) {
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
		_ = srv.Close(context.Background())
	})

	wsDir := shortTempDir(t)
	cli, err := client.New(sockPath, client.WithCWD(wsDir))
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}

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
		_ = srv1.Close(context.Background())
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
	if err := srv1.Close(context.Background()); err != nil {
		t.Fatalf("srv1.Close: %v", err)
	}
	if err := srv2.Start(context.Background()); err != nil {
		t.Fatalf("srv2.Start after srv1 closed: %v", err)
	}
	_ = srv2.Close(context.Background())
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
	s1, err := cli.CreateSession(ctx, api.CreateSessionRequest{
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
	if len(health2.Workspaces) != 1 || health2.Workspaces[0] != wsDir {
		t.Fatalf("expected 1 open workspace (%s), got %v", wsDir, health2.Workspaces)
	}

	time.Sleep(2 * time.Millisecond)

	s2, err := cli.CreateSession(ctx, api.CreateSessionRequest{
		Title: "Session Two",
		Model: "anthropic/claude-4-sonnet",
	})
	if err != nil {
		t.Fatalf("CreateSession s2: %v", err)
	}

	// 3. GetSession
	got1, err := cli.GetSession(ctx, s1.ID)
	if err != nil {
		t.Fatalf("GetSession s1: %v", err)
	}
	if got1.ID != s1.ID || got1.Title != "Session One" || got1.Status != api.StatusIdle {
		t.Errorf("GetSession s1 = %+v", got1)
	}

	// 3b. UpdateSession
	up1, err := cli.UpdateSession(ctx, s1.ID, api.UpdateSessionRequest{
		Title: "Session One Renamed",
		Model: "anthropic/claude-4-sonnet",
	})
	if err != nil {
		t.Fatalf("UpdateSession s1: %v", err)
	}
	if up1.Title != "Session One Renamed" || up1.Model != "anthropic/claude-4-sonnet" {
		t.Errorf("UpdateSession s1 = %+v", up1)
	}

	// 4. ListSessions (ordered by updated_at DESC -> s1 updated after s2)
	list, err := cli.ListSessions(ctx)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(list) != 2 || list[0].ID != s1.ID || list[1].ID != s2.ID {
		t.Fatalf("ListSessions = %+v, want [%s, %s]", list, s1.ID, s2.ID)
	}

	// 5. DeleteSession
	if err := cli.DeleteSession(ctx, s1.ID); err != nil {
		t.Fatalf("DeleteSession s1: %v", err)
	}

	var apiErr *api.APIError
	if _, err := cli.GetSession(ctx, s1.ID); !errors.As(err, &apiErr) || apiErr.StatusCode != 404 {
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

	_, cli, _ := setupDaemonTest(t, prov)

	sess, err := cli.CreateSession(ctx, api.CreateSessionRequest{Title: "SSE Session"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	subCtx, cancelSub := context.WithCancel(ctx)
	defer cancelSub()

	eventsCh := make(chan api.Event, 32)
	subReady := make(chan struct{})
	go func() {
		for ev, err := range cli.EventsWithReady(subCtx, "", func(string) { close(subReady) }, api.SessionStreamTopic(sess.ID)) {
			if err != nil {
				return
			}
			eventsCh <- ev
			if ev.Type == api.EventCompleted {
				return
			}
		}
	}()
	select {
	case <-subReady:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for subscription ready")
	}

	if err := cli.Send(ctx, sess.ID, api.SendRequest{
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
			if ev.Type == api.EventConnected {
				continue
			}
			received = append(received, ev)
			if ev.Type == api.EventCompleted {
				break collectLoop
			}
		case <-timeout:
			t.Fatalf("timed out waiting for SSE events; got so far: %+v", received)
		}
	}

	// Expected sequence:
	// 1. entry_added (user message)
	// 2..4. chunk ("Alpha", " Beta", " Gamma")
	// 5. entry_added (assistant message)
	// 6. completed
	if len(received) != 6 {
		t.Fatalf("expected 6 SSE events, got %d: %+v", len(received), received)
	}
	p0, ok := received[0].Payload.(api.EntryAddedEvent)
	if received[0].Type != api.EventEntryAdded || !ok || p0.Entry.Message == nil || p0.Entry.Message.Role != api.RoleUser {
		t.Errorf("received[0] = %+v, want user entry_added", received[0])
	}
	var streamed strings.Builder
	for i := 1; i <= 3; i++ {
		chPayload, ok := received[i].Payload.(api.ChunkEvent)
		if received[i].Type != api.EventChunk || !ok {
			t.Errorf("received[%d] = %+v, want chunk", i, received[i])
			continue
		}
		for _, b := range chPayload.Chunk.Content {
			streamed.WriteString(b.Text)
		}
	}
	if streamed.String() != "Alpha Beta Gamma" {
		t.Errorf("streamed chunks = %q, want \"Alpha Beta Gamma\"", streamed.String())
	}
	p4, ok := received[4].Payload.(api.EntryAddedEvent)
	if received[4].Type != api.EventEntryAdded || !ok || p4.Entry.Message == nil || p4.Entry.Message.Role != api.RoleAssistant {
		t.Errorf("received[4] = %+v, want assistant entry_added", received[4])
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

	_, cli, _ := setupDaemonTest(t, prov)

	sess, err := cli.CreateSession(ctx, api.CreateSessionRequest{Title: "Background Turn"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// Start a turn with a short-lived client context that disconnects while the turn is running!
	shortCtx, cancelShort := context.WithCancel(ctx)
	sendDone := make(chan error, 1)
	go func() {
		sendDone <- cli.Send(shortCtx, sess.ID, api.SendRequest{
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
	runningSess, err := cli.GetSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("GetSession while running: %v", err)
	}
	if runningSess.Status != api.StatusRunning {
		t.Errorf("GetSession.Status = %q, want %q", runningSess.Status, api.StatusRunning)
	}

	// Concurrent Send while running must return 409 Conflict
	var apiErr *api.APIError
	if err := cli.Send(ctx, sess.ID, api.SendRequest{Content: "concurrent"}); !errors.As(err, &apiErr) || apiErr.StatusCode != 409 {
		t.Errorf("concurrent Send error = %v, want 409 Conflict", err)
	}

	// Disconnect the client HTTP request mid-turn!
	cancelShort()
	<-sendDone

	// Release the LLM turn and verify it completes and persists to SQLite even after client disconnect!
	close(releaseTurn)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		st, err := cli.GetSession(ctx, sess.ID)
		if err == nil && st.Status == api.StatusIdle {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	afterSess, err := cli.GetSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("GetSession after completion: %v", err)
	}
	if afterSess.Status != api.StatusIdle {
		t.Fatalf("afterSess.Status = %q, want %q", afterSess.Status, api.StatusIdle)
	}

	timelineEntries, err := cli.Timeline(ctx, sess.ID)
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	var messages []api.Message
	for _, entry := range timelineEntries {
		if entry.Kind == api.TimelineKindMessage && entry.Message != nil {
			messages = append(messages, *entry.Message)
		}
	}
	if len(messages) != 2 {
		t.Fatalf("expected 2 persisted messages in Timeline after client disconnect, got %d: %+v", len(messages), messages)
	}
	if messages[1].Role != api.RoleAssistant || extractAPIMessageText(messages[1]) != "Completed in background!" {
		t.Errorf("messages[1] = %+v, want 'Completed in background!'", messages[1])
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

	_, cli, _ := setupDaemonTest(t, prov)

	sess, err := cli.CreateSession(ctx, api.CreateSessionRequest{Title: "Cancelable Turn"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if err := cli.Send(ctx, sess.ID, api.SendRequest{
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

	if err := cli.Cancel(ctx, sess.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	afterSess, err := cli.GetSession(ctx, sess.ID)
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

	sess, err := cli.CreateSession(ctx, api.CreateSessionRequest{Title: "Permissions & Revert"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// 1. Test PermissionMode get and set
	mode, err := cli.PermissionMode(ctx, sess.ID)
	if err != nil {
		t.Fatalf("PermissionMode: %v", err)
	}
	if mode != api.ModeDefault {
		t.Errorf("initial mode = %q, want %q", mode, api.ModeDefault)
	}

	if err := cli.SetPermissionMode(ctx, sess.ID, api.ModeAsk); err != nil {
		t.Fatalf("SetPermissionMode(ModeAsk): %v", err)
	}
	if got, err := cli.PermissionMode(ctx, sess.ID); err != nil || got != api.ModeAsk {
		t.Errorf("PermissionMode after Set = (%q, %v), want %q", got, err, api.ModeAsk)
	}
	if err := cli.SetPermissionMode(ctx, sess.ID, api.ModeDefault); err != nil {
		t.Fatalf("SetPermissionMode(ModeDefault): %v", err)
	}

	// Subscribe to SSE to verify permission_requested event
	subCtx, cancelSub := context.WithCancel(ctx)
	defer cancelSub()
	permEvents := make(chan api.Event, 16)
	permReady := make(chan struct{})
	go func() {
		for ev, err := range cli.EventsWithReady(subCtx, "", func(string) { close(permReady) }, api.SessionStreamTopic(sess.ID)) {
			if err != nil {
				return
			}
			if ev.Type == api.EventPermissionRequested {
				permEvents <- ev
			}
		}
	}()
	select {
	case <-permReady:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for permission sub ready")
	}

	// 2. Send triggers write tool call -> pauses on permission gate
	if err := cli.Send(ctx, sess.ID, api.SendRequest{
		Content: "Please update hello.txt",
		Wait:    true,
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	select {
	case ev := <-permEvents:
		p, ok := ev.Payload.(api.PermissionRequestedEvent)
		if !ok || len(p.Permissions) != 1 || p.Permissions[0].ToolName != "write" {
			t.Errorf("unexpected permission_requested event: %+v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for permission_requested SSE event")
	}

	// Session status must be StatusWaitingPermission
	waitingSess, err := cli.GetSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("GetSession waiting: %v", err)
	}
	if waitingSess.Status != api.StatusWaitingPermission {
		t.Errorf("waitingSess.Status = %q, want %q", waitingSess.Status, api.StatusWaitingPermission)
	}

	perms, err := cli.PendingPermissions(ctx, sess.ID)
	if err != nil {
		t.Fatalf("PendingPermissions: %v", err)
	}
	if len(perms) != 1 || perms[0].ToolName != "write" {
		t.Fatalf("PendingPermissions = %+v, want 1 write permission", perms)
	}

	// 3. Approve permission via RespondPermission
	if err := cli.RespondPermission(ctx, sess.ID, api.PermissionDecision{
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
	changes, err := cli.Changes(ctx, sess.ID)
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}
	if len(changes) != 1 || changes[0].Path != "hello.txt" || changes[0].Tool != "write" {
		t.Fatalf("Changes = %+v, want 1 write change on hello.txt", changes)
	}

	diffText, err := cli.DiffChange(ctx, sess.ID, "hello.txt", changes[0].ID)
	if err != nil {
		t.Fatalf("DiffChange: %v", err)
	}
	if !strings.Contains(diffText, "-initial baseline") || !strings.Contains(diffText, "+updated content") {
		t.Errorf("unexpected DiffChange output:\n%s", diffText)
	}

	// 5. Revert session back to baseline via client.Revert
	if err := cli.Revert(ctx, sess.ID, api.RevertRequest{
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
	if err := cli.Revert(ctx, sess.ID, api.RevertRequest{
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
	if err := cli.Revert(ctx, sess.ID, api.RevertRequest{
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
	apiMsg, err := toAPIMessage(reqMsg)
	if err != nil {
		t.Fatalf("toAPIMessage: %v", err)
	}
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
	_, cli, _ := setupDaemonTest(t, prov)

	sess, err := cli.CreateSession(ctx, api.CreateSessionRequest{Title: "Error Session"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	subCtx, cancelSub := context.WithCancel(ctx)
	defer cancelSub()

	errEvents := make(chan api.Event, 4)
	errReady := make(chan struct{})
	go func() {
		for ev, sErr := range cli.EventsWithReady(subCtx, "", func(string) { close(errReady) }, api.SessionStreamTopic(sess.ID)) {
			if sErr != nil {
				return
			}
			if ev.Type == api.EventError {
				errEvents <- ev
				return
			}
		}
	}()
	select {
	case <-errReady:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for error sub ready")
	}

	if err := cli.Send(ctx, sess.ID, api.SendRequest{Content: "trigger error", Wait: false}); err != nil {
		t.Fatalf("Send(Wait: false): %v", err)
	}

	select {
	case ev := <-errEvents:
		p, ok := ev.Payload.(api.ErrorEvent)
		if !ok || !strings.Contains(p.Error, "upstream llm failure") {
			t.Errorf("EventError = %+v, want to contain 'upstream llm failure'", ev)
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
	sess, err := cli.CreateSession(ctx, api.CreateSessionRequest{Title: "Root Session"})
	if err != nil {
		t.Fatalf("CreateSession at root: %v", err)
	}

	// Query ListSessions and GetSession using subDir -> must resolve to the same workspace runtime!
	subCLI := cli.WithCWD(subDir)
	list, err := subCLI.ListSessions(ctx)
	if err != nil {
		t.Fatalf("ListSessions from subDir: %v", err)
	}
	if len(list) != 1 || list[0].ID != sess.ID {
		t.Fatalf("ListSessions from subDir = %+v, want [%s]", list, sess.ID)
	}

	got, err := subCLI.GetSession(ctx, sess.ID)
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
	subReady := make(chan struct{})
	go func() {
		for ev, sErr := range subCLI.EventsWithReady(subCtx, "", func(string) { close(subReady) }, api.SessionStreamTopic(sess.ID)) {
			if sErr != nil {
				return
			}
			if ev.Type == api.EventCompleted {
				doneCh <- struct{}{}
				return
			}
		}
	}()
	select {
	case <-subReady:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for subDir sub ready")
	}

	if err := cli.Send(ctx, sess.ID, api.SendRequest{Content: "hello from root", Wait: true}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	select {
	case <-doneCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for completed SSE event when subscribed via subDir")
	}
}

func TestWorkspaceAgentsModelsEndpoints(t *testing.T) {
	ctx := context.Background()
	_, cli, wsDir := setupDaemonTest(t, nil)

	// 1. GetWorkspace
	ws, err := cli.GetWorkspace(ctx)
	if err != nil {
		t.Fatalf("GetWorkspace: %v", err)
	}
	if ws.RootDir != wsDir {
		t.Errorf("ws.RootDir = %q, want %q", ws.RootDir, wsDir)
	}
	if ws.CWD != wsDir {
		t.Errorf("ws.CWD = %q, want %q", ws.CWD, wsDir)
	}
	if ws.DefaultAgent == "" {
		t.Errorf("ws.DefaultAgent should not be empty")
	}

	// 2. ListAgents
	agents, err := cli.ListAgents(ctx)
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if len(agents) == 0 {
		t.Fatalf("expected at least 1 agent, got 0")
	}
	var foundMain bool
	for _, ag := range agents {
		if ag.Name == "main" {
			foundMain = true
			break
		}
	}
	if !foundMain {
		t.Errorf("expected 'main' agent in ListAgents: %+v", agents)
	}

	// 3. GetAgent (found & 404)
	ag, err := cli.GetAgent(ctx, "main")
	if err != nil {
		t.Fatalf("GetAgent('main'): %v", err)
	}
	if ag.Name != "main" {
		t.Errorf("ag.Name = %q, want 'main'", ag.Name)
	}

	var apiErr *api.APIError
	if _, err := cli.GetAgent(ctx, "nonexistent-agent"); !errors.As(err, &apiErr) || apiErr.StatusCode != 404 {
		t.Errorf("GetAgent non-existent = %v, want 404 APIError", err)
	}

	// 4. ListModels
	models, err := cli.ListModels(ctx)
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	_ = models

	// 5. GetModel with bare name and qualified name (fallback / custom unlisted resolution)
	m1, err := cli.GetModel(ctx, "custom-model")
	if err != nil {
		t.Fatalf("GetModel('custom-model'): %v", err)
	}
	if m1.ID != "custom-model" || m1.Name != "custom-model" || m1.Label != "custom-model" {
		t.Errorf("m1 = %+v, want fallback custom-model", m1)
	}

	m2, err := cli.GetModel(ctx, "ollama/qwen3.8")
	if err != nil {
		t.Fatalf("GetModel('ollama/qwen3.8'): %v", err)
	}
	if m2.ID != "ollama/qwen3.8" || m2.Provider != "ollama" || m2.Name != "qwen3.8" {
		t.Errorf("m2 = %+v, want qualified model", m2)
	}
}

func TestCustomWorkspaceProvidersAndEffort(t *testing.T) {
	ctx := context.Background()
	_, cli, wsDir := setupDaemonTest(t, nil)

	// Create workspace manifest declaring custom default agent
	wsManifest := `---
apiVersion: warp/v1alpha1
kind: Workspace
metadata:
  name: configured-ws
spec:
  defaultAgent: custom-agent
---
Workspace instructions.
`
	if err := os.WriteFile(filepath.Join(wsDir, "WORKSPACE.md"), []byte(wsManifest), 0o644); err != nil {
		t.Fatalf("WriteFile WORKSPACE.md: %v", err)
	}

	agentsDir := filepath.Join(wsDir, ".agents")
	provDir := filepath.Join(agentsDir, "providers")
	if err := os.MkdirAll(provDir, 0o755); err != nil {
		t.Fatalf("MkdirAll provDir: %v", err)
	}

	// Provider with model thinking capability (effort)
	provYAML := `apiVersion: warp/v1alpha1
kind: ModelProvider
metadata:
  name: my-ollama
spec:
  type: ollama
  defaultModel: qwen3.8
  models:
    - id: qwen3.8
      name: qwen3.8
      label: "Qwen 3.8"
      capabilities:
        completion: true
        thinking:
          - type: effort
            default: high
            allowedEfforts: ["low", "medium", "high"]
`
	if err := os.WriteFile(filepath.Join(provDir, "ollama.yaml"), []byte(provYAML), 0o644); err != nil {
		t.Fatalf("WriteFile ollama.yaml: %v", err)
	}

	// Agent with thinking effort
	agentMD := `---
apiVersion: warp/v1alpha1
kind: Agent
metadata:
  name: custom-agent
  displayName: "Custom Agent"
  description: "Specialized helper"
spec:
  models:
    - my-ollama/qwen3.8
  thinking:
    type: effort
    default: medium
---
Custom agent instructions.
`
	if err := os.WriteFile(filepath.Join(agentsDir, "custom-agent.agent.md"), []byte(agentMD), 0o644); err != nil {
		t.Fatalf("WriteFile custom-agent.agent.md: %v", err)
	}

	// Query endpoints with a fresh client pointed to wsDir
	wsCLI := cli.WithCWD(wsDir)

	ws, err := wsCLI.GetWorkspace(ctx)
	if err != nil {
		t.Fatalf("GetWorkspace: %v", err)
	}
	if ws.Name != "configured-ws" {
		t.Errorf("ws.Name = %q, want 'configured-ws'", ws.Name)
	}
	if ws.DefaultAgent != "custom-agent" {
		t.Errorf("ws.DefaultAgent = %q, want 'custom-agent'", ws.DefaultAgent)
	}

	ag, err := wsCLI.GetAgent(ctx, "custom-agent")
	if err != nil {
		t.Fatalf("GetAgent('custom-agent'): %v", err)
	}
	if ag.Name != "custom-agent" || ag.DisplayName != "Custom Agent" || ag.Effort != "medium" {
		t.Errorf("ag = %+v, want Effort='medium'", ag)
	}

	mdl, err := wsCLI.GetModel(ctx, "my-ollama/qwen3.8")
	if err != nil {
		t.Fatalf("GetModel('my-ollama/qwen3.8'): %v", err)
	}
	if mdl.ID != "my-ollama/qwen3.8" || mdl.Provider != "my-ollama" || mdl.Name != "qwen3.8" || mdl.Label != "Qwen 3.8" {
		t.Errorf("mdl = %+v, want Qwen 3.8", mdl)
	}
	if mdl.DefaultEffort != "high" || len(mdl.AllowedEfforts) != 3 {
		t.Errorf("mdl effort = %q, allowed=%v, want 'high' and [low, medium, high]", mdl.DefaultEffort, mdl.AllowedEfforts)
	}

	models, err := wsCLI.ListModels(ctx)
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	var foundOllama bool
	for _, m := range models {
		if m.ID == "my-ollama/qwen3.8" {
			foundOllama = true
			if m.DefaultEffort != "high" || m.Label != "Qwen 3.8" {
				t.Errorf("listed model mismatch: %+v", m)
			}
			break
		}
	}
	if !foundOllama {
		t.Errorf("expected 'my-ollama/qwen3.8' in ListModels: %+v", models)
	}
}

func TestHeaderCWDFallbackToQueryParam(t *testing.T) {
	ctx := context.Background()
	_, cli, wsDir := setupDaemonTest(t, nil)

	unixClient := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", cli.SocketPath())
			},
		},
	}

	// Make raw request using X-TaskSmith-CWD
	rawReq, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix/v1/workspace", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	rawReq.Header.Set("Accept", "application/json")
	rawReq.Header.Set(api.HeaderCWD, wsDir)

	resp, err := unixClient.Do(rawReq)
	if err != nil {
		t.Fatalf("httpClient.Do with header: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("response status with header = %d, want 200", resp.StatusCode)
	}

	// Make raw request using query param fallback ?workspace=...
	queryReq, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix/v1/workspace?workspace="+wsDir, nil)
	if err != nil {
		t.Fatalf("NewRequest query: %v", err)
	}
	queryReq.Header.Set("Accept", "application/json")

	qResp, err := unixClient.Do(queryReq)
	if err != nil {
		t.Fatalf("httpClient.Do with query: %v", err)
	}
	defer qResp.Body.Close()
	if qResp.StatusCode != http.StatusOK {
		t.Fatalf("response status with query = %d, want 200", qResp.StatusCode)
	}

	// Make raw request with no header and no query param -> 400 Bad Request
	emptyReq, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix/v1/workspace", nil)
	if err != nil {
		t.Fatalf("NewRequest empty: %v", err)
	}
	emptyReq.Header.Set("Accept", "application/json")

	eResp, err := unixClient.Do(emptyReq)
	if err != nil {
		t.Fatalf("httpClient.Do empty: %v", err)
	}
	defer eResp.Body.Close()
	if eResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("response status with empty workspace = %d, want 400", eResp.StatusCode)
	}
}

func TestSubprojectPreservesCWDAndLocalConfigs(t *testing.T) {
	ctx := context.Background()
	_, cli, wsDir := setupDaemonTest(t, nil)

	// Workspace root configuration
	wsManifest := `---
apiVersion: warp/v1alpha1
kind: Workspace
metadata:
  name: mono-repo
spec:
  projects: [".", "packages/sub"]
---
Root workspace.
`
	if err := os.WriteFile(filepath.Join(wsDir, "WORKSPACE.md"), []byte(wsManifest), 0o644); err != nil {
		t.Fatalf("WriteFile WORKSPACE.md: %v", err)
	}

	rootAgentsDir := filepath.Join(wsDir, ".agents")
	if err := os.MkdirAll(rootAgentsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll rootAgentsDir: %v", err)
	}
	rootAgentMD := `---
apiVersion: warp/v1alpha1
kind: Agent
metadata:
  name: main
  description: "Root agent"
spec:
  thinking:
    type: effort
    default: high
---
Root agent instructions.
`
	if err := os.WriteFile(filepath.Join(rootAgentsDir, "main.agent.md"), []byte(rootAgentMD), 0o644); err != nil {
		t.Fatalf("WriteFile root main.agent.md: %v", err)
	}

	// Subproject configuration
	subDir := filepath.Join(wsDir, "packages", "sub")
	subAgentsDir := filepath.Join(subDir, ".agents")
	if err := os.MkdirAll(subAgentsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll subAgentsDir: %v", err)
	}
	subAgentMD := `---
apiVersion: warp/v1alpha1
kind: Agent
metadata:
  name: main
  description: "Subproject agent"
spec:
  thinking:
    type: effort
    default: low
---
Subproject agent instructions.
`
	if err := os.WriteFile(filepath.Join(subAgentsDir, "main.agent.md"), []byte(subAgentMD), 0o644); err != nil {
		t.Fatalf("WriteFile sub main.agent.md: %v", err)
	}

	rootCLI := cli.WithCWD(wsDir)
	subCLI := cli.WithCWD(subDir)

	// Verify Root workspace
	rootWS, err := rootCLI.GetWorkspace(ctx)
	if err != nil {
		t.Fatalf("rootCLI.GetWorkspace: %v", err)
	}
	if rootWS.CWD != wsDir {
		t.Errorf("rootWS.CWD = %q, want %q", rootWS.CWD, wsDir)
	}
	rootAgent, err := rootCLI.GetAgent(ctx, "main")
	if err != nil {
		t.Fatalf("rootCLI.GetAgent('main'): %v", err)
	}
	if rootAgent.Effort != "high" || rootAgent.Description != "Root agent" {
		t.Errorf("rootAgent = %+v, want Effort='high' and Description='Root agent'", rootAgent)
	}

	// Verify Subproject preserves subDir CWD and shadows local agent config
	subWS, err := subCLI.GetWorkspace(ctx)
	if err != nil {
		t.Fatalf("subCLI.GetWorkspace: %v", err)
	}
	if subWS.CWD != subDir {
		t.Errorf("subWS.CWD = %q, want %q", subWS.CWD, subDir)
	}
	if subWS.RootDir != wsDir {
		t.Errorf("subWS.RootDir = %q, want %q", subWS.RootDir, wsDir)
	}
	subAgent, err := subCLI.GetAgent(ctx, "main")
	if err != nil {
		t.Fatalf("subCLI.GetAgent('main'): %v", err)
	}
	if subAgent.Effort != "low" || subAgent.Description != "Subproject agent" {
		t.Errorf("subAgent = %+v, want Effort='low' and Description='Subproject agent'", subAgent)
	}
}

func TestStructuredContentConversionFailure(t *testing.T) {
	tool := &message.Tool{
		Base: message.Base{ID: "result"}, ToolCallID: "call",
		StructuredContent: make(chan int),
	}
	converted, err := toAPIMessage(tool)
	var unsupported *json.UnsupportedTypeError
	if converted != nil || !errors.As(err, &unsupported) {
		t.Fatalf("toAPIMessage = (%+v, %v), want an unsupported type error", converted, err)
	}
	entry := timeline.NewMessageEntry("session", tool)
	_, entryErr := toAPITimelineEntry(*entry)
	if entryErr == nil || !errors.As(entryErr, &unsupported) {
		t.Fatalf("toAPITimelineEntry = %v, want unsupported type error", entryErr)
	}
}

func TestToolStructuredContentInEventsAndHistory(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	var step int
	provider := &fakeProvider{
		chunkHandler: func(_ context.Context, req *llm.Request) (llm.StreamResponse, error) {
			step++
			var content message.Content
			if step == 1 {
				content = message.Content{&message.ToolCall{
					ID: "view-call", Name: "view",
					Args: map[string]any{"path": "source.go", "offset": 2, "limit": 1},
				}}
			} else {
				content = message.Content{&message.TextBlock{Text: "Read complete."}}
			}
			return func(yield func(message.AssistantChunk, error) bool) {
				yield(message.AssistantChunk{
					BaseChunk: message.BaseChunk{ID: uuid.NewString()},
					Model:     req.Model, Content: content, Done: true,
				}, nil)
			}, nil
		},
	}
	_, cli, workspace := setupDaemonTest(t, provider)
	if err := os.WriteFile(filepath.Join(workspace, "source.go"), []byte("first\nsecond\nthird\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sess, err := cli.CreateSession(ctx, api.CreateSessionRequest{})
	if err != nil {
		t.Fatal(err)
	}
	ready := make(chan struct{})
	results := make(chan api.Message, 1)
	subErrors := make(chan error, 1)
	go func() {
		for event, err := range cli.EventsWithReady(ctx, "", func(string) { close(ready) }, api.SessionStreamTopic(sess.ID)) {
			if err != nil {
				subErrors <- err
				return
			}
			if event.Type == api.EventError {
				if p, ok := event.Payload.(api.ErrorEvent); ok {
					subErrors <- errors.New(p.Error)
				} else {
					subErrors <- errors.New("event error")
				}
				return
			}
			if event.Type == api.EventEntryAdded {
				if p, ok := event.Payload.(api.EntryAddedEvent); ok && p.Entry.Message != nil && p.Entry.Message.Role == api.RoleTool {
					results <- *p.Entry.Message
					return
				}
			}
		}
	}()
	select {
	case <-ready:
	case err := <-subErrors:
		t.Fatalf("subscribe: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := cli.Send(ctx, sess.ID, api.SendRequest{Content: "Read source.go.", Wait: true}); err != nil {
		t.Fatal(err)
	}
	var live api.Message
	select {
	case live = <-results:
	case err := <-subErrors:
		t.Fatalf("subscribe: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if live.IsError {
		t.Fatalf("view failed: %+v", live)
	}
	var liveOutput api.ViewOutput
	if err := json.Unmarshal(live.StructuredContent, &liveOutput); err != nil {
		t.Fatalf("decode live structured content: %v", err)
	}
	if liveOutput.Path == "" || liveOutput.StartLine != 2 || liveOutput.EndLine != 2 || liveOutput.Content != "second" {
		t.Fatalf("live output = %+v", liveOutput)
	}
	if len(live.Content) != 0 {
		t.Fatalf("expected tool content text block to be omitted when structured content is returned: %+v", live.Content)
	}
	timelineEntries, err := cli.Timeline(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range timelineEntries {
		if entry.Kind != api.TimelineKindMessage || entry.Message == nil || entry.Message.ID != live.ID {
			continue
		}
		var output api.ViewOutput
		if err := json.Unmarshal(entry.Message.StructuredContent, &output); err != nil {
			t.Fatalf("decode persisted structured content: %v", err)
		}
		if output != liveOutput {
			t.Fatalf("persisted output = %+v, want %+v", output, liveOutput)
		}
		return
	}
	t.Fatalf("tool result %q missing from timeline", live.ID)
}
