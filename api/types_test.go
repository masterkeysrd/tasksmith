package api

import (
	"encoding/json"
	"reflect"
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

func testJSONRoundTrip[T any](t *testing.T, original T) {
	t.Helper()
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal(%T): %v", original, err)
	}
	var decoded T
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal(%T): %v", original, err)
	}
	if !reflect.DeepEqual(decoded, original) {
		t.Fatalf("%T roundtrip mismatch:\ngot:  %+v\nwant: %+v", original, decoded, original)
	}
}

func TestGeneratedToolTypesJSONRoundTrip(t *testing.T) {
	t.Run("Bash", func(t *testing.T) {
		testJSONRoundTrip(t, BashInput{
			Command:   "ls -la",
			Dir:       "src",
			Env:       map[string]string{"DEBUG": "1"},
			Stdin:     "input line",
			TimeoutMs: 5000,
		})
		testJSONRoundTrip(t, BashOutput{
			Stdout:     "output",
			Stderr:     "warning",
			ExitCode:   0,
			DurationMs: 123,
		})
	})

	t.Run("Download", func(t *testing.T) {
		testJSONRoundTrip(t, DownloadInput{
			URL:       "https://example.com/archive.tar.gz",
			Path:      "downloads/archive.tar.gz",
			Force:     true,
			Headers:   map[string]string{"Authorization": "Bearer token"},
			MaxBytes:  1048576,
			TimeoutMs: 10000,
		})
		testJSONRoundTrip(t, DownloadOutput{
			URL:          "https://example.com/archive.tar.gz",
			Path:         "downloads/archive.tar.gz",
			StatusCode:   200,
			BytesWritten: 1024,
			Created:      true,
			AfterHash:    "deadbeef",
			BeforeHash:   "oldbeef",
			ContentType:  "application/gzip",
			IsBinary:     true,
		})
	})

	t.Run("Edit", func(t *testing.T) {
		testJSONRoundTrip(t, EditInput{
			Path:       "file.go",
			OldText:    "old",
			NewText:    "new",
			Force:      true,
			ReplaceAll: true,
		})
		testJSONRoundTrip(t, EditOutput{
			Path:         "file.go",
			Replacements: 2,
			Additions:    2,
			Deletions:    2,
			BeforeHash:   "hash1",
			AfterHash:    "hash2",
			Diff:         "--- file.go\n+++ file.go\n",
		})
	})

	t.Run("Glob", func(t *testing.T) {
		testJSONRoundTrip(t, GlobInput{
			Pattern:       "**/*.go",
			Ignore:        []string{"vendor/**"},
			IncludeHidden: true,
			Limit:         100,
			Path:          ".",
		})
		testJSONRoundTrip(t, GlobOutput{
			Pattern:   "**/*.go",
			Path:      ".",
			Matches:   []string{"main.go", "api/types.go"},
			Truncated: false,
		})
	})

	t.Run("Grep", func(t *testing.T) {
		testJSONRoundTrip(t, GrepInput{
			Pattern:         `func Test\w+`,
			CaseInsensitive: true,
			ContextLines:    2,
			Include:         "*.go",
			IncludeHidden:   false,
			Limit:           50,
			Path:            "api",
		})
		testJSONRoundTrip(t, GrepOutput{
			Pattern: `func Test\w+`,
			Matches: []GrepMatch{
				{
					Path:   "api/types_test.go",
					Line:   42,
					Column: 1,
					Text:   "func TestMain(t *testing.T) {",
					Before: []string{"// Comment line 1", "// Comment line 2"},
					After:  []string{"\tt.Parallel()"},
				},
			},
			Truncated: true,
		})
	})

	t.Run("Ls", func(t *testing.T) {
		testJSONRoundTrip(t, LsInput{
			Ignore:        []string{"*.log"},
			IncludeHidden: true,
			Limit:         50,
			MaxDepth:      2,
			Path:          "internal",
			Recursive:     true,
		})
		testJSONRoundTrip(t, LsOutput{
			Path: "internal",
			Entries: []LsEntry{
				{
					Path:  "internal/tools",
					Name:  "tools",
					IsDir: true,
					Size:  0,
				},
				{
					Path:  "internal/doc.go",
					Name:  "doc.go",
					IsDir: false,
					Size:  128,
				},
			},
			Truncated: false,
		})
	})

	t.Run("Remove", func(t *testing.T) {
		testJSONRoundTrip(t, RemoveInput{
			Path:  "scratch.txt",
			Force: true,
		})
		testJSONRoundTrip(t, RemoveOutput{
			Path:       "scratch.txt",
			Deletions:  5,
			BeforeHash: "cafe1234",
			Diff:       "deleted file mode 100644",
		})
	})

	t.Run("View", func(t *testing.T) {
		testJSONRoundTrip(t, ViewInput{
			Path:   "README.md",
			Limit:  20,
			Offset: 1,
		})
		testJSONRoundTrip(t, ViewOutput{
			Path:       "README.md",
			Content:    "Hello world\nLine 2",
			Numbered:   "1\tHello world\n2\tLine 2",
			StartLine:  1,
			EndLine:    2,
			TotalLines: 2,
			Hash:       "abcd5678",
			Truncated:  false,
		})
	})

	t.Run("WebFetch", func(t *testing.T) {
		testJSONRoundTrip(t, WebFetchInput{
			URL:       "https://example.com/api",
			Body:      `{"key":"val"}`,
			Headers:   map[string]string{"Content-Type": "application/json"},
			MaxBytes:  4096,
			Method:    "POST",
			TimeoutMs: 5000,
		})
		testJSONRoundTrip(t, WebFetchOutput{
			URL:         "https://example.com/api",
			StatusCode:  200,
			Status:      "200 OK",
			ContentType: "application/json",
			Headers:     map[string]string{"Content-Type": "application/json"},
			Text:        `{"status":"ok"}`,
			Truncated:   false,
		})
	})

	t.Run("Write", func(t *testing.T) {
		testJSONRoundTrip(t, WriteInput{
			Path:    "output.txt",
			Content: "generated content\n",
			Force:   true,
		})
		testJSONRoundTrip(t, WriteOutput{
			Path:         "output.txt",
			Created:      true,
			BytesWritten: 18,
			Additions:    1,
			Deletions:    0,
			AfterHash:    "feed9999",
			BeforeHash:   "beef8888",
			Diff:         "+generated content\n",
		})
	})
}
