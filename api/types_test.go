package api

import (
	"encoding/json"
	"strings"
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

func TestMessageStructuredContentJSONRoundTrip(t *testing.T) {
	for _, test := range []struct {
		name string
		raw  string
	}{
		{"absent", ""},
		{"object", `{"path":"file.go","start_line":2,"end_line":3}`},
		{"array", `["first",2]`},
		{"string", `"tool output"`},
		{"number", `9007199254740993`},
		{"null", `null`},
	} {
		t.Run(test.name, func(t *testing.T) {
			event := Event{
				Type: EventMessageAdded,
				Message: &Message{
					ID: "result", Role: RoleTool, ToolCallID: "call",
					StructuredContent: json.RawMessage(test.raw),
				},
			}
			data, err := json.Marshal(event)
			if err != nil {
				t.Fatal(err)
			}
			var decoded Event
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatal(err)
			}
			if decoded.Message == nil || string(decoded.Message.StructuredContent) != test.raw {
				t.Fatalf("structured content roundtrip = %+v, want %q", decoded.Message, test.raw)
			}
			if test.raw == "" && strings.Contains(string(data), "structured_content") {
				t.Fatalf("absent structured content was not omitted: %s", data)
			}
		})
	}
}

func TestWireTypesJSONRoundTrip(t *testing.T) {
	if HeaderCWD != "X-TaskSmith-CWD" {
		t.Fatalf("HeaderCWD = %q, want %q", HeaderCWD, "X-TaskSmith-CWD")
	}

	ws := Workspace{
		Name:         "my-ws",
		RootDir:      "/path/to/root",
		CWD:          "/path/to/root/pkg",
		DefaultAgent: "developer",
		DefaultModel: "ollama/qwen3.8",
	}
	wsRaw, err := json.Marshal(ws)
	if err != nil {
		t.Fatalf("Marshal Workspace: %v", err)
	}
	var wsDecoded Workspace
	if err := json.Unmarshal(wsRaw, &wsDecoded); err != nil {
		t.Fatalf("Unmarshal Workspace: %v", err)
	}
	if wsDecoded != ws {
		t.Fatalf("Workspace roundtrip mismatch: got %+v, want %+v", wsDecoded, ws)
	}

	ag := Agent{
		Name:        "developer",
		DisplayName: "Dev Agent",
		Description: "General coding agent",
		Models:      []string{"ollama/qwen3.8"},
		Effort:      "high",
	}
	agRaw, err := json.Marshal(ag)
	if err != nil {
		t.Fatalf("Marshal Agent: %v", err)
	}
	var agDecoded Agent
	if err := json.Unmarshal(agRaw, &agDecoded); err != nil {
		t.Fatalf("Unmarshal Agent: %v", err)
	}
	if agDecoded.Name != ag.Name || agDecoded.Effort != "high" || len(agDecoded.Models) != 1 {
		t.Fatalf("Agent roundtrip mismatch: got %+v, want %+v", agDecoded, ag)
	}

	mdl := Model{
		ID:             "ollama/qwen3.8",
		Provider:       "ollama",
		Name:           "qwen3.8",
		Label:          "Qwen 3.8",
		DefaultEffort:  "medium",
		AllowedEfforts: []string{"low", "medium", "high"},
	}
	mdlRaw, err := json.Marshal(mdl)
	if err != nil {
		t.Fatalf("Marshal Model: %v", err)
	}
	var mdlDecoded Model
	if err := json.Unmarshal(mdlRaw, &mdlDecoded); err != nil {
		t.Fatalf("Unmarshal Model: %v", err)
	}
	if mdlDecoded.ID != mdl.ID || mdlDecoded.Label != "Qwen 3.8" || len(mdlDecoded.AllowedEfforts) != 3 {
		t.Fatalf("Model roundtrip mismatch: got %+v, want %+v", mdlDecoded, mdl)
	}

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

	upReq := UpdateSessionRequest{
		Title: "New Title",
		Agent: "coder",
		Model: "claude-sonnet-4-6",
	}
	upRaw, err := json.Marshal(upReq)
	if err != nil {
		t.Fatalf("Marshal UpdateSessionRequest: %v", err)
	}
	var upDecoded UpdateSessionRequest
	if err := json.Unmarshal(upRaw, &upDecoded); err != nil {
		t.Fatalf("Unmarshal UpdateSessionRequest: %v", err)
	}
	if upDecoded != upReq {
		t.Fatalf("UpdateSessionRequest mismatch: got %+v, want %+v", upDecoded, upReq)
	}
}
