package command

import (
	"cmp"
	"context"
	"strings"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/ui/autocomplete"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

// Sources snapshots the registered commands as headless autocomplete sources.
func (r *Registry) Sources() []autocomplete.Source {
	commands := r.List()
	var commandCandidates, skillCandidates []autocomplete.Candidate
	sources := []autocomplete.Source{{
		Name:      "commands",
		Trigger:   "/",
		StartOnly: true,
	}, {
		Name:        "files",
		Trigger:     "@",
		Passthrough: true,
		Complete:    completeFiles,
	}, {
		Name:     "rules",
		Trigger:  "@rule:",
		Complete: completeRules,
	}, {
		Name:     "skills",
		Trigger:  "@skill:",
		Complete: completeSkills,
	}, {
		// MCP prompts and resources are invoked by reference, so their trigger
		// includes the leading character and candidates insert only the body.
		Name:     "mcp-prompts",
		Trigger:  "/mcp:",
		Complete: completeMCPPrompts,
	}, {
		Name:     "mcp-resources",
		Trigger:  "@mcp:",
		Complete: completeMCPResources,
	}}

	for _, cmd := range commands {
		keys := []string{cmd.Name}
		seen := map[string]bool{cmd.Name: true}
		for _, alias := range cmd.Aliases {
			key := normalizeCommandKey(alias)
			if key != "" && !seen[key] {
				keys = append(keys, key)
				seen[key] = true
			}
		}

		candidate := autocomplete.Candidate{
			Value:       cmd.Name,
			Label:       cmd.Usage,
			Description: cmd.Description,
			Aliases:     keys[1:],
			Data:        cmd,
		}
		if cmd.Complete != nil {
			candidate.InsertText = cmd.Name + " "
		}
		if cmd.skill {
			skillCandidates = append(skillCandidates, candidate)
		} else {
			commandCandidates = append(commandCandidates, candidate)
		}
		if cmd.Complete == nil {
			continue
		}
		for _, key := range keys {
			sources = append(sources, autocomplete.Source{
				Name:      "commands:" + key,
				Trigger:   "/" + key + " ",
				StartOnly: true,
				Complete:  cmd.Complete,
			})
		}
	}

	// The /mcp subcommands take a server name as their second argument, which the
	// command's own "/mcp " trigger cannot reach, so they register their own.
	sources = append(sources, mcpServerNameSources()...)

	candidates := make([]autocomplete.Candidate, 0, len(commands))
	for i := 0; i < len(commandCandidates) || i < len(skillCandidates); i++ {
		if i < len(commandCandidates) {
			candidates = append(candidates, commandCandidates[i])
		}
		if i < len(skillCandidates) {
			candidates = append(candidates, skillCandidates[i])
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

	candidates := make([]autocomplete.Candidate, 0, len(models))
	for _, m := range models {
		if !m.Capabilities.SupportsCompletion() {
			continue
		}
		label := m.ID
		if m.Label != "" {
			label = m.Label
		} else if m.Name != "" {
			label = m.Name
		}
		candidates = append(candidates, autocomplete.Candidate{
			Value:       m.ID,
			Label:       label,
			Description: m.Provider,
			// Aliases answer to the name users actually type ("@fast") while the
			// canonical model id is what gets inserted.
			Aliases: modelAliasRefs(m.Aliases),
		})
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
			Description: agentDescription(a),
		}
	}
	return candidates, nil
}

// completeRules suggests workspace rules for the @rule: attachment trigger,
// inserting @rule:<name> so the agent auto-injects the rule's instructions.
func completeRules(ctx context.Context, _ autocomplete.Request) ([]autocomplete.Candidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	c := store.Client()
	if c == nil {
		return nil, nil
	}

	rules, err := c.ListRules(ctx)
	if err != nil {
		return nil, err
	}

	candidates := make([]autocomplete.Candidate, 0, len(rules))
	for _, r := range rules {
		candidates = append(candidates, autocomplete.Candidate{
			Value:       r.Name,
			Label:       r.Name,
			Description: cmp.Or(r.Description, r.Scope),
			InsertText:  r.Name + " ",
			Data:        r,
		})
	}
	return candidates, nil
}

// completeSkills suggests workspace skills for the @skill: attachment trigger,
// inserting @skill:<name> so the agent auto-injects the skill's instructions.
func completeSkills(ctx context.Context, _ autocomplete.Request) ([]autocomplete.Candidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	c := store.Client()
	if c == nil {
		return nil, nil
	}

	skills, err := c.ListSkills(ctx)
	if err != nil {
		return nil, err
	}

	candidates := make([]autocomplete.Candidate, 0, len(skills))
	for _, sk := range skills {
		candidates = append(candidates, autocomplete.Candidate{
			Value:       sk.Name,
			Label:       sk.Name,
			Description: cmp.Or(sk.Description, sk.Scope),
			InsertText:  sk.Name + " ",
			Data:        sk,
		})
	}
	return candidates, nil
}

// completeFiles suggests workspace files and directories for the @ attachment trigger.
func completeFiles(ctx context.Context, req autocomplete.Request) ([]autocomplete.Candidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	c := store.Client()
	if c == nil {
		return nil, nil
	}

	var sessionID string
	if sessState := store.GetSession(); sessState.Session != nil {
		sessionID = sessState.Session.ID
	}

	query := strings.TrimPrefix(req.Query, "file:")
	query = strings.Trim(query, "\"'")

	files, err := c.ListWorkspaceFiles(ctx, api.ListFilesRequest{
		Query:     query,
		SessionID: sessionID,
		Limit:     50,
	})
	if err != nil {
		return nil, nil
	}

	candidates := make([]autocomplete.Candidate, 0, len(files))
	for _, f := range files {
		label := f.Name
		if f.IsDir {
			label = strings.TrimSuffix(label, "/") + "/"
		}
		if !f.Allowed {
			label += " [Prohibited]"
		}

		desc := f.Dir
		if f.Recent {
			if desc != "" {
				desc += " · recent"
			} else {
				desc = "· recent"
			}
		}

		short := f.ShortPath
		if short == "" {
			short = f.Path
			if f.IsDir && !strings.HasSuffix(short, "/") {
				short += "/"
			}
		}

		candidates = append(candidates, autocomplete.Candidate{
			Value:       f.Path,
			Label:       label,
			Description: desc,
			InsertText:  autocomplete.Quote(short) + " ",
			Data:        f,
		})
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

// completeThinking offers the thinking strategies the active model declares, so
// an adaptive-only model does not suggest on/off and vice versa.
func completeThinking(ctx context.Context, _ autocomplete.Request) ([]autocomplete.Candidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	model, err := newThinkingModel(ctx)
	if err != nil {
		return nil, nil
	}
	var candidates []autocomplete.Candidate
	if _, ok := model.capability("toggle"); ok {
		candidates = append(candidates,
			autocomplete.Candidate{Value: "on", Label: "on", Description: "Enable thinking"},
			autocomplete.Candidate{Value: "off", Label: "off", Description: "Disable thinking"},
		)
	}
	if _, ok := model.capability("adaptive"); ok {
		candidates = append(candidates, autocomplete.Candidate{Value: "adaptive", Label: "adaptive", Description: "Let the model decide"})
	}
	return append(candidates, autocomplete.Candidate{Value: "default", Label: "default"}), nil
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
