package cli

import (
	"bytes"
	"context"
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
	"github.com/masterkeysrd/tasksmith/internal/daemon"
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

func (p *fakeProvider) Stream(ctx context.Context, req *llm.Request) (llm.StreamResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	p.mu.Lock()
	h := p.chunkHandler
	p.mu.Unlock()

	if h != nil {
		return h(ctx, req)
	}

	return func(yield func(message.AssistantChunk, error) bool) {
		yield(message.AssistantChunk{
			BaseChunk: message.BaseChunk{ID: uuid.NewString()},
			Model:     req.Model,
			Content: []message.Block{
				&message.TextBlock{Text: "Hello from the Brain Daemon!"},
			},
			Done: true,
		}, nil)
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

func setupCLITestDaemon(t *testing.T, prov *fakeProvider) (*daemon.Server, string, string) {
	t.Helper()

	xdgRoot := t.TempDir()
	xdg.SetTestEnv(xdgRoot, "tasksmith-cli-test")
	t.Cleanup(func() {
		xdg.ClearCache()
	})

	shortTmp, err := os.MkdirTemp("/tmp", "tscli-*")
	if err != nil {
		t.Fatalf("MkdirTemp /tmp: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(shortTmp)
	})
	sockPath := filepath.Join(shortTmp, "d.sock")
	lockPath := filepath.Join(shortTmp, "d.lock")

	srv, err := daemon.New(daemon.Config{
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

	wsDir := filepath.Join(shortTmp, "ws")
	if err := os.MkdirAll(wsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll wsDir: %v", err)
	}

	return srv, sockPath, wsDir
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func execCLI(t *testing.T, stdin string, args ...string) (string, string, error) {
	t.Helper()
	cmd := NewRootCmd()
	var outBuf, errBuf syncBuffer
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs(args)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := cmd.ExecuteContext(ctx)
	return outBuf.String(), errBuf.String(), err
}

func TestDaemonStatusStartAndStopCommands(t *testing.T) {
	// 1. Check status when daemon is not running
	shortTmp, err := os.MkdirTemp("/tmp", "tscli-stopped-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(shortTmp)
	})
	missingSock := filepath.Join(shortTmp, "none.sock")

	out, _, err := execCLI(t, "", "daemon", "status", "--socket", missingSock)
	if err != nil {
		t.Fatalf("daemon status (stopped) error: %v", err)
	}
	if !strings.Contains(out, "Daemon is not running") {
		t.Errorf("daemon status (stopped) output = %q, want 'Daemon is not running'", out)
	}

	// 2. Start in-process daemon and test `daemon start` and `daemon status`
	_, sockPath, wsDir := setupCLITestDaemon(t, nil)

	startOut, _, err := execCLI(t, "", "daemon", "start", "--socket", sockPath)
	if err != nil {
		t.Fatalf("daemon start error: %v", err)
	}
	if !strings.Contains(startOut, "Daemon running") || !strings.Contains(startOut, sockPath) {
		t.Errorf("daemon start output = %q, want 'Daemon running'", startOut)
	}

	// Open a workspace by running a quick REPL /sessions + /exit so status lists 1 open workspace (with 0 sessions created)
	if _, _, err := execCLI(t, "/sessions\n/exit\n", "repl", "--socket", sockPath, "--workspace", wsDir); err != nil {
		t.Fatalf("repl /sessions /exit error: %v", err)
	}

	statusOut, _, err := execCLI(t, "", "daemon", "status", "--socket", sockPath)
	if err != nil {
		t.Fatalf("daemon status (running) error: %v", err)
	}
	if !strings.Contains(statusOut, "Daemon running") || !strings.Contains(statusOut, "workspaces=1") {
		t.Errorf("daemon status (running) output = %q, want 'Daemon running' and 'workspaces=1'", statusOut)
	}

	// 3. Test `daemon stop`
	stopOut, _, err := execCLI(t, "", "daemon", "stop", "--socket", sockPath)
	if err != nil {
		t.Fatalf("daemon stop error: %v", err)
	}
	if !strings.Contains(stopOut, "Daemon stopped.") {
		t.Errorf("daemon stop output = %q, want 'Daemon stopped.'", stopOut)
	}
}

func TestREPLPromptAndSlashCommands(t *testing.T) {
	_, sockPath, wsDir := setupCLITestDaemon(t, nil)

	c, err := client.New(sockPath)
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}

	// 1. Verify starting REPL and running slash commands without sending a prompt creates 0 sessions
	noPromptInput := strings.Join([]string{
		"/sessions",
		"/mode",
		"/mode plan",
		"/history",
		"/changes",
		"/new claude-opus-4-6",
		"/exit",
	}, "\n") + "\n"
	out, errOut, err := execCLI(t, noPromptInput, "repl", "--socket", sockPath, "--workspace", wsDir)
	if err != nil {
		t.Fatalf("repl no-prompt run failed: %v\nerr: %s", err, errOut)
	}
	if !strings.Contains(out, "TaskSmith") {
		t.Errorf("expected TaskSmith header banner in out:\n%s", out)
	}
	initialSessions, err := c.WithCWD(wsDir).ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(initialSessions) != 0 {
		t.Fatalf("expected 0 sessions before first user prompt, got %d", len(initialSessions))
	}

	// 2. Create a session via client, then resume and mutate it via CLI repl flags
	sess, err := c.WithCWD(wsDir).CreateSession(context.Background(), api.CreateSessionRequest{
		Title: "Initial Session",
		Model: "claude-sonnet-4-6",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	resumeOut, resumeErr, err := execCLI(t, "/exit\n",
		"--socket", sockPath,
		"--workspace", wsDir,
		"--session", sess.ID,
		"--title", "Resumed Mutated Session",
		"--model", "claude-haiku-4-5",
	)
	if err != nil {
		t.Fatalf("root command resume failed: %v\nerr:\n%s", err, resumeErr)
	}
	if !strings.Contains(resumeOut, "TaskSmith") {
		t.Errorf("resumed session stdout missing header banner:\n%s", resumeOut)
	}

	// Verify model and title were mutated on resume
	resumedSess, err := c.WithCWD(wsDir).GetSession(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("GetSession sess.ID: %v", err)
	}
	if resumedSess.Model != "claude-haiku-4-5" {
		t.Errorf("resumed session model = %q, want 'claude-haiku-4-5'", resumedSess.Model)
	}
	if resumedSess.Title != "Resumed Mutated Session" {
		t.Errorf("resumed session title = %q, want 'Resumed Mutated Session'", resumedSess.Title)
	}
}

func TestDaemonServeCommandLifecycle(t *testing.T) {
	xdgRoot := t.TempDir()
	xdg.SetTestEnv(xdgRoot, "tasksmith-cli-serve-test")
	t.Cleanup(func() {
		xdg.ClearCache()
	})

	shortTmp, err := os.MkdirTemp("/tmp", "tsserve-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(shortTmp)
	})
	sockPath := filepath.Join(shortTmp, "s.sock")
	lockPath := filepath.Join(shortTmp, "s.lock")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serveDone := make(chan error, 1)
	go func() {
		cmd := NewRootCmd()
		cmd.SetArgs([]string{"daemon", "serve", "--socket", sockPath, "--lock", lockPath})
		serveDone <- cmd.ExecuteContext(ctx)
	}()

	c, err := client.New(sockPath)
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, hErr := c.Health(ctx); hErr == nil {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}

	if _, hErr := c.Health(ctx); hErr != nil {
		t.Fatalf("daemon serve did not become healthy: %v", hErr)
	}

	cancel()
	select {
	case sErr := <-serveDone:
		if sErr != nil {
			t.Errorf("daemon serve returned error on cancel: %v", sErr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for daemon serve to exit")
	}
}
