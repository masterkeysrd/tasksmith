package harness

import (
	"context"
	"errors"
	"maps"
	"slices"
	"time"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/internal/daemon/std"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
)

// Sentinel errors returned by the harness package.
var (
	ErrInvalidConfig                = errors.New("harness: invalid configuration")
	ErrEmptyThreadID                = errors.New("harness: thread ID must not be empty")
	ErrNilMessage                   = errors.New("harness: input message must not be nil")
	ErrNoPendingPermissions         = errors.New("harness: no pending permissions for thread")
	ErrPendingPermissionsUnresolved = errors.New("harness: all pending permissions must be resolved before resuming")
)

const MetaSystemKind = "tasksmith.system.kind"

const (
	SystemKindCommand = "command"
	SystemKindTask    = "task"
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

type CommandStarted struct {
	ID      string
	Command string
}

type CommandCompleted struct {
	ID         string
	Command    string
	Output     string
	ExitCode   int
	DurationMs int64
}

// Input represents the conversational input to [Harness.Stream].
// It can provide either a new user message or resolved permission decisions.
type Input struct {
	Message   message.Message
	Decisions map[string]tools.PermissionDecision
}

// State holds the checkpointed conversation and permission state for a thread.
// It implements [github.com/masterkeysrd/loom/graph.State].
type State struct {
	Messages            message.MessageList                 `json:"messages"`
	PendingPermissions  []tools.PermissionRequest           `json:"pending_permissions,omitempty"`
	PermissionDecisions map[string]tools.PermissionDecision `json:"permission_decisions,omitempty"`
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

	return State{
		Messages:            msgs,
		PendingPermissions:  pending,
		PermissionDecisions: decisions,
	}
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
)

// Event represents a real-time notification yielded by [Harness.Stream] or
// [Harness.Resume].
type Event struct {
	Type             EventType                 `json:"type"`
	Chunk            *message.AssistantChunk   `json:"chunk,omitempty"`
	Message          message.Message           `json:"message,omitempty"`
	Permissions      []tools.PermissionRequest `json:"permissions,omitempty"`
	CommandStarted   *CommandStarted           `json:"command_started,omitempty"`
	CommandCompleted *CommandCompleted         `json:"command_completed,omitempty"`
}
