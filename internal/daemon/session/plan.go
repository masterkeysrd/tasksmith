package session

import (
	"cmp"
	"context"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/timeline"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools/builtin"
)

var _ builtin.PlanController = (*Session)(nil)

// PlansDir returns the session-scoped safe plans directory (<session-dir>/plans).
func (s *Session) PlansDir() string {
	return s.plansDir()
}

// CurrentMode returns the session's active permission mode.
func (s *Session) CurrentMode() api.PermissionMode {
	s.Lock()
	defer s.Unlock()
	return cmp.Or(s.Config.Mode, api.ModeDefault)
}

// EnterPlanMode transitions the session into plan mode, emitting a timeline
// mode entry and session update if the mode changed.
func (s *Session) EnterPlanMode(ctx context.Context) (bool, string, error) {
	plansDir := s.plansDir()

	s.Lock()
	oldMode := cmp.Or(s.Config.Mode, api.ModeDefault)
	if oldMode == api.ModePlan {
		s.Unlock()
		return true, plansDir, nil
	}
	prevUpdated := s.UpdatedAt
	s.Config.Mode = api.ModePlan
	s.UpdatedAt = time.Now().UTC()
	s.PermManager.SetMode(api.ModePlan)
	s.Unlock()

	if err := s.checkpoint(ctx); err != nil {
		s.Lock()
		s.Config.Mode = oldMode
		s.UpdatedAt = prevUpdated
		s.PermManager.SetMode(oldMode)
		s.Unlock()
		return false, plansDir, err
	}

	entry := timeline.NewModeEntry(api.ModeChange{From: oldMode, To: api.ModePlan})
	if err := s.Timeline.Append(ctx, entry); err != nil {
		return false, plansDir, err
	}
	publishEntryAdded(s.publisher, s.ID, entry)
	s.publisher.Publish(api.TopicSessions, api.SessionUpdatedEvent{
		Workspace: s.workspace.RootDir(),
		Session:   ToAPISession(s),
	})
	return false, plansDir, nil
}

// ExitPlanMode transitions the session out of plan mode into nextMode after
// user approval of a plan document.
func (s *Session) ExitPlanMode(ctx context.Context, _ string, nextMode api.PermissionMode) (api.PermissionMode, error) {
	switch nextMode {
	case api.ModeAcceptEdits, api.ModeDefault, api.ModeAuto:
	default:
		nextMode = api.ModeDefault
	}

	s.Lock()
	oldMode := cmp.Or(s.Config.Mode, api.ModeDefault)
	if oldMode == nextMode {
		s.Unlock()
		return nextMode, nil
	}
	prevUpdated := s.UpdatedAt
	s.Config.Mode = nextMode
	s.UpdatedAt = time.Now().UTC()
	s.PermManager.SetMode(nextMode)
	s.Unlock()

	if err := s.checkpoint(ctx); err != nil {
		s.Lock()
		s.Config.Mode = oldMode
		s.UpdatedAt = prevUpdated
		s.PermManager.SetMode(oldMode)
		s.Unlock()
		return oldMode, err
	}

	entry := timeline.NewModeEntry(api.ModeChange{From: oldMode, To: nextMode})
	if err := s.Timeline.Append(ctx, entry); err != nil {
		return nextMode, err
	}
	publishEntryAdded(s.publisher, s.ID, entry)
	s.publisher.Publish(api.TopicSessions, api.SessionUpdatedEvent{
		Workspace: s.workspace.RootDir(),
		Session:   ToAPISession(s),
	})
	return nextMode, nil
}
