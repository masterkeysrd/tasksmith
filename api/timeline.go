package api

import "time"

// TimelineKind indicates the variety of a timeline entry.
type TimelineKind string

const (
	TimelineKindMessage     TimelineKind = "message"
	TimelineKindModel       TimelineKind = "model"
	TimelineKindEffort      TimelineKind = "effort"
	TimelineKindAgent       TimelineKind = "agent"
	TimelineKindMode        TimelineKind = "mode"
	TimelineKindPermission  TimelineKind = "permission"
	TimelineKindInterrupted TimelineKind = "interrupted"
)

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

// TimelineEntry represents an append-only entry in a session's chronological timeline.
type TimelineEntry struct {
	ID         string            `json:"id"`
	SessionID  string            `json:"session_id"`
	Kind       TimelineKind      `json:"kind"`
	CreatedAt  time.Time         `json:"created_at"`
	Message    *Message          `json:"message,omitempty"`
	Model      *ModelChange      `json:"model,omitempty"`
	Effort     *EffortChange     `json:"effort,omitempty"`
	Agent      *AgentChange      `json:"agent,omitempty"`
	Mode       *ModeChange       `json:"mode,omitempty"`
	Permission *PermissionRecord `json:"permission,omitempty"`
}
