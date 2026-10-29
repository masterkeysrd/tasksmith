package daemon

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
	"github.com/masterkeysrd/tasksmith/internal/daemon/broker"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway"
	"github.com/masterkeysrd/tasksmith/internal/daemon/permissions"
	"github.com/masterkeysrd/tasksmith/internal/daemon/session"
	"github.com/masterkeysrd/tasksmith/internal/daemon/std"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tasks"
	"github.com/masterkeysrd/tasksmith/internal/daemon/timeline"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
)

func toAPISession(sess *session.Session) api.Session {
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
		ts = toAPITasks(sess.State.Tasks)
		todos = toAPITodos(sess.Todos)
		queueItems = toAPIQueueItems(sess.VisibleQueue())
		ctxState = sess.Context
	}
	return api.Session{
		ID:    sess.ID,
		Title: sess.Title,
		Config: api.SessionConfig{
			Agent:    sess.Config.Agent,
			Model:    sess.Config.Model,
			Thinking: sess.Config.Thinking,
			Mode:     api.PermissionMode(sess.Config.Mode),
		},
		Status:             status,
		Diagnostics:        toSessionDiagnostics(sess.Diagnostics),
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

func toSessionDiagnostics(in []gateway.Diagnostic) []api.SessionDiagnostic {
	out := make([]api.SessionDiagnostic, len(in))
	for i, d := range in {
		out[i] = api.SessionDiagnostic{Code: d.Code, Setting: d.Setting, Message: d.Message}
	}
	return out
}

func toAPIQueueItems(items []session.QueueItem) []api.QueueItem {
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

func toAPIQueueItem(it session.QueueItem) (api.QueueItem, bool) {
	switch it.Kind {
	case session.QueueKindMessage:
		p, ok := it.Payload.(session.QueueMessagePayload)
		if !ok {
			return api.QueueItem{}, false
		}
		apiMsg, err := toAPIMessage(p.Message)
		if err != nil || apiMsg == nil {
			return api.QueueItem{}, false
		}
		return api.QueueItem{
			ID:        it.ID,
			Kind:      api.QueueKindMessage,
			Payload:   api.QueueMessagePayload{Message: *apiMsg},
			CreatedAt: it.CreatedAt,
		}, true
	case session.QueueKindCommand:
		p, ok := it.Payload.(session.QueueCommandPayload)
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

func toAPITasks(ts []tasks.Snapshot) []api.Task {
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

func toAPIMessage(msg message.Message) (*api.Message, error) {
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
		if converted, ok := toAPIBlock(b); ok {
			out = append(out, converted)
		}
	}
	return out
}

func toAPIBlock(b message.Block) (api.Block, bool) {
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
	return &api.Chunk{
		ID:      ch.ID,
		Content: toAPIBlocks(ch.Content),
		Done:    ch.Done,
	}
}

func fromSendRequest(req api.SendRequest) (session.SendInput, error) {
	if req.Compact != nil {
		if req.Question != nil || req.Decision != nil || strings.TrimSpace(req.Content) != "" || len(req.Blocks) > 0 || strings.TrimSpace(req.Command) != "" {
			return session.SendInput{}, fmt.Errorf("%w: cannot combine compaction request with other input", session.ErrInvalidConfig)
		}
		id := strings.TrimSpace(req.ID)
		if id != "" {
			parsed, err := uuid.Parse(id)
			if err != nil || parsed.Version() != 7 || parsed.String() != id {
				return session.SendInput{}, fmt.Errorf("%w: invalid message id %q", session.ErrInvalidConfig, id)
			}
		}
		return session.SendInput{ID: id, Compact: req.Compact}, nil
	}
	if req.Question != nil {
		if req.Decision != nil || strings.TrimSpace(req.Content) != "" || len(req.Blocks) > 0 || strings.TrimSpace(req.Command) != "" || strings.TrimSpace(req.ID) != "" {
			return session.SendInput{}, fmt.Errorf("%w: cannot combine question response with other input", session.ErrInvalidConfig)
		}
		answer := fromAPIQuestionResponse(*req.Question)
		return session.SendInput{Question: &answer}, nil
	}
	if req.Decision != nil && (strings.TrimSpace(req.Content) != "" || len(req.Blocks) > 0 || strings.TrimSpace(req.Command) != "" || strings.TrimSpace(req.ID) != "") {
		return session.SendInput{}, fmt.Errorf("%w: cannot specify both message and decision", session.ErrInvalidConfig)
	}
	if req.Decision != nil {
		dec := fromAPIPermissionDecision(*req.Decision)
		return session.SendInput{Decision: &dec}, nil
	}
	id := strings.TrimSpace(req.ID)
	if id != "" {
		parsed, err := uuid.Parse(id)
		if err != nil || parsed.Version() != 7 || parsed.String() != id {
			return session.SendInput{}, fmt.Errorf("%w: invalid message id %q", session.ErrInvalidConfig, id)
		}
	}
	if cmd := strings.TrimSpace(req.Command); cmd != "" {
		if strings.TrimSpace(req.Content) != "" || len(req.Blocks) > 0 {
			return session.SendInput{}, fmt.Errorf("%w: cannot specify both message and command", session.ErrInvalidConfig)
		}
		return session.SendInput{ID: id, Command: cmd}, nil
	}
	var blocks []message.Block
	if strings.TrimSpace(req.Content) != "" {
		blocks = append(blocks, &message.TextBlock{Text: req.Content})
	}
	for _, b := range req.Blocks {
		if converted, ok := fromAPIBlock(b); ok {
			blocks = append(blocks, converted)
		}
	}
	if len(blocks) == 0 {
		return session.SendInput{}, ErrEmptyMessage
	}
	msg := message.NewUser(blocks...)
	if id != "" {
		msg.SetID(id)
	}
	return session.SendInput{ID: id, Message: msg}, nil
}

func fromAPIBlock(b api.Block) (message.Block, bool) {
	switch b.Type {
	case api.BlockText:
		if b.Text == "" {
			return nil, false
		}
		return &message.TextBlock{Text: b.Text}, true

	case api.BlockReasoning:
		if b.Text == "" {
			return nil, false
		}
		var extras map[string]any
		if b.Signature != "" {
			extras = map[string]any{"signature": b.Signature}
		}
		return &message.ThinkingBlock{
			Thinking: b.Text,
			Extras:   extras,
		}, true

	case api.BlockToolCall:
		if b.ToolCall == nil {
			return nil, false
		}
		return &message.ToolCall{
			ID:   b.ToolCall.ID,
			Name: b.ToolCall.Name,
			Args: maps.Clone(b.ToolCall.Args),
		}, true

	default:
		return nil, false
	}
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

func toAPIQuestionResponse(r tools.QuestionResponse, questions []api.Question) api.QuestionResponse {
	out := api.QuestionResponse{RequestID: r.RequestID, ToolCallID: r.ToolCallID, Canceled: r.Canceled}
	for i, a := range r.Answers {
		answer := api.QuestionAnswer{Question: a.Question, Selected: slices.Clone(a.Selected), Text: a.Text}
		if i < len(questions) && questions[i].Type == string(api.QuestionTypeConfirm) {
			answer.Confirmed = new(a.Confirmed)
		}
		out.Answers = append(out.Answers, answer)
	}
	return out
}

func fromAPIQuestionResponse(r api.QuestionResponse) tools.QuestionResponse {
	out := tools.QuestionResponse{RequestID: r.RequestID, ToolCallID: r.ToolCallID, Canceled: r.Canceled}
	for _, a := range r.Answers {
		answer := tools.QuestionAnswer{Question: a.Question, Selected: slices.Clone(a.Selected), Text: a.Text}
		if a.Confirmed != nil {
			answer.Confirmed = *a.Confirmed
		}
		out.Answers = append(out.Answers, answer)
	}
	return out
}

func fromAPIPermissionDecision(dec api.PermissionDecision) tools.PermissionDecision {
	return tools.PermissionDecision{
		RequestID:    dec.RequestID,
		ToolCallID:   dec.ToolCallID,
		Kind:         tools.PermissionDecisionKind(dec.Kind),
		GrantPattern: dec.GrantPattern,
		Scope:        string(dec.Scope),
		Feedback:     dec.Feedback,
	}
}

func toAPIPermissions(p permissions.Permissions) api.Permissions {
	return api.Permissions{
		Allow: slices.Clone(p.Allow),
		Deny:  slices.Clone(p.Deny),
	}
}

func fromAPIPermissionEffect(eff api.PermissionEffect) (permissions.Effect, error) {
	switch eff {
	case api.EffectAllow:
		return permissions.EffectAllow, nil
	case api.EffectDeny:
		return permissions.EffectDeny, nil
	default:
		return "", fmt.Errorf("%w: invalid effect %q", permissions.ErrInvalidRule, eff)
	}
}

func toAPITimelineEntry(entry timeline.Entry) (api.TimelineEntry, error) {
	out := api.TimelineEntry{
		ID:        entry.ID,
		SessionID: entry.SessionID,
		Kind:      api.TimelineKind(entry.Kind),
		CreatedAt: entry.CreatedAt,
	}

	if entry.Message != nil {
		msg, err := toAPIMessage(entry.Message)
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
		out.Model = &api.ModelChange{
			From: entry.Model.From,
			To:   entry.Model.To,
		}
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
		out.Question = &api.QuestionRecord{Request: toAPIQuestionRequests([]tools.QuestionRequest{entry.Question.Request})[0], Response: toAPIQuestionResponse(entry.Question.Response, entry.Question.Request.Questions)}
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

	return out, nil
}

func toAPIFileChange(fc std.FileChange) api.FileChange {
	return api.FileChange{
		ID:         fc.ID,
		Path:       fc.Path,
		Source:     string(fc.Source),
		Tool:       fc.Tool,
		Kind:       string(fc.Kind),
		BeforeHash: fc.BeforeHash,
		AfterHash:  fc.AfterHash,
		TurnID:     fc.TurnID,
		MessageID:  fc.MessageID,
		ToolCallID: fc.ToolCallID,
		Additions:  fc.Additions,
		Deletions:  fc.Deletions,
		Timestamp:  fc.Timestamp,
	}
}

func toAPIAgent(ag workspace.Agent) api.Agent {
	return api.Agent{
		Name:        ag.Name,
		DisplayName: ag.DisplayName,
		Description: ag.Description,
		Models:      slices.Clone(ag.Models),
		Thinking:    session.AgentThinking(ag.Thinking),
		Context:     ag.Context,
	}
}

func toAPIArguments(args []workspace.Argument) []api.Argument {
	if args == nil {
		return nil
	}
	out := make([]api.Argument, len(args))
	for i, a := range args {
		out[i] = api.Argument{
			Name:        a.Name,
			Description: a.Description,
			Required:    a.Required,
			Default:     a.Default,
		}
	}
	return out
}

func toAPICommand(cmd workspace.Command) api.Command {
	return api.Command{
		Name:         cmd.Name,
		DisplayName:  cmd.DisplayName,
		Description:  cmd.Description,
		ArgumentHint: cmd.ArgumentHint,
		Arguments:    toAPIArguments(cmd.Arguments),
		Models:       slices.Clone(cmd.Models),
		Tools:        slices.Clone(cmd.Tools),
		Namespace:    cmd.Namespace,
	}
}

func toAPISkill(sk workspace.Skill) api.Skill {
	return api.Skill{
		Name:         sk.Name,
		DisplayName:  sk.DisplayName,
		Description:  sk.Description,
		UseWhen:      sk.UseWhen,
		Keywords:     slices.Clone(sk.Keywords),
		ArgumentHint: sk.ArgumentHint,
		Arguments:    toAPIArguments(sk.Arguments),
		Namespace:    sk.Namespace,
		Scope:        formatSkillScope(sk.Namespace),
		Instructions: sk.Instructions,
	}
}

func formatSkillScope(ns string) string {
	switch strings.ToLower(strings.TrimSpace(ns)) {
	case "workspace":
		return "Workspace"
	case "local", "project":
		return "Project"
	case "user":
		return "User"
	case "system", "builtin", "":
		return "Builtin"
	default:
		s := strings.TrimSpace(ns)
		if len(s) > 0 {
			return strings.ToUpper(s[:1]) + s[1:]
		}
		return "Builtin"
	}
}

func toAPIThinkingValues(kind string, values []string) []api.ThinkingValue {
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

func toAPIModel(providerName string, pm workspace.ProviderModel) api.Model {
	id := pm.ID
	if providerName != "" && !strings.HasPrefix(id, providerName+"/") {
		id = providerName + "/" + id
	}
	name := cmp.Or(pm.Name, pm.ID)
	label := cmp.Or(pm.Label, pm.Name, pm.ID)

	model := api.Model{
		ID:           id,
		Provider:     providerName,
		Name:         name,
		Label:        label,
		ContextLimit: pm.Limits.Context,
		OutputLimit:  pm.Limits.Output,
	}
	if pm.Capabilities != nil {
		for _, capability := range pm.Capabilities.Thinking {
			kind := strings.ToLower(strings.TrimSpace(capability.Type))
			if kind == "budget" {
				kind = "budget_tokens"
			}
			def := ""
			if capability.Default != nil {
				def = fmt.Sprint(capability.Default)
			}
			model.Thinking = append(model.Thinking, api.ThinkingCapability{Type: kind, Values: toAPIThinkingValues(kind, capability.AllowedEfforts), Default: def, Max: capability.MaxBudgetTokens})
		}
	}
	return model
}

func resolvedToAPIModel(rm *workspace.ResolvedModel) api.Model {
	if rm.Model != nil {
		return toAPIModel(rm.ProviderName, *rm.Model)
	}
	return api.Model{
		ID:       rm.QualifiedID,
		Provider: rm.ProviderName,
		Name:     rm.ModelID,
		Label:    cmp.Or(rm.ModelID, rm.QualifiedID),
	}
}

// FormatEvent converts an internal broker message into a public API event.
func (d *Daemon) FormatEvent(msg broker.Message) (api.Event, error) {
	switch ev := msg.Event.(type) {
	case session.EntryAddedEvent:
		apiEntry, err := toAPITimelineEntry(ev.Entry)
		if err != nil {
			return api.Event{
				ID:    msg.ID,
				Type:  api.EventError,
				Topic: msg.Topic,
				Payload: api.ErrorEvent{
					SessionID: ev.SessionID,
					Error:     err.Error(),
				},
			}, nil
		}
		return api.Event{
			ID:    msg.ID,
			Type:  api.EventEntryAdded,
			Topic: msg.Topic,
			Payload: api.EntryAddedEvent{
				SessionID: ev.SessionID,
				Entry:     apiEntry,
			},
		}, nil

	case session.ChunkEvent:
		var ch api.Chunk
		if converted := toAPIChunk(&ev.Chunk); converted != nil {
			ch = *converted
		}
		return api.Event{
			ID:    msg.ID,
			Type:  api.EventChunk,
			Topic: msg.Topic,
			Payload: api.ChunkEvent{
				SessionID: ev.SessionID,
				Chunk:     ch,
			},
		}, nil

	case session.PermissionRequestedEvent:
		return api.Event{
			ID:    msg.ID,
			Type:  api.EventPermissionRequested,
			Topic: msg.Topic,
			Payload: api.PermissionRequestedEvent{
				SessionID:   ev.SessionID,
				Permissions: toAPIPermissionRequests(ev.Permissions),
			},
		}, nil

	case session.QuestionRequestedEvent:
		return api.Event{ID: msg.ID, Type: api.EventQuestionRequested, Topic: msg.Topic, Payload: api.QuestionRequestedEvent{SessionID: ev.SessionID, Questions: toAPIQuestionRequests(ev.Questions)}}, nil

	case session.StatusChangedEvent:
		return api.Event{
			ID:    msg.ID,
			Type:  api.EventStatusChanged,
			Topic: msg.Topic,
			Payload: api.StatusChangedEvent{
				SessionID: ev.SessionID,
				Status:    api.SessionStatus(ev.Status),
			},
		}, nil

	case session.CompletedEvent:
		return api.Event{
			ID:    msg.ID,
			Type:  api.EventCompleted,
			Topic: msg.Topic,
			Payload: api.CompletedEvent{
				SessionID: ev.SessionID,
			},
		}, nil

	case session.ErrorEvent:
		return api.Event{
			ID:    msg.ID,
			Type:  api.EventError,
			Topic: msg.Topic,
			Payload: api.ErrorEvent{
				SessionID: ev.SessionID,
				Error:     ev.Error,
			},
		}, nil

	case session.CreatedEvent:
		return api.Event{
			ID:    msg.ID,
			Type:  api.EventSessionCreated,
			Topic: msg.Topic,
			Payload: api.SessionCreatedEvent{
				Workspace: ev.Workspace,
				Session:   toAPISession(ev.Session),
			},
		}, nil

	case session.UpdatedEvent:
		return api.Event{
			ID:    msg.ID,
			Type:  api.EventSessionUpdated,
			Topic: msg.Topic,
			Payload: api.SessionUpdatedEvent{
				Workspace: ev.Workspace,
				Session:   toAPISession(ev.Session),
			},
		}, nil

	case session.TaskUpdatedEvent:
		return api.Event{
			ID:    msg.ID,
			Type:  api.EventTaskUpdated,
			Topic: msg.Topic,
			Payload: api.TaskUpdatedEvent{
				SessionID: ev.SessionID,
				Task:      toAPITask(ev.Task),
			},
		}, nil

	case session.CommandStartedEvent:
		return api.Event{
			ID:    msg.ID,
			Type:  api.EventCommandStarted,
			Topic: msg.Topic,
			Payload: api.CommandStartedEvent{
				SessionID: ev.SessionID,
				ID:        ev.ID,
				Command:   ev.Command,
			},
		}, nil

	case session.QueueUpdatedEvent:
		return api.Event{
			ID:    msg.ID,
			Type:  api.EventQueueUpdated,
			Topic: msg.Topic,
			Payload: api.QueueUpdatedEvent{
				SessionID: ev.SessionID,
				Queue:     toAPIQueueItems(ev.Queue),
			},
		}, nil

	case session.TodosUpdatedEvent:
		return api.Event{
			ID:    msg.ID,
			Type:  api.EventTodosUpdated,
			Topic: msg.Topic,
			Payload: api.TodosUpdatedEvent{
				SessionID: ev.SessionID,
				Todos:     toAPITodos(ev.Todos),
			},
		}, nil

	case session.CompactionStartedEvent:
		return api.Event{
			ID:    msg.ID,
			Type:  api.EventCompactionStarted,
			Topic: msg.Topic,
			Payload: api.CompactionStartedEvent{
				SessionID: ev.SessionID,
				ID:        ev.ID,
				Trigger:   ev.Trigger,
				Focus:     ev.Focus,
			},
		}, nil

	case session.ContextUpdatedEvent:
		return api.Event{
			ID:    msg.ID,
			Type:  api.EventContextUpdated,
			Topic: msg.Topic,
			Payload: api.ContextUpdatedEvent{
				SessionID: ev.SessionID,
				Context:   ev.Context,
			},
		}, nil

	case session.DeletedEvent:
		return api.Event{
			ID:    msg.ID,
			Type:  api.EventSessionDeleted,
			Topic: msg.Topic,
			Payload: api.SessionDeletedEvent{
				Workspace: ev.Workspace,
				SessionID: ev.SessionID,
			},
		}, nil

	case api.EventPayload:
		return api.Event{
			ID:      msg.ID,
			Type:    api.EventType(ev.EventType()),
			Topic:   msg.Topic,
			Payload: ev,
		}, nil

	default:
		return api.Event{ID: msg.ID}, fmt.Errorf("daemon: unknown broker event type %T", msg.Event)
	}
}
