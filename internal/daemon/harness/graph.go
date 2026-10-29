package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/masterkeysrd/loom/graph"
	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/loom/stream"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/daemon/permissions"
	"github.com/masterkeysrd/tasksmith/internal/daemon/scope"
	"github.com/masterkeysrd/tasksmith/internal/daemon/std"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
)

type GraphConfig struct {
	Checkpointer graph.Checkpointer
	Model        *llm.Model
	Container    tools.Container
	Permissions  permissions.SessionManager
	SystemPrompt string
	Queue        Queue
	Bash         BashRunner
	Spiller      tools.Spiller
	Limits       workspace.ModelLimits
	Static       StaticContext
	Logger       log.Interface
}

type AgentNodeConfig struct {
	Model        *llm.Model
	Container    tools.Container
	SystemPrompt string
	Limits       workspace.ModelLimits
	Static       StaticContext
	Logger       log.Interface
}

type CompactionNodeConfig struct {
	Model        *llm.Model
	Container    tools.Container
	SystemPrompt string
	Limits       workspace.ModelLimits
	Static       StaticContext
	Logger       log.Interface
}

type ToolsNodeConfig struct {
	Container   tools.Container
	Permissions permissions.SessionManager
	Limits      workspace.ModelLimits
	Static      StaticContext
	Logger      log.Interface
}

type InboxNodeConfig struct {
	Queue  Queue
	Limits workspace.ModelLimits
	Static StaticContext
	Logger log.Interface
}

type CommandNodeConfig struct {
	Queue   Queue
	Bash    BashRunner
	Spiller tools.Spiller
	Limits  workspace.ModelLimits
	Static  StaticContext
	Logger  log.Interface
}

func buildGraph(cfg GraphConfig) (*graph.Graph[State], error) {
	compactNode := NewCompactionNode(CompactionNodeConfig{
		Model:        cfg.Model,
		Container:    cfg.Container,
		SystemPrompt: cfg.SystemPrompt,
		Limits:       cfg.Limits,
		Static:       cfg.Static,
		Logger:       cfg.Logger,
	})
	agentNode := NewAgentNode(AgentNodeConfig{
		Model:        cfg.Model,
		Container:    cfg.Container,
		SystemPrompt: cfg.SystemPrompt,
		Limits:       cfg.Limits,
		Static:       cfg.Static,
		Logger:       cfg.Logger,
	})
	toolsNode := NewToolsNode(ToolsNodeConfig{
		Container:   cfg.Container,
		Permissions: cfg.Permissions,
		Limits:      cfg.Limits,
		Static:      cfg.Static,
		Logger:      cfg.Logger,
	})
	inboxNode := NewInboxNode(InboxNodeConfig{
		Queue:  cfg.Queue,
		Limits: cfg.Limits,
		Static: cfg.Static,
		Logger: cfg.Logger,
	})
	commandNode := NewCommandNode(CommandNodeConfig{
		Queue:   cfg.Queue,
		Bash:    cfg.Bash,
		Spiller: cfg.Spiller,
		Limits:  cfg.Limits,
		Static:  cfg.Static,
		Logger:  cfg.Logger,
	})

	hasLeadingCommand := func(_ State) bool {
		item, ok := cfg.Queue.Peek()
		return ok && item.Kind == QueueKindCommand
	}
	queueHasItems := func(_ State) bool {
		_, ok := cfg.Queue.Peek()
		return ok
	}

	g, err := graph.New[State]().
		WithName("harness").
		WithCheckpointer(cfg.Checkpointer).
		AddNode("command", commandNode).
		AddNode("inbox", inboxNode).
		AddNode("agent", agentNode).
		AddNode("tools", toolsNode).
		AddNode("compact", compactNode).
		// START routing:
		AddConditionalEdge(graph.START, "compact", State.HasLeadingCompact).
		AddConditionalEdge(graph.START, "tools", func(s State) bool {
			return !s.HasLeadingCompact() && s.HasPendingToolCalls()
		}).
		AddConditionalEdge(graph.START, "command", func(s State) bool {
			return !s.HasLeadingCompact() && !s.HasPendingToolCalls()
		}).
		// Command routing:
		AddConditionalEdge("command", "command", hasLeadingCommand).
		AddConditionalEdge("command", "inbox", func(s State) bool {
			return !hasLeadingCommand(s)
		}).
		// Inbox routing:
		AddConditionalEdge("inbox", "compact", func(s State) bool {
			return s.HasLeadingCompact() || (s.ShouldWakeAgent() && s.ShouldAutoCompact())
		}).
		AddConditionalEdge("inbox", "agent", func(s State) bool {
			return !s.HasLeadingCompact() && !s.ShouldAutoCompact() && s.ShouldWakeAgent()
		}).
		AddConditionalEdge("inbox", graph.END, func(s State) bool {
			return !s.HasLeadingCompact() && !s.ShouldWakeAgent()
		}).
		// Agent routing:
		AddConditionalEdge("agent", "tools", State.HasPendingToolCalls).
		AddConditionalEdge("agent", "compact", func(s State) bool {
			return !s.HasPendingToolCalls() && s.HasLeadingCompact()
		}).
		AddConditionalEdge("agent", "command", func(s State) bool {
			return !s.HasPendingToolCalls() && !s.HasLeadingCompact()
		}).
		// Tools routing:
		AddConditionalEdge("tools", "compact", State.ShouldAutoCompact).
		AddConditionalEdge("tools", "inbox", func(s State) bool {
			return !s.ShouldAutoCompact()
		}).
		// Compact routing:
		AddConditionalEdge("compact", "agent", State.IsAutoCompacted).
		AddConditionalEdge("compact", "command", func(s State) bool {
			return !s.IsAutoCompacted() && hasLeadingCommand(s)
		}).
		AddConditionalEdge("compact", "inbox", func(s State) bool {
			return !s.IsAutoCompacted() && !hasLeadingCommand(s) && queueHasItems(s)
		}).
		AddConditionalEdge("compact", graph.END, func(s State) bool {
			return !s.IsAutoCompacted() && !hasLeadingCommand(s) && !queueHasItems(s)
		}).
		Build()
	if err != nil {
		return nil, fmt.Errorf("harness: build graph: %w", err)
	}

	return g, nil
}

func emit(ctx context.Context, ev StreamEvent) {
	if ev == nil {
		return
	}
	if nme, ok := ev.(NodeMessageEvent); ok {
		if nme.Message == nil || SystemKindOf(nme.Message) != "" {
			return
		}
	}
	if sw, ok := stream.WriterFromContext(ctx); ok && sw != nil {
		_ = sw.Write(ctx, stream.Event{
			Name: ev.EventName(),
			Data: ev,
		})
	}
}

func extractTodos(msg *message.Tool) ([]tools.TodoItem, bool) {
	if msg == nil || msg.Name != "todos" || msg.IsError {
		return nil, false
	}
	out, ok := msg.StructuredContent.(api.TodosOutput)
	if !ok {
		if m, ok := msg.StructuredContent.(map[string]any); ok {
			data, err := json.Marshal(m)
			if err == nil {
				_ = json.Unmarshal(data, &out)
			} else {
				return nil, false
			}
		} else {
			return nil, false
		}
	}
	res := make([]tools.TodoItem, len(out.Todos))
	for i, it := range out.Todos {
		res[i] = tools.TodoItem{
			Content:    it.Content,
			ActiveText: it.ActiveText,
			Status:     tools.TodoStatus(it.Status),
		}
	}
	return res, true
}

// CommandNode executes shell commands and injects their outputs into the conversation state.
type CommandNode struct {
	queue   Queue
	bash    BashRunner
	spiller tools.Spiller
	limits  workspace.ModelLimits
	static  StaticContext
	logger  log.Interface
}

func NewCommandNode(cfg CommandNodeConfig) *CommandNode {
	return &CommandNode{
		queue:   cfg.Queue,
		bash:    cfg.Bash,
		spiller: cfg.Spiller,
		limits:  cfg.Limits,
		static:  cfg.Static,
		logger:  cfg.Logger.ForComponent("harness.command"),
	}
}

func (n *CommandNode) Execute(ctx context.Context, state State) (graph.Command[State], error) {
	item, ok := n.queue.PopCommand()
	if !ok {
		return graph.Update[State](func(s State) State { return s }), nil
	}
	payload, ok := item.Payload.(QueueCommandPayload)
	if !ok {
		return graph.Update[State](func(s State) State { return s }), nil
	}
	cmdID := item.ID
	if cmdID == "" || uuid.Validate(cmdID) != nil {
		if id, err := uuid.NewV7(); err == nil {
			cmdID = id.String()
		}
	}

	emit(ctx, CommandStarted{ID: cmdID, Command: payload.Command})

	w := n.spiller.NewWriter(ctx)
	defer w.Close()

	res, err := n.bash.Bash(ctx, std.BashRequest{Command: payload.Command, Output: w})
	w.Close()
	if err != nil {
		return nil, err
	}
	outText := strings.TrimRight(w.String(), "\r\n")

	emit(ctx, CommandCompleted{
		ID:         cmdID,
		Command:    payload.Command,
		Output:     outText,
		ExitCode:   res.ExitCode,
		DurationMs: res.Duration.Milliseconds(),
	})

	msgText := fmt.Sprintf("<bash_command command=%q exit_code=\"%d\">\n%s\n</bash_command>", payload.Command, res.ExitCode, outText)
	msg := message.NewUserTextMeta(msgText, map[string]any{MetaSystemKind: SystemKindCommand})
	msg.SetID(cmdID)

	return graph.Update[State](func(s State) State {
		s = s.AppendMessage(msg, MessageUpdateOptions{Static: n.static, Limits: n.limits})
		emit(ctx, ContextUpdatedEvent{Context: s.Context})
		return s
	}), nil
}

// AgentNode is the primary reasoning and completion node in the harness workflow graph.
// It implements [github.com/masterkeysrd/loom/graph.Node].
type AgentNode struct {
	model        *llm.Model
	tools        tools.Container
	systemPrompt string
	limits       workspace.ModelLimits
	static       StaticContext
	logger       log.Interface
}

// NewAgentNode constructs a new [AgentNode] bound to the resolved LLM model,
// tool container, system prompt, limits, static context, and structured logger.
func NewAgentNode(cfg AgentNodeConfig) *AgentNode {
	return &AgentNode{
		model:        cfg.Model,
		tools:        cfg.Container,
		systemPrompt: strings.TrimSpace(cfg.SystemPrompt),
		limits:       cfg.Limits,
		static:       cfg.Static,
		logger:       cfg.Logger.ForComponent("harness.agent"),
	}
}

// Execute binds all registered tool definitions to the LLM model, invokes it with
// the transient system prompt prepended to the conversation state, emits the
// resulting assistant message to the stream, and appends it to [State.Messages].
func (n *AgentNode) Execute(ctx context.Context, state State) (graph.Command[State], error) {
	llmMessages := make([]message.Message, 0, len(state.Messages)+1)
	if n.systemPrompt != "" {
		llmMessages = append(llmMessages, message.NewSystemText(n.systemPrompt))
	}
	llmMessages = append(llmMessages, state.Messages...)

	boundModel := n.model
	if defs := n.tools.Definitions(); len(defs) > 0 {
		boundModel = n.model.BindToolDefs(defs...)
	}

	resp, err := boundModel.Invoke(ctx, llmMessages)
	if err != nil {
		return nil, fmt.Errorf("harness: invoke llm: %w", err)
	}

	id, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("harness: generate message id: %w", err)
	}
	resp.ID = id.String()
	emit(ctx, NodeMessageEvent{Message: resp})

	if resp.Metrics != nil {
		prevCached := state.Context.CachedTokens
		currCached := resp.Metrics.Tokens.CacheRead
		if prevCached > 0 && currCached < prevCached {
			emit(ctx, CacheBroken{
				PrevCached:  prevCached,
				CurrCached:  currCached,
				InputTokens: resp.Metrics.Tokens.Input,
			})
		}
	}

	return graph.Update[State](func(s State) State {
		s = s.AppendMessage(resp, MessageUpdateOptions{Static: n.static, Limits: n.limits})
		emit(ctx, ContextUpdatedEvent{Context: s.Context})
		return s
	}), nil
}

// InboxNode drains messages from the session queue and injects them into the conversation state.
// It implements [github.com/masterkeysrd/loom/graph.Node].
type InboxNode struct {
	queue  Queue
	limits workspace.ModelLimits
	static StaticContext
	logger log.Interface
}

// NewInboxNode constructs a new [InboxNode] bound to the queue and logger.
func NewInboxNode(cfg InboxNodeConfig) *InboxNode {
	return &InboxNode{
		queue:  cfg.Queue,
		limits: cfg.Limits,
		static: cfg.Static,
		logger: cfg.Logger.ForComponent("harness.inbox"),
	}
}

// Execute drains queued messages from the queue, generates IDs if needed, emits notifications,
// and appends them to [State.Messages].
func (n *InboxNode) Execute(ctx context.Context, state State) (graph.Command[State], error) {
	queued := n.queue.PopMessages()
	if len(queued) == 0 {
		return graph.Update[State](func(s State) State { return s }), nil
	}

	drained := make(message.MessageList, 0, len(queued))
	for _, msg := range queued {
		if msg == nil {
			continue
		}
		cloned := message.CloneMessage(msg)
		if cloned.GetID() == "" || uuid.Validate(cloned.GetID()) != nil {
			if id, err := uuid.NewV7(); err == nil {
				cloned.SetID(id.String())
			}
		}
		emit(ctx, NodeMessageEvent{Message: cloned})
		drained = append(drained, cloned)
	}

	return graph.Update[State](func(s State) State {
		s = s.AppendMessages(drained, MessageUpdateOptions{Static: n.static, Limits: n.limits})
		emit(ctx, ContextUpdatedEvent{Context: s.Context})
		return s
	}), nil
}

// ToolsNode orchestrates pre-flight tool preparation, permission gate evaluation,
// interrupt/resume lifecycles, and tool execution.
// It implements [github.com/masterkeysrd/loom/graph.Node].
type ToolsNode struct {
	tools       tools.Container
	permissions permissions.SessionManager
	limits      workspace.ModelLimits
	static      StaticContext
	logger      log.Interface
}

// NewToolsNode constructs a new [ToolsNode] bound to the tool container, permission manager, and logger.
func NewToolsNode(cfg ToolsNodeConfig) *ToolsNode {
	return &ToolsNode{
		tools:       cfg.Container,
		permissions: cfg.Permissions,
		limits:      cfg.Limits,
		static:      cfg.Static,
		logger:      cfg.Logger.ForComponent("harness.tools"),
	}
}

// Execute handles tool call evaluation, permission interrupts, permission resumes,
// and cancellation when a user sends a new message while tool calls are pending.
func (n *ToolsNode) Execute(ctx context.Context, state State) (graph.Command[State], error) {
	asstIdx, _, unfulfilled, hasUserAfter := state.ActiveToolCalls()
	if asstIdx < 0 || len(unfulfilled) == 0 {
		return clearPermissionsUpdate(), nil
	}
	if hasUserAfter {
		return n.cancelCalls(ctx, asstIdx, unfulfilled, state.Messages)
	}

	asstMsgID := state.Messages[asstIdx].GetID()
	turnID := state.LastUserMessageID()
	ctx = scope.With(ctx, scope.Scope{TurnID: turnID, MessageID: asstMsgID})

	if len(state.PendingPermissions) > 0 || len(state.PendingQuestions) > 0 {
		return n.resumeCalls(ctx, unfulfilled, state.PendingPermissions, state.PermissionDecisions, state.PendingQuestions, state.QuestionResponses)
	}
	return n.evaluateCalls(ctx, unfulfilled)
}

func (n *ToolsNode) cancelCalls(ctx context.Context, asstIdx int, unfulfilled []*message.ToolCall, messages message.MessageList) (graph.Command[State], error) {
	canceledMsgs := make(message.MessageList, 0, len(unfulfilled))
	for _, tc := range unfulfilled {
		toolMsg, err := newErrorToolMessage(tc, "tool execution canceled: user sent a new message")
		if err != nil {
			return nil, err
		}
		emit(ctx, NodeMessageEvent{Message: toolMsg})
		canceledMsgs = append(canceledMsgs, toolMsg)
	}

	userID, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("harness: generate user message id: %w", err)
	}
	trailingUser := message.CloneMessage(messages[len(messages)-1])
	trailingUser.SetID(userID.String())
	emit(ctx, NodeMessageEvent{Message: trailingUser})

	return graph.Update[State](func(s State) State {
		if len(s.Messages) > 0 {
			s.Messages[len(s.Messages)-1].SetID(userID.String())
		}
		insertIdx := firstNonToolIndex(s.Messages, asstIdx+1)
		updated := make(message.MessageList, 0, len(s.Messages)+len(canceledMsgs))
		updated = append(updated, s.Messages[:insertIdx]...)
		updated = append(updated, canceledMsgs...)
		updated = append(updated, s.Messages[insertIdx:]...)
		s.Messages = updated
		s.PendingPermissions = nil
		s.PermissionDecisions = nil
		s.PendingQuestions = nil
		s.QuestionResponses = nil
		s.Context = computeContextState(n.static, s, n.limits)
		emit(ctx, ContextUpdatedEvent{Context: s.Context})
		return s
	}), nil
}

func (n *ToolsNode) evaluateCalls(ctx context.Context, unfulfilled []*message.ToolCall) (graph.Command[State], error) {
	toolMsgs := make(message.MessageList, 0, len(unfulfilled))
	var pending []tools.PermissionRequest
	var questions []tools.QuestionRequest
	var updatedTodos []tools.TodoItem
	var hasTodos bool

	for _, tc := range unfulfilled {
		if tc.Name == "ask_question" {
			plan, err := n.tools.Prepare(ctx, tc)
			if err == nil {
				var access tools.AccessDecision
				var req *tools.PermissionRequest
				access, req, err = n.permissions.Evaluate(plan)
				if err == nil {
					switch access {
					case tools.AccessAsk:
						if req == nil {
							err = tools.ErrPermissionDenied
						} else {
							pending = append(pending, *req)
							continue
						}
					case tools.AccessAllow:
					default:
						err = tools.ErrPermissionDenied
					}
				}
			}
			var question tools.QuestionRequest
			if err == nil {
				question, err = newQuestionRequest(tc)
			}
			if err != nil {
				msg, msgErr := newErrorToolMessage(tc, err.Error())
				if msgErr != nil {
					return nil, msgErr
				}
				emit(ctx, NodeMessageEvent{Message: msg})
				toolMsgs = append(toolMsgs, msg)
				continue
			}
			questions = append(questions, question)
			continue
		}
		toolMsg, req, err := n.evaluateSingleCall(ctx, tc)
		if err != nil {
			return nil, err
		}
		if req != nil {
			pending = append(pending, *req)
			continue
		}
		emit(ctx, NodeMessageEvent{Message: toolMsg})
		toolMsgs = append(toolMsgs, toolMsg)
		if todos, ok := extractTodos(toolMsg); ok {
			updatedTodos = todos
			hasTodos = true
			emit(ctx, TodosUpdatedEvent{Todos: updatedTodos})
		}
	}

	if len(pending) > 0 || len(questions) > 0 {
		return graph.InterruptUpdate[State](func(s State) State {
			s.Messages = append(s.Messages, toolMsgs...)
			s.PendingPermissions = clonePermissionRequests(pending)
			s.PermissionDecisions = nil
			s.PendingQuestions = cloneQuestionRequests(questions)
			s.QuestionResponses = nil
			if hasTodos {
				s.Todos = slices.Clone(updatedTodos)
			}
			s.Context = computeContextState(n.static, s, n.limits)
			emit(ctx, ContextUpdatedEvent{Context: s.Context})
			return s
		}), nil
	}

	return n.appendToolMessagesUpdate(ctx, toolMsgs, updatedTodos, hasTodos), nil
}

func newQuestionRequest(tc *message.ToolCall) (tools.QuestionRequest, error) {
	data, err := json.Marshal(tc.Args)
	if err != nil {
		return tools.QuestionRequest{}, err
	}
	var input api.AskQuestionInput
	if err := json.Unmarshal(data, &input); err != nil {
		return tools.QuestionRequest{}, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return tools.QuestionRequest{}, err
	}
	return tools.QuestionRequest{ID: id.String(), ToolCallID: tc.ID, Title: input.Title, Description: input.Description, Questions: input.Questions}, nil
}

func (n *ToolsNode) evaluateSingleCall(ctx context.Context, tc *message.ToolCall) (*message.Tool, *tools.PermissionRequest, error) {
	plan, err := n.tools.Prepare(ctx, tc)
	if err != nil {
		msg, msgErr := newErrorToolMessage(tc, err.Error())
		return msg, nil, msgErr
	}

	access, req, err := n.permissions.Evaluate(plan)
	if err != nil {
		msg, msgErr := newErrorToolMessage(tc, err.Error())
		return msg, nil, msgErr
	}
	if access == tools.AccessAsk && req != nil {
		return nil, req, nil
	}
	if access == tools.AccessDeny {
		msg, msgErr := newErrorToolMessage(tc, tools.ErrPermissionDenied.Error())
		return msg, nil, msgErr
	}

	msg, execErr := n.tools.Execute(ctx, tc)
	if execErr != nil {
		return nil, nil, fmt.Errorf("harness: execute tool %q: %w", tc.Name, execErr)
	}
	return msg, nil, nil
}

func (n *ToolsNode) resumeCalls(ctx context.Context, unfulfilled []*message.ToolCall, pending []tools.PermissionRequest, decisions map[string]tools.PermissionDecision, questions []tools.QuestionRequest, answers map[string]tools.QuestionResponse) (graph.Command[State], error) {
	pendingByCall := make(map[string]tools.PermissionRequest, len(pending))
	for _, req := range pending {
		pendingByCall[req.ToolCallID] = req
	}
	questionsByCall := make(map[string]tools.QuestionRequest, len(questions))
	for _, req := range questions {
		questionsByCall[req.ToolCallID] = req
	}

	toolMsgs := make(message.MessageList, 0, len(unfulfilled))
	var nextQuestions []tools.QuestionRequest
	var updatedTodos []tools.TodoItem
	var hasTodos bool

	for _, tc := range unfulfilled {
		if req, ok := questionsByCall[tc.ID]; ok {
			response, found := lookupQuestionResponse(answers, req)
			if !found {
				return nil, ErrPendingQuestionsUnresolved
			}
			out := api.AskQuestionOutput{Canceled: response.Canceled, Answers: make([]api.QuestionAnswer, len(response.Answers))}
			for i, answer := range response.Answers {
				out.Answers[i] = api.QuestionAnswer{Question: answer.Question, Selected: slices.Clone(answer.Selected), Text: answer.Text}
				if i < len(req.Questions) && req.Questions[i].Type == string(api.QuestionTypeConfirm) {
					out.Answers[i].Confirmed = new(answer.Confirmed)
				}
			}
			data, err := json.Marshal(out)
			if err != nil {
				return nil, err
			}
			id, err := uuid.NewV7()
			if err != nil {
				return nil, err
			}
			msg := &message.Tool{Base: message.Base{ID: id.String()}, ToolCallID: tc.ID, Name: tc.Name, Content: message.Content{&message.TextBlock{Text: string(data)}}, StructuredContent: out}
			emit(ctx, NodeMessageEvent{Message: msg})
			toolMsgs = append(toolMsgs, msg)
			continue
		}
		req, ok := pendingByCall[tc.ID]
		if !ok {
			return nil, ErrPendingPermissionsUnresolved
		}
		if tc.Name == "ask_question" {
			dec, ok := lookupDecision(decisions, req)
			if !ok {
				return nil, ErrPendingPermissionsUnresolved
			}
			if err := n.permissions.ApplyDecision(&req, dec); err != nil {
				msg, msgErr := newErrorToolMessage(tc, err.Error())
				if msgErr != nil {
					return nil, msgErr
				}
				emit(ctx, NodeMessageEvent{Message: msg})
				toolMsgs = append(toolMsgs, msg)
				continue
			}
			question, err := newQuestionRequest(tc)
			if err != nil {
				return nil, err
			}
			nextQuestions = append(nextQuestions, question)
			continue
		}
		toolMsg, err := n.resumeSingleCall(ctx, tc, req, decisions)
		if err != nil {
			return nil, err
		}
		emit(ctx, NodeMessageEvent{Message: toolMsg})
		toolMsgs = append(toolMsgs, toolMsg)
		if todos, ok := extractTodos(toolMsg); ok {
			updatedTodos = todos
			hasTodos = true
			emit(ctx, TodosUpdatedEvent{Todos: updatedTodos})
		}
	}

	if len(nextQuestions) > 0 {
		return graph.InterruptUpdate[State](func(s State) State {
			s = n.appendToolMessagesUpdate(ctx, toolMsgs, updatedTodos, hasTodos)(s)
			s.PendingQuestions = cloneQuestionRequests(nextQuestions)
			return s
		}), nil
	}
	return n.appendToolMessagesUpdate(ctx, toolMsgs, updatedTodos, hasTodos), nil
}

func (n *ToolsNode) resumeSingleCall(ctx context.Context, tc *message.ToolCall, req tools.PermissionRequest, decisions map[string]tools.PermissionDecision) (*message.Tool, error) {
	dec, ok := lookupDecision(decisions, req)
	if !ok {
		return nil, ErrPendingPermissionsUnresolved
	}
	if err := n.permissions.ApplyDecision(&req, dec); err != nil {
		return newErrorToolMessage(tc, err.Error())
	}
	toolMsg, err := n.tools.Execute(ctx, tc)
	if err != nil {
		return nil, fmt.Errorf("harness: execute tool %q: %w", tc.Name, err)
	}
	return toolMsg, nil
}

func firstNonToolIndex(messages message.MessageList, start int) int {
	idx := start
	for idx < len(messages) {
		if _, isTool := messages[idx].(*message.Tool); !isTool {
			return idx
		}
		idx++
	}
	return idx
}

func clearPermissionsUpdate() graph.Update[State] {
	return func(s State) State {
		s.PendingPermissions = nil
		s.PermissionDecisions = nil
		s.PendingQuestions = nil
		s.QuestionResponses = nil
		return s
	}
}

func (n *ToolsNode) appendToolMessagesUpdate(ctx context.Context, toolMsgs message.MessageList, todos []tools.TodoItem, hasTodos bool) graph.Update[State] {
	return func(s State) State {
		s = s.AppendMessages(toolMsgs, MessageUpdateOptions{Static: n.static, Limits: n.limits})
		s.PendingPermissions = nil
		s.PermissionDecisions = nil
		s.PendingQuestions = nil
		s.QuestionResponses = nil
		if hasTodos {
			s.Todos = slices.Clone(todos)
		}
		emit(ctx, ContextUpdatedEvent{Context: s.Context})
		return s
	}
}

func lookupQuestionResponse(answers map[string]tools.QuestionResponse, req tools.QuestionRequest) (tools.QuestionResponse, bool) {
	if response, ok := answers[req.ID]; ok {
		return response, true
	}
	response, ok := answers[req.ToolCallID]
	return response, ok
}

func lookupDecision(decisions map[string]tools.PermissionDecision, req tools.PermissionRequest) (tools.PermissionDecision, bool) {
	if len(decisions) == 0 {
		return tools.PermissionDecision{}, false
	}
	if dec, ok := decisions[req.ID]; ok && isValidDecisionKind(dec.Kind) {
		return dec, true
	}
	if dec, ok := decisions[req.ToolCallID]; ok && isValidDecisionKind(dec.Kind) {
		return dec, true
	}
	return tools.PermissionDecision{}, false
}

func isValidDecisionKind(k tools.PermissionDecisionKind) bool {
	switch k {
	case tools.DecisionAllowOnce, tools.DecisionAllowSession, tools.DecisionReject:
		return true
	default:
		return false
	}
}

func newErrorToolMessage(tc *message.ToolCall, errText string) (*message.Tool, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("harness: generate tool message id: %w", err)
	}
	return &message.Tool{
		Base:       message.Base{ID: id.String()},
		ToolCallID: tc.ID,
		Name:       tc.Name,
		IsError:    true,
		Content: message.Content{
			&message.TextBlock{Text: errText},
		},
	}, nil
}
