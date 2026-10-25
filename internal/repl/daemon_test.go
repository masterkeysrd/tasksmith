package repl

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
	"github.com/masterkeysrd/tasksmith/internal/repl/store"
)

type testDaemonState struct {
	mu       sync.Mutex
	sessions map[string]api.Session
	history  map[string][]api.Message
	modes    map[string]api.PermissionMode
	sent     []api.SendRequest
	cancels  []string
	sseCh    chan api.Event
	nextID   int
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

	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	st := &testDaemonState{
		sessions: map[string]api.Session{
			"s1": {
				ID:    "s1",
				Title: "Initial",
				Config: api.SessionConfig{
					Agent: "build",
					Model: "claude-sonnet-4-6",
					Mode:  api.ModeDefault,
				},
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
						{Type: api.BlockText, Text: "Hello REPL2"},
					},
				},
			},
		},
		modes: map[string]api.PermissionMode{
			"s1": api.ModeDefault,
		},
		sseCh:  make(chan api.Event, 32),
		nextID: 1,
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
		id := req.ID
		if id == "" {
			st.nextID++
			id = fmt.Sprintf("s%d", st.nextID)
		}
		model := req.Config.Model
		if model == "" {
			model = "claude-sonnet-4-6"
		}
		agent := req.Config.Agent
		if agent == "" {
			agent = "build"
		}
		mode := req.Config.Mode
		if mode == "" {
			mode = api.ModeDefault
		}
		sess := api.Session{
			ID:    id,
			Title: req.Title,
			Config: api.SessionConfig{
				Agent:  agent,
				Model:  model,
				Effort: req.Config.Effort,
				Mode:   mode,
			},
			Status:    api.StatusIdle,
			UpdatedAt: time.Date(2026, 10, 24, 12, 5, 0, 0, time.UTC),
		}
		st.sessions[id] = sess
		st.modes[id] = mode
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
		if req.Config.Agent != "" {
			s.Config.Agent = req.Config.Agent
		}
		if req.Config.Model != "" {
			s.Config.Model = req.Config.Model
		}
		if req.Config.Effort != "" {
			s.Config.Effort = req.Config.Effort
		}
		if req.Config.Mode != "" {
			s.Config.Mode = req.Config.Mode
		}
		st.sessions[id] = s
		_ = json.NewEncoder(w).Encode(s)
	})

	mux.HandleFunc("GET /v1/sessions/{id}/timeline", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		st.mu.Lock()
		defer st.mu.Unlock()
		msgs := st.history[id]
		entries := make([]api.TimelineEntry, len(msgs))
		for i, m := range msgs {
			msg := m
			entries[i] = api.TimelineEntry{
				ID:        msg.ID,
				SessionID: id,
				Kind:      api.TimelineKindMessage,
				Message:   &msg,
			}
		}
		_ = json.NewEncoder(w).Encode(entries)
	})

	mux.HandleFunc("POST /v1/sessions/{id}/send", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		var req api.SendRequest
		_ = json.NewDecoder(r.Body).Decode(&req)

		st.mu.Lock()
		st.sent = append(st.sent, req)
		if req.Content != "" {
			st.history[id] = append(st.history[id], api.Message{
				ID:      req.ID,
				Role:    api.RoleUser,
				Content: []api.Block{{Type: api.BlockText, Text: req.Content}},
			})
		}
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

func TestStoreInitSessionAndSendAndCancel(t *testing.T) {
	c, st := startTestUnixDaemon(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	store.ResetApp()
	store.ResetStream()
	store.ClearTranscript()
	store.ResetSession()

	store.InitSession(ctx, c, store.InitSessionOptions{
		Agent: "build",
		Model: "claude-sonnet-4-6",
	})

	// Initial state
	if store.GetStream().Streaming {
		t.Fatal("expected not streaming initially")
	}

	// Send message
	store.Send("Hello world")

	// Verify pending user message in stream
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		st.mu.Lock()
		sentCount := len(st.sent)
		st.mu.Unlock()
		if sentCount > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	st.mu.Lock()
	if len(st.sent) == 0 {
		st.mu.Unlock()
		t.Fatal("expected message sent to daemon")
	}
	req := st.sent[0]
	st.mu.Unlock()

	if req.ID == "" {
		t.Fatal("expected non-empty client message ID")
	}
	if req.Content != "Hello world" {
		t.Fatalf("expected 'Hello world', got %q", req.Content)
	}

	// Stream should be streaming
	if !store.GetStream().Streaming {
		t.Fatal("expected store stream to be streaming")
	}

	// Cancel
	store.Cancel()

	for time.Now().Before(deadline) {
		st.mu.Lock()
		cancelCount := len(st.cancels)
		st.mu.Unlock()
		if cancelCount > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	st.mu.Lock()
	if len(st.cancels) == 0 {
		st.mu.Unlock()
		t.Fatal("expected cancel sent to daemon")
	}
	st.mu.Unlock()
}
