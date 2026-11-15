package timeline

import (
	"errors"
	"slices"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/ptr"
)

// Sentinel errors returned by the timeline package.
var (
	ErrInvalidConfig  = errors.New("timeline: invalid configuration")
	ErrEmptySessionID = errors.New("timeline: session ID must not be empty")
	ErrInvalidEntry   = errors.New("timeline: invalid entry")
	ErrEntryNotFound  = errors.New("timeline: entry not found")
)

// Entry is a single append-only timeline event in a session log.
type Entry struct {
	api.TimelineEntry
	Message message.Message `json:"-"`
}

// Clone returns a deep copy of the timeline entry.
func (e Entry) Clone() Entry {
	cp := e
	if e.Message != nil {
		cp.Message = message.CloneMessage(e.Message)
	}
	cp.Command = ptr.Clone(e.Command)
	cp.Model = ptr.Clone(e.Model)
	cp.Effort = ptr.Clone(e.Effort)
	if e.Thinking != nil {
		cp.Thinking = &api.ThinkingChange{
			From: ptr.Clone(e.Thinking.From),
			To:   ptr.Clone(e.Thinking.To),
		}
	}
	cp.Agent = ptr.Clone(e.Agent)
	cp.Mode = ptr.Clone(e.Mode)
	if e.Permission != nil {
		perm := *e.Permission
		perm.Request.GrantPatterns = slices.Clone(e.Permission.Request.GrantPatterns)
		cp.Permission = &perm
	}
	if e.Question != nil {
		cp.Question = &api.QuestionRecord{
			Request:  *e.Question.Request.Clone(),
			Response: *e.Question.Response.Clone(),
		}
	}
	cp.Compacted = ptr.Clone(e.Compacted)
	cp.CacheBroken = ptr.Clone(e.CacheBroken)
	cp.TurnCompleted = ptr.Clone(e.TurnCompleted)
	cp.Rule = ptr.Clone(e.Rule)
	return cp
}

// NewMessageEntry creates an Entry initialized with a message.Message payload.
func NewMessageEntry(msg message.Message) *Entry {
	var id string
	if msg != nil {
		id = msg.GetID()
	}
	return &Entry{
		TimelineEntry: api.TimelineEntry{
			ID:   id,
			Kind: api.TimelineKindMessage,
		},
		Message: msg,
	}
}

// NewModelEntry creates an Entry initialized with a ModelChange payload.
func NewModelEntry(change api.ModelChange) *Entry {
	return &Entry{
		TimelineEntry: api.TimelineEntry{
			Kind:  api.TimelineKindModel,
			Model: &change,
		},
	}
}

// NewEffortEntry creates an Entry initialized with an EffortChange payload.
func NewEffortEntry(change api.EffortChange) *Entry {
	return &Entry{
		TimelineEntry: api.TimelineEntry{
			Kind:   api.TimelineKindEffort,
			Effort: &change,
		},
	}
}

// NewThinkingEntry creates an Entry initialized with a ThinkingChange payload.
func NewThinkingEntry(change api.ThinkingChange) *Entry {
	return &Entry{
		TimelineEntry: api.TimelineEntry{
			Kind: api.TimelineKindThinking,
			Thinking: &api.ThinkingChange{
				From: ptr.Clone(change.From),
				To:   ptr.Clone(change.To),
			},
		},
	}
}

// NewAgentEntry creates an Entry initialized with an AgentChange payload.
func NewAgentEntry(change api.AgentChange) *Entry {
	return &Entry{
		TimelineEntry: api.TimelineEntry{
			Kind:  api.TimelineKindAgent,
			Agent: &change,
		},
	}
}

// NewModeEntry creates an Entry initialized with a ModeChange payload.
func NewModeEntry(change api.ModeChange) *Entry {
	return &Entry{
		TimelineEntry: api.TimelineEntry{
			Kind: api.TimelineKindMode,
			Mode: &change,
		},
	}
}

// NewPermissionEntry creates an Entry initialized with a PermissionRecord payload.
func NewPermissionEntry(record api.PermissionRecord) *Entry {
	return &Entry{
		TimelineEntry: api.TimelineEntry{
			Kind:       api.TimelineKindPermission,
			Permission: &record,
		},
	}
}

// NewQuestionEntry creates an Entry initialized with a QuestionRecord payload.
func NewQuestionEntry(record api.QuestionRecord) *Entry {
	return &Entry{
		TimelineEntry: api.TimelineEntry{
			Kind:     api.TimelineKindQuestion,
			Question: &record,
		},
	}
}

// NewCommandEntry creates an Entry initialized with a CommandRecord payload.
func NewCommandEntry(cmd api.CommandRecord) *Entry {
	return &Entry{
		TimelineEntry: api.TimelineEntry{
			Kind:    api.TimelineKindCommand,
			Command: &cmd,
		},
	}
}

// NewInterruptedEntry creates an Entry recording that a turn was interrupted/canceled by the user.
func NewInterruptedEntry() *Entry {
	return &Entry{
		TimelineEntry: api.TimelineEntry{
			Kind: api.TimelineKindInterrupted,
		},
	}
}

// NewCompactedEntry creates an Entry initialized with a CompactedRecord payload.
func NewCompactedEntry(rec api.CompactedRecord) *Entry {
	return &Entry{
		TimelineEntry: api.TimelineEntry{
			Kind:      api.TimelineKindCompacted,
			Compacted: &rec,
		},
	}
}

// NewCacheBrokenEntry creates an Entry initialized with a CacheBrokenRecord payload.
func NewCacheBrokenEntry(rec api.CacheBrokenRecord) *Entry {
	return &Entry{
		TimelineEntry: api.TimelineEntry{
			Kind:        api.TimelineKindCacheBroken,
			CacheBroken: &rec,
		},
	}
}

// NewTurnCompletedEntry creates an Entry initialized with a TurnCompletedRecord payload.
func NewTurnCompletedEntry(rec api.TurnCompletedRecord) *Entry {
	return &Entry{
		TimelineEntry: api.TimelineEntry{
			Kind:          api.TimelineKindTurnCompleted,
			TurnCompleted: &rec,
		},
	}
}

// NewRuleEntry creates an Entry initialized with a RuleRecord payload.
func NewRuleEntry(rec api.RuleRecord) *Entry {
	return &Entry{
		TimelineEntry: api.TimelineEntry{
			Kind: api.TimelineKindRule,
			Rule: &rec,
		},
	}
}
