package session

import (
	"context"
	"errors"
	"time"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/internal/std"
	"github.com/masterkeysrd/tasksmith/internal/tools"
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
)

// Session represents a persistent user-facing chat thread.
type Session struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Agent     string    `json:"agent"`
	Model     string    `json:"model"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
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
	// EventMessageAdded is published whenever a message is persisted to the
	// session's UI message history.
	EventMessageAdded EventType = "message_added"

	// EventChunk is published for each incremental streaming chunk produced by
	// the assistant during a conversational turn.
	EventChunk EventType = "chunk"

	// EventCompleted is published once the conversational turn completes and the
	// final assistant reply has been persisted.
	EventCompleted EventType = "completed"

	// EventPermissionRequested is published when the harness pauses execution to
	// request user approval for one or more pending tool executions.
	EventPermissionRequested EventType = "permission_requested"
)

// Event represents a real-time session notification delivered via [Publisher].
type Event struct {
	Type        EventType                 `json:"type"`
	SessionID   string                    `json:"session_id"`
	Message     message.Message           `json:"message,omitempty"`
	Chunk       *message.AssistantChunk   `json:"chunk,omitempty"`
	Permissions []tools.PermissionRequest `json:"permissions,omitempty"`
}

// Publisher broadcasts real-time session events to downstream subscribers or UI layers.
type Publisher interface {
	Publish(ctx context.Context, event Event)
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
