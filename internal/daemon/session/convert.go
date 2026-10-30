package session

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/models"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tasks"
	"github.com/masterkeysrd/tasksmith/internal/daemon/timeline"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
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
		perms = toAPIPermissionRequests(sess.PendingPermissions)
		questions = toAPIQuestionRequests(sess.PendingQuestions)
		ts = ToAPITasks(sess.State.Tasks)
		todos = toAPITodos(sess.Todos)
		queueItems = toAPIQueueItems(sess.VisibleQueue())
		ctxState = sess.Context
	}
	var resolvedModel *api.Model
	if m, thinking, ok := sess.ResolvedModel(); ok {
		converted := CatalogToAPIModel(m)
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
			Mode:     api.PermissionMode(sess.Config.Mode),
		},
		Status:             status,
		Diagnostics:        ToSessionDiagnostics(sess.Diagnostics),
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

// ToSessionDiagnostics converts internal model diagnostics into the public
// representation used by the API.
func ToSessionDiagnostics(in []models.Diagnostic) []api.SessionDiagnostic {
	out := make([]api.SessionDiagnostic, len(in))
	for i, d := range in {
		out[i] = api.SessionDiagnostic{Code: d.Code, Setting: d.Setting, Message: d.Message}
	}
	return out
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
		p, ok := it.Payload.(QueueMessagePayload)
		if !ok {
			return api.QueueItem{}, false
		}
		apiMsg, err := ToAPIMessage(p.Message)
		if err != nil || apiMsg == nil {
			return api.QueueItem{}, false
		}
		return api.QueueItem{
			ID:        it.ID,
			Kind:      api.QueueKindMessage,
			Payload:   api.QueueMessagePayload{Message: *apiMsg},
			CreatedAt: it.CreatedAt,
		}, true
	case QueueKindCommand:
		p, ok := it.Payload.(QueueCommandPayload)
		if !ok {
			return api.QueueItem{}, false
		}
		return api.QueueItem{
			ID:        it.ID,
			Kind:      api.QueueKindCommand,
			Payload:   api.QueueCommandPayload{Command: p.Command},
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

func toAPITodos(items []tools.TodoItem) []api.TodoItem {
	if len(items) == 0 {
		return nil
	}
	out := make([]api.TodoItem, len(items))
	for i, it := range items {
		out[i] = api.TodoItem{
			Content:    it.Content,
			ActiveText: it.ActiveText,
			Status:     string(it.Status),
		}
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

func toAPIPermissionRequest(req tools.PermissionRequest) api.PermissionRequest {
	return api.PermissionRequest{
		ID:            req.ID,
		ToolCallID:    req.ToolCallID,
		ToolName:      req.ToolName,
		Action:        api.ActionType(req.Action),
		Safety:        api.SafetyLevel(req.Safety),
		Target:        req.Target,
		Summary:       req.Summary,
		Diff:          req.Diff,
		GrantPatterns: slices.Clone(req.GrantPatterns),
	}
}

func toAPIPermissionRequests(reqs []tools.PermissionRequest) []api.PermissionRequest {
	if len(reqs) == 0 {
		return nil
	}
	out := make([]api.PermissionRequest, len(reqs))
	for i, r := range reqs {
		out[i] = toAPIPermissionRequest(r)
	}
	return out
}

func toAPIQuestionRequests(reqs []tools.QuestionRequest) []api.QuestionRequest {
	var out []api.QuestionRequest
	for i := range reqs {
		r := reqs[i].Clone()
		out = append(out, api.QuestionRequest{ID: r.ID, ToolCallID: r.ToolCallID, Title: r.Title, Description: r.Description, Questions: r.Questions})
	}
	return out
}

// ToAPIQuestionResponse converts an internal [tools.QuestionResponse] into the
// public [api.QuestionResponse] representation, attaching confirmation flags for
// confirm-typed questions.
func ToAPIQuestionResponse(r tools.QuestionResponse, questions []api.Question) api.QuestionResponse {
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
			answer.Confirmed = new(a.Confirmed)
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
	cloned := entry.Clone()
	apiEntry, err := ToAPITimelineEntry(cloned)
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
		ID:        entry.ID,
		SessionID: entry.SessionID,
		Kind:      entry.Kind,
		CreatedAt: entry.CreatedAt,
	}

	if entry.Message != nil {
		msg, err := ToAPIMessage(entry.Message)
		if err != nil {
			return api.TimelineEntry{}, err
		}
		out.Message = msg
	}

	if entry.Command != nil {
		out.Command = &api.CommandRecord{
			Command:    entry.Command.Command,
			Output:     entry.Command.Output,
			ExitCode:   entry.Command.ExitCode,
			DurationMs: entry.Command.DurationMs,
		}
	}

	if entry.Model != nil {
		m := *entry.Model
		out.Model = &m
	}

	if entry.Effort != nil {
		out.Effort = &api.EffortChange{
			From: entry.Effort.From,
			To:   entry.Effort.To,
		}
	}

	if entry.Thinking != nil {
		out.Thinking = entry.Clone().Thinking
	}

	if entry.Agent != nil {
		out.Agent = &api.AgentChange{
			From: entry.Agent.From,
			To:   entry.Agent.To,
		}
	}

	if entry.Mode != nil {
		out.Mode = &api.ModeChange{
			From: api.PermissionMode(entry.Mode.From),
			To:   api.PermissionMode(entry.Mode.To),
		}
	}

	if entry.Permission != nil {
		out.Permission = &api.PermissionRecord{
			Request: toAPIPermissionRequest(entry.Permission.Request),
			Decision: api.PermissionDecision{
				RequestID:    entry.Permission.Decision.RequestID,
				ToolCallID:   entry.Permission.Decision.ToolCallID,
				Kind:         api.DecisionKind(entry.Permission.Decision.Kind),
				GrantPattern: entry.Permission.Decision.GrantPattern,
				Scope:        api.PermissionScope(entry.Permission.Decision.Scope),
				Feedback:     entry.Permission.Decision.Feedback,
			},
		}
	}

	if entry.Question != nil {
		out.Question = &api.QuestionRecord{
			Request:  toAPIQuestionRequests([]tools.QuestionRequest{entry.Question.Request})[0],
			Response: ToAPIQuestionResponse(entry.Question.Response, entry.Question.Request.Questions),
		}
	}

	if entry.Compacted != nil {
		out.Compacted = &api.CompactedRecord{
			Trigger:      entry.Compacted.Trigger,
			Focus:        entry.Compacted.Focus,
			Summary:      entry.Compacted.Summary,
			BeforeTokens: entry.Compacted.BeforeTokens,
			AfterTokens:  entry.Compacted.AfterTokens,
			DurationMs:   entry.Compacted.DurationMs,
		}
	}

	if entry.CacheBroken != nil {
		out.CacheBroken = &api.CacheBrokenRecord{
			PrevCached:  entry.CacheBroken.PrevCached,
			CurrCached:  entry.CacheBroken.CurrCached,
			InputTokens: entry.CacheBroken.InputTokens,
		}
	}

	if entry.TurnCompleted != nil {
		tc := entry.TurnCompleted
		out.TurnCompleted = &api.TurnCompletedRecord{
			TurnID:       tc.TurnID,
			Status:       tc.Status,
			ElapsedMs:    tc.ElapsedMs,
			InputTokens:  tc.InputTokens,
			OutputTokens: tc.OutputTokens,
			CostNano:     tc.CostNano,
			Unpriced:     tc.Unpriced,
		}
	}

	return out, nil
}

// ToAPIThinkingValues projects a list of thinking option values onto the public
// [api.ThinkingValue] representation, attaching human-readable descriptions for
// known reasoning-effort levels.
func ToAPIThinkingValues(kind string, values []string) []api.ThinkingValue {
	if len(values) == 0 {
		return nil
	}
	out := make([]api.ThinkingValue, len(values))
	for i, value := range values {
		out[i].Value = value
		if strings.EqualFold(kind, "effort") {
			switch strings.ToLower(strings.TrimSpace(value)) {
			case "none":
				out[i].Description = "Requests no reasoning effort, where supported."
			case "minimal":
				out[i].Description = "Minimal reasoning; potentially faster."
			case "low":
				out[i].Description = "Less reasoning; potentially faster."
			case "medium":
				out[i].Description = "Balances reasoning depth and speed."
			case "high":
				out[i].Description = "More reasoning; potentially slower."
			case "xhigh":
				out[i].Description = "More extensive reasoning; potentially slower."
			case "max":
				out[i].Description = "Maximum reasoning; potentially slower."
			case "auto":
				out[i].Description = "Lets the provider choose the reasoning effort."
			}
		}
	}
	return out
}

// CatalogToAPIModel projects an internal catalog [models.Model] onto the public
// [api.Model] representation used by the API.
func CatalogToAPIModel(m models.Model) api.Model {
	out := api.Model{
		ID:           string(m.ID),
		Provider:     m.ID.Provider(),
		Name:         m.Name,
		Label:        cmp.Or(m.Label, m.Name),
		ContextLimit: m.Limits.Context,
		OutputLimit:  m.Limits.Output,
		Capabilities: api.ModelCapabilities{
			Completion:       m.Capabilities.Completion,
			Decision:         m.Capabilities.Decision,
			Embedding:        m.Capabilities.Embedding,
			Tools:            m.Capabilities.Tools,
			Attachment:       m.Capabilities.Attachment,
			StructuredOutput: m.Capabilities.StructuredOutput,
			Temperature:      m.Capabilities.Temperature,
			Dimensions:       slices.Clone(m.Capabilities.Dimensions),
			InputModalities:  slices.Clone(m.Modalities.Input),
			OutputModalities: slices.Clone(m.Modalities.Output),
		},
	}
	for _, o := range m.Capabilities.Thinking {
		out.Thinking = append(out.Thinking, api.ThinkingCapability{
			Type:    o.Type,
			Values:  ToAPIThinkingValues(o.Type, o.Values),
			Default: o.Default,
			Min:     o.Min,
			Max:     o.Max,
		})
	}
	if p := m.Pricing; p != nil {
		out.Pricing = &api.ModelPricing{
			Input:      p.Input,
			Output:     p.Output,
			CacheRead:  p.CacheRead,
			CacheWrite: p.CacheWrite,
			Reasoning:  p.Reasoning,
		}
		for _, t := range p.Tiers {
			out.Pricing.Tiers = append(out.Pricing.Tiers, api.ModelPricingTier{
				Label:         t.Label,
				ConditionType: t.Condition.Type,
				ConditionSize: t.Condition.Size,
				Input:         t.Input,
				Output:        t.Output,
				CacheRead:     t.CacheRead,
				CacheWrite:    t.CacheWrite,
				Reasoning:     t.Reasoning,
			})
		}
	}
	return out
}
