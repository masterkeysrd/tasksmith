package store

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
)

func TestPermissionResyncReconcilesSlots(t *testing.T) {
	ClearSlot()
	defer ClearSlot()
	acc := newConversationAccumulator()
	acc.setSessionID("session")
	old := api.PermissionRequest{ID: "old", ToolCallID: "old-call"}
	current := api.PermissionRequest{ID: "new", ToolCallID: "new-call"}
	q := api.QuestionRequest{ID: "question", ToolCallID: "question-call", Questions: []api.Question{{Type: "text", Question: "Why?"}}}
	acc.SeedPermissions([]api.PermissionRequest{old})
	acc.SeedQuestions([]api.QuestionRequest{q})
	acc.reconcilePermissions([]api.PermissionRequest{current})
	if len(acc.permissions) != 1 || acc.permissions[0].ID != current.ID || !GetStream().WaitingPermission {
		t.Fatalf("permission resync failed: %+v", acc.permissions)
	}
	if GetSlot().Queue == nil || len(*GetSlot().Queue) != 2 {
		t.Fatalf("unexpected queue: %+v", GetSlot())
	}
	for _, item := range *GetSlot().Queue {
		if item.ID == permSlotID(old.ID) {
			t.Fatal("stale permission still queued")
		}
	}
	acc.reconcilePermissions(nil)
	if len(acc.permissions) != 0 || GetStream().WaitingPermission || !GetStream().WaitingQuestion {
		t.Fatalf("question waiting state lost: %+v", GetStream())
	}
}

func TestQuestionEventsAndSessionReconciliation(t *testing.T) {
	ClearSlot()
	ClearTranscript()
	defer ClearSlot()
	defer ClearTranscript()
	acc := newConversationAccumulator()
	acc.setSessionID("session")
	q := api.QuestionRequest{ID: "q1", ToolCallID: "call1", Questions: []api.Question{{Type: "confirm", Question: "Proceed?"}}}
	p := api.PermissionRequest{ID: "p1", ToolCallID: "call2"}
	acc.SeedPermissions([]api.PermissionRequest{p})
	acc.applyEvent(api.Event{Type: api.EventQuestionRequested, Payload: api.QuestionRequestedEvent{SessionID: "other", Questions: []api.QuestionRequest{q}}})
	if len(acc.questions) != 0 {
		t.Fatal("accepted another session's question")
	}
	acc.applyEvent(api.Event{Type: api.EventQuestionRequested, Payload: api.QuestionRequestedEvent{SessionID: "session", Questions: []api.QuestionRequest{q}}})
	acc.applyEvent(api.Event{Type: api.EventQuestionRequested, Payload: api.QuestionRequestedEvent{SessionID: "session", Questions: []api.QuestionRequest{q}}})
	if len(acc.questions) != 1 || GetSlot().Queue == nil || len(*GetSlot().Queue) != 2 {
		t.Fatalf("unexpected queue: %+v", GetSlot())
	}
	acc.removePermission(p.ID)
	RemoveSlot(permSlotID(p.ID))
	if GetSlot().Active == nil || GetSlot().Active.ID != questionSlotID(q.ID) || !GetStream().WaitingQuestion {
		t.Fatalf("question not active: %+v", GetSlot())
	}
	acc.applyEvent(api.Event{Type: api.EventEntryAdded, Payload: api.EntryAddedEvent{SessionID: "session", Entry: api.TimelineEntry{ID: "answer", Kind: api.TimelineKindQuestion, Question: &api.QuestionRecord{Request: q}}}})
	if GetSlot().Active != nil || len(acc.questions) != 0 {
		t.Fatalf("resolved question remains: %+v", GetSlot())
	}
	acc.applyEvent(api.Event{Type: api.EventQuestionRequested, Payload: api.QuestionRequestedEvent{SessionID: "session", Questions: []api.QuestionRequest{q}}})
	if GetSlot().Active != nil {
		t.Fatal("replayed event restored a resolved question")
	}
	acc.SeedQuestions([]api.QuestionRequest{q})
	acc.SeedQuestions(nil)
	if GetSlot().Active != nil || len(acc.questions) != 0 {
		t.Fatal("reconciliation did not remove stale question")
	}
}

func TestQuestionRecordsStayWithToolOnReload(t *testing.T) {
	ClearSlot()
	ClearTranscript()
	defer ClearSlot()
	defer ClearTranscript()
	q1 := api.QuestionRequest{ID: "q1", ToolCallID: "call1", Questions: []api.Question{{Type: "text", Question: "Why?"}}}
	q2 := api.QuestionRequest{ID: "q2", ToolCallID: "call2", Questions: []api.Question{{Type: "text", Question: "Where?"}}}
	record := api.QuestionRecord{Request: q1, Response: api.QuestionResponse{Answers: []api.QuestionAnswer{{Question: "Why?", Text: "Because"}}}}
	message := &api.Message{ID: "assistant", Role: api.RoleAssistant, Content: []api.Block{
		{Type: api.BlockToolCall, ToolCall: &api.ToolCall{ID: "call1", Name: "ask_question"}},
		{Type: api.BlockToolCall, ToolCall: &api.ToolCall{ID: "call2", Name: "ask_question"}},
	}}
	entries := []api.TimelineEntry{
		{ID: "assistant-entry", Kind: api.TimelineKindMessage, Message: message},
		{ID: "question-entry", Kind: api.TimelineKindQuestion, Question: &record},
	}
	acc := newConversationAccumulator()
	acc.setSessionID("session")
	acc.loadTimeline(entries, []api.QuestionRequest{q2})
	if GetStream().Messages == nil || len(*GetStream().Messages) != 1 {
		t.Fatalf("pending tools not in stream: %+v", GetStream())
	}
	parts := (*GetStream().Messages)[0].Parts
	if len(parts) != 2 || parts[0].Tool.Question == nil || parts[0].Tool.Question.Response.Answers[0].Text != "Because" || parts[1].Tool.Question != nil {
		t.Fatalf("question records not paired: %+v", parts)
	}
	if GetTranscript().Items != nil {
		t.Fatalf("question record duplicated in transcript: %+v", *GetTranscript().Items)
	}
	acc.applyEvent(api.Event{Type: api.EventEntryAdded, Payload: api.EntryAddedEvent{SessionID: "session", Entry: api.TimelineEntry{ID: "q2-answer", Kind: api.TimelineKindQuestion, Question: &api.QuestionRecord{Request: q2, Response: api.QuestionResponse{Answers: []api.QuestionAnswer{{Question: "Where?", Text: "Here"}}}}}}})
	if GetStream().Messages == nil || (*GetStream().Messages)[0].Parts[1].Tool.Question == nil {
		t.Fatal("live answer was not paired with its tool")
	}
}

func TestSubmitQuestionSendsStructuredResponse(t *testing.T) {
	ResetSession()
	ResetStream()
	ClearSlot()
	defer ResetSession()
	defer ResetStream()
	defer ClearSlot()
	payload := make(chan api.SendRequest, 1)
	rt := &mockSessionRoundTripper{handler: func(req *http.Request) (*http.Response, error) {
		var body api.SendRequest
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			return nil, err
		}
		payload <- body
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	}}
	c, err := client.New("/tmp/dummy.sock", client.WithCWD("/workspace"), client.WithHTTPClient(&http.Client{Transport: rt}))
	if err != nil {
		t.Fatal(err)
	}
	sessMu.Lock()
	activeClient, activeCtx = c, context.Background()
	sessMu.Unlock()
	SetSession(&api.Session{ID: "session"})
	globalConversation.setSessionID("session")
	q := api.QuestionRequest{ID: "req", ToolCallID: "call", Questions: []api.Question{{Type: "text", Question: "Why?"}}}
	globalConversation.SeedQuestions([]api.QuestionRequest{q})
	done := make(chan error, 1)
	SubmitQuestion(q, api.QuestionResponse{Answers: []api.QuestionAnswer{{Question: "Why?", Text: "Reason"}}}, func(err error) { done <- err })
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("submission timed out")
	}
	got := <-payload
	if got.Question == nil || got.Question.RequestID != "req" || got.Question.ToolCallID != "call" || got.Question.Answers[0].Text != "Reason" {
		t.Fatalf("unexpected payload: %+v", got.Question)
	}
	if GetSlot().Active != nil {
		t.Fatal("successful submission did not remove slot")
	}
}
