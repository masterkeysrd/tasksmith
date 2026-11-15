package session

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway"
	"github.com/masterkeysrd/tasksmith/internal/daemon/permissions"
	"github.com/masterkeysrd/warp"
)

func TestResolveConfigRejectsNonCompletionModel(t *testing.T) {
	_, ws, _, _, _ := setupTestEnv(t, nil)

	catalog := testCatalog(t, ws,
		warp.ProviderModel{ID: "chat-model"},
		warp.ProviderModel{ID: "embed-model", Capabilities: &warp.ProviderModelCapabilities{
			Completion: new(false),
			Embedding:  new(true),
		}},
	)

	if _, err := resolveConfig(ws, catalog(), api.SessionConfig{Model: "local/embed-model"}); err == nil {
		t.Fatal("expected an embedding-only model to be rejected for a chat session")
	} else if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got: %v", err)
	} else if !strings.Contains(err.Error(), "does not support chat completion") {
		t.Fatalf("error should explain the capability gap, got: %v", err)
	}

	resolved, err := resolveConfig(ws, catalog(), api.SessionConfig{Model: "local/chat-model"})
	if err != nil {
		t.Fatalf("completion-capable model was rejected: %v", err)
	}
	if resolved.Model != "local/chat-model" {
		t.Fatalf("resolved model = %q, want %q", resolved.Model, "local/chat-model")
	}
}

func TestResolveConfigKeepsUnreportedCompletionCapability(t *testing.T) {
	_, ws, _, _, _ := setupTestEnv(t, nil)

	catalog := testCatalog(t, ws, warp.ProviderModel{ID: "silent-model"})

	if _, err := resolveConfig(ws, catalog(), api.SessionConfig{Model: "local/silent-model"}); err != nil {
		t.Fatalf("a model with no reported completion flag must stay selectable, got: %v", err)
	}
}

func TestPreviewRejectsNonCompletionModel(t *testing.T) {
	ctx := context.Background()
	_, ws, _, _, _ := setupTestEnv(t, nil)

	catalog := testCatalog(t, ws,
		warp.ProviderModel{ID: "embed-model", Capabilities: &warp.ProviderModelCapabilities{Completion: new(false)}},
	)

	if _, _, _, _, err := Preview(ctx, ws, catalog(), api.SessionConfig{Model: "local/embed-model"}); err == nil {
		t.Fatal("expected Preview to reject an embedding-only model")
	} else if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got: %v", err)
	}
}

func TestResolveConfigRejectsNonCompletionDefaultFromAgent(t *testing.T) {
	_, ws, _, _, _ := setupTestEnv(t, nil)

	catalog := testCatalog(t, ws,
		warp.ProviderModel{ID: "embed-model", Capabilities: &warp.ProviderModelCapabilities{Completion: new(false), Embedding: new(true)}},
	)

	// With only an embedding-only model available, resolution either declines it
	// or leaves the session unset; it must never hand back that model.
	resolved, err := resolveConfig(ws, catalog(), api.SessionConfig{})
	if err != nil {
		t.Logf("resolution declined the only model: %v", err)
		return
	}
	if resolved.Model == "local/embed-model" {
		t.Fatal("resolution selected an embedding-only model for a chat session")
	}
}

func TestSessionUpdateRejectsNonCompletionModel(t *testing.T) {
	ctx := context.Background()
	db, ws, gw, pub, _ := setupTestEnv(t, nil)
	perms, err := permissions.New(permissions.Config{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}

	catalog := testCatalog(t, ws,
		warp.ProviderModel{ID: "chat-model"},
		warp.ProviderModel{ID: "embed-model", Aliases: []string{"embed"}, Capabilities: &warp.ProviderModelCapabilities{
			Completion: new(false),
			Embedding:  new(true),
		}},
	)
	mgr, err := newTestCoordinator(Config{
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
	defer func() { _ = mgr.Close() }()

	sess, _, err := mgr.Create(ctx, api.CreateSessionRequest{
		Title:  "test",
		Config: api.SessionConfig{Model: "local/chat-model"},
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, ref := range []string{"local/embed-model", "@embed"} {
		if _, err := mgr.Update(ctx, sess.ID, api.UpdateSessionRequest{
			Config: api.SessionConfig{Model: ref},
		}); err == nil {
			t.Fatalf("expected UpdateSession(%q) to reject non-completion model", ref)
		} else if !errors.Is(err, ErrInvalidConfig) || !strings.Contains(err.Error(), "does not support chat completion") {
			t.Fatalf("UpdateSession(%q) error = %v", ref, err)
		}
	}
}
