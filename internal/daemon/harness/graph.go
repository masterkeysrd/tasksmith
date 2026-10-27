package harness

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/masterkeysrd/loom/graph"
	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/loom/stream"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/daemon/permissions"
	"github.com/masterkeysrd/tasksmith/internal/daemon/scope"
	"github.com/masterkeysrd/tasksmith/internal/daemon/std"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
)

const (
	eventNodeMessage      = "on_node_message"
	eventCommandStarted   = "on_command_started"
	eventCommandCompleted = "on_command_completed"
)

func buildGraph(cp graph.Checkpointer, model *llm.Model, container tools.Container, perms permissions.SessionManager, systemPrompt string, queue Queue, bash BashRunner, spiller tools.Spiller, logger log.Interface) (*graph.Graph[State], error) {
	agentNode := NewAgentNode(model, container, systemPrompt, logger)
	toolsNode := NewToolsNode(container, perms, logger)
	inboxNode := NewInboxNode(queue, logger)
	commandNode := NewCommandNode(queue, bash, spiller, logger)

	hasLeadingCommand := func(_ State) bool {
		item, ok := queue.Peek()
		return ok && item.Kind == QueueKindCommand
	}
	shouldWakeAgent := func(s State) bool {
		if len(s.Messages) == 0 {
			return false
		}
		last := s.Messages[len(s.Messages)-1]
		if _, isAssistant := last.(*message.Assistant); isAssistant {
			return false
		}
		return SystemKindOf(last) != SystemKindCommand
	}

	g, err := graph.New[State]().
		WithName("harness").
		WithCheckpointer(cp).
		AddNode("command", commandNode).
		AddNode("inbox", inboxNode).
		AddNode("agent", agentNode).
		AddNode("tools", toolsNode).
		AddEdge(graph.START, "command").
		AddConditionalEdge("command", "command", hasLeadingCommand).
		AddConditionalEdge("command", "inbox", func(s State) bool {
			return !hasLeadingCommand(s)
		}).
		AddConditionalEdge("inbox", "agent", shouldWakeAgent).
		AddConditionalEdge("inbox", graph.END, func(s State) bool {
			return !shouldWakeAgent(s)
		}).
		AddConditionalEdge("agent", "tools", State.HasPendingToolCalls).
		AddConditionalEdge("agent", "command", func(s State) bool {
			return !s.HasPendingToolCalls()
		}).
		AddEdge("tools", "inbox").
		Build()
	if err != nil {
		return nil, fmt.Errorf("harness: build graph: %w", err)
	}

	return g, nil
}

func emitNodeMessage(ctx context.Context, msg message.Message) {
	if msg == nil || SystemKindOf(msg) != "" {
		return
	}
	if sw, ok := stream.WriterFromContext(ctx); ok && sw != nil {
		_ = sw.Write(ctx, stream.Event{
			Name: eventNodeMessage,
			Data: message.CloneMessage(msg),
		})
	}
}

func emitCommandStarted(ctx context.Context, cmd CommandStarted) {
	if sw, ok := stream.WriterFromContext(ctx); ok && sw != nil {
		_ = sw.Write(ctx, stream.Event{
			Name: eventCommandStarted,
			Data: cmd,
		})
	}
}

func emitCommandCompleted(ctx context.Context, res CommandCompleted) {
	if sw, ok := stream.WriterFromContext(ctx); ok && sw != nil {
		_ = sw.Write(ctx, stream.Event{
			Name: eventCommandCompleted,
			Data: res,
		})
	}
}

// CommandNode executes shell commands and injects their outputs into the conversation state.
type CommandNode struct {
	queue   Queue
	bash    BashRunner
	spiller tools.Spiller
	logger  log.Interface
}

func NewCommandNode(queue Queue, bash BashRunner, spiller tools.Spiller, logger log.Interface) *CommandNode {
	return &CommandNode{
		queue:   queue,
		bash:    bash,
		spiller: spiller,
		logger:  logger.ForComponent("harness.command"),
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

	emitCommandStarted(ctx, CommandStarted{ID: cmdID, Command: payload.Command})

	w := n.spiller.NewWriter(ctx)
	defer w.Close()

	res, err := n.bash.Bash(ctx, std.BashRequest{Command: payload.Command, Output: w})
	w.Close()
	if err != nil {
		return nil, err
	}
	outText := strings.TrimRight(w.String(), "\r\n")

	emitCommandCompleted(ctx, CommandCompleted{
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
		s.Messages = append(s.Messages, msg)
		return s
	}), nil
}

// AgentNode is the primary reasoning and completion node in the harness workflow graph.
// It implements [github.com/masterkeysrd/loom/graph.Node].
type AgentNode struct {
	model        *llm.Model
	tools        tools.Container
	systemPrompt string
	logger       log.Interface
}

// NewAgentNode constructs a new [AgentNode] bound to the resolved LLM model,
// tool container, system prompt, and structured logger.
func NewAgentNode(model *llm.Model, container tools.Container, systemPrompt string, logger log.Interface) *AgentNode {
	return &AgentNode{
		model:        model,
		tools:        container,
		systemPrompt: strings.TrimSpace(systemPrompt),
		logger:       logger.ForComponent("harness.agent"),
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
	emitNodeMessage(ctx, resp)

	return graph.Update[State](func(s State) State {
		s.Messages = append(s.Messages, resp)
		return s
	}), nil
}

// InboxNode drains messages from the session queue and injects them into the conversation state.
// It implements [github.com/masterkeysrd/loom/graph.Node].
type InboxNode struct {
	queue  Queue
	logger log.Interface
}

// NewInboxNode constructs a new [InboxNode] bound to the queue and logger.
func NewInboxNode(queue Queue, logger log.Interface) *InboxNode {
	return &InboxNode{
		queue:  queue,
		logger: logger.ForComponent("harness.inbox"),
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
		emitNodeMessage(ctx, cloned)
		drained = append(drained, cloned)
	}

	return graph.Update[State](func(s State) State {
		s.Messages = append(s.Messages, drained...)
		return s
	}), nil
}

// ToolsNode orchestrates pre-flight tool preparation, permission gate evaluation,
// interrupt/resume lifecycles, and tool execution.
// It implements [github.com/masterkeysrd/loom/graph.Node].
type ToolsNode struct {
	tools       tools.Container
	permissions permissions.SessionManager
	logger      log.Interface
}

// NewToolsNode constructs a new [ToolsNode] bound to the tool container, permission manager, and logger.
func NewToolsNode(container tools.Container, perms permissions.SessionManager, logger log.Interface) *ToolsNode {
	return &ToolsNode{
		tools:       container,
		permissions: perms,
		logger:      logger.ForComponent("harness.tools"),
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
	turnID := lastUserMessageID(state.Messages)
	ctx = scope.With(ctx, scope.Scope{TurnID: turnID, MessageID: asstMsgID})

	if len(state.PendingPermissions) > 0 {
		return n.resumeCalls(ctx, unfulfilled, state.PendingPermissions, state.PermissionDecisions)
	}
	return n.evaluateCalls(ctx, unfulfilled)
}

func lastUserMessageID(messages message.MessageList) string {
	for _, message0 := range slices.Backward(messages) {
		if u, ok := message0.(*message.User); ok && u != nil {
			return u.GetID()
		}
	}
	return ""
}

func (n *ToolsNode) cancelCalls(ctx context.Context, asstIdx int, unfulfilled []*message.ToolCall, messages message.MessageList) (graph.Command[State], error) {
	canceledMsgs := make(message.MessageList, 0, len(unfulfilled))
	for _, tc := range unfulfilled {
		toolMsg, err := newErrorToolMessage(tc, "tool execution canceled: user sent a new message")
		if err != nil {
			return nil, err
		}
		emitNodeMessage(ctx, toolMsg)
		canceledMsgs = append(canceledMsgs, toolMsg)
	}

	userID, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("harness: generate user message id: %w", err)
	}
	trailingUser := message.CloneMessage(messages[len(messages)-1])
	trailingUser.SetID(userID.String())
	emitNodeMessage(ctx, trailingUser)

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
		return s
	}), nil
}

func (n *ToolsNode) evaluateCalls(ctx context.Context, unfulfilled []*message.ToolCall) (graph.Command[State], error) {
	toolMsgs := make(message.MessageList, 0, len(unfulfilled))
	var pending []tools.PermissionRequest

	for _, tc := range unfulfilled {
		toolMsg, req, err := n.evaluateSingleCall(ctx, tc)
		if err != nil {
			return nil, err
		}
		if req != nil {
			pending = append(pending, *req)
			continue
		}
		emitNodeMessage(ctx, toolMsg)
		toolMsgs = append(toolMsgs, toolMsg)
	}

	if len(pending) > 0 {
		return graph.InterruptUpdate[State](func(s State) State {
			s.Messages = append(s.Messages, toolMsgs...)
			s.PendingPermissions = clonePermissionRequests(pending)
			s.PermissionDecisions = nil
			return s
		}), nil
	}

	return appendToolMessagesUpdate(toolMsgs), nil
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

func (n *ToolsNode) resumeCalls(ctx context.Context, unfulfilled []*message.ToolCall, pending []tools.PermissionRequest, decisions map[string]tools.PermissionDecision) (graph.Command[State], error) {
	pendingByCall := make(map[string]tools.PermissionRequest, len(pending))
	for _, req := range pending {
		pendingByCall[req.ToolCallID] = req
	}

	toolMsgs := make(message.MessageList, 0, len(unfulfilled))
	for _, tc := range unfulfilled {
		req, ok := pendingByCall[tc.ID]
		if !ok {
			return nil, ErrPendingPermissionsUnresolved
		}
		toolMsg, err := n.resumeSingleCall(ctx, tc, req, decisions)
		if err != nil {
			return nil, err
		}
		emitNodeMessage(ctx, toolMsg)
		toolMsgs = append(toolMsgs, toolMsg)
	}

	return appendToolMessagesUpdate(toolMsgs), nil
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
		return s
	}
}

func appendToolMessagesUpdate(toolMsgs message.MessageList) graph.Update[State] {
	return func(s State) State {
		s.Messages = append(s.Messages, toolMsgs...)
		s.PendingPermissions = nil
		s.PermissionDecisions = nil
		return s
	}
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
