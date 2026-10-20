package harness

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/masterkeysrd/loom/graph"
	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
)

func buildGraph(cp graph.Checkpointer, model *llm.Model, systemPrompt string, logger log.Interface) (*graph.Graph[State], error) {
	agentNode := NewAgentNode(model, systemPrompt, logger)

	g, err := graph.New[State]().
		WithName("harness").
		WithCheckpointer(cp).
		AddNode("agent", agentNode).
		AddEdge(graph.START, "agent").
		AddEdge("agent", graph.END).
		Build()
	if err != nil {
		return nil, fmt.Errorf("harness: build graph: %w", err)
	}

	return g, nil
}

// AgentNode is the primary reasoning and completion node in the harness workflow graph.
// It implements [github.com/masterkeysrd/loom/graph.Node].
type AgentNode struct {
	model        *llm.Model
	systemPrompt string
	logger       log.Interface
}

// NewAgentNode constructs a new [AgentNode] bound to the resolved LLM model,
// system prompt, and structured logger.
func NewAgentNode(model *llm.Model, systemPrompt string, logger log.Interface) *AgentNode {
	return &AgentNode{
		model:        model,
		systemPrompt: strings.TrimSpace(systemPrompt),
		logger:       logger.ForComponent("harness.agent"),
	}
}

// Execute invokes the LLM with the transient system prompt prepended to the
// conversation state, and returns a state update command appending the assistant response.
func (n *AgentNode) Execute(ctx context.Context, state State) (graph.Command[State], error) {
	llmMessages := make([]message.Message, 0, len(state.Messages)+1)
	if n.systemPrompt != "" {
		llmMessages = append(llmMessages, message.NewSystemText(n.systemPrompt))
	}
	llmMessages = append(llmMessages, state.Messages...)

	resp, err := n.model.Invoke(ctx, llmMessages)
	if err != nil {
		return nil, fmt.Errorf("harness: invoke llm: %w", err)
	}

	if resp.ID == "" {
		resp.ID = uuid.NewString()
	}

	return graph.Update[State](func(s State) State {
		s.Messages = append(s.Messages, resp)
		return s
	}), nil
}
