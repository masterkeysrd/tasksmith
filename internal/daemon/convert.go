package daemon

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/broker"
	"github.com/masterkeysrd/tasksmith/internal/daemon/permissions"
	"github.com/masterkeysrd/tasksmith/internal/daemon/session"
	"github.com/masterkeysrd/tasksmith/internal/daemon/std"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
)

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
	for i, b := range req.Blocks {
		if b.Type != api.BlockText {
			return session.SendInput{}, fmt.Errorf("%w: block %d of type %q is not supported in user messages", session.ErrInvalidConfig, i, b.Type)
		}
		converted, ok := fromAPIBlock(b)
		if !ok {
			return session.SendInput{}, fmt.Errorf("%w: invalid or unsupported block %d of type %q", session.ErrInvalidConfig, i, b.Type)
		}
		blocks = append(blocks, converted)
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

func fromAPIQuestionResponse(r api.QuestionResponse) tools.QuestionResponse {
	out := tools.QuestionResponse{
		RequestID:  r.RequestID,
		ToolCallID: r.ToolCallID,
		Canceled:   r.Canceled,
	}
	for _, a := range r.Answers {
		answer := tools.QuestionAnswer{
			Question: a.Question,
			Selected: slices.Clone(a.Selected),
			Text:     a.Text,
		}
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

// FormatEvent converts an internal broker message into a public API event.
// Sessions publish [api.EventPayload] values directly, so this only needs to
// wrap the payload with the message envelope.
func (d *Daemon) FormatEvent(msg broker.Message) (api.Event, error) {
	switch ev := msg.Event.(type) {
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
