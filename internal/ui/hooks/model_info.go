package hooks

import (
	"cmp"
	"context"
	"strings"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/kite/promise"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
	uiclient "github.com/masterkeysrd/tasksmith/internal/ui/client"
)

// ModelInfo is the display name and reasoning effort of the active session's
// model. Ready is false until they have been resolved.
type ModelInfo struct {
	Label    string
	Thinking string
	Ready    bool
}

type modelInfoKey struct {
	Socket string
	CWD    string
	ID     string
	Agent  string
	Model  string
}

func newModelInfoKey(c *client.Client, s api.Session) modelInfoKey {
	return modelInfoKey{Socket: c.SocketPath(), CWD: c.CWD(), ID: s.ID, Agent: s.Agent, Model: s.Model}
}

// UseModelInfo resolves the label and effort for the active session, asking
// the daemon for the model unless the provider already knows them. While a new
// session's model is being resolved it keeps reporting the previous result.
func UseModelInfo() ModelInfo {
	scope := useSessionScope()
	c := uiclient.UseClient()
	key := newModelInfoKey(c, scope.state.Session.Session)
	known := scope.initialInfo.Ready && key == scope.initialKey

	fetch := promise.WrapWithProps(func(ctx context.Context, k modelInfoKey) (ModelInfo, error) {
		label, thinking := ResolveModelInfo(ctx, c, k.Agent, k.Model, nil)
		return ModelInfo{Label: label, Thinking: thinking, Ready: true}, nil
	})
	result := wind.Use(key, fetch, wind.Options{Enabled: !known})

	last := kitex.UseRef(ModelInfo{})
	switch {
	case known:
		last.Current = scope.initialInfo
	case !result.IsLoading && !result.IsError && result.Data.Ready:
		last.Current = result.Data
	}
	return last.Current
}

// ResolveModelInfo returns the display label and reasoning effort for the
// given agent and model, falling back to the workspace and agent defaults for
// blank values. When sess is set its agent and model take precedence.
func ResolveModelInfo(ctx context.Context, c *client.Client, cfgAgent, cfgModel string, sess *api.Session) (modelLabel, thinking string) {
	agentName := strings.TrimSpace(cfgAgent)
	modelID := strings.TrimSpace(cfgModel)

	if sess != nil {
		agentName = strings.TrimSpace(sess.Agent)
		modelID = strings.TrimSpace(sess.Model)
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
