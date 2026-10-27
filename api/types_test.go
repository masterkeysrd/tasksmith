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
				Type: EventEntryAdded,
				Payload: EntryAddedEvent{
					SessionID: "s1",
					Entry: TimelineEntry{
						ID:   "result",
						Kind: TimelineKindMessage,
						Message: &Message{
							ID: "result", Role: RoleTool, ToolCallID: "call",
							StructuredContent: json.RawMessage(test.raw),
						},
					},
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
			payload, ok := decoded.Payload.(EntryAddedEvent)
			if !ok || payload.Entry.Message == nil || string(payload.Entry.Message.StructuredContent) != test.raw {
				t.Fatalf("structured content roundtrip = %+v, want %q", decoded.Payload, test.raw)
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
		Warnings:     []string{`provider "openai": missing required environment variable "OPENAI_API_KEY"`},
	}
	wsRaw, err := json.Marshal(ws)
	if err != nil {
		t.Fatalf("Marshal Workspace: %v", err)
	}
	var wsDecoded Workspace
	if err := json.Unmarshal(wsRaw, &wsDecoded); err != nil {
		t.Fatalf("Unmarshal Workspace: %v", err)
	}
	if !reflect.DeepEqual(wsDecoded, ws) {
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
		Type:  EventChunk,
		Topic: "sessions.sess-1.stream",
		Payload: ChunkEvent{
			SessionID: "sess-1",
			Chunk: Chunk{
				ID: "chk-1",
				Content: []Block{
					{Type: BlockReasoning, Text: "thinking...", Signature: "sig-1"},
					{Type: BlockText, Text: "hello"},
					{Type: BlockToolCall, ToolCall: &ToolCall{ID: "tc-1", Name: "view", Args: map[string]any{"path": "a.go"}}},
				},
				Done: false,
			},
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
	if decoded.Type != EventChunk || decoded.Topic != "sessions.sess-1.stream" {
		t.Fatalf("unexpected decoded Event: %+v", decoded)
	}
	chkPayload, ok := decoded.Payload.(ChunkEvent)
	if !ok || len(chkPayload.Chunk.Content) != 3 {
		t.Fatalf("unexpected chunk payload: %+v", decoded.Payload)
	}
	_ = now

	subReq := SubscriptionRequest{
		Topics: []string{"sessions", "sessions.s1.stream"},
		Since:  "0192e4c0-0000-7000-8000-000000000001",
	}
	subRaw, err := json.Marshal(subReq)
	if err != nil {
		t.Fatalf("Marshal SubscriptionRequest: %v", err)
	}
	var subDecoded SubscriptionRequest
	if err := json.Unmarshal(subRaw, &subDecoded); err != nil {
		t.Fatalf("Unmarshal SubscriptionRequest: %v", err)
	}
	if !reflect.DeepEqual(subReq, subDecoded) {
		t.Fatalf("SubscriptionRequest roundtrip mismatch: %+v vs %+v", subReq, subDecoded)
	}

	upReq := UpdateSessionRequest{
		Title: "New Title",
		Config: SessionConfig{
			Agent: "coder",
			Model: "claude-sonnet-4-6",
		},
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
			Command:     "ls -la",
			Description: "List directory contents",
			Dir:         "src",
		})
		testJSONRoundTrip(t, BashOutput{
			Output:     "output",
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

	t.Run("Tasks", func(t *testing.T) {
		testJSONRoundTrip(t, TasksInput{
			Action: "status",
			TaskID: "task-1",
			Limit:  50,
			Input:  "stdin input",
		})
		testJSONRoundTrip(t, TasksOutput{
			Status:   "running",
			ExitCode: 0,
			LogPath:  "/tmp/task-1.log",
			Output:   "some logs",
			Message:  "Task is running.",
			Tasks: []TasksTask{
				{
					ID:         "task-1",
					Kind:       "bash",
					Title:      "server",
					Status:     "running",
					DurationMs: 1200,
					Command:    "go run main.go",
					LogPath:    "/tmp/task-1.log",
				},
			},
		})
	})

	t.Run("Todos", func(t *testing.T) {
		testJSONRoundTrip(t, TodosInput{
			Todos: []TodoItem{
				{
					Content:    "Task 1",
					ActiveText: "Working on Task 1",
					Status:     "in_progress",
				},
			},
		})
		testJSONRoundTrip(t, TodosOutput{
			Todos: []TodoItem{
				{
					Content:    "Task 1",
					ActiveText: "Working on Task 1",
					Status:     "in_progress",
				},
			},
			Total:      1,
			Completed:  0,
			InProgress: 1,
			Pending:    0,
			Message:    "1 total",
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
			URL:     "https://example.com/api",
			Headers: map[string]string{"Content-Type": "application/json"},
		})
		testJSONRoundTrip(t, WebFetchOutput{
			URL:         "https://example.com/api",
			StatusCode:  200,
			ContentType: "application/json",
			Text:        `{"status":"ok"}`,
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

func TestHeaderAndTopicHelpers(t *testing.T) {
	if HeaderSubscriberID != "X-TaskSmith-Subscriber-ID" {
		t.Errorf("HeaderSubscriberID = %q, want X-TaskSmith-Subscriber-ID", HeaderSubscriberID)
	}
	if TopicSessions != "sessions" {
		t.Errorf("TopicSessions = %q, want sessions", TopicSessions)
	}
	if got := SessionStreamTopic("abc-123"); got != "sessions.abc-123.stream" {
		t.Errorf("SessionStreamTopic() = %q, want sessions.abc-123.stream", got)
	}
}

func TestSubscriptionDTOsRoundTrip(t *testing.T) {
	req := SubscriptionRequest{
		Topics: []string{"sessions", "sessions.123.stream"},
	}
	testJSONRoundTrip(t, req)

	resp := SubscriptionResponse{
		Topics: []string{"sessions"},
	}
	testJSONRoundTrip(t, resp)
}

func TestTimelineWireDTOs(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	entry := TimelineEntry{
		ID:        "entry-1",
		SessionID: "sess-1",
		Kind:      TimelineKindMessage,
		CreatedAt: now,
		Message: &Message{
			ID:      "m1",
			Role:    RoleUser,
			Content: []Block{{Type: BlockText, Text: "hi"}},
		},
		Model: &ModelChange{
			From: "claude-3-5-sonnet",
			To:   "claude-3-7-sonnet",
		},
		Effort: &EffortChange{
			From: "low",
			To:   "high",
		},
		Agent: &AgentChange{
			From: "coder",
			To:   "reviewer",
		},
		Mode: &ModeChange{
			From: ModeDefault,
			To:   ModePlan,
		},
		Permission: &PermissionRecord{
			Request: PermissionRequest{
				ID:         "p1",
				ToolCallID: "tc1",
				ToolName:   "write",
				Action:     ActionWrite,
				Safety:     SafetyUnsafe,
				Summary:    "write to file",
			},
			Decision: PermissionDecision{
				RequestID: "p1",
				Kind:      DecisionAllowOnce,
			},
		},
	}

	testJSONRoundTrip(t, entry)
}

func TestEventPayloadsAndJSONRoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	tests := []struct {
		name    string
		ev      Event
		wantTyp EventType
	}{
		{
			name: "connected",
			ev: Event{
				Type:  EventConnected,
				Topic: TopicSessions,
				Payload: ConnectedEvent{
					SubscriberID: "sub-123",
					Topics:       []string{TopicSessions, "sessions.s1.stream"},
				},
			},
			wantTyp: EventConnected,
		},
		{
			name: "session_created",
			ev: Event{
				Type:  EventSessionCreated,
				Topic: TopicSessions,
				Payload: SessionCreatedEvent{
					Workspace: "/workspace",
					Session: Session{
						ID:    "s1",
						Title: "New Session",
						Config: SessionConfig{
							Agent: "developer",
							Model: "gpt-4o",
						},
						Status:    StatusIdle,
						CreatedAt: now,
						UpdatedAt: now,
					},
				},
			},
			wantTyp: EventSessionCreated,
		},
		{
			name: "session_updated",
			ev: Event{
				Type:  EventSessionUpdated,
				Topic: TopicSessions,
				Payload: SessionUpdatedEvent{
					Workspace: "/workspace",
					Session: Session{
						ID:    "s1",
						Title: "Renamed Session",
						Config: SessionConfig{
							Agent: "developer",
							Model: "gpt-4o",
						},
						Status:    StatusThinking,
						CreatedAt: now,
						UpdatedAt: now,
					},
				},
			},
			wantTyp: EventSessionUpdated,
		},
		{
			name: "status_changed",
			ev: Event{
				Type:  EventStatusChanged,
				Topic: SessionStreamTopic("s1"),
				Payload: StatusChangedEvent{
					SessionID: "s1",
					Status:    StatusGenerating,
				},
			},
			wantTyp: EventStatusChanged,
		},
		{
			name: "session_deleted",
			ev: Event{
				Type:  EventSessionDeleted,
				Topic: TopicSessions,
				Payload: SessionDeletedEvent{
					Workspace: "/workspace",
					SessionID: "s1",
				},
			},
			wantTyp: EventSessionDeleted,
		},
		{
			name: "entry_added",
			ev: Event{
				Type:  EventEntryAdded,
				Topic: SessionStreamTopic("s1"),
				Payload: EntryAddedEvent{
					SessionID: "s1",
					Entry: TimelineEntry{
						ID:        "te-1",
						SessionID: "s1",
						Kind:      TimelineKindMessage,
						CreatedAt: now,
						Message: &Message{
							ID:      "msg-1",
							Role:    RoleUser,
							Content: []Block{{Type: BlockText, Text: "Hello"}},
						},
					},
				},
			},
			wantTyp: EventEntryAdded,
		},
		{
			name: "chunk",
			ev: Event{
				Type:  EventChunk,
				Topic: SessionStreamTopic("s1"),
				Payload: ChunkEvent{
					SessionID: "s1",
					Chunk: Chunk{
						ID:      "chk-1",
						Content: []Block{{Type: BlockText, Text: "part"}},
						Done:    false,
					},
				},
			},
			wantTyp: EventChunk,
		},
		{
			name: "permission_requested",
			ev: Event{
				Type:  EventPermissionRequested,
				Topic: SessionStreamTopic("s1"),
				Payload: PermissionRequestedEvent{
					SessionID: "s1",
					Permissions: []PermissionRequest{
						{
							ID:         "pr-1",
							ToolCallID: "tc-1",
							ToolName:   "write",
							Action:     ActionWrite,
							Safety:     SafetyUnsafe,
							Summary:    "Write file",
						},
					},
				},
			},
			wantTyp: EventPermissionRequested,
		},
		{
			name: "completed",
			ev: Event{
				ID:    "0192e4c0-0000-7000-8000-000000000001",
				Type:  EventCompleted,
				Topic: SessionStreamTopic("s1"),
				Payload: CompletedEvent{
					SessionID: "s1",
				},
			},
			wantTyp: EventCompleted,
		},
		{
			name: "task_updated",
			ev: Event{
				ID:    "0192e4c0-0000-7000-8000-000000000002",
				Type:  EventTaskUpdated,
				Topic: SessionStreamTopic("s1"),
				Payload: TaskUpdatedEvent{
					SessionID: "s1",
					Task: Task{
						ID:           "task-123",
						Kind:         "bash",
						Title:        "npm test",
						Command:      "npm test",
						Status:       TaskStatusRunning,
						ExitCode:     0,
						LogPath:      "/tmp/task-123.log",
						StartedAt:    now,
						DurationMs:   1500,
						Backgrounded: true,
					},
				},
			},
			wantTyp: EventTaskUpdated,
		},
		{
			name: "todos_updated",
			ev: Event{
				ID:    "0192e4c0-0000-7000-8000-000000000003",
				Type:  EventTodosUpdated,
				Topic: SessionStreamTopic("s1"),
				Payload: TodosUpdatedEvent{
					SessionID: "s1",
					Todos: []TodoItem{
						{
							Content:    "Write tests",
							ActiveText: "Writing tests",
							Status:     "in_progress",
						},
					},
				},
			},
			wantTyp: EventTodosUpdated,
		},
		{
			name: "error",
			ev: Event{
				Type:  EventError,
				Topic: SessionStreamTopic("s1"),
				Payload: ErrorEvent{
					SessionID: "s1",
					Error:     "context cancelled",
				},
			},
			wantTyp: EventError,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Verify EventType method on payload
			if got := tc.ev.Payload.EventType(); got != string(tc.wantTyp) {
				t.Errorf("Payload.EventType() = %q, want %q", got, tc.wantTyp)
			}

			// Marshal with explicit Type
			data, err := json.Marshal(tc.ev)
			if err != nil {
				t.Fatalf("json.Marshal(ev): %v", err)
			}

			// Unmarshal and verify
			var decoded Event
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("json.Unmarshal: %v", err)
			}
			if decoded.ID != tc.ev.ID {
				t.Errorf("decoded.ID = %q, want %q", decoded.ID, tc.ev.ID)
			}
			if decoded.Type != tc.wantTyp {
				t.Errorf("decoded.Type = %q, want %q", decoded.Type, tc.wantTyp)
			}
			if decoded.Topic != tc.ev.Topic {
				t.Errorf("decoded.Topic = %q, want %q", decoded.Topic, tc.ev.Topic)
			}
			if !reflect.DeepEqual(decoded.Payload, tc.ev.Payload) {
				t.Errorf("payload mismatch:\ngot  %+v\nwant %+v", decoded.Payload, tc.ev.Payload)
			}
		})
	}
}

func TestEventNullAndUnknownPayload(t *testing.T) {
	// Null payload
	rawNull := `{"type":"connected","topic":"sessions","payload":null}`
	var evNull Event
	if err := json.Unmarshal([]byte(rawNull), &evNull); err != nil {
		t.Fatalf("Unmarshal null payload: %v", err)
	}
	if evNull.Type != EventConnected || evNull.Payload != nil {
		t.Errorf("expected nil payload for null, got %+v", evNull)
	}

	// Unknown event type
	rawUnknown := `{"type":"future_type","topic":"future","payload":{"foo":"bar"}}`
	var evUnknown Event
	if err := json.Unmarshal([]byte(rawUnknown), &evUnknown); err == nil {
		t.Fatalf("expected error for unknown event type, got nil")
	} else if !strings.Contains(err.Error(), `unknown event type "future_type"`) {
		t.Fatalf("expected unknown event type error, got %v", err)
	}
}
