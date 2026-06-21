package graph

import (
	"context"
	"fmt"

	"github.com/masterkeysrd/loom/graph"
	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/loom/stream"
	"github.com/masterkeysrd/loom/tool"
	"github.com/masterkeysrd/tasksmith/internal/agent/tools"
	"github.com/masterkeysrd/tasksmith/internal/workspace"
)

// AgentState represents the state passed between nodes in the agent loop.
type AgentState struct {
	Messages message.MessageList `json:"messages"`
}

// Copy performs a deep copy of AgentState to satisfy the loom graph.State interface.
func (s AgentState) Copy() AgentState {
	copied := AgentState{}
	if s.Messages != nil {
		copied.Messages = make(message.MessageList, len(s.Messages))
		copy(copied.Messages, s.Messages)
	}
	return copied
}

// LLM defines the interface for model invocations.
type LLM interface {
	Invoke(ctx context.Context, messages []message.Message) (*message.Assistant, error)
}

// LLMModel defines the interface for a model that can bind tools.
type LLMModel interface {
	LLM
	BindTools(tools ...*tool.Tool) LLMModel
}

type loomModel struct {
	model *llm.Model
}

func (m loomModel) Invoke(ctx context.Context, messages []message.Message) (*message.Assistant, error) {
	return m.model.Invoke(ctx, messages)
}

func (m loomModel) BindTools(tools ...*tool.Tool) LLMModel {
	return loomModel{model: m.model.BindTools(tools...)}
}

// NewLoomModel wraps a *llm.Model into an LLMModel interface.
func NewLoomModel(m *llm.Model) LLMModel {
	return loomModel{model: m}
}

// AgentGraph orchestrates the flow of model invocation.
type AgentGraph struct {
	model     LLM
	container *tool.Container
}

// New creates a new AgentGraph orchestrator by loading/binding tools outside of the execution nodes.
func New(ctx context.Context, model LLMModel, ws *workspace.Workspace, storage tools.FileStorage) (*AgentGraph, error) {
	var allowedTools map[string]bool
	if ws != nil {
		cfg, err := ws.GetWorkspaceConfig(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get workspace config: %w", err)
		}
		allowedTools = cfg.AuthorizedTools
	}

	handlers := tools.NewHandlers(storage)
	allLoomTools, err := tools.LoomTools(handlers)
	if err != nil {
		return nil, fmt.Errorf("failed to load loom tools: %w", err)
	}

	var activeTools []*tool.Tool
	for _, lt := range allLoomTools {
		if allowedTools == nil || allowedTools[lt.Definition.Name] {
			activeTools = append(activeTools, lt)
		}
	}

	var boundModel LLM = model
	if len(activeTools) > 0 && model != nil {
		boundModel = model.BindTools(activeTools...)
	}

	var container *tool.Container
	if len(activeTools) > 0 {
		container = tool.NewContainer(activeTools...)
	}

	return &AgentGraph{
		model:     boundModel,
		container: container,
	}, nil
}

// Build compiles the graph using the loom builder.
func (a *AgentGraph) Build(cp graph.Checkpointer) (*graph.Graph[AgentState], error) {
	builder := graph.New[AgentState]().
		WithName("agent_loop").
		AddNode("think", graph.NodeFunc(a.think)).
		AddNode("execute_tools", graph.NodeFunc(a.executeTools)).
		AddEdge(graph.START, "think")

	builder.AddRouteEdge("think", func(s AgentState) (string, error) {
		if len(s.Messages) == 0 {
			return graph.END, nil
		}
		lastMsg := s.Messages[len(s.Messages)-1]
		for _, block := range lastMsg.GetContent() {
			if _, ok := block.(*message.ToolCall); ok {
				return "execute_tools", nil
			}
		}
		return graph.END, nil
	}, map[string]string{
		"execute_tools": "execute_tools",
		graph.END:       graph.END,
	})

	builder.AddEdge("execute_tools", "think")

	if cp != nil {
		builder.WithCheckpointer(cp)
	}

	return builder.Build()
}

// think node queries the LLM and appends the result to the history.
func (a *AgentGraph) think(ctx context.Context, s AgentState) (graph.Command[AgentState], error) {
	if a.model == nil {
		return nil, fmt.Errorf("no model configured in graph")
	}

	rehydratedMessages := tools.RehydrateMessagesForLLM(s.Messages)
	msg, err := a.model.Invoke(ctx, rehydratedMessages)
	if err != nil {
		return nil, fmt.Errorf("llm call failed: %w", err)
	}

	sw, hasWriter := stream.WriterFromContext(ctx)
	if hasWriter {
		_ = sw.Write(ctx, stream.Event{
			Name: "agent_message",
			Data: msg,
		})
	}

	return graph.Update[AgentState](func(state AgentState) AgentState {
		state.Messages = append(state.Messages, msg)
		return state
	}), nil
}

// executeTools executes any tool calls from the last assistant message.
func (a *AgentGraph) executeTools(ctx context.Context, s AgentState) (graph.Command[AgentState], error) {
	if a.container == nil {
		return nil, fmt.Errorf("no tools container configured in graph")
	}

	lastMsg := s.Messages[len(s.Messages)-1]
	var toolResults []message.Message

	sw, hasWriter := stream.WriterFromContext(ctx)

	for _, block := range lastMsg.GetContent() {
		if tc, ok := block.(*message.ToolCall); ok {
			var toolMsg *message.Tool
			toolCallCtx := context.WithValue(ctx, "tool_call_id", tc.ID)
			toolResp, err := a.container.Call(toolCallCtx, tc)
			if err != nil {
				toolMsg = &message.Tool{
					ToolCallID: tc.ID,
					Name:       tc.Name,
					IsError:    true,
					Content:    message.Content{&message.TextBlock{Text: err.Error()}},
				}
			} else {
				toolMsg = toolResp
			}
			toolResults = append(toolResults, toolMsg)

			if hasWriter {
				_ = sw.Write(ctx, stream.Event{
					Name: "tool_message",
					Data: toolMsg,
				})
			}
		}
	}

	return graph.Update[AgentState](func(state AgentState) AgentState {
		state.Messages = append(state.Messages, toolResults...)
		return state
	}), nil
}
