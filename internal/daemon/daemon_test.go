package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
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
	"github.com/masterkeysrd/tasksmith/internal/core/sys"
	"github.com/masterkeysrd/tasksmith/internal/core/testutils"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway/auth"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway/providers"
	"github.com/masterkeysrd/tasksmith/internal/daemon/models"
	"github.com/masterkeysrd/tasksmith/internal/daemon/sandbox"
	"github.com/masterkeysrd/tasksmith/internal/daemon/session"
	"github.com/masterkeysrd/tasksmith/internal/daemon/timeline"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
	"github.com/masterkeysrd/tasksmith/internal/version"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/oauth2"
)

type fakeProvider struct {
	mu           sync.Mutex
	chunkHandler func(ctx context.Context, req *llm.Request) (llm.StreamResponse, error)
	profiles     map[string]profile.Profile
}

func (p *fakeProvider) Name() string                    { return "fake" }
func (p *fakeProvider) ListProfiles() []profile.Profile { return nil }
func (p *fakeProvider) GetProfile(id string) (profile.Profile, bool) {
	if p.profiles != nil {
		if prof, ok := p.profiles[id]; ok {
			return prof, true
		}
	}
	return profile.Profile{}, false
}
func (p *fakeProvider) SearchProfiles(string) []profile.Profile { return nil }
func (p *fakeProvider) OverrideProfile(string, profile.Profile) {}

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
	provider  *fakeProvider
	providers []gateway.ProviderInfo
	models    map[string][]profile.Profile
}

func (g *fakeGateway) LLM(ctx context.Context, model models.Model, cfg llm.ModelConfig) (*llm.Model, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return llm.NewModel(g.provider, string(model.ID), &cfg)
}

func (g *fakeGateway) Decision(context.Context, models.Model, llm.ModelConfig) (*decision.Model, error) {
	return nil, gateway.ErrUnsupportedCapability
}

func (g *fakeGateway) Embedding(context.Context, models.Model) (*embedding.Model, error) {
	return nil, gateway.ErrUnsupportedCapability
}

func (g *fakeGateway) ListProviders() []gateway.ProviderInfo {
	return g.providers
}

func (g *fakeGateway) ListModels(_ context.Context, provider string) ([]profile.Profile, bool, error) {
	models, ok := g.models[provider]
	return models, ok, nil
}

func (g *fakeGateway) GetProfile(qualifiedID string) (profile.Profile, bool) {
	id := models.ID(qualifiedID)
	if g.provider != nil {
		if prof, found := g.provider.GetProfile(id.Name()); found {
			return prof, true
		}
		if prof, found := g.provider.GetProfile(string(id)); found {
			return prof, true
		}
	}
	return profile.Profile{}, false
}

func (g *fakeGateway) Quota(context.Context, models.Model) (*api.ModelQuota, error) {
	return nil, nil
}

func (g *fakeGateway) QuotaSnapshots(context.Context) (*api.QuotaSnapshotResponse, error) {
	return &api.QuotaSnapshotResponse{}, nil
}

func (g *fakeGateway) QuotaCapable(models.Model) bool { return false }

func (g *fakeGateway) Close() error { return nil }

type fakeGatewayPool struct {
	gw       *fakeGateway
	closeErr error
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
	return p.closeErr
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

	testutils.SetupXDG(t, "tasksmith-daemon-test")

	runDir := shortTempDir(t)
	sockPath := filepath.Join(runDir, "b.sock")
	lockPath := filepath.Join(runDir, "b.lock")

	srv, err := New(Config{
		SocketPath:  sockPath,
		LockPath:    lockPath,
		GatewayPool: newFakeGatewayPool(prov),
		Logger:      log.Default(),
		// Workspace manifests in these tests declare real MCP commands and
		// endpoints; connecting them would spawn processes and reach the network.
		DisableMCPAutoConnect: true,
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
	if err := workspace.TrustFolder(wsDir, api.TrustScopeFolder); err != nil {
		t.Fatal(err)
	}
	manifest := "---\napiVersion: warp/v1alpha1\nkind: Workspace\nmetadata:\n  name: test\nspec:\n  defaultProvider: fake\n  defaultModels:\n    completion: fake-default-model\n---\n"
	if err := os.WriteFile(filepath.Join(wsDir, "WORKSPACE.md"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	cli, err := client.New(sockPath, client.WithCWD(wsDir))
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}

	return srv, cli, wsDir
}

type closeTestOAuthDriver struct {
	auth.BrowserOAuthDriver
	closeErr error
	closes   int
}

func (d *closeTestOAuthDriver) Close() error {
	d.closes++
	return d.closeErr
}

func TestDaemonCloseAggregatesPoolAndOAuthDriverErrors(t *testing.T) {
	const provider = providers.ClaudeCodeName
	original, ok := auth.GetBrowserOAuthDriver(provider)
	if !ok {
		t.Fatalf("browser OAuth driver %q not registered", provider)
	}
	driver := &closeTestOAuthDriver{
		BrowserOAuthDriver: original,
		closeErr:           errors.New("browser OAuth driver close failed"),
	}
	auth.RegisterBrowserOAuthDriver(provider, driver)
	t.Cleanup(func() { auth.RegisterBrowserOAuthDriver(provider, original) })

	dir := t.TempDir()
	t.Cleanup(xdg.ClearCache)
	t.Setenv("XDG_DATA_HOME", dir)
	xdg.ClearCache()

	pool := newFakeGatewayPool(nil)
	pool.closeErr = errors.New("gateway pool close failed")
	d, err := New(Config{
		SocketPath:  filepath.Join(dir, "daemon.sock"),
		LockPath:    filepath.Join(dir, "daemon.lock"),
		GatewayPool: pool,
		Logger:      log.Default(),
	})
	if err != nil {
		t.Fatal(err)
	}

	err = d.Close(t.Context())
	for _, want := range []error{pool.closeErr, driver.closeErr} {
		if !errors.Is(err, want) {
			t.Errorf("Close() = %v, want it to contain %v", err, want)
		}
	}
	if again := d.Close(t.Context()); again != err {
		t.Errorf("second Close() = %v, want the original result %v", again, err)
	}
	if driver.closes != 1 {
		t.Errorf("driver close count = %d, want 1", driver.closes)
	}
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
	testutils.SetupXDG(t, "tasksmith-daemon-cfg-test")

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

func TestLockPathForSocket(t *testing.T) {
	tests := []struct {
		socket string
		want   string
	}{
		{"/run/tasksmith/brain.sock", "/run/tasksmith/brain.lock"},
		{"relative/brain.sock", "relative/brain.lock"},
		{"/run/tasksmith/custom.sock", "/run/tasksmith/custom.lock"},
		{"/run/tasksmith/odd", "/run/tasksmith/odd.lock"},
	}
	for _, tt := range tests {
		if got := LockPathForSocket(tt.socket); got != tt.want {
			t.Errorf("LockPathForSocket(%q) = %q, want %q", tt.socket, got, tt.want)
		}
	}
}

// TestStartRetriesLockDuringTeardown simulates the version-upgrade handoff: the
// previous daemon already closed its socket but still holds brain.lock while it
// finishes teardown. A replacement daemon must wait briefly for the lock and
// then start cleanly instead of exiting with ErrAlreadyRunning.
func TestStartRetriesLockDuringTeardown(t *testing.T) {
	runDir := shortTempDir(t)
	sock := filepath.Join(runDir, "handoff.sock")
	lockPath := filepath.Join(runDir, "handoff.lock")

	staleLock, err := sys.AcquireLock(lockPath)
	if err != nil {
		t.Fatalf("AcquireLock (simulated outgoing daemon): %v", err)
	}
	go func() {
		time.Sleep(300 * time.Millisecond)
		_ = sys.ReleaseLock(staleLock)
	}()

	srv, err := New(Config{
		SocketPath:  sock,
		LockPath:    lockPath,
		GatewayPool: newFakeGatewayPool(nil),
		Logger:      log.Default(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	start := time.Now()
	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start during teardown handoff: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 200*time.Millisecond {
		t.Errorf("Start returned after %v, expected it to wait for the lock handoff", elapsed)
	}
	_ = srv.Close(context.Background())
}

// TestStartFailsFastWhenSocketAlive verifies that when a daemon is both holding
// the lock and serving the socket, a second Start fails immediately with
// ErrAlreadyRunning annotated with the holder pid instead of burning the retry window.
func TestStartFailsFastWhenSocketAlive(t *testing.T) {
	runDir := shortTempDir(t)
	sock := filepath.Join(runDir, "alive.sock")
	lockPath := filepath.Join(runDir, "alive.lock")

	held, err := sys.AcquireLock(lockPath)
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}
	defer func() {
		_ = sys.ReleaseLock(held)
	}()

	ln, err := sys.ListenUnix(context.Background(), sock)
	if err != nil {
		t.Fatalf("ListenUnix: %v", err)
	}
	defer func() {
		_ = ln.Close()
	}()

	srv, err := New(Config{
		SocketPath:  sock,
		LockPath:    lockPath,
		GatewayPool: newFakeGatewayPool(nil),
		Logger:      log.Default(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	start := time.Now()
	err = srv.Start(context.Background())
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("Start error = %v, want %v", err, ErrAlreadyRunning)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("Start took %v with a live socket holder, want fast failure", elapsed)
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("held by pid=%d", os.Getpid())) {
		t.Errorf("Start error = %q, want it to name the holder pid", err)
	}
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
	if health1.Version != version.Get() {
		t.Errorf("Health Version = %q, want %q", health1.Version, version.Get())
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
		Config: api.SessionConfig{
			Model: "anthropic/claude-4-sonnet",
		},
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
		Config: api.SessionConfig{
			Model: "anthropic/claude-4-sonnet",
		},
	})
	if err != nil {
		t.Fatalf("UpdateSession s1: %v", err)
	}
	if up1.Title != "Session One Renamed" || up1.Config.Model != "anthropic/claude-4-sonnet" {
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
		for ev, err := range cli.EventsWithReady(subCtx, "", "", func(string) { close(subReady) }, api.SessionStreamTopic(sess.ID)) {
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
	// 0. status_changed (thinking)
	// 1. entry_added (user message)
	// 2. status_changed (generating)
	// 3..5. chunk ("Alpha", " Beta", " Gamma")
	// 6. entry_added (assistant message)
	// 7. context_updated (context)
	// 8. status_changed (idle)
	// 9. completed
	if len(received) != 10 {
		t.Fatalf("expected 10 SSE events, got %d: %+v", len(received), received)
	}
	if received[0].Type != api.EventStatusChanged {
		t.Errorf("received[0] = %+v, want status_changed", received[0])
	}
	if p, ok := received[0].Payload.(api.StatusChangedEvent); !ok || p.Status != api.StatusThinking {
		t.Errorf("received[0].Payload = %+v, want StatusThinking", received[0].Payload)
	}
	p1, ok := received[1].Payload.(api.EntryAddedEvent)
	if received[1].Type != api.EventEntryAdded || !ok || p1.Entry.Message == nil || p1.Entry.Message.Role != api.RoleUser {
		t.Errorf("received[1] = %+v, want user entry_added", received[1])
	}
	if received[2].Type != api.EventStatusChanged {
		t.Errorf("received[2] = %+v, want status_changed", received[2])
	}
	if p, ok := received[2].Payload.(api.StatusChangedEvent); !ok || p.Status != api.StatusGenerating {
		t.Errorf("received[2].Payload = %+v, want StatusGenerating", received[2].Payload)
	}
	var streamed strings.Builder
	for i := 3; i <= 5; i++ {
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
	p6, ok := received[6].Payload.(api.EntryAddedEvent)
	if received[6].Type != api.EventEntryAdded || !ok || p6.Entry.Message == nil || p6.Entry.Message.Role != api.RoleAssistant {
		t.Errorf("received[6] = %+v, want assistant entry_added", received[6])
	}
	if received[7].Type != api.EventContextUpdated {
		t.Errorf("received[7] = %+v, want context_updated", received[7])
	}
	if received[8].Type != api.EventStatusChanged {
		t.Errorf("received[8] = %+v, want status_changed", received[8])
	}
	if p, ok := received[8].Payload.(api.StatusChangedEvent); !ok || p.Status != api.StatusIdle {
		t.Errorf("received[8].Payload = %+v, want StatusIdle", received[8].Payload)
	}
	if received[9].Type != api.EventCompleted {
		t.Errorf("received[9] = %+v, want completed", received[9])
	}
}

func TestBackgroundTurnSurvivesClientDisconnect(t *testing.T) {
	ctx := context.Background()

	turnStarted := make(chan struct{})
	releaseTurn := make(chan struct{})

	var startOnce sync.Once
	prov := &fakeProvider{
		chunkHandler: func(runCtx context.Context, req *llm.Request) (llm.StreamResponse, error) {
			startOnce.Do(func() {
				close(turnStarted)
			})
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
		})
	}()

	select {
	case <-turnStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for turn to start")
	}

	// Verify GetSession reports active status (thinking) while turn is active
	runningSess, err := cli.GetSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("GetSession while running: %v", err)
	}
	if runningSess.Status != api.StatusThinking {
		t.Errorf("GetSession.Status = %q, want %q", runningSess.Status, api.StatusThinking)
	}

	// Concurrent Decision while running must return 409 Conflict
	var apiErr *api.APIError
	if err := cli.Send(ctx, sess.ID, api.SendRequest{Decision: &api.PermissionDecision{Kind: api.DecisionAllowOnce}}); !errors.As(err, &apiErr) || apiErr.StatusCode != 409 {
		t.Errorf("concurrent Decision error = %v, want 409 Conflict", err)
	}

	// Disconnect the client HTTP request mid-turn!
	cancelShort()
	if err := <-sendDone; err != nil && !errors.Is(err, context.Canceled) {
		t.Errorf("send during disconnect: %v", err)
	}

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
		chunkHandler: func(runCtx context.Context, req *llm.Request) (llm.StreamResponse, error) {
			return func(yield func(message.AssistantChunk, error) bool) {
				yield(message.AssistantChunk{
					BaseChunk: message.BaseChunk{ID: uuid.NewString()},
					Model:     req.Model,
					Content: message.Content{
						&message.ThinkingBlock{Thinking: "Analyzing request..."},
						&message.TextBlock{Text: "Partial response before cancel"},
					},
					Done: false,
				}, nil)
				close(turnStarted)
				<-runCtx.Done()
				yield(message.AssistantChunk{}, runCtx.Err())
			}, nil
		},
	}

	_, cli, _ := setupDaemonTest(t, prov)

	sess, err := cli.CreateSession(ctx, api.CreateSessionRequest{Title: "Cancelable Turn"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	subCtx, cancelSub := context.WithCancel(ctx)
	defer cancelSub()
	ready := make(chan struct{})
	eventsCh := make(chan api.Event, 32)
	go func() {
		for ev, err := range cli.EventsWithReady(subCtx, "", "", func(string) { close(ready) }, api.SessionStreamTopic(sess.ID)) {
			if err != nil {
				return
			}
			eventsCh <- ev
		}
	}()
	<-ready

	if err := cli.Send(ctx, sess.ID, api.SendRequest{
		Content: "Hang forever",
	}); err != nil {
		t.Fatalf("Send: %v", err)
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

	// Verify SSE events received for partial message and interrupted entry
	var gotPartialMsg, gotInterruptedEvent bool
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && (!gotPartialMsg || !gotInterruptedEvent) {
		select {
		case ev := <-eventsCh:
			if ev.Type == api.EventEntryAdded {
				if p, ok := ev.Payload.(api.EntryAddedEvent); ok {
					if p.Entry.Kind == api.TimelineKindMessage && p.Entry.Message != nil && p.Entry.Message.Role == api.RoleAssistant {
						gotPartialMsg = true
					}
					if p.Entry.Kind == api.TimelineKindInterrupted {
						gotInterruptedEvent = true
					}
				}
			}
		case <-time.After(50 * time.Millisecond):
		}
	}

	if !gotPartialMsg {
		t.Error("expected EventEntryAdded for partial assistant message on SSE")
	}
	if !gotInterruptedEvent {
		t.Error("expected EventEntryAdded for TimelineKindInterrupted on SSE")
	}

	// Verify timeline persistence
	tl, err := cli.Timeline(ctx, sess.ID)
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}

	var foundAsstMessage bool
	var foundInterrupted bool
	for _, entry := range tl {
		if entry.Kind == api.TimelineKindMessage && entry.Message != nil && entry.Message.Role == api.RoleAssistant {
			foundAsstMessage = true
			if len(entry.Message.Content) != 2 {
				t.Errorf("expected 2 blocks in partial assistant message, got %d", len(entry.Message.Content))
			} else {
				if entry.Message.Content[0].Type != api.BlockReasoning || entry.Message.Content[0].Text != "Analyzing request..." {
					t.Errorf("unexpected block 0: %+v", entry.Message.Content[0])
				}
				if entry.Message.Content[1].Type != api.BlockText || entry.Message.Content[1].Text != "Partial response before cancel" {
					t.Errorf("unexpected block 1: %+v", entry.Message.Content[1])
				}
			}
		}
		if entry.Kind == api.TimelineKindInterrupted {
			foundInterrupted = true
		}
	}

	if !foundAsstMessage {
		t.Error("expected partial assistant message entry in timeline")
	}
	if !foundInterrupted {
		t.Error("expected TimelineKindInterrupted entry in timeline")
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

	// 1. Test PermissionMode get and set via GetSession and UpdateSession
	sessInfo, err := cli.GetSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if sessInfo.Config.Mode != api.ModeDefault {
		t.Errorf("initial mode = %q, want %q", sessInfo.Config.Mode, api.ModeDefault)
	}

	updatedSess, err := cli.UpdateSession(ctx, sess.ID, api.UpdateSessionRequest{
		Config: api.SessionConfig{Mode: api.ModeAsk},
	})
	if err != nil {
		t.Fatalf("UpdateSession(ModeAsk): %v", err)
	}
	if updatedSess.Config.Mode != api.ModeAsk {
		t.Errorf("Mode after Update = %q, want %q", updatedSess.Config.Mode, api.ModeAsk)
	}
	if _, err := cli.UpdateSession(ctx, sess.ID, api.UpdateSessionRequest{
		Config: api.SessionConfig{Mode: api.ModeDefault},
	}); err != nil {
		t.Fatalf("UpdateSession(ModeDefault): %v", err)
	}

	// Subscribe to SSE to verify permission_requested event
	subCtx, cancelSub := context.WithCancel(ctx)
	defer cancelSub()
	permEvents := make(chan api.Event, 16)
	permReady := make(chan struct{})
	go func() {
		for ev, err := range cli.EventsWithReady(subCtx, "", "", func(string) { close(permReady) }, api.SessionStreamTopic(sess.ID)) {
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

	perms := waitingSess.PendingPermissions
	if len(perms) != 1 || perms[0].ToolName != "write" {
		t.Fatalf("PendingPermissions = %+v, want 1 write permission", perms)
	}

	// 3. Approve permission via Send decision
	if err := cli.Send(ctx, sess.ID, api.SendRequest{
		Decision: &api.PermissionDecision{
			RequestID: perms[0].ID,
			Kind:      api.DecisionAllowOnce,
		},
	}); err != nil {
		t.Fatalf("Send decision: %v", err)
	}

	// Verify file on disk was updated and the turn has settled so journal
	// snapshots are committed before querying Changes.
	var (
		diskBytes []byte
		settled   api.SessionStatus
	)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		b, readErr := os.ReadFile(targetFile)
		st, sessErr := cli.GetSession(ctx, sess.ID)
		if sessErr == nil {
			settled = st.Status
		}
		if readErr == nil && string(b) == "updated content\n" && sessErr == nil && st.Status == api.StatusIdle {
			diskBytes = b
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if string(diskBytes) != "updated content\n" || settled != api.StatusIdle {
		t.Fatalf("after approval: disk content = %q, status = %q; want \"updated content\\n\" and %q", string(diskBytes), settled, api.StatusIdle)
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

	// Test block conversions (ThinkingBlock, ToolCall, ToolCallChunk, session.FromAPIBlock)
	for _, block := range []api.Block{
		{Type: api.BlockReasoning, Text: "thought", Signature: "sig1"},
		{Type: api.BlockToolCall, ToolCall: &api.ToolCall{ID: "tc1", Name: "view", Args: map[string]any{"path": "x.go"}}},
	} {
		converted, ok := session.FromAPIBlock(block)
		if !ok {
			t.Fatalf("session.FromAPIBlock: %+v", block)
		}
		roundTrip, ok := session.ToAPIBlock(converted)
		if !ok || !reflect.DeepEqual(roundTrip, block) {
			t.Fatalf("block round trip: %+v, want %+v", roundTrip, block)
		}
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
	var apiErr *api.APIError
	if err := cli.Send(ctx, sess.ID, api.SendRequest{Content: "valid text", Blocks: []api.Block{{Type: "image"}}}); !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid block error = %v, want bad request", err)
	}

	subCtx, cancelSub := context.WithCancel(ctx)
	defer cancelSub()

	errEvents := make(chan api.Event, 4)
	errReady := make(chan struct{})
	go func() {
		for ev, sErr := range cli.EventsWithReady(subCtx, "", "", func(string) { close(errReady) }, api.SessionStreamTopic(sess.ID)) {
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

	if err := cli.Send(ctx, sess.ID, api.SendRequest{Content: "trigger error"}); err != nil {
		t.Fatalf("Send: %v", err)
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
	wsManifest := "---\napiVersion: warp/v1alpha1\nkind: Workspace\nmetadata:\n  name: root-ws\nspec:\n  defaultProvider: fake\n  defaultModels:\n    completion: fake-default-model\n---\n"
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
		for ev, sErr := range subCLI.EventsWithReady(subCtx, "", "", func(string) { close(subReady) }, api.SessionStreamTopic(sess.ID)) {
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

	if err := cli.Send(ctx, sess.ID, api.SendRequest{Content: "hello from root"}); err != nil {
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
	if !ws.SandboxMode.IsValid() {
		t.Errorf("ws.SandboxMode = %q, want native or local", ws.SandboxMode)
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
	if m1.ID != "fake/custom-model" || m1.Provider != "fake" || m1.Name != "custom-model" || m1.Label != "custom-model" {
		t.Errorf("m1 = %+v, want qualified custom-model", m1)
	}

	m2, err := cli.GetModel(ctx, "ollama/qwen3.8")
	if err != nil {
		t.Fatalf("GetModel('ollama/qwen3.8'): %v", err)
	}
	if m2.ID != "ollama/qwen3.8" || m2.Provider != "ollama" || m2.Name != "qwen3.8" {
		t.Errorf("m2 = %+v, want qualified model", m2)
	}
}

func TestWorkspacePermissionsEndpoints(t *testing.T) {
	ctx := context.Background()
	_, cli, _ := setupDaemonTest(t, nil)

	// 1. Initial Permissions (empty)
	perms, err := cli.Permissions(ctx)
	if err != nil {
		t.Fatalf("Permissions: %v", err)
	}
	if len(perms.Project.Allow) != 0 || len(perms.Global.Allow) != 0 {
		t.Fatalf("expected empty initial permissions, got %+v", perms)
	}

	// 2. Add Project Allow Rule
	addResp, err := cli.AddPermissionRule(ctx, api.PermissionRuleRequest{
		Scope:  api.ScopeProject,
		Effect: api.EffectAllow,
		Rule:   "bash(go *)",
	})
	if err != nil {
		t.Fatalf("AddPermissionRule (project allow): %v", err)
	}
	if !slices.Contains(addResp.Project.Allow, "bash(go *)") {
		t.Fatalf("expected bash(go *) in project allow, got %v", addResp.Project.Allow)
	}

	// 3. Add Global Deny Rule
	addGlobResp, err := cli.AddPermissionRule(ctx, api.PermissionRuleRequest{
		Scope:  api.ScopeGlobal,
		Effect: api.EffectDeny,
		Rule:   "bash(rm *)",
	})
	if err != nil {
		t.Fatalf("AddPermissionRule (global deny): %v", err)
	}
	if !slices.Contains(addGlobResp.Global.Deny, "bash(rm *)") {
		t.Fatalf("expected bash(rm *) in global deny, got %v", addGlobResp.Global.Deny)
	}

	// 4. Verify Permissions endpoint reflects both
	pResp, err := cli.Permissions(ctx)
	if err != nil {
		t.Fatalf("Permissions after add: %v", err)
	}
	if !slices.Contains(pResp.Project.Allow, "bash(go *)") {
		t.Errorf("expected bash(go *) in project allow, got %v", pResp.Project.Allow)
	}
	if !slices.Contains(pResp.Global.Deny, "bash(rm *)") {
		t.Errorf("expected bash(rm *) in global deny, got %v", pResp.Global.Deny)
	}

	// 5. Remove Project Rule
	delResp, err := cli.RemovePermissionRule(ctx, api.PermissionRuleRequest{
		Scope:  api.ScopeProject,
		Effect: api.EffectAllow,
		Rule:   "bash(go *)",
	})
	if err != nil {
		t.Fatalf("RemovePermissionRule: %v", err)
	}
	if slices.Contains(delResp.Project.Allow, "bash(go *)") {
		t.Errorf("expected bash(go *) removed from project allow, got %v", delResp.Project.Allow)
	}

	// 6. Validation errors (invalid rule, invalid scope, invalid effect)
	var apiErr *api.APIError
	if _, err := cli.AddPermissionRule(ctx, api.PermissionRuleRequest{
		Scope:  api.ScopeProject,
		Effect: api.EffectAllow,
		Rule:   "invalid-rule-format",
	}); !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for invalid rule format, got %v", err)
	}

	if _, err := cli.AddPermissionRule(ctx, api.PermissionRuleRequest{
		Scope:  "unknown-scope",
		Effect: api.EffectAllow,
		Rule:   "bash(*)",
	}); !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for invalid scope, got %v", err)
	}

	if _, err := cli.AddPermissionRule(ctx, api.PermissionRuleRequest{
		Scope:  api.ScopeProject,
		Effect: "unknown-effect",
		Rule:   "bash(*)",
	}); !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for invalid effect, got %v", err)
	}
}

func TestWorkspaceSandboxEndpoints(t *testing.T) {
	ctx := context.Background()
	_, cli, _ := setupDaemonTest(t, nil)

	// 1. Initial sandbox posture: empty scopes over the built-in defaults.
	resp, err := cli.GetSandbox(ctx)
	if err != nil {
		t.Fatalf("GetSandbox: %v", err)
	}
	if resp.Global.Mode != "" || resp.Project.Mode != "" {
		t.Errorf("expected empty stored scopes, got global=%+v project=%+v", resp.Global, resp.Project)
	}
	if resp.Effective.AllowUnsandboxed == nil || !*resp.Effective.AllowUnsandboxed {
		t.Errorf("expected allowUnsandboxed true in trusted workspace effective settings, got %+v", resp.Effective)
	}
	if resp.ResolvedPaths.WorkDir == "" {
		t.Errorf("expected resolved workDir, got %+v", resp.ResolvedPaths)
	}
	if want := sandbox.Availability(); resp.Availability != want {
		t.Errorf("availability = %+v, want host probe %+v", resp.Availability, want)
	}
	if resp.Restricted {
		t.Errorf("expected unrestricted workspace posture, got restricted=true")
	}

	// 2. PUT project scope persists settings and reflects them in the response.
	putResp, err := cli.UpdateSandbox(ctx, api.UpdateSandboxRequest{
		Scope: api.ScopeProject,
		Settings: api.SandboxSettings{
			Mode:             api.SandboxLocal,
			ExcludedCommands: []string{"git commit *"},
		},
	})
	if err != nil {
		t.Fatalf("UpdateSandbox (project): %v", err)
	}
	if putResp.Project.Mode != api.SandboxLocal || !slices.Contains(putResp.Project.ExcludedCommands, "git commit *") {
		t.Errorf("project scope = %+v, want mode local with git commit exclusion", putResp.Project)
	}
	if putResp.Effective.Mode != api.SandboxLocal {
		t.Errorf("effective mode = %q, want local", putResp.Effective.Mode)
	}

	// 3. PUT global scope locks allowUnsandboxed off; the project scope can no
	//    longer widen it back (monotonic lock).
	lockedOff := false
	relaxedOn := true
	if _, err := cli.UpdateSandbox(ctx, api.UpdateSandboxRequest{
		Scope:    api.ScopeGlobal,
		Settings: api.SandboxSettings{AllowUnsandboxed: &lockedOff},
	}); err != nil {
		t.Fatalf("UpdateSandbox (global lock): %v", err)
	}
	if _, err := cli.UpdateSandbox(ctx, api.UpdateSandboxRequest{
		Scope:    api.ScopeProject,
		Settings: api.SandboxSettings{Mode: api.SandboxLocal, AllowUnsandboxed: &relaxedOn},
	}); err != nil {
		t.Fatalf("UpdateSandbox (project relax attempt): %v", err)
	}
	locked, err := cli.GetSandbox(ctx)
	if err != nil {
		t.Fatalf("GetSandbox after lock: %v", err)
	}
	if locked.Effective.AllowUnsandboxed == nil || *locked.Effective.AllowUnsandboxed {
		t.Errorf("expected global allowUnsandboxed:false lock to hold, got %+v", locked.Effective)
	}
	if len(locked.Effective.ExcludedCommands) != 0 {
		t.Errorf("expected exclusions disabled under allowUnsandboxed lock, got %v", locked.Effective.ExcludedCommands)
	}

	// 4. Validation errors surface as 400 Bad Request.
	var apiErr *api.APIError
	if _, err := cli.UpdateSandbox(ctx, api.UpdateSandboxRequest{
		Scope: "unknown-scope",
	}); !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid scope, got %v", err)
	}

	if _, err := cli.UpdateSandbox(ctx, api.UpdateSandboxRequest{
		Scope:    api.ScopeProject,
		Settings: api.SandboxSettings{Mode: "docker"},
	}); !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid mode, got %v", err)
	}

	if _, err := cli.UpdateSandbox(ctx, api.UpdateSandboxRequest{
		Scope: api.ScopeGlobal,
		Settings: api.SandboxSettings{
			Filesystem: api.SandboxFilesystemSettings{AllowWrite: []string{"relative/path"}},
		},
	}); !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for relative path in global scope, got %v", err)
	}

	if _, err := cli.UpdateSandbox(ctx, api.UpdateSandboxRequest{
		Scope: api.ScopeProject,
		Settings: api.SandboxSettings{
			Network: api.SandboxNetworkSettings{AllowedDomains: []string{"not a domain/"}},
		},
	}); !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid domain pattern, got %v", err)
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
  defaultModels:
    completion: qwen3.8
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
    - unavailable-provider/missing
    - my-ollama/qwen3.8
  thinking:
    type: effort
    effort: medium
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
	preview, err := wsCLI.PreviewSession(ctx, api.PreviewSessionRequest{Config: api.SessionConfig{Agent: "custom-agent"}})
	if err != nil || preview == nil || preview.Model == nil || preview.Model.ID != "my-ollama/qwen3.8" || preview.Model.ThinkingDefault == nil || preview.Model.ThinkingDefault.Effort != "medium" {
		t.Fatalf("draft preview = %+v, err = %v", preview, err)
	}
	if ws.DefaultModel == nil || ws.DefaultModel.ID != preview.Model.ID {
		t.Fatalf("workspace default = %+v, preview = %+v", ws.DefaultModel, preview)
	}
	explicit, err := wsCLI.PreviewSession(ctx, api.PreviewSessionRequest{Config: api.SessionConfig{Agent: "custom-agent", Model: "my-ollama/other-model"}})
	if err != nil || explicit == nil || explicit.Model == nil || explicit.Model.ID != "my-ollama/other-model" {
		t.Fatalf("explicit preview = %+v, err = %v", explicit, err)
	}
	created, err := wsCLI.CreateSession(ctx, api.CreateSessionRequest{Config: api.SessionConfig{Agent: "custom-agent"}})
	if err != nil || created.Model == nil || created.Model.ID != preview.Model.ID {
		t.Fatalf("created session = %+v, err = %v", created, err)
	}

	ag, err := wsCLI.GetAgent(ctx, "custom-agent")
	if err != nil {
		t.Fatalf("GetAgent('custom-agent'): %v", err)
	}
	if ag.Name != "custom-agent" || ag.DisplayName != "Custom Agent" || ag.Thinking == nil || ag.Thinking.Effort != "medium" {
		t.Errorf("ag = %+v, want Effort='medium'", ag)
	}

	mdl, err := wsCLI.GetModel(ctx, "my-ollama/qwen3.8")
	if err != nil {
		t.Fatalf("GetModel('my-ollama/qwen3.8'): %v", err)
	}
	if mdl.ID != "my-ollama/qwen3.8" || mdl.Provider != "my-ollama" || mdl.Name != "qwen3.8" || mdl.Label != "Qwen 3.8" {
		t.Errorf("mdl = %+v, want Qwen 3.8", mdl)
	}
	if len(mdl.Thinking) != 1 || mdl.Thinking[0].Default != "high" || len(mdl.Thinking[0].Values) != 3 {
		t.Fatalf("unexpected model thinking: %+v", mdl.Thinking)
	}
	if mdl.Thinking[0].Values[0].Value != "low" || mdl.Thinking[0].Values[0].Description == "" {
		t.Fatalf("missing effort description: %+v", mdl.Thinking[0].Values)
	}

	models, err := wsCLI.ListModels(ctx)
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	var foundOllama bool
	for _, m := range models {
		if m.ID == "my-ollama/qwen3.8" {
			foundOllama = true
			if len(m.Thinking) != 1 || m.Thinking[0].Default != "high" || m.Label != "Qwen 3.8" || !reflect.DeepEqual(m.Thinking, mdl.Thinking) {
				t.Errorf("listed model mismatch: %+v", m)
			}
			break
		}
	}
	if !foundOllama {
		t.Errorf("expected 'my-ollama/qwen3.8' in ListModels: %+v", models)
	}
}

func TestDaemonModelProfileEnrichment(t *testing.T) {
	ctx := context.Background()
	prov := &fakeProvider{
		profiles: map[string]profile.Profile{
			"fake-prov/enriched-model": {
				ID:   "enriched-model",
				Name: "Enriched Model Name",
				Limits: profile.Limits{
					Context: 128000,
					Output:  16384,
				},
			},
			"enriched-model": {
				ID:   "enriched-model",
				Name: "Enriched Model Name",
				Limits: profile.Limits{
					Context: 128000,
					Output:  16384,
				},
			},
		},
	}
	_, cli, wsDir := setupDaemonTest(t, prov)

	provDir := filepath.Join(wsDir, ".agents", "providers")
	if err := os.MkdirAll(provDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	provYAML := `apiVersion: warp/v1alpha1
kind: ModelProvider
metadata:
  name: fake-prov
spec:
  type: fake
  models:
    - id: enriched-model
`
	if err := os.WriteFile(filepath.Join(provDir, "fake.yaml"), []byte(provYAML), 0o644); err != nil {
		t.Fatalf("WriteFile fake.yaml: %v", err)
	}

	wsCLI := cli.WithCWD(wsDir)

	// Test ListModels enrichment
	models, err := wsCLI.ListModels(ctx)
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	var found bool
	for _, m := range models {
		if m.ID == "fake-prov/enriched-model" {
			found = true
			if m.Name != "Enriched Model Name" {
				t.Errorf("m.Name = %q, want 'Enriched Model Name'", m.Name)
			}
			if m.Label != "Enriched Model Name" {
				t.Errorf("m.Label = %q, want 'Enriched Model Name'", m.Label)
			}
			if m.ContextLimit != 128000 || m.OutputLimit != 16384 {
				t.Errorf("m limits = Context:%d Output:%d, want 128000/16384", m.ContextLimit, m.OutputLimit)
			}
			break
		}
	}
	if !found {
		t.Fatalf("expected 'fake-prov/enriched-model' in ListModels: %+v", models)
	}

	// Test GetModel enrichment with qualified ID
	m1, err := wsCLI.GetModel(ctx, "fake-prov/enriched-model")
	if err != nil {
		t.Fatalf("GetModel('fake-prov/enriched-model'): %v", err)
	}
	if m1.Name != "Enriched Model Name" || m1.Label != "Enriched Model Name" {
		t.Errorf("m1 Name/Label = %q/%q, want 'Enriched Model Name'", m1.Name, m1.Label)
	}
	if m1.ContextLimit != 128000 || m1.OutputLimit != 16384 {
		t.Errorf("m1 limits = Context:%d Output:%d, want 128000/16384", m1.ContextLimit, m1.OutputLimit)
	}

	// Test GetModel enrichment with unlisted / bare model
	m2, err := wsCLI.GetModel(ctx, "enriched-model")
	if err != nil {
		t.Fatalf("GetModel('enriched-model'): %v", err)
	}
	if m2.Name != "Enriched Model Name" || m2.Label != "Enriched Model Name" {
		t.Errorf("m2 Name/Label = %q/%q, want 'Enriched Model Name'", m2.Name, m2.Label)
	}
	if m2.ContextLimit != 128000 || m2.OutputLimit != 16384 {
		t.Errorf("m2 limits = Context:%d Output:%d, want 128000/16384", m2.ContextLimit, m2.OutputLimit)
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
	defer checkClose(t, resp.Body)
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
	defer checkClose(t, qResp.Body)
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
	defer checkClose(t, eResp.Body)
	if eResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("response status with empty workspace = %d, want 400", eResp.StatusCode)
	}
}

func TestSubprojectPreservesCWDAndLocalConfigs(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()

	var (
		mu             sync.Mutex
		seenSubPrompts []string
		step           int
	)
	provider := &fakeProvider{
		chunkHandler: func(_ context.Context, req *llm.Request) (llm.StreamResponse, error) {
			if len(req.Messages) > 0 {
				sysText := req.Messages[0].GetContent().Text()
				if !strings.Contains(sysText, "title-generator") {
					mu.Lock()
					seenSubPrompts = append(seenSubPrompts, sysText)
					step++
					curStep := step
					mu.Unlock()

					var content message.Content
					switch curStep {
					case 1:
						content = message.Content{&message.ToolCall{
							ID:   "bash-pwd",
							Name: "bash",
							Args: map[string]any{"command": "pwd"},
						}}
					case 2:
						content = message.Content{&message.ToolCall{
							ID:   "view-sub",
							Name: "view",
							Args: map[string]any{"path": "sub.txt"},
						}}
					default:
						content = message.Content{&message.TextBlock{Text: "Done."}}
					}
					return func(yield func(message.AssistantChunk, error) bool) {
						yield(message.AssistantChunk{
							BaseChunk: message.BaseChunk{ID: uuid.NewString()},
							Model:     req.Model,
							Content:   content,
							Done:      true,
						}, nil)
					}, nil
				}
			}
			return func(yield func(message.AssistantChunk, error) bool) {
				yield(message.AssistantChunk{
					BaseChunk: message.BaseChunk{ID: uuid.NewString()},
					Model:     req.Model,
					Content:   message.Content{&message.TextBlock{Text: "Title"}},
					Done:      true,
				}, nil)
			}, nil
		},
	}

	_, cli, wsDir := setupDaemonTest(t, provider)

	// Workspace root configuration
	wsManifest := `---
apiVersion: warp/v1alpha1
kind: Workspace
metadata:
  name: mono-repo
spec:
  projects: [".", "packages/sub", "packages/other"]
---
Root workspace.
`
	if err := os.WriteFile(filepath.Join(wsDir, "WORKSPACE.md"), []byte(wsManifest), 0o644); err != nil {
		t.Fatalf("WriteFile WORKSPACE.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wsDir, "AGENTS.md"), []byte("Root AGENTS instructions."), 0o644); err != nil {
		t.Fatalf("WriteFile root AGENTS.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wsDir, "root-only.txt"), []byte("from-root\n"), 0o644); err != nil {
		t.Fatalf("WriteFile root-only.txt: %v", err)
	}

	rootAgentsDir := filepath.Join(wsDir, ".agents")
	rootProvDir := filepath.Join(rootAgentsDir, "providers")
	if err := os.MkdirAll(rootProvDir, 0o755); err != nil {
		t.Fatalf("MkdirAll rootProvDir: %v", err)
	}
	provYAML := `apiVersion: warp/v1alpha1
kind: ModelProvider
metadata:
  name: fake
spec:
  type: ollama
  defaultModels:
    completion: root-model
  models:
    - id: root-model
      capabilities:
        completion: true
        thinking:
          - type: effort
            default: high
            allowedEfforts: ["low", "high"]
    - id: sub-model
      capabilities:
        completion: true
        thinking:
          - type: effort
            default: high
            allowedEfforts: ["low", "high"]
`
	if err := os.WriteFile(filepath.Join(rootProvDir, "fake.yaml"), []byte(provYAML), 0o644); err != nil {
		t.Fatalf("WriteFile fake.yaml: %v", err)
	}

	rootAgentMD := `---
apiVersion: warp/v1alpha1
kind: Agent
metadata:
  name: main
  description: "Root agent"
spec:
  models: ["fake/root-model"]
  thinking:
    type: effort
    effort: high
---
Root agent instructions.
{{if .Context}}<project_context path="{{.Context.Path}}">{{.Context.Instructions}}</project_context>{{end}}
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
	if err := workspace.TrustFolder(subDir, api.TrustScopeFolder); err != nil {
		t.Fatalf("TrustFolder subDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "AGENTS.md"), []byte("Subproject AGENTS instructions."), 0o644); err != nil {
		t.Fatalf("WriteFile sub AGENTS.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "sub.txt"), []byte("from-subproject\n"), 0o644); err != nil {
		t.Fatalf("WriteFile sub.txt: %v", err)
	}
	subAgentMD := `---
apiVersion: warp/v1alpha1
kind: Agent
metadata:
  name: main
  description: "Subproject agent"
spec:
  tools: ["bash", "view"]
  models: ["fake/sub-model"]
  thinking:
    type: effort
    effort: low
---
Subproject agent instructions.
{{if .Context}}<project_context path="{{.Context.Path}}">{{.Context.Instructions}}</project_context>{{end}}
`
	if err := os.WriteFile(filepath.Join(subAgentsDir, "main.agent.md"), []byte(subAgentMD), 0o644); err != nil {
		t.Fatalf("WriteFile sub main.agent.md: %v", err)
	}

	otherDir := filepath.Join(wsDir, "packages", "other")
	if err := os.MkdirAll(otherDir, 0o755); err != nil {
		t.Fatalf("MkdirAll otherDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(otherDir, "other.txt"), []byte("from-other\n"), 0o644); err != nil {
		t.Fatalf("WriteFile other.txt: %v", err)
	}

	rootCLI := cli.WithCWD(wsDir)
	subCLI := cli.WithCWD(subDir)
	otherCLI := cli.WithCWD(otherDir)

	// Open rootCLI FIRST and verify root workspace & session
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
	if rootAgent.Thinking == nil || rootAgent.Thinking.Effort != "high" || rootAgent.Description != "Root agent" {
		t.Errorf("rootAgent = %+v, want Effort='high' and Description='Root agent'", rootAgent)
	}
	rootSess, err := rootCLI.CreateSession(ctx, api.CreateSessionRequest{Title: "root-session"})
	if err != nil {
		t.Fatalf("rootCLI.CreateSession: %v", err)
	}
	if rootSess.WorkDir != wsDir {
		t.Errorf("rootSess.WorkDir = %q, want %q", rootSess.WorkDir, wsDir)
	}
	if rootSess.Config.Model != "fake/root-model" {
		t.Errorf("rootSess.Config.Model = %q, want fake/root-model", rootSess.Config.Model)
	}

	// Verify Subproject preserves subDir CWD, local agent/model overrides, and WorkDir
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
	if subWS.DefaultModel == nil || subWS.DefaultModel.ID != "fake/sub-model" || subWS.DefaultModel.ThinkingDefault == nil || subWS.DefaultModel.ThinkingDefault.Effort != "low" {
		t.Errorf("subWS.DefaultModel = %+v, want fake/sub-model with Effort='low'", subWS.DefaultModel)
	}
	subAgent, err := subCLI.GetAgent(ctx, "main")
	if err != nil {
		t.Fatalf("subCLI.GetAgent('main'): %v", err)
	}
	if subAgent.Thinking == nil || subAgent.Thinking.Effort != "low" || subAgent.Description != "Subproject agent" {
		t.Errorf("subAgent = %+v, want Effort='low' and Description='Subproject agent'", subAgent)
	}

	subPreview, err := subCLI.PreviewSession(ctx, api.PreviewSessionRequest{})
	if err != nil {
		t.Fatalf("subCLI.PreviewSession: %v", err)
	}
	if subPreview.Config.Model != "fake/sub-model" {
		t.Errorf("subPreview.Config.Model = %q, want fake/sub-model", subPreview.Config.Model)
	}
	if subPreview.Model == nil || subPreview.Model.ThinkingDefault == nil || subPreview.Model.ThinkingDefault.Effort != "low" {
		t.Errorf("subPreview.Model = %+v, want ThinkingDefault.Effort='low'", subPreview.Model)
	}

	subSess, err := subCLI.CreateSession(ctx, api.CreateSessionRequest{
		Title:  "sub-session",
		Config: api.SessionConfig{Mode: api.ModeAuto},
	})
	if err != nil {
		t.Fatalf("subCLI.CreateSession: %v", err)
	}
	if subSess.WorkDir != subDir {
		t.Fatalf("subSess.WorkDir = %q, want %q", subSess.WorkDir, subDir)
	}
	if subSess.Config.Model != "fake/sub-model" {
		t.Errorf("subSess.Config.Model = %q, want fake/sub-model", subSess.Config.Model)
	}
	if subSess.Model == nil || subSess.Model.ThinkingDefault == nil || subSess.Model.ThinkingDefault.Effort != "low" {
		t.Errorf("subSess.Model = %+v, want ThinkingDefault.Effort='low'", subSess.Model)
	}

	// Execute bash (pwd) and view (sub.txt) in subSess
	ready := make(chan struct{})
	doneTurn := make(chan struct{})
	var (
		turnErr     error
		toolOutputs = make(map[string]api.Message)
	)
	go func() {
		for event, err := range subCLI.EventsWithReady(ctx, "", "", func(string) { close(ready) }, api.SessionStreamTopic(subSess.ID)) {
			if err != nil {
				return
			}
			switch event.Type {
			case api.EventError:
				if ev, ok := event.Payload.(api.ErrorEvent); ok {
					turnErr = errors.New(ev.Error)
				}
				close(doneTurn)
				return
			case api.EventEntryAdded:
				if ev, ok := event.Payload.(api.EntryAddedEvent); ok && ev.Entry.Message != nil && ev.Entry.Message.Role == api.RoleTool {
					toolOutputs[ev.Entry.Message.ToolCallID] = *ev.Entry.Message
				}
			case api.EventCompleted:
				close(doneTurn)
				return
			}
		}
	}()
	select {
	case <-ready:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := subCLI.Send(ctx, subSess.ID, api.SendRequest{Content: "Run pwd and read sub.txt"}); err != nil {
		t.Fatalf("subCLI.Send: %v", err)
	}
	select {
	case <-doneTurn:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if turnErr != nil {
		t.Fatalf("subSess turn error: %v", turnErr)
	}

	mu.Lock()
	if len(seenSubPrompts) == 0 {
		mu.Unlock()
		t.Fatal("expected subproject turn to invoke provider")
	}
	sysPrompt := seenSubPrompts[0]
	mu.Unlock()
	if !strings.Contains(sysPrompt, "Subproject agent instructions.") || !strings.Contains(sysPrompt, "Subproject AGENTS instructions.") {
		t.Errorf("subSess system prompt missing subproject agent/AGENTS.md: %q", sysPrompt)
	}
	if strings.Contains(sysPrompt, "Root agent instructions.") || strings.Contains(sysPrompt, "Root AGENTS instructions.") {
		t.Errorf("subSess system prompt leaked root agent/AGENTS.md: %q", sysPrompt)
	}

	bashMsg, ok := toolOutputs["bash-pwd"]
	if !ok || bashMsg.IsError {
		t.Fatalf("bash-pwd result = %+v (ok=%v)", bashMsg, ok)
	}
	var bashOut api.BashOutput
	if err := json.Unmarshal(bashMsg.StructuredContent, &bashOut); err != nil {
		t.Fatalf("decode bash output: %v", err)
	}
	if strings.TrimSpace(bashOut.Output) != subDir {
		t.Errorf("bash pwd = %q, want %q", strings.TrimSpace(bashOut.Output), subDir)
	}

	viewMsg, ok := toolOutputs["view-sub"]
	if !ok || viewMsg.IsError {
		t.Fatalf("view-sub result = %+v (ok=%v)", viewMsg, ok)
	}
	var viewOut api.ViewOutput
	if err := json.Unmarshal(viewMsg.StructuredContent, &viewOut); err != nil {
		t.Fatalf("decode view output: %v", err)
	}
	if !strings.Contains(viewOut.Content, "from-subproject") {
		t.Errorf("view sub.txt content = %q, want 'from-subproject'", viewOut.Content)
	}

	// Verify file indexing scopes to subDir (with and without SessionID)
	for _, sessID := range []string{"", subSess.ID} {
		files, err := subCLI.ListWorkspaceFiles(ctx, api.ListFilesRequest{SessionID: sessID, Limit: 50})
		if err != nil {
			t.Fatalf("subCLI.ListWorkspaceFiles(sessionID=%q): %v", sessID, err)
		}
		var sawSub, sawRoot, sawOther bool
		for _, f := range files {
			switch f.Path {
			case "sub.txt":
				sawSub = true
			case "root-only.txt":
				sawRoot = true
			case "other.txt":
				sawOther = true
			}
		}
		if !sawSub || sawRoot || sawOther {
			t.Errorf("subCLI.ListWorkspaceFiles(sessionID=%q) = %+v (sawSub=%v, sawRoot=%v, sawOther=%v)", sessID, files, sawSub, sawRoot, sawOther)
		}
	}

	// Verify LatestSession scopes per WorkDir (-c / --continue)
	latestRoot, err := rootCLI.LatestSession(ctx)
	if err != nil {
		t.Fatalf("rootCLI.LatestSession: %v", err)
	}
	if latestRoot == nil || latestRoot.ID != rootSess.ID {
		t.Errorf("rootCLI.LatestSession = %+v, want %q", latestRoot, rootSess.ID)
	}
	latestSub, err := subCLI.LatestSession(ctx)
	if err != nil {
		t.Fatalf("subCLI.LatestSession: %v", err)
	}
	if latestSub == nil || latestSub.ID != subSess.ID {
		t.Errorf("subCLI.LatestSession = %+v, want %q", latestSub, subSess.ID)
	}
	latestOther, err := otherCLI.LatestSession(ctx)
	if err != nil {
		t.Fatalf("otherCLI.LatestSession: %v", err)
	}
	if latestOther != nil {
		t.Errorf("otherCLI.LatestSession = %+v, want nil when subproject has no sessions", latestOther)
	}

	// Deleting a subproject session must never delete subDir on disk.
	disposable, err := subCLI.CreateSession(ctx, api.CreateSessionRequest{Title: "disposable"})
	if err != nil {
		t.Fatalf("CreateSession disposable: %v", err)
	}
	if err := subCLI.DeleteSession(ctx, disposable.ID); err != nil {
		t.Fatalf("DeleteSession disposable: %v", err)
	}
	if _, err := os.Stat(filepath.Join(subDir, "sub.txt")); err != nil {
		t.Fatalf("DeleteSession removed subproject files: %v", err)
	}
}

func TestSubprojectSessionRestoreFromSQLite(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()

	testutils.SetupXDG(t, "tasksmith-subproject-restore-test")

	wsDir := shortTempDir(t)
	subDir := filepath.Join(wsDir, "packages", "sub")
	if err := os.MkdirAll(filepath.Join(wsDir, ".agents", "providers"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(subDir, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}

	wsManifest := "---\napiVersion: warp/v1alpha1\nkind: Workspace\nmetadata:\n  name: restore-mono\nspec:\n  projects: [\".\", \"packages/sub\"]\n---\n"
	if err := os.WriteFile(filepath.Join(wsDir, "WORKSPACE.md"), []byte(wsManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	provYAML := `apiVersion: warp/v1alpha1
kind: ModelProvider
metadata:
  name: fake
spec:
  type: ollama
  defaultModels:
    completion: root-model
  models:
    - id: root-model
      capabilities:
        completion: true
        thinking:
          - type: effort
            default: high
            allowedEfforts: ["low", "high"]
    - id: sub-model
      capabilities:
        completion: true
        thinking:
          - type: effort
            default: high
            allowedEfforts: ["low", "high"]
`
	if err := os.WriteFile(filepath.Join(wsDir, ".agents", "providers", "fake.yaml"), []byte(provYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	rootAgentMD := "---\napiVersion: warp/v1alpha1\nkind: Agent\nmetadata:\n  name: main\nspec:\n  models: [\"fake/root-model\"]\n  thinking:\n    type: effort\n    effort: high\n---\nRoot agent.\n{{if .Context}}<project_context path=\"{{.Context.Path}}\">{{.Context.Instructions}}</project_context>{{end}}\n"
	if err := os.WriteFile(filepath.Join(wsDir, ".agents", "main.agent.md"), []byte(rootAgentMD), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wsDir, "AGENTS.md"), []byte("Root context."), 0o644); err != nil {
		t.Fatal(err)
	}

	subAgentMD := "---\napiVersion: warp/v1alpha1\nkind: Agent\nmetadata:\n  name: main\nspec:\n  tools: [\"bash\"]\n  models: [\"fake/sub-model\"]\n  thinking:\n    type: effort\n    effort: low\n---\nSubagent restored.\n{{if .Context}}<project_context path=\"{{.Context.Path}}\">{{.Context.Instructions}}</project_context>{{end}}\n"
	if err := os.WriteFile(filepath.Join(subDir, ".agents", "main.agent.md"), []byte(subAgentMD), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "AGENTS.md"), []byte("Subproject restored context."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, ".env"), []byte("SUBPROJECT_RESTORE_ENV=sub-env-ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := workspace.TrustFolder(wsDir, api.TrustScopeFolder); err != nil {
		t.Fatal(err)
	}
	if err := workspace.TrustFolder(subDir, api.TrustScopeFolder); err != nil {
		t.Fatal(err)
	}

	startDaemon := func(prov *fakeProvider) (*Daemon, *client.Client) {
		runDir := shortTempDir(t)
		srv, err := New(Config{
			SocketPath:            filepath.Join(runDir, "b.sock"),
			LockPath:              filepath.Join(runDir, "b.lock"),
			GatewayPool:           newFakeGatewayPool(prov),
			Logger:                log.Default(),
			DisableMCPAutoConnect: true,
		})
		if err != nil {
			t.Fatalf("daemon.New: %v", err)
		}
		if err := srv.Start(ctx); err != nil {
			t.Fatalf("srv.Start: %v", err)
		}
		c, err := client.New(srv.SocketPath(), client.WithCWD(wsDir))
		if err != nil {
			_ = srv.Close(ctx)
			t.Fatalf("client.New: %v", err)
		}
		return srv, c
	}

	srv1, cli1 := startDaemon(&fakeProvider{})
	rootSess, err := cli1.WithCWD(wsDir).CreateSession(ctx, api.CreateSessionRequest{Title: "root"})
	if err != nil {
		t.Fatalf("cli1 root CreateSession: %v", err)
	}
	subSess, err := cli1.WithCWD(subDir).CreateSession(ctx, api.CreateSessionRequest{
		Title:  "sub",
		Config: api.SessionConfig{Mode: api.ModeAuto},
	})
	if err != nil {
		t.Fatalf("cli1 sub CreateSession: %v", err)
	}
	if err := srv1.Close(ctx); err != nil {
		t.Fatalf("srv1.Close: %v", err)
	}

	var (
		mu             sync.Mutex
		restoredPrompt string
		step           int
	)
	prov2 := &fakeProvider{
		chunkHandler: func(_ context.Context, req *llm.Request) (llm.StreamResponse, error) {
			if len(req.Messages) > 0 {
				sysText := req.Messages[0].GetContent().Text()
				if !strings.Contains(sysText, "title-generator") {
					mu.Lock()
					restoredPrompt = sysText
					step++
					cur := step
					mu.Unlock()

					var content message.Content
					if cur == 1 {
						content = message.Content{&message.ToolCall{
							ID:   "bash-env",
							Name: "bash",
							Args: map[string]any{"command": "pwd && printf '%s' \"$SUBPROJECT_RESTORE_ENV\""},
						}}
					} else {
						content = message.Content{&message.TextBlock{Text: "Restored."}}
					}
					return func(yield func(message.AssistantChunk, error) bool) {
						yield(message.AssistantChunk{
							BaseChunk: message.BaseChunk{ID: uuid.NewString()},
							Model:     req.Model,
							Content:   content,
							Done:      true,
						}, nil)
					}, nil
				}
			}
			return func(yield func(message.AssistantChunk, error) bool) {
				yield(message.AssistantChunk{
					BaseChunk: message.BaseChunk{ID: uuid.NewString()},
					Model:     req.Model,
					Content:   message.Content{&message.TextBlock{Text: "Title"}},
					Done:      true,
				}, nil)
			}, nil
		},
	}

	srv2, cli2 := startDaemon(prov2)
	t.Cleanup(func() { _ = srv2.Close(context.Background()) })

	rootCLI2 := cli2.WithCWD(wsDir)
	subCLI2 := cli2.WithCWD(subDir)

	// Open root workspace FIRST in srv2 so owner is rootDir, then restore subSess via SQLite.
	if _, err := rootCLI2.GetWorkspace(ctx); err != nil {
		t.Fatalf("rootCLI2.GetWorkspace: %v", err)
	}

	restored, err := rootCLI2.GetSession(ctx, subSess.ID)
	if err != nil {
		t.Fatalf("rootCLI2.GetSession(subSess): %v", err)
	}
	if restored.WorkDir != subDir {
		t.Errorf("restored.WorkDir = %q, want %q", restored.WorkDir, subDir)
	}
	if restored.Config.Model != "fake/sub-model" {
		t.Errorf("restored.Config.Model = %q, want fake/sub-model", restored.Config.Model)
	}
	if restored.Model == nil || restored.Model.ThinkingDefault == nil || restored.Model.ThinkingDefault.Effort != "low" {
		t.Errorf("restored.Model = %+v, want ThinkingDefault.Effort='low'", restored.Model)
	}

	// Verify LatestSession scoping after cold restart
	if latestRoot, err := rootCLI2.LatestSession(ctx); err != nil || latestRoot == nil || latestRoot.ID != rootSess.ID {
		t.Errorf("rootCLI2.LatestSession = (%+v, %v), want %q", latestRoot, err, rootSess.ID)
	}
	if latestSub, err := subCLI2.LatestSession(ctx); err != nil || latestSub == nil || latestSub.ID != subSess.ID {
		t.Errorf("subCLI2.LatestSession = (%+v, %v), want %q", latestSub, err, subSess.ID)
	}

	// Verify restored session executes tools in subDir with subproject .env and AGENTS.md
	ready := make(chan struct{})
	doneTurn := make(chan struct{})
	var bashMsg api.Message
	var turnErr error
	go func() {
		for event, err := range subCLI2.EventsWithReady(ctx, "", "", func(string) { close(ready) }, api.SessionStreamTopic(subSess.ID)) {
			if err != nil {
				return
			}
			switch event.Type {
			case api.EventError:
				if ev, ok := event.Payload.(api.ErrorEvent); ok {
					turnErr = errors.New(ev.Error)
				}
				close(doneTurn)
				return
			case api.EventEntryAdded:
				if ev, ok := event.Payload.(api.EntryAddedEvent); ok && ev.Entry.Message != nil && ev.Entry.Message.Role == api.RoleTool {
					bashMsg = *ev.Entry.Message
				}
			case api.EventCompleted:
				close(doneTurn)
				return
			}
		}
	}()
	select {
	case <-ready:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := subCLI2.Send(ctx, subSess.ID, api.SendRequest{Content: "Verify restored subproject env and pwd"}); err != nil {
		t.Fatalf("subCLI2.Send: %v", err)
	}
	select {
	case <-doneTurn:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if turnErr != nil {
		t.Fatalf("restored turn error: %v", turnErr)
	}

	mu.Lock()
	gotPrompt := restoredPrompt
	mu.Unlock()
	if !strings.Contains(gotPrompt, "Subagent restored.") || !strings.Contains(gotPrompt, "Subproject restored context.") {
		t.Errorf("restored system prompt missing subproject agent/AGENTS.md: %q", gotPrompt)
	}
	if strings.Contains(gotPrompt, "Root agent.") || strings.Contains(gotPrompt, "Root context.") {
		t.Errorf("restored system prompt leaked root agent/AGENTS.md: %q", gotPrompt)
	}

	var bashOut api.BashOutput
	if err := json.Unmarshal(bashMsg.StructuredContent, &bashOut); err != nil {
		t.Fatalf("decode bash output: %v (msg=%+v)", err, bashMsg)
	}
	if !strings.Contains(bashOut.Output, subDir) || !strings.Contains(bashOut.Output, "sub-env-ok") {
		t.Errorf("restored bash output = %q, want %q and sub-env-ok", bashOut.Output, subDir)
	}
}

func TestSubprojectReceivesDiscoveredMCPToolsAndResources(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()

	mcpSrv := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "notes-server", Version: "1.0.0"}, nil)
	type echoIn struct {
		Text string `json:"text"`
	}
	type echoOut struct {
		Text string `json:"text"`
	}
	falseVal := false
	sdkmcp.AddTool(mcpSrv, &sdkmcp.Tool{
		Name:        "echo",
		Description: "Echo note",
		Annotations: &sdkmcp.ToolAnnotations{
			ReadOnlyHint:    true,
			DestructiveHint: &falseVal,
			OpenWorldHint:   &falseVal,
		},
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, in echoIn) (*sdkmcp.CallToolResult, echoOut, error) {
		return nil, echoOut{Text: "sub-echo:" + in.Text}, nil
	})
	mcpSrv.AddResource(&sdkmcp.Resource{URI: "file:///notes/readme.md", Name: "readme", MIMEType: "text/markdown"},
		func(_ context.Context, _ *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
			return &sdkmcp.ReadResourceResult{Contents: []*sdkmcp.ResourceContents{{
				URI:      "file:///notes/readme.md",
				MIMEType: "text/markdown",
				Text:     "hello mcp resource",
			}}}, nil
		})
	httpSrv := httptest.NewServer(sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return mcpSrv }, nil))
	defer httpSrv.Close()

	testutils.SetupXDG(t, "tasksmith-subproject-mcp-test")

	var (
		mu   sync.Mutex
		step int
	)
	prov := &fakeProvider{
		chunkHandler: func(_ context.Context, req *llm.Request) (llm.StreamResponse, error) {
			if len(req.Messages) > 0 && strings.Contains(req.Messages[0].GetContent().Text(), "title-generator") {
				return func(yield func(message.AssistantChunk, error) bool) {
					yield(message.AssistantChunk{
						BaseChunk: message.BaseChunk{ID: uuid.NewString()},
						Model:     req.Model,
						Content:   message.Content{&message.TextBlock{Text: "Title"}},
						Done:      true,
					}, nil)
				}, nil
			}
			mu.Lock()
			step++
			cur := step
			mu.Unlock()

			var content message.Content
			switch cur {
			case 1:
				content = message.Content{&message.ToolCall{
					ID:   "call-echo",
					Name: "notes__echo",
					Args: map[string]any{"text": "ping"},
				}}
			case 2:
				content = message.Content{&message.ToolCall{
					ID:   "call-res",
					Name: "read_mcp_resource",
					Args: map[string]any{"server": "notes", "uri": "file:///notes/readme.md"},
				}}
			default:
				content = message.Content{&message.TextBlock{Text: "MCP ok"}}
			}
			return func(yield func(message.AssistantChunk, error) bool) {
				yield(message.AssistantChunk{
					BaseChunk: message.BaseChunk{ID: uuid.NewString()},
					Model:     req.Model,
					Content:   content,
					Done:      true,
				}, nil)
			}, nil
		},
	}

	runDir := shortTempDir(t)
	srv, err := New(Config{
		SocketPath:  filepath.Join(runDir, "b.sock"),
		LockPath:    filepath.Join(runDir, "b.lock"),
		GatewayPool: newFakeGatewayPool(prov),
		Logger:      log.Default(),
	})
	if err != nil {
		t.Fatalf("daemon.New: %v", err)
	}
	if err := srv.Start(ctx); err != nil {
		t.Fatalf("srv.Start: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close(context.Background()) })

	wsDir := shortTempDir(t)
	subDir := filepath.Join(wsDir, "packages", "sub")
	if err := os.MkdirAll(filepath.Join(wsDir, ".agents", "mcps"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(subDir, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wsDir, "WORKSPACE.md"), []byte("---\napiVersion: warp/v1alpha1\nkind: Workspace\nmetadata:\n  name: mcp-mono\nspec:\n  projects: [\".\", \"packages/sub\"]\n  defaultProvider: fake\n  defaultModels:\n    completion: fake-default-model\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mcpManifest := fmt.Sprintf("---\napiVersion: warp/v1alpha1\nkind: MCP\nmetadata:\n  name: notes\nspec:\n  type: http\n  endpoint: %s\n---\n", httpSrv.URL)
	if err := os.WriteFile(filepath.Join(wsDir, ".agents", "mcps", "notes.mcp.yaml"), []byte(mcpManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "AGENTS.md"), []byte("subproject mcp"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := workspace.TrustFolder(wsDir, api.TrustScopeFolder); err != nil {
		t.Fatal(err)
	}
	if err := workspace.TrustFolder(subDir, api.TrustScopeFolder); err != nil {
		t.Fatal(err)
	}

	cli, err := client.New(srv.SocketPath(), client.WithCWD(wsDir))
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}
	rootCLI := cli.WithCWD(wsDir)
	subCLI := cli.WithCWD(subDir)

	// Open root FIRST and wait until root has discovered both the MCP tool and resource
	// BEFORE opening the subproject runtime.
	if _, err := rootCLI.GetWorkspace(ctx); err != nil {
		t.Fatalf("rootCLI.GetWorkspace: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		mcps, err := rootCLI.ListMCPs(ctx)
		if err != nil {
			t.Fatalf("rootCLI.ListMCPs: %v", err)
		}
		if len(mcps) == 1 && mcps[0].Status == api.MCPStatusConnected && len(mcps[0].Tools) > 0 && len(mcps[0].Resources) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for root MCP discovery: %+v", mcps)
		}
		time.Sleep(25 * time.Millisecond)
	}

	// Subproject opened AFTER discovery must immediately list notes__echo and read_mcp_resource.
	subTools, err := subCLI.ListTools(ctx, "main")
	if err != nil {
		t.Fatalf("subCLI.ListTools: %v", err)
	}
	hasTool := func(list []api.Tool, name string) bool {
		for _, tl := range list {
			if tl.Name == name && tl.Status != api.ToolStatusInactive && tl.Status != api.ToolStatusDenied {
				return true
			}
		}
		return false
	}
	if !hasTool(subTools, "notes__echo") || !hasTool(subTools, "read_mcp_resource") {
		t.Fatalf("subproject tools missing MCP tool/resource reader: %+v", subTools)
	}

	subSess, err := subCLI.CreateSession(ctx, api.CreateSessionRequest{
		Title:  "sub-mcp",
		Config: api.SessionConfig{Mode: api.ModeAuto},
	})
	if err != nil {
		t.Fatalf("subCLI.CreateSession: %v", err)
	}

	ready := make(chan struct{})
	doneTurn := make(chan struct{})
	var (
		turnErr     error
		toolResults = make(map[string]api.Message)
	)
	go func() {
		for event, err := range subCLI.EventsWithReady(ctx, "", "", func(string) { close(ready) }, api.SessionStreamTopic(subSess.ID)) {
			if err != nil {
				return
			}
			switch event.Type {
			case api.EventError:
				if ev, ok := event.Payload.(api.ErrorEvent); ok {
					turnErr = errors.New(ev.Error)
				}
				close(doneTurn)
				return
			case api.EventPermissionRequested:
				if ev, ok := event.Payload.(api.PermissionRequestedEvent); ok {
					turnErr = fmt.Errorf("unexpected permission request: %+v", ev.Permissions)
				}
				close(doneTurn)
				return
			case api.EventEntryAdded:
				if ev, ok := event.Payload.(api.EntryAddedEvent); ok && ev.Entry.Message != nil && ev.Entry.Message.Role == api.RoleTool {
					toolResults[ev.Entry.Message.ToolCallID] = *ev.Entry.Message
				}
			case api.EventCompleted:
				close(doneTurn)
				return
			}
		}
	}()
	select {
	case <-ready:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := subCLI.Send(ctx, subSess.ID, api.SendRequest{Content: "Call MCP tool and resource"}); err != nil {
		t.Fatalf("subCLI.Send: %v", err)
	}
	select {
	case <-doneTurn:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if turnErr != nil {
		t.Fatalf("subSess MCP turn error: %v", turnErr)
	}
	if msg := toolResults["call-echo"]; msg.IsError || !strings.Contains(fmt.Sprint(msg.Content), "sub-echo:ping") {
		t.Fatalf("notes__echo result = %+v", msg)
	}
	if msg := toolResults["call-res"]; msg.IsError || !strings.Contains(string(msg.StructuredContent), "hello mcp resource") {
		t.Fatalf("read_mcp_resource result = %+v (structured=%s)", msg, string(msg.StructuredContent))
	}

	// Reloading root must re-sync MCP tools onto the subproject workspace too.
	if err := rootCLI.ReloadWorkspace(ctx); err != nil {
		t.Fatalf("rootCLI.ReloadWorkspace: %v", err)
	}
	subToolsAfter, err := subCLI.ListTools(ctx, "main")
	if err != nil {
		t.Fatalf("subCLI.ListTools after reload: %v", err)
	}
	if !hasTool(subToolsAfter, "notes__echo") || !hasTool(subToolsAfter, "read_mcp_resource") {
		t.Fatalf("subproject tools after root reload missing MCP tool/resource reader: %+v", subToolsAfter)
	}
}

func TestSubprojectReloadPropagatesFromRootToLiveSessions(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()

	var (
		mu          sync.Mutex
		seenPrompts []string
	)
	prov := &fakeProvider{
		chunkHandler: func(_ context.Context, req *llm.Request) (llm.StreamResponse, error) {
			if len(req.Messages) > 0 {
				sys := req.Messages[0].GetContent().Text()
				if !strings.Contains(sys, "title-generator") {
					mu.Lock()
					seenPrompts = append(seenPrompts, sys)
					mu.Unlock()
				}
			}
			return func(yield func(message.AssistantChunk, error) bool) {
				yield(message.AssistantChunk{
					BaseChunk: message.BaseChunk{ID: uuid.NewString()},
					Model:     req.Model,
					Content:   message.Content{&message.TextBlock{Text: "ok"}},
					Done:      true,
				}, nil)
			}, nil
		},
	}

	_, cli, wsDir := setupDaemonTest(t, prov)
	subDir := filepath.Join(wsDir, "packages", "sub")
	if err := os.MkdirAll(filepath.Join(wsDir, ".agents", "providers"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(subDir, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := workspace.TrustFolder(subDir, api.TrustScopeFolder); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wsDir, "WORKSPACE.md"), []byte("---\napiVersion: warp/v1alpha1\nkind: Workspace\nmetadata:\n  name: reload-mono\nspec:\n  projects: [\".\", \"packages/sub\"]\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	provYAML := `apiVersion: warp/v1alpha1
kind: ModelProvider
metadata:
  name: fake
spec:
  type: ollama
  defaultModels:
    completion: m1
  models:
    - id: m1
      capabilities:
        completion: true
        thinking:
          - type: effort
            default: low
            allowedEfforts: ["low", "high"]
`
	if err := os.WriteFile(filepath.Join(wsDir, ".agents", "providers", "fake.yaml"), []byte(provYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	writeSubFiles := func(agentsText, effort string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(subDir, "AGENTS.md"), []byte(agentsText), 0o644); err != nil {
			t.Fatal(err)
		}
		agentMD := fmt.Sprintf("---\napiVersion: warp/v1alpha1\nkind: Agent\nmetadata:\n  name: main\nspec:\n  models: [\"fake/m1\"]\n  thinking:\n    type: effort\n    effort: %s\n---\nSubagent body.\n{{if .Context}}<project_context path=\"{{.Context.Path}}\">{{.Context.Instructions}}</project_context>{{end}}\n", effort)
		if err := os.WriteFile(filepath.Join(subDir, ".agents", "main.agent.md"), []byte(agentMD), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeSubFiles("SUB_AGENTS_V1", "low")

	rootCLI := cli.WithCWD(wsDir)
	subCLI := cli.WithCWD(subDir)

	if _, err := rootCLI.GetWorkspace(ctx); err != nil {
		t.Fatal(err)
	}
	subSess, err := subCLI.CreateSession(ctx, api.CreateSessionRequest{Title: "sub-reload"})
	if err != nil {
		t.Fatal(err)
	}
	if subSess.Model == nil || subSess.Model.ThinkingDefault == nil || subSess.Model.ThinkingDefault.Effort != "low" {
		t.Fatalf("initial subSess.Model = %+v, want Effort='low'", subSess.Model)
	}

	// Mutate subproject config, then reload via ROOT runtime.
	writeSubFiles("SUB_AGENTS_V2", "high")
	if err := rootCLI.ReloadWorkspace(ctx); err != nil {
		t.Fatalf("rootCLI.ReloadWorkspace: %v", err)
	}

	// Live subproject session and subCLI preview/workspace must reflect reloaded subproject files.
	reloadedSess, err := subCLI.GetSession(ctx, subSess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloadedSess.Model == nil || reloadedSess.Model.ThinkingDefault == nil || reloadedSess.Model.ThinkingDefault.Effort != "high" {
		t.Errorf("reloaded subSess.Model = %+v, want Effort='high'", reloadedSess.Model)
	}
	subPreview, err := subCLI.PreviewSession(ctx, api.PreviewSessionRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if subPreview.Model == nil || subPreview.Model.ThinkingDefault == nil || subPreview.Model.ThinkingDefault.Effort != "high" {
		t.Errorf("subPreview.Model after root reload = %+v, want Effort='high'", subPreview.Model)
	}

	ready := make(chan struct{})
	doneTurn := make(chan struct{})
	go func() {
		for event, err := range subCLI.EventsWithReady(ctx, "", "", func(string) { close(ready) }, api.SessionStreamTopic(subSess.ID)) {
			if err != nil {
				return
			}
			if event.Type == api.EventCompleted || event.Type == api.EventError {
				close(doneTurn)
				return
			}
		}
	}()
	select {
	case <-ready:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := subCLI.Send(ctx, subSess.ID, api.SendRequest{Content: "hello"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-doneTurn:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	mu.Lock()
	defer mu.Unlock()
	if len(seenPrompts) == 0 || !strings.Contains(seenPrompts[0], "SUB_AGENTS_V2") {
		t.Errorf("live subproject session did not pick up SUB_AGENTS_V2 after root reload: %v", seenPrompts)
	}
}

func TestSubprojectSandboxTrustIsolation(t *testing.T) {
	ctx := context.Background()
	srv, cli, wsDir := setupDaemonTest(t, nil)

	untrustedSubDir := filepath.Join(wsDir, "packages", "untrusted")
	if err := os.MkdirAll(untrustedSubDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wsDir, "WORKSPACE.md"), []byte("---\napiVersion: warp/v1alpha1\nkind: Workspace\nmetadata:\n  name: trust-iso\nspec:\n  projects: [\".\", \"packages/untrusted\"]\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Open the UNTRUSTED subproject FIRST to ensure it does not taint the trusted root,
	// and trusted root does not taint the untrusted subproject.
	subCLI := cli.WithCWD(untrustedSubDir)
	rootCLI := cli.WithCWD(wsDir)

	subWS, err := subCLI.GetWorkspace(ctx)
	if err != nil {
		t.Fatalf("subCLI.GetWorkspace: %v", err)
	}
	if !subWS.IsRestricted {
		t.Fatalf("expected untrusted subproject IsRestricted=true, got false")
	}
	subSbx, err := subCLI.GetSandbox(ctx)
	if err != nil {
		t.Fatalf("subCLI.GetSandbox: %v", err)
	}
	if !subSbx.Restricted {
		t.Fatalf("expected untrusted subproject Sandbox.Restricted=true, got false")
	}

	rootWS, err := rootCLI.GetWorkspace(ctx)
	if err != nil {
		t.Fatalf("rootCLI.GetWorkspace: %v", err)
	}
	if rootWS.IsRestricted {
		t.Fatalf("expected trusted root IsRestricted=false even after untrusted subproject opened first")
	}
	rootSbx, err := rootCLI.GetSandbox(ctx)
	if err != nil {
		t.Fatalf("rootCLI.GetSandbox: %v", err)
	}
	if rootSbx.Restricted {
		t.Fatalf("expected trusted root Sandbox.Restricted=false even after untrusted subproject opened first")
	}

	// Update sandbox settings with allowUnsandboxed=true on trusted root:
	// trusted root keeps allowUnsandboxed=true, untrusted subproject keeps it forced false.
	trueVal := true
	if _, err := rootCLI.UpdateSandbox(ctx, api.UpdateSandboxRequest{
		Scope:    api.ScopeProject,
		Settings: api.SandboxSettings{AllowUnsandboxed: &trueVal},
	}); err != nil {
		t.Fatalf("rootCLI.UpdateSandbox: %v", err)
	}
	subSess, err := subCLI.CreateSession(ctx, api.CreateSessionRequest{Title: "untrusted-sub"})
	if err != nil {
		t.Fatalf("subCLI.CreateSession: %v", err)
	}
	rootSess, err := rootCLI.CreateSession(ctx, api.CreateSessionRequest{Title: "trusted-root"})
	if err != nil {
		t.Fatalf("rootCLI.CreateSession: %v", err)
	}
	subRT, err := srv.workspace(ctx, untrustedSubDir)
	if err != nil {
		t.Fatalf("srv.workspace(untrustedSubDir): %v", err)
	}
	rootRT, err := srv.workspace(ctx, wsDir)
	if err != nil {
		t.Fatalf("srv.workspace(wsDir): %v", err)
	}
	subEff, err := subRT.SandboxConfig().Effective()
	if err != nil {
		t.Fatalf("subRT.SandboxConfig().Effective: %v", err)
	}
	rootEff, err := rootRT.SandboxConfig().Effective()
	if err != nil {
		t.Fatalf("rootRT.SandboxConfig().Effective: %v", err)
	}
	if subEff.AllowUnsandboxed {
		t.Errorf("untrusted subproject AllowUnsandboxed = true, want false")
	}
	if !rootEff.AllowUnsandboxed {
		t.Errorf("trusted root AllowUnsandboxed = false, want true")
	}
	liveSub, err := subRT.GetSession(ctx, subSess.ID)
	if err != nil || !liveSub.Sandbox.ReadOnly() {
		t.Errorf("liveSub.Sandbox.ReadOnly = (%v, %v), want true", liveSub != nil && liveSub.Sandbox.ReadOnly(), err)
	}
	liveRoot, err := rootRT.GetSession(ctx, rootSess.ID)
	if err != nil || liveRoot.Sandbox.ReadOnly() {
		t.Errorf("liveRoot.Sandbox.ReadOnly = (%v, %v), want false", liveRoot != nil && liveRoot.Sandbox.ReadOnly(), err)
	}

	// Reloading root must leave root unrestricted and untrusted subproject restricted.
	if err := rootCLI.ReloadWorkspace(ctx); err != nil {
		t.Fatalf("rootCLI.ReloadWorkspace: %v", err)
	}
	if sbxNow, err := rootCLI.GetSandbox(ctx); err != nil || sbxNow.Restricted {
		t.Fatalf("rootCLI.GetSandbox = (%+v, %v), want Restricted=false", sbxNow, err)
	}
	if subSbxNow, err := subCLI.GetSandbox(ctx); err != nil || !subSbxNow.Restricted {
		t.Fatalf("subCLI.GetSandbox = (%+v, %v), want Restricted=true", subSbxNow, err)
	}
}

func TestCreateSessionExplicitIDColdSQLiteNoDeadlock(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	testutils.SetupXDG(t, "tasksmith-explicit-id-cold-test")

	wsDir := shortTempDir(t)
	subDir := filepath.Join(wsDir, "packages", "sub")
	if err := os.MkdirAll(filepath.Join(subDir, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := workspace.TrustFolder(wsDir, api.TrustScopeFolder); err != nil {
		t.Fatal(err)
	}
	if err := workspace.TrustFolder(subDir, api.TrustScopeFolder); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wsDir, "WORKSPACE.md"), []byte("---\napiVersion: warp/v1alpha1\nkind: Workspace\nmetadata:\n  name: cold-id\nspec:\n  projects: [\".\", \"packages/sub\"]\n  defaultProvider: fake\n  defaultModels:\n    completion: fake-default-model\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "AGENTS.md"), []byte("cold id subproject"), 0o644); err != nil {
		t.Fatal(err)
	}

	newDaemon := func() (*Daemon, *client.Client) {
		runDir := shortTempDir(t)
		srv, err := New(Config{
			SocketPath:            filepath.Join(runDir, "b.sock"),
			LockPath:              filepath.Join(runDir, "b.lock"),
			GatewayPool:           newFakeGatewayPool(nil),
			Logger:                log.Default(),
			DisableMCPAutoConnect: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := srv.Start(ctx); err != nil {
			t.Fatal(err)
		}
		c, err := client.New(srv.SocketPath(), client.WithCWD(subDir))
		if err != nil {
			_ = srv.Close(ctx)
			t.Fatal(err)
		}
		return srv, c
	}

	srv1, cli1 := newDaemon()
	explicitID := uuid.Must(uuid.NewV7()).String()
	created, err := cli1.CreateSession(ctx, api.CreateSessionRequest{
		ID:    explicitID,
		Title: "persisted-id",
	})
	if err != nil {
		t.Fatalf("cli1.CreateSession: %v", err)
	}
	if created.WorkDir != subDir {
		t.Fatalf("created.WorkDir = %q, want %q", created.WorkDir, subDir)
	}
	if err := srv1.Close(ctx); err != nil {
		t.Fatal(err)
	}

	srv2, cli2 := newDaemon()
	t.Cleanup(func() { _ = srv2.Close(context.Background()) })

	// Calling CreateSession with the same explicit ID against a cold SQLite store must
	// return the persisted session without deadlocking on owner.createMu.
	reused, err := cli2.CreateSession(ctx, api.CreateSessionRequest{
		ID:    explicitID,
		Title: "persisted-id",
	})
	if err != nil {
		t.Fatalf("cli2.CreateSession(explicitID): %v", err)
	}
	if reused.ID != explicitID || reused.WorkDir != subDir {
		t.Errorf("reused = (%q, %q), want (%q, %q)", reused.ID, reused.WorkDir, explicitID, subDir)
	}
}

func TestStructuredContentConversionFailure(t *testing.T) {
	tool := &message.Tool{
		Base: message.Base{ID: "result"}, ToolCallID: "call",
		StructuredContent: make(chan int),
	}
	converted, err := session.ToAPIMessage(tool)
	var unsupported *json.UnsupportedTypeError
	if converted != nil || !errors.As(err, &unsupported) {
		t.Fatalf("ToAPIMessage = (%+v, %v), want an unsupported type error", converted, err)
	}
	entry := timeline.NewMessageEntry(tool)
	_, entryErr := session.ToAPITimelineEntry(*entry)
	if entryErr == nil || !errors.As(entryErr, &unsupported) {
		t.Fatalf("ToAPITimelineEntry = %v, want unsupported type error", entryErr)
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
	sess, err := cli.CreateSession(ctx, api.CreateSessionRequest{
		Title: "Structured Tool Result",
	})
	if err != nil {
		t.Fatal(err)
	}
	ready := make(chan struct{})
	results := make(chan api.Message, 1)
	subErrors := make(chan error, 1)
	go func() {
		for event, err := range cli.EventsWithReady(ctx, "", "", func(string) { close(ready) }, api.SessionStreamTopic(sess.ID)) {
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
	if err := cli.Send(ctx, sess.ID, api.SendRequest{Content: "Read source.go."}); err != nil {
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
	if liveOutput.Path == "" || liveOutput.StartLine != 2 || liveOutput.EndLine != 2 || liveOutput.Content != "2\tsecond" {
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

func TestDaemonTasksEndpoints(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	var step int
	provider := &fakeProvider{
		chunkHandler: func(_ context.Context, req *llm.Request) (llm.StreamResponse, error) {
			step++
			var content message.Content
			if step == 1 {
				content = message.Content{&message.ToolCall{
					ID:   "bash-call",
					Name: "bash",
					Args: map[string]any{
						"command":           "echo 'hello from task' && sleep 5",
						"run_in_background": true,
					},
				}}
			} else {
				content = message.Content{&message.TextBlock{Text: "Task started."}}
			}
			return func(yield func(message.AssistantChunk, error) bool) {
				yield(message.AssistantChunk{
					BaseChunk: message.BaseChunk{ID: uuid.NewString()},
					Model:     req.Model,
					Content:   content,
					Done:      true,
				}, nil)
			}, nil
		},
	}

	_, cli, _ := setupDaemonTest(t, provider)
	_, err := cli.AddPermissionRule(ctx, api.PermissionRuleRequest{
		Scope:  api.ScopeGlobal,
		Effect: api.EffectAllow,
		Rule:   "bash(*)",
	})
	if err != nil {
		t.Fatal(err)
	}

	sess, err := cli.CreateSession(ctx, api.CreateSessionRequest{
		Title: "Tasks Endpoints",
	})
	if err != nil {
		t.Fatal(err)
	}

	tasks, err := cli.Tasks(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 0 {
		t.Fatalf("expected 0 initial tasks, got %d", len(tasks))
	}

	ready := make(chan struct{})
	doneTurn := make(chan struct{})
	var turnErr error
	go func() {
		for event, err := range cli.EventsWithReady(ctx, "", "", func(string) { close(ready) }, api.SessionStreamTopic(sess.ID)) {
			if err != nil {
				return
			}
			if event.Type == api.EventError {
				if ev, ok := event.Payload.(api.ErrorEvent); ok {
					turnErr = fmt.Errorf("stream error: %s", ev.Error)
				} else {
					turnErr = fmt.Errorf("stream error: unknown error payload")
				}
				close(doneTurn)
				return
			}
			if event.Type == api.EventCompleted {
				close(doneTurn)
				return
			}
		}
	}()

	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for event stream ready")
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	if err := cli.Send(ctx, sess.ID, api.SendRequest{Content: "Run background command."}); err != nil {
		t.Fatal(err)
	}

	select {
	case <-doneTurn:
		if turnErr != nil {
			t.Fatalf("turn failed: %v", turnErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for turn completion")
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	tasks, err = cli.Tasks(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 background task, got %d", len(tasks))
	}
	taskID := tasks[0].ID
	if tasks[0].Status != api.TaskStatusRunning {
		t.Fatalf("task status = %q, want %q", tasks[0].Status, api.TaskStatusRunning)
	}

	// Stream output
	outputCtx, cancelOutput := context.WithTimeout(ctx, 3*time.Second)
	defer cancelOutput()

	var output string
	for chunk, err := range cli.StreamTaskOutput(outputCtx, sess.ID, taskID) {
		if err != nil {
			break
		}
		output += chunk
		if strings.Contains(output, "hello from task") {
			break
		}
	}
	if !strings.Contains(output, "hello from task") {
		t.Fatalf("streamed output does not contain expected text, got %q", output)
	}

	// Kill task
	if err := cli.KillTask(ctx, sess.ID, taskID); err != nil {
		t.Fatalf("kill task: %v", err)
	}

	// Verify task is now canceled or completed/failed
	time.Sleep(100 * time.Millisecond)
	tasks, err = cli.Tasks(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].Status != api.TaskStatusCanceled && tasks[0].Status != api.TaskStatusFailed {
		t.Fatalf("killed task status = %q, want canceled or failed", tasks[0].Status)
	}

	// Killing an already finished/canceled task returns conflict (409)
	err = cli.KillTask(ctx, sess.ID, taskID)
	if err == nil {
		t.Fatal("expected error killing already finished task, got nil")
	}
	var apiErr *api.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 Conflict, got %v", err)
	}
}

func TestGetWorkspaceSurfacesWarnings(t *testing.T) {
	ctx := context.Background()
	testutils.SetupXDG(t, "tasksmith-daemon-warnings-test")

	runDir := shortTempDir(t)
	sockPath := filepath.Join(runDir, "b.sock")
	lockPath := filepath.Join(runDir, "b.lock")

	pool := &fakeGatewayPool{
		gw: &fakeGateway{
			provider: &fakeProvider{},
			providers: []gateway.ProviderInfo{
				{
					Name:    "anthropic",
					Warning: `provider "anthropic": missing required environment variable "ANTHROPIC_API_KEY"`,
				},
				{
					Name:    "ollama",
					Warning: "",
				},
			},
		},
	}

	srv, err := New(Config{
		SocketPath:  sockPath,
		LockPath:    lockPath,
		GatewayPool: pool,
		Logger:      log.Default(),
	})
	if err != nil {
		t.Fatalf("daemon.New: %v", err)
	}

	if err := srv.Start(ctx); err != nil {
		t.Fatalf("srv.Start: %v", err)
	}
	t.Cleanup(func() {
		_ = srv.Close(ctx)
	})

	wsDir := shortTempDir(t)
	cli, err := client.New(sockPath, client.WithCWD(wsDir))
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}

	getWorkspace := func() *api.Workspace {
		t.Helper()
		ws, err := cli.GetWorkspace(ctx)
		if err != nil {
			t.Fatalf("cli.GetWorkspace: %v", err)
		}
		return ws
	}
	initial := getWorkspace()

	if _, err := cli.UpdateSandbox(ctx, api.UpdateSandboxRequest{
		Scope:    api.ScopeGlobal,
		Settings: api.SandboxSettings{Mode: api.SandboxLocal},
	}); err != nil {
		t.Fatalf("cli.UpdateSandbox: %v", err)
	}
	local := getWorkspace()
	if local.SandboxMode != api.SandboxLocal || len(local.SandboxWarnings) != 0 {
		t.Errorf("explicit local mode: got mode %q, sandbox warnings %v", local.SandboxMode, local.SandboxWarnings)
	}
	if want := pool.gw.providers[0].Warning; !slices.Contains(local.Warnings, want) {
		t.Errorf("Warnings = %q, want provider warning %q", local.Warnings, want)
	}

	checkWarnings := func(ws *api.Workspace, wantSandboxPrefix string) {
		t.Helper()
		if wantSandboxPrefix == "" {
			if len(ws.SandboxWarnings) != 0 {
				t.Errorf("SandboxWarnings = %q, want none", ws.SandboxWarnings)
			}
		} else if len(ws.SandboxWarnings) != 1 || !strings.HasPrefix(ws.SandboxWarnings[0], wantSandboxPrefix) {
			t.Errorf("SandboxWarnings = %q, want one warning starting with %q", ws.SandboxWarnings, wantSandboxPrefix)
		}
		if want := slices.Concat(ws.SandboxWarnings, local.Warnings); !slices.Equal(ws.Warnings, want) {
			t.Errorf("Warnings = %q, want %q", ws.Warnings, want)
		}
	}
	if sandbox.NativeAvailable() != nil {
		checkWarnings(initial, "Kernel sandboxing is unavailable on this host (")
	} else {
		checkWarnings(initial, "")
	}

	sandboxPath, err := xdg.SubConfigDir("sandbox.json")
	if err != nil {
		t.Fatalf("resolve sandbox config path: %v", err)
	}
	if err := os.WriteFile(sandboxPath, []byte("{"), 0o600); err != nil {
		t.Fatalf("write malformed sandbox config: %v", err)
	}
	invalid := getWorkspace()
	if want := sandbox.DefaultMode(); invalid.SandboxMode != want {
		t.Errorf("malformed config fallback mode = %q, want %q", invalid.SandboxMode, want)
	}
	checkWarnings(invalid, "sandbox.json could not be read (")
}

func TestWorkspaceCommandsAndSkillsEndpointsAndExecution(t *testing.T) {
	ctx := context.Background()
	var (
		promptMu   sync.Mutex
		lastPrompt string
	)
	getPrompt := func() string {
		promptMu.Lock()
		defer promptMu.Unlock()
		return lastPrompt
	}
	prov := &fakeProvider{
		chunkHandler: func(ctx context.Context, req *llm.Request) (llm.StreamResponse, error) {
			if len(req.Messages) > 0 {
				var chunk strings.Builder
				for _, b := range req.Messages[len(req.Messages)-1].GetContent() {
					if tb, ok := b.(*message.TextBlock); ok && tb != nil {
						chunk.WriteString(tb.Text)
					}
				}
				promptMu.Lock()
				lastPrompt += chunk.String()
				promptMu.Unlock()
			}
			return func(yield func(message.AssistantChunk, error) bool) {
				yield(message.AssistantChunk{
					BaseChunk: message.BaseChunk{ID: uuid.NewString()},
					Model:     req.Model,
					Content:   []message.Block{&message.TextBlock{Text: "Command executed"}},
					Done:      true,
				}, nil)
			}, nil
		},
	}
	_, cli, wsDir := setupDaemonTest(t, prov)

	// Create command manifest
	cmdsDir := filepath.Join(wsDir, ".agents", "commands")
	if err := os.MkdirAll(cmdsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cmdContent := `---
apiVersion: warp/v1alpha1
kind: Command
metadata:
  name: review
  displayName: Review PR
  description: Review a pull request
spec:
  argumentHint: "Usage: /review <pr_number> [branch]"
  arguments:
    - name: pr_number
      required: true
    - name: branch
      default: main
---
Reviewing PR #{{.pr_number}} on branch {{.branch}}.
`
	if err := os.WriteFile(filepath.Join(cmdsDir, "review.command.md"), []byte(cmdContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create skill manifest
	skillsDir := filepath.Join(wsDir, ".agents", "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	skillContent := `---
apiVersion: warp/v1alpha1
kind: Skill
metadata:
  name: deploy
  displayName: Deploy App
  description: Deployment instructions
spec:
  argumentHint: "Usage: /deploy <target>"
  arguments:
    - name: target
      required: true
---
Deploying to environment {{.target}}.
`
	if err := os.WriteFile(filepath.Join(skillsDir, "deploy.skill.md"), []byte(skillContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// 1. ListCommands
	commands, err := cli.ListCommands(ctx)
	if err != nil {
		t.Fatalf("ListCommands: %v", err)
	}
	var reviewCmd *api.Command
	for i := range commands {
		if commands[i].Name == "review" {
			reviewCmd = &commands[i]
			break
		}
	}
	if reviewCmd == nil {
		t.Fatalf("expected review command in ListCommands: %+v", commands)
	}
	if len(reviewCmd.Arguments) != 2 || reviewCmd.Arguments[0].Name != "pr_number" || !reviewCmd.Arguments[0].Required {
		t.Errorf("unexpected command arguments: %+v", reviewCmd.Arguments)
	}

	// 2. GetCommand (found & 404)
	cmd, err := cli.GetCommand(ctx, "review")
	if err != nil {
		t.Fatalf("GetCommand: %v", err)
	}
	if cmd.Name != "review" || cmd.ArgumentHint != "Usage: /review <pr_number> [branch]" {
		t.Errorf("unexpected cmd: %+v", cmd)
	}
	var apiErr *api.APIError
	if _, err := cli.GetCommand(ctx, "nonexistent"); !errors.As(err, &apiErr) || apiErr.StatusCode != 404 {
		t.Errorf("GetCommand non-existent = %v, want 404 APIError", err)
	}

	// 3. ListSkills
	skills, err := cli.ListSkills(ctx)
	if err != nil {
		t.Fatalf("ListSkills: %v", err)
	}
	foundSkills := map[string]bool{}
	for _, skill := range skills {
		foundSkills[skill.Name] = true
	}
	if !foundSkills["deploy"] || !foundSkills["tasksmith-customizations"] {
		t.Fatalf("unexpected ListSkills: %+v", skills)
	}

	// 4. GetSkill (found & 404)
	sk, err := cli.GetSkill(ctx, "deploy")
	if err != nil {
		t.Fatalf("GetSkill: %v", err)
	}
	if sk.Name != "deploy" || sk.ArgumentHint != "Usage: /deploy <target>" {
		t.Errorf("unexpected sk: %+v", sk)
	}
	if _, err := cli.GetSkill(ctx, "nonexistent"); !errors.As(err, &apiErr) || apiErr.StatusCode != 404 {
		t.Errorf("GetSkill non-existent = %v, want 404 APIError", err)
	}

	// 5. Execution: Create session and run command with positional args and defaulted arg
	sess, err := cli.CreateSession(ctx, api.CreateSessionRequest{Title: "Cmd Test"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	waitForPrompt := func(substr string) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if strings.Contains(getPrompt(), substr) {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Errorf("expected rendered prompt containing %q, got: %q", substr, getPrompt())
	}

	if err := cli.Send(ctx, sess.ID, api.SendRequest{Content: "/review 456"}); err != nil {
		t.Fatalf("Send /review 456: %v", err)
	}
	waitForPrompt("Reviewing PR #456 on branch main.")

	// 6. Execution: Missing required argument returns 400 Bad Request
	if err := cli.Send(ctx, sess.ID, api.SendRequest{Content: "/review"}); !errors.As(err, &apiErr) || apiErr.StatusCode != 400 {
		t.Errorf("expected 400 Bad Request on missing required argument, got: %v", err)
	}

	// 7. Execution: Run skill via slash command
	if err := cli.Send(ctx, sess.ID, api.SendRequest{Content: "/deploy staging"}); err != nil {
		t.Fatalf("Send /deploy staging: %v", err)
	}
	waitForPrompt("Deploying to environment staging.")
}

func TestWorkspaceToolsEndpoint(t *testing.T) {
	ctx := context.Background()
	srv, cli, wsDir := setupDaemonTest(t, nil)

	toolsDir := filepath.Join(wsDir, ".agents", "tools")
	if err := os.MkdirAll(toolsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	toolContent := `---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: linter
  displayName: Workspace Linter
  description: Lint the workspace sources
spec:
  command: ["golangci-lint", "run", "{{.Input.path}}"]
  annotations:
    isReadOnly: false
    isDangerous: true
    isOpenWorld: false
    isIdempotent: true
    userHint: "Lint %s"
  inputSchema:
    type: object
    properties:
      path:
        type: string
        description: Path to lint.
    required:
      - path
  outputSchema:
    type: string
---
Run the project linter over the requested path.
`
	if err := os.WriteFile(filepath.Join(toolsDir, "linter.tool.md"), []byte(toolContent), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := workspace.TrustFolder(wsDir, api.TrustScopeFolder); err != nil {
		t.Fatal(err)
	}
	if err := srv.ReloadWorkspace(ctx, wsDir); err != nil {
		t.Fatalf("ReloadWorkspace: %v", err)
	}

	tools, err := cli.ListTools(ctx, "")
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	byName := make(map[string]api.Tool, len(tools))
	for _, tl := range tools {
		byName[tl.Name] = tl
	}

	// 1. Custom workspace tool round-trips with command, schemas, annotations,
	// and its project scope.
	linter, ok := byName["linter"]
	if !ok {
		t.Fatalf("custom tool missing from ListTools: %+v", tools)
	}
	if linter.DisplayName != "Workspace Linter" || linter.Description != "Lint the workspace sources" {
		t.Errorf("linter metadata = %+v", linter)
	}
	if linter.Scope != "Project" {
		t.Errorf("linter Scope = %q, want Project", linter.Scope)
	}
	if !slices.Equal(linter.Command, []string{"golangci-lint", "run", "{{.Input.path}}"}) {
		t.Errorf("linter Command = %v", linter.Command)
	}
	if linter.InputSchema["type"] != "object" {
		t.Errorf("linter InputSchema = %v, want object schema", linter.InputSchema)
	}
	if linter.OutputSchema["type"] != "string" {
		t.Errorf("linter OutputSchema = %v, want string schema", linter.OutputSchema)
	}
	if !linter.Annotations.IsDangerous || !linter.Annotations.IsIdempotent || linter.Annotations.IsReadOnly {
		t.Errorf("linter annotations = %+v", linter.Annotations)
	}
	if linter.Annotations.UserHint != "Lint %s" {
		t.Errorf("linter annotations UserHint = %q", linter.Annotations.UserHint)
	}
	if linter.Tokens <= 0 {
		t.Errorf("linter Tokens = %d, want > 0", linter.Tokens)
	}

	// 2. Built-in system tools are reported with the Builtin scope and their
	// declarative safety annotations.
	bash, ok := byName["bash"]
	if !ok {
		t.Fatalf("built-in tool bash missing from ListTools: %+v", tools)
	}
	if bash.Scope != "Builtin" {
		t.Errorf("bash Scope = %q, want Builtin", bash.Scope)
	}
	if !bash.Annotations.IsDangerous || !bash.Annotations.IsOpenWorld || bash.Annotations.IsReadOnly {
		t.Errorf("bash annotations = %+v", bash.Annotations)
	}
	if len(bash.Command) != 0 {
		t.Errorf("bash Command = %v, want empty for host-native tool", bash.Command)
	}
	view, ok := byName["view"]
	if !ok {
		t.Fatalf("built-in tool view missing from ListTools: %+v", tools)
	}
	if !view.Annotations.IsReadOnly || !view.Annotations.IsIdempotent {
		t.Errorf("view annotations = %+v", view.Annotations)
	}
	if view.Tokens <= 0 {
		t.Errorf("view Tokens = %d, want > 0", view.Tokens)
	}

	// 3. Tools are returned sorted by name.
	for i := 1; i < len(tools); i++ {
		if tools[i-1].Name > tools[i].Name {
			t.Fatalf("ListTools not sorted at index %d: %q before %q", i, tools[i-1].Name, tools[i].Name)
		}
	}

	// 4. Scoping the listing to an agent classifies each tool's availability for
	// that agent: the main agent lists view but not the custom linter, so the
	// linter is deferred and tool_search is auto-activated to discover it.
	scoped, err := cli.ListTools(ctx, "main")
	if err != nil {
		t.Fatalf("ListTools(main): %v", err)
	}
	scopedByName := make(map[string]api.Tool, len(scoped))
	for _, tl := range scoped {
		scopedByName[tl.Name] = tl
	}
	if got := scopedByName["view"].Status; got != api.ToolStatusEager {
		t.Errorf("view status for main = %q, want %q", got, api.ToolStatusEager)
	}
	if got := scopedByName["linter"].Status; got != api.ToolStatusDeferred {
		t.Errorf("linter status for main = %q, want %q", got, api.ToolStatusDeferred)
	}
	if got := scopedByName["tool_search"].Status; got != api.ToolStatusEager {
		t.Errorf("tool_search status for main = %q, want %q", got, api.ToolStatusEager)
	}
	if got := scopedByName["invoke_agent"].Status; got != api.ToolStatusEager {
		t.Errorf("invoke_agent status for main = %q, want %q", got, api.ToolStatusEager)
	}

	// 5. An unknown agent is rejected rather than silently unclassified.
	if _, err := cli.ListTools(ctx, "no-such-agent"); err == nil {
		t.Fatal("expected error listing tools for unknown agent")
	} else {
		var apiErr *api.APIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusNotFound {
			t.Errorf("unknown agent error = %v, want 404 API error", err)
		}
	}
}

func TestSendWithClientSuppliedID(t *testing.T) {
	ctx := context.Background()

	prov := &fakeProvider{
		chunkHandler: func(_ context.Context, req *llm.Request) (llm.StreamResponse, error) {
			return func(yield func(message.AssistantChunk, error) bool) {
				yield(message.AssistantChunk{
					BaseChunk: message.BaseChunk{ID: "asst-chunk-1"},
					Model:     req.Model,
					Content:   []message.Block{&message.TextBlock{Text: "hello back"}},
					Done:      true,
				}, nil)
			}, nil
		},
	}

	_, cli, _ := setupDaemonTest(t, prov)

	sess, err := cli.CreateSession(ctx, api.CreateSessionRequest{Title: "Custom ID Test"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	subCtx, cancelSub := context.WithCancel(ctx)
	defer cancelSub()

	events := make(chan api.Event, 16)
	subReady := make(chan struct{})

	go func() {
		for ev, err := range cli.EventsWithReady(subCtx, "", "", func(string) { close(subReady) }, api.SessionStreamTopic(sess.ID)) {
			if err != nil {
				return
			}
			events <- ev
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

	customMsgID := uuid.Must(uuid.NewV7()).String()

	err = cli.Send(ctx, sess.ID, api.SendRequest{
		ID:      customMsgID,
		Content: "Hello with custom ID",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	// Verify EventEntryAdded contains the exact client-supplied UUIDv7 ID
	var confirmedID string
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && confirmedID == "" {
		select {
		case ev := <-events:
			if ev.Type == api.EventEntryAdded {
				if p, ok := ev.Payload.(api.EntryAddedEvent); ok && p.Entry.Kind == api.TimelineKindMessage {
					if p.Entry.Message != nil && p.Entry.Message.Role == api.RoleUser {
						confirmedID = p.Entry.ID
						if p.Entry.Message.ID != customMsgID {
							t.Errorf("p.Entry.Message.ID = %q, want %q", p.Entry.Message.ID, customMsgID)
						}
					}
				}
			}
		case <-time.After(50 * time.Millisecond):
		}
	}

	if confirmedID != customMsgID {
		t.Fatalf("EventEntryAdded ID = %q, want %q", confirmedID, customMsgID)
	}

	// Verify persisted timeline also has the exact ID
	timeline, err := cli.Timeline(ctx, sess.ID)
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	var foundUser bool
	for _, entry := range timeline {
		if entry.Kind == api.TimelineKindMessage && entry.Message != nil && entry.Message.Role == api.RoleUser {
			foundUser = true
			if entry.ID != customMsgID {
				t.Errorf("persisted timeline entry.ID = %q, want %q", entry.ID, customMsgID)
			}
			if entry.Message.ID != customMsgID {
				t.Errorf("persisted timeline entry.Message.ID = %q, want %q", entry.Message.ID, customMsgID)
			}
		}
	}
	if !foundUser {
		t.Fatal("expected user message entry in timeline")
	}
}

func TestSendWithInvalidID(t *testing.T) {
	ctx := context.Background()
	_, cli, _ := setupDaemonTest(t, nil)

	sess, err := cli.CreateSession(ctx, api.CreateSessionRequest{Title: "Validation Test"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	tests := map[string]struct {
		req  api.SendRequest
		code int
	}{
		"not-a-uuid": {
			req:  api.SendRequest{ID: "not-a-uuid", Content: "hello"},
			code: http.StatusBadRequest,
		},
		"uuid-v4": {
			req:  api.SendRequest{ID: uuid.NewString(), Content: "hello"},
			code: http.StatusBadRequest,
		},
		"decision-and-id": {
			req: api.SendRequest{
				ID:       uuid.Must(uuid.NewV7()).String(),
				Decision: &api.PermissionDecision{Kind: api.DecisionAllowOnce},
			},
			code: http.StatusBadRequest,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			err := cli.Send(ctx, sess.ID, tc.req)
			var apiErr *api.APIError
			if !errors.As(err, &apiErr) || apiErr.StatusCode != tc.code {
				t.Fatalf("Send error = %v, want HTTP status %d", err, tc.code)
			}
		})
	}
}

func TestCreateSessionWithCallerSuppliedID(t *testing.T) {
	ctx := context.Background()
	_, cli, _ := setupDaemonTest(t, nil)
	id := uuid.Must(uuid.NewV7()).String()

	created, err := cli.CreateSession(ctx, api.CreateSessionRequest{ID: id, Title: "Draft"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if created.ID != id || created.Title != "Draft" {
		t.Fatalf("created = %+v", created)
	}

	// Creating it again is idempotent, including when optional fields are omitted.
	for _, req := range []api.CreateSessionRequest{{ID: id, Title: "Draft"}, {ID: id}} {
		again, err := cli.CreateSession(ctx, req)
		if err != nil {
			t.Fatalf("repeat CreateSession(%+v): %v", req, err)
		}
		if again.ID != id || again.Title != "Draft" || !again.CreatedAt.Equal(created.CreatedAt) {
			t.Errorf("repeat = %+v, want the existing session", again)
		}
	}
	list, err := cli.ListSessions(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("ListSessions = %d, %v; want 1", len(list), err)
	}
}

func TestCreateSessionIDErrors(t *testing.T) {
	ctx := context.Background()
	_, cli, _ := setupDaemonTest(t, nil)
	id := uuid.Must(uuid.NewV7()).String()
	if _, err := cli.CreateSession(ctx, api.CreateSessionRequest{ID: id, Title: "Original"}); err != nil {
		t.Fatal(err)
	}

	for name, tc := range map[string]struct {
		req  api.CreateSessionRequest
		code int
	}{
		"conflicting title": {api.CreateSessionRequest{ID: id, Title: "Different"}, http.StatusConflict},
		"invalid id":        {api.CreateSessionRequest{ID: "not-a-uuid"}, http.StatusBadRequest},
		"uuid v4":           {api.CreateSessionRequest{ID: uuid.NewString()}, http.StatusBadRequest},
	} {
		_, err := cli.CreateSession(ctx, tc.req)
		var apiErr *api.APIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != tc.code {
			t.Errorf("%s: err = %v, want API error with status %d", name, err, tc.code)
		}
	}
}

func TestWorkspaceStatusInitAndVerifyProvider(t *testing.T) {
	ctx := context.Background()
	_, cli, _ := setupDaemonTest(t, nil)

	// 1. GetWorkspaceStatus
	status, err := cli.GetWorkspaceStatus(ctx)
	if err != nil {
		t.Fatalf("GetWorkspaceStatus error: %v", err)
	}
	if !status.IsTrusted {
		t.Errorf("expected test workspace to be trusted, got %+v", status)
	}
	if len(status.GlobalProviders) == 0 {
		t.Errorf("expected global providers to include presets, got 0")
	}

	// 2. VerifyProvider
	verifyResp, err := cli.VerifyProvider(ctx, api.VerifyProviderRequest{
		Provider: "anthropic",
		APIKey:   "sk-ant-test-1234",
	})
	if err != nil {
		t.Fatalf("VerifyProvider error: %v", err)
	}
	if !verifyResp.Valid {
		t.Fatalf("expected valid provider verification, got %+v", verifyResp)
	}

	// 3. InitWorkspace
	initReq := api.InitWorkspaceRequest{
		CWD:           cli.CWD(),
		Scope:         "workspace",
		Provider:      "anthropic",
		APIKey:        "sk-ant-test-1234",
		DefaultMode:   api.ModeDefault,
		DisabledTools: []string{"download"},
	}
	if err := cli.InitWorkspace(ctx, initReq); err != nil {
		t.Fatalf("InitWorkspace error: %v", err)
	}

	// Status should now show configured
	statusAfter, err := cli.GetWorkspaceStatus(ctx)
	if err != nil {
		t.Fatalf("GetWorkspaceStatus after init error: %v", err)
	}
	if !statusAfter.IsConfigured {
		t.Errorf("expected workspace to be configured after InitWorkspace, got %+v", statusAfter)
	}
}

func TestAntigravityOAuthEndpointsAndStatus(t *testing.T) {
	_, cli, _ := setupDaemonTest(t, nil)
	ctx := context.Background()

	// 1. Initial status: idle / no credentials
	status1, err := cli.GetProviderAuthStatus(ctx, "antigravity")
	if err != nil {
		t.Fatalf("GetProviderAuthStatus idle error: %v", err)
	}
	if status1.Status != "idle" {
		t.Errorf("expected status idle, got %q", status1.Status)
	}

	// 2. VerifyProvider fails when no token exists
	verifyBefore, err := cli.VerifyProvider(ctx, api.VerifyProviderRequest{
		Provider: "antigravity",
	})
	if err != nil {
		t.Fatalf("VerifyProvider error: %v", err)
	}
	if verifyBefore.Valid {
		t.Errorf("expected invalid verify before auth, got valid")
	}

	// 3. Save a token and verify authenticated status
	tok := &oauth2.Token{
		AccessToken:  "mock-access-token",
		RefreshToken: "mock-refresh-token",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(1 * time.Hour),
	}
	if err := auth.SaveProviderToken("antigravity", tok, "developer@google.com"); err != nil {
		t.Fatalf("SaveAntigravityToken error: %v", err)
	}

	status2, err := cli.GetProviderAuthStatus(ctx, "antigravity")
	if err != nil {
		t.Fatalf("GetProviderAuthStatus authenticated error: %v", err)
	}
	if status2.Status != "authenticated" || status2.Account != "developer@google.com" {
		t.Errorf("unexpected status2: %+v", status2)
	}

	// 4. VerifyProvider succeeds with on-disk token
	verifyAfter, err := cli.VerifyProvider(ctx, api.VerifyProviderRequest{
		Provider: "antigravity",
	})
	if err != nil {
		t.Fatalf("VerifyProvider after auth error: %v", err)
	}
	if !verifyAfter.Valid {
		t.Errorf("expected valid verify after auth, got invalid: %s", verifyAfter.ErrorMessage)
	}

	// 5. GetWorkspaceStatus reflects OAuth status
	wsStatus, err := cli.GetWorkspaceStatus(ctx)
	if err != nil {
		t.Fatalf("GetWorkspaceStatus error: %v", err)
	}
	found := false
	for _, p := range wsStatus.GlobalProviders {
		if p.Name == "antigravity" {
			found = true
			if !p.HasKey {
				t.Errorf("expected antigravity to have key")
			}
			if p.AuthType != "oauth2" {
				t.Errorf("expected antigravity AuthType oauth2, got %q", p.AuthType)
			}
			if p.Account != "developer@google.com" {
				t.Errorf("expected account developer@google.com, got %q", p.Account)
			}
			break
		}
	}
	if !found {
		t.Errorf("antigravity provider not found in GlobalProviders")
	}
}

// TestInvalidProviderManifestIsolatedInDaemon verifies that a workspace with
// one misconfigured provider manifest (e.g. auth.type: "env") alongside a
// healthy provider does not break ListModels, GetWorkspaceStatus,
// CreateSession, or ListWorkspaceProviders with HTTP 500.
func TestInvalidProviderManifestIsolatedInDaemon(t *testing.T) {
	ctx := context.Background()
	xdgDir := shortTempDir(t)
	t.Cleanup(xdg.ClearCache)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(xdgDir, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(xdgDir, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(xdgDir, "state"))
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(xdgDir, "run"))
	t.Setenv("HEALTHY_TEST_KEY", "sk-healthy-daemon")
	xdg.ClearCache()

	runDir := shortTempDir(t)
	sockPath := filepath.Join(runDir, "daemon.sock")
	lockPath := filepath.Join(runDir, "daemon.lock")

	pool := gateway.NewPool()
	srv, err := New(Config{
		SocketPath:            sockPath,
		LockPath:              lockPath,
		GatewayPool:           pool,
		Logger:                log.Default(),
		DisableMCPAutoConnect: true,
	})
	if err != nil {
		t.Fatalf("daemon.New: %v", err)
	}
	if err := srv.Start(ctx); err != nil {
		t.Fatalf("daemon.Start: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close(context.Background()) })

	wsDir := shortTempDir(t)
	if err := workspace.TrustFolder(wsDir, api.TrustScopeFolder); err != nil {
		t.Fatal(err)
	}
	wsManifest := "---\napiVersion: warp/v1alpha1\nkind: Workspace\nmetadata:\n  name: fault-isolation\nspec:\n  defaultProvider: healthy\n  defaultModels:\n    completion: gpt-4o\n---\n"
	if err := os.WriteFile(filepath.Join(wsDir, "WORKSPACE.md"), []byte(wsManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	provDir := filepath.Join(wsDir, ".agents", "providers")
	if err := os.MkdirAll(provDir, 0o755); err != nil {
		t.Fatal(err)
	}
	brokenManifest := `apiVersion: warp/v1alpha1
kind: ModelProvider
metadata:
  name: broken-anthropic
spec:
  type: anthropic
  auth:
    type: env
    token: "${env:ANTHROPIC_API_KEY}"
  models:
    - id: claude-sonnet-5-5
      name: claude-sonnet-5-5
`
	healthyManifest := `apiVersion: warp/v1alpha1
kind: ModelProvider
metadata:
  name: healthy
spec:
  type: openai
  auth:
    type: bearer
    token: "${env:HEALTHY_TEST_KEY}"
  models:
    - id: gpt-4o
      name: gpt-4o
`
	if err := os.WriteFile(filepath.Join(provDir, "broken-anthropic.provider.yaml"), []byte(brokenManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(provDir, "healthy.provider.yaml"), []byte(healthyManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	cli, err := client.New(sockPath, client.WithCWD(wsDir))
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}

	wsProviders, err := cli.ListWorkspaceProviders(ctx)
	if err != nil {
		t.Fatalf("ListWorkspaceProviders failed: %v", err)
	}
	statusByName := make(map[string]api.WorkspaceProvider)
	for _, p := range wsProviders {
		statusByName[p.Name] = p
	}
	if got := statusByName["broken-anthropic"]; got.Status != "invalid_config" || got.HasKey {
		t.Errorf("broken-anthropic provider = %+v, want Status=invalid_config HasKey=false", got)
	}
	if got := statusByName["healthy"]; got.Status != "active" || !got.HasKey {
		t.Errorf("healthy provider = %+v, want Status=active HasKey=true", got)
	}

	if _, err := cli.GetWorkspaceStatus(ctx); err != nil {
		t.Fatalf("GetWorkspaceStatus failed: %v", err)
	}
	wsInfo, err := cli.GetWorkspace(ctx)
	if err != nil {
		t.Fatalf("GetWorkspace failed: %v", err)
	}
	if !slices.ContainsFunc(wsInfo.Warnings, func(w string) bool {
		return strings.Contains(w, "broken-anthropic") && strings.Contains(w, "unknown auth type")
	}) {
		t.Errorf("GetWorkspace warnings = %q, want warning naming broken-anthropic and unknown auth type", wsInfo.Warnings)
	}

	modelList, err := cli.ListModels(ctx)
	if err != nil {
		t.Fatalf("ListModels failed: %v", err)
	}
	if !slices.ContainsFunc(modelList, func(m api.Model) bool { return m.ID == "healthy/gpt-4o" }) {
		t.Errorf("ListModels = %+v, want healthy/gpt-4o present", modelList)
	}

	sess, err := cli.CreateSession(ctx, api.CreateSessionRequest{Config: api.SessionConfig{Model: "healthy/gpt-4o"}})
	if err != nil {
		t.Fatalf("CreateSession targeting healthy provider failed: %v", err)
	}
	if sess.ID == "" {
		t.Fatal("CreateSession returned empty ID")
	}

	// Directly targeting the broken provider through the workspace gateway returns ErrInvalidConfig.
	rt, err := srv.workspace(ctx, wsDir)
	if err != nil {
		t.Fatalf("srv.workspace: %v", err)
	}
	if _, err := rt.Gateway().LLM(ctx, models.Model{ID: "broken-anthropic/claude-sonnet-5-5", ProviderType: "anthropic"}, llm.ModelConfig{}); !errors.Is(err, gateway.ErrInvalidConfig) {
		t.Errorf("gateway.LLM(broken-anthropic) = %v, want ErrInvalidConfig", err)
	}
}

type gatedSessionStore struct {
	session.Store
	ctx    context.Context
	once   sync.Once
	read   chan struct{}
	resume chan struct{}
}

func (s *gatedSessionStore) Get(ctx context.Context, id string) (*session.Session, error) {
	sess, err := s.Store.Get(ctx, id)
	if ctx == s.ctx {
		s.once.Do(func() { close(s.read); <-s.resume })
	}
	return sess, err
}

func TestSessionDeleteDuringRestore(t *testing.T) {
	for _, tree := range []bool{false, true} {
		t.Run(fmt.Sprintf("tree=%t", tree), func(t *testing.T) {
			srv, _, dir := setupDaemonTest(t, &fakeProvider{})
			ctx := t.Context()
			rt, err := srv.workspace(ctx, dir)
			if err != nil {
				t.Fatal(err)
			}
			parent, _, err := rt.CreateSession(ctx, api.CreateSessionRequest{})
			if err != nil {
				t.Fatal(err)
			}
			var childID string
			if tree {
				child, _, err := rt.CreateSession(ctx, api.CreateSessionRequest{})
				if err != nil {
					t.Fatal(err)
				}
				child.ParentID = parent.ID
				childID = child.ID
				if err := child.CheckpointTo(ctx, rt.sessionStore); err != nil {
					t.Fatal(err)
				}
			}
			rt.sessions.Delete(parent.ID)
			if err := parent.Close(); err != nil {
				t.Fatal(err)
			}
			gatedCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			gate := &gatedSessionStore{Store: rt.sessionStore, ctx: gatedCtx, read: make(chan struct{}), resume: make(chan struct{})}
			rt.sessionStore = gate
			done := make(chan error, 1)
			go func() {
				if tree {
					done <- rt.DeleteSession(gatedCtx, parent.ID)
					return
				}
				_, err := rt.GetSession(gatedCtx, parent.ID)
				done <- err
			}()
			<-gate.read
			if tree {
				writer := make(chan struct{})
				go func() {
					rt.reloadMu.Lock()
					rt.RefreshSessions()
					rt.reloadMu.Unlock()
					close(writer)
				}()
				for rt.reloadMu.TryRLock() {
					rt.reloadMu.RUnlock()
					time.Sleep(time.Millisecond)
				}
				close(gate.resume)
				select {
				case <-writer:
				case <-time.After(5 * time.Second):
					t.Fatal("reload blocked by recursive deletion")
				}
			} else {
				err := rt.DeleteSession(ctx, parent.ID)
				close(gate.resume)
				if err != nil {
					t.Fatal(err)
				}
			}
			err = <-done
			if tree && err != nil || !tree && !errors.Is(err, session.ErrSessionNotFound) {
				t.Fatalf("unexpected result: %v", err)
			}
			for _, id := range []string{parent.ID, childID} {
				if id == "" {
					continue
				}
				if _, ok := rt.sessions.Snapshot().Get(id); ok {
					t.Fatalf("cached deleted session %s", id)
				}
				if _, err := rt.sessionStore.Get(ctx, id); !errors.Is(err, session.ErrSessionNotFound) {
					t.Fatalf("deleted session %s: %v", id, err)
				}
			}
		})
	}
}

func TestScanGlobalMCPsIncludesHomeAgents(t *testing.T) {
	home := t.TempDir()
	xdgCfg := filepath.Join(home, "xdg-config")
	t.Cleanup(xdg.ClearCache)
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", xdgCfg)
	t.Setenv("TASKSMITH_APPNAME", "tasksmith")
	xdg.ClearCache()

	xdgMCPDir := filepath.Join(xdgCfg, "tasksmith", "agents", "mcps")
	homeMCPDir := filepath.Join(home, ".agents", "mcps")
	for _, dir := range []string{xdgMCPDir, homeMCPDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{
		filepath.Join(xdgMCPDir, "alpha.mcp.yaml"),
		filepath.Join(xdgMCPDir, "shared.mcp.yaml"),
		filepath.Join(homeMCPDir, "shared.mcp.yaml"),
		filepath.Join(homeMCPDir, "beta.yaml"),
	} {
		if err := os.WriteFile(path, []byte("name: test\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	got := (&Daemon{}).scanGlobalMCPs()
	want := []string{"alpha", "beta", "shared"}
	if !slices.Equal(got, want) {
		t.Fatalf("scanGlobalMCPs() = %v, want %v", got, want)
	}
}

func checkClose(t *testing.T, closer io.Closer) {
	t.Helper()
	if err := closer.Close(); err != nil {
		t.Errorf("close %T: %v", closer, err)
	}
}

type pausedGatewayPool struct {
	*fakeGatewayPool
	entered chan struct{}
	release chan struct{}
	calls   atomic.Int32
}

func (p *pausedGatewayPool) Acquire(ctx context.Context, cfg gateway.Config) (gateway.Gateway, error) {
	if p.calls.Add(1) == 1 {
		close(p.entered)
		select {
		case <-p.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return p.fakeGatewayPool.Acquire(ctx, cfg)
}

func TestWorkspaceOpenLifecycle(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		t.Run(fmt.Sprintf("shutdown=%t", shutdown), func(t *testing.T) {
			testutils.SetupXDG(t, "open-lifecycle")

			pool := &pausedGatewayPool{
				fakeGatewayPool: newFakeGatewayPool(nil),
				entered:         make(chan struct{}),
				release:         make(chan struct{}),
			}
			d, err := New(Config{
				SocketPath:            "brain.sock",
				LockPath:              "brain.lock",
				GatewayPool:           pool,
				Logger:                log.Default(),
				DisableMCPAutoConnect: true,
			})
			if err != nil {
				t.Fatal(err)
			}

			release := sync.OnceFunc(func() { close(pool.release) })
			defer func() {
				release()
				_ = d.Close(t.Context())
			}()

			type result struct {
				rt  *workspaceRuntime
				err error
			}

			dir := t.TempDir()
			leaderCtx, cancelLeader := context.WithCancel(t.Context())
			defer cancelLeader()

			leaderRes := make(chan result, 1)
			go func() {
				rt, openErr := d.workspace(leaderCtx, dir)
				leaderRes <- result{rt: rt, err: openErr}
			}()

			<-pool.entered

			results := make(chan result, 8)
			for range cap(results) {
				go func() {
					rt, openErr := d.workspace(t.Context(), dir)
					results <- result{rt: rt, err: openErr}
				}()
			}

			// Canceling the leader request must not abort the shared open for healthy waiters.
			cancelLeader()
			select {
			case got := <-leaderRes:
				if !errors.Is(got.err, context.Canceled) {
					t.Fatalf("canceled leader: %v", got.err)
				}
			case <-time.After(time.Second):
				t.Fatal("leader did not unblock on cancellation")
			}

			health := make(chan []string, 1)
			go func() { health <- d.Workspaces() }()
			select {
			case paths := <-health:
				if len(paths) != 0 {
					t.Fatal(paths)
				}
			case <-time.After(time.Second):
				t.Fatal("health blocked on open")
			}

			other := t.TempDir()
			independent := make(chan error, 1)
			go func() {
				_, openErr := d.workspace(t.Context(), other)
				independent <- openErr
			}()
			select {
			case err := <-independent:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("unrelated workspace waited on slow open")
			}

			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			if _, err := d.workspace(ctx, dir); !errors.Is(err, context.Canceled) {
				t.Fatalf("canceled waiter: %v", err)
			}

			closed := make(chan error, 1)
			if shutdown {
				// Daemon.Close cancels openCtx so shutdown does not hang waiting for release().
				go func() { closed <- d.Close(t.Context()) }()
			} else {
				release()
			}

			var first *workspaceRuntime
			for range cap(results) {
				got := <-results
				if shutdown {
					if got.err == nil {
						t.Fatal("open succeeded during close")
					}
					continue
				}
				if got.err != nil {
					t.Fatal(got.err)
				}
				if first != nil && first != got.rt {
					t.Fatal("distinct runtimes for one path")
				}
				first = got.rt
			}

			if shutdown {
				select {
				case err := <-closed:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(3 * time.Second):
					t.Fatal("Close hung on in-flight workspace open")
				}
				if len(d.Workspaces()) != 0 {
					t.Fatal("workspace survived close")
				}
			}

			if pool.calls.Load() != 2 {
				t.Fatalf("gateway acquired %d times", pool.calls.Load())
			}
		})
	}
}

func TestDeleteSessionCancelsTitleGenerationWithoutDeadlock(t *testing.T) {
	testutils.SetupXDG(t, "delete-title-deadlock")

	titleStarted := make(chan struct{})
	titleCanceled := make(chan struct{})
	var titleOnce sync.Once

	provider := &fakeProvider{
		chunkHandler: func(ctx context.Context, req *llm.Request) (llm.StreamResponse, error) {
			for _, msg := range req.Messages {
				if msg.Role() != message.RoleSystem {
					continue
				}
				for _, b := range msg.GetContent() {
					if tb, ok := b.(*message.TextBlock); ok && strings.Contains(strings.ToLower(tb.Text), "title") {
						titleOnce.Do(func() { close(titleStarted) })
						<-ctx.Done()
						close(titleCanceled)
						return nil, ctx.Err()
					}
				}
			}
			return func(yield func(message.AssistantChunk, error) bool) {
				yield(message.AssistantChunk{
					BaseChunk: message.BaseChunk{ID: uuid.NewString()},
					Model:     req.Model,
					Content:   []message.Block{&message.TextBlock{Text: "ok"}},
					Done:      true,
				}, nil)
			}, nil
		},
	}

	d, err := New(Config{
		SocketPath:            "brain.sock",
		LockPath:              "brain.lock",
		GatewayPool:           newFakeGatewayPool(provider),
		Logger:                log.Default(),
		DisableMCPAutoConnect: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close(t.Context()) }()

	dir := t.TempDir()
	if err := workspace.TrustFolder(dir, api.TrustScopeFolder); err != nil {
		t.Fatal(err)
	}
	manifest := "---\napiVersion: warp/v1alpha1\nkind: Workspace\nmetadata:\n  name: test\nspec:\n  defaultProvider: fake\n  defaultModels:\n    completion: fake-default-model\n---\n"
	if err := os.WriteFile(filepath.Join(dir, "WORKSPACE.md"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	s, _, err := d.CreateSession(t.Context(), dir, api.CreateSessionRequest{})
	if err != nil {
		t.Fatal(err)
	}

	if err := d.Send(t.Context(), dir, s.ID, api.SendRequest{Content: "hello world"}); err != nil {
		t.Fatal(err)
	}

	select {
	case <-titleStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("title generation did not start")
	}

	reloadDone := make(chan error, 1)
	go func() {
		reloadDone <- d.ReloadWorkspace(t.Context(), dir)
	}()

	deleteDone := make(chan error, 1)
	go func() {
		deleteDone <- d.DeleteSession(t.Context(), dir, s.ID)
	}()

	select {
	case <-titleCanceled:
	case <-time.After(3 * time.Second):
		t.Fatal("DeleteSession did not cancel in-flight title generation")
	}

	select {
	case err := <-deleteDone:
		if err != nil {
			t.Fatalf("DeleteSession: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("DeleteSession deadlocked")
	}

	select {
	case err := <-reloadDone:
		if err != nil {
			t.Fatalf("ReloadWorkspace: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("ReloadWorkspace deadlocked")
	}
}

func TestUsageWhileRenamingSession(t *testing.T) {
	testutils.SetupXDG(t, "usage-rename")

	d, err := New(Config{
		SocketPath:            "brain.sock",
		LockPath:              "brain.lock",
		GatewayPool:           newFakeGatewayPool(nil),
		Logger:                log.Default(),
		DisableMCPAutoConnect: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close(t.Context()) }()

	dir := t.TempDir()
	if err := workspace.TrustFolder(dir, api.TrustScopeFolder); err != nil {
		t.Fatal(err)
	}

	manifest := "---\napiVersion: warp/v1alpha1\nkind: Workspace\nmetadata:\n  name: test\nspec:\n  defaultProvider: fake\n  defaultModels:\n    completion: fake-default-model\n---\n"
	if err := os.WriteFile(filepath.Join(dir, "WORKSPACE.md"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	s, _, err := d.CreateSession(t.Context(), dir, api.CreateSessionRequest{Title: "initial"})
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		for i := range 30 {
			req := api.UpdateSessionRequest{Title: fmt.Sprint(i)}
			if _, err := d.UpdateSession(t.Context(), dir, s.ID, req); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()

	for range 30 {
		if _, err := d.Usage(t.Context(), dir, api.UsageRequest{}); err != nil {
			t.Fatal(err)
		}
	}

	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
