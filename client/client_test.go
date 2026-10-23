package client

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
	"testing"

	"github.com/masterkeysrd/tasksmith/api"
)

func TestNewAndValidation(t *testing.T) {
	if _, err := New("   "); !errors.Is(err, ErrInvalidSocketPath) {
		t.Fatalf("New empty socketPath error = %v, want %v", err, ErrInvalidSocketPath)
	}

	customHC := &http.Client{}
	c, err := New("/tmp/brain.sock", WithHTTPClient(customHC), WithCWD("/tmp/init-ws"))
	if err != nil {
		t.Fatalf("New error: %v", err)
	}
	if c.SocketPath() != "/tmp/brain.sock" {
		t.Errorf("SocketPath() = %q, want \"/tmp/brain.sock\"", c.SocketPath())
	}
	if c.CWD() != "/tmp/init-ws" {
		t.Errorf("CWD() = %q, want \"/tmp/init-ws\"", c.CWD())
	}
	if c.httpClient != customHC {
		t.Errorf("expected custom HTTP client to be set")
	}

	switched := c.WithCWD("/tmp/switched-ws")
	if switched.CWD() != "/tmp/switched-ws" {
		t.Errorf("switched.CWD() = %q, want \"/tmp/switched-ws\"", switched.CWD())
	}
	if c.CWD() != "/tmp/init-ws" {
		t.Errorf("c.CWD() should remain \"/tmp/init-ws\", got %q", c.CWD())
	}

	// Empty CWD client validation
	emptyCWDClient, err := New("/tmp/brain.sock")
	if err != nil {
		t.Fatalf("New emptyCWDClient error: %v", err)
	}
	ctx := context.Background()

	if _, err := emptyCWDClient.GetWorkspace(ctx); !errors.Is(err, ErrEmptyWorkspace) {
		t.Errorf("GetWorkspace empty CWD error = %v, want %v", err, ErrEmptyWorkspace)
	}
	if _, err := emptyCWDClient.ListAgents(ctx); !errors.Is(err, ErrEmptyWorkspace) {
		t.Errorf("ListAgents empty CWD error = %v, want %v", err, ErrEmptyWorkspace)
	}
	if _, err := emptyCWDClient.GetAgent(ctx, "dev"); !errors.Is(err, ErrEmptyWorkspace) {
		t.Errorf("GetAgent empty CWD error = %v, want %v", err, ErrEmptyWorkspace)
	}
	if _, err := emptyCWDClient.ListModels(ctx); !errors.Is(err, ErrEmptyWorkspace) {
		t.Errorf("ListModels empty CWD error = %v, want %v", err, ErrEmptyWorkspace)
	}
	if _, err := emptyCWDClient.GetModel(ctx, "m1"); !errors.Is(err, ErrEmptyWorkspace) {
		t.Errorf("GetModel empty CWD error = %v, want %v", err, ErrEmptyWorkspace)
	}
	if _, err := emptyCWDClient.CreateSession(ctx, api.CreateSessionRequest{}); !errors.Is(err, ErrEmptyWorkspace) {
		t.Errorf("CreateSession empty CWD error = %v, want %v", err, ErrEmptyWorkspace)
	}
	if _, err := emptyCWDClient.ListSessions(ctx); !errors.Is(err, ErrEmptyWorkspace) {
		t.Errorf("ListSessions empty CWD error = %v, want %v", err, ErrEmptyWorkspace)
	}

	// With CWD configured, test empty parameter checks
	withCWD := emptyCWDClient.WithCWD("/tmp/ws")
	if _, err := withCWD.GetAgent(ctx, ""); err == nil {
		t.Errorf("GetAgent empty name expected error, got nil")
	}
	if _, err := withCWD.GetModel(ctx, ""); err == nil {
		t.Errorf("GetModel empty ID expected error, got nil")
	}
	if _, err := withCWD.GetSession(ctx, ""); !errors.Is(err, ErrEmptySessionID) {
		t.Errorf("GetSession empty sessionID error = %v, want %v", err, ErrEmptySessionID)
	}
	if err := withCWD.DeleteSession(ctx, ""); !errors.Is(err, ErrEmptySessionID) {
		t.Errorf("DeleteSession empty sessionID error = %v, want %v", err, ErrEmptySessionID)
	}
	if _, err := withCWD.History(ctx, ""); !errors.Is(err, ErrEmptySessionID) {
		t.Errorf("History empty sessionID error = %v, want %v", err, ErrEmptySessionID)
	}
	if err := withCWD.Send(ctx, "", api.SendRequest{}); !errors.Is(err, ErrEmptySessionID) {
		t.Errorf("Send empty sessionID error = %v, want %v", err, ErrEmptySessionID)
	}
	if err := withCWD.Cancel(ctx, ""); !errors.Is(err, ErrEmptySessionID) {
		t.Errorf("Cancel empty sessionID error = %v, want %v", err, ErrEmptySessionID)
	}
	if _, err := withCWD.PendingPermissions(ctx, ""); !errors.Is(err, ErrEmptySessionID) {
		t.Errorf("PendingPermissions empty sessionID error = %v, want %v", err, ErrEmptySessionID)
	}
	if err := withCWD.RespondPermission(ctx, "", api.PermissionDecision{}); !errors.Is(err, ErrEmptySessionID) {
		t.Errorf("RespondPermission empty sessionID error = %v, want %v", err, ErrEmptySessionID)
	}
	if _, err := withCWD.PermissionMode(ctx, ""); !errors.Is(err, ErrEmptySessionID) {
		t.Errorf("PermissionMode empty sessionID error = %v, want %v", err, ErrEmptySessionID)
	}
	if err := withCWD.SetPermissionMode(ctx, "", api.ModeAuto); !errors.Is(err, ErrEmptySessionID) {
		t.Errorf("SetPermissionMode empty sessionID error = %v, want %v", err, ErrEmptySessionID)
	}
	if _, err := withCWD.Changes(ctx, ""); !errors.Is(err, ErrEmptySessionID) {
		t.Errorf("Changes empty sessionID error = %v, want %v", err, ErrEmptySessionID)
	}
	if _, err := withCWD.DiffChange(ctx, "", "a.go", "e1"); !errors.Is(err, ErrEmptySessionID) {
		t.Errorf("DiffChange empty sessionID error = %v, want %v", err, ErrEmptySessionID)
	}
	if err := withCWD.Revert(ctx, "", api.RevertRequest{}); !errors.Is(err, ErrEmptySessionID) {
		t.Errorf("Revert empty sessionID error = %v, want %v", err, ErrEmptySessionID)
	}
}

func TestClientAllMethodsOverUnixSocket(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "tsc-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(dir)
	})
	sockPath := filepath.Join(dir, "test.sock")

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("net.Listen unix: %v", err)
	}
	t.Cleanup(func() {
		_ = ln.Close()
	})

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(api.HealthResponse{PID: 42, Workspaces: []string{"/ws"}})
	})
	mux.HandleFunc("POST /v1/shutdown", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("GET /v1/workspace", func(w http.ResponseWriter, r *http.Request) {
		cwd := r.Header.Get(api.HeaderCWD)
		if cwd != "/ws" {
			http.Error(w, "missing or bad HeaderCWD", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(api.Workspace{
			Name:         "test-ws",
			RootDir:      "/ws",
			CWD:          cwd,
			DefaultAgent: "developer",
			DefaultModel: "ollama/qwen3.8",
		})
	})
	mux.HandleFunc("GET /v1/agents", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(api.HeaderCWD) != "/ws" {
			http.Error(w, "bad header", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode([]api.Agent{{Name: "developer", Effort: "high"}})
	})
	mux.HandleFunc("GET /v1/agents/{name}", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(api.HeaderCWD) != "/ws" {
			http.Error(w, "bad header", http.StatusBadRequest)
			return
		}
		if r.PathValue("name") == "missing" {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(api.ErrorResponse{Code: "not_found", Message: "agent not found"})
			return
		}
		_ = json.NewEncoder(w).Encode(api.Agent{Name: r.PathValue("name"), Effort: "high"})
	})
	mux.HandleFunc("GET /v1/models", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(api.HeaderCWD) != "/ws" {
			http.Error(w, "bad header", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode([]api.Model{{ID: "ollama/qwen3.8", Name: "qwen3.8"}})
	})
	mux.HandleFunc("GET /v1/models/{id...}", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(api.HeaderCWD) != "/ws" {
			http.Error(w, "bad header", http.StatusBadRequest)
			return
		}
		id := r.PathValue("id")
		if id == "missing" {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(api.ErrorResponse{Code: "not_found", Message: "model not found"})
			return
		}
		_ = json.NewEncoder(w).Encode(api.Model{ID: id, Name: id, Label: "Test Model"})
	})

	mux.HandleFunc("POST /v1/sessions", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(api.HeaderCWD) != "/ws" {
			http.Error(w, "bad header", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(api.Session{ID: "s1", Title: "Test", Status: api.StatusIdle})
	})
	mux.HandleFunc("GET /v1/sessions", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(api.HeaderCWD) != "/ws" {
			http.Error(w, "bad header", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode([]api.Session{{ID: "s1", Title: "Test", Status: api.StatusIdle}})
	})
	mux.HandleFunc("GET /v1/sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(api.HeaderCWD) != "/ws" {
			http.Error(w, "bad header", http.StatusBadRequest)
			return
		}
		if r.PathValue("id") == "missing" {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(api.ErrorResponse{Code: "not_found", Message: "missing session"})
			return
		}
		if r.PathValue("id") == "plain-err" {
			http.Error(w, "raw error text", http.StatusBadGateway)
			return
		}
		_ = json.NewEncoder(w).Encode(api.Session{ID: r.PathValue("id"), Status: api.StatusIdle})
	})
	mux.HandleFunc("PATCH /v1/sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(api.HeaderCWD) != "/ws" {
			http.Error(w, "bad header", http.StatusBadRequest)
			return
		}
		var req api.UpdateSessionRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		_ = json.NewEncoder(w).Encode(api.Session{
			ID:     r.PathValue("id"),
			Title:  req.Title,
			Agent:  req.Agent,
			Model:  req.Model,
			Status: api.StatusIdle,
		})
	})
	mux.HandleFunc("DELETE /v1/sessions/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /v1/sessions/{id}/messages", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]api.Message{{ID: "m1", Role: api.RoleUser}})
	})
	mux.HandleFunc("POST /v1/sessions/{id}/messages", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})
	mux.HandleFunc("POST /v1/sessions/{id}/cancel", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /v1/sessions/{id}/permissions", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]api.PermissionRequest{{ID: "p1", ToolName: "write"}})
	})
	mux.HandleFunc("POST /v1/sessions/{id}/permissions", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /v1/sessions/{id}/mode", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(api.ModeResponse{Mode: api.ModeAcceptEdits})
	})
	mux.HandleFunc("PUT /v1/sessions/{id}/mode", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(api.ModeResponse{Mode: api.ModePlan})
	})
	mux.HandleFunc("GET /v1/sessions/{id}/changes", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]api.FileChange{{ID: "c1", Path: "a.go"}})
	})
	mux.HandleFunc("GET /v1/sessions/{id}/diff", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(api.DiffResponse{Path: "a.go", EntryID: "c1", Diff: "--- a/a.go\n+++ b/a.go\n"})
	})
	mux.HandleFunc("POST /v1/sessions/{id}/revert", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /v1/events", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(api.HeaderCWD) == "bad" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(api.ErrorResponse{Code: "bad_request", Message: "bad workspace"})
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, ": connected\n\nevent: completed\ndata: {\"session_id\":\"s1\"}\n\n")
	})

	srv := &http.Server{Handler: mux}
	go func() {
		_ = srv.Serve(ln)
	}()
	t.Cleanup(func() {
		_ = srv.Close()
	})

	c, err := New(sockPath, WithCWD("/ws"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()

	if h, err := c.Health(ctx); err != nil || h.PID != 42 {
		t.Errorf("Health = (%+v, %v)", h, err)
	}
	if err := c.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown: %v", err)
	}

	ws, err := c.GetWorkspace(ctx)
	if err != nil || ws.Name != "test-ws" || ws.CWD != "/ws" {
		t.Errorf("GetWorkspace = (%+v, %v)", ws, err)
	}

	agents, err := c.ListAgents(ctx)
	if err != nil || len(agents) != 1 || agents[0].Name != "developer" {
		t.Errorf("ListAgents = (%+v, %v)", agents, err)
	}

	ag, err := c.GetAgent(ctx, "developer")
	if err != nil || ag.Name != "developer" {
		t.Errorf("GetAgent = (%+v, %v)", ag, err)
	}

	models, err := c.ListModels(ctx)
	if err != nil || len(models) != 1 || models[0].ID != "ollama/qwen3.8" {
		t.Errorf("ListModels = (%+v, %v)", models, err)
	}

	// Test GetModel with slash ID
	mdl, err := c.GetModel(ctx, "ollama/qwen3.8")
	if err != nil || mdl.ID != "ollama/qwen3.8" {
		t.Errorf("GetModel qualified = (%+v, %v)", mdl, err)
	}

	// Test GetModel with bare ID
	mdlBare, err := c.GetModel(ctx, "qwen3.8")
	if err != nil || mdlBare.ID != "qwen3.8" {
		t.Errorf("GetModel bare = (%+v, %v)", mdlBare, err)
	}

	if s, err := c.CreateSession(ctx, api.CreateSessionRequest{Title: "Test"}); err != nil || s.ID != "s1" {
		t.Errorf("CreateSession = (%+v, %v)", s, err)
	}
	if list, err := c.ListSessions(ctx); err != nil || len(list) != 1 {
		t.Errorf("ListSessions = (%+v, %v)", list, err)
	}
	if s, err := c.GetSession(ctx, "s1"); err != nil || s.ID != "s1" {
		t.Errorf("GetSession = (%+v, %v)", s, err)
	}

	if up, err := c.UpdateSession(ctx, "s1", api.UpdateSessionRequest{Title: "Updated", Model: "gpt-5"}); err != nil || up.Title != "Updated" || up.Model != "gpt-5" {
		t.Errorf("UpdateSession = (%+v, %v)", up, err)
	}

	var apiErr *api.APIError
	if _, err := c.GetSession(ctx, "missing"); !errors.As(err, &apiErr) || apiErr.StatusCode != 404 || apiErr.Code != "not_found" {
		t.Errorf("GetSession missing error = %v", err)
	}
	if _, err := c.GetSession(ctx, "plain-err"); !errors.As(err, &apiErr) || apiErr.StatusCode != 502 || apiErr.Message != "raw error text" {
		t.Errorf("GetSession plain-err error = %v", err)
	}

	if err := c.DeleteSession(ctx, "s1"); err != nil {
		t.Errorf("DeleteSession: %v", err)
	}
	if msgs, err := c.History(ctx, "s1"); err != nil || len(msgs) != 1 {
		t.Errorf("History = (%+v, %v)", msgs, err)
	}
	if err := c.Send(ctx, "s1", api.SendRequest{Content: "hi"}); err != nil {
		t.Errorf("Send: %v", err)
	}
	if err := c.Cancel(ctx, "s1"); err != nil {
		t.Errorf("Cancel: %v", err)
	}
	if perms, err := c.PendingPermissions(ctx, "s1"); err != nil || len(perms) != 1 {
		t.Errorf("PendingPermissions = (%+v, %v)", perms, err)
	}
	if err := c.RespondPermission(ctx, "s1", api.PermissionDecision{RequestID: "p1", Kind: api.DecisionAllowOnce}); err != nil {
		t.Errorf("RespondPermission: %v", err)
	}
	if mode, err := c.PermissionMode(ctx, "s1"); err != nil || mode != api.ModeAcceptEdits {
		t.Errorf("PermissionMode = (%q, %v)", mode, err)
	}
	if err := c.SetPermissionMode(ctx, "s1", api.ModePlan); err != nil {
		t.Errorf("SetPermissionMode: %v", err)
	}
	if ch, err := c.Changes(ctx, "s1"); err != nil || len(ch) != 1 {
		t.Errorf("Changes = (%+v, %v)", ch, err)
	}
	if diff, err := c.DiffChange(ctx, "s1", "a.go", "c1"); err != nil || !strings.Contains(diff, "+++ b/a.go") {
		t.Errorf("DiffChange = (%q, %v)", diff, err)
	}
	if err := c.Revert(ctx, "s1", api.RevertRequest{Path: "a.go"}); err != nil {
		t.Errorf("Revert: %v", err)
	}

	var count int
	for ev, err := range c.Subscribe(ctx, "s1") {
		if err != nil {
			t.Fatalf("Subscribe error: %v", err)
		}
		if ev.Type == api.EventCompleted {
			count++
		}
	}
	if count != 1 {
		t.Errorf("Subscribe received %d events, want 1", count)
	}

	readyCalls := 0
	for _, err := range c.SubscribeWithReady(ctx, "s1", func() { readyCalls++ }) {
		if err != nil || readyCalls != 1 {
			t.Fatalf("SubscribeWithReady: err=%v, ready calls=%d; want nil, 1", err, readyCalls)
		}
	}
	if readyCalls != 1 {
		t.Errorf("ready calls = %d, want 1", readyCalls)
	}

	badC := c.WithCWD("bad")
	var subErr error
	for _, err := range badC.SubscribeWithReady(ctx, "s1", func() { t.Error("ready called for unsuccessful subscription") }) {
		subErr = err
	}
	if !errors.As(subErr, &apiErr) || apiErr.StatusCode != 400 {
		t.Errorf("Subscribe bad workspace error = %v, want 400 APIError", subErr)
	}
}

func TestStreamSSEParsing(t *testing.T) {
	sseInput := strings.Join([]string{
		": connected",
		"",
		": ping",
		"event: chunk",
		`data: {"workspace":"/tmp/ws","session_id":"s1","chunk":{"id":"c1","content":[{"type":"text","text":"hi"}]}}`,
		"",
		"event: completed",
		`data: {"type":"completed","workspace":"/tmp/ws","session_id":"s1"}`,
		"",
		"",
	}, "\n")

	var events []api.Event
	streamSSE(context.Background(), strings.NewReader(sseInput), func(ev api.Event, err error) bool {
		if err != nil {
			t.Fatalf("unexpected streamSSE error: %v", err)
		}
		events = append(events, ev)
		return true
	})

	if len(events) != 2 {
		t.Fatalf("expected 2 SSE events, got %d: %+v", len(events), events)
	}
	if events[0].Type != api.EventChunk || events[0].Chunk == nil || len(events[0].Chunk.Content) != 1 || events[0].Chunk.Content[0].Text != "hi" {
		t.Errorf("unexpected events[0]: %+v", events[0])
	}
	if events[1].Type != api.EventCompleted || events[1].SessionID != "s1" {
		t.Errorf("unexpected events[1]: %+v", events[1])
	}
}
