package daemon

import (
	"fmt"
	"slices"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/broker"
	"github.com/masterkeysrd/tasksmith/internal/daemon/permissions"
	"github.com/masterkeysrd/tasksmith/internal/daemon/std"
)

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
