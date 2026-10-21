package harness

import (
	"errors"
	"maps"
	"slices"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/internal/tools"
)

// Sentinel errors returned by the harness package.
var (
	ErrInvalidConfig                = errors.New("harness: invalid configuration")
	ErrEmptyThreadID                = errors.New("harness: thread ID must not be empty")
	ErrNilMessage                   = errors.New("harness: input message must not be nil")
	ErrNoPendingPermissions         = errors.New("harness: no pending permissions for thread")
	ErrPendingPermissionsUnresolved = errors.New("harness: all pending permissions must be resolved before resuming")
)

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
)

// Event represents a real-time notification yielded by [Harness.Stream] or
// [Harness.Resume].
type Event struct {
	Type        EventType                 `json:"type"`
	Chunk       *message.AssistantChunk   `json:"chunk,omitempty"`
	Message     message.Message           `json:"message,omitempty"`
	Permissions []tools.PermissionRequest `json:"permissions,omitempty"`
}
