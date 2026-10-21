package daemon

import (
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/session"
	"github.com/masterkeysrd/tasksmith/internal/std"
	"github.com/masterkeysrd/tasksmith/internal/tools"
)

func toAPISession(sess *session.Session, status api.SessionStatus) api.Session {
	if sess == nil {
		return api.Session{}
	}
	return api.Session{
		ID:        sess.ID,
		Title:     sess.Title,
		Agent:     sess.Agent,
		Model:     sess.Model,
		Status:    status,
		CreatedAt: sess.CreatedAt,
		UpdatedAt: sess.UpdatedAt,
	}
}

func toAPIMessage(msg message.Message) *api.Message {
	if msg == nil {
		return nil
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
	}

	return out
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

func fromSendRequest(req api.SendRequest) (message.Message, error) {
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
		return nil, ErrEmptyMessage
	}
	return message.NewUser(blocks...), nil
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

func fromAPIPermissionDecision(dec api.PermissionDecision) tools.PermissionDecision {
	return tools.PermissionDecision{
		RequestID:    dec.RequestID,
		ToolCallID:   dec.ToolCallID,
		Kind:         tools.PermissionDecisionKind(dec.Kind),
		GrantPattern: dec.GrantPattern,
		Feedback:     dec.Feedback,
	}
}

func toAPIEvent(workspaceDir string, ev session.Event) api.Event {
	return api.Event{
		Type:        api.EventType(ev.Type),
		Workspace:   workspaceDir,
		SessionID:   ev.SessionID,
		Message:     toAPIMessage(ev.Message),
		Chunk:       toAPIChunk(ev.Chunk),
		Permissions: toAPIPermissionRequests(ev.Permissions),
	}
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
		Additions:  fc.Additions,
		Deletions:  fc.Deletions,
		Timestamp:  fc.Timestamp,
	}
}
