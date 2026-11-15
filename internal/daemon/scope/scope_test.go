package scope_test

import (
	"context"
	"testing"

	"github.com/masterkeysrd/tasksmith/internal/daemon/scope"
)

func TestScopeProgressiveMerging(t *testing.T) {
	ctx := context.Background()

	// Initial check: empty context returns zero Scope and false
	if s, ok := scope.From(ctx); ok || s != (scope.Scope{}) {
		t.Fatalf("expected empty scope from background ctx, got ok=%v s=%+v", ok, s)
	}
	if s, ok := scope.From(nil); ok || s != (scope.Scope{}) {
		t.Fatalf("expected empty scope from nil ctx, got ok=%v s=%+v", ok, s)
	}

	// 1. Merge SessionID
	ctx1 := scope.With(ctx, scope.Scope{SessionID: "sess-123"})
	s1, ok := scope.From(ctx1)
	if !ok {
		t.Fatalf("expected ok=true for ctx1")
	}
	if s1.SessionID != "sess-123" || s1.TurnID != "" || s1.MessageID != "" || s1.ToolCallID != "" || s1.ToolName != "" {
		t.Fatalf("unexpected s1: %+v", s1)
	}

	// 2. Merge TurnID and MessageID (progressive merging should preserve SessionID)
	ctx2 := scope.With(ctx1, scope.Scope{
		TurnID:    "turn-456",
		MessageID: "msg-789",
	})
	s2, ok := scope.From(ctx2)
	if !ok {
		t.Fatalf("expected ok=true for ctx2")
	}
	if s2.SessionID != "sess-123" {
		t.Errorf("s2.SessionID = %q, want %q", s2.SessionID, "sess-123")
	}
	if s2.TurnID != "turn-456" {
		t.Errorf("s2.TurnID = %q, want %q", s2.TurnID, "turn-456")
	}
	if s2.MessageID != "msg-789" {
		t.Errorf("s2.MessageID = %q, want %q", s2.MessageID, "msg-789")
	}
	if s2.ToolCallID != "" || s2.ToolName != "" {
		t.Errorf("expected empty tool fields in s2, got %+v", s2)
	}

	// 3. Merge ToolCallID and ToolName (should preserve SessionID, TurnID, MessageID)
	ctx3 := scope.With(ctx2, scope.Scope{
		ToolCallID: "tc-001",
		ToolName:   "write",
	})
	s3, ok := scope.From(ctx3)
	if !ok {
		t.Fatalf("expected ok=true for ctx3")
	}
	if s3.SessionID != "sess-123" {
		t.Errorf("s3.SessionID = %q, want %q", s3.SessionID, "sess-123")
	}
	if s3.TurnID != "turn-456" {
		t.Errorf("s3.TurnID = %q, want %q", s3.TurnID, "turn-456")
	}
	if s3.MessageID != "msg-789" {
		t.Errorf("s3.MessageID = %q, want %q", s3.MessageID, "msg-789")
	}
	if s3.ToolCallID != "tc-001" {
		t.Errorf("s3.ToolCallID = %q, want %q", s3.ToolCallID, "tc-001")
	}
	if s3.ToolName != "write" {
		t.Errorf("s3.ToolName = %q, want %q", s3.ToolName, "write")
	}

	// 4. Merging empty scope does not overwrite anything
	ctx4 := scope.With(ctx3, scope.Scope{})
	s4, ok := scope.From(ctx4)
	if !ok || s4 != s3 {
		t.Errorf("s4 = %+v, want %+v", s4, s3)
	}

	// 5. With with nil context behaves safely
	ctxNil := scope.With(nil, scope.Scope{SessionID: "sess-nil"})
	sNil, ok := scope.From(ctxNil)
	if !ok || sNil.SessionID != "sess-nil" {
		t.Errorf("sNil = %+v, want SessionID=%q", sNil, "sess-nil")
	}
}
