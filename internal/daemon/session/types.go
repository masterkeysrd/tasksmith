package session

import (
	"errors"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/broker"
	"github.com/masterkeysrd/tasksmith/internal/daemon/harness"
	"github.com/masterkeysrd/tasksmith/internal/daemon/journal"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tasks"
	"github.com/masterkeysrd/tasksmith/internal/daemon/timeline"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
)

// Sentinel errors returned by the session package.
var (
	ErrInvalidConfig        = errors.New("session: invalid configuration")
	ErrSessionNotFound      = errors.New("session: session not found")
	ErrEmptySessionID       = errors.New("session: session ID must not be empty")
	ErrNilMessage           = errors.New("session: message must not be nil")
	ErrJournalNotFound      = journal.ErrNotFound
	ErrEntryNotFound        = journal.ErrEntryNotFound
	ErrBlobNotFound         = journal.ErrBlobNotFound
	ErrRevertConflict       = journal.ErrRevertConflict
	ErrNoPendingPermissions = errors.New("session: no pending permissions")
	ErrPermissionNotFound   = errors.New("session: permission request not found")
	ErrInvalidDecision      = errors.New("session: invalid permission decision")
	ErrNoPendingQuestions   = errors.New("session: no pending questions")
	ErrQuestionNotFound     = errors.New("session: question request not found")
	ErrInvalidQuestion      = errors.New("session: invalid question response")
	ErrInvalidSessionID     = errors.New("session: session ID must be a canonical lowercase UUIDv7")
	ErrSessionConflict      = errors.New("session: session already exists with different parameters")
	ErrSessionRunning       = errors.New("session: session already has an active turn")
)

// CreateParams holds optional parameters for creating a new [Session].
//
// A non-empty ID must be a canonical lowercase UUIDv7 and makes creation
// idempotent.
type CreateParams struct {
	ID     string        `json:"id,omitempty"`
	Title  string        `json:"title,omitempty"`
	Config SessionConfig `json:"config"`
}

// UpdateParams holds optional parameters for updating an existing [Session].
type UpdateParams struct {
	Title  string        `json:"title,omitempty"`
	Config SessionConfig `json:"config"`
}

// SendInput provides the input to Session.Send, carrying either a new user message or a permission decision.
type SendInput struct {
	ID       string
	Message  message.Message
	Command  string
	Decision *tools.PermissionDecision
	Question *tools.QuestionResponse
	Compact  *api.CompactRequest
}

type (
	QueueKind           = harness.QueueKind
	QueueItemPayload    = harness.QueueItemPayload
	QueueMessagePayload = harness.QueueMessagePayload
	QueueCommandPayload = harness.QueueCommandPayload
	QueueTaskPayload    = harness.QueueTaskPayload
	QueueItem           = harness.QueueItem
)

const (
	QueueKindMessage = harness.QueueKindMessage
	QueueKindCommand = harness.QueueKindCommand
	QueueKindTask    = harness.QueueKindTask
)

// EventType identifies the kind of real-time notification published by [Manager].
type EventType string

const (
	// EventEntryAdded is published whenever an entry is persisted to the
	// session timeline (e.g. messages, model/agent transitions, permission changes).
	EventEntryAdded EventType = "entry_added"

	// EventStatusChanged is published when a session changes its execution status.
	EventStatusChanged EventType = "status_changed"

	// EventChunk is published for each incremental streaming chunk produced by
	// the assistant during a conversational turn.
	EventChunk EventType = "chunk"

	// EventCompleted is published once the conversational turn completes and the
	// final assistant reply has been persisted.
	EventCompleted EventType = "completed"

	// EventPermissionRequested is published when the harness pauses execution to
	// request user approval for one or more pending tool executions.
	EventPermissionRequested EventType = "permission_requested"
	EventQuestionRequested   EventType = "question_requested"

	// EventError is published when an asynchronous conversational turn fails with an error.
	EventError EventType = "error"

	// EventSessionCreated is published when a new session is created.
	EventSessionCreated EventType = "session_created"

	// EventSessionUpdated is published when an existing session is updated.
	EventSessionUpdated EventType = "session_updated"

	// EventSessionDeleted is published when a session is deleted.
	EventSessionDeleted EventType = "session_deleted"

	// EventTaskUpdated is published when a background task updates in a session.
	EventTaskUpdated EventType = "task_updated"

	// EventCommandStarted is published when a direct command starts executing.
	EventCommandStarted EventType = "command_started"

	// EventQueueUpdated is published when the visible queue of items updates.
	EventQueueUpdated EventType = "queue_updated"

	// EventTodosUpdated is published when the session's todos checklist updates.
	EventTodosUpdated EventType = "todos_updated"

	// EventCompactionStarted is published when context compaction starts.
	EventCompactionStarted EventType = "compaction_started"

	// EventContextUpdated is published when the session's context window state is recomputed.
	EventContextUpdated EventType = "context_updated"
)

// Event is implemented by all real-time session notifications published by [Manager].
type Event interface {
	EventType() string
}

type CreatedEvent struct {
	Workspace string
	Session   *Session
}

func (CreatedEvent) EventType() string { return string(EventSessionCreated) }

type UpdatedEvent struct {
	Workspace string
	Session   *Session
}

func (UpdatedEvent) EventType() string { return string(EventSessionUpdated) }

type DeletedEvent struct {
	Workspace string
	SessionID string
}

func (DeletedEvent) EventType() string { return string(EventSessionDeleted) }

type EntryAddedEvent struct {
	SessionID string
	Entry     timeline.Entry
}

func (EntryAddedEvent) EventType() string { return string(EventEntryAdded) }

type StatusChangedEvent struct {
	SessionID string
	Status    Status
}

func (StatusChangedEvent) EventType() string { return string(EventStatusChanged) }

type ChunkEvent struct {
	SessionID string
	Chunk     message.AssistantChunk
}

func (ChunkEvent) EventType() string { return string(EventChunk) }

type PermissionRequestedEvent struct {
	SessionID   string
	Permissions []tools.PermissionRequest
}

func (PermissionRequestedEvent) EventType() string { return string(EventPermissionRequested) }

type QuestionRequestedEvent struct {
	SessionID string
	Questions []tools.QuestionRequest
}

func (QuestionRequestedEvent) EventType() string { return string(EventQuestionRequested) }

type CompletedEvent struct {
	SessionID string
}

func (CompletedEvent) EventType() string { return string(EventCompleted) }

type TaskUpdatedEvent struct {
	SessionID string
	Task      tasks.Snapshot
}

func (TaskUpdatedEvent) EventType() string { return string(EventTaskUpdated) }

type CommandStartedEvent struct {
	SessionID string
	ID        string
	Command   string
}

func (CommandStartedEvent) EventType() string { return string(EventCommandStarted) }

type QueueUpdatedEvent struct {
	SessionID string
	Queue     []QueueItem
}

func (QueueUpdatedEvent) EventType() string { return string(EventQueueUpdated) }

type ErrorEvent struct {
	SessionID string
	Error     string
}

func (ErrorEvent) EventType() string { return string(EventError) }

type TodosUpdatedEvent struct {
	SessionID string
	Todos     []tools.TodoItem
}

func (TodosUpdatedEvent) EventType() string { return string(EventTodosUpdated) }

type CompactionStartedEvent struct {
	SessionID string
	ID        string
	Trigger   string
	Focus     string
}

func (CompactionStartedEvent) EventType() string { return string(EventCompactionStarted) }

type ContextUpdatedEvent struct {
	SessionID string
	Context   api.ContextState
}

func (ContextUpdatedEvent) EventType() string { return string(EventContextUpdated) }

// Publisher broadcasts real-time session events to a topic.
type Publisher interface {
	Publish(topic string, event broker.Event)
	DeleteTopic(topic string)
}
