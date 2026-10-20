package harness

import (
	"errors"

	"github.com/masterkeysrd/loom/message"
)

// Sentinel errors returned by the harness package.
var (
	ErrInvalidConfig = errors.New("harness: invalid configuration")
	ErrEmptyThreadID = errors.New("harness: thread ID must not be empty")
	ErrNilMessage    = errors.New("harness: input message must not be nil")
)

// State holds the checkpointed conversation state for a thread.
// It implements [github.com/masterkeysrd/loom/graph.State].
type State struct {
	Messages message.MessageList `json:"messages"`
}

// Copy returns a deep copy of the State so graph transitions do not mutate
// previously captured snapshots.
func (s State) Copy() State {
	msgs := make(message.MessageList, len(s.Messages))
	for i, m := range s.Messages {
		msgs[i] = message.CloneMessage(m)
	}
	return State{Messages: msgs}
}

// EventType identifies the kind of streaming event emitted during [Harness.Stream].
type EventType string

const (
	// EventChunk is emitted for each streaming [message.AssistantChunk] produced
	// by the language model.
	EventChunk EventType = "chunk"

	// EventCompleted is emitted once the graph finishes execution and the final
	// [*message.Assistant] message has been assembled and checkpointed.
	EventCompleted EventType = "completed"
)

// Event represents a real-time notification yielded by [Harness.Stream].
type Event struct {
	Type    EventType
	Chunk   *message.AssistantChunk // Populated when Type == EventChunk
	Message *message.Assistant      // Populated when Type == EventCompleted
}
