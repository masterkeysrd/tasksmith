package session

import (
	"errors"
	"time"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/internal/daemon/broker"
	"github.com/masterkeysrd/tasksmith/internal/daemon/harness"
	"github.com/masterkeysrd/tasksmith/internal/daemon/std"
	"github.com/masterkeysrd/tasksmith/internal/daemon/timeline"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
)

// Sentinel errors returned by the session package.
var (
	ErrInvalidConfig        = errors.New("session: invalid configuration")
	ErrSessionNotFound      = errors.New("session: session not found")
	ErrEmptySessionID       = errors.New("session: session ID must not be empty")
	ErrNilMessage           = errors.New("session: message must not be nil")
	ErrJournalNotFound      = errors.New("session: file change journal not found")
	ErrEntryNotFound        = errors.New("session: journal entry not found")
	ErrBlobNotFound         = errors.New("session: file snapshot blob not found")
	ErrRevertConflict       = errors.New("session: revert conflict with external changes")
	ErrNoPendingPermissions = errors.New("session: no pending permissions")
	ErrPermissionNotFound   = errors.New("session: permission request not found")
	ErrInvalidDecision      = errors.New("session: invalid permission decision")
	ErrInvalidSessionID     = errors.New("session: session ID must be a canonical lowercase UUIDv7")
	ErrSessionConflict      = errors.New("session: session already exists with different parameters")
	ErrSessionRunning       = errors.New("session: session already has an active turn")
)

// Status represents the operational execution state of a [Session].
type Status string

const (
	StatusIdle              Status = "idle"
	StatusRunning           Status = "running"
	StatusWaitingPermission Status = "waiting_permission"
)

// Session represents a persistent user-facing chat thread and its runtime state.
type Session struct {
	// We embed [State] here so that Session supports state fields and locking
	// directly, but marshal it as a struct in JSON.
	//
	// State also provides a [sync.Mutex] which is used as lock for both
	// the Session and State.
	*State `json:"state"`

	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Agent     string    `json:"agent"`
	Model     string    `json:"model"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	Root    string           `json:"-"` // Path to <WorkspaceDir>/sessions/<id>
	Tracker *FileTracker     `json:"-"`
	Tools   *tools.Container `json:"-"`
	Harness *harness.Harness `json:"-"`
}

// Clone returns a deep copy of the session and its exported execution state,
// synchronizing on the session's embedded mutex.
func (s *Session) Clone() *Session {
	if s == nil {
		return nil
	}
	if s.State != nil {
		s.Lock()
		defer s.Unlock()
	}
	cp := &Session{
		ID:        s.ID,
		Title:     s.Title,
		Agent:     s.Agent,
		Model:     s.Model,
		CreatedAt: s.CreatedAt,
		UpdatedAt: s.UpdatedAt,
		Root:      s.Root,
		Tracker:   s.Tracker,
		Tools:     s.Tools,
		Harness:   s.Harness,
	}
	if s.State != nil {
		cp.State = s.State.Clone()
	} else {
		cp.State = NewState()
	}
	return cp
}

// CreateParams holds optional parameters for creating a new [Session].
//
// A non-empty ID must be a canonical lowercase UUIDv7 and makes creation
// idempotent; see [Manager.Ensure].
type CreateParams struct {
	ID    string `json:"id,omitempty"`
	Title string `json:"title,omitempty"`
	Agent string `json:"agent,omitempty"`
	Model string `json:"model,omitempty"`
}

// UpdateParams holds optional parameters for updating an existing [Session].
type UpdateParams struct {
	Title string `json:"title,omitempty"`
	Agent string `json:"agent,omitempty"`
	Model string `json:"model,omitempty"`
}

// EventType identifies the kind of real-time notification published by [Manager].
type EventType string

const (
	// EventEntryAdded is published whenever an entry is persisted to the
	// session timeline (e.g. messages, model/agent transitions, permission changes).
	EventEntryAdded EventType = "entry_added"

	// EventChunk is published for each incremental streaming chunk produced by
	// the assistant during a conversational turn.
	EventChunk EventType = "chunk"

	// EventCompleted is published once the conversational turn completes and the
	// final assistant reply has been persisted.
	EventCompleted EventType = "completed"

	// EventPermissionRequested is published when the harness pauses execution to
	// request user approval for one or more pending tool executions.
	EventPermissionRequested EventType = "permission_requested"

	// EventError is published when an asynchronous conversational turn fails with an error.
	EventError EventType = "error"
)

// Event is implemented by all real-time session notifications published by [Manager].
type Event interface {
	EventType() string
}

type EntryAddedEvent struct {
	SessionID string
	Entry     timeline.Entry
}

func (EntryAddedEvent) EventType() string { return string(EventEntryAdded) }

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

type CompletedEvent struct {
	SessionID string
}

func (CompletedEvent) EventType() string { return string(EventCompleted) }

type ErrorEvent struct {
	SessionID string
	Error     string
}

func (ErrorEvent) EventType() string { return string(EventError) }

// Publisher broadcasts real-time session events to a topic.
type Publisher interface {
	Publish(topic string, event broker.Event)
}

// JournalHeader is stored as Line 1 of each `<path_hash>.jsonl` file inside a
// session's `changes/` directory, recording the canonical workspace-relative path
// and its initial baseline blob hash.
type JournalHeader struct {
	Path     string `json:"path"`
	Baseline string `json:"baseline,omitempty"`
	Binary   bool   `json:"binary,omitempty"`
}

// JournalEntry is stored on Lines 2..N of `<path_hash>.jsonl`, recording a compact
// mutation event without repeating the file path or storing inline diffs.
type JournalEntry struct {
	ID        string           `json:"id"`
	Timestamp time.Time        `json:"ts"`
	Source    std.ChangeSource `json:"source"`
	Tool      string           `json:"tool,omitempty"`
	Kind      std.ChangeKind   `json:"kind"`
	Before    string           `json:"before,omitempty"`
	After     string           `json:"after,omitempty"`
	Additions int              `json:"add,omitempty"`
	Deletions int              `json:"del,omitempty"`
}
