package session

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/loom/profile"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway"
	"github.com/masterkeysrd/tasksmith/internal/daemon/models"
	"github.com/masterkeysrd/tasksmith/internal/daemon/permissions"
	"github.com/masterkeysrd/warp"
)

func TestThinkingValidationAndConfig(t *testing.T) {
	prof := &profile.Profile{Capabilities: profile.Capabilities{ReasoningOptions: []profile.ReasoningOption{
		{Type: "effort", Values: []string{"minimal", "high"}},
		{Type: "budget_tokens", Min: 100, Max: 1000},
		{Type: "toggle"},
	}}}
	cases := []struct {
		name    string
		setting api.ThinkingSetting
		valid   bool
	}{
		{"effort", api.ThinkingSetting{Type: "effort", Effort: "minimal"}, true},
		{"invalid effort", api.ThinkingSetting{Type: "effort", Effort: "low"}, false},
		{"budget", api.ThinkingSetting{Type: "budget", Budget: 200}, true},
		{"budget below minimum", api.ThinkingSetting{Type: "budget", Budget: 99}, false},
		{"budget above maximum", api.ThinkingSetting{Type: "budget", Budget: 1001}, false},
		{"toggle off", api.ThinkingSetting{Type: "toggle", Enabled: false}, true},
		{"unknown", api.ThinkingSetting{Type: "other"}, false},
	}
	model := models.Merge("provider/model", "ollama", nil, prof)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resolved, _, err := models.Configure(model, models.Preferences{SessionThinking: &tc.setting})
			if (err == nil) != tc.valid {
				t.Fatalf("resolve thinking: %v", err)
			}
			if tc.valid && resolved.Thinking == nil {
				t.Fatalf("thinking missing: %+v", resolved)
			}
		})
	}
	if _, _, err := models.Configure(models.Model{}, models.Preferences{
		SessionThinking: &api.ThinkingSetting{Type: "toggle"},
	}); err == nil {
		t.Fatal("unknown model must not accept a toggle")
	}
	for _, setting := range []api.ThinkingSetting{
		{Type: "toggle", Enabled: false},
		{Type: "budget", Budget: 200},
		{Type: "effort", Effort: "minimal"},
	} {
		got, _, err := models.Configure(model, models.Preferences{SessionThinking: &setting})
		if err != nil || got.Thinking == nil || (setting.Type == "toggle" && got.Thinking.Enabled) ||
			(setting.Type == "budget" && got.Thinking.Budget != 200) ||
			(setting.Type == "effort" && got.Thinking.Effort != "minimal") {
			t.Fatalf("thinking setting %+v: %+v, %v", setting, got, err)
		}
	}
}

func TestThinkingDiagnostics(t *testing.T) {
	ctx := context.Background()
	db, ws, _, pub, _ := setupTestEnv(t, nil)
	dir := filepath.Join(ws.RootDir(), ".agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "---\napiVersion: warp/v1alpha1\nkind: Agent\nmetadata:\n  name: thinker\n" +
		"spec:\n  thinking:\n    effort: high\n---\nHelpful.\n"
	if err := os.WriteFile(filepath.Join(dir, "thinker.agent.md"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ws.Reload(ctx); err != nil {
		t.Fatal(err)
	}
	gw, err := gateway.New(ctx, gateway.Config{Providers: []gateway.ProviderConfig{{Name: "local", Type: "ollama"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	perms, err := permissions.New(permissions.Config{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	catalog := testCatalog(t, ws,
		warp.ProviderModel{
			ID: "supported",
			Capabilities: &warp.ProviderModelCapabilities{
				Thinking: []warp.ProviderModelThinkingCapability{
					{Type: "effort", AllowedEfforts: []string{"high"}},
				},
			},
		},
		warp.ProviderModel{ID: "unsupported", Capabilities: &warp.ProviderModelCapabilities{}},
	)
	mgr, err := New(Config{
		DB:          db,
		Workspace:   ws,
		Gateway:     func() gateway.Gateway { return gw },
		Publisher:   pub,
		Logger:      log.Default(),
		Permissions: perms,
		Catalog:     catalog,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()
	sess, _, err := mgr.Create(ctx, CreateParams{
		Title: "test",
		Config: SessionConfig{
			Agent: "thinker",
			Model: "local/unsupported",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	resolved, diagnostics, err := sess.resolveModelConfig(sess.Config)
	if err != nil || resolved.Thinking != nil || len(diagnostics) != 1 || diagnostics[0].Code != "thinking_unsupported" {
		t.Fatalf("agent thinking: %+v, %+v, %v", resolved, diagnostics, err)
	}
	_, err = mgr.Update(ctx, sess.ID, UpdateParams{
		Config: SessionConfig{
			Model:    "local/supported",
			Thinking: &api.ThinkingSetting{Type: "effort", Effort: "high"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	changed, err := gateway.New(ctx, gateway.Config{Providers: []gateway.ProviderConfig{{Name: "local", Type: "ollama"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer changed.Close()
	changedCatalog := testCatalog(t, ws, warp.ProviderModel{
		ID:           "supported",
		Capabilities: &warp.ProviderModelCapabilities{},
	})
	reopened, err := New(Config{
		DB:          db,
		Workspace:   ws,
		Gateway:     func() gateway.Gateway { return changed },
		Publisher:   pub,
		Logger:      log.Default(),
		Permissions: perms,
		Catalog:     changedCatalog,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	loaded, err := reopened.Get(ctx, sess.ID)
	if err != nil || loaded.Config.Thinking == nil || len(loaded.Diagnostics) != 1 ||
		loaded.Diagnostics[0].Code != "thinking_override_unsupported" {
		t.Fatalf("reopened diagnostics: %+v, %v", loaded, err)
	}
	updated, err := mgr.Update(ctx, sess.ID, UpdateParams{Config: SessionConfig{Model: "local/unsupported"}})
	if err != nil || updated.Config.Thinking != nil || len(updated.Diagnostics) != 2 ||
		updated.Diagnostics[1].Code != "thinking_override_cleared" {
		t.Fatalf("model change: %+v, %v", updated, err)
	}
	sess.Lock()
	sess.Config.Thinking = &api.ThinkingSetting{Type: "effort", Effort: "high"}
	sess.Unlock()
	mgr.Refresh()
	if sess.Config.Thinking == nil || len(sess.Diagnostics) != 1 ||
		sess.Diagnostics[0].Code != "thinking_override_unsupported" {
		t.Fatalf("reload diagnostics: %+v", sess.Diagnostics)
	}
}

func TestSessionThinkingEndToEnd(t *testing.T) {
	ctx := context.Background()
	requests := make(chan map[string]any, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if r.URL.Path != "/api/chat" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
			return
		}
		requests <- body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"test-model","message":{"role":"assistant","content":"done"},"done":true}` + "\n"))
	}))
	defer server.Close()
	db, ws, _, pub, _ := setupTestEnv(t, nil)
	gw, err := gateway.New(ctx, gateway.Config{Providers: []gateway.ProviderConfig{{
		Name: "local", Type: "ollama", Endpoint: server.URL,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	perms, err := permissions.New(permissions.Config{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	catalog := testCatalog(t, ws, warp.ProviderModel{
		ID: "test-model",
		Capabilities: &warp.ProviderModelCapabilities{
			Thinking: []warp.ProviderModelThinkingCapability{
				{Type: "effort", AllowedEfforts: []string{"low", "high"}},
				{Type: "toggle"},
			},
		},
	})
	mgr, err := New(Config{
		DB:          db,
		Workspace:   ws,
		Gateway:     func() gateway.Gateway { return gw },
		Publisher:   pub,
		Logger:      log.Default(),
		Permissions: perms,
		Catalog:     catalog,
	})
	if err != nil {
		t.Fatal(err)
	}
	sess, _, err := mgr.Create(ctx, CreateParams{
		Title: "test",
		Config: SessionConfig{
			Model:    "local/test-model",
			Thinking: &api.ThinkingSetting{Type: "effort", Effort: "high"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.Send(ctx, SendInput{Message: message.NewUserText("hello")}); err != nil {
		t.Fatal(err)
	}
	if err := sess.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	first := <-requests
	if got := first["think"]; got != "high" {
		t.Fatalf("effort request: %#v", first)
	}
	updated, err := mgr.Update(ctx, sess.ID, UpdateParams{
		Config: SessionConfig{
			Thinking: &api.ThinkingSetting{Type: "toggle", Enabled: false},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Config.Thinking == nil || updated.Config.Thinking.Enabled {
		t.Fatalf("toggle not stored: %+v", updated.Config)
	}
	if err := sess.Send(ctx, SendInput{Message: message.NewUserText("hello")}); err != nil {
		t.Fatal(err)
	}
	if err := sess.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	if got := (<-requests)["think"]; got != false {
		t.Fatalf("toggle request: %#v", got)
	}
	entries, err := sess.Timeline.ListByKind(ctx, api.TimelineKindThinking)
	if err != nil || len(entries) != 1 || entries[0].Thinking == nil ||
		entries[0].Thinking.To == nil || entries[0].Thinking.To.Enabled {
		t.Fatalf("thinking timeline: %+v, %v", entries, err)
	}
	updated, err = mgr.Update(ctx, sess.ID, UpdateParams{
		Config: SessionConfig{
			Thinking: &api.ThinkingSetting{Type: "default"},
		},
	})
	if err != nil || updated.Config.Thinking != nil {
		t.Fatalf("reset: %+v, %v", updated, err)
	}
}
