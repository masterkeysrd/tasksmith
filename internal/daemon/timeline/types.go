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

// Kind classifies the type of timeline event.
type Kind string

const (
	KindMessage       Kind = "message"
	KindCommand       Kind = "command"
	KindModel         Kind = "model"
	KindEffort        Kind = "effort"
	KindThinking      Kind = "thinking"
	KindAgent         Kind = "agent"
	KindMode          Kind = "mode"
	KindPermission    Kind = "permission"
	KindQuestion      Kind = "question"
	KindInterrupted   Kind = "interrupted"
	KindCompacted     Kind = "compacted"
	KindCacheBroken   Kind = "cache_broken"
	KindTurnCompleted Kind = "turn_completed"
)

// CommandRecord captures a direct user shell command (!) and its output.
type CommandRecord struct {
	Command    string `json:"command"`
	Output     string `json:"output,omitempty"`
	ExitCode   int    `json:"exit_code"`
	DurationMs int64  `json:"duration_ms,omitempty"`
}

// ModelChange captures a transition of the active LLM / model.
type ModelChange struct {
	From   string `json:"from,omitempty"`
	To     string `json:"to"`
	Effort string `json:"effort,omitempty"`
}

// EffortChange captures a change in reasoning effort level.
type EffortChange struct {
	From string `json:"from,omitempty"`
	To   string `json:"to"`
}

// AgentChange captures a change of the active agent persona or role.
type AgentChange struct {
	From string `json:"from,omitempty"`
	To   string `json:"to"`
}

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

// CompactedRecord captures a context compaction event in the session timeline.
type CompactedRecord struct {
	Trigger      string `json:"trigger,omitempty"`
	Focus        string `json:"focus,omitempty"`
	Summary      string `json:"summary,omitempty"`
	BeforeTokens int    `json:"before_tokens,omitempty"`
	AfterTokens  int    `json:"after_tokens,omitempty"`
	DurationMs   int64  `json:"duration_ms,omitempty"`
}

// CacheBrokenRecord captures a prompt cache drop between turns in the session timeline.
type CacheBrokenRecord struct {
	PrevCached  int `json:"prev_cached"`
	CurrCached  int `json:"curr_cached"`
	InputTokens int `json:"input_tokens,omitempty"`
}

// TurnCompletedRecord summarizes a finished turn: the span from a user message
// until the agent's final answer (or until the turn was canceled or failed).
type TurnCompletedRecord struct {
	TurnID       string `json:"turn_id"`
	Status       string `json:"status"` // "done", "canceled" or "failed"
	ElapsedMs    int64  `json:"elapsed_ms"`
	InputTokens  int64  `json:"input_tokens,omitempty"`
	OutputTokens int64  `json:"output_tokens,omitempty"`
	CostNano     int64  `json:"cost_nano,omitempty"`
	// Unpriced reports that some calls of the turn had no pricing, so
	// CostNano is a lower bound.
	Unpriced bool `json:"unpriced,omitempty"`
}

// Entry is a single append-only timeline event in a session log.
type Entry struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	Kind      Kind      `json:"kind"`
	CreatedAt time.Time `json:"created_at"`

	Message       message.Message      `json:"message,omitempty"`
	Command       *CommandRecord       `json:"command,omitempty"`
	Model         *ModelChange         `json:"model,omitempty"`
	Effort        *EffortChange        `json:"effort,omitempty"`
	Thinking      *api.ThinkingChange  `json:"thinking,omitempty"`
	Agent         *AgentChange         `json:"agent,omitempty"`
	Mode          *ModeChange          `json:"mode,omitempty"`
	Permission    *PermissionRecord    `json:"permission,omitempty"`
	Question      *QuestionRecord      `json:"question,omitempty"`
	Compacted     *CompactedRecord     `json:"compacted,omitempty"`
	CacheBroken   *CacheBrokenRecord   `json:"cache_broken,omitempty"`
	TurnCompleted *TurnCompletedRecord `json:"turn_completed,omitempty"`
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
	if e.Kind == KindMessage && e.Message != nil {
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
		Kind:      KindMessage,
		Message:   msg,
	}
}

// NewModelEntry creates an Entry initialized with a ModelChange payload.
func NewModelEntry(sessionID string, change ModelChange) *Entry {
	c := change
	return &Entry{
		SessionID: sessionID,
		Kind:      KindModel,
		Model:     &c,
	}
}

// NewEffortEntry creates an Entry initialized with an EffortChange payload.
func NewEffortEntry(sessionID string, change EffortChange) *Entry {
	c := change
	return &Entry{
		SessionID: sessionID,
		Kind:      KindEffort,
		Effort:    &c,
	}
}

// NewAgentEntry creates an Entry initialized with an AgentChange payload.
func NewAgentEntry(sessionID string, change AgentChange) *Entry {
	c := change
	return &Entry{
		SessionID: sessionID,
		Kind:      KindAgent,
		Agent:     &c,
	}
}

// NewModeEntry creates an Entry initialized with a ModeChange payload.
func NewModeEntry(sessionID string, change ModeChange) *Entry {
	c := change
	return &Entry{
		SessionID: sessionID,
		Kind:      KindMode,
		Mode:      &c,
	}
}

// NewPermissionEntry creates an Entry initialized with a PermissionRecord payload.
func NewPermissionEntry(sessionID string, record PermissionRecord) *Entry {
	r := record
	return &Entry{
		SessionID:  sessionID,
		Kind:       KindPermission,
		Permission: &r,
	}
}

func NewQuestionEntry(sessionID string, record QuestionRecord) *Entry {
	return &Entry{SessionID: sessionID, Kind: KindQuestion, Question: &record}
}

// NewCommandEntry creates an Entry initialized with a CommandRecord payload.
func NewCommandEntry(sessionID string, cmd CommandRecord) *Entry {
	c := cmd
	return &Entry{
		SessionID: sessionID,
		Kind:      KindCommand,
		Command:   &c,
	}
}

// NewInterruptedEntry creates an Entry recording that a turn was interrupted/canceled by the user.
func NewInterruptedEntry(sessionID string) *Entry {
	return &Entry{
		SessionID: sessionID,
		Kind:      KindInterrupted,
	}
}

// NewCompactedEntry creates an Entry initialized with a CompactedRecord payload.
func NewCompactedEntry(sessionID string, rec CompactedRecord) *Entry {
	r := rec
	return &Entry{
		SessionID: sessionID,
		Kind:      KindCompacted,
		Compacted: &r,
	}
}

// NewCacheBrokenEntry creates an Entry initialized with a CacheBrokenRecord payload.
func NewCacheBrokenEntry(sessionID string, rec CacheBrokenRecord) *Entry {
	r := rec
	return &Entry{
		SessionID:   sessionID,
		Kind:        KindCacheBroken,
		CacheBroken: &r,
	}
}

// NewTurnCompletedEntry creates an Entry initialized with a TurnCompletedRecord payload.
func NewTurnCompletedEntry(sessionID string, rec TurnCompletedRecord) *Entry {
	r := rec
	return &Entry{
		SessionID:     sessionID,
		Kind:          KindTurnCompleted,
		TurnCompleted: &r,
	}
}
