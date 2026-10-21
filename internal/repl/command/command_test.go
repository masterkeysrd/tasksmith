package command

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
)

func setupTestDaemon(t *testing.T) (*client.Client, string) {
	t.Helper()
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "d.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	mux := http.NewServeMux()
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

	sessions := map[string]api.Session{
		"s1": {
			ID:        "s1",
			Title:     "Session One",
			Agent:     "build",
			Model:     "gpt-5",
			Status:    api.StatusIdle,
			CreatedAt: now,
			UpdatedAt: now,
		},
	}
	history := map[string][]api.Message{
		"s1": {
			{
				ID:   "m1",
				Role: api.RoleUser,
				Content: []api.Block{
					{Type: api.BlockText, Text: "Hello command test"},
				},
			},
		},
	}
	changes := map[string][]api.FileChange{
		"s1": {
			{
				ID:        "c1",
				Path:      "main.go",
				Kind:      "modify",
				Additions: 5,
				Deletions: 2,
				Timestamp: now,
			},
		},
	}
	diffs := map[string]string{
		"main.go:c1": "--- a/main.go\n+++ b/main.go\n@@ -1,2 +1,5 @@\n+line1\n",
	}
	modes := map[string]api.PermissionMode{
		"s1": api.ModeDefault,
	}

	mux.HandleFunc("GET /v1/sessions", func(w http.ResponseWriter, _ *http.Request) {
		out := make([]api.Session, 0, len(sessions))
		for _, s := range sessions {
			out = append(out, s)
		}
		_ = json.NewEncoder(w).Encode(out)
	})
	mux.HandleFunc("GET /v1/sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		s, ok := sessions[id]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(api.ErrorResponse{Code: "not_found", Message: "session not found"})
			return
		}
		_ = json.NewEncoder(w).Encode(s)
	})
	mux.HandleFunc("GET /v1/sessions/{id}/messages", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(history[r.PathValue("id")])
	})
	mux.HandleFunc("GET /v1/sessions/{id}/changes", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(changes[r.PathValue("id")])
	})
	mux.HandleFunc("GET /v1/sessions/{id}/diff", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Query().Get("path")
		entry := r.URL.Query().Get("entry_id")
		key := path + ":" + entry
		if d, ok := diffs[key]; ok {
			_ = json.NewEncoder(w).Encode(api.DiffResponse{Diff: d})
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(api.ErrorResponse{Code: "not_found", Message: "diff not found"})
	})
	mux.HandleFunc("POST /v1/sessions/{id}/revert", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /v1/sessions/{id}/mode", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(api.ModeResponse{Mode: modes[r.PathValue("id")]})
	})
	mux.HandleFunc("PUT /v1/sessions/{id}/mode", func(w http.ResponseWriter, r *http.Request) {
		var req api.SetModeRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		modes[r.PathValue("id")] = req.Mode
		_ = json.NewEncoder(w).Encode(api.ModeResponse{Mode: req.Mode})
	})

	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() {
		_ = srv.Close()
		_ = os.Remove(sockPath)
	})

	c, err := client.New(sockPath, client.WithCWD("/workspace/project"))
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}
	return c, sockPath
}

func TestRegistryLookupSuggestAndExecute(t *testing.T) {
	reg := NewRegistry()
	cmds := reg.List()
	if len(cmds) == 0 {
		t.Fatalf("expected non-empty command registry")
	}

	// Lookup by primary name and alias
	if c, ok := reg.Lookup("help"); !ok || c.Name != "help" {
		t.Errorf("Lookup('help') failed: %+v", c)
	}
	if c, ok := reg.Lookup("h"); !ok || c.Name != "help" {
		t.Errorf("Lookup('h') alias failed: %+v", c)
	}
	if c, ok := reg.Lookup("/h"); !ok || c.Name != "help" {
		t.Errorf("Lookup('/h') with slash failed: %+v", c)
	}
	if _, ok := reg.Lookup("nonexistent"); ok {
		t.Errorf("Lookup('nonexistent') should return false")
	}

	// Suggest
	suggs := reg.Suggest("s")
	if len(suggs) < 2 { // sessions, switch
		t.Errorf("Suggest('s') expected >=2, got %d", len(suggs))
	}
	suggsLs := reg.Suggest("/l")
	if len(suggsLs) == 0 {
		t.Errorf("Suggest('/l') expected alias match for ls/sessions")
	}

	// Custom registration override
	reg.Register(Command{
		Name:        "custom",
		Description: "A custom test command",
		Handler: func(_ context.Context, _ *Env, _ []string) (Result, error) {
			return Result{Output: "custom ok"}, nil
		},
	})
	res, err := reg.Execute(context.Background(), nil, "/custom")
	if err != nil || res.Output != "custom ok" {
		t.Errorf("Execute custom failed: res=%+v, err=%v", res, err)
	}

	// Empty line execution
	resEmpty, errEmpty := reg.Execute(context.Background(), nil, "   ")
	if errEmpty != nil || resEmpty != (Result{}) {
		t.Errorf("Execute empty line error: res=%+v, err=%v", resEmpty, errEmpty)
	}

	// Unknown command error
	_, errUnknown := reg.Execute(context.Background(), nil, "/unknown_cmd")
	if errUnknown == nil || !strings.Contains(errUnknown.Error(), "unknown command") {
		t.Errorf("expected unknown command error, got %v", errUnknown)
	}
}

func TestBuiltinCommandsExecution(t *testing.T) {
	c, _ := setupTestDaemon(t)
	ctx := context.Background()
	reg := NewRegistry()

	sess := &api.Session{
		ID:    "s1",
		Title: "Session One",
		Agent: "build",
		Model: "gpt-5",
	}

	env := &Env{
		Client:       c,
		Workspace:    "/workspace/project",
		Session:      sess,
		Mode:         api.ModeDefault,
		DefaultAgent: "developer",
		DefaultModel: "claude-sonnet-4-6",
		DefaultTitle: "Test Title",
	}

	// 1. /help
	resHelp, err := reg.Execute(ctx, env, "/help")
	if err != nil || !strings.Contains(resHelp.Output, "Available commands:") {
		t.Errorf("/help failed: res=%+v, err=%v", resHelp, err)
	}

	// 2. /new
	resNew, err := reg.Execute(ctx, env, "/new my-model")
	if err != nil || !resNew.ResetSession || resNew.ModelOverride != "my-model" {
		t.Errorf("/new failed: res=%+v, err=%v", resNew, err)
	}

	// 3. /sessions
	resSessions, err := reg.Execute(ctx, env, "/sessions")
	if err != nil || !strings.Contains(resSessions.Output, "s1") {
		t.Errorf("/sessions failed: res=%+v, err=%v", resSessions, err)
	}

	// 4. /switch
	resSwitch, err := reg.Execute(ctx, env, "/switch s1")
	if err != nil || resSwitch.SwitchTo == nil || resSwitch.SwitchTo.ID != "s1" {
		t.Errorf("/switch failed: res=%+v, err=%v", resSwitch, err)
	}

	// 5. /history
	resHist, err := reg.Execute(ctx, env, "/history")
	if err != nil || !strings.Contains(resHist.Output, "Hello command test") {
		t.Errorf("/history failed: res=%+v, err=%v", resHist, err)
	}

	// 6. /mode read & set
	resModeRead, err := reg.Execute(ctx, env, "/mode")
	if err != nil || !strings.Contains(resModeRead.Output, "default") {
		t.Errorf("/mode read failed: res=%+v, err=%v", resModeRead, err)
	}
	if resModeRead.Picker == nil || len(resModeRead.Picker.Options) != 5 {
		t.Errorf("/mode read expected Picker with 5 options, got %+v", resModeRead.Picker)
	}
	resModeSet, err := reg.Execute(ctx, env, "/mode accept_edits")
	if err != nil || resModeSet.ModeChange != api.ModeAcceptEdits {
		t.Errorf("/mode set failed: res=%+v, err=%v", resModeSet, err)
	}

	// 7. /changes
	resChanges, err := reg.Execute(ctx, env, "/changes")
	if err != nil || !strings.Contains(resChanges.Output, "main.go") {
		t.Errorf("/changes failed: res=%+v, err=%v", resChanges, err)
	}

	// 8. /diff
	resDiff, err := reg.Execute(ctx, env, "/diff main.go c1")
	if err != nil || !strings.Contains(resDiff.Output, "main.go") {
		t.Errorf("/diff failed: res=%+v, err=%v", resDiff, err)
	}

	// 9. /revert
	resRevert, err := reg.Execute(ctx, env, "/revert main.go")
	if err != nil || !strings.Contains(resRevert.Output, "Reverted main.go.") {
		t.Errorf("/revert failed: res=%+v, err=%v", resRevert, err)
	}

	// 10. /clear
	resClear, err := reg.Execute(ctx, env, "/clear")
	if err != nil || !resClear.Clear {
		t.Errorf("/clear failed: res=%+v, err=%v", resClear, err)
	}

	// 11. /exit
	resExit, err := reg.Execute(ctx, env, "/exit")
	if err != nil || !resExit.Quit {
		t.Errorf("/exit failed: res=%+v, err=%v", resExit, err)
	}
}

func TestSuggestArgs(t *testing.T) {
	reg := NewRegistry()
	ctx := context.Background()
	env := &Env{
		Mode: api.ModeDefault,
	}

	// 1. Without space -> false
	if _, _, ok := reg.SuggestArgs(ctx, env, "/mode"); ok {
		t.Errorf("SuggestArgs without space should return false")
	}

	// 2. Trailing space -> all 5 options
	cmd, opts, ok := reg.SuggestArgs(ctx, env, "/mode ")
	if !ok || cmd.Name != "mode" || len(opts) != 5 {
		t.Fatalf("SuggestArgs('/mode ') = (%+v, len=%d, ok=%v)", cmd, len(opts), ok)
	}
	if !opts[0].Current {
		t.Errorf("expected opts[0] (default) to be Current")
	}

	// 3. Filter by prefix
	_, filteredAS, ok := reg.SuggestArgs(ctx, env, "/mode as")
	if !ok || len(filteredAS) != 1 || filteredAS[0].Value != "ask" {
		t.Errorf("SuggestArgs('/mode as') expected 'ask', got %+v", filteredAS)
	}

	// 4. Filter exact match
	_, filteredPlan, ok := reg.SuggestArgs(ctx, env, "/mode plan")
	if !ok || len(filteredPlan) != 1 || filteredPlan[0].Value != "plan" {
		t.Errorf("SuggestArgs('/mode plan') expected 'plan', got %+v", filteredPlan)
	}

	// 5. Command without options -> false
	if _, _, ok := reg.SuggestArgs(ctx, env, "/help "); ok {
		t.Errorf("SuggestArgs('/help ') should return false")
	}
}
