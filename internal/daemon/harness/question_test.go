package harness

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/internal/core/log"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
	"github.com/masterkeysrd/warp"
)

func TestQuestionPermissionEvaluation(t *testing.T) {
	for _, tc := range []struct {
		name     string
		deny     bool
		policy   bool
		decision api.DecisionKind
	}{
		{name: "deny rule", deny: true},
		{name: "workspace policy", deny: true, policy: true},
		{name: "allow once", decision: api.DecisionAllowOnce},
		{name: "allow session", decision: api.DecisionAllowSession},
		{name: "reject", decision: api.DecisionReject},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			_, registry, perms := newTestToolsRegistry(t)
			if tc.policy {
				perms.SetPolicies(&warp.Policies{Tools: &warp.ToolPolicies{Exclude: []string{"ask_question"}}})
			} else if tc.deny {
				if err := perms.AddDeny("ask_question(*)"); err != nil {
					t.Fatal(err)
				}
			} else {
				perms.SetMode(api.ModeAsk)
			}
			provider := &fakeProvider{
				stepHandler: func(_ context.Context, _ *llm.Request, turn int) ([]message.AssistantChunk, error) {
					if turn == 1 {
						return []message.AssistantChunk{
							{
								Content: []message.Block{
									&message.ToolCall{
										ID:   "ask",
										Name: "ask_question",
										Args: map[string]any{
											"questions": []any{
												map[string]any{
													"type":        "confirm",
													"question":    "Proceed?",
													"description": "Confirm.",
												},
											},
										},
									},
								},
								Done: true,
							},
						}, nil
					}
					return []message.AssistantChunk{{Content: []message.Block{&message.TextBlock{Text: "done"}}, Done: true}}, nil
				}}
			h, err := New(Config{ID: "question-permission", DB: openTestSQLite(t), Model: newTestModel(t, provider), Tools: registry, Bash: &fakeBashRunner{}, Spiller: newTestSpiller(t), Permissions: perms, Queue: &fakeQueue{}, Logger: log.Default()})
			if err != nil {
				t.Fatal(err)
			}
			var permission api.PermissionRequest
			var result *message.Tool
			for event, err := range h.Stream(ctx, Input{Message: message.NewUserText("start")}) {
				if err != nil {
					t.Fatal(err)
				}
				if event.Type == EventInterrupted {
					if len(event.Questions) != 0 || len(event.Permissions) != 1 {
						t.Fatalf("expected permission only, got %+v", event)
					}
					permission = event.Permissions[0]
				}
				if msg, ok := event.Message.(*message.Tool); ok {
					result = msg
				}
			}
			if tc.deny {
				if permission.ID != "" || result == nil || !strings.Contains(result.Content.Text(), tools.ErrPermissionDenied.Error()) {
					t.Fatalf("expected denied result without prompt: permission=%+v result=%+v", permission, result)
				}
				return
			}
			if permission.ID == "" || permission.ToolCallID != "ask" {
				t.Fatalf("missing permission: %+v", permission)
			}
			var question api.QuestionRequest
			for event, err := range h.Stream(ctx, Input{Decisions: map[string]api.PermissionDecision{permission.ID: {Kind: tc.decision}}}) {
				if err != nil {
					t.Fatal(err)
				}
				if event.Type == EventInterrupted && len(event.Questions) == 1 && len(event.Permissions) == 0 {
					question = event.Questions[0]
				}
				if msg, ok := event.Message.(*message.Tool); ok {
					result = msg
				}
			}
			if tc.decision == api.DecisionReject {
				if question.ID != "" || result == nil || !strings.Contains(result.Content.Text(), tools.ErrPermissionDenied.Error()) {
					t.Fatalf("expected rejected result without question: question=%+v result=%+v", question, result)
				}
				return
			}
			if question.ID == "" || question.ToolCallID != "ask" || result != nil {
				t.Fatalf("expected question after approval: question=%+v result=%+v", question, result)
			}
			for event, err := range h.Stream(ctx, Input{Answers: map[string]api.QuestionResponse{question.ID: {Answers: []api.QuestionAnswer{{Question: "Proceed?", Confirmed: new(true)}}}}}) {
				if err != nil {
					t.Fatal(err)
				}
				if msg, ok := event.Message.(*message.Tool); ok {
					result = msg
				}
			}
			if result == nil || !strings.Contains(result.Content.Text(), `"confirmed":true`) {
				t.Fatalf("missing answer result: %+v", result)
			}
		})
	}
}

func TestApprovedQuestionWithOtherToolCalls(t *testing.T) {
	ctx := context.Background()
	_, registry, perms := newTestToolsRegistry(t)
	perms.SetMode(api.ModeAsk)
	provider := &fakeProvider{stepHandler: func(_ context.Context, _ *llm.Request, turn int) ([]message.AssistantChunk, error) {
		if turn == 1 {
			return []message.AssistantChunk{{Content: []message.Block{
				&message.ToolCall{ID: "ask", Name: "ask_question", Args: map[string]any{"questions": []any{map[string]any{"type": "confirm", "question": "Proceed?", "description": "Confirm."}}}},
				&message.ToolCall{ID: "write", Name: "write", Args: map[string]any{"path": "mixed.txt", "content": "data"}},
			}, Done: true}}, nil
		}
		return []message.AssistantChunk{{Content: []message.Block{&message.TextBlock{Text: "done"}}, Done: true}}, nil
	}}
	h, err := New(Config{ID: "question-permission-mixed", DB: openTestSQLite(t), Model: newTestModel(t, provider), Tools: registry, Bash: &fakeBashRunner{}, Spiller: newTestSpiller(t), Permissions: perms, Queue: &fakeQueue{}, Logger: log.Default()})
	if err != nil {
		t.Fatal(err)
	}
	decisions := map[string]api.PermissionDecision{}
	for event, err := range h.Stream(ctx, Input{Message: message.NewUserText("start")}) {
		if err != nil {
			t.Fatal(err)
		}
		if event.Type == EventInterrupted {
			if len(event.Permissions) != 2 || len(event.Questions) != 0 {
				t.Fatalf("expected two permission requests: %+v", event)
			}
			for _, req := range event.Permissions {
				kind := api.DecisionReject
				if req.ToolCallID == "ask" {
					kind = api.DecisionAllowOnce
				}
				decisions[req.ID] = api.PermissionDecision{Kind: kind}
			}
		}
	}
	if len(decisions) != 2 {
		t.Fatalf("missing permission requests: %+v", decisions)
	}
	var question api.QuestionRequest
	var writes int
	for event, err := range h.Stream(ctx, Input{Decisions: decisions}) {
		if err != nil {
			t.Fatal(err)
		}
		if event.Type == EventInterrupted && len(event.Questions) == 1 && len(event.Permissions) == 0 {
			question = event.Questions[0]
		}
		if msg, ok := event.Message.(*message.Tool); ok && msg.ToolCallID == "write" {
			writes++
			if !strings.Contains(msg.Content.Text(), tools.ErrPermissionDenied.Error()) {
				t.Fatalf("unexpected write result: %+v", msg)
			}
		}
	}
	if question.ID == "" || writes != 1 {
		t.Fatalf("expected question and one write result: question=%+v writes=%d", question, writes)
	}
	for event, err := range h.Stream(ctx, Input{Answers: map[string]api.QuestionResponse{question.ID: {Answers: []api.QuestionAnswer{{Question: "Proceed?", Confirmed: new(true)}}}}}) {
		if err != nil {
			t.Fatal(err)
		}
		if msg, ok := event.Message.(*message.Tool); ok && msg.ToolCallID == "write" {
			writes++
		}
	}
	if writes != 1 || len(provider.recordedTurns()) != 2 {
		t.Fatalf("unexpected repeated work: write results=%d model calls=%d", writes, len(provider.recordedTurns()))
	}
}

func TestQuestionInterruptResume(t *testing.T) {
	ctx := context.Background()
	_, registry, perms := newTestToolsRegistry(t)
	provider := &fakeProvider{stepHandler: func(_ context.Context, _ *llm.Request, turn int) ([]message.AssistantChunk, error) {
		if turn == 1 {
			return []message.AssistantChunk{{BaseChunk: message.BaseChunk{ID: "question-turn"}, Content: []message.Block{&message.ToolCall{ID: "ask-1", Name: "ask_question", Args: map[string]any{"questions": []any{map[string]any{"type": "confirm", "question": "Proceed?", "description": "Confirm the operation."}}}}}, Done: true}}, nil
		}
		return []message.AssistantChunk{{BaseChunk: message.BaseChunk{ID: "final"}, Content: []message.Block{&message.TextBlock{Text: "done"}}, Done: true}}, nil
	}}
	h, err := New(Config{ID: "question-test", DB: openTestSQLite(t), Model: newTestModel(t, provider), Tools: registry, Bash: &fakeBashRunner{}, Spiller: newTestSpiller(t), Permissions: perms, Queue: &fakeQueue{}, Logger: log.Default()})
	if err != nil {
		t.Fatal(err)
	}
	var req api.QuestionRequest
	for event, err := range h.Stream(ctx, Input{Message: message.NewUserText("start")}) {
		if err != nil {
			t.Fatal(err)
		}
		if event.Type == EventInterrupted && len(event.Questions) == 1 {
			req = event.Questions[0]
		}
	}
	if req.ID == "" || req.ToolCallID != "ask-1" {
		t.Fatalf("missing question: %+v", req)
	}
	state, err := h.LoadState(ctx)
	if err != nil || len(state.PendingQuestions) != 1 {
		t.Fatalf("checkpoint: %+v, %v", state, err)
	}
	for _, err := range h.Stream(ctx, Input{Answers: map[string]api.QuestionResponse{}}) {
		if !errors.Is(err, ErrPendingQuestionsUnresolved) {
			t.Fatalf("missing response: %v", err)
		}
	}
	var result *message.Tool
	for event, err := range h.Stream(ctx, Input{Answers: map[string]api.QuestionResponse{req.ID: {Answers: []api.QuestionAnswer{{Question: "Proceed?", Confirmed: new(true)}}}}}) {
		if err != nil {
			t.Fatal(err)
		}
		if msg, ok := event.Message.(*message.Tool); ok {
			result = msg
		}
	}
	if result == nil || !strings.Contains(result.Content.Text(), `"confirmed":true`) {
		t.Fatalf("missing answer tool result: %+v", result)
	}
}

func TestMixedQuestionAndPermission(t *testing.T) {
	ctx := context.Background()
	_, registry, perms := newTestToolsRegistry(t)
	provider := &fakeProvider{stepHandler: func(_ context.Context, _ *llm.Request, turn int) ([]message.AssistantChunk, error) {
		if turn == 1 {
			return []message.AssistantChunk{{BaseChunk: message.BaseChunk{ID: "mixed"}, Content: []message.Block{
				&message.ToolCall{ID: "ask", Name: "ask_question", Args: map[string]any{"questions": []any{map[string]any{"type": "confirm", "question": "Proceed?", "description": "Confirm."}}}},
				&message.ToolCall{ID: "write", Name: "write", Args: map[string]any{"path": "mixed.txt", "content": "data"}},
			}, Done: true}}, nil
		}
		return []message.AssistantChunk{{BaseChunk: message.BaseChunk{ID: "done"}, Content: []message.Block{&message.TextBlock{Text: "done"}}, Done: true}}, nil
	}}
	h, err := New(Config{ID: "mixed", DB: openTestSQLite(t), Model: newTestModel(t, provider), Tools: registry, Bash: &fakeBashRunner{}, Spiller: newTestSpiller(t), Permissions: perms, Queue: &fakeQueue{}, Logger: log.Default()})
	if err != nil {
		t.Fatal(err)
	}
	var q api.QuestionRequest
	var p api.PermissionRequest
	for event, err := range h.Stream(ctx, Input{Message: message.NewUserText("start")}) {
		if err != nil {
			t.Fatal(err)
		}
		if event.Type == EventInterrupted && len(event.Questions) == 1 && len(event.Permissions) == 1 {
			q, p = event.Questions[0], event.Permissions[0]
		}
	}
	if q.ID == "" || p.ID == "" {
		t.Fatalf("missing mixed requests: %+v %+v", q, p)
	}
	decisions := map[string]api.PermissionDecision{p.ID: {Kind: api.DecisionReject}}
	for _, err := range h.Stream(ctx, Input{Decisions: decisions}) {
		if !errors.Is(err, ErrPendingQuestionsUnresolved) {
			t.Fatalf("missing answer error: %v", err)
		}
	}
	var result *message.Tool
	for event, err := range h.Stream(ctx, Input{Decisions: decisions, Answers: map[string]api.QuestionResponse{q.ID: {Answers: []api.QuestionAnswer{{Question: "Proceed?", Confirmed: new(false)}}}}}) {
		if err != nil {
			t.Fatal(err)
		}
		if msg, ok := event.Message.(*message.Tool); ok && msg.ToolCallID == "ask" {
			result = msg
		}
	}
	if result == nil || !strings.Contains(result.Content.Text(), `"confirmed":false`) {
		t.Fatalf("missing no answer: %+v", result)
	}
}

func TestQuestionOutputOmitsUnrelatedConfirmation(t *testing.T) {
	ctx := context.Background()
	_, registry, perms := newTestToolsRegistry(t)
	provider := &fakeProvider{stepHandler: func(_ context.Context, _ *llm.Request, turn int) ([]message.AssistantChunk, error) {
		if turn == 1 {
			return []message.AssistantChunk{{BaseChunk: message.BaseChunk{ID: "ask"}, Content: []message.Block{&message.ToolCall{ID: "ask", Name: "ask_question", Args: map[string]any{"questions": []any{
				map[string]any{"type": "single", "question": "Choice?", "description": "Choose.", "options": []any{map[string]any{"id": "a", "label": "A"}, map[string]any{"id": "b", "label": "B"}}},
				map[string]any{"type": "multi", "question": "Choices?", "description": "Choose.", "options": []any{map[string]any{"id": "a", "label": "A"}, map[string]any{"id": "b", "label": "B"}}},
				map[string]any{"type": "text", "question": "Why?", "description": "Explain."},
				map[string]any{"type": "confirm", "question": "Yes?", "description": "Confirm."},
				map[string]any{"type": "confirm", "question": "No?", "description": "Confirm."},
			}}}}, Done: true}}, nil
		}
		return []message.AssistantChunk{{BaseChunk: message.BaseChunk{ID: "done"}, Content: []message.Block{&message.TextBlock{Text: "done"}}, Done: true}}, nil
	}}
	h, err := New(Config{ID: "question-output", DB: openTestSQLite(t), Model: newTestModel(t, provider), Tools: registry, Bash: &fakeBashRunner{}, Spiller: newTestSpiller(t), Permissions: perms, Queue: &fakeQueue{}, Logger: log.Default()})
	if err != nil {
		t.Fatal(err)
	}
	var req api.QuestionRequest
	for event, err := range h.Stream(ctx, Input{Message: message.NewUserText("start")}) {
		if err != nil {
			t.Fatal(err)
		}
		if event.Type == EventInterrupted && len(event.Questions) == 1 {
			req = event.Questions[0]
		}
	}
	if req.ID == "" {
		t.Fatal("missing question request")
	}
	answers := []api.QuestionAnswer{{Question: "No?", Confirmed: new(false)}, {Question: "Choices?", Selected: []string{"b"}}, {Question: "Yes?", Confirmed: new(true)}, {Question: "Choice?", Selected: []string{"a"}}, {Question: "Why?", Text: "Reason"}}
	var result *message.Tool
	for event, err := range h.Stream(ctx, Input{Answers: map[string]api.QuestionResponse{req.ID: {Answers: answers}}}) {
		if err != nil {
			t.Fatal(err)
		}
		if msg, ok := event.Message.(*message.Tool); ok {
			result = msg
		}
	}
	if result == nil {
		t.Fatal("missing tool response")
	}
	var decoded struct {
		Answers []map[string]any `json:"answers"`
	}
	if err := json.Unmarshal([]byte(result.Content.Text()), &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Answers) != 5 {
		t.Fatalf("unexpected output: %+v", decoded)
	}
	for i, question := range []string{"Choice?", "Choices?", "Why?", "Yes?", "No?"} {
		if decoded.Answers[i]["question"] != question {
			t.Fatalf("answer %d associated with wrong question: %+v", i, decoded.Answers)
		}
	}
	for i := range 3 {
		if _, ok := decoded.Answers[i]["confirmed"]; ok {
			t.Fatalf("unexpected confirmation: %+v", decoded.Answers[i])
		}
	}
	if decoded.Answers[3]["confirmed"] != true || decoded.Answers[4]["confirmed"] != false {
		t.Fatalf("confirmation lost: %+v", decoded.Answers)
	}
}

func TestQuestionResumeDuplicateText(t *testing.T) {
	call := &message.ToolCall{ID: "call", Name: "ask_question"}
	request := api.QuestionRequest{ID: "request", ToolCallID: call.ID, Questions: []api.Question{
		{Type: "text", Question: "Same?"},
		{Type: "confirm", Question: "Same?"},
		{Type: "text", Question: "Other?"},
	}}
	answers := api.QuestionResponse{Answers: []api.QuestionAnswer{
		{Question: "Same?", Text: "reason"},
		{Question: "Same?", Confirmed: new(false)},
		{Question: "Other?", Text: "detail"},
	}}
	node := &ToolsNode{}
	cmd, err := node.resumeCalls(context.Background(), []*message.ToolCall{call}, nil, nil, []api.QuestionRequest{request}, map[string]api.QuestionResponse{request.ID: answers})
	if err != nil {
		t.Fatal(err)
	}
	state := cmd.Apply(State{})
	if len(state.Messages) != 1 {
		t.Fatalf("messages = %+v", state.Messages)
	}
	result, ok := state.Messages[0].(*message.Tool)
	if !ok {
		t.Fatalf("unexpected message: %+v", state.Messages[0])
	}
	output, ok := result.StructuredContent.(api.AskQuestionOutput)
	if !ok || len(output.Answers) != 3 || output.Answers[0].Text != "reason" || output.Answers[0].Confirmed != nil || output.Answers[1].Confirmed == nil || *output.Answers[1].Confirmed || output.Answers[2].Text != "detail" {
		t.Fatalf("unexpected answer associations: %+v", result.StructuredContent)
	}
}

func TestQuestionStateCopy(t *testing.T) {
	state := State{
		PendingQuestions:  []api.QuestionRequest{{ID: "q", Questions: []api.Question{{Options: []api.QuestionOption{{ID: "a"}}}}}},
		QuestionResponses: map[string]api.QuestionResponse{"q": {Answers: []api.QuestionAnswer{{Selected: []string{"a"}}}}},
	}
	copy := state.Copy()
	copy.PendingQuestions[0].Questions[0].Options[0].ID = "changed"
	copy.QuestionResponses["q"].Answers[0].Selected[0] = "changed"
	if state.PendingQuestions[0].Questions[0].Options[0].ID != "a" || state.QuestionResponses["q"].Answers[0].Selected[0] != "a" {
		t.Fatal("question state copy aliases original")
	}
}
