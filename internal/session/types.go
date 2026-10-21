package session

import (
	"context"
	"errors"
	"time"

	"github.com/masterkeysrd/loom/message"
)

// Sentinel errors returned by the session package.
var (
	ErrInvalidConfig   = errors.New("session: invalid configuration")
	ErrSessionNotFound = errors.New("session: session not found")
	ErrEmptySessionID  = errors.New("session: session ID must not be empty")
	ErrNilMessage      = errors.New("session: message must not be nil")
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
type CreateParams struct {
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
)

// Event represents a real-time session notification delivered via [Publisher].
type Event struct {
	Type      EventType               `json:"type"`
	SessionID string                  `json:"session_id"`
	Message   message.Message         `json:"message,omitempty"`
	Chunk     *message.AssistantChunk `json:"chunk,omitempty"`
}

// Publisher broadcasts real-time session events to downstream subscribers or UI layers.
type Publisher interface {
	Publish(ctx context.Context, event Event)
}
