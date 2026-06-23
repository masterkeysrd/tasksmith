package graph

import (
	"context"
	"fmt"
	"strings"

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
	Messages        message.MessageList `json:"messages"`
	Todos           []tools.Todo        `json:"todos"`
	ActivatedSkills []string            `json:"activated_skills"`
}

// Copy performs a deep copy of AgentState to satisfy the loom graph.State interface.
func (s AgentState) Copy() AgentState {
	copied := AgentState{}
	if s.Messages != nil {
		copied.Messages = make(message.MessageList, len(s.Messages))
		copy(copied.Messages, s.Messages)
	}
	if s.Todos != nil {
		copied.Todos = make([]tools.Todo, len(s.Todos))
		copy(copied.Todos, s.Todos)
	}
	if s.ActivatedSkills != nil {
		copied.ActivatedSkills = make([]string, len(s.ActivatedSkills))
		copy(copied.ActivatedSkills, s.ActivatedSkills)
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

// InboxProvider defines the interface to retrieve pending user messages.
type InboxProvider interface {
	PopMessages() []message.Message
}

// AgentGraph orchestrates the flow of model invocation.
type AgentGraph struct {
	model          LLM
	container      *tool.Container
	inbox          InboxProvider
	systemPrompt   string
	agentName      string
	onTodosUpdated func(ctx context.Context, todos []tools.Todo) error
}

// Options defines the configurations and dependencies to initialize the AgentGraph.
type Options struct {
	Model          LLMModel
	Workspace      *workspace.Workspace
	Storage        tools.FileStorage
	Inbox          InboxProvider
	TaskManager    *tools.TaskManager
	SessionID      string
	SystemPrompt   string
	AgentName      string
	OnTodosUpdated func(ctx context.Context, todos []tools.Todo) error
}

// New creates a new AgentGraph orchestrator by loading/binding tools outside of the execution nodes.
func New(ctx context.Context, opts Options) (*AgentGraph, error) {
	var allowedTools map[string]bool
	if opts.Workspace != nil {
		cfg, err := opts.Workspace.GetWorkspaceConfig(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get workspace config: %w", err)
		}
		allowedTools = cfg.AuthorizedTools
	}

	var cwd string
	if opts.Workspace != nil {
		cwd = opts.Workspace.CWD()
	}
	handlers := tools.NewHandlers(opts.Storage, cwd).
		WithTaskManager(opts.TaskManager, opts.SessionID).
		WithSkillResolver(&skillResolver{ws: opts.Workspace, agentName: opts.AgentName})
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

	var boundModel LLM = opts.Model
	if len(activeTools) > 0 && opts.Model != nil {
		boundModel = opts.Model.BindTools(activeTools...)
	}

	var container *tool.Container
	if len(activeTools) > 0 {
		container = tool.NewContainer(activeTools...)
	}

	return &AgentGraph{
		model:          boundModel,
		container:      container,
		inbox:          opts.Inbox,
		systemPrompt:   opts.SystemPrompt,
		agentName:      opts.AgentName,
		onTodosUpdated: opts.OnTodosUpdated,
	}, nil
}

// Build compiles the graph using the loom builder.
func (a *AgentGraph) Build(cp graph.Checkpointer) (*graph.Graph[AgentState], error) {
	builder := graph.New[AgentState]().
		WithName("agent_loop").
		AddNode("check_inbox", graph.NodeFunc(a.checkInbox)).
		AddNode("think", graph.NodeFunc(a.think)).
		AddNode("execute_tools", graph.NodeFunc(a.executeTools)).
		AddEdge(graph.START, "check_inbox")

	builder.AddRouteEdge("check_inbox", func(s AgentState) (string, error) {
		if len(s.Messages) == 0 {
			return graph.END, nil
		}
		lastMsg := s.Messages[len(s.Messages)-1]
		if lastMsg.Role() == message.RoleAssistant {
			return graph.END, nil
		}
		return "think", nil
	}, map[string]string{
		"think":   "think",
		graph.END: graph.END,
	})

	builder.AddRouteEdge("think", func(s AgentState) (string, error) {
		if len(s.Messages) == 0 {
			return "check_inbox", nil
		}
		lastMsg := s.Messages[len(s.Messages)-1]
		for _, block := range lastMsg.GetContent() {
			if _, ok := block.(*message.ToolCall); ok {
				return "execute_tools", nil
			}
		}
		return "check_inbox", nil
	}, map[string]string{
		"execute_tools": "execute_tools",
		"check_inbox":   "check_inbox",
	})

	builder.AddEdge("execute_tools", "check_inbox")

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
	if a.systemPrompt != "" {
		systemMsg := message.NewSystemText(a.systemPrompt)
		rehydratedMessages = append([]message.Message{systemMsg}, rehydratedMessages...)
	}

	msg, err := a.model.Invoke(ctx, rehydratedMessages)
	if err != nil {
		return nil, fmt.Errorf("llm call failed: %w", err)
	}

	if a.agentName != "" {
		meta := msg.GetMetadata()
		if meta == nil {
			meta = make(map[string]any)
		}
		meta["agent_name"] = a.agentName
		msg.SetMetadata(meta)
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

		// Apply hooks for successful tool executions
		for _, res := range toolResults {
			if tMsg, ok := res.(*message.Tool); ok {
				if tMsg.IsError {
					continue
				}
				// Find corresponding tool call
				for _, block := range lastMsg.GetContent() {
					if tc, ok := block.(*message.ToolCall); ok && tc.ID == tMsg.ToolCallID {
						if hook, ok := toolStateHooks[tc.Name]; ok {
							if updateFn, err := hook(ctx, tc.Args, a); err == nil && updateFn != nil {
								state = updateFn(state)
							}
						}
					}
				}
			}
		}

		return state
	}), nil
}

// checkInbox checks for new user messages in the inbox and appends them to the execution state.
func (a *AgentGraph) checkInbox(ctx context.Context, s AgentState) (graph.Command[AgentState], error) {
	if a.inbox == nil {
		return graph.Update[AgentState](func(state AgentState) AgentState {
			return state
		}), nil
	}

	msgs := a.inbox.PopMessages()
	if len(msgs) == 0 {
		return graph.Update[AgentState](func(state AgentState) AgentState {
			return state
		}), nil
	}

	sw, hasWriter := stream.WriterFromContext(ctx)
	if hasWriter {
		for _, msg := range msgs {
			_ = sw.Write(ctx, stream.Event{
				Name: "user_message",
				Data: msg,
			})
		}
	}

	return graph.Update[AgentState](func(state AgentState) AgentState {
		if len(msgs) > 0 {
			InjectReminders(msgs[len(msgs)-1], state)
		}
		state.Messages = append(state.Messages, msgs...)
		return state
	}), nil
}

// InjectReminders appends system reminders to the user message.
func InjectReminders(msg message.Message, s AgentState) {
	userMsg, ok := msg.(*message.User)
	if !ok {
		return
	}

	meta := userMsg.GetMetadata()
	if meta != nil {
		if isSys, ok := meta["is_system_notification"].(bool); ok && isSys {
			return
		}
	}

	var reminders []string
	if len(s.Todos) == 0 {
		reminders = append(reminders, "Your todos list is currently empty. If a task is non-trivial, you must establish a plan first. Use the todo tool to create your initial task list before you do anything else—including reading project plans, fetching files, or exploring the codebase. You can always update the todos later as you discover more. Do not mention this reminder or the empty state to the user.")
	}

	if len(s.ActivatedSkills) == 0 {
		reminders = append(reminders, "You do not have any skills activated. If the user's request matches one of your available skills, you must activate it using the 'activate_skill' tool first.")
	}

	if len(reminders) > 0 {
		reminderBlock := fmt.Sprintf("<system_reminder>\n%s\n</system_reminder>", strings.Join(reminders, "\n\n"))
		userMsg.Content = append(userMsg.Content, &message.TextBlock{Text: "\n\n" + reminderBlock})
	}
}
