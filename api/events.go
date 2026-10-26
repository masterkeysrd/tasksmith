package api

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// HeaderSubscriberID is the HTTP header carrying the SSE subscriber ID.
const HeaderSubscriberID = "X-TaskSmith-Subscriber-ID"

// ErrCodeNeedSync is the API error code returned when a subscriber's cursor is out of date.
const ErrCodeNeedSync = "need_sync"

// TopicSessions is the broker topic for session lifecycle and metadata events.
const TopicSessions = "sessions"

// SessionStreamTopic returns the broker topic for a specific session's turn stream.
func SessionStreamTopic(sessionID string) string {
	return "sessions." + sessionID + ".stream"
}

// SubscriptionRequest contains topics to subscribe to or unsubscribe from,
// and an optional cursor for replaying missed events.
type SubscriptionRequest struct {
	Topics []string `json:"topics"`
	Since  string   `json:"since,omitempty"`
}

// SubscriptionResponse reports the active set of subscribed topics.
type SubscriptionResponse struct {
	Topics []string `json:"topics"`
}

// EventType identifies the kind of real-time Server-Sent Event emitted by the daemon.
type EventType string

const (
	EventConnected           EventType = "connected"
	EventSessionCreated      EventType = "session_created"
	EventSessionUpdated      EventType = "session_updated"
	EventSessionDeleted      EventType = "session_deleted"
	EventEntryAdded          EventType = "entry_added"
	EventStatusChanged       EventType = "status_changed"
	EventChunk               EventType = "chunk"
	EventPermissionRequested EventType = "permission_requested"
	EventCompleted           EventType = "completed"
	EventTaskUpdated         EventType = "task_updated"
	EventError               EventType = "error"
)

// EventPayload is implemented by all concrete SSE event payload structs.
// Its EventType method also satisfies broker.Event directly.
type EventPayload interface {
	EventType() string
}

// ConnectedEvent represents the payload for an [EventConnected] event.
type ConnectedEvent struct {
	SubscriberID string   `json:"subscriber_id"`
	Topics       []string `json:"topics,omitempty"`
}

func (ConnectedEvent) EventType() string { return string(EventConnected) }

// SessionCreatedEvent represents the payload for an [EventSessionCreated] event.
type SessionCreatedEvent struct {
	Workspace string  `json:"workspace"`
	Session   Session `json:"session"`
}

func (SessionCreatedEvent) EventType() string { return string(EventSessionCreated) }

// SessionUpdatedEvent represents the payload for an [EventSessionUpdated] event.
type SessionUpdatedEvent struct {
	Workspace string  `json:"workspace"`
	Session   Session `json:"session"`
}

func (SessionUpdatedEvent) EventType() string { return string(EventSessionUpdated) }

// SessionDeletedEvent represents the payload for an [EventSessionDeleted] event.
type SessionDeletedEvent struct {
	Workspace string `json:"workspace"`
	SessionID string `json:"session_id"`
}

func (SessionDeletedEvent) EventType() string { return string(EventSessionDeleted) }

// EntryAddedEvent represents the payload for an [EventEntryAdded] event.
type EntryAddedEvent struct {
	SessionID string        `json:"session_id"`
	Entry     TimelineEntry `json:"entry"`
}

func (EntryAddedEvent) EventType() string { return string(EventEntryAdded) }

// StatusChangedEvent represents the payload for an [EventStatusChanged] event.
type StatusChangedEvent struct {
	SessionID string        `json:"session_id"`
	Status    SessionStatus `json:"status"`
}

func (StatusChangedEvent) EventType() string { return string(EventStatusChanged) }

// ChunkEvent represents the payload for an [EventChunk] event.
type ChunkEvent struct {
	SessionID string `json:"session_id"`
	Chunk     Chunk  `json:"chunk"`
}

func (ChunkEvent) EventType() string { return string(EventChunk) }

// PermissionRequestedEvent represents the payload for an [EventPermissionRequested] event.
type PermissionRequestedEvent struct {
	SessionID   string              `json:"session_id"`
	Permissions []PermissionRequest `json:"permissions"`
}

func (PermissionRequestedEvent) EventType() string { return string(EventPermissionRequested) }

// CompletedEvent represents the payload for an [EventCompleted] event.
type CompletedEvent struct {
	SessionID string `json:"session_id"`
}

func (CompletedEvent) EventType() string { return string(EventCompleted) }

// TaskUpdatedEvent represents the payload for an [EventTaskUpdated] event.
type TaskUpdatedEvent struct {
	SessionID string `json:"session_id"`
	Task      Task   `json:"task"`
}

func (TaskUpdatedEvent) EventType() string { return string(EventTaskUpdated) }

// ErrorEvent represents the payload for an [EventError] event.
type ErrorEvent struct {
	SessionID string `json:"session_id"`
	Error     string `json:"error"`
}

func (ErrorEvent) EventType() string { return string(EventError) }

// Event is the real-time notification payload streamed over SSE.
type Event struct {
	ID      string       `json:"id,omitempty"`
	Type    EventType    `json:"type"`
	Topic   string       `json:"topic,omitempty"`
	Payload EventPayload `json:"payload,omitempty"`
}

// UnmarshalJSON implements [json.Unmarshaler] for [*Event].
// It decodes id, type, topic, and payload, unmarshaling payload into the concrete
// payload struct matching Type when non-empty and not "null".
func (e *Event) UnmarshalJSON(data []byte) error {
	type wire struct {
		ID      string          `json:"id,omitempty"`
		Type    EventType       `json:"type"`
		Topic   string          `json:"topic,omitempty"`
		Payload json.RawMessage `json:"payload,omitempty"`
	}
	var w wire
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	e.ID = w.ID
	e.Type = w.Type
	e.Topic = w.Topic
	e.Payload = nil

	raw := bytes.TrimSpace(w.Payload)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil
	}

	switch w.Type {
	case EventConnected:
		var p ConnectedEvent
		if err := json.Unmarshal(raw, &p); err != nil {
			return fmt.Errorf("api: unmarshal connected payload: %w", err)
		}
		e.Payload = p
	case EventSessionCreated:
		var p SessionCreatedEvent
		if err := json.Unmarshal(raw, &p); err != nil {
			return fmt.Errorf("api: unmarshal session_created payload: %w", err)
		}
		e.Payload = p
	case EventSessionUpdated:
		var p SessionUpdatedEvent
		if err := json.Unmarshal(raw, &p); err != nil {
			return fmt.Errorf("api: unmarshal session_updated payload: %w", err)
		}
		e.Payload = p
	case EventSessionDeleted:
		var p SessionDeletedEvent
		if err := json.Unmarshal(raw, &p); err != nil {
			return fmt.Errorf("api: unmarshal session_deleted payload: %w", err)
		}
		e.Payload = p
	case EventEntryAdded:
		var p EntryAddedEvent
		if err := json.Unmarshal(raw, &p); err != nil {
			return fmt.Errorf("api: unmarshal entry_added payload: %w", err)
		}
		e.Payload = p
	case EventStatusChanged:
		var p StatusChangedEvent
		if err := json.Unmarshal(raw, &p); err != nil {
			return fmt.Errorf("api: unmarshal status_changed payload: %w", err)
		}
		e.Payload = p
	case EventChunk:
		var p ChunkEvent
		if err := json.Unmarshal(raw, &p); err != nil {
			return fmt.Errorf("api: unmarshal chunk payload: %w", err)
		}
		e.Payload = p
	case EventPermissionRequested:
		var p PermissionRequestedEvent
		if err := json.Unmarshal(raw, &p); err != nil {
			return fmt.Errorf("api: unmarshal permission_requested payload: %w", err)
		}
		e.Payload = p
	case EventCompleted:
		var p CompletedEvent
		if err := json.Unmarshal(raw, &p); err != nil {
			return fmt.Errorf("api: unmarshal completed payload: %w", err)
		}
		e.Payload = p
	case EventTaskUpdated:
		var p TaskUpdatedEvent
		if err := json.Unmarshal(raw, &p); err != nil {
			return fmt.Errorf("api: unmarshal task_updated payload: %w", err)
		}
		e.Payload = p
	case EventError:
		var p ErrorEvent
		if err := json.Unmarshal(raw, &p); err != nil {
			return fmt.Errorf("api: unmarshal error payload: %w", err)
		}
		e.Payload = p
	default:
		return fmt.Errorf("api: unknown event type %q", w.Type)
	}

	return nil
}
