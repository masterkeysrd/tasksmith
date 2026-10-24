package api

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// HeaderSubscriberID is the HTTP header carrying the SSE subscriber ID.
const HeaderSubscriberID = "X-TaskSmith-Subscriber-ID"

// TopicSessions is the broker topic for session lifecycle and metadata events.
const TopicSessions = "sessions"

// SessionStreamTopic returns the broker topic for a specific session's turn stream.
func SessionStreamTopic(sessionID string) string {
	return "sessions." + sessionID + ".stream"
}

// SubscriptionRequest contains topics to subscribe to or unsubscribe from.
type SubscriptionRequest struct {
	Topics []string `json:"topics"`
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
	EventChunk               EventType = "chunk"
	EventPermissionRequested EventType = "permission_requested"
	EventCompleted           EventType = "completed"
	EventError               EventType = "error"
)

// EventPayload is implemented by all concrete SSE event payload structs.
// Its EventType method also satisfies broker.Event directly.
type EventPayload interface {
	EventType() string
}

// ConnectedPayload represents the payload for an [EventConnected] event.
type ConnectedPayload struct {
	SubscriberID string   `json:"subscriber_id"`
	Topics       []string `json:"topics,omitempty"`
}

func (ConnectedPayload) EventType() string { return string(EventConnected) }

// SessionCreatedPayload represents the payload for an [EventSessionCreated] event.
type SessionCreatedPayload struct {
	Workspace string  `json:"workspace"`
	Session   Session `json:"session"`
}

func (SessionCreatedPayload) EventType() string { return string(EventSessionCreated) }

// SessionUpdatedPayload represents the payload for an [EventSessionUpdated] event.
type SessionUpdatedPayload struct {
	Workspace string  `json:"workspace"`
	Session   Session `json:"session"`
}

func (SessionUpdatedPayload) EventType() string { return string(EventSessionUpdated) }

// SessionDeletedPayload represents the payload for an [EventSessionDeleted] event.
type SessionDeletedPayload struct {
	Workspace string `json:"workspace"`
	SessionID string `json:"session_id"`
}

func (SessionDeletedPayload) EventType() string { return string(EventSessionDeleted) }

// EntryAddedPayload represents the payload for an [EventEntryAdded] event.
type EntryAddedPayload struct {
	SessionID string        `json:"session_id"`
	Entry     TimelineEntry `json:"entry"`
}

func (EntryAddedPayload) EventType() string { return string(EventEntryAdded) }

// ChunkPayload represents the payload for an [EventChunk] event.
type ChunkPayload struct {
	SessionID string `json:"session_id"`
	Chunk     Chunk  `json:"chunk"`
}

func (ChunkPayload) EventType() string { return string(EventChunk) }

// PermissionRequestedPayload represents the payload for an [EventPermissionRequested] event.
type PermissionRequestedPayload struct {
	SessionID   string              `json:"session_id"`
	Permissions []PermissionRequest `json:"permissions"`
}

func (PermissionRequestedPayload) EventType() string { return string(EventPermissionRequested) }

// CompletedPayload represents the payload for an [EventCompleted] event.
type CompletedPayload struct {
	SessionID string   `json:"session_id"`
	Message   *Message `json:"message,omitempty"`
}

func (CompletedPayload) EventType() string { return string(EventCompleted) }

// ErrorPayload represents the payload for an [EventError] event.
type ErrorPayload struct {
	SessionID string `json:"session_id"`
	Error     string `json:"error"`
}

func (ErrorPayload) EventType() string { return string(EventError) }

// Event is the real-time notification payload streamed over SSE.
type Event struct {
	Type    EventType    `json:"type"`
	Topic   string       `json:"topic,omitempty"`
	Payload EventPayload `json:"payload,omitempty"`
}

// UnmarshalJSON implements [json.Unmarshaler] for [*Event].
// It decodes type, topic, and payload, unmarshaling payload into the concrete
// payload struct matching Type when non-empty and not "null".
func (e *Event) UnmarshalJSON(data []byte) error {
	type wire struct {
		Type    EventType       `json:"type"`
		Topic   string          `json:"topic,omitempty"`
		Payload json.RawMessage `json:"payload,omitempty"`
	}
	var w wire
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	e.Type = w.Type
	e.Topic = w.Topic
	e.Payload = nil

	raw := bytes.TrimSpace(w.Payload)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil
	}

	switch w.Type {
	case EventConnected:
		var p ConnectedPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return fmt.Errorf("api: unmarshal connected payload: %w", err)
		}
		e.Payload = p
	case EventSessionCreated:
		var p SessionCreatedPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return fmt.Errorf("api: unmarshal session_created payload: %w", err)
		}
		e.Payload = p
	case EventSessionUpdated:
		var p SessionUpdatedPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return fmt.Errorf("api: unmarshal session_updated payload: %w", err)
		}
		e.Payload = p
	case EventSessionDeleted:
		var p SessionDeletedPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return fmt.Errorf("api: unmarshal session_deleted payload: %w", err)
		}
		e.Payload = p
	case EventEntryAdded:
		var p EntryAddedPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return fmt.Errorf("api: unmarshal entry_added payload: %w", err)
		}
		e.Payload = p
	case EventChunk:
		var p ChunkPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return fmt.Errorf("api: unmarshal chunk payload: %w", err)
		}
		e.Payload = p
	case EventPermissionRequested:
		var p PermissionRequestedPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return fmt.Errorf("api: unmarshal permission_requested payload: %w", err)
		}
		e.Payload = p
	case EventCompleted:
		var p CompletedPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return fmt.Errorf("api: unmarshal completed payload: %w", err)
		}
		e.Payload = p
	case EventError:
		var p ErrorPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return fmt.Errorf("api: unmarshal error payload: %w", err)
		}
		e.Payload = p
	default:
		return fmt.Errorf("api: unknown event type %q", w.Type)
	}

	return nil
}
