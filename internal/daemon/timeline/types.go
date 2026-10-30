package timeline

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
)

// Sentinel errors returned by the timeline package.
var (
	ErrInvalidConfig  = errors.New("timeline: invalid configuration")
	ErrEmptySessionID = errors.New("timeline: session ID must not be empty")
	ErrInvalidEntry   = errors.New("timeline: invalid entry")
	ErrEntryNotFound  = errors.New("timeline: entry not found")
)

// ModeChange captures a transition of the session permission mode.
type ModeChange struct {
	From tools.PermissionMode `json:"from,omitempty"`
	To   tools.PermissionMode `json:"to"`
}

// PermissionRecord captures a requested permission and its resolution decision.
type PermissionRecord struct {
	Request  tools.PermissionRequest  `json:"request"`
	Decision tools.PermissionDecision `json:"decision"`
}

type QuestionRecord struct {
	Request  tools.QuestionRequest  `json:"request"`
	Response tools.QuestionResponse `json:"response"`
}

// Entry is a single append-only timeline event in a session log.
type Entry struct {
	ID        string           `json:"id"`
	SessionID string           `json:"session_id"`
	Kind      api.TimelineKind `json:"kind"`
	CreatedAt time.Time        `json:"created_at"`

	Message       message.Message          `json:"message,omitempty"`
	Command       *api.CommandRecord       `json:"command,omitempty"`
	Model         *api.ModelChange         `json:"model,omitempty"`
	Effort        *api.EffortChange        `json:"effort,omitempty"`
	Thinking      *api.ThinkingChange      `json:"thinking,omitempty"`
	Agent         *api.AgentChange         `json:"agent,omitempty"`
	Mode          *ModeChange              `json:"mode,omitempty"`
	Permission    *PermissionRecord        `json:"permission,omitempty"`
	Question      *QuestionRecord          `json:"question,omitempty"`
	Compacted     *api.CompactedRecord     `json:"compacted,omitempty"`
	CacheBroken   *api.CacheBrokenRecord   `json:"cache_broken,omitempty"`
	TurnCompleted *api.TurnCompletedRecord `json:"turn_completed,omitempty"`
}

// Clone returns a deep copy of the timeline entry.
func (e Entry) Clone() Entry {
	cp := e
	if e.Message != nil {
		cp.Message = message.CloneMessage(e.Message)
	}
	if e.Command != nil {
		c := *e.Command
		cp.Command = &c
	}
	if e.Model != nil {
		m := *e.Model
		cp.Model = &m
	}
	if e.Effort != nil {
		eff := *e.Effort
		cp.Effort = &eff
	}
	if e.Thinking != nil {
		change := *e.Thinking
		if change.From != nil {
			value := *change.From
			change.From = &value
		}
		if change.To != nil {
			value := *change.To
			change.To = &value
		}
		cp.Thinking = &change
	}
	if e.Agent != nil {
		ag := *e.Agent
		cp.Agent = &ag
	}
	if e.Mode != nil {
		md := *e.Mode
		cp.Mode = &md
	}
	if e.Permission != nil {
		perm := *e.Permission
		perm.Request.GrantPatterns = slices.Clone(e.Permission.Request.GrantPatterns)
		cp.Permission = &perm
	}
	if e.Question != nil {
		cp.Question = &QuestionRecord{Request: *e.Question.Request.Clone(), Response: *e.Question.Response.Clone()}
	}
	if e.Compacted != nil {
		comp := *e.Compacted
		cp.Compacted = &comp
	}
	if e.CacheBroken != nil {
		cb := *e.CacheBroken
		cp.CacheBroken = &cb
	}
	if e.TurnCompleted != nil {
		tc := *e.TurnCompleted
		cp.TurnCompleted = &tc
	}
	return cp
}

// MarshalJSON implements json.Marshaler for Entry to ensure polymorphic Message payloads
// include the role discriminator required by loom/message.
func (e Entry) MarshalJSON() ([]byte, error) {
	if e.Kind == api.TimelineKindMessage && e.Message != nil {
		msgData, err := json.Marshal(message.MessageList{e.Message})
		if err != nil {
			return nil, fmt.Errorf("timeline: marshal message: %w", err)
		}
		trimmed := bytes.TrimSpace(msgData)
		if bytes.HasPrefix(trimmed, []byte("[")) && bytes.HasSuffix(trimmed, []byte("]")) {
			trimmed = bytes.TrimSpace(trimmed[1 : len(trimmed)-1])
		}
		type entryAlias Entry
		var aux struct {
			entryAlias
			Message json.RawMessage `json:"message,omitempty"`
		}
		aux.entryAlias = entryAlias(e)
		aux.Message = trimmed
		return json.Marshal(aux)
	}

	type noCustom Entry
	return json.Marshal(noCustom(e))
}

// UnmarshalJSON implements json.Unmarshaler for Entry to reconstruct polymorphic Message payloads.
func (e *Entry) UnmarshalJSON(data []byte) error {
	type entryAlias Entry
	var aux struct {
		entryAlias
		RawMessage json.RawMessage `json:"message,omitempty"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*e = Entry(aux.entryAlias)
	if len(aux.RawMessage) > 0 && string(bytes.TrimSpace(aux.RawMessage)) != "null" {
		raw := bytes.TrimSpace(aux.RawMessage)
		var list message.MessageList
		if bytes.HasPrefix(raw, []byte("[")) {
			if err := json.Unmarshal(raw, &list); err != nil {
				return fmt.Errorf("timeline: unmarshal message: %w", err)
			}
		} else {
			wrapped := append(append([]byte("["), raw...), ']')
			if err := json.Unmarshal(wrapped, &list); err != nil {
				return fmt.Errorf("timeline: unmarshal message: %w", err)
			}
		}
		if len(list) == 1 && list[0] != nil {
			e.Message = list[0]
		}
	}
	return nil
}

// NewMessageEntry creates an Entry initialized with a message.Message payload.
func NewMessageEntry(sessionID string, msg message.Message) *Entry {
	var id string
	if msg != nil {
		id = msg.GetID()
	}
	return &Entry{
		ID:        id,
		SessionID: sessionID,
		Kind:      api.TimelineKindMessage,
		Message:   msg,
	}
}

// NewModelEntry creates an Entry initialized with a ModelChange payload.
func NewModelEntry(sessionID string, change api.ModelChange) *Entry {
	c := change
	return &Entry{
		SessionID: sessionID,
		Kind:      api.TimelineKindModel,
		Model:     &c,
	}
}

// NewEffortEntry creates an Entry initialized with an EffortChange payload.
func NewEffortEntry(sessionID string, change api.EffortChange) *Entry {
	c := change
	return &Entry{
		SessionID: sessionID,
		Kind:      api.TimelineKindEffort,
		Effort:    &c,
	}
}

// NewAgentEntry creates an Entry initialized with an AgentChange payload.
func NewAgentEntry(sessionID string, change api.AgentChange) *Entry {
	c := change
	return &Entry{
		SessionID: sessionID,
		Kind:      api.TimelineKindAgent,
		Agent:     &c,
	}
}

// NewModeEntry creates an Entry initialized with a ModeChange payload.
func NewModeEntry(sessionID string, change ModeChange) *Entry {
	c := change
	return &Entry{
		SessionID: sessionID,
		Kind:      api.TimelineKindMode,
		Mode:      &c,
	}
}

// NewPermissionEntry creates an Entry initialized with a PermissionRecord payload.
func NewPermissionEntry(sessionID string, record PermissionRecord) *Entry {
	r := record
	return &Entry{
		SessionID:  sessionID,
		Kind:       api.TimelineKindPermission,
		Permission: &r,
	}
}

func NewQuestionEntry(sessionID string, record QuestionRecord) *Entry {
	return &Entry{
		SessionID: sessionID,
		Kind:      api.TimelineKindQuestion,
		Question:  &record,
	}
}

// NewCommandEntry creates an Entry initialized with a CommandRecord payload.
func NewCommandEntry(sessionID string, cmd api.CommandRecord) *Entry {
	c := cmd
	return &Entry{
		SessionID: sessionID,
		Kind:      api.TimelineKindCommand,
		Command:   &c,
	}
}

// NewInterruptedEntry creates an Entry recording that a turn was interrupted/canceled by the user.
func NewInterruptedEntry(sessionID string) *Entry {
	return &Entry{
		SessionID: sessionID,
		Kind:      api.TimelineKindInterrupted,
	}
}

// NewCompactedEntry creates an Entry initialized with a CompactedRecord payload.
func NewCompactedEntry(sessionID string, rec api.CompactedRecord) *Entry {
	r := rec
	return &Entry{
		SessionID: sessionID,
		Kind:      api.TimelineKindCompacted,
		Compacted: &r,
	}
}

// NewCacheBrokenEntry creates an Entry initialized with a CacheBrokenRecord payload.
func NewCacheBrokenEntry(sessionID string, rec api.CacheBrokenRecord) *Entry {
	r := rec
	return &Entry{
		SessionID:   sessionID,
		Kind:        api.TimelineKindCacheBroken,
		CacheBroken: &r,
	}
}

// NewTurnCompletedEntry creates an Entry initialized with a TurnCompletedRecord payload.
func NewTurnCompletedEntry(sessionID string, rec api.TurnCompletedRecord) *Entry {
	r := rec
	return &Entry{
		SessionID:     sessionID,
		Kind:          api.TimelineKindTurnCompleted,
		TurnCompleted: &r,
	}
}
