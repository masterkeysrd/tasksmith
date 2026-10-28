package harness

import (
	"context"
	"strings"
	"testing"

	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
)

func TestAutoCompactionTurnAndCommandBoundaries(t *testing.T) {
	ctx := context.Background()
	_, reg, perm := newTestToolsRegistry(t)
	q := &fakeQueue{}
	prov := &fakeProvider{
		stepHandler: func(_ context.Context, _ *llm.Request, turn int) ([]message.AssistantChunk, error) {
			text := "answer"
			var metrics *message.TokenMetrics
			if turn == 1 {
				text = strings.Repeat("a", 4000)
				metrics = &message.TokenMetrics{TotalTokens: 1000}
			} else if turn == 2 {
				text = "previous conversation summary"
			}
			return []message.AssistantChunk{{Content: []message.Block{&message.TextBlock{Text: text}}, Metrics: metrics, Done: true}}, nil
		},
	}
	h, err := New(Config{
		ID: "compact-boundaries", DB: openTestSQLite(t), Model: newTestModel(t, prov),
		Tools: reg, Permissions: perm, Queue: q, Bash: &fakeBashRunner{},
		Spiller: newTestSpiller(t), Logger: log.Default(),
		Limits: workspace.ModelLimits{Context: 10000, Output: 1000},
	})
	if err != nil {
		t.Fatal(err)
	}

	run := func(input Input) []Event {
		t.Helper()
		var events []Event
		for event, err := range h.Stream(ctx, input) {
			if err != nil {
				t.Fatal(err)
			}
			events = append(events, event)
		}
		return events
	}
	assertNoCompaction := func(events []Event) {
		t.Helper()
		for _, event := range events {
			if event.Type == EventCompactionStarted || event.Type == EventCompactionCompleted {
				t.Fatalf("unexpected compaction event: %+v", event)
			}
		}
	}

	assertNoCompaction(run(Input{Message: message.NewUserText("first request")}))
	state, err := h.LoadState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if state.Context.UsedTokens != state.Context.TriggerTokens || len(prov.recordedTurns()) != 1 {
		t.Fatalf("expected one completed turn at the threshold: context=%+v calls=%d", state.Context, len(prov.recordedTurns()))
	}

	q.items = append(q.items, QueueItem{Kind: QueueKindCommand, Payload: QueueCommandPayload{Command: "show status"}})
	assertNoCompaction(run(Input{}))
	state, err = h.LoadState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if state.Context.UsedTokens < state.Context.TriggerTokens || len(prov.recordedTurns()) != 1 {
		t.Fatalf("command unexpectedly invoked the model: context=%+v calls=%d", state.Context, len(prov.recordedTurns()))
	}

	const prompt = "new request with exact stack trace\nline 42"
	events := run(Input{Message: message.NewUserText(prompt)})
	starts, completions := 0, 0
	for _, event := range events {
		switch event.Type {
		case EventCompactionStarted:
			starts++
		case EventCompactionCompleted:
			completions++
		}
	}
	turns := prov.recordedTurns()
	if starts != 1 || completions != 1 || len(turns) != 3 {
		t.Fatalf("expected one compaction and one answer: starts=%d completions=%d calls=%d", starts, completions, len(turns))
	}
	if len(turns[1].messages) != 3 || msgText(turns[1].messages[0]) != "first request" || msgText(turns[1].messages[1]) != strings.Repeat("a", 4000) {
		t.Fatalf("summarization did not preserve the prior prefix: %+v", turns[1].messages)
	}
	messages := turns[2].messages
	if len(messages) != 3 || SystemKindOf(messages[0]) != SystemKindCompaction || SystemKindOf(messages[1]) != SystemKindCommand || msgText(messages[2]) != prompt {
		t.Fatalf("agent did not receive summary, command output, and exact prompt: %+v", messages)
	}
	if strings.Contains(msgText(messages[0]), "Continue working on the latest user request above") {
		t.Fatal("entry summary contains a continuation directive")
	}
}
