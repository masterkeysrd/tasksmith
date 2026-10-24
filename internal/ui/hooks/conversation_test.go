package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
	uiclient "github.com/masterkeysrd/tasksmith/internal/ui/client"
)

func conversationText(id string, role api.Role, text string) api.Message {
	return api.Message{ID: id, Role: role, Content: []api.Block{{Type: api.BlockText, Text: text}}}
}

func conversationTool(id string) api.Block {
	return api.Block{Type: api.BlockToolCall, ToolCall: &api.ToolCall{
		ID: id, Name: "read_file", Args: map[string]any{"path": id + ".go"},
	}}
}

func testEntryAdded(sessionID string, msg api.Message) api.Event {
	var topic string
	if sessionID != "" {
		topic = api.SessionStreamTopic(sessionID)
	}
	return api.Event{
		Topic: topic,
		Type:  api.EventEntryAdded,
		Payload: api.EntryAddedEvent{
			SessionID: sessionID,
			Entry: api.TimelineEntry{
				ID:        msg.ID,
				SessionID: sessionID,
				Kind:      api.TimelineKindMessage,
				Message:   &msg,
			},
		},
	}
}

func testChunk(sessionID string, chunk api.Chunk) api.Event {
	var topic string
	if sessionID != "" {
		topic = api.SessionStreamTopic(sessionID)
	}
	return api.Event{
		Topic: topic,
		Type:  api.EventChunk,
		Payload: api.ChunkEvent{
			SessionID: sessionID,
			Chunk:     chunk,
		},
	}
}

func testPermissions(sessionID string, perms ...api.PermissionRequest) api.Event {
	var topic string
	if sessionID != "" {
		topic = api.SessionStreamTopic(sessionID)
	}
	return api.Event{
		Topic: topic,
		Type:  api.EventPermissionRequested,
		Payload: api.PermissionRequestedEvent{
			SessionID:   sessionID,
			Permissions: perms,
		},
	}
}

func testCompleted(sessionID string, _ *api.Message) api.Event {
	var topic string
	if sessionID != "" {
		topic = api.SessionStreamTopic(sessionID)
	}
	return api.Event{
		Topic: topic,
		Type:  api.EventCompleted,
		Payload: api.CompletedEvent{
			SessionID: sessionID,
		},
	}
}

func testError(sessionID string, err string) api.Event {
	var topic string
	if sessionID != "" {
		topic = api.SessionStreamTopic(sessionID)
	}
	return api.Event{
		Topic: topic,
		Type:  api.EventError,
		Payload: api.ErrorEvent{
			SessionID: sessionID,
			Error:     err,
		},
	}
}

func applyConversation(t *testing.T, state *conversationState, events ...api.Event) {
	t.Helper()
	for _, event := range events {
		if err := state.apply(event); err != nil {
			t.Fatalf("apply %q: %v", event.Type, err)
		}
	}
}

func TestConversationOrderedPartsAndParallelTools(t *testing.T) {
	state := conversationState{sessionID: "s1", draft: -1, status: StreamRunning}
	user := conversationText("user", api.RoleUser, "inspect")
	assistant := api.Message{ID: "assistant", Role: api.RoleAssistant, Content: []api.Block{
		{Type: api.BlockReasoning, Text: "first thought", Signature: "signed"},
		{Type: api.BlockText, Text: "before"},
		conversationTool("a"),
		{Type: api.BlockReasoning, Text: "second thought"},
		conversationTool("b"),
		{Type: api.BlockText, Text: "after"},
	}}
	permission := api.PermissionRequest{ID: "approval", ToolCallID: "a", Diff: "full diff", GrantPatterns: []string{"read:*"}}
	applyConversation(t, &state,
		testEntryAdded("s1", user),
		testEntryAdded("s1", assistant),
		testPermissions("s1", permission),
	)
	before := state.view()
	if len(before.History) != 1 || len(before.Stream.Messages) != 1 {
		t.Fatalf("partition = %+v", before)
	}
	parts := before.Stream.Messages[0].Parts
	var kinds []MessagePartType
	for _, part := range parts {
		kinds = append(kinds, part.Type)
	}
	if !slices.Equal(kinds, []MessagePartType{PartThought, PartText, PartTool, PartThought, PartTool, PartText}) {
		t.Fatalf("part order = %v", kinds)
	}
	if parts[0].Thought.Signature != "signed" || parts[2].Tool.Status != ToolWaitingPermission {
		t.Fatalf("thought/permission = %+v, %+v", parts[0].Thought, parts[2].Tool)
	}
	b := conversationText("response-b", api.RoleTool, "complete successful output")
	b.ToolCallID, b.Name = "b", "read_file"
	b.Content = append(b.Content, api.Block{Type: api.BlockText, Text: "\nsecond block"})
	reply := conversationText("reply", api.RoleAssistant, "finished")
	applyConversation(t, &state,
		testEntryAdded("s1", b),
		testEntryAdded("s1", reply),
	)
	during := state.view()
	if len(during.History) != 1 || len(during.Stream.Messages) != 2 {
		t.Fatalf("later messages escaped the mutable suffix: %+v", during)
	}
	if !reflect.DeepEqual(during.Stream.Messages[0].Parts[4].Tool.Response, &b) {
		t.Fatal("successful output was not preserved")
	}
	a := conversationText("response-a", api.RoleTool, "permission denied, with details")
	a.ToolCallID, a.IsError = "a", true
	applyConversation(t, &state,
		testEntryAdded("s1", a),
		testCompleted("s1", &reply),
	)
	after := state.view()
	if len(after.History) != 3 || len(after.Stream.Messages) != 0 || len(after.Permissions) != 0 {
		t.Fatalf("final partition = %+v", after)
	}
	if after.History[1].Parts[2].Tool.Status != ToolFailed || after.History[1].Parts[4].Tool.Status != ToolSucceeded {
		t.Fatal("responses were not joined to their own calls")
	}
	if parts[2].Tool.Response != nil || parts[4].Tool.Response != nil || len(parts[2].Tool.Permissions) != 1 {
		t.Fatal("previously published snapshot was mutated")
	}
}

func TestConversationStructuredContentSnapshotIsolation(t *testing.T) {
	const output = `{"path":"file.go","start_line":2,"end_line":3}`
	for _, matched := range []bool{false, true} {
		t.Run(fmt.Sprintf("matched=%v", matched), func(t *testing.T) {
			state := conversationState{sessionID: "s1", draft: -1, status: StreamRunning}
			if matched {
				call := api.Message{ID: "call", Role: api.RoleAssistant, Content: []api.Block{conversationTool("a")}}
				applyConversation(t, &state, testEntryAdded("s1", call))
			}
			response := api.Message{
				ID: "result", Role: api.RoleTool, ToolCallID: "a",
				StructuredContent: json.RawMessage(output),
			}
			applyConversation(t, &state, testEntryAdded("s1", response))
			response.StructuredContent[0] = '['
			view := state.view()
			messages := append(view.History, view.Stream.Messages...)
			got := messages[0].Parts[0].Tool.Response.StructuredContent
			if string(got) != output {
				t.Fatalf("incoming response mutated conversation state: %s", got)
			}
			got[0] = '['
			view = state.view()
			messages = append(view.History, view.Stream.Messages...)
			if got := messages[0].Parts[0].Tool.Response.StructuredContent; string(got) != output {
				t.Fatalf("published response mutated conversation state: %s", got)
			}
		})
	}
}

func TestConversationDraftReconciliationAndIdentity(t *testing.T) {
	state := conversationState{sessionID: "s1", draft: -1, status: StreamIdle}
	applyConversation(t, &state, testChunk("s1", api.Chunk{
		ID: "provider-id", Content: []api.Block{{Type: api.BlockReasoning, Text: "think"}},
	}))
	first := state.view().Stream.Messages[0]
	applyConversation(t, &state, testChunk("s1", api.Chunk{
		ID: "provider-id", Content: []api.Block{
			{Type: api.BlockReasoning, Text: " more"},
			{Type: api.BlockText, Text: "answer"},
		}, Done: true,
	}))
	draft := state.view().Stream.Messages[0]
	if draft.ID != first.ID || draft.Parts[0].ID != first.Parts[0].ID ||
		draft.Parts[0].Thought.Text != "think more" || first.Parts[0].Thought.Text != "think" ||
		draft.Streaming || draft.Parts[0].Thought.Streaming {
		t.Fatalf("draft snapshots = %+v, %+v", first, draft)
	}
	final := api.Message{ID: "persisted-id", Role: api.RoleAssistant, Content: []api.Block{
		{Type: api.BlockReasoning, Text: "think more", Signature: "final signature"},
		{Type: api.BlockText, Text: "answer, finalized"},
	}}
	applyConversation(t, &state,
		testEntryAdded("s1", final),
		testEntryAdded("s1", final),
		testCompleted("s1", &final),
	)
	view := state.view()
	if len(view.History) != 1 || len(view.Stream.Messages) != 0 {
		t.Fatalf("duplicate final message: %+v", view)
	}
	message := view.History[0]
	if message.ID != first.ID || message.SourceID != final.ID || message.Parts[0].ID != first.Parts[0].ID ||
		message.Parts[1].Text.Text != "answer, finalized" || message.Parts[0].Thought.Signature != "final signature" {
		t.Fatalf("final message = %+v", message)
	}
}

func TestConversationPartialToolArguments(t *testing.T) {
	state := conversationState{sessionID: "s1", draft: -1, status: StreamIdle}
	applyConversation(t, &state, testChunk("s1", api.Chunk{Content: []api.Block{
		{Type: api.BlockToolCall, ToolCall: &api.ToolCall{Name: "read_file"}},
	}}))
	before := state.view().Stream.Messages[0].Parts[0]
	applyConversation(t, &state, testChunk("s1", api.Chunk{Content: []api.Block{conversationTool("a")}}))
	after := state.view().Stream.Messages[0].Parts[0]
	if before.ID != after.ID || before.Tool.ID != "" || after.Tool.ID != "a" ||
		after.Tool.Call.Args["path"] != "a.go" || before.Tool.Call.Args != nil || after.Tool.Status != ToolReceiving {
		t.Fatalf("partial call changed identity or an earlier snapshot: before=%+v, after=%+v", before, after)
	}
	update := conversationTool("a")
	update.ToolCall.Args["path"] = "updated.go"
	applyConversation(t, &state,
		testChunk("s1", api.Chunk{Content: []api.Block{conversationTool("b")}}),
		testChunk("s1", api.Chunk{Content: []api.Block{update}}),
	)
	parts := state.view().Stream.Messages[0].Parts
	if len(parts) != 2 || parts[0].ID != before.ID || parts[0].Tool.Call.Args["path"] != "updated.go" ||
		parts[1].Tool.ID != "b" || after.Tool.Call.Args["path"] != "a.go" {
		t.Fatalf("interleaved call updates created duplicates: %+v", parts)
	}
}

func TestConversationNotificationDuringDraftKeepsPersistedOrder(t *testing.T) {
	state := conversationState{sessionID: "s1", draft: -1, status: StreamIdle}
	notification := conversationText("notice", api.RoleSystem, "notice")
	final := conversationText("final", api.RoleAssistant, "answer")
	applyConversation(t, &state,
		testChunk("s1", api.Chunk{Content: final.Content}),
		testEntryAdded("s1", notification),
		testEntryAdded("s1", final),
	)
	view := state.view()
	if len(view.History) != 2 || view.History[0].SourceID != "notice" || view.History[1].SourceID != "final" {
		t.Fatalf("persisted message order = %+v", view.History)
	}
}

func TestConversationKeepsUnmatchedData(t *testing.T) {
	state := conversationState{sessionID: "s1", draft: -1, status: StreamRunning}
	request := api.PermissionRequest{ID: "p1", ToolCallID: "later"}
	applyConversation(t, &state, testPermissions("s1", request))
	if len(state.view().Permissions) != 1 {
		t.Fatal("unmatched permission was dropped")
	}
	response := conversationText("result", api.RoleTool, "orphan output")
	response.ToolCallID = "later"
	applyConversation(t, &state, testEntryAdded("s1", response))
	view := state.view()
	if len(view.History) != 0 || len(view.Stream.Messages) != 1 ||
		view.Stream.Messages[0].Parts[0].Tool.Call != nil ||
		view.Stream.Messages[0].Parts[0].Tool.Response.Content[0].Text != "orphan output" {
		t.Fatalf("unmatched response = %+v", view)
	}
	call := api.Message{ID: "call", Role: api.RoleAssistant, Content: []api.Block{conversationTool("later")}}
	applyConversation(t, &state, testEntryAdded("s1", call))
	view = state.view()
	if len(view.History) != 1 || len(view.Stream.Messages) != 0 || view.History[0].Parts[0].Tool.Response.ID != "result" {
		t.Fatalf("late call did not reconcile: %+v", view)
	}
}

func TestConversationReplayDoesNotReopenResolvedPermissions(t *testing.T) {
	state := conversationState{sessionID: "s1", draft: -1, status: StreamIdle}
	call := api.Message{ID: "call", Role: api.RoleAssistant, Content: []api.Block{conversationTool("tool")}}
	result := conversationText("result", api.RoleTool, "done")
	result.ToolCallID = "tool"
	for _, message := range []api.Message{call, result} {
		if err := state.addMessage(message); err != nil {
			t.Fatal(err)
		}
	}
	before := state.view()
	applyConversation(t, &state,
		testChunk("s1", api.Chunk{ID: "provider", Content: call.Content}),
		testEntryAdded("s1", call),
		testPermissions("s1", api.PermissionRequest{ID: "p", ToolCallID: "tool"}),
		testEntryAdded("s1", result),
		testCompleted("s1", nil),
	)
	after := state.view()
	if !reflect.DeepEqual(before.History, after.History) || len(after.Stream.Messages) != 0 || len(after.Permissions) != 0 {
		t.Fatalf("replay changed finalized data: %+v", after)
	}
}

func TestConversationFailureRetainsDraftAndPermissions(t *testing.T) {
	state := conversationState{sessionID: "s1", draft: -1, status: StreamIdle}
	applyConversation(t, &state,
		testChunk("s1", api.Chunk{Content: []api.Block{
			{Type: api.BlockText, Text: "partial"}, conversationTool("a"),
		}}),
		testPermissions("s1", api.PermissionRequest{ID: "p", ToolCallID: "a"}),
		testError("s1", "cancelled"),
	)
	view := state.view()
	if view.Error == nil || view.Stream.Status != StreamFailed || view.Stream.Messages[0].Streaming ||
		view.Stream.Messages[0].Parts[0].Text.Text != "partial" ||
		view.Stream.Messages[0].Parts[1].Tool.Status != ToolInterrupted ||
		len(view.Permissions) != 1 || len(view.Stream.Messages[0].Parts[1].Tool.Permissions) != 1 {
		t.Fatalf("failed stream = %+v", view)
	}
}

func TestConversationRejectsMalformedData(t *testing.T) {
	call := api.Message{ID: "call", Role: api.RoleAssistant, Content: []api.Block{conversationTool("a")}}
	tests := []struct {
		name    string
		prepare []api.Event
		event   api.Event
	}{
		{name: "missing message", event: api.Event{Type: api.EventEntryAdded, Payload: api.EntryAddedEvent{Entry: api.TimelineEntry{Kind: api.TimelineKindMessage}}}},
		{name: "missing chunk", event: api.Event{Type: api.EventChunk}},
		{name: "unknown event", event: api.Event{Type: "future"}},
		{name: "unknown block", event: testChunk("", api.Chunk{Content: []api.Block{{Type: "image"}}})},
		{name: "nil call", event: testChunk("", api.Chunk{Content: []api.Block{{Type: api.BlockToolCall}}})},
		{name: "missing ID", event: testEntryAdded("", api.Message{Role: api.RoleUser})},
		{name: "unlinked response", event: testEntryAdded("", api.Message{ID: "result", Role: api.RoleTool})},
		{name: "invalid permission", event: testPermissions("", api.PermissionRequest{ID: "p"})},
		{name: "incomplete draft", prepare: []api.Event{testChunk("", api.Chunk{})}, event: testCompleted("", nil)},
		{name: "missing response", prepare: []api.Event{testEntryAdded("", call)}, event: testCompleted("", nil)},
		{name: "conflicting ID", prepare: []api.Event{testEntryAdded("", call)},
			event: testEntryAdded("", api.Message{ID: "call", Role: api.RoleAssistant})},
		{name: "reused tool ID", prepare: []api.Event{testEntryAdded("", call)},
			event: testEntryAdded("", api.Message{ID: "other", Role: api.RoleAssistant, Content: call.Content})},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := conversationState{draft: -1, status: StreamIdle}
			applyConversation(t, &state, test.prepare...)
			if err := state.apply(test.event); err == nil {
				t.Fatal("malformed event was accepted")
			}
		})
	}
}

type conversationTestStream struct {
	ctx    context.Context
	frames chan []byte
	buffer []byte
	closed chan struct{}
	once   sync.Once
}

func (s *conversationTestStream) Read(p []byte) (int, error) {
	for len(s.buffer) == 0 {
		select {
		case <-s.ctx.Done():
			return 0, s.ctx.Err()
		case <-s.closed:
			return 0, io.EOF
		case s.buffer = <-s.frames:
		}
	}
	n := copy(p, s.buffer)
	s.buffer = s.buffer[n:]
	return n, nil
}

func (s *conversationTestStream) Close() error {
	s.once.Do(func() { close(s.closed) })
	return nil
}

func (s *conversationTestStream) send(t *testing.T, events ...api.Event) {
	t.Helper()
	var frame bytes.Buffer
	for _, event := range events {
		frame.WriteString("data: ")
		if err := json.NewEncoder(&frame).Encode(event); err != nil {
			t.Fatal(err)
		}
		frame.WriteByte('\n')
	}
	select {
	case s.frames <- frame.Bytes():
	case <-t.Context().Done():
		t.Fatal("timed out sending conversation events")
	}
}

func awaitConversationConnection(t *testing.T, connections <-chan *conversationTestStream) *conversationTestStream {
	t.Helper()
	select {
	case stream := <-connections:
		return stream
	case <-time.After(3 * time.Second):
		t.Fatal("timed out opening conversation stream")
		return nil
	}
}

type conversationTestServer struct {
	mu          sync.Mutex
	history     []api.Message
	permissions []api.PermissionRequest
	status      api.SessionStatus
	failPath    string
	onHistory   func(*http.Request) error
	connections chan *conversationTestStream
}

func newConversationTestServer() *conversationTestServer {
	return &conversationTestServer{status: api.StatusIdle, connections: make(chan *conversationTestStream, 10)}
}

func (s *conversationTestServer) roundTrip(r *http.Request) (*http.Response, error) {
	if (strings.HasSuffix(r.URL.Path, "/messages") || strings.HasSuffix(r.URL.Path, "/timeline")) && s.onHistory != nil {
		if err := s.onHistory(r); err != nil {
			return nil, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.URL.Path == s.failPath {
		return response(r, http.StatusServiceUnavailable, api.ErrorResponse{Message: "offline"}), nil
	}
	switch {
	case r.URL.Path == "/v1/events":
		stream := &conversationTestStream{ctx: r.Context(), frames: make(chan []byte, 10), closed: make(chan struct{})}
		var initialFrame bytes.Buffer
		connEv := api.Event{
			Type: api.EventConnected,
			Payload: api.ConnectedEvent{
				SubscriberID: "test-sub",
				Topics:       r.URL.Query()["topic"],
			},
		}
		initialFrame.WriteString("data: ")
		_ = json.NewEncoder(&initialFrame).Encode(connEv)
		initialFrame.WriteByte('\n')
		stream.frames <- initialFrame.Bytes()

		s.connections <- stream
		return &http.Response{StatusCode: http.StatusOK, Body: stream, Header: http.Header{
			"Content-Type":         {"text/event-stream"},
			api.HeaderSubscriberID: {"test-sub"},
		}, Request: r}, nil
	case r.URL.Path == "/v1/events/subscribe" || r.URL.Path == "/v1/events/unsubscribe":
		var req api.SubscriptionRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		return response(r, http.StatusOK, api.SubscriptionResponse{Topics: req.Topics}), nil
	case strings.HasSuffix(r.URL.Path, "/messages"):
		return response(r, http.StatusOK, s.history), nil
	case strings.HasSuffix(r.URL.Path, "/timeline"):
		entries := make([]api.TimelineEntry, len(s.history))
		for i, msg := range s.history {
			m := msg
			entries[i] = api.TimelineEntry{
				ID:        msg.ID,
				SessionID: "s1",
				Kind:      api.TimelineKindMessage,
				Message:   &m,
			}
		}
		return response(r, http.StatusOK, entries), nil
	case strings.HasSuffix(r.URL.Path, "/permissions"):
		return response(r, http.StatusOK, s.permissions), nil
	default:
		return response(r, http.StatusOK, api.Session{ID: "s1", Status: s.status}), nil
	}
}

func TestUseConversationHydratesAfterConnectingAndFoldsEveryEvent(t *testing.T) {
	server := newConversationTestServer()
	server.history = []api.Message{conversationText("user", api.RoleUser, "hello")}
	c := hookClient(t, "/daemon.sock", "/workspace", server.roundTrip)
	inst, load, _ := mountHooks(t, c, func() Conversation { return UseConversation("s1") })
	awaitHooks(t, load, func(c *Conversation) bool { return !c.IsLoading && len(c.History) == 1 })
	stream := awaitConversationConnection(t, server.connections)
	var events []api.Event
	var text strings.Builder
	for i := range 100 {
		delta := fmt.Sprintf("%d ", i)
		text.WriteString(delta)
		events = append(events, testChunk("s1", api.Chunk{
			ID: "provider", Content: []api.Block{{Type: api.BlockText, Text: delta}},
		}))
	}
	stream.send(t, events...)
	draft := awaitHooks(t, load, func(c *Conversation) bool {
		return len(c.Stream.Messages) == 1 && c.Stream.Messages[0].Parts[0].Text.Text == text.String()
	})
	id := draft.Stream.Messages[0].ID
	final := conversationText("persisted", api.RoleAssistant, text.String())
	stream.send(t,
		testEntryAdded("s1", final),
		testCompleted("s1", &final),
	)
	result := awaitHooks(t, load, func(c *Conversation) bool { return c.Stream.Status == StreamCompleted })
	if result.Error != nil || len(result.History) != 2 || result.History[1].ID != id ||
		result.History[1].SourceID != "persisted" || len(result.Stream.Messages) != 0 {
		t.Fatalf("completed conversation = %+v", result)
	}
	server.mu.Lock()
	server.history = append(server.history, final)
	server.mu.Unlock()
	inst.Engine().PostMacro(func() { load().Refetch() })
	result = awaitHooks(t, load, func(c *Conversation) bool {
		return !c.IsLoading && len(c.History) == 2 && c.Stream.Status == StreamIdle
	})
	if result.History[1].ID != id {
		t.Fatal("refetch changed a finalized message's view identity")
	}
}

func TestUseConversationReconcilesEventsBufferedDuringHydration(t *testing.T) {
	for _, included := range []bool{false, true} {
		t.Run(fmt.Sprintf("history-includes-events=%v", included), func(t *testing.T) {
			server := newConversationTestServer()
			server.history = []api.Message{conversationText("old", api.RoleUser, "old")}
			started, release := make(chan struct{}), make(chan struct{})
			server.onHistory = func(r *http.Request) error {
				close(started)
				select {
				case <-release:
					return nil
				case <-r.Context().Done():
					return r.Context().Err()
				}
			}
			c := hookClient(t, "/daemon.sock", "/workspace", server.roundTrip)
			_, load, _ := mountHooks(t, c, func() Conversation { return UseConversation("s1") })
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				t.Fatal("history never started")
			}
			user := conversationText("new-user", api.RoleUser, "new")
			reply := conversationText("new-assistant", api.RoleAssistant, "new response")
			if included {
				server.mu.Lock()
				server.history = append(server.history, user, reply)
				server.mu.Unlock()
			}
			close(release)
			stream := awaitConversationConnection(t, server.connections)
			stream.send(t,
				testEntryAdded("s1", user),
				testChunk("s1", api.Chunk{ID: "provider", Content: reply.Content}),
				testEntryAdded("s1", reply),
				testCompleted("s1", &reply),
			)
			result := awaitHooks(t, load, func(c *Conversation) bool { return c.Stream.Status == StreamCompleted })
			if result.Error != nil || len(result.History) != 3 || len(result.Stream.Messages) != 0 ||
				result.History[1].SourceID != user.ID || result.History[2].SourceID != reply.ID {
				t.Fatalf("history/event overlap = %+v", result)
			}
		})
	}
}

func TestUseConversationHydratesPendingToolsAndPermissions(t *testing.T) {
	server := newConversationTestServer()
	server.status = api.StatusWaitingPermission
	server.history = []api.Message{{ID: "call", Role: api.RoleAssistant, Content: []api.Block{conversationTool("a")}}}
	server.permissions = []api.PermissionRequest{{ID: "approval", ToolCallID: "a", Target: "a.go"}}
	c := hookClient(t, "/daemon.sock", "/workspace", server.roundTrip)
	_, load, _ := mountHooks(t, c, func() Conversation { return UseConversation("s1") })
	result := awaitHooks(t, load, func(c *Conversation) bool { return !c.IsLoading })
	if result.Error != nil || result.Stream.Status != StreamWaitingPermission || len(result.History) != 0 ||
		len(result.Stream.Messages) != 1 || len(result.Stream.Messages[0].Parts[0].Tool.Permissions) != 1 {
		t.Fatalf("pending conversation = %+v", result)
	}
	stream := awaitConversationConnection(t, server.connections)
	output := conversationText("response", api.RoleTool, "full output")
	output.ToolCallID = "a"
	stream.send(t, testEntryAdded("s1", output))
	result = awaitHooks(t, load, func(c *Conversation) bool { return len(c.History) == 1 })
	if len(result.Permissions) != 0 || result.History[0].Parts[0].Tool.Status != ToolSucceeded || result.Stream.Status != StreamRunning {
		t.Fatalf("resolved conversation = %+v", result)
	}
}

func TestUseConversationRefetchRetainsDataWhileLoadingAndOnFailure(t *testing.T) {
	server := newConversationTestServer()
	server.history = []api.Message{conversationText("user", api.RoleUser, "hello")}
	var block atomic.Bool
	started, release := make(chan struct{}), make(chan struct{})
	server.onHistory = func(r *http.Request) error {
		if block.Load() {
			close(started)
			select {
			case <-release:
			case <-r.Context().Done():
				return r.Context().Err()
			}
		}
		return nil
	}
	c := hookClient(t, "/daemon.sock", "/workspace", server.roundTrip)
	inst, load, _ := mountHooks(t, c, func() Conversation { return UseConversation("s1") })
	stream := awaitConversationConnection(t, server.connections)
	stream.send(t,
		testChunk("s1", api.Chunk{Content: []api.Block{
			{Type: api.BlockText, Text: "partial"}, conversationTool("a"),
		}}),
		testPermissions("s1", api.PermissionRequest{ID: "p", ToolCallID: "a"}),
	)
	before := awaitHooks(t, load, func(c *Conversation) bool { return len(c.Permissions) == 1 })
	server.mu.Lock()
	server.failPath = "/v1/sessions/s1/timeline"
	server.mu.Unlock()
	block.Store(true)
	inst.Engine().PostMacro(func() { load().Refetch() })
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("refetch never started")
	}
	loading := awaitHooks(t, load, func(c *Conversation) bool { return c.IsLoading })
	if len(loading.History) != 1 || len(loading.Stream.Messages) != 1 || len(loading.Permissions) != 1 {
		t.Fatalf("loading discarded conversation data: %+v", loading)
	}
	close(release)
	failed := awaitHooks(t, load, func(c *Conversation) bool { return c.Error != nil && !c.IsLoading })
	if len(failed.History) != 1 || len(failed.Stream.Messages) != 1 || len(failed.Permissions) != 1 ||
		failed.Stream.Messages[0].Parts[0].Text.Text != "partial" ||
		failed.Stream.Messages[0].Parts[1].Tool.Status != ToolInterrupted ||
		before.Stream.Messages[0].Parts[1].Tool.Status != ToolWaitingPermission {
		t.Fatalf("failed refetch discarded or mutated conversation data: %+v", failed)
	}
}

func TestUseConversationUnmountCancelsHydration(t *testing.T) {
	server := newConversationTestServer()
	started := make(chan struct{})
	canceled := make(chan struct{})
	server.onHistory = func(r *http.Request) error {
		close(started)
		<-r.Context().Done()
		close(canceled)
		return r.Context().Err()
	}
	c := hookClient(t, "/daemon.sock", "/workspace", server.roundTrip)
	_, _, stop := mountHooks(t, c, func() Conversation { return UseConversation("s1") })
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("history never started")
	}
	stop()
	select {
	case <-canceled:
	case <-time.After(3 * time.Second):
		t.Fatal("hydration was not canceled on unmount")
	}
}

func TestUseConversationFailuresAndRefetch(t *testing.T) {
	for _, path := range []string{"/v1/events", "/v1/sessions/s1/timeline", "/v1/sessions/s1/permissions", "/v1/sessions/s1"} {
		t.Run(path, func(t *testing.T) {
			server := newConversationTestServer()
			server.failPath = path
			c := hookClient(t, "/daemon.sock", "/workspace", server.roundTrip)
			inst, load, _ := mountHooks(t, c, func() Conversation { return UseConversation("s1") })
			result := awaitHooks(t, load, func(c *Conversation) bool { return c.Error != nil })
			if result.IsLoading || result.Stream.Status != StreamFailed {
				t.Fatalf("failure = %+v", result)
			}
			var apiErr *api.APIError
			if !errors.As(result.Error, &apiErr) || apiErr.StatusCode != http.StatusServiceUnavailable {
				t.Fatalf("error = %v", result.Error)
			}
			server.mu.Lock()
			server.failPath = ""
			server.history = []api.Message{conversationText("reloaded", api.RoleUser, "restored")}
			server.mu.Unlock()
			inst.Engine().PostMacro(func() { load().Refetch() })
			result = awaitHooks(t, load, func(c *Conversation) bool {
				return c.Error == nil && !c.IsLoading && len(c.History) == 1
			})
			if result.History[0].ID != "reloaded" {
				t.Fatalf("reloaded history = %+v", result.History)
			}
		})
	}
}

func TestUseConversationDisconnectPreservesPartialData(t *testing.T) {
	server := newConversationTestServer()
	c := hookClient(t, "/daemon.sock", "/workspace", server.roundTrip)
	_, load, _ := mountHooks(t, c, func() Conversation { return UseConversation("s1") })
	stream := awaitConversationConnection(t, server.connections)
	stream.send(t, testChunk("s1", api.Chunk{Content: []api.Block{{Type: api.BlockText, Text: "partial"}}}))
	awaitHooks(t, load, func(c *Conversation) bool { return len(c.Stream.Messages) == 1 })
	_ = stream.Close()
	result := awaitHooks(t, load, func(c *Conversation) bool { return c.Error != nil })
	if !errors.Is(result.Error, io.EOF) || result.Stream.Status != StreamFailed ||
		len(result.Stream.Messages) != 1 || result.Stream.Messages[0].Parts[0].Text.Text != "partial" ||
		result.Stream.Messages[0].Streaming {
		t.Fatalf("disconnected conversation = %+v", result)
	}
}

func TestUseConversationSessionSwitchDisablesAndCancels(t *testing.T) {
	server := newConversationTestServer()
	server.history = []api.Message{conversationText("old", api.RoleUser, "old session")}
	c := hookClient(t, "/daemon.sock", "/workspace", server.roundTrip)
	type snapshot struct {
		Conversation
		selectSession func(string)
	}
	inst, load, stop := mountHooks(t, c, func() snapshot {
		session, setSession := kitex.UseState("s1")
		return snapshot{Conversation: UseConversation(session()), selectSession: setSession}
	})
	awaitHooks(t, load, func(s *snapshot) bool { return len(s.History) == 1 })
	old := awaitConversationConnection(t, server.connections)
	inst.Engine().PostMacro(func() { load().selectSession("") })
	awaitHooks(t, load, func(s *snapshot) bool {
		return !s.IsLoading && s.Error == nil && len(s.History) == 0 && len(s.Stream.Messages) == 0
	})
	select {
	case <-old.closed:
	case <-time.After(3 * time.Second):
		t.Fatal("old subscription was not cancelled")
	}
	server.mu.Lock()
	server.history = []api.Message{conversationText("new", api.RoleUser, "new session")}
	server.mu.Unlock()
	inst.Engine().PostMacro(func() { load().selectSession("s2") })
	current := awaitConversationConnection(t, server.connections)
	awaitHooks(t, load, func(s *snapshot) bool { return len(s.History) == 1 && s.History[0].ID == "new" })
	old.send(t, testError("s1", "late old error"))
	current.send(t, testChunk("s1", api.Chunk{Content: []api.Block{{Type: api.BlockText, Text: "wrong session"}}}),
		testCompleted("s2", nil))
	result := awaitHooks(t, load, func(s *snapshot) bool { return s.Stream.Status == StreamCompleted })
	if result.Error != nil || len(result.Stream.Messages) != 0 || result.History[0].ID != "new" {
		t.Fatalf("old session leaked: %+v", result)
	}
	stop()
	select {
	case <-current.closed:
	case <-time.After(3 * time.Second):
		t.Fatal("unmount did not cancel subscription")
	}
}

func TestUseConversationDisabledDoesNotFetch(t *testing.T) {
	var calls atomic.Int32
	c := hookClient(t, "/daemon.sock", "/workspace", func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return response(r, http.StatusInternalServerError, nil), nil
	})
	_, load, _ := mountHooks(t, c, func() Conversation { return UseConversation("") })
	awaitHooks(t, load, func(c *Conversation) bool { return c.Refetch != nil })
	if calls.Load() != 0 || load().IsLoading || load().Stream.Status != StreamIdle {
		t.Fatalf("disabled hook fetched: calls=%d, state=%+v", calls.Load(), load())
	}
}

func TestUseConversationClientScopes(t *testing.T) {
	first, second := newConversationTestServer(), newConversationTestServer()
	first.history = []api.Message{conversationText("first", api.RoleUser, "first client")}
	second.history = []api.Message{conversationText("second", api.RoleUser, "second client")}
	clients := []*client.Client{
		hookClient(t, "/daemon.sock", "/workspace", first.roundTrip),
		hookClient(t, "/daemon.sock", "/workspace", second.roundTrip),
		hookClient(t, "/other.sock", "/other", first.roundTrip),
	}
	type snapshot struct {
		Conversation
		selectClient func(int)
	}
	var latest atomic.Pointer[snapshot]
	observer := trail.FC("ConversationClientScope", func(setClient func(int)) trail.Node {
		value := snapshot{Conversation: UseConversation("s1"), selectClient: setClient}
		latest.Store(&value)
		return kitex.Text("conversation")
	})
	root := trail.FC("ConversationClientProvider", func(_ struct{}) trail.Node {
		index, setIndex := kitex.UseState(0)
		return uiclient.Provider(uiclient.Props{Client: clients[index()]}, observer(setIndex))
	})
	inst, _, _ := mountHooks(t, clients[0], func() struct{} { return struct{}{} }, root(struct{}{}))
	awaitHooks(t, latest.Load, func(s *snapshot) bool { return len(s.History) == 1 && s.History[0].ID == "first" })
	old := awaitConversationConnection(t, first.connections)
	inst.Engine().PostMacro(func() { latest.Load().selectClient(1) })
	awaitHooks(t, latest.Load, func(s *snapshot) bool { return len(s.History) == 1 && s.History[0].ID == "second" })
	select {
	case <-old.closed:
	case <-time.After(3 * time.Second):
		t.Fatal("client switch did not cancel old subscription")
	}
	inst.Engine().PostMacro(func() { latest.Load().selectClient(2) })
	awaitHooks(t, latest.Load, func(s *snapshot) bool { return len(s.History) == 1 && s.History[0].ID == "first" })
}
