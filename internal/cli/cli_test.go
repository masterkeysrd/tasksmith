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

func execCLI(t *testing.T, stdin string, args ...string) (string, string, error) {
	t.Helper()
	cmd := NewRootCmd()
	var outBuf, errBuf bytes.Buffer
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

	// Verify starting REPL and running slash commands without sending a prompt creates 0 sessions
	noPromptInput := strings.Join([]string{
		"/sessions",
		"/mode",
		"/mode plan",
		"/history",
		"/changes",
		"/new claude-opus-4-6",
		"/exit",
	}, "\n") + "\n"
	if _, _, err := execCLI(t, noPromptInput, "repl", "--socket", sockPath, "--workspace", wsDir); err != nil {
		t.Fatalf("repl no-prompt run failed: %v", err)
	}
	initialSessions, err := c.ListSessions(context.Background(), wsDir)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(initialSessions) != 0 {
		t.Fatalf("expected 0 sessions before first user prompt, got %d", len(initialSessions))
	}

	replInput := strings.Join([]string{
		"Hello TaskSmith!",
		"/history",
		"/sessions",
		"/mode",
		"/mode accept_edits",
		"/mode",
		"/mode invalid_mode",
		"/changes",
		"/new claude-opus-4-6",
		"Second session message",
		"/sessions",
		"/switch",
		"/switch nonexistent-id",
		"/unknown",
		"/exit",
	}, "\n") + "\n"

	out, errOut, err := execCLI(t, replInput,
		"repl",
		"--socket", sockPath,
		"--workspace", wsDir,
		"--title", "CLI Test Session",
	)
	if err != nil {
		t.Fatalf("repl execution failed: %v\nout:\n%s\nerr:\n%s", err, out, errOut)
	}

	wantOutSubstrings := []string{
		"TaskSmith REPL",
		"Hello from the Brain Daemon!",
		"[user] Hello TaskSmith!",
		"[assistant] Hello from the Brain Daemon!",
		"Permission mode: default",
		"Permission mode set to: accept_edits",
		"Permission mode: accept_edits",
		"No tracked file changes.",
		"Switched to new session",
		"model=claude-opus-4-6",
	}
	for _, sub := range wantOutSubstrings {
		if !strings.Contains(out, sub) {
			t.Errorf("REPL stdout missing %q.\nFull stdout:\n%s", sub, out)
		}
	}

	wantErrSubstrings := []string{
		"error setting permission mode",
		"usage: /switch <session_id>",
		"error switching session",
		"unknown command: /unknown",
	}
	for _, sub := range wantErrSubstrings {
		if !strings.Contains(errOut, sub) {
			t.Errorf("REPL stderr missing %q.\nFull stderr:\n%s", sub, errOut)
		}
	}

	// Verify we can list the 2 sessions via client, switch between them, and resume via --session
	sessions, err := c.ListSessions(context.Background(), wsDir)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions created by REPL, got %d", len(sessions))
	}

	var firstSessionID, secondSessionID string
	for _, s := range sessions {
		if s.Model == "claude-opus-4-6" {
			secondSessionID = s.ID
			continue
		}
		firstSessionID = s.ID
	}
	if firstSessionID == "" || secondSessionID == "" {
		t.Fatalf("could not identify first and second session IDs from %+v", sessions)
	}

	// Resume firstSessionID using the root command default (no `repl` subcommand) and `--session`,
	// then `/switch` to secondSessionID and `/quit`.
	resumeInput := strings.Join([]string{
		"/history",
		"/switch " + secondSessionID,
		"/quit",
	}, "\n") + "\n"

	resumeOut, resumeErr, err := execCLI(t, resumeInput,
		"--socket", sockPath,
		"--workspace", wsDir,
		"--session", firstSessionID,
	)
	if err != nil {
		t.Fatalf("root command resume failed: %v\nerr:\n%s", err, resumeErr)
	}
	if !strings.Contains(resumeOut, "[user] Hello TaskSmith!") {
		t.Errorf("resumed session stdout missing prior history:\n%s", resumeOut)
	}
	if !strings.Contains(resumeOut, "Switched to session "+secondSessionID) {
		t.Errorf("resumed session stdout missing switch confirmation:\n%s", resumeOut)
	}
}

func TestREPLInteractivePermissionChangesAndRevert(t *testing.T) {
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
							&message.TextBlock{Text: "Proposing write to note.txt..."},
							&message.ToolCall{
								ID:   "tc-write-note",
								Name: "write",
								Args: map[string]any{
									"path":    "note.txt",
									"content": "modified via repl\n",
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
						&message.TextBlock{Text: "Finished writing note.txt!"},
					},
					Done: true,
				}, nil)
			}, nil
		},
	}

	_, sockPath, wsDir := setupCLITestDaemon(t, prov)

	notePath := filepath.Join(wsDir, "note.txt")
	if err := os.WriteFile(notePath, []byte("original note\n"), 0o644); err != nil {
		t.Fatalf("WriteFile note.txt: %v", err)
	}

	// First REPL run: send prompt -> approve permission ('y') -> inspect /changes -> /exit
	input1 := strings.Join([]string{
		"Please update note.txt",
		"y",
		"/changes",
		"/exit",
	}, "\n") + "\n"

	out1, errOut1, err := execCLI(t, input1, "repl", "--socket", sockPath, "--workspace", wsDir)
	if err != nil {
		t.Fatalf("repl permission run failed: %v\nerr:\n%s", err, errOut1)
	}

	for _, want := range []string{
		"Proposing write to note.txt...",
		"[Permission Required]",
		"Allow? [y]es once / [a]llow session / [n]o (reject):",
		"Finished writing note.txt!",
		"note.txt",
	} {
		if !strings.Contains(out1, want) {
			t.Errorf("out1 missing %q.\nFull out1:\n%s", want, out1)
		}
	}

	// Verify file on disk has been modified
	gotBytes, err := os.ReadFile(notePath)
	if err != nil {
		t.Fatalf("ReadFile note.txt: %v", err)
	}
	if string(gotBytes) != "modified via repl\n" {
		t.Fatalf("note.txt after approval = %q, want 'modified via repl\\n'", string(gotBytes))
	}

	// Find session ID to test `/revert note.txt` and `/revert`
	c, err := client.New(sockPath)
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}
	sessions, err := c.ListSessions(context.Background(), wsDir)
	if err != nil || len(sessions) != 1 {
		t.Fatalf("ListSessions = %+v, %v", sessions, err)
	}

	input2 := strings.Join([]string{
		"/revert note.txt",
		"/revert",
		"/exit",
	}, "\n") + "\n"

	out2, errOut2, err := execCLI(t, input2,
		"repl",
		"--socket", sockPath,
		"--workspace", wsDir,
		"--session", sessions[0].ID,
	)
	if err != nil {
		t.Fatalf("repl revert run failed: %v\nerr:\n%s", err, errOut2)
	}
	if !strings.Contains(out2, "Reverted note.txt.") || !strings.Contains(out2, "Reverted all session file changes.") {
		t.Errorf("out2 missing revert confirmations:\n%s", out2)
	}

	revertedBytes, err := os.ReadFile(notePath)
	if err != nil {
		t.Fatalf("ReadFile note.txt after revert: %v", err)
	}
	if string(revertedBytes) != "original note\n" {
		t.Errorf("note.txt after /revert = %q, want 'original note\\n'", string(revertedBytes))
	}
}

func TestParsePermissionInputVariants(t *testing.T) {
	perm := api.PermissionRequest{
		ID:            "perm-1",
		ToolCallID:    "tc-1",
		GrantPatterns: []string{"write:**"},
	}

	dOnce := parsePermissionInput(perm, "yes")
	if dOnce.Kind != api.DecisionAllowOnce {
		t.Errorf("parsePermissionInput('yes').Kind = %q, want %q", dOnce.Kind, api.DecisionAllowOnce)
	}

	dSession := parsePermissionInput(perm, "a")
	if dSession.Kind != api.DecisionAllowSession || dSession.GrantPattern != "write:**" {
		t.Errorf("parsePermissionInput('a') = %+v, want DecisionAllowSession with GrantPattern 'write:**'", dSession)
	}

	dReject1 := parsePermissionInput(perm, "n do not touch")
	if dReject1.Kind != api.DecisionReject || dReject1.Feedback != "do not touch" {
		t.Errorf("parsePermissionInput('n do not touch') = %+v, want DecisionReject with feedback", dReject1)
	}

	dReject2 := parsePermissionInput(perm, "no wrong file")
	if dReject2.Kind != api.DecisionReject || dReject2.Feedback != "wrong file" {
		t.Errorf("parsePermissionInput('no wrong file') = %+v, want DecisionReject with feedback", dReject2)
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
