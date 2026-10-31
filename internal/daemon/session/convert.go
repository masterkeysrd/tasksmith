package session

import (
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tasks"
	"github.com/masterkeysrd/tasksmith/internal/daemon/timeline"
)

// ToAPISession converts an internal [Session] into the public [api.Session]
// representation used for HTTP responses and published events.
func ToAPISession(sess *Session) api.Session {
	if sess == nil {
		return api.Session{}
	}
	sess = sess.Clone()
	status := api.StatusIdle
	var perms []api.PermissionRequest
	var questions []api.QuestionRequest
	var ts []api.Task
	var todos []api.TodoItem
	var queueItems []api.QueueItem
	var ctxState api.ContextState
	if sess.State != nil {
		if sess.Status != "" {
			status = api.SessionStatus(sess.Status)
		}
		perms = sess.PendingPermissions
		questions = sess.PendingQuestions
		ts = ToAPITasks(sess.State.Tasks)
		todos = sess.Todos
		queueItems = toAPIQueueItems(sess.VisibleQueue())
		ctxState = sess.Context
	}
	var resolvedModel *api.Model
	if m, thinking, ok := sess.ResolvedModel(); ok {
		converted := m.ToAPI()
		converted.ThinkingDefault = thinking
		resolvedModel = &converted
	}
	return api.Session{
		Model: resolvedModel,
		ID:    sess.ID,
		Title: sess.Title,
		Config: api.SessionConfig{
			Agent:    sess.Config.Agent,
			Model:    string(sess.Config.Model),
			Thinking: sess.Config.Thinking,
			Mode:     sess.Config.Mode,
		},
		Status:             status,
		Diagnostics:        slices.Clone(sess.Diagnostics),
		Context:            ctxState,
		PendingPermissions: perms,
		PendingQuestions:   questions,
		Tasks:              ts,
		Todos:              todos,
		Queue:              queueItems,
		CreatedAt:          sess.CreatedAt,
		UpdatedAt:          sess.UpdatedAt,
	}
}

func toAPIQueueItems(items []QueueItem) []api.QueueItem {
	if len(items) == 0 {
		return nil
	}
	out := make([]api.QueueItem, 0, len(items))
	for _, it := range items {
		if apiItem, ok := toAPIQueueItem(it); ok {
			out = append(out, apiItem)
		}
	}
	return out
}

func toAPIQueueItem(it QueueItem) (api.QueueItem, bool) {
	switch it.Kind {
	case QueueKindMessage:
		if it.Message == nil {
			return api.QueueItem{}, false
		}
		apiMsg, err := ToAPIMessage(it.Message)
		if err != nil || apiMsg == nil {
			return api.QueueItem{}, false
		}
		return api.QueueItem{
			ID:        it.ID,
			Kind:      api.QueueKindMessage,
			Message:   apiMsg,
			CreatedAt: it.CreatedAt,
		}, true
	case QueueKindCommand:
		return api.QueueItem{
			ID:        it.ID,
			Kind:      api.QueueKindCommand,
			Command:   it.Command,
			CreatedAt: it.CreatedAt,
		}, true
	default:
		return api.QueueItem{}, false
	}
}

func toAPITask(t tasks.Snapshot) api.Task {
	return api.Task{
		ID:           t.ID,
		Kind:         t.Kind,
		Title:        t.Title,
		Command:      t.Command,
		Status:       api.TaskStatus(t.Status),
		ExitCode:     t.ExitCode,
		Error:        t.Error,
		LogPath:      t.LogPath,
		StartedAt:    t.StartedAt,
		FinishedAt:   t.FinishedAt,
		DurationMs:   t.DurationMs,
		Backgrounded: t.Backgrounded,
	}
}

// ToAPITasks converts a slice of internal task snapshots into the public
// representation used by the API.
func ToAPITasks(ts []tasks.Snapshot) []api.Task {
	if len(ts) == 0 {
		return nil
	}
	out := make([]api.Task, len(ts))
	for i, t := range ts {
		out[i] = toAPITask(t)
	}
	return out
}

// ToAPIMessage converts an internal [message.Message] into the public
// [api.Message] representation. It returns an error when a structured tool
// payload cannot be serialized.
func ToAPIMessage(msg message.Message) (*api.Message, error) {
	if msg == nil {
		return nil, nil
	}

	out := &api.Message{
		ID:        msg.GetID(),
		Role:      api.Role(msg.Role()),
		Content:   toAPIBlocks(msg.GetContent()),
		CreatedAt: extractUUIDv7Time(msg.GetID()),
	}

	if tm, ok := msg.(*message.Tool); ok && tm != nil {
		out.ToolCallID = tm.ToolCallID
		out.Name = tm.Name
		out.IsError = tm.IsError
		if tm.StructuredContent != nil {
			data, err := tm.CanonicalStructuredContent()
			if err != nil {
				return nil, fmt.Errorf("daemon: marshal structured content for message %q: %w", msg.GetID(), err)
			}
			out.StructuredContent = data
		}
	}

	return out, nil
}

func extractUUIDv7Time(id string) time.Time {
	u, err := uuid.Parse(id)
	if err != nil || u.Version() != 7 {
		return time.Time{}
	}
	sec, nsec := u.Time().UnixTime()
	return time.Unix(sec, nsec).UTC()
}

func toAPIBlocks(blocks []message.Block) []api.Block {
	if len(blocks) == 0 {
		return nil
	}
	out := make([]api.Block, 0, len(blocks))
	for _, b := range blocks {
		if converted, ok := ToAPIBlock(b); ok {
			out = append(out, converted)
		}
	}
	return out
}

// ToAPIBlock converts a single internal [message.Block] into the public
// [api.Block] representation. The boolean reports whether the block type is
// supported by the public API.
func ToAPIBlock(b message.Block) (api.Block, bool) {
	switch v := b.(type) {
	case *message.TextBlock:
		if v == nil {
			return api.Block{}, false
		}
		return api.Block{
			Type: api.BlockText,
			Text: v.Text,
		}, true

	case *message.ThinkingBlock:
		if v == nil {
			return api.Block{}, false
		}
		sig, _ := v.Extras["signature"].(string)
		return api.Block{
			Type:      api.BlockReasoning,
			Text:      v.Thinking,
			Signature: sig,
		}, true

	case *message.ToolCall:
		if v == nil {
			return api.Block{}, false
		}
		return api.Block{
			Type: api.BlockToolCall,
			ToolCall: &api.ToolCall{
				ID:   v.ID,
				Name: v.Name,
				Args: maps.Clone(v.Args),
			},
		}, true

	case *message.ToolCallChunk:
		if v == nil {
			return api.Block{}, false
		}
		var args map[string]any
		if tc, err := v.ToToolCall(); err == nil && tc != nil {
			args = tc.Args
		}
		return api.Block{
			Type: api.BlockToolCall,
			ToolCall: &api.ToolCall{
				ID:   v.ID,
				Name: v.Name,
				Args: args,
			},
		}, true

	default:
		return api.Block{}, false
	}
}

func toAPIChunk(ch *message.AssistantChunk) *api.Chunk {
	if ch == nil {
		return nil
	}
	out := &api.Chunk{
		ID:      ch.ID,
		Content: toAPIBlocks(ch.Content),
		Done:    ch.Done,
	}
	if ch.Metrics != nil && (ch.Metrics.Tokens.Input > 0 || ch.Metrics.Tokens.Output > 0) {
		out.Usage = &api.ChunkUsage{Input: ch.Metrics.Tokens.Input, Output: ch.Metrics.Tokens.Output}
	}
	return out
}

// ToAPIQuestionResponse converts an internal [api.QuestionResponse] into the
// public [api.QuestionResponse] representation, attaching confirmation flags for
// confirm-typed questions.
func ToAPIQuestionResponse(r api.QuestionResponse, questions []api.Question) api.QuestionResponse {
	out := api.QuestionResponse{
		RequestID:  r.RequestID,
		ToolCallID: r.ToolCallID,
		Canceled:   r.Canceled,
	}
	for i, a := range r.Answers {
		answer := api.QuestionAnswer{
			Question: a.Question,
			Selected: slices.Clone(a.Selected),
			Text:     a.Text,
		}
		if i < len(questions) && questions[i].Type == string(api.QuestionTypeConfirm) {
			answer.Confirmed = a.Confirmed
		}
		out.Answers = append(out.Answers, answer)
	}
	return out
}

// publishEntryAdded converts a freshly-persisted timeline entry into the public
// [api.EntryAddedEvent] and publishes it. If the conversion fails (for example
// an un-marshalable structured message payload) it publishes an
// [api.ErrorEvent] on the same topic instead of dropping the notification.
func publishEntryAdded(p Publisher, sessionID string, entry *timeline.Entry) {
	if entry == nil {
		return
	}
	apiEntry, err := ToAPITimelineEntry(*entry)
	if err != nil {
		p.Publish(api.SessionStreamTopic(sessionID), api.ErrorEvent{SessionID: sessionID, Error: err.Error()})
		return
	}
	p.Publish(api.SessionStreamTopic(sessionID), api.EntryAddedEvent{SessionID: sessionID, Entry: apiEntry})
}

// ToAPITimelineEntry converts an internal [timeline.Entry] into the public
// [api.TimelineEntry] representation used by the API.
func ToAPITimelineEntry(entry timeline.Entry) (api.TimelineEntry, error) {
	out := api.TimelineEntry{
		ID:            entry.ID,
		SessionID:     entry.SessionID,
		Kind:          entry.Kind,
		CreatedAt:     entry.CreatedAt,
		Command:       entry.Command,
		Model:         entry.Model,
		Effort:        entry.Effort,
		Thinking:      entry.Thinking,
		Agent:         entry.Agent,
		Mode:          entry.Mode,
		Permission:    entry.Permission,
		Question:      entry.Question,
		Compacted:     entry.Compacted,
		CacheBroken:   entry.CacheBroken,
		TurnCompleted: entry.TurnCompleted,
	}

	if entry.Message != nil {
		msg, err := ToAPIMessage(entry.Message)
		if err != nil {
			return api.TimelineEntry{}, err
		}
		out.Message = msg
	}

	return out, nil
}
