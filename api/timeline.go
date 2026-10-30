package api

import "time"

// TimelineKind indicates the variety of a timeline entry.
type TimelineKind string

const (
	TimelineKindMessage       TimelineKind = "message"
	TimelineKindCommand       TimelineKind = "command"
	TimelineKindModel         TimelineKind = "model"
	TimelineKindEffort        TimelineKind = "effort"
	TimelineKindThinking      TimelineKind = "thinking"
	TimelineKindAgent         TimelineKind = "agent"
	TimelineKindMode          TimelineKind = "mode"
	TimelineKindPermission    TimelineKind = "permission"
	TimelineKindQuestion      TimelineKind = "question"
	TimelineKindInterrupted   TimelineKind = "interrupted"
	TimelineKindCompacted     TimelineKind = "compacted"
	TimelineKindCacheBroken   TimelineKind = "cache_broken"
	TimelineKindTurnCompleted TimelineKind = "turn_completed"
)

// CommandRecord captures a direct user shell command (!) and its output.
type CommandRecord struct {
	Command    string `json:"command"`
	Output     string `json:"output,omitempty"`
	ExitCode   int    `json:"exit_code"`
	DurationMs int64  `json:"duration_ms,omitempty"`
}

// ModelChange captures a transition of the active LLM.
type ModelChange struct {
	From string `json:"from,omitempty"`
	To   string `json:"to"`
}

// EffortChange captures a change in reasoning effort level.
type EffortChange struct {
	From string `json:"from,omitempty"`
	To   string `json:"to"`
}

type ThinkingChange struct {
	From *ThinkingSetting `json:"from,omitempty"`
	To   *ThinkingSetting `json:"to,omitempty"`
}

// AgentChange captures a change of the active agent persona.
type AgentChange struct {
	From string `json:"from,omitempty"`
	To   string `json:"to"`
}

// ModeChange captures a transition of the session permission mode.
type ModeChange struct {
	From PermissionMode `json:"from,omitempty"`
	To   PermissionMode `json:"to"`
}

// PermissionRecord captures a requested permission and its resolution decision.
type PermissionRecord struct {
	Request  PermissionRequest  `json:"request"`
	Decision PermissionDecision `json:"decision"`
}

type QuestionRecord struct {
	Request  QuestionRequest  `json:"request"`
	Response QuestionResponse `json:"response"`
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

// Turn completion statuses of a [TurnCompletedRecord].
const (
	TurnStatusDone     = "done"
	TurnStatusCanceled = "canceled"
	TurnStatusFailed   = "failed"
)

// TurnCompletedRecord summarizes a finished turn: the span from a user message
// until the agent's final answer, or until the turn was canceled or failed.
type TurnCompletedRecord struct {
	TurnID       string `json:"turn_id"`
	Status       string `json:"status"`
	ElapsedMs    int64  `json:"elapsed_ms"`
	InputTokens  int64  `json:"input_tokens,omitempty"`
	OutputTokens int64  `json:"output_tokens,omitempty"`
	CostNano     int64  `json:"cost_nano,omitempty"`
	// Unpriced reports that some calls of the turn had no pricing, so
	// CostNano is a lower bound.
	Unpriced bool `json:"unpriced,omitempty"`
}

// TimelineEntry represents an append-only entry in a session's chronological timeline.
type TimelineEntry struct {
	ID            string               `json:"id"`
	SessionID     string               `json:"session_id"`
	Kind          TimelineKind         `json:"kind"`
	CreatedAt     time.Time            `json:"created_at"`
	Message       *Message             `json:"message,omitempty"`
	Command       *CommandRecord       `json:"command,omitempty"`
	Model         *ModelChange         `json:"model,omitempty"`
	Effort        *EffortChange        `json:"effort,omitempty"`
	Thinking      *ThinkingChange      `json:"thinking,omitempty"`
	Agent         *AgentChange         `json:"agent,omitempty"`
	Mode          *ModeChange          `json:"mode,omitempty"`
	Permission    *PermissionRecord    `json:"permission,omitempty"`
	Question      *QuestionRecord      `json:"question,omitempty"`
	Compacted     *CompactedRecord     `json:"compacted,omitempty"`
	CacheBroken   *CacheBrokenRecord   `json:"cache_broken,omitempty"`
	TurnCompleted *TurnCompletedRecord `json:"turn_completed,omitempty"`
}
