package acp

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"time"

	sdk "github.com/coder/acp-go-sdk"
	"github.com/masterkeysrd/tasksmith/api"
)

type translation struct {
	cwd                             string
	calls                           map[string]*api.ToolCall
	streamedText, streamedReasoning int
	journalOnly                     bool
	childID                         string
	idle                            *sdk.StateUpdateIdle
	caps                            sdk.ClientCapabilities
	usage                           sdk.SessionUsageUpdate
	compaction                      sdk.CompactionId
}

func childState(id sdk.SessionId, state sdk.StateUpdate) sdk.SessionUpdate {
	return sdk.SessionUpdate{
		SubagentUpdate: &sdk.SessionSubagentUpdate{
			SessionId: id,
			State:     sdk.PatchSet(state),
		},
	}
}

func (t *translation) announcement(sess *api.Session) sdk.SessionUpdate {
	state := sdk.StateUpdate{
		Running: &sdk.StateUpdateRunning{},
	}
	if sess.Status == api.StatusIdle {
		idle := t.idle
		if idle == nil {
			idle = &sdk.StateUpdateIdle{
				None: &sdk.StateUpdateIdleNone{},
			}
		}
		state = sdk.StateUpdate{Idle: idle}
	} else {
		t.idle = nil
	}

	update := childState(sdk.SessionId(t.childID), state)
	update.SubagentUpdate.Title = sdk.PatchSet(sess.Title)
	update.SubagentUpdate.Cwd = sdk.PatchSet(t.cwd)
	return update
}

func (t *translation) finish(payload api.EventPayload) *sdk.SessionUpdate {
	switch p := payload.(type) {
	case api.ErrorEvent:
		t.idle = &sdk.StateUpdateIdle{
			Error: &sdk.StateUpdateIdleError{},
		}
	case api.EntryAddedEvent:
		record := p.Entry.TurnCompleted
		switch {
		case p.Entry.Kind == api.TimelineKindInterrupted || (record != nil && record.Status == api.TurnStatusCanceled):
			t.idle = &sdk.StateUpdateIdle{
				Cancelled: &sdk.StateUpdateIdleCancelled{},
			}
		case record != nil && record.Status == api.TurnStatusFailed:
			t.idle = &sdk.StateUpdateIdle{
				Error: &sdk.StateUpdateIdleError{},
			}
		default:
			return nil
		}
	case api.CompletedEvent:
		if t.idle != nil {
			return nil
		}
		t.idle = &sdk.StateUpdateIdle{
			EndTurn: &sdk.StateUpdateIdleEndTurn{},
		}
	default:
		return nil
	}

	update := childState(sdk.SessionId(t.childID), sdk.StateUpdate{Idle: t.idle})
	return &update
}

func warning(message string) sdk.SessionUpdate {
	return sdk.SessionUpdate{
		Notice: &sdk.SessionUpdateNotice{
			Severity: sdk.NoticeSeverityWarning,
			Title:    message,
		},
	}
}

func (t *translation) display(id string, event api.EventPayload) []sdk.SessionUpdate {
	var update sdk.SessionUpdate
	switch p := event.(type) {
	case api.ChunkEvent:
		return t.chunk(p.Chunk)
	case api.EntryAddedEvent:
		return t.entry(p.Entry, false)
	case api.StatusChangedEvent:
		var updates []sdk.SessionUpdate
		if p.Status == api.StatusExecuting {
			for callID := range t.calls {
				updates = append(updates, sdk.UpdateToolCall(sdk.ToolCallId(callID),
					sdk.WithUpdateStatus(sdk.ToolCallStatusInProgress)))
			}
		}
		return updates
	case api.TodosUpdatedEvent:
		update = t.plan(p.Todos)
	case api.SessionUpdatedEvent:
		if p.Session.ID != id {
			return nil
		}
		info := &sdk.SessionSessionInfoUpdate{Title: sdk.PatchSet(p.Session.Title)}
		if !p.Session.UpdatedAt.IsZero() {
			info.UpdatedAt = sdk.PatchSet(p.Session.UpdatedAt.Format(time.RFC3339))
		}
		update.SessionInfoUpdate = info
	case api.ContextUpdatedEvent:
		t.usage.Used = int64(p.Context.UsedTokens)
		t.usage.Size = int64(p.Context.MaxTokens)
		update.UsageUpdate = new(t.usage)
	case api.UsageUpdatedEvent:
		total := p.Usage.Totals
		t.usage.Cost = nil
		if total.CostNano != 0 || total.Calls > total.QuotaCalls+total.UnpricedCalls {
			t.usage.Cost = &sdk.Cost{
				Amount:   float64(total.CostNano) / 1e9,
				Currency: "USD",
			}
		}
		update.UsageUpdate = new(t.usage)
	case api.CompactionStartedEvent:
		if t.caps.Session == nil || t.caps.Session.Compaction == nil {
			return nil
		}
		t.compaction = sdk.CompactionId(p.ID)
		update.CompactionUpdate = &sdk.SessionCompactionUpdate{
			CompactionId: t.compaction,
			Status:       sdk.CompactionStatusInProgress,
		}
	case api.QuotaUpdatedEvent:
		bucket, ok := p.Quota.Bottleneck()
		if t.caps.Session == nil || t.caps.Session.Notices == nil || !ok || !bucket.Exhausted() {
			return nil
		}
		update = warning("Rate limit reached for " + p.Quota.Model)
	default:
		return nil
	}

	return []sdk.SessionUpdate{update}
}

func newTranslation(cwd string) *translation {
	return &translation{
		cwd:   cwd,
		calls: make(map[string]*api.ToolCall),
	}
}

func kind(name string) sdk.ToolKind {
	switch name {
	case "view", "ls":
		return sdk.ToolKindRead
	case "glob", "grep":
		return sdk.ToolKindSearch
	case "edit", "write":
		return sdk.ToolKindEdit
	case "remove":
		return sdk.ToolKindDelete
	case "bash":
		return sdk.ToolKindExecute
	case "web_fetch", "web_search", "download":
		return sdk.ToolKindFetch
	case "invoke_agent":
		return sdk.ToolKindThink
	}

	return sdk.ToolKindOther
}

func (t *translation) path(c *api.ToolCall) string {
	path, _ := c.Args["path"].(string)
	if path != "" && !filepath.IsAbs(path) {
		return filepath.Join(t.cwd, path)
	}

	return path
}

func (t *translation) plan(items []api.TodoItem) sdk.SessionUpdate {
	entries := make([]sdk.PlanEntry, 0, len(items))
	for _, v := range items {
		entries = append(entries, sdk.PlanEntry{
			Content:  v.Content,
			Status:   sdk.PlanEntryStatus(v.Status),
			Priority: sdk.PlanEntryPriorityMedium,
		})
	}

	if t.caps.Plan == nil {
		return sdk.UpdatePlan(entries...)
	}
	if len(entries) == 0 {
		return sdk.SessionUpdate{
			PlanRemoved: &sdk.SessionUpdatePlanRemoved{PlanId: "todos"},
		}
	}
	return sdk.SessionUpdate{
		PlanUpdate: &sdk.SessionPlanUpdate{Plan: sdk.NewPlanUpdateContentItems("todos", entries)},
	}
}

func (t *translation) chunk(c api.Chunk) (out []sdk.SessionUpdate) {
	for _, b := range c.Content {
		if b.Text == "" {
			continue
		}

		switch b.Type {
		case api.BlockText:
			t.streamedText += len(b.Text)
			out = append(out, sdk.UpdateAgentMessageText(b.Text))
		case api.BlockReasoning:
			t.streamedReasoning += len(b.Text)
			out = append(out, sdk.UpdateAgentThoughtText(b.Text))
		}
	}

	return
}

func (t *translation) entry(e api.TimelineEntry, replay bool) (out []sdk.SessionUpdate) {
	if rec := e.Compacted; rec != nil && t.caps.Session != nil && t.caps.Session.Compaction != nil {
		id := t.compaction
		if id == "" {
			id = sdk.CompactionId(e.ID)
		}
		t.compaction = ""
		summary := sdk.TextBlock(rec.Summary)
		if rec.Summary != "" {
			out = append(out, sdk.SessionUpdate{
				CompactionSummaryChunk: &sdk.SessionUpdateCompactionSummaryChunk{
					CompactionId: id,
					Content:      summary,
				},
			})
		}
		return append(out, sdk.SessionUpdate{
			CompactionUpdate: &sdk.SessionCompactionUpdate{
				CompactionId: id,
				Status:       sdk.CompactionStatusCompleted,
				Summary:      sdk.PatchSet([]sdk.ContentBlock{summary}),
			},
		})
	}
	if !replay && e.Mode != nil && e.Mode.To != "" {
		out = append(out, sdk.SessionUpdate{
			CurrentModeUpdate: &sdk.SessionCurrentModeUpdate{CurrentModeId: sdk.SessionModeId(e.Mode.To)},
		})
	}

	m := e.Message
	if m == nil {
		return
	}
	if m.Role == api.RoleTool {
		return t.result(m)
	}

	for _, b := range m.Content {
		if c := b.ToolCall; c != nil {
			t.calls[c.ID] = c
			opts := []sdk.ToolCallStartOpt{
				sdk.WithStartName(c.Name),
				sdk.WithStartKind(kind(c.Name)),
				sdk.WithStartRawInput(c.Args),
				sdk.WithStartStatus(sdk.ToolCallStatusPending),
			}
			if path := t.path(c); path != "" {
				opts = append(opts, sdk.WithStartLocations([]sdk.ToolCallLocation{
					{Path: path},
				}))
			}

			id := sdk.ToolCallId(c.ID)
			out = append(out, sdk.StartToolCall(id, c.Name, opts...))
			continue
		}

		if !replay && m.Role == api.RoleUser {
			continue
		}
		if !replay && m.Role == api.RoleAssistant {
			switch b.Type {
			case api.BlockText:
				b.Text = t.remaining(b.Text, &t.streamedText)
			case api.BlockReasoning:
				b.Text = t.remaining(b.Text, &t.streamedReasoning)
			}
			if b.Text == "" {
				continue
			}
		}

		switch {
		case b.Type == api.BlockReasoning:
			out = append(out, sdk.UpdateAgentThoughtText(b.Text))
		case b.Type == api.BlockText && m.Role == api.RoleAssistant:
			out = append(out, sdk.UpdateAgentMessageText(b.Text))
		case b.Type == api.BlockText && m.Role == api.RoleUser:
			out = append(out, sdk.UpdateUserMessageText(b.Text))
		}
	}

	if m.Role == api.RoleAssistant {
		t.streamedText = 0
		t.streamedReasoning = 0
	}

	return
}

func (t *translation) remaining(text string, delivered *int) string {
	if *delivered == 0 {
		return text
	}
	if !t.journalOnly {
		return ""
	}

	n := min(len(text), *delivered)
	*delivered -= n
	return text[n:]
}

func (t *translation) result(m *api.Message) (out []sdk.SessionUpdate) {
	status := sdk.ToolCallStatusCompleted
	if m.IsError {
		status = sdk.ToolCallStatusFailed
	}

	opts := []sdk.ToolCallUpdateOpt{sdk.WithUpdateStatus(status)}
	var content []sdk.ToolCallContent
	for _, b := range m.Content {
		if b.Text != "" {
			content = append(content, sdk.ToolContent(sdk.TextBlock(b.Text)))
		}
	}
	if len(m.StructuredContent) > 0 {
		opts = append(opts, sdk.WithUpdateRawOutput(m.StructuredContent))
	}

	c := t.calls[m.ToolCallID]
	if c != nil && !m.IsError {
		path := t.path(c)
		switch c.Name {
		case "edit", "write":
			var result api.WriteOutput
			if json.Unmarshal(m.StructuredContent, &result) == nil && result.Diff != "" {
				if result.Path != "" {
					path = result.Path
					if !filepath.IsAbs(path) {
						path = filepath.Join(t.cwd, path)
					}
				}
				content = append(content, hunkContent(path, result.Diff, result.Created)...)
				break
			}

			if c.Name == "write" {
				if text, ok := c.Args["content"].(string); ok && path != "" {
					content = append(content, sdk.ToolDiffContent(path, text))
				}
				break
			}

			old, ok := c.Args["old_text"].(string)
			newText, ok2 := c.Args["new_text"].(string)
			if path != "" && ok && ok2 {
				content = append(content, sdk.ToolDiffContent(path, newText, old))
			}
		case "todos":
			var result api.TodosOutput
			if json.Unmarshal(m.StructuredContent, &result) == nil && result.Todos != nil {
				out = append(out, t.plan(result.Todos))
			}
		}
	}

	delete(t.calls, m.ToolCallID)
	if len(content) > 0 {
		opts = append(opts, sdk.WithUpdateContent(content))
	}

	return append(out, sdk.UpdateToolCall(sdk.ToolCallId(m.ToolCallID), opts...))
}

func hunkContent(path, patch string, created bool) []sdk.ToolCallContent {
	var content []sdk.ToolCallContent
	var oldText, newText strings.Builder
	active := false

	flush := func() {
		if !active {
			return
		}

		if created {
			content = append(content, sdk.ToolDiffContent(path, newText.String()))
		} else {
			content = append(content, sdk.ToolDiffContent(path, newText.String(), oldText.String()))
		}

		oldText.Reset()
		newText.Reset()
	}

	for line := range strings.SplitAfterSeq(patch, "\n") {
		if strings.HasPrefix(line, "@@ ") {
			flush()
			active = true
			continue
		}
		if !active || line == "" {
			continue
		}

		switch line[0] {
		case ' ':
			oldText.WriteString(line[1:])
			newText.WriteString(line[1:])
		case '-':
			oldText.WriteString(line[1:])
		case '+':
			newText.WriteString(line[1:])
		}
	}

	flush()
	return content
}
