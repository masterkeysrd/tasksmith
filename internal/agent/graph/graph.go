package graph

import (
	"context"
	"fmt"

	"github.com/masterkeysrd/loom/graph"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/warp"
)

// AgentState represents the state passed between nodes in the agent loop.
type AgentState struct {
	Messages message.MessageList `json:"messages"`
	Tools    []*warp.Tool        `json:"tools"`
	Error    string              `json:"error,omitempty"`
}

// Copy performs a deep copy of AgentState to satisfy the loom graph.State interface.
func (s AgentState) Copy() AgentState {
	copied := AgentState{
		Error: s.Error,
	}
	if s.Messages != nil {
		copied.Messages = make(message.MessageList, len(s.Messages))
		copy(copied.Messages, s.Messages)
	}
	if s.Tools != nil {
		copied.Tools = make([]*warp.Tool, len(s.Tools))
		for i, t := range s.Tools {
			copied.Tools[i] = t.DeepCopy()
		}
	}
	return copied
}

// LLMCaller is a function type that makes a structured call to the language model.
type LLMCaller func(ctx context.Context, messages []message.Message) (*message.Assistant, error)

// AgentGraph orchestrates the flow of model invocation.
type AgentGraph struct {
	llm LLMCaller
}

// New creates a new AgentGraph orchestrator.
func New(llm LLMCaller) *AgentGraph {
	return &AgentGraph{
		llm: llm,
	}
}

// Build compiles the graph using the loom builder.
func (a *AgentGraph) Build(cp graph.Checkpointer) (*graph.Graph[AgentState], error) {
	builder := graph.New[AgentState]().
		WithName("agent_loop").
		AddNode("think", graph.NodeFunc(a.think)).
		AddEdge(graph.START, "think").
		AddEdge("think", graph.END)

	if cp != nil {
		builder.WithCheckpointer(cp)
	}

	return builder.Build()
}

// think node queries the LLM and appends the result to the history.
func (a *AgentGraph) think(ctx context.Context, s AgentState) (graph.Command[AgentState], error) {
	msg, err := a.llm(ctx, s.Messages)
	if err != nil {
		return nil, fmt.Errorf("llm call failed: %w", err)
	}

	return graph.Update[AgentState](func(state AgentState) AgentState {
		state.Messages = append(state.Messages, msg)
		return state
	}), nil
}
