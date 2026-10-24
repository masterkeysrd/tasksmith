package store

import (
	"cmp"
	"context"
	"strings"

	"github.com/masterkeysrd/kite/extras/kites"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
)

// SessionState holds the active session and its configuration, permission mode,
// and display model label.
type SessionState struct {
	Session    *api.Session
	Config     api.SessionConfig
	ModelLabel string
	Thinking   string
	Ready      bool
}

var session = kites.Create(SessionState{Config: api.SessionConfig{Mode: api.ModeDefault}})

// UseSession subscribes a component to a comparable slice of SessionState.
func UseSession[U comparable](selector func(SessionState) U) U {
	return kites.Use(session, selector)
}

// GetSession returns the current SessionState snapshot.
func GetSession() SessionState {
	return session.Get()
}

// UseMode returns the active permission mode, defaulting to api.ModeDefault if unset.
func UseMode() api.PermissionMode {
	return UseSession(func(s SessionState) api.PermissionMode {
		if s.Config.Mode == "" {
			return api.ModeDefault
		}
		return s.Config.Mode
	})
}

// SetSession updates the session and its configuration. If sess is non-nil,
// Mode defaults to api.ModeDefault if empty.
func SetSession(sess *api.Session) {
	session.Set(func(s SessionState) SessionState {
		s.Session = sess
		if sess != nil {
			s.Config = sess.Config
			if s.Config.Mode == "" {
				s.Config.Mode = api.ModeDefault
			}
		}
		return s
	})
}

// NextPermissionMode returns the next permission mode in the cycle sequence:
// default -> accept_edits -> plan -> ask -> auto -> default.
func NextPermissionMode(current api.PermissionMode) api.PermissionMode {
	switch current {
	case "", api.ModeDefault:
		return api.ModeAcceptEdits
	case api.ModeAcceptEdits:
		return api.ModePlan
	case api.ModePlan:
		return api.ModeAsk
	case api.ModeAsk:
		return api.ModeAuto
	default:
		return api.ModeDefault
	}
}

// CycleMode atomically advances to the next permission mode in sequence and returns the new mode.
func CycleMode() api.PermissionMode {
	var next api.PermissionMode
	session.Set(func(s SessionState) SessionState {
		next = NextPermissionMode(s.Config.Mode)
		s.Config.Mode = next
		return s
	})
	return next
}

// SetMode updates the active permission mode.
func SetMode(mode api.PermissionMode) {
	session.Set(func(s SessionState) SessionState {
		s.Config.Mode = mode
		return s
	})
}

// SetModelInfo sets the display model label and reasoning effort.
func SetModelInfo(label, thinking string) {
	session.Set(func(s SessionState) SessionState {
		s.ModelLabel = label
		s.Thinking = thinking
		s.Ready = true
		return s
	})
}

// ResetSession resets the session state to its initial default mode.
func ResetSession() {
	session.Set(func(SessionState) SessionState {
		return SessionState{Config: api.SessionConfig{Mode: api.ModeDefault}}
	})
}

// ResolveModelInfo returns the display label and reasoning effort for the
// given agent and model, falling back to the workspace and agent defaults for
// blank values. When sess is set its agent and model take precedence.
func ResolveModelInfo(ctx context.Context, c *client.Client, cfgAgent, cfgModel string, sess *api.Session) (modelLabel, thinking string) {
	agentName := strings.TrimSpace(cfgAgent)
	modelID := strings.TrimSpace(cfgModel)

	if sess != nil {
		agentName = strings.TrimSpace(sess.Config.Agent)
		modelID = strings.TrimSpace(sess.Config.Model)
	}

	var ws *api.Workspace
	if c != nil && (agentName == "" || modelID == "") {
		ws, _ = c.GetWorkspace(ctx)
		if ws != nil && agentName == "" {
			agentName = strings.TrimSpace(ws.DefaultAgent)
		}
	}

	var ag *api.Agent
	if c != nil && agentName != "" {
		ag, _ = c.GetAgent(ctx, agentName)
	}

	if modelID == "" {
		if ag != nil && len(ag.Models) > 0 {
			modelID = strings.TrimSpace(ag.Models[0])
		}
		if modelID == "" && ws != nil {
			modelID = strings.TrimSpace(ws.DefaultModel)
		}
	}

	var mdl *api.Model
	if c != nil && modelID != "" {
		mdl, _ = c.GetModel(ctx, modelID)
	}

	var agEffort, mdlLabel, mdlEffort string
	if ag != nil {
		agEffort = strings.TrimSpace(ag.Effort)
	}
	if mdl != nil {
		mdlLabel = cmp.Or(strings.TrimSpace(mdl.Label), strings.TrimSpace(mdl.Name), strings.TrimSpace(mdl.ID))
		mdlEffort = strings.TrimSpace(mdl.DefaultEffort)
	}

	modelLabel = cmp.Or(mdlLabel, modelID, "default")
	thinking = cmp.Or(agEffort, mdlEffort, "auto")
	return modelLabel, thinking
}
