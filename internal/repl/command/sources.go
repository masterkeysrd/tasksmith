package command

import (
	"context"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/ui/autocomplete"
)

// Sources snapshots the registered commands as headless autocomplete sources.
// Register commands before calling it. Argument completers run on demand with
// the engine's request context.
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
