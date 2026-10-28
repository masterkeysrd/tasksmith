package session

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/loom/profile"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway"
	"github.com/masterkeysrd/tasksmith/internal/daemon/permissions"
	"github.com/masterkeysrd/tasksmith/internal/daemon/timeline"
)

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
		Models: []gateway.ModelSpec{{ID: "test-model", Capabilities: &gateway.ModelCapabilities{Thinking: []profile.ReasoningOption{{Type: "effort", Values: []string{"low", "high"}}, {Type: "toggle"}}}}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	perms, err := permissions.New(permissions.Config{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	mgr, err := New(Config{DB: db, Workspace: ws, Gateway: gw, Publisher: pub, Logger: log.Default(), Permissions: perms})
	if err != nil {
		t.Fatal(err)
	}
	sess, _, err := mgr.Create(ctx, CreateParams{Title: "test", Config: SessionConfig{Model: "local/test-model", Thinking: &api.ThinkingSetting{Type: "effort", Effort: "high"}}})
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
	updated, err := mgr.Update(ctx, sess.ID, UpdateParams{Config: SessionConfig{Thinking: &api.ThinkingSetting{Type: "toggle", Enabled: false}}})
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
	entries, err := sess.Timeline.ListByKind(ctx, timeline.KindThinking)
	if err != nil || len(entries) != 1 || entries[0].Thinking == nil || entries[0].Thinking.To == nil || entries[0].Thinking.To.Enabled {
		t.Fatalf("thinking timeline: %+v, %v", entries, err)
	}
	updated, err = mgr.Update(ctx, sess.ID, UpdateParams{Config: SessionConfig{Thinking: &api.ThinkingSetting{Type: "default"}}})
	if err != nil || updated.Config.Thinking != nil {
		t.Fatalf("reset: %+v, %v", updated, err)
	}
}
