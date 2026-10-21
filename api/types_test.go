package api

import (
	"encoding/json"
	"testing"
	"time"
)

func TestAPIErrorFormatting(t *testing.T) {
	var nilErr *APIError
	if got := nilErr.Error(); got != "" {
		t.Errorf("nil APIError.Error() = %q, want empty", got)
	}

	withCode := &APIError{
		StatusCode: 404,
		Code:       "not_found",
		Message:    "session not found",
	}
	if got := withCode.Error(); got != "api: not_found (404): session not found" {
		t.Errorf("withCode.Error() = %q", got)
	}

	withoutCode := &APIError{
		StatusCode: 500,
		Message:    "internal error",
	}
	if got := withoutCode.Error(); got != "api: status 500: internal error" {
		t.Errorf("withoutCode.Error() = %q", got)
	}
}

func TestWireTypesJSONRoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	ev := Event{
		Type:      EventMessageAdded,
		Workspace: "/tmp/ws",
		SessionID: "sess-1",
		Message: &Message{
			ID:   "msg-1",
			Role: RoleAssistant,
			Content: []Block{
				{Type: BlockReasoning, Text: "thinking...", Signature: "sig-1"},
				{Type: BlockText, Text: "hello"},
				{Type: BlockToolCall, ToolCall: &ToolCall{ID: "tc-1", Name: "view", Args: map[string]any{"path": "a.go"}}},
			},
			CreatedAt: now,
		},
	}

	raw, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("Marshal Event: %v", err)
	}

	var decoded Event
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("Unmarshal Event: %v", err)
	}
	if decoded.Type != EventMessageAdded || decoded.Message == nil || len(decoded.Message.Content) != 3 {
		t.Fatalf("unexpected decoded Event: %+v", decoded)
	}
}
