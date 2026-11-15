package store

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
)

var (
	subagentOpenerMu sync.RWMutex
	subagentOpener   func(string)
)

// SetSubagentOpener registers a callback that opens a subagent's live view.
func SetSubagentOpener(fn func(string)) {
	subagentOpenerMu.Lock()
	defer subagentOpenerMu.Unlock()
	subagentOpener = fn
}

// OpenSubagent opens the subagent live view for the specified subagent ID.
func OpenSubagent(subagentID string) {
	subagentOpenerMu.RLock()
	fn := subagentOpener
	subagentOpenerMu.RUnlock()
	if fn != nil {
		fn(subagentID)
	}
}

// UseSubagents subscribes a component to the active session's child subagents.
func UseSubagents() []api.Session {
	subs := UseSession(func(s SessionState) *[]api.Session {
		return s.Subagents
	})
	if subs == nil {
		return nil
	}
	return *subs
}

// UseRunningSubagents subscribes a component to the active session's running subagents.
func UseRunningSubagents() []api.Session {
	all := UseSubagents()
	var running []api.Session
	for _, s := range all {
		if s.Status != "" && s.Status != api.StatusIdle {
			running = append(running, s)
		}
	}
	return running
}

// UseWaitingSubagents subscribes a component to the active session's subagents waiting for permission or a question.
func UseWaitingSubagents() []api.Session {
	all := UseSubagents()
	var waiting []api.Session
	for _, s := range all {
		if s.Status == api.StatusWaitingPermission || s.Status == api.StatusWaitingQuestion || len(s.PendingPermissions) > 0 || len(s.PendingQuestions) > 0 {
			waiting = append(waiting, s)
		}
	}
	return waiting
}

// GetSubagents returns a snapshot of child subagents for the active session.
func GetSubagents() []api.Session {
	st := GetSession()
	if st.Subagents == nil {
		return nil
	}
	return *st.Subagents
}

// GetRunningSubagents returns a snapshot of running subagents for the active session.
func GetRunningSubagents() []api.Session {
	all := GetSubagents()
	var running []api.Session
	for _, s := range all {
		if s.Status != "" && s.Status != api.StatusIdle {
			running = append(running, s)
		}
	}
	return running
}

// GetWaitingSubagents returns a snapshot of subagents waiting for permission or a question.
func GetWaitingSubagents() []api.Session {
	all := GetSubagents()
	var waiting []api.Session
	for _, s := range all {
		if s.Status == api.StatusWaitingPermission || s.Status == api.StatusWaitingQuestion || len(s.PendingPermissions) > 0 || len(s.PendingQuestions) > 0 {
			waiting = append(waiting, s)
		}
	}
	return waiting
}

// UpsertSubagent updates an existing child subagent in SessionState by ID, or appends it.
func UpsertSubagent(sub api.Session) {
	session.Set(func(s SessionState) SessionState {
		var current []api.Session
		if s.Subagents != nil {
			current = make([]api.Session, len(*s.Subagents))
			copy(current, *s.Subagents)
		}
		found := false
		for i, existing := range current {
			if existing.ID == sub.ID {
				current[i] = sub
				found = true
				break
			}
		}
		if !found {
			current = append(current, sub)
		}
		s.Subagents = &current
		if s.Session != nil {
			s.Session.Subagents = current
		}
		return s
	})
}

// KillSubagent sends a cancel request to terminate an active child subagent.
func KillSubagent(subagentID string) error {
	subagentID = strings.TrimSpace(subagentID)
	if subagentID == "" {
		return errors.New("repl: subagent ID must not be empty")
	}
	sessMu.Lock()
	c := activeClient
	ctx := activeCtx
	sessMu.Unlock()

	if c == nil {
		return errors.New("repl: no active client")
	}
	if ctx == nil || ctx.Err() != nil {
		ctx = context.Background()
	}
	return c.Cancel(ctx, subagentID)
}

// DecideSubagentPermission submits a user decision on a pending permission request for a child subagent.
func DecideSubagentPermission(subagentID string, req api.PermissionRequest, dec api.PermissionDecision) {
	if req.ID == "" || subagentID == "" {
		return
	}
	dec.RequestID = req.ID
	dec.ToolCallID = req.ToolCallID

	// Optimistically remove req.ID from that subagent's PendingPermissions in SessionState
	session.Set(func(s SessionState) SessionState {
		if s.Subagents == nil {
			return s
		}
		subs := make([]api.Session, len(*s.Subagents))
		copy(subs, *s.Subagents)
		for i := range subs {
			if subs[i].ID == subagentID {
				subs[i].PendingPermissions = slices.DeleteFunc(subs[i].PendingPermissions, func(p api.PermissionRequest) bool {
					return p.ID == req.ID
				})
				if len(subs[i].PendingPermissions) == 0 && len(subs[i].PendingQuestions) == 0 && subs[i].Status == api.StatusWaitingPermission {
					subs[i].Status = api.StatusExecuting
				}
				break
			}
		}
		s.Subagents = &subs
		if s.Session != nil {
			s.Session.Subagents = subs
		}
		return s
	})

	sessMu.Lock()
	c := activeClient
	ctx := activeCtx
	sessMu.Unlock()

	if c == nil {
		return
	}

	go func() {
		if ctx == nil || ctx.Err() != nil {
			ctx = context.Background()
		}
		err := c.Send(ctx, subagentID, api.SendRequest{Decision: &dec})
		if err != nil {
			AppendError("Error", err.Error())
		}
	}()
}

// SubmitSubagentQuestion submits a user answer to a pending question for a child subagent.
func SubmitSubagentQuestion(subagentID string, req api.QuestionRequest, resp api.QuestionResponse, done func(error)) {
	if req.ID == "" || subagentID == "" {
		if done != nil {
			done(errors.New("repl: invalid subagent or question request"))
		}
		return
	}
	resp.RequestID = req.ID
	resp.ToolCallID = req.ToolCallID

	// Optimistically remove req.ID from that subagent's PendingQuestions in SessionState
	session.Set(func(s SessionState) SessionState {
		if s.Subagents == nil {
			return s
		}
		subs := make([]api.Session, len(*s.Subagents))
		copy(subs, *s.Subagents)
		for i := range subs {
			if subs[i].ID == subagentID {
				subs[i].PendingQuestions = slices.DeleteFunc(subs[i].PendingQuestions, func(q api.QuestionRequest) bool {
					return q.ID == req.ID
				})
				if len(subs[i].PendingPermissions) == 0 && len(subs[i].PendingQuestions) == 0 && subs[i].Status == api.StatusWaitingQuestion {
					subs[i].Status = api.StatusExecuting
				}
				break
			}
		}
		s.Subagents = &subs
		if s.Session != nil {
			s.Session.Subagents = subs
		}
		return s
	})

	sessMu.Lock()
	c := activeClient
	ctx := activeCtx
	sessMu.Unlock()

	if c == nil {
		if done != nil {
			done(errors.New("repl: no active client"))
		}
		return
	}

	go func() {
		if ctx == nil || ctx.Err() != nil {
			ctx = context.Background()
		}
		err := c.Send(ctx, subagentID, api.SendRequest{Question: &resp})
		if done != nil {
			done(err)
		}
	}()
}

// SubagentConversation maintains an isolated conversation stream for a subagent session.
type SubagentConversation struct {
	acc *conversationAccumulator
}

// NewSubagentConversation creates an isolated accumulator for tracking a subagent session's live timeline and events.
func NewSubagentConversation(sessionID string) *SubagentConversation {
	return &SubagentConversation{
		acc: newIsolatedConversationAccumulator(sessionID),
	}
}

// LoadTimeline initializes or updates the conversation history from timeline entries.
func (s *SubagentConversation) LoadTimeline(entries []api.TimelineEntry, pending ...[]api.QuestionRequest) {
	s.acc.loadTimeline(entries, pending...)
}

// SeedPermissions imports pending permission requests into the subagent conversation.
func (s *SubagentConversation) SeedPermissions(requests []api.PermissionRequest) {
	s.acc.SeedPermissions(requests)
}

// RemovePermission removes a resolved permission request by ID from the subagent conversation.
func (s *SubagentConversation) RemovePermission(reqID string) {
	s.acc.removePermission(reqID)
}

// RemoveQuestion removes a resolved question request by ID from the subagent conversation.
func (s *SubagentConversation) RemoveQuestion(reqID string) {
	s.acc.mu.Lock()
	defer s.acc.mu.Unlock()
	s.acc.removeQuestionLocked(reqID)
	s.acc.publishLocked()
}

// ApplyEvent processes a live SSE event for this subagent session.
func (s *SubagentConversation) ApplyEvent(event api.Event) {
	s.acc.applyEvent(event)
}

// Messages returns all projected messages (finalized history + live stream) in chronological order.
func (s *SubagentConversation) Messages() []Message {
	s.acc.mu.Lock()
	defer s.acc.mu.Unlock()
	history, streamMsgs := s.acc.projectLocked()
	all := make([]Message, 0, len(history)+len(streamMsgs))
	all = append(all, history...)
	all = append(all, streamMsgs...)
	return all
}

// SessionStatus returns the current execution status of the subagent.
func (s *SubagentConversation) SessionStatus() api.SessionStatus {
	s.acc.mu.Lock()
	defer s.acc.mu.Unlock()
	return s.acc.sessionStatus
}

// IsStreaming reports whether an LLM turn is currently streaming.
func (s *SubagentConversation) IsStreaming() bool {
	s.acc.mu.Lock()
	defer s.acc.mu.Unlock()
	return s.acc.status == StreamRunning || s.acc.status == StreamWaitingPermission || s.acc.status == StreamWaitingQuestion || s.acc.pendingUser != nil || (s.acc.draft >= 0 && s.acc.status != StreamIdle && s.acc.status != StreamCompleted && s.acc.status != StreamFailed)
}

// Todos returns a snapshot of active checklist items for the subagent.
func (s *SubagentConversation) Todos() []api.TodoItem {
	s.acc.mu.Lock()
	defer s.acc.mu.Unlock()
	return slices.Clone(s.acc.todos)
}

// TurnStats returns the start time, input tokens, and output tokens for the current turn.
func (s *SubagentConversation) TurnStats() (startedAt time.Time, input int64, output int64) {
	s.acc.mu.Lock()
	defer s.acc.mu.Unlock()
	return s.acc.turnStart, s.acc.callInput, s.acc.callOutput
}

// PendingPermissions returns the pending permission requests for this subagent.
func (s *SubagentConversation) PendingPermissions() []api.PermissionRequest {
	s.acc.mu.Lock()
	defer s.acc.mu.Unlock()
	return slices.Clone(s.acc.permissions)
}

// PendingQuestions returns the pending questions for this subagent.
func (s *SubagentConversation) PendingQuestions() []api.QuestionRequest {
	s.acc.mu.Lock()
	defer s.acc.mu.Unlock()
	return slices.Clone(s.acc.questions)
}
