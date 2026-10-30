package harness

import (
	"context"
	"testing"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
)

func TestPrepareResponsesNormalizesAndClones(t *testing.T) {
	state := State{
		PendingPermissions: []api.PermissionRequest{{ID: "permission", ToolCallID: "write"}},
		PendingQuestions:   []api.QuestionRequest{{ID: "question", ToolCallID: "ask"}},
	}
	selected := []string{"original"}
	input := Input{
		Decisions: map[string]api.PermissionDecision{"write": {Kind: api.DecisionReject}},
		Answers: map[string]api.QuestionResponse{"question": {
			Answers: []api.QuestionAnswer{{Question: "Choose", Selected: selected}},
		}},
	}
	update, err := prepareResponses(input, state)
	if err != nil {
		t.Fatal(err)
	}
	selected[0] = "changed"
	result := update.Apply(State{})
	for _, key := range []string{"permission", "write"} {
		decision, ok := result.PermissionDecisions[key]
		if !ok || decision.RequestID != "permission" || decision.ToolCallID != "write" || decision.Kind != api.DecisionReject {
			t.Fatalf("decision %q: %+v", key, decision)
		}
	}
	for _, key := range []string{"question", "ask"} {
		answer, ok := result.QuestionResponses[key]
		if !ok || answer.RequestID != "question" || answer.ToolCallID != "ask" || len(answer.Answers) != 1 || answer.Answers[0].Selected[0] != "original" {
			t.Fatalf("answer %q: %+v", key, answer)
		}
	}
}

func TestStreamStoppedAtInputEventDoesNotStartGraph(t *testing.T) {
	ctx := context.Background()
	_, registry, perms := newTestToolsRegistry(t)
	provider := &fakeProvider{}
	h, err := New(Config{ID: "stopped-input", DB: openTestSQLite(t), Model: newTestModel(t, provider), Tools: registry, Bash: &fakeBashRunner{}, Spiller: newTestSpiller(t), Permissions: perms, Queue: &fakeQueue{}, Logger: log.Default()})
	if err != nil {
		t.Fatal(err)
	}
	input := message.NewUserText("hello")
	input.SetID("")
	for event, err := range h.Stream(ctx, Input{Message: input}) {
		if err != nil || event.Type != EventMessage || event.Message == nil || event.Message.GetID() == "" {
			t.Fatalf("first event: %+v, %v", event, err)
		}
		break
	}
	state, err := h.LoadState(ctx)
	if err != nil || len(state.Messages) != 0 || len(provider.recordedTurns()) != 0 || input.GetID() != "" {
		t.Fatalf("graph started or input mutated: state=%+v turns=%d id=%q err=%v", state, len(provider.recordedTurns()), input.GetID(), err)
	}
}
