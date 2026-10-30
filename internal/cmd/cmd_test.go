package cmd

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/masterkeysrd/kite/backend"
	"github.com/masterkeysrd/kite/backend/mock"
	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/loom/decision"
	"github.com/masterkeysrd/loom/embedding"
	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/loom/profile"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
	"github.com/masterkeysrd/tasksmith/internal/cli"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/daemon"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway"
	"github.com/masterkeysrd/tasksmith/internal/daemon/models"
	"github.com/spf13/cobra"
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

func (g *fakeGateway) ListProviders() []gateway.ProviderInfo { return nil }
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

func (g *fakeGateway) Close() error { return nil }

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

func setupCLITestDaemon(t *testing.T, prov *fakeProvider) (*daemon.Daemon, string, string) {
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
		_ = srv.Close(context.Background())
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

	// Open a workspace so status lists 1 open workspace (with 0 sessions created)
	c, err := client.New(sockPath, client.WithCWD(wsDir))
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}
	if _, err := c.GetWorkspace(context.Background()); err != nil {
		t.Fatalf("GetWorkspace: %v", err)
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

func feedMockREPLInput(b *mock.Backend, lines ...string) {
	go func() {
		time.Sleep(100 * time.Millisecond)
		for _, line := range lines {
			for _, r := range line {
				b.PostEvent(&backend.RawKeyEvent{
					Key: key.Key{Code: r, Text: string(r)},
				})
				time.Sleep(2 * time.Millisecond)
			}
			b.PostEvent(&backend.RawKeyEvent{
				Key: key.Key{Code: key.KeyEnter},
			})
			time.Sleep(50 * time.Millisecond)
		}
	}()
}

func TestREPLPromptAndSlashCommands(t *testing.T) {
	_, sockPath, wsDir := setupCLITestDaemon(t, nil)

	c, err := client.New(sockPath, client.WithCWD(wsDir))
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}

	// 1. Verify starting REPL and running slash commands without sending a prompt creates 0 sessions
	b1 := mock.New(80, 24)
	feedMockREPLInput(b1,
		"/mode plan",
		"/new claude-opus-4-6",
		"/exit",
	)
	if err := cli.Run(context.Background(), cli.Options{
		Client:    c,
		Workspace: wsDir,
		Backend:   b1,
	}); err != nil {
		t.Fatalf("repl no-prompt run failed: %v", err)
	}
	if sess := cli.ActiveSession(); sess != nil {
		t.Errorf("expected nil ActiveSession() after exiting draft REPL, got %+v", sess)
	}
	initialSessions, err := c.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(initialSessions) != 0 {
		t.Fatalf("expected 0 sessions before first user prompt, got %d", len(initialSessions))
	}

	// 2. Create a session via client, then resume and mutate it via repl.Options
	sess, err := c.CreateSession(context.Background(), api.CreateSessionRequest{
		Title: "Initial Session",
		Config: api.SessionConfig{
			Model: "claude-sonnet-4-6",
		},
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	b2 := mock.New(80, 24)
	feedMockREPLInput(b2, "/exit")
	if err := cli.Run(context.Background(), cli.Options{
		Client:    c,
		Workspace: wsDir,
		SessionID: sess.ID,
		Title:     "Resumed Mutated Session",
		Model:     "claude-haiku-4-5",
		Backend:   b2,
	}); err != nil {
		t.Fatalf("repl resume failed: %v", err)
	}
	if len(b2.InsertedAbove) == 0 {
		t.Errorf("resumed session missing header banner in InsertedAbove")
	}

	// Verify model and title were mutated on resume
	resumedSess, err := c.GetSession(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("GetSession sess.ID: %v", err)
	}
	if resumedSess.Config.Model != "claude-haiku-4-5" {
		t.Errorf("resumed session model = %q, want 'claude-haiku-4-5'", resumedSess.Config.Model)
	}
	if resumedSess.Title != "Resumed Mutated Session" {
		t.Errorf("resumed session title = %q, want 'Resumed Mutated Session'", resumedSess.Title)
	}
	if active := cli.ActiveSession(); active == nil || active.ID != sess.ID {
		t.Errorf("expected ActiveSession().ID = %q, got %+v", sess.ID, active)
	}

	// 3. Verify runREPL prints resume command summary on exit of active session and prints nothing on draft exit
	rootOpts := &rootOptions{
		workspace: wsDir,
		socket:    sockPath,
	}

	var bufActive bytes.Buffer
	cmdActive := &cobra.Command{}
	cmdActive.SetOut(&bufActive)
	cmdActive.SetContext(context.Background())
	b3 := mock.New(80, 24)
	feedMockREPLInput(b3, "/exit")

	if err := runREPL(cmdActive, rootOpts, &replOptions{sessionID: sess.ID, backend: b3}); err != nil {
		t.Fatalf("runREPL active session: %v", err)
	}
	expectedMsg := fmt.Sprintf("Resume this session with:\n    tasksmith --session %s", sess.ID)
	if !strings.Contains(bufActive.String(), expectedMsg) {
		t.Errorf("expected stdout to contain %q, got:\n%s", expectedMsg, bufActive.String())
	}

	var bufDraft bytes.Buffer
	cmdDraft := &cobra.Command{}
	cmdDraft.SetOut(&bufDraft)
	cmdDraft.SetContext(context.Background())
	b4 := mock.New(80, 24)
	feedMockREPLInput(b4, "/exit")

	if err := runREPL(cmdDraft, rootOpts, &replOptions{backend: b4}); err != nil {
		t.Fatalf("runREPL draft session: %v", err)
	}
	if strings.Contains(bufDraft.String(), "Resume this session with:") {
		t.Errorf("expected no resume message on draft session exit, got:\n%s", bufDraft.String())
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
