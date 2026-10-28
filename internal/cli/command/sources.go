package command

import (
	"context"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/ui/autocomplete"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

// Sources snapshots the registered commands as headless autocomplete sources.
func (r *Registry) Sources() []autocomplete.Source {
	commands := r.List()
	candidates := make([]autocomplete.Candidate, len(commands))
	sources := []autocomplete.Source{{
		Name:      "commands",
		Trigger:   "/",
		StartOnly: true,
	}}

	for i, cmd := range commands {
		keys := []string{cmd.Name}
		seen := map[string]bool{cmd.Name: true}
		for _, alias := range cmd.Aliases {
			key := normalizeCommandKey(alias)
			if key != "" && !seen[key] {
				keys = append(keys, key)
				seen[key] = true
			}
		}

		candidates[i] = autocomplete.Candidate{
			Value:       cmd.Name,
			Label:       cmd.Usage,
			Description: cmd.Description,
			Aliases:     keys[1:],
			Data:        cmd,
		}
		if cmd.Complete == nil {
			continue
		}
		candidates[i].InsertText = cmd.Name + " "
		for _, key := range keys {
			sources = append(sources, autocomplete.Source{
				Name:      "commands:" + key,
				Trigger:   "/" + key + " ",
				StartOnly: true,
				Complete:  cmd.Complete,
			})
		}
	}

	sources[0].Complete = autocomplete.Static(candidates...)
	return sources
}

func completeModes(ctx context.Context, _ autocomplete.Request) ([]autocomplete.Candidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	options := modeOptionsWithActive("")
	candidates := make([]autocomplete.Candidate, len(options))
	for i, opt := range options {
		candidates[i] = autocomplete.Candidate{
			Value:       opt.Value,
			Label:       opt.Label,
			Description: opt.Description,
			Data:        api.PermissionMode(opt.Value),
		}
	}
	return candidates, nil
}

func completeModels(ctx context.Context, _ autocomplete.Request) ([]autocomplete.Candidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	c := store.Client()
	if c == nil {
		return nil, nil
	}

	models, err := c.ListModels(ctx)
	if err != nil {
		return nil, err
	}

	candidates := make([]autocomplete.Candidate, len(models))
	for i, m := range models {
		label := m.ID
		if m.Label != "" {
			label = m.Label
		} else if m.Name != "" {
			label = m.Name
		}
		candidates[i] = autocomplete.Candidate{
			Value:       m.ID,
			Label:       label,
			Description: m.Provider,
		}
	}
	return candidates, nil
}

func completeAgents(ctx context.Context, _ autocomplete.Request) ([]autocomplete.Candidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	c := store.Client()
	if c == nil {
		return nil, nil
	}

	agents, err := c.ListAgents(ctx)
	if err != nil {
		return nil, err
	}

	candidates := make([]autocomplete.Candidate, len(agents))
	for i, a := range agents {
		candidates[i] = autocomplete.Candidate{
			Value:       a.Name,
			Label:       a.Name,
			Description: a.Description,
		}
	}
	return candidates, nil
}

func completeEffort(ctx context.Context, _ autocomplete.Request) ([]autocomplete.Candidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	model, err := store.CurrentThinkingModel(ctx)
	if err != nil {
		return nil, nil
	}
	var candidates []autocomplete.Candidate
	for _, capability := range model.Thinking {
		if capability.Type == "effort" {
			for _, value := range capability.Values {
				candidates = append(candidates, autocomplete.Candidate{Value: value.Value, Label: value.Value, Description: value.Description})
			}
			candidates = append(candidates, autocomplete.Candidate{Value: "default", Label: "default"})
		}
	}
	return candidates, nil
}

func completeThemes(ctx context.Context, _ autocomplete.Request) ([]autocomplete.Candidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	themes := theme.List()
	candidates := make([]autocomplete.Candidate, len(themes))
	for i, name := range themes {
		candidates[i] = autocomplete.Candidate{
			Value: name,
			Label: name,
		}
	}
	return candidates, nil
}
