package repl

import (
	"bytes"
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

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
	"github.com/masterkeysrd/tasksmith/internal/repl/command"
	"github.com/masterkeysrd/tasksmith/internal/repl/ui"
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
		out := make([]api.Session, 0, len(st.sessions))
		for _, s := range st.sessions {
			out = append(out, s)
		}
		st.mu.Unlock()
		_ = json.NewEncoder(w).Encode(out)
	})

	mux.HandleFunc("GET /v1/sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		st.mu.Lock()
		sess, ok := st.sessions[id]
		st.mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(api.ErrorResponse{Code: "not_found", Message: "session not found"})
			return
		}
		_ = json.NewEncoder(w).Encode(sess)
	})

	mux.HandleFunc("PATCH /v1/sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		var req api.UpdateSessionRequest
		_ = json.NewDecoder(r.Body).Decode(&req)

		st.mu.Lock()
		sess, ok := st.sessions[id]
		if !ok {
			st.mu.Unlock()
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(api.ErrorResponse{Code: "not_found", Message: "session not found"})
			return
		}
		if req.Title != "" {
			sess.Title = req.Title
		}
		if req.Agent != "" {
			sess.Agent = req.Agent
		}
		if req.Model != "" {
			sess.Model = req.Model
		}
		sess.UpdatedAt = time.Now().UTC()
		st.sessions[id] = sess
		st.mu.Unlock()

		_ = json.NewEncoder(w).Encode(sess)
	})

	mux.HandleFunc("DELETE /v1/sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		st.mu.Lock()
		delete(st.sessions, id)
		delete(st.modes, id)
		delete(st.history, id)
		st.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("GET /v1/sessions/{id}/messages", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		st.mu.Lock()
		msgs := st.history[id]
		st.mu.Unlock()
		_ = json.NewEncoder(w).Encode(msgs)
	})

	mux.HandleFunc("POST /v1/sessions/{id}/messages", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		var req api.SendRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Content == "fail-send" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(api.ErrorResponse{Code: "bad_request", Message: "simulated send error"})
			return
		}

		st.mu.Lock()
		st.sent = append(st.sent, req)
		st.mu.Unlock()

		select {
		case st.sseCh <- api.Event{
			Type:      api.EventChunk,
			SessionID: id,
			Chunk: &api.Chunk{
				ID:      "c1",
				Content: []api.Block{{Type: api.BlockText, Text: "Echo: " + req.Content}},
			},
		}:
		default:
		}
		select {
		case st.sseCh <- api.Event{
			Type:      api.EventCompleted,
			SessionID: id,
		}:
		default:
		}

		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("POST /v1/sessions/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		st.mu.Lock()
		st.cancels = append(st.cancels, id)
		st.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("GET /v1/sessions/{id}/permissions", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		st.mu.Lock()
		perms := st.permissions[id]
		st.mu.Unlock()
		_ = json.NewEncoder(w).Encode(perms)
	})

	mux.HandleFunc("POST /v1/sessions/{id}/permissions", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		var dec api.PermissionDecision
		_ = json.NewDecoder(r.Body).Decode(&dec)
		st.mu.Lock()
		st.decisions = append(st.decisions, dec)
		if len(st.permissions[id]) > 0 {
			st.permissions[id] = st.permissions[id][1:]
		}
		st.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("GET /v1/sessions/{id}/mode", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		st.mu.Lock()
		mode := st.modes[id]
		st.mu.Unlock()
		if mode == "" {
			mode = api.ModeDefault
		}
		_ = json.NewEncoder(w).Encode(api.ModeResponse{Mode: mode})
	})

	mux.HandleFunc("PUT /v1/sessions/{id}/mode", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		var req api.SetModeRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		switch req.Mode {
		case api.ModeDefault, api.ModeAcceptEdits, api.ModePlan, api.ModeAsk, api.ModeAuto:
			st.mu.Lock()
			st.modes[id] = req.Mode
			st.mu.Unlock()
			_ = json.NewEncoder(w).Encode(api.ModeResponse{Mode: req.Mode})
		default:
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(api.ErrorResponse{Code: "invalid_mode", Message: "invalid permission mode"})
		}
	})

	mux.HandleFunc("GET /v1/sessions/{id}/changes", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		st.mu.Lock()
		ch := st.changes[id]
		st.mu.Unlock()
		_ = json.NewEncoder(w).Encode(ch)
	})

	mux.HandleFunc("GET /v1/sessions/{id}/diff", func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Query().Get("path")
		e := r.URL.Query().Get("entry_id")
		st.mu.Lock()
		d, ok := st.diffs[p+":"+e]
		st.mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(api.ErrorResponse{Code: "not_found", Message: "diff entry not found"})
			return
		}
		_ = json.NewEncoder(w).Encode(api.DiffResponse{Path: p, EntryID: e, Diff: d})
	})

	mux.HandleFunc("POST /v1/sessions/{id}/revert", func(w http.ResponseWriter, r *http.Request) {
		var req api.RevertRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Path == "fail.go" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(api.ErrorResponse{Code: "revert_failed", Message: "dirty working tree"})
			return
		}
		st.mu.Lock()
		st.reverts = append(st.reverts, req)
		st.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("GET /v1/events", func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, ": connected\n\n")
		flusher.Flush()

		for {
			select {
			case <-r.Context().Done():
				return
			case ev, open := <-st.sseCh:
				if !open {
					return
				}
				raw, _ := json.Marshal(ev)
				_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, raw)
				flusher.Flush()
			}
		}
	})
}

func TestConfigValidate(t *testing.T) {
	c, _ := client.New("/tmp/test.sock")
	in := strings.NewReader("")
	var out, errOut bytes.Buffer

	valid := Config{
		Client:    c,
		Workspace: "/tmp/ws",
		In:        in,
		Out:       &out,
		ErrOut:    &errOut,
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
		{"nil In", func(cfg *Config) { cfg.In = nil }},
		{"nil Out", func(cfg *Config) { cfg.Out = nil }},
		{"nil ErrOut", func(cfg *Config) { cfg.ErrOut = nil }},
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

func TestRegistryLookupSuggestListAndCommands(t *testing.T) {
	c, st := startTestUnixDaemon(t)
	ctx := context.Background()

	reg := NewRegistry()
	list := reg.List()
	if len(list) < 9 {
		t.Fatalf("expected at least 9 built-in commands, got %d", len(list))
	}

	// Lookup by primary name and aliases (with and without leading '/')
	for _, query := range []string{"help", "/help", "h", "/?", "sessions", "/ls", "exit", "/quit", "q"} {
		if _, ok := reg.Lookup(query); !ok {
			t.Errorf("Lookup(%q) = false, want true", query)
		}
	}
	if _, ok := reg.Lookup("switch"); ok {
		t.Errorf("Lookup(\"switch\") = true, want false (/switch was deleted)")
	}
	if _, ok := reg.Lookup("history"); ok {
		t.Errorf("Lookup(\"history\") = true, want false (/history was deleted)")
	}
	if _, ok := reg.Lookup(""); ok {
		t.Errorf("Lookup(\"\") = true, want false")
	}
	if _, ok := reg.Lookup("/nonexistent"); ok {
		t.Errorf("Lookup(\"/nonexistent\") = true, want false")
	}

	// Suggest
	suggestions := reg.Suggest("/s")
	if len(suggestions) != 1 || suggestions[0].Name != "sessions" {
		t.Errorf("Suggest(\"/s\") = %+v, want [sessions]", suggestions)
	}
	aliasSuggestions := reg.Suggest("/ls")
	if len(aliasSuggestions) != 1 || aliasSuggestions[0].Name != "sessions" {
		t.Errorf("Suggest(\"/ls\") = %+v, want [sessions]", aliasSuggestions)
	}

	// Custom Register replacement
	reg.Register(Command{Name: ""})
	reg.Register(Command{
		Name:        "custom",
		Aliases:     []string{"c"},
		Description: "Custom test command",
		Handler: func(_ CommandEnv, _ []string) (tea.Msg, error) {
			return command.OutputMsg{Text: "custom-ok"}, nil
		},
	})
	if res, err := reg.Execute(CommandEnv{}, "/c"); err != nil || res != (command.OutputMsg{Text: "custom-ok"}) {
		t.Errorf("Execute(\"/c\") = (%+v, %v), want custom-ok", res, err)
	}

	env := CommandEnv{
		Session:      &api.Session{ID: "s1", Agent: "build", Model: "claude-sonnet-4-6"},
		Mode:         api.ModeDefault,
		DefaultAgent: "build",
		DefaultModel: "claude-sonnet-4-6",
		ListSessions: func() ([]api.Session, error) {
			return c.ListSessions(ctx)
		},
	}

	// 1. /help
	helpRes, err := reg.Execute(env, "/help")
	if err != nil {
		t.Fatalf("/help = %v", err)
	}
	outHelp, ok := helpRes.(command.OutputMsg)
	if !ok || !strings.Contains(outHelp.Text, "/new [model]") || !strings.Contains(outHelp.Text, "/diff <path> <entry_id>") {
		t.Errorf("/help = (%+v, %v)", helpRes, err)
	}

	// 2. /sessions
	sessRes, err := reg.Execute(env, "/ls")
	if err != nil {
		t.Fatalf("/ls = %v", err)
	}
	if _, ok := sessRes.(command.OpenPickerMsg); !ok {
		t.Errorf("/ls = (%+v, %v), want OpenPickerMsg", sessRes, err)
	}

	// 4. /mode get & set & invalid
	modeGetRes, err := reg.Execute(env, "/mode")
	if err != nil {
		t.Fatalf("/mode = %v", err)
	}
	openPicker, ok := modeGetRes.(command.OpenPickerMsg)
	if !ok || len(openPicker.Picker.Options) != 5 {
		t.Errorf("/mode = %+v", modeGetRes)
	}
	modeSetRes, err := reg.Execute(env, "/mode plan")
	if err != nil || modeSetRes != (command.ChangeModeMsg{Mode: api.ModePlan}) {
		t.Errorf("/mode plan = (%+v, %v)", modeSetRes, err)
	}
	if _, err := reg.Execute(env, "/mode invalid"); err == nil {
		t.Errorf("/mode invalid expected error, got nil")
	}

	// 5. /changes & /diff & /revert
	chRes, err := reg.Execute(env, "/changes")
	if err != nil || chRes != (command.ListChangesMsg{}) {
		t.Errorf("/changes = (%+v, %v)", chRes, err)
	}

	diffRes, err := reg.Execute(env, "/diff main.go e1")
	if err != nil || diffRes != (command.ShowDiffMsg{Path: "main.go", EntryID: "e1"}) {
		t.Errorf("/diff main.go e1 = (%+v, %v)", diffRes, err)
	}
	if _, err := reg.Execute(env, "/diff main.go"); err == nil {
		t.Errorf("/diff missing entry_id expected error")
	}
	if _, err := reg.Execute(env, "/diff"); err == nil {
		t.Errorf("/diff missing args expected error")
	}

	revFileRes, err := reg.Execute(env, "/revert main.go")
	if err != nil || revFileRes != (command.RevertMsg{Path: "main.go"}) {
		t.Errorf("/revert main.go = (%+v, %v)", revFileRes, err)
	}
	revAllRes, err := reg.Execute(env, "/revert")
	if err != nil || revAllRes != (command.RevertMsg{}) {
		t.Errorf("/revert = (%+v, %v)", revAllRes, err)
	}

	// 6. /new & /sessions
	newRes, err := reg.Execute(env, "/new claude-opus-4-6")
	if err != nil || newRes != (command.NewSessionMsg{Model: "claude-opus-4-6"}) {
		t.Errorf("/new = (%+v, %v)", newRes, err)
	}
	swRes, err := reg.Execute(env, "/sessions s1")
	if err != nil || swRes != (command.SwitchSessionMsg{SessionID: "s1"}) {
		t.Errorf("/sessions s1 = (%+v, %v)", swRes, err)
	}
	lsRes, err := reg.Execute(env, "/sessions")
	if err != nil {
		t.Fatalf("/sessions = %v", err)
	}
	if _, ok := lsRes.(command.OpenPickerMsg); !ok {
		t.Errorf("/sessions = (%+v, %v), want OpenPickerMsg", lsRes, err)
	}

	// 7. /clear & /exit
	clearRes, err := reg.Execute(env, "/clear")
	if err != nil || clearRes != (command.ClearScreenMsg{}) {
		t.Errorf("/clear = (%+v, %v)", clearRes, err)
	}
	exitRes, err := reg.Execute(env, "/exit")
	if err != nil || exitRes != (command.QuitMsg{}) {
		t.Errorf("/exit = (%+v, %v)", exitRes, err)
	}

	// Test Model execution of command events with daemon client
	cfg := Config{
		Client:    c,
		Workspace: "/tmp/ws",
		SessionID: "s1",
		Agent:     "build",
		Model:     "claude-sonnet-4-6",
		Title:     "REPL Test",
		In:        strings.NewReader(""),
		Out:       &bytes.Buffer{},
		ErrOut:    &bytes.Buffer{},
	}
	m := NewModel(ctx, cfg, reg, &api.Session{ID: "s1", Agent: "build", Model: "claude-sonnet-4-6"}, api.ModeDefault)

	// Model handling of commands
	updatedM, cmdMode := m.Update(command.ChangeModeMsg{Mode: api.ModePlan})
	m = updatedM.(Model)
	if m.Mode() != api.ModePlan {
		t.Errorf("Model mode = %v, want ModePlan", m.Mode())
	}
	_ = cmdMode

	updatedM, _ = m.executeSlashCommand("/sessions")
	m = updatedM.(Model)
	if m.state != ui.StatePicker {
		t.Fatalf("expected state ui.StatePicker after /sessions, got %v", m.state)
	}
	if len(m.picker.Options) != 1 {
		t.Fatalf("expected 1 session option in picker, got %d", len(m.picker.Options))
	}
	if len(m.picker.Options[0].Columns) != 4 {
		t.Fatalf("expected 4 columns in session option, got %d", len(m.picker.Options[0].Columns))
	}
	if m.picker.Options[0].Columns[0] != "Initial" || m.picker.Options[0].Columns[1] != "s1" {
		t.Fatalf("unexpected columns in session option: %+v", m.picker.Options[0].Columns)
	}

	// Switch session via picker selection
	swMsg := m.picker.Select()
	if swMsg != (command.SwitchSessionMsg{SessionID: "s1"}) {
		t.Fatalf("picker.Select() = %+v, want SwitchSessionMsg s1", swMsg)
	}
	updatedM, _ = m.Update(swMsg)
	m = updatedM.(Model)
	if m.state != ui.StateIdle {
		t.Fatalf("expected state ui.StateIdle after SwitchSessionMsg, got %v", m.state)
	}

	// Test deleting session via 'd' key in picker
	now := time.Now().UTC()
	st.mu.Lock()
	st.sessions["s2"] = api.Session{
		ID:        "s2",
		Title:     "Session To Delete",
		Agent:     "build",
		Model:     "claude-sonnet-4-6",
		Status:    api.StatusIdle,
		UpdatedAt: now,
	}
	st.mu.Unlock()

	updatedM, _ = m.executeSlashCommand("/sessions")
	m = updatedM.(Model)
	if m.state != ui.StatePicker {
		t.Fatalf("expected state ui.StatePicker after /sessions, got %v", m.state)
	}

	var s2Opt ui.Option
	for _, opt := range m.picker.Options {
		if opt.Value == "s2" {
			s2Opt = opt
			break
		}
	}

	// Test renaming session via 'f2' input action
	f2Act, ok := m.picker.InputActions["f2"]
	if !ok {
		t.Fatalf("expected 'f2' input action in session picker")
	}
	renMsg := f2Act.OnSubmit(s2Opt, "Renamed Session Two")
	if renMsg != (command.RenameSessionMsg{SessionID: "s2", Title: "Renamed Session Two"}) {
		t.Fatalf("f2Act = %+v, want RenameSessionMsg s2", renMsg)
	}
	updatedM, _ = m.Update(renMsg)
	m = updatedM.(Model)
	if m.state != ui.StatePicker {
		t.Fatalf("expected state ui.StatePicker to stay open after rename, got %v", m.state)
	}
	st.mu.Lock()
	s2Renamed := st.sessions["s2"]
	st.mu.Unlock()
	if s2Renamed.Title != "Renamed Session Two" {
		t.Errorf("expected daemon session s2 title = 'Renamed Session Two', got %q", s2Renamed.Title)
	}

	// Test deleting session via 'f4' confirm action
	f4Act, ok := m.picker.ConfirmActions["f4"]
	if !ok {
		t.Fatalf("expected 'f4' confirm action in session picker")
	}
	delMsg := f4Act.OnConfirm(s2Opt)
	if delMsg != (command.DeleteSessionMsg{SessionID: "s2"}) {
		t.Fatalf("f4Act = %+v, want DeleteSessionMsg s2", delMsg)
	}
	updatedM, _ = m.Update(delMsg)
	m = updatedM.(Model)
	if m.state != ui.StatePicker {
		t.Fatalf("expected state ui.StatePicker after DeleteSessionMsg with remaining sessions, got %v", m.state)
	}
	if len(m.picker.Options) != 1 || m.picker.Options[0].Value != "s1" {
		t.Fatalf("expected 1 remaining session (s1), got %+v", m.picker.Options)
	}
	st.mu.Lock()
	_, s2Exists := st.sessions["s2"]
	st.mu.Unlock()
	if s2Exists {
		t.Fatalf("expected s2 to be deleted from daemon")
	}

	// Test deleting the active session (s1), which is also the last remaining session.
	// The picker must stay open in ui.StatePicker (until esc).
	updatedM, _ = m.Update(command.DeleteSessionMsg{SessionID: "s1"})
	m = updatedM.(Model)
	if m.session != nil {
		t.Fatalf("expected m.session to be nil after deleting active session, got %+v", m.session)
	}
	if m.state != ui.StatePicker {
		t.Fatalf("expected state ui.StatePicker to remain open even with 0 sessions remaining, got %v", m.state)
	}
	if len(m.picker.Options) != 0 {
		t.Fatalf("expected 0 options remaining in picker, got %d", len(m.picker.Options))
	}

	// Dismiss empty picker with Esc
	updatedM, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = updatedM.(Model)
	if m.state != ui.StateIdle {
		t.Fatalf("expected state ui.StateIdle after Esc on picker, got %v", m.state)
	}

	// Restore s1 with ModePlan in daemon to test mode sync and header re-rendering on switch
	st.mu.Lock()
	st.sessions["s1"] = api.Session{
		ID:        "s1",
		Title:     "Initial",
		Agent:     "build",
		Model:     "claude-sonnet-4-6",
		Status:    api.StatusIdle,
		UpdatedAt: now,
	}
	st.modes["s1"] = api.ModePlan
	st.mu.Unlock()

	updatedM, swCmd := m.Update(command.SwitchSessionMsg{SessionID: "s1"})
	m = updatedM.(Model)
	if m.Mode() != api.ModePlan {
		t.Errorf("Model mode after switch = %v, want ModePlan", m.Mode())
	}
	if swCmd == nil {
		t.Fatalf("expected tea.Cmd sequence from switch session")
	}

	// Test /new clearing session, resetting mode to Default, and re-rendering header
	updatedM, newCmd := m.Update(command.NewSessionMsg{Model: "claude-haiku-4-5"})
	m = updatedM.(Model)
	if m.session != nil {
		t.Fatalf("expected nil session after /new")
	}
	if m.Mode() != api.ModeDefault {
		t.Fatalf("expected ModeDefault after /new, got %v", m.Mode())
	}
	if newCmd == nil {
		t.Fatalf("expected tea.Cmd sequence from /new")
	}

	// Restore session for remaining tests
	m.session = &api.Session{ID: "s1", Agent: "build", Model: "claude-sonnet-4-6"}

	updatedM, _ = m.Update(command.ListChangesMsg{})
	m = updatedM.(Model)

	updatedM, _ = m.Update(command.ShowDiffMsg{Path: "main.go", EntryID: "e1"})
	m = updatedM.(Model)

	updatedM, _ = m.Update(command.RevertMsg{Path: "main.go"})
	m = updatedM.(Model)

	updatedM, _ = m.Update(command.RevertMsg{})
	m = updatedM.(Model)

	// Empty session branches for changes
	m.session = nil
	updatedM, _ = m.Update(command.ListChangesMsg{})
	m = updatedM.(Model)

	// Empty changes in daemon session
	st.mu.Lock()
	st.changes["s2"] = nil
	st.history["s2"] = nil
	st.mu.Unlock()
	m.session = &api.Session{ID: "s2"}
	updatedM, _ = m.Update(command.ListChangesMsg{})
	m = updatedM.(Model)
}

func TestStylesAndFormatters(t *testing.T) {
	for _, mode := range []api.PermissionMode{
		api.ModeDefault,
		api.ModeAcceptEdits,
		api.ModePlan,
		api.ModeAsk,
		api.ModeAuto,
	} {
		banner := FormatHeader(Header{
			Version:  "v0.1.0",
			Model:    "Claude 3.7 Sonnet",
			Thinking: "high",
			Path:     "/workspace/project",
		})
		lines := strings.Split(banner, "\n")
		if len(lines) != 3 {
			t.Fatalf("FormatBanner expected 3 lines, got %d: %q", len(lines), banner)
		}
		if !strings.Contains(lines[0], "TaskSmith") || !strings.Contains(lines[0], "v0.1.0") {
			t.Errorf("FormatBanner line 1 = %q, want TaskSmith and v0.1.0", lines[0])
		}
		if !strings.Contains(lines[1], "Claude 3.7 Sonnet with high effort") {
			t.Errorf("FormatBanner line 2 = %q, want 'Claude 3.7 Sonnet with high effort'", lines[1])
		}
		if !strings.Contains(lines[2], "/workspace/project") {
			t.Errorf("FormatBanner line 3 = %q, want '/workspace/project'", lines[2])
		}

		for _, state := range []State{StateIdle, StateStreaming, StateApproval} {
			bar := FormatStatusBar(StatusBar{
				State:    state,
				Mode:     mode,
				Model:    "claude-sonnet-4-6",
				Thinking: "auto",
				Width:    80,
			})
			if mode != api.ModeDefault {
				if !strings.Contains(bar, string(mode)) {
					t.Errorf("FormatStatusBar(%s, %s) missing mode: %q", state, mode, bar)
				}
			} else {
				if strings.Contains(bar, "default · ") {
					t.Errorf("default mode should not prefix with 'default · ': %s", bar)
				}
			}
			if !strings.Contains(bar, "claude-sonnet-4-6 · auto") {
				t.Errorf("missing model · thinking in status bar: %s", bar)
			}
		}
	}

	if StateIdle.String() != "idle" || StateStreaming.String() != "streaming" || StateApproval.String() != "approval" || State(99).String() != "unknown" {
		t.Errorf("unexpected State.String() values")
	}

	prompt := FormatUserPrompt("build a feature")
	if !strings.Contains(prompt, "build a feature") {
		t.Errorf("FormatUserPrompt = %q", prompt)
	}

	// FormatMessage across user, assistant (with tool call), tool (normal + error), system
	msgs := []api.Message{
		{
			Role:    api.RoleUser,
			Content: []api.Block{{Type: api.BlockText, Text: "user text"}},
		},
		{
			Role: api.RoleAssistant,
			Content: []api.Block{
				{Type: api.BlockReasoning, Text: "step 1"},
				{Type: api.BlockText, Text: "assistant text"},
				{Type: api.BlockToolCall, ToolCall: &api.ToolCall{ID: "tc1", Name: "write", Args: map[string]any{"path": "a.go"}}},
			},
		},
		{
			Role:    api.RoleTool,
			Name:    "write",
			Content: []api.Block{{Type: api.BlockText, Text: "wrote 10 bytes"}},
		},
		{
			Role:       api.RoleTool,
			ToolCallID: "tc1",
			IsError:    true,
			Content:    []api.Block{{Type: api.BlockText, Text: "permission denied"}},
		},
		{
			Role:    api.RoleSystem,
			Content: []api.Block{{Type: api.BlockText, Text: "system notice"}},
		},
	}
	for _, m := range msgs {
		formatted := FormatMessage(m)
		if formatted == "" {
			t.Errorf("FormatMessage(%+v) returned empty string", m)
		}
	}

	// FormatDiff
	diff := FormatDiff("--- a/file.go\n+++ b/file.go\n@@ -1,2 +1,2 @@\n-old line\n+new line\n context")
	for _, want := range []string{"--- a/file.go", "+++ b/file.go", "@@ -1,2 +1,2 @@", "-old line", "+new line", "context"} {
		if !strings.Contains(diff, want) {
			t.Errorf("FormatDiff missing %q in:\n%s", want, diff)
		}
	}

	// FormatPermissionCard across safety levels and feedback mode
	for _, safety := range []api.SafetyLevel{api.SafetySafe, api.SafetyOutside, api.SafetyUnsafe} {
		perm := api.PermissionRequest{
			ID:       "p1",
			ToolName: "bash",
			Action:   api.ActionExec,
			Safety:   safety,
			Target:   "rm -rf build/",
			Summary:  "Execute shell command",
			Diff:     "-a\n+b",
		}
		card := FormatPermissionCard(perm, 1, false, "")
		for _, want := range []string{"[Permission Required]", "Execute shell command", "rm -rf build/", "[y] Allow once", "[a] Allow for session", "[n] Reject"} {
			if !strings.Contains(card, want) {
				t.Errorf("FormatPermissionCard missing %q:\n%s", want, card)
			}
		}
		fbCard := FormatPermissionCard(perm, 2, true, "too risky")
		if !strings.Contains(fbCard, "too risky") {
			t.Errorf("FormatPermissionCard feedback missing 'too risky':\n%s", fbCard)
		}
	}
}

func TestBubbleTeaModelLifecycle(t *testing.T) {
	c, st := startTestUnixDaemon(t)
	ctx := context.Background()

	cfg := Config{
		Client:    c,
		Workspace: "/tmp/ws",
		SessionID: "s1",
		Agent:     "build",
		Model:     "claude-sonnet-4-6",
		Title:     "TUI Test",
		In:        strings.NewReader(""),
		Out:       &bytes.Buffer{},
		ErrOut:    &bytes.Buffer{},
	}

	sess := &api.Session{ID: "s1", Agent: "build", Model: "claude-sonnet-4-6"}
	m := NewModel(ctx, cfg, NewRegistry(), sess, api.ModeDefault)
	_ = m.Init()

	if m.State() != StateIdle {
		t.Fatalf("initial state = %v, want StateIdle", m.State())
	}
	idleView := m.View()
	if idleView.AltScreen {
		t.Fatalf("View().AltScreen = true, want false (inline mode required)")
	}

	// 1. Slash command autocomplete hints when input starts with "/"
	m.SetInputValue("/mo")
	if m.InputValue() != "/mo" {
		t.Errorf("InputValue() = %q, want \"/mo\"", m.InputValue())
	}
	hintView := m.View()
	if !strings.Contains(hintView.Content, "/mode") {
		t.Errorf("idle view with '/mo' missing autocomplete hint:\n%s", hintView.Content)
	}

	// 2. Execute "/mode accept_edits" via Enter
	m.SetInputValue("/mode accept_edits")
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)
	if cmd == nil {
		t.Errorf("expected tea.Println command from /mode")
	}
	if m.Mode() != api.ModeAcceptEdits {
		t.Errorf("Mode() after /mode accept_edits = %q, want %q", m.Mode(), api.ModeAcceptEdits)
	}

	// 3. Shift+Tab cycles mode: accept_edits -> plan -> ask -> auto -> default -> accept_edits
	wantModes := []api.PermissionMode{
		api.ModePlan,
		api.ModeAsk,
		api.ModeAuto,
		api.ModeDefault,
		api.ModeAcceptEdits,
	}
	for _, want := range wantModes {
		updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
		m = updated.(Model)
		if m.Mode() != want {
			t.Errorf("Shift+Tab Mode() = %q, want %q", m.Mode(), want)
		}
	}

	// 4. Up / Down prompt history navigation
	m.SetInputValue("draft text")
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	m = updated.(Model)
	if m.InputValue() != "/mode accept_edits" {
		t.Errorf("Up history value = %q, want '/mode accept_edits'", m.InputValue())
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = updated.(Model)
	if m.InputValue() != "draft text" {
		t.Errorf("Down history restored draft = %q, want 'draft text'", m.InputValue())
	}

	// 5. Submit user prompt -> transitions to StateStreaming
	m.SetInputValue("Please edit main.go")
	updated, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)
	if m.State() != StateStreaming {
		t.Fatalf("State after prompt Enter = %v, want StateStreaming", m.State())
	}
	if cmd == nil {
		t.Fatalf("expected batch command after prompt submission")
	}

	// Tick spinner in StateStreaming
	updated, _ = m.Update(spinner.TickMsg{})
	m = updated.(Model)

	// 6. Stream EventChunk (reasoning + text)
	updated, _ = m.Update(api.Event{
		Type:      api.EventChunk,
		SessionID: "s1",
		Chunk: &api.Chunk{
			ID: "c1",
			Content: []api.Block{
				{Type: api.BlockReasoning, Text: "Analyzing main.go..."},
				{Type: api.BlockText, Text: "I will update main.go now."},
			},
		},
	})
	m = updated.(Model)
	if m.StreamText() != "I will update main.go now." || m.StreamReasoning() != "Analyzing main.go..." {
		t.Errorf("unexpected stream buffers: text=%q reasoning=%q", m.StreamText(), m.StreamReasoning())
	}
	streamView := m.View()
	if !strings.Contains(streamView.Content, "I will update main.go now.") || !strings.Contains(streamView.Content, "esc to cancel") {
		t.Errorf("streaming view missing content:\n%s", streamView.Content)
	}
	if !strings.Contains(streamView.Content, "─") || !strings.Contains(streamView.Content, "❯ ") {
		t.Errorf("streaming view missing anchored composer:\n%s", streamView.Content)
	}

	// Typing while streaming updates draft in composer; Enter is ignored
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'e', Text: "e"})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyPressMsg{Code: 't', Text: "t"})
	m = updated.(Model)
	if m.InputValue() != "next" {
		t.Errorf("InputValue while streaming = %q, want 'next'", m.InputValue())
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)
	if m.State() != StateStreaming {
		t.Errorf("Enter while streaming should be ignored, got state = %v", m.State())
	}
	// WindowSizeMsg updates composer width
	updated, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = updated.(Model)
	m.SetInputValue("") // clear draft for subsequent steps

	// Intermediate tool call and tool result via EventMessageAdded
	updated, _ = m.Update(api.Event{
		Type:      api.EventMessageAdded,
		SessionID: "s1",
		Message: &api.Message{
			Role: api.RoleAssistant,
			Content: []api.Block{
				{Type: api.BlockToolCall, ToolCall: &api.ToolCall{ID: "tc1", Name: "write"}},
			},
		},
	})
	m = updated.(Model)

	updated, _ = m.Update(api.Event{
		Type:      api.EventMessageAdded,
		SessionID: "s1",
		Message: &api.Message{
			Role:    api.RoleTool,
			Name:    "write",
			Content: []api.Block{{Type: api.BlockText, Text: "ok"}},
		},
	})
	m = updated.(Model)

	// 7. EventPermissionRequested -> transitions to StateApproval with 3 permissions to test y, a, and r/n
	perms := []api.PermissionRequest{
		{
			ID:            "p1",
			ToolCallID:    "tc1",
			ToolName:      "write",
			Action:        api.ActionWrite,
			Safety:        api.SafetyOutside,
			Target:        "main.go",
			Summary:       "Write main.go",
			Diff:          "-old\n+new",
			GrantPatterns: []string{"write:main.go"},
		},
		{
			ID:            "p2",
			ToolCallID:    "tc2",
			ToolName:      "bash",
			Action:        api.ActionExec,
			Safety:        api.SafetyUnsafe,
			Summary:       "Run go test",
			GrantPatterns: []string{"bash:go test*"},
		},
		{
			ID:         "p3",
			ToolCallID: "tc3",
			ToolName:   "delete",
			Action:     api.ActionDelete,
			Safety:     api.SafetyUnsafe,
			Summary:    "Delete temp file",
		},
	}
	updated, _ = m.Update(api.Event{
		Type:        api.EventPermissionRequested,
		SessionID:   "s1",
		Permissions: perms,
	})
	m = updated.(Model)
	if m.State() != StateApproval || len(m.PendingPermissions()) != 3 {
		t.Fatalf("expected StateApproval with 3 pending permissions, got state=%v count=%d", m.State(), len(m.PendingPermissions()))
	}
	approvalView := m.View()
	if !strings.Contains(approvalView.Content, "Write main.go") {
		t.Errorf("approval view missing permission summary:\n%s", approvalView.Content)
	}

	// Navigate cursor Down/Up in StateApproval, then press 'y' (Allow once) for p1
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = updated.(Model)
	if m.PermCursor() != 1 {
		t.Errorf("PermCursor after Down = %d, want 1", m.PermCursor())
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	m = updated.(Model)
	if m.PermCursor() != 0 {
		t.Errorf("PermCursor after Up = %d, want 0", m.PermCursor())
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	m = updated.(Model)
	if m.State() != StateApproval || len(m.PendingPermissions()) != 2 {
		t.Fatalf("expected StateApproval with 2 remaining permissions, got %v (%d)", m.State(), len(m.PendingPermissions()))
	}

	// Press 'a' (Allow session) for p2
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	m = updated.(Model)
	if m.State() != StateApproval || len(m.PendingPermissions()) != 1 {
		t.Fatalf("expected StateApproval with 1 remaining permission, got %v (%d)", m.State(), len(m.PendingPermissions()))
	}

	// Press 'r' to enter inline feedback mode for p3, type feedback, press Enter
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	m = updated.(Model)
	if !m.FeedbackMode() {
		t.Fatalf("expected FeedbackMode() == true after pressing 'r'")
	}
	for _, ch := range "do not delete" {
		updated, _ = m.Update(tea.KeyPressMsg{Code: ch, Text: string(ch)})
		m = updated.(Model)
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)
	if m.State() != StateStreaming || len(m.PendingPermissions()) != 0 {
		t.Fatalf("expected transition back to StateStreaming after last permission, got %v", m.State())
	}

	st.mu.Lock()
	if len(st.decisions) != 3 {
		t.Fatalf("expected 3 recorded decisions on daemon, got %d", len(st.decisions))
	}
	if st.decisions[0].Kind != api.DecisionAllowOnce {
		t.Errorf("decisions[0] = %+v, want allow_once", st.decisions[0])
	}
	if st.decisions[1].Kind != api.DecisionAllowSession || st.decisions[1].GrantPattern != "bash:go test*" {
		t.Errorf("decisions[1] = %+v, want allow_session with pattern", st.decisions[1])
	}
	if st.decisions[2].Kind != api.DecisionReject || st.decisions[2].Feedback != "do not delete" {
		t.Errorf("decisions[2] = %+v, want reject with feedback 'do not delete'", st.decisions[2])
	}
	st.mu.Unlock()

	// 8. Stream final assistant response and EventCompleted -> StateIdle
	updated, _ = m.Update(api.Event{
		Type:      api.EventChunk,
		SessionID: "s1",
		Chunk: &api.Chunk{
			ID:      "c2",
			Content: []api.Block{{Type: api.BlockText, Text: "All done!"}},
		},
	})
	m = updated.(Model)
	updated, _ = m.Update(api.Event{
		Type:      api.EventCompleted,
		SessionID: "s1",
	})
	m = updated.(Model)
	if m.State() != StateIdle || m.StreamText() != "" {
		t.Fatalf("State after EventCompleted = %v, StreamText = %q", m.State(), m.StreamText())
	}

	// 9. Start another turn and press Esc in StateStreaming -> cancels turn and returns to StateIdle
	m.SetInputValue("Long running turn")
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)
	if m.State() != StateStreaming {
		t.Fatalf("expected StateStreaming, got %v", m.State())
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = updated.(Model)
	if m.State() != StateIdle {
		t.Errorf("State after Esc in StateStreaming = %v, want StateIdle", m.State())
	}
	st.mu.Lock()
	cancelCount := len(st.cancels)
	st.mu.Unlock()
	if cancelCount != 1 {
		t.Errorf("daemon cancel count = %d, want 1", cancelCount)
	}

	// 10. EventError transitions back to StateIdle
	m.SetInputValue("Turn that errors")
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)
	updated, _ = m.Update(api.Event{
		Type:      api.EventError,
		SessionID: "s1",
		Error:     "model overloaded",
	})
	m = updated.(Model)
	if m.State() != StateIdle {
		t.Errorf("State after EventError = %v, want StateIdle", m.State())
	}

	// 11. Test Enter selection in StateApproval, 'n' hotkey, Esc from feedback mode, sendErrorMsg, /new, /clear, /exit
	updated, _ = m.Update(api.Event{
		Type:      api.EventPermissionRequested,
		SessionID: "s1",
		Permissions: []api.PermissionRequest{
			{ID: "p4", ToolCallID: "tc4", ToolName: "write", Action: api.ActionWrite, Safety: api.SafetySafe, Summary: "Write a.txt"},
			{ID: "p5", ToolCallID: "tc5", ToolName: "write", Action: api.ActionWrite, Safety: api.SafetySafe, Summary: "Write b.txt"},
		},
	})
	m = updated.(Model)
	// Enter feedback mode with 'r', then cancel feedback mode with Esc
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = updated.(Model)
	if m.FeedbackMode() {
		t.Errorf("expected FeedbackMode() == false after Esc")
	}
	// Press Enter on cursor 0 (Allow once) for p4
	m.SetPermCursor(0)
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)
	// Press 'n' (Reject) for p5
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	m = updated.(Model)
	if m.State() != StateStreaming {
		t.Errorf("expected StateStreaming after p5, got %v", m.State())
	}

	// Simulate sendErrorMsg
	updated, _ = m.Update(sendErrorMsg{err: errors.New("network failure")})
	m = updated.(Model)
	if m.State() != StateIdle {
		t.Errorf("expected StateIdle after sendErrorMsg, got %v", m.State())
	}

	// Execute /new (resets session to nil), then submit a prompt to lazily create session with model gpt-5.4
	m.SetInputValue("/new gpt-5.4")
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)
	if m.Session() != nil {
		t.Errorf("expected nil session immediately after /new, got %+v", m.Session())
	}
	m.SetInputValue("Prompt in new session")
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)
	if m.Session() == nil || m.Session().Model != "gpt-5.4" {
		t.Errorf("expected lazily created session with model gpt-5.4, got %+v", m.Session())
	}
	updated, _ = m.Update(api.Event{Type: api.EventCompleted, SessionID: m.Session().ID})
	m = updated.(Model)

	m.SetInputValue("/clear")
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)

	// 12. Ctrl+C in StateIdle quits cleanly
	updated, quitCmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	m = updated.(Model)
	if quitCmd == nil || m.View().Content != "" {
		t.Errorf("expected quit command and empty view on Ctrl+C")
	}
}

func TestLazySessionCreationNoPrompts(t *testing.T) {
	c, st := startTestUnixDaemon(t)
	st.mu.Lock()
	clear(st.sessions)
	st.mu.Unlock()

	input := strings.Join([]string{
		"/sessions",
		"/mode",
		"/mode plan",
		"/changes",
		"/new claude-opus-4-6",
		"/exit",
	}, "\n") + "\n"

	var out, errOut bytes.Buffer
	err := Run(context.Background(), Config{
		Client:    c,
		Workspace: "/tmp/ws",
		Agent:     "build",
		Model:     "claude-sonnet-4-6",
		Title:     "Lazy Test",
		In:        strings.NewReader(input),
		Out:       &out,
		ErrOut:    &errOut,
	})
	if err != nil {
		t.Fatalf("Run failed: %v\nerrOut: %s", err, errOut.String())
	}

	st.mu.Lock()
	sessionCount := len(st.sessions)
	st.mu.Unlock()
	if sessionCount != 0 {
		t.Fatalf("expected 0 sessions created when no prompt was sent, got %d", sessionCount)
	}
}

func TestBuildHeaderAndFormatHeader(t *testing.T) {
	c, _ := startTestUnixDaemon(t)
	ctx := context.Background()

	// 1. Without session, empty agent and model -> fallbacks to workspace default
	cfg1 := Config{
		Client:    c,
		Workspace: "/workspace/project",
	}
	h1 := buildHeader(ctx, cfg1, nil)
	if h1.Model != "Claude 3.7 Sonnet" {
		t.Errorf("h1.Model = %q, want 'Claude 3.7 Sonnet'", h1.Model)
	}
	if h1.Thinking != "high" { // ag.Effort = "high" from "build"
		t.Errorf("h1.Thinking = %q, want 'high'", h1.Thinking)
	}

	// 2. Explicit agent and model overrides
	cfg2 := Config{
		Client:    c,
		Workspace: "/workspace/project",
		Agent:     "build",
		Model:     "claude-sonnet-4-6",
	}
	h2 := buildHeader(ctx, cfg2, nil)
	if h2.Model != "Claude 3.7 Sonnet" {
		t.Errorf("h2.Model = %q, want 'Claude 3.7 Sonnet'", h2.Model)
	}
	if h2.Thinking != "high" {
		t.Errorf("h2.Thinking = %q, want 'high'", h2.Thinking)
	}

	// 3. With existing session
	sess := &api.Session{
		ID:    "s1",
		Agent: "build",
		Model: "claude-sonnet-4-6",
	}
	h3 := buildHeader(ctx, cfg1, sess)
	if h3.Model != "Claude 3.7 Sonnet" {
		t.Errorf("h3.Model = %q, want 'Claude 3.7 Sonnet'", h3.Model)
	}
	if h3.Thinking != "high" {
		t.Errorf("h3.Thinking = %q, want 'high'", h3.Thinking)
	}

	// 4. Header format rendering
	b1 := FormatHeader(Header{
		Version:  "0.1.0",
		Model:    "Claude 3.7 Sonnet",
		Thinking: "high",
		Path:     "/workspace/project",
	})
	if !strings.Contains(b1, "TaskSmith") || !strings.Contains(b1, "v0.1.0") {
		t.Errorf("expected leading 'v' to be prepended, got:\n%s", b1)
	}
	if !strings.Contains(b1, "Claude 3.7 Sonnet with high effort") {
		t.Errorf("expected line 2 model and effort, got:\n%s", b1)
	}

	// 5. Default version when empty
	b2 := FormatHeader(Header{Path: "/workspace/project"})
	if !strings.Contains(b2, "TaskSmith") || !strings.Contains(b2, "v0.1.0") {
		t.Errorf("expected default v0.1.0, got:\n%s", b2)
	}
	if !strings.Contains(b2, "default with auto effort") {
		t.Errorf("expected 'default with auto effort', got:\n%s", b2)
	}
}

func TestRunResumeSessionWithMutation(t *testing.T) {
	c, st := startTestUnixDaemon(t)
	ctx := context.Background()

	// Initial session in daemon is s1 (Title: "Initial", Agent: "build", Model: "claude-sonnet-4-6")
	var out, errOut bytes.Buffer
	cfg := Config{
		Client:    c,
		Workspace: "/workspace/project",
		SessionID: "s1",
		Title:     "Updated Resumed Title",
		Model:     "claude-opus-4-6",
		In:        strings.NewReader("/exit\n"),
		Out:       &out,
		ErrOut:    &errOut,
	}

	if err := Run(ctx, cfg); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Verify session in daemon was mutated
	st.mu.Lock()
	s1 := st.sessions["s1"]
	st.mu.Unlock()

	if s1.Title != "Updated Resumed Title" {
		t.Errorf("session Title = %q, want 'Updated Resumed Title'", s1.Title)
	}
	if s1.Model != "claude-opus-4-6" {
		t.Errorf("session Model = %q, want 'claude-opus-4-6'", s1.Model)
	}

	// Verify session history was printed on resume
	if !strings.Contains(out.String(), "Hello REPL") || !strings.Contains(out.String(), "Hi there!") {
		t.Errorf("resumed session stdout expected history messages, got:\n%s", out.String())
	}
}

func TestInteractiveModePickerAndFilterFlows(t *testing.T) {
	c, _ := startTestUnixDaemon(t)
	ctx := context.Background()

	cfg := Config{
		Client:    c,
		Workspace: "/workspace/project",
		SessionID: "s1",
		In:        strings.NewReader(""),
		Out:       &bytes.Buffer{},
		ErrOut:    &bytes.Buffer{},
	}
	sess := &api.Session{ID: "s1", Agent: "build", Model: "claude-sonnet-4-6"}
	m := NewModel(ctx, cfg, NewRegistry(), sess, api.ModeDefault)
	_ = m.Init()

	// --- Flow 1: Typing "/mode" and Enter opens StatePicker ---
	m.SetInputValue("/mode")
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)
	if m.State() != StatePicker {
		t.Fatalf("State after '/mode' Enter = %v, want StatePicker", m.State())
	}
	pickerView := m.View()
	if !strings.Contains(pickerView.Content, "Select permission mode:") ||
		!strings.Contains(pickerView.Content, "default") ||
		!strings.Contains(pickerView.Content, "esc to cancel") {
		t.Fatalf("picker view mismatch:\n%s", pickerView.Content)
	}
	if !strings.Contains(pickerView.Content, "─") {
		t.Errorf("picker view missing top horizontal separator line:\n%s", pickerView.Content)
	}
	pickerLines := strings.Split(strings.TrimRight(pickerView.Content, "\n"), "\n")
	if len(pickerLines) < 3 || strings.TrimSpace(pickerLines[len(pickerLines)-2]) != "" {
		t.Errorf("picker view missing blank line before status bar:\n%s", pickerView.Content)
	}
	if strings.Contains(pickerView.Content, "mode: default │") || strings.Contains(pickerView.Content, "shift+tab cycle mode") {
		t.Errorf("picker view should own its status bar and not render global status bar:\n%s", pickerView.Content)
	}
	// Initial cursor is at 0 (ModeDefault)
	if m.PickerCursor() != 0 {
		t.Errorf("initial picker cursor = %d, want 0", m.PickerCursor())
	}
	// Navigate down to 1 (accept_edits)
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = updated.(Model)
	if m.PickerCursor() != 1 {
		t.Errorf("picker cursor after down = %d, want 1", m.PickerCursor())
	}
	// Cancel picker with Esc -> returns to StateIdle with mode unchanged
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = updated.(Model)
	if m.State() != StateIdle {
		t.Fatalf("State after Esc = %v, want StateIdle", m.State())
	}
	if m.Mode() != api.ModeDefault {
		t.Errorf("Mode after canceled picker = %v, want ModeDefault", m.Mode())
	}

	// Open picker again, navigate down to 2 (plan), confirm with Enter
	m.SetInputValue("/mode")
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown}) // to 1 (accept_edits)
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown}) // to 2 (plan)
	m = updated.(Model)
	if m.PickerCursor() != 2 {
		t.Fatalf("picker cursor = %d, want 2", m.PickerCursor())
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)
	if m.State() != StateIdle {
		t.Fatalf("State after picker Enter = %v, want StateIdle", m.State())
	}
	if m.Mode() != api.ModePlan {
		t.Errorf("Mode after picker select = %v, want ModePlan", m.Mode())
	}

	// --- Flow 2: Typing "/mode " with trailing space (live inline argument filter) ---
	m.SetInputValue("/mode ")
	idleView := m.View()
	// All 5 options should be visible in inline suggestion block
	if !strings.Contains(idleView.Content, "accept_edits") ||
		!strings.Contains(idleView.Content, "plan") ||
		!strings.Contains(idleView.Content, "ask") ||
		!strings.Contains(idleView.Content, "auto") {
		t.Fatalf("idle view with '/mode ' missing options:\n%s", idleView.Content)
	}
	if m.ArgCursor() != 0 {
		t.Errorf("initial argCursor = %d, want 0", m.ArgCursor())
	}

	// Navigate down in suggestions
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = updated.(Model)
	if m.ArgCursor() != 1 {
		t.Errorf("argCursor after Down = %d, want 1", m.ArgCursor())
	}
	// Navigate up
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	m = updated.(Model)
	if m.ArgCursor() != 0 {
		t.Errorf("argCursor after Up = %d, want 0", m.ArgCursor())
	}

	// Filter with query: "/mode as" (matches "ask")
	m.SetInputValue("/mode as")
	// Down and Enter to select "ask"
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)
	if m.Mode() != api.ModeAsk {
		t.Errorf("Mode after filtered select = %v, want ModeAsk", m.Mode())
	}

	// --- Flow 3: Typing "/mode plan" directly and pressing Enter ---
	m.SetInputValue("/mode plan")
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)
	if m.Mode() != api.ModePlan {
		t.Errorf("Mode after direct '/mode plan' = %v, want ModePlan", m.Mode())
	}
}
