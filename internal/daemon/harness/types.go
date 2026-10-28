package harness

import (
	"context"
	"errors"
	"maps"
	"slices"
	"time"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/std"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
)

// Sentinel errors returned by the harness package.
var (
	ErrInvalidConfig                = errors.New("harness: invalid configuration")
	ErrEmptyThreadID                = errors.New("harness: thread ID must not be empty")
	ErrNilMessage                   = errors.New("harness: input message must not be nil")
	ErrNoPendingPermissions         = errors.New("harness: no pending permissions for thread")
	ErrPendingPermissionsUnresolved = errors.New("harness: all pending permissions must be resolved before resuming")
	ErrNoPendingQuestions           = errors.New("harness: no pending questions for thread")
	ErrPendingQuestionsUnresolved   = errors.New("harness: all pending questions must be resolved before resuming")
)

const (
	MetaSystemKind        = "tasksmith.system.kind"
	MetaCompactionTrigger = "tasksmith.compaction.trigger"
	MetaCompactionFocus   = "tasksmith.compaction.focus"
)

const (
	SystemKindCommand    = "command"
	SystemKindTask       = "task"
	SystemKindCompaction = "compaction"
)

// SystemKindOf returns the "tasksmith.system.kind" metadata value on msg, or "" if unset.
func SystemKindOf(msg message.Message) string {
	if msg == nil {
		return ""
	}
	meta := msg.GetMetadata()
	if meta == nil {
		return ""
	}
	v, _ := meta[MetaSystemKind].(string)
	return v
}

type QueueKind string

const (
	QueueKindMessage QueueKind = "message"
	QueueKindCommand QueueKind = "command"
	QueueKindTask    QueueKind = "task" // internal only; filtered out of API
)

type QueueItemPayload interface {
	QueueKind() QueueKind
}

type QueueMessagePayload struct {
	Message message.Message
}

func (QueueMessagePayload) QueueKind() QueueKind { return QueueKindMessage }

type QueueCommandPayload struct {
	Command string
}

func (QueueCommandPayload) QueueKind() QueueKind { return QueueKindCommand }

type QueueTaskPayload struct {
	Message message.Message
}

func (QueueTaskPayload) QueueKind() QueueKind { return QueueKindTask }

type QueueItem struct {
	ID        string
	Kind      QueueKind
	Payload   QueueItemPayload
	CreatedAt time.Time
}

// Queue is the unified FIFO queue of pending messages, tasks, and shell commands
// shared between Session and the Harness execution graph.
type Queue interface {
	Peek() (QueueItem, bool)
	PopCommand() (QueueItem, bool)
	PopMessages() []message.Message
}

// BashRunner executes a shell command inside the session environment.
// Satisfied by *std.Std.
type BashRunner interface {
	Bash(ctx context.Context, req std.BashRequest) (*std.BashResult, error)
}

const (
	eventNodeMessage         = "on_node_message"
	eventCommandStarted      = "on_command_started"
	eventCommandCompleted    = "on_command_completed"
	eventTodosUpdated        = "on_todos_updated"
	eventCompactionStarted   = "on_compaction_started"
	eventCompactionCompleted = "on_compaction_completed"
	eventContextUpdated      = "on_context_updated"
	eventCacheBroken         = "on_cache_broken"
)

// StreamEvent represents an event yielded from the harness graph nodes.
type StreamEvent interface {
	EventName() string
	ToEvent() Event
}

type NodeMessageEvent struct {
	Message message.Message
}

func (e NodeMessageEvent) EventName() string { return eventNodeMessage }
func (e NodeMessageEvent) ToEvent() Event {
	return Event{
		Type:    EventMessage,
		Message: message.CloneMessage(e.Message),
	}
}

type CommandStarted struct {
	ID      string
	Command string
}

func (e CommandStarted) EventName() string { return eventCommandStarted }
func (e CommandStarted) ToEvent() Event {
	cmd := e
	return Event{
		Type:           EventCommandStarted,
		CommandStarted: &cmd,
	}
}

type CommandCompleted struct {
	ID         string
	Command    string
	Output     string
	ExitCode   int
	DurationMs int64
}

func (e CommandCompleted) EventName() string { return eventCommandCompleted }
func (e CommandCompleted) ToEvent() Event {
	cmd := e
	return Event{
		Type:             EventCommandCompleted,
		CommandCompleted: &cmd,
	}
}

type TodosUpdatedEvent struct {
	Todos []tools.TodoItem
}

func (e TodosUpdatedEvent) EventName() string { return eventTodosUpdated }
func (e TodosUpdatedEvent) ToEvent() Event {
	return Event{
		Type:  EventTodosUpdated,
		Todos: slices.Clone(e.Todos),
	}
}

type CompactionStarted struct {
	ID      string
	Trigger string // "auto" | "manual"
	Focus   string
}

func (e CompactionStarted) EventName() string { return eventCompactionStarted }
func (e CompactionStarted) ToEvent() Event {
	cs := e
	return Event{
		Type:              EventCompactionStarted,
		CompactionStarted: &cs,
	}
}

type CompactionCompleted struct {
	ID           string
	Trigger      string // "auto" | "manual"
	Focus        string
	Summary      string
	BeforeTokens int
	AfterTokens  int
	DurationMs   int64
}

func (e CompactionCompleted) EventName() string { return eventCompactionCompleted }
func (e CompactionCompleted) ToEvent() Event {
	cc := e
	return Event{
		Type:                EventCompactionCompleted,
		CompactionCompleted: &cc,
	}
}

type ContextUpdatedEvent struct {
	Context api.ContextState
}

func (e ContextUpdatedEvent) EventName() string { return eventContextUpdated }
func (e ContextUpdatedEvent) ToEvent() Event {
	ctx := e.Context
	return Event{
		Type:    EventContextUpdated,
		Context: &ctx,
	}
}

type CacheBroken struct {
	PrevCached  int
	CurrCached  int
	InputTokens int
}

func (e CacheBroken) EventName() string { return eventCacheBroken }
func (e CacheBroken) ToEvent() Event {
	cb := e
	return Event{
		Type:        EventCacheBroken,
		CacheBroken: &cb,
	}
}

// CompactInput provides parameters for direct context compaction.
type CompactInput struct {
	Focus string
}

// Input represents the conversational input to [Harness.Stream].
// It can provide either a new user message, resolved permission decisions and question answers,
// or a compaction request.
type Input struct {
	Message   message.Message
	Decisions map[string]tools.PermissionDecision
	Answers   map[string]tools.QuestionResponse
	Compact   *CompactInput
}

// State holds the checkpointed conversation and permission state for a thread.
// It implements [github.com/masterkeysrd/loom/graph.State].
type State struct {
	Messages            message.MessageList                 `json:"messages"`
	PendingCompaction   *CompactInput                       `json:"pending_compaction,omitempty"`
	PendingPermissions  []tools.PermissionRequest           `json:"pending_permissions,omitempty"`
	PermissionDecisions map[string]tools.PermissionDecision `json:"permission_decisions,omitempty"`
	PendingQuestions    []tools.QuestionRequest             `json:"pending_questions,omitempty"`
	QuestionResponses   map[string]tools.QuestionResponse   `json:"question_responses,omitempty"`
	Todos               []tools.TodoItem                    `json:"todos,omitempty"`
	Context             api.ContextState                    `json:"context"`
}

// Copy returns a deep copy of the State so graph transitions do not mutate
// previously captured snapshots.
func (s State) Copy() State {
	msgs := make(message.MessageList, len(s.Messages))
	for i, m := range s.Messages {
		msgs[i] = message.CloneMessage(m)
	}

	var pending []tools.PermissionRequest
	if len(s.PendingPermissions) > 0 {
		pending = clonePermissionRequests(s.PendingPermissions)
	}

	var decisions map[string]tools.PermissionDecision
	if len(s.PermissionDecisions) > 0 {
		decisions = maps.Clone(s.PermissionDecisions)
	}

	questions := cloneQuestionRequests(s.PendingQuestions)
	var responses map[string]tools.QuestionResponse
	if s.QuestionResponses != nil {
		responses = make(map[string]tools.QuestionResponse, len(s.QuestionResponses))
		for key, response := range s.QuestionResponses {
			responses[key] = *response.Clone()
		}
	}

	var todos []tools.TodoItem
	if len(s.Todos) > 0 {
		todos = slices.Clone(s.Todos)
	}

	var pendingCompact *CompactInput
	if s.PendingCompaction != nil {
		pendingCompact = &CompactInput{Focus: s.PendingCompaction.Focus}
	}

	return State{
		Messages:            msgs,
		PendingCompaction:   pendingCompact,
		PendingPermissions:  pending,
		PermissionDecisions: decisions,
		PendingQuestions:    questions,
		QuestionResponses:   responses,
		Todos:               todos,
		Context:             s.Context,
	}
}

// IsAutoCompacted reports whether the last message was produced by auto-compaction.
func (s State) IsAutoCompacted() bool {
	if len(s.Messages) > 0 {
		if meta := s.Messages[len(s.Messages)-1].GetMetadata(); meta != nil {
			if trig, ok := meta[MetaCompactionTrigger].(string); ok && trig == "auto" {
				return true
			}
		}
	}
	return false
}

// ShouldAutoCompact reports whether the current conversation state qualifies for
// mid-turn or entry auto-compaction.
func (s State) ShouldAutoCompact() bool {
	if s.HasPendingToolCalls() {
		return false
	}
	if s.Context.TriggerTokens <= 0 || s.Context.UsedTokens < s.Context.TriggerTokens {
		return false
	}
	if len(s.Messages) < 2 {
		return false
	}
	lastMsg := s.Messages[len(s.Messages)-1]
	if SystemKindOf(lastMsg) == SystemKindCompaction {
		return false
	}
	return true
}

// HasLeadingCompact reports whether there is a manual compaction request pending.
func (s State) HasLeadingCompact() bool {
	return s.PendingCompaction != nil
}

// ShouldWakeAgent checks if the latest message warrants waking the agent node.
func (s State) ShouldWakeAgent() bool {
	if len(s.Messages) == 0 {
		return false
	}
	last := s.Messages[len(s.Messages)-1]
	if _, isAssistant := last.(*message.Assistant); isAssistant {
		return false
	}
	return SystemKindOf(last) != SystemKindCommand
}

// LastUserMessageID returns the ID of the latest [*message.User] message, or "" if none.
func (s State) LastUserMessageID() string {
	for _, m := range slices.Backward(s.Messages) {
		if u, ok := m.(*message.User); ok && u != nil {
			return u.GetID()
		}
	}
	return ""
}

// ApplyCompaction atomically replaces messages with the compaction summary message,
// clears pending compaction, updates compaction statistics, and recomputes the context state.
func (s State) ApplyCompaction(summaryMsg message.Message, static StaticContext, limits workspace.ModelLimits) State {
	s.Messages = message.MessageList{summaryMsg}
	s.PendingCompaction = nil
	s.Context.CachedTokens = 0
	s.Context.Compactions++
	s.Context.LastCompacted = time.Now().UTC()
	s.Context = computeContextState(static, s, limits)
	return s
}

// AppendMessage appends a message and recomputes the context state.
func (s State) AppendMessage(msg message.Message, static StaticContext, limits workspace.ModelLimits) State {
	s.Messages = append(s.Messages, msg)
	s.Context = computeContextState(static, s, limits)
	return s
}

// AppendMessages appends multiple messages and recomputes the context state.
func (s State) AppendMessages(msgs message.MessageList, static StaticContext, limits workspace.ModelLimits) State {
	s.Messages = append(s.Messages, msgs...)
	s.Context = computeContextState(static, s, limits)
	return s
}

// HasPendingToolCalls reports whether the latest assistant message in the state
// has one or more unfulfilled tool calls.
func (s State) HasPendingToolCalls() bool {
	_, _, unfulfilled, _ := s.ActiveToolCalls()
	return len(unfulfilled) > 0
}

// ActiveToolCalls locates the latest [*message.Assistant] message with tool calls
// and returns its index, the assistant message, any unfulfilled tool calls, and
// whether a new user message was appended after that assistant turn.
func (s State) ActiveToolCalls() (int, *message.Assistant, []*message.ToolCall, bool) {
	for i, v := range slices.Backward(s.Messages) {
		asst, ok := v.(*message.Assistant)
		if !ok || asst == nil || len(asst.ToolCalls()) == 0 {
			continue
		}

		fulfilled := make(map[string]bool)
		hasUserAfter := false
		for j := i + 1; j < len(s.Messages); j++ {
			switch m := s.Messages[j].(type) {
			case *message.Tool:
				fulfilled[m.ToolCallID] = true
			case *message.User:
				hasUserAfter = true
			}
		}

		var unfulfilled []*message.ToolCall
		for _, tc := range asst.ToolCalls() {
			if tc != nil && !fulfilled[tc.ID] {
				unfulfilled = append(unfulfilled, tc)
			}
		}

		if len(unfulfilled) > 0 {
			return i, asst, unfulfilled, hasUserAfter
		}
		return -1, nil, nil, false
	}
	return -1, nil, nil, false
}

func clonePermissionRequests(in []tools.PermissionRequest) []tools.PermissionRequest {
	if len(in) == 0 {
		return nil
	}
	out := make([]tools.PermissionRequest, len(in))
	for i, r := range in {
		r.GrantPatterns = slices.Clone(r.GrantPatterns)
		out[i] = r
	}
	return out
}

func cloneQuestionRequests(in []tools.QuestionRequest) []tools.QuestionRequest {
	if len(in) == 0 {
		return nil
	}
	out := make([]tools.QuestionRequest, len(in))
	for i := range in {
		out[i] = *in[i].Clone()
	}
	return out
}

// EventType identifies the kind of streaming event emitted during [Harness.Stream]
// or [Harness.Resume].
type EventType string

const (
	// EventChunk is emitted for each streaming [message.AssistantChunk] produced
	// by the language model.
	EventChunk EventType = "chunk"

	// EventMessage is emitted whenever a graph node produces a complete
	// conversation message (such as an [*message.Assistant] turn or a
	// [*message.Tool] execution result).
	EventMessage EventType = "message"

	// EventInterrupted is emitted when execution pauses at [ToolsNode] awaiting
	// user approval for one or more [tools.PermissionRequest] items.
	EventInterrupted EventType = "interrupted"

	// EventCompleted is emitted once the graph finishes execution and the final
	// [*message.Assistant] message has been assembled and checkpointed.
	EventCompleted EventType = "completed"

	// EventCommandStarted is emitted when a command starts executing in the harness.
	EventCommandStarted EventType = "command_started"

	// EventCommandCompleted is emitted when a command finishes executing in the harness.
	EventCommandCompleted EventType = "command_completed"

	// EventTodosUpdated is emitted whenever the todos tool updates the session's todos.
	EventTodosUpdated EventType = "todos_updated"

	// EventCompactionStarted is emitted when context compaction starts.
	EventCompactionStarted EventType = "compaction_started"

	// EventCompactionCompleted is emitted when context compaction finishes.
	EventCompactionCompleted EventType = "compaction_completed"

	// EventContextUpdated is emitted whenever the session's context window state is recomputed.
	EventContextUpdated EventType = "context_updated"

	// EventCacheBroken is emitted when a prompt cache drop is detected between turns.
	EventCacheBroken EventType = "cache_broken"
)

// Event represents a real-time notification yielded by [Harness.Stream] or
// [Harness.Resume].
type Event struct {
	Type                EventType                 `json:"type"`
	Chunk               *message.AssistantChunk   `json:"chunk,omitempty"`
	Message             message.Message           `json:"message,omitempty"`
	Permissions         []tools.PermissionRequest `json:"permissions,omitempty"`
	Questions           []tools.QuestionRequest   `json:"questions,omitempty"`
	CommandStarted      *CommandStarted           `json:"command_started,omitempty"`
	CommandCompleted    *CommandCompleted         `json:"command_completed,omitempty"`
	Todos               []tools.TodoItem          `json:"todos,omitempty"`
	CompactionStarted   *CompactionStarted        `json:"compaction_started,omitempty"`
	CompactionCompleted *CompactionCompleted      `json:"compaction_completed,omitempty"`
	CacheBroken         *CacheBroken              `json:"cache_broken,omitempty"`
	Context             *api.ContextState         `json:"context,omitempty"`
}
