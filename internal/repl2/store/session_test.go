package store

import (
	"context"
	"testing"

	"github.com/masterkeysrd/tasksmith/api"
)

func TestSessionStoreBasic(t *testing.T) {
	ResetSession()
	defer ResetSession()

	s := GetSession()
	if s.Config.Mode != api.ModeDefault {
		t.Fatalf("expected ModeDefault initially, got %q", s.Config.Mode)
	}
	if s.Ready {
		t.Fatal("expected Ready to be false initially")
	}

	SetMode(api.ModePlan)
	if GetSession().Config.Mode != api.ModePlan {
		t.Fatalf("expected ModePlan, got %q", GetSession().Config.Mode)
	}

	SetModelInfo("Claude 3.7", "high")
	s = GetSession()
	if s.ModelLabel != "Claude 3.7" || s.Thinking != "high" || !s.Ready {
		t.Fatalf("unexpected model info: %+v", s)
	}

	// SetSession with empty mode should default to ModeDefault
	sess := &api.Session{
		ID: "sess-1",
		Config: api.SessionConfig{
			Agent: "coding",
			Model: "claude-3-7",
		},
	}
	SetSession(sess)
	s = GetSession()
	if s.Session != sess || s.Config.Mode != api.ModeDefault {
		t.Fatalf("unexpected session after SetSession: %+v", s)
	}

	// SetSession with explicit mode
	sess.Config.Mode = api.ModeAcceptEdits
	SetSession(sess)
	if GetSession().Config.Mode != api.ModeAcceptEdits {
		t.Fatalf("expected ModeAcceptEdits, got %q", GetSession().Config.Mode)
	}

	ResetSession()
	s = GetSession()
	if s.Config.Mode != api.ModeDefault || s.Session != nil || s.Ready {
		t.Fatalf("expected clean state after ResetSession, got %+v", s)
	}
}

func TestResolveModelInfoNilClient(t *testing.T) {
	ctx := context.Background()

	// Default fallback
	label, thinking := ResolveModelInfo(ctx, nil, "", "", nil)
	if label != "default" || thinking != "auto" {
		t.Fatalf("expected default/auto, got %q/%q", label, thinking)
	}

	// Passed config
	label, thinking = ResolveModelInfo(ctx, nil, "agent1", "gpt-4", nil)
	if label != "gpt-4" || thinking != "auto" {
		t.Fatalf("expected gpt-4/auto, got %q/%q", label, thinking)
	}

	// Sess overrides cfg
	sess := &api.Session{
		Config: api.SessionConfig{
			Agent: "sess-agent",
			Model: "claude-3-7",
		},
	}
	label, thinking = ResolveModelInfo(ctx, nil, "agent1", "gpt-4", sess)
	if label != "claude-3-7" || thinking != "auto" {
		t.Fatalf("expected claude-3-7/auto from sess, got %q/%q", label, thinking)
	}
}
