package harness

import (
	"maps"
	"slices"
	"time"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/models"
)

const (
	MetaSystemKind         = "tasksmith.system.kind"
	MetaCompactionTrigger  = "tasksmith.compaction.trigger"
	MetaCompactionFocus    = "tasksmith.compaction.focus"
	MetaCompactionRequests = "tasksmith.compaction.requests"
)

const (
	SystemKindCommand    = "command"
	SystemKindTask       = "task"
	SystemKindCompaction = "compaction"
)

// SystemKindOf returns the "tasksmith.system.kind" metadata value on msg, or "" if unset.
func SystemKindOf(msg message.Message) string {
	if msg == nil {
		return ""
	}
	meta := msg.GetMetadata()
	if meta == nil {
		return ""
	}
	v, _ := meta[MetaSystemKind].(string)
	return v
}

// State holds the checkpointed conversation and permission state for a thread.
// It implements [github.com/masterkeysrd/loom/graph.State].
type State struct {
	Messages            message.MessageList               `json:"messages"`
	PendingCompaction   *api.CompactRequest               `json:"pending_compaction,omitempty"`
	PendingPermissions  []api.PermissionRequest           `json:"pending_permissions,omitempty"`
	PermissionDecisions map[string]api.PermissionDecision `json:"permission_decisions,omitempty"`
	PendingQuestions    []api.QuestionRequest             `json:"pending_questions,omitempty"`
	QuestionResponses   map[string]api.QuestionResponse   `json:"question_responses,omitempty"`
	Todos               []api.TodoItem                    `json:"todos,omitempty"`
	Context             api.ContextState                  `json:"context"`
}

// Copy returns a deep copy of the State so graph transitions do not mutate
// previously captured snapshots.
func (s State) Copy() State {
	msgs := make(message.MessageList, len(s.Messages))
	for i, m := range s.Messages {
		msgs[i] = message.CloneMessage(m)
	}

	var pending []api.PermissionRequest
	if len(s.PendingPermissions) > 0 {
		pending = clonePermissionRequests(s.PendingPermissions)
	}

	var decisions map[string]api.PermissionDecision
	if len(s.PermissionDecisions) > 0 {
		decisions = maps.Clone(s.PermissionDecisions)
	}

	questions := cloneQuestionRequests(s.PendingQuestions)
	var responses map[string]api.QuestionResponse
	if s.QuestionResponses != nil {
		responses = make(map[string]api.QuestionResponse, len(s.QuestionResponses))
		for key, response := range s.QuestionResponses {
			responses[key] = *response.Clone()
		}
	}

	var todos []api.TodoItem
	if len(s.Todos) > 0 {
		todos = slices.Clone(s.Todos)
	}

	var pendingCompact *api.CompactRequest
	if s.PendingCompaction != nil {
		pendingCompact = &api.CompactRequest{Focus: s.PendingCompaction.Focus}
	}

	return State{
		Messages:            msgs,
		PendingCompaction:   pendingCompact,
		PendingPermissions:  pending,
		PermissionDecisions: decisions,
		PendingQuestions:    questions,
		QuestionResponses:   responses,
		Todos:               todos,
		Context:             s.Context,
	}
}

// IsAutoCompacted reports whether the leading message was produced by auto-compaction.
func (s State) IsAutoCompacted() bool {
	if len(s.Messages) > 0 && SystemKindOf(s.Messages[0]) == SystemKindCompaction {
		if meta := s.Messages[0].GetMetadata(); meta != nil {
			if trig, ok := meta[MetaCompactionTrigger].(string); ok && trig == "auto" {
				return true
			}
		}
	}
	return false
}

// ShouldAutoCompact reports whether the current conversation state qualifies for
// mid-turn or entry auto-compaction.
func (s State) ShouldAutoCompact() bool {
	if s.HasPendingToolCalls() {
		return false
	}
	if s.Context.TriggerTokens <= 0 || s.Context.UsedTokens < s.Context.TriggerTokens {
		return false
	}
	if len(s.Messages) < 2 {
		return false
	}
	lastMsg := s.Messages[len(s.Messages)-1]
	return SystemKindOf(lastMsg) != SystemKindCompaction
}

// HasLeadingCompact reports whether there is a manual compaction request pending.
func (s State) HasLeadingCompact() bool {
	return s.PendingCompaction != nil
}

// ShouldWakeAgent checks if the latest message warrants waking the agent node.
func (s State) ShouldWakeAgent() bool {
	if len(s.Messages) == 0 {
		return false
	}
	last := s.Messages[len(s.Messages)-1]
	if _, isAssistant := last.(*message.Assistant); isAssistant {
		return false
	}
	return SystemKindOf(last) != SystemKindCommand
}

// LastUserMessageID returns the ID of the latest [*message.User] message, or "" if none.
func (s State) LastUserMessageID() string {
	for _, m := range slices.Backward(s.Messages) {
		if u, ok := m.(*message.User); ok && u != nil {
			return u.GetID()
		}
	}
	return ""
}

type MessageUpdateOptions struct {
	Static                   StaticContext
	Limits                   models.Limits
	SkipContextRecalculation bool
}

func (s State) ApplyCompaction(static StaticContext, limits models.Limits) State {
	s.PendingCompaction = nil
	s.Context.CachedTokens = 0
	s.Context.Compactions++
	s.Context.LastCompacted = time.Now().UTC()
	s.Context = computeContextState(static, s, limits)
	return s
}

func (s State) PrependMessage(msg message.Message, opts MessageUpdateOptions) State {
	s.Messages = append(message.MessageList{msg}, s.Messages...)
	if !opts.SkipContextRecalculation {
		s.Context = computeContextState(opts.Static, s, opts.Limits)
	}
	return s
}

func (s State) AppendMessage(msg message.Message, opts MessageUpdateOptions) State {
	return s.AppendMessages(message.MessageList{msg}, opts)
}

func (s State) AppendMessages(msgs message.MessageList, opts MessageUpdateOptions) State {
	s.Messages = append(s.Messages, msgs...)
	if !opts.SkipContextRecalculation {
		s.Context = computeContextState(opts.Static, s, opts.Limits)
	}
	return s
}

// HasPendingToolCalls reports whether the latest assistant message in the state
// has one or more unfulfilled tool calls.
func (s State) HasPendingToolCalls() bool {
	_, _, unfulfilled, _ := s.ActiveToolCalls()
	return len(unfulfilled) > 0
}

// ActiveToolCalls locates the latest [*message.Assistant] message with tool calls
// and returns its index, the assistant message, any unfulfilled tool calls, and
// whether a new user message was appended after that assistant turn.
func (s State) ActiveToolCalls() (int, *message.Assistant, []*message.ToolCall, bool) {
	for i, v := range slices.Backward(s.Messages) {
		asst, ok := v.(*message.Assistant)
		if !ok || asst == nil || len(asst.ToolCalls()) == 0 {
			continue
		}

		fulfilled := make(map[string]bool)
		hasUserAfter := false
		for j := i + 1; j < len(s.Messages); j++ {
			switch m := s.Messages[j].(type) {
			case *message.Tool:
				fulfilled[m.ToolCallID] = true
			case *message.User:
				hasUserAfter = true
			}
		}

		var unfulfilled []*message.ToolCall
		for _, tc := range asst.ToolCalls() {
			if tc != nil && !fulfilled[tc.ID] {
				unfulfilled = append(unfulfilled, tc)
			}
		}

		if len(unfulfilled) > 0 {
			return i, asst, unfulfilled, hasUserAfter
		}
		return -1, nil, nil, false
	}
	return -1, nil, nil, false
}

func clonePermissionRequests(in []api.PermissionRequest) []api.PermissionRequest {
	if len(in) == 0 {
		return nil
	}
	out := make([]api.PermissionRequest, len(in))
	for i, r := range in {
		r.GrantPatterns = slices.Clone(r.GrantPatterns)
		out[i] = r
	}
	return out
}

func cloneQuestionRequests(in []api.QuestionRequest) []api.QuestionRequest {
	if len(in) == 0 {
		return nil
	}
	out := make([]api.QuestionRequest, len(in))
	for i := range in {
		out[i] = *in[i].Clone()
	}
	return out
}
