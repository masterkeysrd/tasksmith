package api

import (
	"encoding/json"
	"reflect"
	"slices"
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
		DefaultModel: &Model{ID: "ollama/qwen3.8"},
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
		Thinking:    &ThinkingSetting{Type: "effort", Effort: "high"},
	}
	agRaw, err := json.Marshal(ag)
	if err != nil {
		t.Fatalf("Marshal Agent: %v", err)
	}
	var agDecoded Agent
	if err := json.Unmarshal(agRaw, &agDecoded); err != nil {
		t.Fatalf("Unmarshal Agent: %v", err)
	}
	if agDecoded.Name != ag.Name || agDecoded.Thinking == nil || agDecoded.Thinking.Effort != "high" || len(agDecoded.Models) != 1 {
		t.Fatalf("Agent roundtrip mismatch: got %+v, want %+v", agDecoded, ag)
	}

	mdl := Model{
		ID:       "ollama/qwen3.8",
		Provider: "ollama",
		Name:     "qwen3.8",
		Label:    "Qwen 3.8",
		Thinking: []ThinkingCapability{{Type: "effort", Default: "medium", Values: []ThinkingValue{{Value: "low", Description: "Less reasoning; potentially faster."}, {Value: "medium"}, {Value: "custom"}}}},
	}
	mdlRaw, err := json.Marshal(mdl)
	if err != nil {
		t.Fatalf("Marshal Model: %v", err)
	}
	var mdlDecoded Model
	if err := json.Unmarshal(mdlRaw, &mdlDecoded); err != nil {
		t.Fatalf("Unmarshal Model: %v", err)
	}
	if !reflect.DeepEqual(mdlDecoded, mdl) {
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

func TestToolWireTypesJSONRoundTrip(t *testing.T) {
	custom := Tool{
		Name:         "linter",
		DisplayName:  "Linter",
		Description:  "Lint the workspace",
		Namespace:    "workspace",
		Scope:        "Workspace",
		Command:      []string{"golangci-lint", "run", "{{.Input.path}}"},
		InputSchema:  map[string]any{"type": "object", "required": []any{"path"}},
		OutputSchema: map[string]any{"type": "string"},
		Annotations: ToolAnnotation{
			IsOpenWorld:  true,
			IsDangerous:  true,
			IsIdempotent: true,
			UserHint:     "Lint %s",
		},
		Tokens: 240,
		Status: ToolStatusEager,
	}
	testJSONRoundTrip(t, custom)

	raw, err := json.Marshal(custom)
	if err != nil {
		t.Fatalf("Marshal Tool: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("Unmarshal Tool fields: %v", err)
	}
	for _, key := range []string{"name", "display_name", "description", "namespace", "scope", "command", "input_schema", "output_schema", "annotations", "tokens", "status"} {
		if _, ok := fields[key]; !ok {
			t.Errorf("serialized Tool missing wire key %q: %s", key, raw)
		}
	}
	annotations, ok := fields["annotations"].(map[string]any)
	if !ok {
		t.Fatalf("annotations = %T, want object", fields["annotations"])
	}
	for _, key := range []string{"is_open_world", "is_dangerous", "is_idempotent", "user_hint"} {
		if _, ok := annotations[key]; !ok {
			t.Errorf("serialized ToolAnnotation missing wire key %q: %s", key, raw)
		}
	}
	if _, ok := annotations["is_read_only"]; ok {
		t.Errorf("serialized ToolAnnotation should omit false is_read_only: %s", raw)
	}

	// A built-in host-native tool omits command, schemas, and tokens, and always
	// carries the annotations object.
	builtin := Tool{
		Name:        "view",
		DisplayName: "View File",
		Namespace:   "system",
		Scope:       "Builtin",
		Annotations: ToolAnnotation{IsReadOnly: true, IsIdempotent: true},
	}
	builtinRaw, err := json.Marshal(builtin)
	if err != nil {
		t.Fatalf("Marshal builtin Tool: %v", err)
	}
	var builtinFields map[string]any
	if err := json.Unmarshal(builtinRaw, &builtinFields); err != nil {
		t.Fatalf("Unmarshal builtin Tool fields: %v", err)
	}
	for _, key := range []string{"command", "input_schema", "output_schema", "tokens", "description", "status"} {
		if _, ok := builtinFields[key]; ok {
			t.Errorf("serialized builtin Tool should omit %q: %s", key, builtinRaw)
		}
	}
	if _, ok := builtinFields["annotations"]; !ok {
		t.Errorf("serialized builtin Tool should include annotations: %s", builtinRaw)
	}
	testJSONRoundTrip(t, builtin)
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
			Content:    "1\tHello world\n2\tLine 2",
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
			name: "compaction_started",
			ev: Event{
				Type:  EventCompactionStarted,
				Topic: SessionStreamTopic("s1"),
				Payload: CompactionStartedEvent{
					SessionID: "s1",
					ID:        "compact-1",
					Trigger:   "auto",
					Focus:     "tests",
				},
			},
			wantTyp: EventCompactionStarted,
		},
		{
			name: "context_updated",
			ev: Event{
				Type:  EventContextUpdated,
				Topic: SessionStreamTopic("s1"),
				Payload: ContextUpdatedEvent{
					SessionID: "s1",
					Context: ContextState{
						UsedTokens:    12000,
						MaxTokens:     128000,
						OutputLimit:   16384,
						ReserveTokens: 24384,
						TriggerTokens: 103616,
						FreeTokens:    91616,
						Breakdown: ContextBreakdown{
							SystemPrompt:   3000,
							ContextFiles:   1000,
							SystemTools:    2000,
							CustomTools:    500,
							MCPTools:       200,
							Skills:         400,
							Subagents:      300,
							Summary:        1000,
							UserMessages:   600,
							AgentResponses: 1500,
							ToolCalls:      1500,
						},
						InputTokens:   11500,
						OutputTokens:  500,
						CachedTokens:  8000,
						Compactions:   1,
						LastCompacted: now,
					},
				},
			},
			wantTyp: EventContextUpdated,
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

func TestContextAndCompactionTypes(t *testing.T) {
	// 1. SessionStatus compacting
	if StatusCompacting != "compacting" {
		t.Fatalf("StatusCompacting = %q, want %q", StatusCompacting, "compacting")
	}

	// 2. QueueKind compact and QueueItem
	queueItem := QueueItem{
		ID:        "qi-1",
		Kind:      QueueKindCompact,
		Compact:   &CompactRequest{Focus: "preserve test instructions"},
		CreatedAt: time.Now().UTC().Truncate(time.Second),
	}
	qiData, err := json.Marshal(queueItem)
	if err != nil {
		t.Fatalf("Marshal QueueItem: %v", err)
	}
	var qiDecoded QueueItem
	if err := json.Unmarshal(qiData, &qiDecoded); err != nil {
		t.Fatalf("Unmarshal QueueItem: %v", err)
	}
	if qiDecoded.Kind != QueueKindCompact {
		t.Errorf("qiDecoded.Kind = %q, want %q", qiDecoded.Kind, QueueKindCompact)
	}
	if qiDecoded.Compact == nil || qiDecoded.Compact.Focus != "preserve test instructions" {
		t.Errorf("qiDecoded.Compact = %+v, want focus %q", qiDecoded.Compact, "preserve test instructions")
	}

	// 3. Agent and Model context limit fields
	agent := Agent{
		Name: "coder",
		Context: ContextBreakdown{
			SystemPrompt: 1200,
			Skills:       300,
		},
	}
	agentData, err := json.Marshal(agent)
	if err != nil {
		t.Fatalf("Marshal Agent: %v", err)
	}
	var agentDecoded Agent
	if err := json.Unmarshal(agentData, &agentDecoded); err != nil {
		t.Fatalf("Unmarshal Agent: %v", err)
	}
	if agentDecoded.Context.SystemPrompt != 1200 || agentDecoded.Context.Skills != 300 {
		t.Errorf("agentDecoded.Context mismatch: %+v", agentDecoded.Context)
	}

	model := Model{
		ID:           "test-model",
		ContextLimit: 200000,
		OutputLimit:  32000,
	}
	modelData, err := json.Marshal(model)
	if err != nil {
		t.Fatalf("Marshal Model: %v", err)
	}
	var modelDecoded Model
	if err := json.Unmarshal(modelData, &modelDecoded); err != nil {
		t.Fatalf("Unmarshal Model: %v", err)
	}
	if modelDecoded.ContextLimit != 200000 || modelDecoded.OutputLimit != 32000 {
		t.Errorf("modelDecoded limits mismatch: ContextLimit=%d, OutputLimit=%d", modelDecoded.ContextLimit, modelDecoded.OutputLimit)
	}

	// 4. SendRequest with CompactRequest
	sr := SendRequest{
		ID:      "sr-1",
		Compact: &CompactRequest{Focus: "api focus"},
	}
	srData, err := json.Marshal(sr)
	if err != nil {
		t.Fatalf("Marshal SendRequest: %v", err)
	}
	var srDecoded SendRequest
	if err := json.Unmarshal(srData, &srDecoded); err != nil {
		t.Fatalf("Unmarshal SendRequest: %v", err)
	}
	if srDecoded.Compact == nil || srDecoded.Compact.Focus != "api focus" {
		t.Errorf("srDecoded.Compact mismatch: %+v", srDecoded.Compact)
	}

	// 5. Session with ContextState
	now := time.Now().UTC().Truncate(time.Second)
	sess := Session{
		ID:     "s-1",
		Title:  "Test Session",
		Status: StatusCompacting,
		Context: ContextState{
			UsedTokens:    50000,
			MaxTokens:     200000,
			OutputLimit:   32000,
			ReserveTokens: 40000,
			TriggerTokens: 160000,
			FreeTokens:    110000,
			Breakdown: ContextBreakdown{
				SystemPrompt:   4000,
				ContextFiles:   2000,
				SystemTools:    3000,
				CustomTools:    1000,
				MCPTools:       500,
				Skills:         500,
				Subagents:      500,
				Summary:        10000,
				UserMessages:   2500,
				AgentResponses: 12000,
				ToolCalls:      14000,
			},
			InputTokens:   48000,
			OutputTokens:  2000,
			CachedTokens:  35000,
			Compactions:   2,
			LastCompacted: now,
		},
	}
	sessData, err := json.Marshal(sess)
	if err != nil {
		t.Fatalf("Marshal Session: %v", err)
	}
	var sessDecoded Session
	if err := json.Unmarshal(sessData, &sessDecoded); err != nil {
		t.Fatalf("Unmarshal Session: %v", err)
	}
	if sessDecoded.Status != StatusCompacting {
		t.Errorf("sessDecoded.Status = %q, want %q", sessDecoded.Status, StatusCompacting)
	}
	if sessDecoded.Context.UsedTokens != 50000 || sessDecoded.Context.Breakdown.Summary != 10000 {
		t.Errorf("sessDecoded.Context mismatch: %+v", sessDecoded.Context)
	}
	if sessDecoded.Context.Compactions != 2 {
		t.Errorf("sessDecoded.Context.Compactions = %d, want 2", sessDecoded.Context.Compactions)
	}

	// 6. TimelineEntry with CompactedRecord and CacheBrokenRecord
	compactedEntry := TimelineEntry{
		ID:        "tl-comp-1",
		SessionID: "s-1",
		Kind:      TimelineKindCompacted,
		CreatedAt: now,
		Compacted: &CompactedRecord{
			Trigger:      "auto",
			Focus:        "focus-area",
			Summary:      "## Summary text",
			BeforeTokens: 162000,
			AfterTokens:  15000,
			DurationMs:   2340,
		},
	}
	ceData, err := json.Marshal(compactedEntry)
	if err != nil {
		t.Fatalf("Marshal TimelineEntry(compacted): %v", err)
	}
	var ceDecoded TimelineEntry
	if err := json.Unmarshal(ceData, &ceDecoded); err != nil {
		t.Fatalf("Unmarshal TimelineEntry(compacted): %v", err)
	}
	if ceDecoded.Kind != TimelineKindCompacted || ceDecoded.Compacted == nil {
		t.Fatalf("ceDecoded compacted mismatch: %+v", ceDecoded)
	}
	if ceDecoded.Compacted.BeforeTokens != 162000 || ceDecoded.Compacted.AfterTokens != 15000 || ceDecoded.Compacted.DurationMs != 2340 {
		t.Errorf("CompactedRecord fields mismatch: %+v", ceDecoded.Compacted)
	}

	cacheBrokenEntry := TimelineEntry{
		ID:        "tl-cb-1",
		SessionID: "s-1",
		Kind:      TimelineKindCacheBroken,
		CreatedAt: now,
		CacheBroken: &CacheBrokenRecord{
			PrevCached:  50000,
			CurrCached:  0,
			InputTokens: 55000,
		},
	}
	cbData, err := json.Marshal(cacheBrokenEntry)
	if err != nil {
		t.Fatalf("Marshal TimelineEntry(cache_broken): %v", err)
	}
	var cbDecoded TimelineEntry
	if err := json.Unmarshal(cbData, &cbDecoded); err != nil {
		t.Fatalf("Unmarshal TimelineEntry(cache_broken): %v", err)
	}
	if cbDecoded.Kind != TimelineKindCacheBroken || cbDecoded.CacheBroken == nil {
		t.Fatalf("cbDecoded cache_broken mismatch: %+v", cbDecoded)
	}
	if cbDecoded.CacheBroken.PrevCached != 50000 || cbDecoded.CacheBroken.CurrCached != 0 || cbDecoded.CacheBroken.InputTokens != 55000 {
		t.Errorf("CacheBrokenRecord fields mismatch: %+v", cbDecoded.CacheBroken)
	}
}

func sortedFieldNames(fields map[string]json.RawMessage) []string {
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func TestMCPServerJSONRoundTrip(t *testing.T) {
	server := MCPServer{
		Name:        "github",
		DisplayName: "GitHub",
		Description: "GitHub MCP server",
		Namespace:   "workspace",
		Scope:       "Workspace",
		Path:        ".agents/github.mcp.yaml",
		Transport:   "stdio",
		Command:     []string{"npx", "-y", "@modelcontextprotocol/server-github"},
		AuthType:    "oauth2",
		Status:      MCPStatusAuthRequired,
		AuthURL:     "https://github.com/login/oauth/authorize?state=abc",
		Tools: []MCPToolSummary{
			{
				Name:        "github__create_issue",
				RawName:     "create_issue",
				Description: "Create an issue",
				Allowed:     true,
				Annotations: ToolAnnotation{IsReadOnly: false, IsDangerous: true, UserHint: "Create issue"},
				InputSchema: map[string]any{"type": "object"},
			},
			{
				Name:    "github__delete_repo",
				RawName: "delete_repo",
				Allowed: false,
			},
		},
		Prompts: []MCPPromptSummary{
			{
				Name:        "review-pr",
				Description: "Review a pull request",
				Arguments:   []MCPPromptArgument{{Name: "pr", Title: "PR", Description: "PR number", Required: true}},
				Allowed:     true,
			},
		},
		Resources: []MCPResourceSummary{
			{Name: "contributing", Title: "Contributing", URI: "file://CONTRIBUTING.md", MIMEType: "text/markdown", Size: 2048, Allowed: false},
		},
	}

	data, err := json.Marshal(server)
	if err != nil {
		t.Fatalf("Marshal MCPServer: %v", err)
	}

	var decoded MCPServer
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal MCPServer: %v", err)
	}
	if decoded.Status != MCPStatusAuthRequired || decoded.AuthURL != server.AuthURL {
		t.Errorf("status/authUrl mismatch: %+v", decoded)
	}
	if len(decoded.Tools) != 2 || decoded.Tools[0].Name != "github__create_issue" || !decoded.Tools[0].Allowed {
		t.Errorf("tools mismatch: %+v", decoded.Tools)
	}
	if decoded.Tools[1].Allowed {
		t.Errorf("prohibited tool must round-trip with allowed=false: %+v", decoded.Tools[1])
	}
	if decoded.Tools[0].Annotations.UserHint != "Create issue" || !decoded.Tools[0].Annotations.IsDangerous {
		t.Errorf("annotations mismatch: %+v", decoded.Tools[0].Annotations)
	}
	if len(decoded.Prompts) != 1 || len(decoded.Prompts[0].Arguments) != 1 || !decoded.Prompts[0].Arguments[0].Required {
		t.Errorf("prompts mismatch: %+v", decoded.Prompts)
	}
	if len(decoded.Resources) != 1 || decoded.Resources[0].URI != "file://CONTRIBUTING.md" || decoded.Resources[0].Size != 2048 {
		t.Errorf("resources mismatch: %+v", decoded.Resources)
	}

	// Required fields are always present; optional ones are omitted when empty.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatalf("unmarshal MCPServer into field map: %v", err)
	}
	for _, required := range []string{"name", "transport", "status"} {
		if _, ok := fields[required]; !ok {
			t.Errorf("expected required JSON field %q, got fields %v", required, sortedFieldNames(fields))
		}
	}
	for _, optional := range []string{"endpoint", "error"} {
		if _, ok := fields[optional]; ok {
			t.Errorf("expected empty optional JSON field %q to be omitted", optional)
		}
	}

	// A disconnected server with nothing discovered reports an empty tool list.
	empty, err := json.Marshal(MCPServer{Name: "offline", Transport: "stdio", Status: MCPStatusDisconnected})
	if err != nil {
		t.Fatalf("Marshal empty MCPServer: %v", err)
	}
	for _, field := range []string{"tools", "prompts", "resources", "command"} {
		if strings.Contains(string(empty), `"`+field+`"`) {
			t.Errorf("expected empty discovery field %q to be omitted, got %s", field, empty)
		}
	}
}

func TestMCPStatusValues(t *testing.T) {
	want := []MCPStatus{
		MCPStatusDisconnected,
		MCPStatusConnecting,
		MCPStatusConnected,
		MCPStatusAuthRequired,
		MCPStatusError,
	}
	for i, status := range want {
		if string(status) == "" {
			t.Fatalf("MCPStatus at index %d is empty", i)
		}
	}
	if got := []string{
		string(MCPStatusDisconnected),
		string(MCPStatusConnecting),
		string(MCPStatusConnected),
		string(MCPStatusAuthRequired),
		string(MCPStatusError),
	}; !reflect.DeepEqual(got, []string{"disconnected", "connecting", "connected", "auth_required", "error"}) {
		t.Errorf("MCPStatus wire values = %v", got)
	}
}

func TestMCPOperationRequestResponseJSON(t *testing.T) {
	authData, err := json.Marshal(MCPAuthResponse{AuthURL: "https://example.com/authorize", Status: MCPStatusAuthRequired})
	if err != nil {
		t.Fatalf("Marshal MCPAuthResponse: %v", err)
	}
	if !strings.Contains(string(authData), `"authUrl":"https://example.com/authorize"`) {
		t.Errorf("MCPAuthResponse JSON = %s", authData)
	}

	promptReq := MCPPromptRequest{Args: map[string]string{"pr": "42"}, AllowProhibited: true}
	promptReqData, err := json.Marshal(promptReq)
	if err != nil {
		t.Fatalf("Marshal MCPPromptRequest: %v", err)
	}
	var decodedPromptReq MCPPromptRequest
	if err := json.Unmarshal(promptReqData, &decodedPromptReq); err != nil {
		t.Fatalf("Unmarshal MCPPromptRequest: %v", err)
	}
	if decodedPromptReq.Args["pr"] != "42" || !decodedPromptReq.AllowProhibited {
		t.Errorf("MCPPromptRequest round-trip = %+v (json %s)", decodedPromptReq, promptReqData)
	}

	promptRespData, err := json.Marshal(MCPPromptResponse{
		Description: "Review a pull request",
		Messages:    []MCPPromptMessage{{Role: RoleUser, Text: "Review PR 42"}},
	})
	if err != nil {
		t.Fatalf("Marshal MCPPromptResponse: %v", err)
	}
	var decodedPromptResp MCPPromptResponse
	if err := json.Unmarshal(promptRespData, &decodedPromptResp); err != nil {
		t.Fatalf("Unmarshal MCPPromptResponse: %v", err)
	}
	if len(decodedPromptResp.Messages) != 1 || decodedPromptResp.Messages[0].Role != RoleUser {
		t.Errorf("MCPPromptResponse round-trip = %+v", decodedPromptResp)
	}

	resourceReqData, err := json.Marshal(MCPResourceRequest{URI: "file://CONTRIBUTING.md"})
	if err != nil {
		t.Fatalf("Marshal MCPResourceRequest: %v", err)
	}
	if strings.Contains(string(resourceReqData), "allowProhibited") {
		t.Errorf("expected allowProhibited to be omitted when false, got %s", resourceReqData)
	}
	var decodedResourceReq MCPResourceRequest
	if err := json.Unmarshal(resourceReqData, &decodedResourceReq); err != nil {
		t.Fatalf("Unmarshal MCPResourceRequest: %v", err)
	}
	if decodedResourceReq.URI != "file://CONTRIBUTING.md" || decodedResourceReq.AllowProhibited {
		t.Errorf("MCPResourceRequest round-trip = %+v", decodedResourceReq)
	}

	resourceResp := MCPResourceResponse{URI: "file://CONTRIBUTING.md", MIMEType: "text/markdown", Text: "# Contributing"}
	resourceRespData, err := json.Marshal(resourceResp)
	if err != nil {
		t.Fatalf("Marshal MCPResourceResponse: %v", err)
	}
	var decodedResourceResp MCPResourceResponse
	if err := json.Unmarshal(resourceRespData, &decodedResourceResp); err != nil {
		t.Fatalf("Unmarshal MCPResourceResponse: %v", err)
	}
	if decodedResourceResp != resourceResp {
		t.Errorf("MCPResourceResponse round-trip = %+v, want %+v", decodedResourceResp, resourceResp)
	}

	allowReqData, err := json.Marshal(MCPAllowPolicyRequest{Kind: MCPAllowPolicyKindResource, Value: "file://CONTRIBUTING.md"})
	if err != nil {
		t.Fatalf("Marshal MCPAllowPolicyRequest: %v", err)
	}
	if !strings.Contains(string(allowReqData), `"kind":"resource"`) {
		t.Errorf("MCPAllowPolicyRequest JSON = %s", allowReqData)
	}
}

func TestSandboxModeIsValid(t *testing.T) {
	for _, m := range []SandboxMode{SandboxNative, SandboxLocal} {
		if !m.IsValid() {
			t.Errorf("%q.IsValid() = false, want true", m)
		}
		if m.String() != string(m) {
			t.Errorf("%q.String() = %q", m, m.String())
		}
	}
	for _, m := range []SandboxMode{"", "docker", "NATIVE"} {
		if m.IsValid() {
			t.Errorf("%q.IsValid() = true, want false", m)
		}
	}
}
