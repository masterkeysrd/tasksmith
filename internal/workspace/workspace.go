package workspace

import (
	"context"
	"fmt"

	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/warp"
)

type Workspace struct {
	cwd      string
	logger   log.Interface
	registry *warp.Registry
}

func New(cwd string) *Workspace {
	return &Workspace{
		cwd:    cwd,
		logger: log.ForComponent("workspace"),
	}
}

func (w *Workspace) Load(ctx context.Context) error {
	w.logger.Info("Loading workspace", log.String("cwd", w.cwd))
	provider := &systemProvider{}
	reg, err := warp.Load(w.cwd, provider)
	if err != nil {
		return fmt.Errorf("load workspace: %w", err)
	}
	w.registry = reg
	return nil
}

func (w *Workspace) Resources() []warp.Resource {
	resolver := w.resolver()
	if resolver == nil {
		return nil
	}

	resources := resolver.ListResources(warp.QueryOptions{})
	return resources
}

func (w *Workspace) Projects() []*warp.Project {
	if w.registry == nil {
		return nil
	}

	projects := w.registry.ListProjects()
	return projects
}

func (w *Workspace) Agents() []*warp.Agent {
	resolver := w.resolver()
	if resolver == nil {
		return nil
	}

	resources := resolver.ListResources(warp.QueryOptions{
		Kinds: []string{string(warp.KindAgent)},
	})
	agents := make([]*warp.Agent, 0, len(resources))
	for _, r := range resources {
		if agent, ok := r.(*warp.Agent); ok {
			agents = append(agents, agent)
		}
	}
	return agents
}

func (w *Workspace) Providers() []*warp.ModelProvider {
	resolver := w.resolver()
	if resolver == nil {
		return nil
	}

	resources := resolver.ListResources(warp.QueryOptions{
		Kinds: []string{string(warp.KindModelProvider)},
	})
	providers := make([]*warp.ModelProvider, 0, len(resources))
	for _, r := range resources {
		if provider, ok := r.(*warp.ModelProvider); ok {
			providers = append(providers, provider)
		}
	}
	return providers
}

func (w *Workspace) resolver() warp.Resolver {
	if w.registry == nil {
		return nil
	}

	project, ok := w.registry.ProjectFromPath(w.cwd)
	if ok {
		return w.registry.Project(project.Name)
	}
	return w.registry
}
