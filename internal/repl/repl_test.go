package repl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/masterkeysrd/kite/backend"
	"github.com/masterkeysrd/kite/backend/mock"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
	"github.com/masterkeysrd/tasksmith/internal/repl/command"
)

type testDaemonState struct {
	mu          sync.Mutex
	sessions    map[string]api.Session
	history     map[string][]api.Message
	modes       map[string]api.PermissionMode
	changes     map[string][]api.FileChange
	diffs       map[string]string
	permissions map[string][]api.PermissionRequest
	decisions   []api.PermissionDecision
	reverts     []api.RevertRequest
	cancels     []string
	sent        []api.SendRequest
	sseCh       chan api.Event
	nextID      int
}

func startTestUnixDaemon(t *testing.T) (*client.Client, *testDaemonState) {
	t.Helper()

	dir, err := os.MkdirTemp("/tmp", "tsrepl-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(dir)
	})

	sockPath := filepath.Join(dir, "d.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("net.Listen unix: %v", err)
	}
	t.Cleanup(func() {
		_ = ln.Close()
	})

	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	st := &testDaemonState{
		sessions: map[string]api.Session{
			"s1": {
				ID:        "s1",
				Title:     "Initial",
				Agent:     "build",
				Model:     "claude-sonnet-4-6",
				Status:    api.StatusIdle,
				CreatedAt: now,
				UpdatedAt: now,
			},
		},
		history: map[string][]api.Message{
			"s1": {
				{
					ID:   "m1",
					Role: api.RoleUser,
					Content: []api.Block{
						{Type: api.BlockText, Text: "Hello REPL"},
					},
				},
				{
					ID:   "m2",
					Role: api.RoleAssistant,
					Content: []api.Block{
						{Type: api.BlockReasoning, Text: "Thinking..."},
						{Type: api.BlockText, Text: "Hi there!"},
					},
				},
			},
		},
		modes: map[string]api.PermissionMode{
			"s1": api.ModeDefault,
		},
		changes: map[string][]api.FileChange{
			"s1": {
				{
					ID:        "e1",
					Path:      "main.go",
					Kind:      "modify",
					Additions: 3,
					Deletions: 1,
					Timestamp: now,
				},
			},
		},
		diffs: map[string]string{
			"main.go:e1": "--- a/main.go\n+++ b/main.go\n@@ -1,2 +1,4 @@\n-old\n+new\n",
		},
		permissions: make(map[string][]api.PermissionRequest),
		sseCh:       make(chan api.Event, 32),
		nextID:      1,
	}

	mux := http.NewServeMux()
	registerTestDaemonRoutes(mux, st)

	srv := &http.Server{Handler: mux}
	go func() {
		_ = srv.Serve(ln)
	}()
	t.Cleanup(func() {
		close(st.sseCh)
		_ = srv.Close()
	})

	c, err := client.New(sockPath, client.WithCWD("/workspace/project"))
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}
	return c, st
}

func registerTestDaemonRoutes(mux *http.ServeMux, st *testDaemonState) {
	mux.HandleFunc("GET /v1/workspace", func(w http.ResponseWriter, r *http.Request) {
		cwd := r.Header.Get(api.HeaderCWD)
		if cwd == "" {
			cwd = "/workspace/project"
		}
		_ = json.NewEncoder(w).Encode(api.Workspace{
			Name:         "test-workspace",
			RootDir:      "/workspace/project",
			CWD:          cwd,
			DefaultAgent: "build",
			DefaultModel: "claude-sonnet-4-6",
		})
	})
	mux.HandleFunc("GET /v1/agents", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]api.Agent{{Name: "build", Models: []string{"claude-sonnet-4-6"}, Effort: "high"}})
	})
	mux.HandleFunc("GET /v1/agents/{name}", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(api.Agent{
			Name:   r.PathValue("name"),
			Models: []string{"claude-sonnet-4-6"},
			Effort: "high",
		})
	})
	mux.HandleFunc("GET /v1/models", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]api.Model{{ID: "claude-sonnet-4-6", Name: "claude-sonnet-4-6", Label: "Claude 3.7 Sonnet", DefaultEffort: "medium"}})
	})
	mux.HandleFunc("GET /v1/models/{id...}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		_ = json.NewEncoder(w).Encode(api.Model{
			ID:            id,
			Name:          id,
			Label:         "Claude 3.7 Sonnet",
			DefaultEffort: "medium",
		})
	})

	mux.HandleFunc("POST /v1/sessions", func(w http.ResponseWriter, r *http.Request) {
		var req api.CreateSessionRequest
		_ = json.NewDecoder(r.Body).Decode(&req)

		st.mu.Lock()
		st.nextID++
		id := fmt.Sprintf("s%d", st.nextID)
		model := req.Model
		if model == "" {
			model = "default-model"
		}
		agent := req.Agent
		if agent == "" {
			agent = "build"
		}
		sess := api.Session{
			ID:        id,
			Title:     req.Title,
			Agent:     agent,
			Model:     model,
			Status:    api.StatusIdle,
			UpdatedAt: time.Date(2026, 10, 21, 12, 5, 0, 0, time.UTC),
		}
		st.sessions[id] = sess
		st.modes[id] = api.ModeDefault
		st.mu.Unlock()

		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(sess)
	})

	mux.HandleFunc("GET /v1/sessions", func(w http.ResponseWriter, _ *http.Request) {
		st.mu.Lock()
		defer st.mu.Unlock()
		var list []api.Session
		for _, s := range st.sessions {
			list = append(list, s)
		}
		_ = json.NewEncoder(w).Encode(list)
	})

	mux.HandleFunc("GET /v1/sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		st.mu.Lock()
		defer st.mu.Unlock()
		s, ok := st.sessions[id]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(s)
	})

	mux.HandleFunc("PATCH /v1/sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		var req api.UpdateSessionRequest
		_ = json.NewDecoder(r.Body).Decode(&req)

		st.mu.Lock()
		defer st.mu.Unlock()
		s, ok := st.sessions[id]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if req.Title != "" {
			s.Title = req.Title
		}
		if req.Agent != "" {
			s.Agent = req.Agent
		}
		if req.Model != "" {
			s.Model = req.Model
		}
		st.sessions[id] = s
		_ = json.NewEncoder(w).Encode(s)
	})

	mux.HandleFunc("DELETE /v1/sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		st.mu.Lock()
		delete(st.sessions, id)
		delete(st.history, id)
		delete(st.modes, id)
		delete(st.changes, id)
		st.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})

	getHistory := func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		st.mu.Lock()
		defer st.mu.Unlock()
		msgs := st.history[id]
		if msgs == nil {
			msgs = []api.Message{}
		}
		_ = json.NewEncoder(w).Encode(msgs)
	}
	mux.HandleFunc("GET /v1/sessions/{id}/history", getHistory)
	mux.HandleFunc("GET /v1/sessions/{id}/messages", getHistory)

	mux.HandleFunc("POST /v1/sessions/{id}/messages", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		var req api.SendRequest
		_ = json.NewDecoder(r.Body).Decode(&req)

		st.mu.Lock()
		st.sent = append(st.sent, req)
		st.history[id] = append(st.history[id], api.Message{
			ID:      fmt.Sprintf("m-%d", len(st.sent)),
			Role:    api.RoleUser,
			Content: []api.Block{{Type: api.BlockText, Text: req.Content}},
		})
		st.mu.Unlock()

		w.WriteHeader(http.StatusAccepted)
	})

	mux.HandleFunc("POST /v1/sessions/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		st.mu.Lock()
		st.cancels = append(st.cancels, id)
		st.mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
	})

	mux.HandleFunc("GET /v1/sessions/{id}/permissions", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		st.mu.Lock()
		defer st.mu.Unlock()
		perms := st.permissions[id]
		if perms == nil {
			perms = []api.PermissionRequest{}
		}
		_ = json.NewEncoder(w).Encode(perms)
	})

	respondPerm := func(w http.ResponseWriter, r *http.Request) {
		var dec api.PermissionDecision
		_ = json.NewDecoder(r.Body).Decode(&dec)

		st.mu.Lock()
		st.decisions = append(st.decisions, dec)
		st.mu.Unlock()

		w.WriteHeader(http.StatusAccepted)
	}
	mux.HandleFunc("POST /v1/sessions/{id}/permissions", respondPerm)
	mux.HandleFunc("POST /v1/sessions/{id}/permissions/respond", respondPerm)

	mux.HandleFunc("GET /v1/sessions/{id}/mode", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		st.mu.Lock()
		defer st.mu.Unlock()
		m := st.modes[id]
		if m == "" {
			m = api.ModeDefault
		}
		_ = json.NewEncoder(w).Encode(m)
	})

	mux.HandleFunc("PUT /v1/sessions/{id}/mode", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		var m api.PermissionMode
		_ = json.NewDecoder(r.Body).Decode(&m)

		st.mu.Lock()
		st.modes[id] = m
		st.mu.Unlock()

		_ = json.NewEncoder(w).Encode(m)
	})

	mux.HandleFunc("GET /v1/sessions/{id}/changes", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		st.mu.Lock()
		defer st.mu.Unlock()
		changes := st.changes[id]
		if changes == nil {
			changes = []api.FileChange{}
		}
		_ = json.NewEncoder(w).Encode(changes)
	})

	mux.HandleFunc("GET /v1/sessions/{id}/diff", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Query().Get("path")
		entryID := r.URL.Query().Get("entry_id")
		key := fmt.Sprintf("%s:%s", path, entryID)

		st.mu.Lock()
		diff := st.diffs[key]
		st.mu.Unlock()

		_ = json.NewEncoder(w).Encode(diff)
	})

	mux.HandleFunc("POST /v1/sessions/{id}/revert", func(w http.ResponseWriter, r *http.Request) {
		var req api.RevertRequest
		_ = json.NewDecoder(r.Body).Decode(&req)

		st.mu.Lock()
		st.reverts = append(st.reverts, req)
		st.mu.Unlock()

		w.WriteHeader(http.StatusAccepted)
	})

	mux.HandleFunc("GET /v1/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "no flusher", http.StatusInternalServerError)
			return
		}
		flusher.Flush()

		for {
			select {
			case <-r.Context().Done():
				return
			case ev, open := <-st.sseCh:
				if !open {
					return
				}
				data, err := json.Marshal(ev)
				if err != nil {
					continue
				}
				_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()
			}
		}
	})
}

func insertedAboveText(b *mock.Backend) string {
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
	return sb.String()
}

func TestConfigValidate(t *testing.T) {
	c, _ := startTestUnixDaemon(t)

	valid := Config{
		Client:    c,
		Workspace: "/tmp/ws",
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("expected valid Config, got %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*Config)
	}{
		{"nil client", func(cfg *Config) { cfg.Client = nil }},
		{"empty workspace", func(cfg *Config) { cfg.Workspace = "   " }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := valid
			tc.mutate(&cfg)
			if err := cfg.Validate(); !errors.Is(err, ErrInvalidConfig) {
				t.Errorf("Validate() error = %v, want %v", err, ErrInvalidConfig)
			}
			if err := Run(context.Background(), cfg); !errors.Is(err, ErrInvalidConfig) {
				t.Errorf("Run() error = %v, want %v", err, ErrInvalidConfig)
			}
		})
	}
}

func feedMockCommands(b *mock.Backend, lines ...string) {
	go func() {
		time.Sleep(100 * time.Millisecond)
		for _, line := range lines {
			for _, r := range line {
				b.PostEvent(&backend.RawKeyEvent{
					Key: key.Key{Code: r, Text: string(r)},
				})
				time.Sleep(2 * time.Millisecond)
			}
			// These tests submit complete commands rather than accepting suggestions.
			time.Sleep(25 * time.Millisecond)
			b.PostEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyEscape}})
			time.Sleep(25 * time.Millisecond)
			b.PostEvent(&backend.RawKeyEvent{
				Key: key.Key{Code: key.KeyEnter},
			})
			time.Sleep(50 * time.Millisecond)
			b.PostEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyEscape}})
			time.Sleep(50 * time.Millisecond)
		}
	}()
}

func TestLazySessionCreationNoPrompts(t *testing.T) {
	c, st := startTestUnixDaemon(t)
	st.mu.Lock()
	clear(st.sessions)
	st.mu.Unlock()

	b := mock.New(80, 24)
	feedMockCommands(b,
		"/sessions",
		"/mode",
		"/mode plan",
		"/changes",
		"/new claude-opus-4-6",
		"/exit",
	)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	err := Run(ctx, Config{
		Client:    c,
		Workspace: "/tmp/ws",
		Agent:     "build",
		Model:     "claude-sonnet-4-6",
		Title:     "Lazy Test",
		Backend:   b,
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	st.mu.Lock()
	sessionCount := len(st.sessions)
	st.mu.Unlock()
	if sessionCount != 0 {
		t.Fatalf("expected 0 sessions created when no prompt was sent, got %d", sessionCount)
	}
}

func TestRunResumeSessionWithMutation(t *testing.T) {
	c, st := startTestUnixDaemon(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	b := mock.New(80, 24)
	feedMockCommands(b, "/exit")

	cfg := Config{
		Client:    c,
		Workspace: "/workspace/project",
		SessionID: "s1",
		Title:     "Mutated Title",
		Agent:     "build",
		Model:     "claude-haiku-4-5",
		Backend:   b,
	}

	if err := Run(ctx, cfg); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	st.mu.Lock()
	sess := st.sessions["s1"]
	st.mu.Unlock()

	if sess.Title != "Mutated Title" {
		t.Errorf("session title = %q, want 'Mutated Title'", sess.Title)
	}
	if sess.Model != "claude-haiku-4-5" {
		t.Errorf("session model = %q, want 'claude-haiku-4-5'", sess.Model)
	}
}

func TestAppFunctionalComponentLifecycle(t *testing.T) {
	c, st := startTestUnixDaemon(t)
	ctx := t.Context()

	sess, err := c.GetSession(ctx, "s1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}

	b := mock.New(80, 24)
	props := AppProps{
		Client:            c,
		Workspace:         "/workspace/project",
		Version:           "v0.1.0",
		SessionID:         "s1",
		InitialSess:       sess,
		InitialMode:       api.ModeDefault,
		InitialModelLabel: "Claude 3.7 Sonnet",
		InitialThinking:   "high",
		Registry:          command.NewRegistry(),
	}

	inst, err := trail.New(App(props), trail.WithBackend(b))
	if err != nil {
		t.Fatalf("trail.New: %v", err)
	}

	runDone := make(chan error, 1)
	go func() {
		runDone <- inst.Run(ctx)
	}()

	time.Sleep(50 * time.Millisecond)

	// 1. Submit prompt via key events
	for _, ch := range "Hello assistant" {
		b.PostEvent(&backend.RawKeyEvent{Key: key.Key{Code: ch, Text: string(ch)}})
		time.Sleep(2 * time.Millisecond)
	}
	b.PostEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyEnter}})
	time.Sleep(50 * time.Millisecond)

	st.mu.Lock()
	sentCount := len(st.sent)
	st.mu.Unlock()
	if sentCount == 0 {
		t.Errorf("expected SendRequest to daemon, got 0")
	}

	// The daemon echoes the persisted user message over SSE.
	st.sseCh <- api.Event{
		Type:      api.EventMessageAdded,
		SessionID: "s1",
		Message: &api.Message{
			ID:      "m3",
			Role:    api.RoleUser,
			Content: []api.Block{{Type: api.BlockText, Text: "Hello assistant"}},
		},
	}
	time.Sleep(50 * time.Millisecond)

	// 2. Stream chunk via SSE
	st.sseCh <- api.Event{
		Type:      api.EventChunk,
		SessionID: "s1",
		Chunk: &api.Chunk{
			ID: "c1",
			Content: []api.Block{
				{Type: api.BlockReasoning, Text: "Analyzing request"},
				{Type: api.BlockText, Text: "Working on it"},
			},
		},
	}
	time.Sleep(50 * time.Millisecond)

	// 3. Permission request via SSE
	st.sseCh <- api.Event{
		Type:      api.EventPermissionRequested,
		SessionID: "s1",
		Permissions: []api.PermissionRequest{
			{
				ID:         "p1",
				ToolCallID: "tc-write",
				Action:     api.ActionWrite,
				Safety:     api.SafetySafe,
				ToolName:   "write_file",
				Summary:    "Write test.go",
			},
		},
	}
	time.Sleep(50 * time.Millisecond)

	// Press 'y' to allow once
	b.PostEvent(&backend.RawKeyEvent{Key: key.Key{Code: 'y', Text: "y"}})
	time.Sleep(50 * time.Millisecond)

	st.mu.Lock()
	decCount := len(st.decisions)
	st.mu.Unlock()
	if decCount == 0 || st.decisions[0].Kind != api.DecisionAllowOnce {
		t.Errorf("expected DecisionAllowOnce sent to daemon, got %+v", st.decisions)
	}

	// 4. Complete turn with the persisted assistant message
	st.sseCh <- api.Event{
		Type:      api.EventCompleted,
		SessionID: "s1",
		Message: &api.Message{
			ID:   "m4",
			Role: api.RoleAssistant,
			Content: []api.Block{
				{Type: api.BlockReasoning, Text: "Analyzing request"},
				{Type: api.BlockText, Text: "Working on it"},
			},
		},
	}
	time.Sleep(50 * time.Millisecond)

	// 5. Submit /exit
	for _, ch := range "/exit" {
		b.PostEvent(&backend.RawKeyEvent{Key: key.Key{Code: ch, Text: string(ch)}})
		time.Sleep(2 * time.Millisecond)
	}
	time.Sleep(25 * time.Millisecond)
	b.PostEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyEscape}})
	time.Sleep(25 * time.Millisecond)
	b.PostEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyEnter}})

	select {
	case runErr := <-runDone:
		if runErr != nil {
			t.Fatalf("inst.Run error: %v", runErr)
		}
	case <-time.After(3 * time.Second):
		inst.Exit(nil)
		<-runDone
		t.Error("timed out submitting /exit")
	}

	// Now that inst.Run has exited, safely verify output
	above := insertedAboveText(b)
	if !strings.Contains(above, "TaskSmith v0.1.0") {
		t.Errorf("expected header banner in scrollback, got:\n%s", above)
	}
	if !strings.Contains(above, "Hi there!") {
		t.Errorf("expected loaded history in scrollback, got:\n%s", above)
	}
	if !strings.Contains(above, "Hello assistant") {
		t.Errorf("expected submitted prompt in scrollback, got:\n%s", above)
	}
	if !strings.Contains(above, "Write test.go") {
		t.Errorf("expected decision in scrollback, got:\n%s", above)
	}
}

func TestLiveToolCallStreamingAndReasoningDuration(t *testing.T) {
	c, st := startTestUnixDaemon(t)
	ctx := t.Context()

	sess, err := c.GetSession(ctx, "s1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}

	b := mock.New(100, 30)
	props := AppProps{
		Client:            c,
		Workspace:         "/workspace/project",
		Version:           "v0.1.0",
		SessionID:         "s1",
		InitialSess:       sess,
		InitialMode:       api.ModeDefault,
		InitialModelLabel: "Claude 3.7 Sonnet",
		InitialThinking:   "high",
		Registry:          command.NewRegistry(),
	}

	inst, err := trail.New(App(props), trail.WithBackend(b))
	if err != nil {
		t.Fatalf("trail.New: %v", err)
	}

	runDone := make(chan error, 1)
	go func() {
		runDone <- inst.Run(ctx)
	}()

	time.Sleep(50 * time.Millisecond)

	// 1. Stream reasoning chunk
	st.sseCh <- api.Event{
		Type:      api.EventChunk,
		SessionID: "s1",
		Chunk: &api.Chunk{
			ID: "c1",
			Content: []api.Block{
				{Type: api.BlockReasoning, Text: "planning commands to execute"},
			},
		},
	}
	time.Sleep(30 * time.Millisecond)

	// 2. Assistant message with reasoning + tool call arrives
	st.sseCh <- api.Event{
		Type:      api.EventMessageAdded,
		SessionID: "s1",
		Message: &api.Message{
			ID:   "m3",
			Role: api.RoleAssistant,
			Content: []api.Block{
				{Type: api.BlockReasoning, Text: "planning commands to execute"},
				{Type: api.BlockToolCall, ToolCall: &api.ToolCall{
					ID:   "tc1",
					Name: "bash",
					Args: map[string]any{"command": "echo running-cmd"},
				}},
			},
		},
	}
	time.Sleep(30 * time.Millisecond)

	// 3. Tool completion arrives (success)
	st.sseCh <- api.Event{
		Type:      api.EventMessageAdded,
		SessionID: "s1",
		Message: &api.Message{
			ID:         "m4",
			Role:       api.RoleTool,
			ToolCallID: "tc1",
			IsError:    false,
			Content: []api.Block{
				{Type: api.BlockText, Text: "running-cmd output\nmore output"},
			},
		},
	}
	time.Sleep(30 * time.Millisecond)

	// 4. Now send another assistant message with a failing tool call
	st.sseCh <- api.Event{
		Type:      api.EventMessageAdded,
		SessionID: "s1",
		Message: &api.Message{
			ID:   "m5",
			Role: api.RoleAssistant,
			Content: []api.Block{
				{Type: api.BlockToolCall, ToolCall: &api.ToolCall{
					ID:   "tc2",
					Name: "view",
					Args: map[string]any{"path": "/pkg/missing.go"},
				}},
			},
		},
	}
	time.Sleep(30 * time.Millisecond)

	// 5. Tool completion arrives (error)
	st.sseCh <- api.Event{
		Type:      api.EventMessageAdded,
		SessionID: "s1",
		Message: &api.Message{
			ID:         "m6",
			Role:       api.RoleTool,
			ToolCallID: "tc2",
			IsError:    true,
			Content: []api.Block{
				{Type: api.BlockText, Text: "file not found: missing.go\ndetails here"},
			},
		},
	}
	time.Sleep(30 * time.Millisecond)

	// 6. Complete
	st.sseCh <- api.Event{
		Type:      api.EventCompleted,
		SessionID: "s1",
	}
	time.Sleep(30 * time.Millisecond)

	inst.Exit(nil)
	<-runDone

	above := insertedAboveText(b)
	if !strings.Contains(above, "● Thought (planning commands to execute)") {
		t.Errorf("expected thought in transcript, got:\n%s", above)
	}
	if !strings.Contains(above, "● Ran (echo running-cmd)") {
		t.Errorf("expected finished bash tool call in transcript, got:\n%s", above)
	}
	if strings.Contains(above, "more output") {
		t.Errorf("expected raw tool output to be suppressed, got:\n%s", above)
	}
	if !strings.Contains(above, "● Read failed /pkg/missing.go") {
		t.Errorf("expected finished view tool call in transcript, got:\n%s", above)
	}
	if !strings.Contains(above, "file not found: missing.go") {
		t.Errorf("expected indented error indicator in transcript, got:\n%s", above)
	}
}
