package harness

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"text/template"

	"github.com/masterkeysrd/loom/graph"
	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
)

func TestCompactionNodeFailureCases(t *testing.T) {
	_, reg, _ := newTestToolsRegistry(t)
	state := State{PendingCompaction: &CompactInput{Focus: "focus"}, Messages: message.MessageList{message.NewUserText("request")}}
	provider := &fakeProvider{handler: func(context.Context, *llm.Request) ([]string, string, error) {
		return []string{"  \n "}, "response", nil
	}}
	node := NewCompactionNode(CompactionNodeConfig{Model: newTestModel(t, provider), Container: reg, Logger: log.Default()})

	cmd, err := node.Execute(context.Background(), state)
	if cmd != nil || err == nil || !strings.Contains(err.Error(), "compaction returned empty summary") {
		t.Fatalf("empty summary: command=%v error=%v", cmd, err)
	}
	if state.PendingCompaction == nil || len(state.Messages) != 1 {
		t.Fatalf("state changed after empty summary: %+v", state)
	}

	original := summarizeTemplate
	summarizeTemplate = template.Must(template.New("failure").Parse("{{.Missing}}"))
	t.Cleanup(func() { summarizeTemplate = original })
	cmd, err = node.Execute(context.Background(), state)
	if cmd != nil || err == nil || !strings.Contains(err.Error(), "execute compaction prompt template") {
		t.Fatalf("template failure: command=%v error=%v", cmd, err)
	}
	if len(provider.recordedTurns()) != 1 || state.PendingCompaction == nil || len(state.Messages) != 1 {
		t.Fatalf("template failure invoked model or changed state: %+v", state)
	}
}

func TestCompactionFailurePreservesCheckpointAndResumes(t *testing.T) {
	for _, auto := range []bool{false, true} {
		name := "manual"
		if auto {
			name = "auto"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			_, reg, perm := newTestToolsRegistry(t)
			failure := errors.New("summary provider unavailable")
			provider := &fakeProvider{stepHandler: func(_ context.Context, _ *llm.Request, turn int) ([]message.AssistantChunk, error) {
				switch turn {
				case 1:
					return []message.AssistantChunk{{Content: []message.Block{&message.TextBlock{Text: strings.Repeat("a", 4000)}}, Metrics: &message.TokenMetrics{TotalTokens: 1000}, Done: true}}, nil
				case 2:
					return nil, failure
				case 3:
					return []message.AssistantChunk{{Content: []message.Block{&message.TextBlock{Text: "recovered summary"}}, Done: true}}, nil
				default:
					return []message.AssistantChunk{{Content: []message.Block{&message.TextBlock{Text: "answer"}}, Done: true}}, nil
				}
			}}
			h, err := New(Config{ID: "compact-failure-" + name, DB: openTestSQLite(t), Model: newTestModel(t, provider), Tools: reg, Permissions: perm, Queue: &fakeQueue{}, Bash: &fakeBashRunner{}, Spiller: newTestSpiller(t), Logger: log.Default(), Limits: workspace.ModelLimits{Context: 10000, Output: 1000}})
			if err != nil {
				t.Fatal(err)
			}
			for _, err := range h.Stream(ctx, Input{Message: message.NewUserText("first request")}) {
				if err != nil {
					t.Fatal(err)
				}
			}
			input := Input{Compact: &CompactInput{Focus: "focus"}}
			if auto {
				input = Input{Message: message.NewUserText("new request")}
			}
			var streamErr error
			var events []Event
			for ev, err := range h.Stream(ctx, input) {
				if err != nil {
					streamErr = err
				} else {
					events = append(events, ev)
				}
			}
			if !errors.Is(streamErr, failure) {
				t.Fatalf("expected provider error, got %v", streamErr)
			}
			for _, ev := range events {
				if ev.Type == EventCompleted || ev.Type == EventCompactionCompleted {
					t.Fatalf("unexpected completion after failure: %+v", ev)
				}
			}
			if len(provider.recordedTurns()) != 2 {
				t.Fatalf("agent ran after failure: calls=%d", len(provider.recordedTurns()))
			}
			impl := h.(*harness)
			snap, err := impl.graph.Load(ctx, graph.Location{ThreadID: impl.id})
			if err != nil || snap == nil {
				t.Fatalf("load checkpoint: snapshot=%v error=%v", snap, err)
			}
			if snap.IsDone() || !reflect.DeepEqual(snap.Next, []string{"compact"}) {
				t.Fatalf("expected pending compact checkpoint: %+v", snap)
			}
			if snap.State.Context.Compactions != 0 || len(snap.State.Messages) < 2 || msgText(snap.State.Messages[0]) != "first request" {
				t.Fatalf("history changed after failure: %+v", snap.State)
			}
			if auto && msgText(snap.State.Messages[len(snap.State.Messages)-1]) != "new request" {
				t.Fatalf("new request lost: %+v", snap.State.Messages)
			}
			if !auto && (snap.State.PendingCompaction == nil || snap.State.PendingCompaction.Focus != "focus") {
				t.Fatalf("manual request lost: %+v", snap.State.PendingCompaction)
			}
			resumed, err := impl.graph.Execute(ctx, nil, &graph.Location{ThreadID: impl.id})
			if err != nil || !resumed.IsDone() {
				t.Fatalf("resume failed: snapshot=%v error=%v", resumed, err)
			}
			if resumed.State.Context.Compactions != 1 || SystemKindOf(resumed.State.Messages[0]) != SystemKindCompaction {
				t.Fatalf("resume did not compact: %+v", resumed.State)
			}
			wantCalls := 3
			if auto {
				wantCalls = 4
			}
			if len(provider.recordedTurns()) != wantCalls {
				t.Fatalf("unexpected resumed model calls: got %d want %d", len(provider.recordedTurns()), wantCalls)
			}
		})
	}
}
