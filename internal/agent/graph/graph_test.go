package graph_test

import (
	"context"
	"errors"
	"testing"

	"github.com/masterkeysrd/loom/graph"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/loom/tool"
	agentgraph "github.com/masterkeysrd/tasksmith/internal/agent/graph"
)

type mockLLMModel struct {
	invokeFn func(ctx context.Context, messages []message.Message) (*message.Assistant, error)
}

func (m *mockLLMModel) Invoke(ctx context.Context, messages []message.Message) (*message.Assistant, error) {
	return m.invokeFn(ctx, messages)
}

func (m *mockLLMModel) BindTools(tools ...*tool.Tool) agentgraph.LLMModel {
	return m
}

func TestAgentGraph_Execution(t *testing.T) {
	// Tracks LLM calls
	callCount := 0

	// Mock LLM Model
	mockModel := &mockLLMModel{
		invokeFn: func(ctx context.Context, messages []message.Message) (*message.Assistant, error) {
			callCount++
			if callCount == 1 {
				return &message.Assistant{
					Content: message.Content{
						&message.TextBlock{Text: "TaskSmith is defined in GEMINI.md and README.md"},
					},
				}, nil
			}
			return nil, errors.New("unexpected LLM invocation")
		},
	}

	// Construct agent graph
	ag, err := agentgraph.New(context.Background(), mockModel, nil)
	if err != nil {
		t.Fatalf("failed to construct agent graph: %v", err)
	}
	g, err := ag.Build(nil)
	if err != nil {
		t.Fatalf("failed to build graph: %v", err)
	}

	// Create initial state
	initialState := agentgraph.AgentState{
		Messages: message.MessageList{
			message.NewUserText("Where is TaskSmith defined?"),
		},
	}

	// We wrap the initial state in a graph.Update command to set the initial state
	initCmd := graph.Update[agentgraph.AgentState](func(s agentgraph.AgentState) agentgraph.AgentState {
		return initialState
	})

	// Execute graph
	snapshot, err := g.Execute(context.Background(), initCmd, nil)
	if err != nil {
		t.Fatalf("graph execution failed: %v", err)
	}

	// Assertions
	if !snapshot.IsDone() {
		t.Error("expected snapshot to be done")
	}
	lastMsg := snapshot.State.Messages[len(snapshot.State.Messages)-1]
	if lastMsg.GetContent().Text() != "TaskSmith is defined in GEMINI.md and README.md" {
		t.Errorf("unexpected output: %q", lastMsg.GetContent().Text())
	}
	if len(snapshot.State.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(snapshot.State.Messages))
	}
	// user, assistant (final answer)
	if snapshot.State.Messages[0].Role() != message.RoleUser {
		t.Errorf("msg 0 role: expected user, got %s", snapshot.State.Messages[0].Role())
	}
	if snapshot.State.Messages[1].Role() != message.RoleAssistant {
		t.Errorf("msg 1 role: expected assistant, got %s", snapshot.State.Messages[1].Role())
	}
}
