package acp

import (
	"cmp"
	"context"
	"slices"
	"strconv"
	"strings"

	sdk "github.com/coder/acp-go-sdk"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
)

var modes = []string{
	"default",
	"accept_edits",
	"plan",
	"ask",
	"auto",
}

func selectOption(
	id, name, current string,
	category sdk.SessionConfigOptionCategory,
	values []string,
) sdk.SessionConfigOption {
	choices := make(sdk.SessionConfigSelectOptionsUngrouped, 0, len(values))
	for _, v := range values {
		choices = append(choices, sdk.SessionConfigSelectOption{
			Value: sdk.SessionConfigValueId(v),
			Name:  v,
		})
	}

	o := sdk.NewSessionConfigOptionSelect(
		sdk.SessionConfigValueId(current),
		sdk.SessionConfigSelectOptions{Ungrouped: &choices},
	)

	o.Select.Id = sdk.SessionConfigId(id)
	o.Select.Name = name
	o.Select.Category = &category

	return o
}

func thoughtValue(s *api.ThinkingSetting) string {
	if s == nil {
		return "default"
	}

	switch s.Type {
	case "toggle":
		if s.Enabled {
			return "on"
		}
		return "off"
	case "effort":
		return s.Effort
	case "budget":
		return "budget:" + strconv.Itoa(s.Budget)
	default:
		return cmp.Or(s.Type, "default")
	}
}

func configuration(ctx context.Context, c *client.Client, s *api.Session) ([]sdk.SessionConfigOption, error) {
	opts, err := modelOptions(ctx, c, s)
	if err != nil {
		return nil, err
	}

	mode := string(cmp.Or(s.Config.Mode, "default"))
	opts = append(opts, selectOption("mode", "Mode", mode, sdk.SessionConfigOptionCategoryMode, modes))

	levels := []string{"default"}
	if s.Model != nil {
		for _, cap := range s.Model.Thinking {
			switch cap.Type {
			case "effort":
				for _, v := range cap.Values {
					levels = append(levels, v.Value)
				}
			case "adaptive":
				levels = append(levels, "adaptive")
			case "toggle":
				levels = append(levels, "on", "off")
			case "budget_tokens":
				for _, n := range []int{
					cap.Min,
					cap.Max,
				} {
					if n > 0 {
						levels = append(levels, "budget:"+strconv.Itoa(n))
					}
				}
			}
		}
	}

	current := thoughtValue(s.Config.Thinking)
	if !slices.Contains(levels, current) {
		levels = append(levels, current)
	}
	opts = append(opts, selectOption(
		"thought_level", "Thought level", current,
		sdk.SessionConfigOptionCategoryThoughtLevel, levels,
	))

	agents, err := c.ListAgents(ctx)
	if err != nil {
		return nil, err
	}

	var names []string
	for _, agent := range agents {
		if agent.CanHumanTrigger() {
			names = append(names, agent.Name)
		}
	}

	if len(names) > 0 {
		opts = append(opts, selectOption("agent", "Agent", cmp.Or(s.Config.Agent, "main"), "_agent", names))
	}

	return opts, nil
}

func configPatch(id, value string) (cfg api.SessionConfig, err error) {
	switch id {
	case "agent":
		cfg.Agent = value
	case "model":
		cfg.Model = value
	case "mode":
		cfg.Mode = api.PermissionMode(value)
		if !cfg.Mode.IsValid() {
			return cfg, sdk.NewInvalidParams("invalid mode")
		}
	case "thought_level":
		t := &api.ThinkingSetting{}
		switch value {
		case "on", "off":
			t.Type, t.Enabled = "toggle", value == "on"
		case "default", "adaptive":
			t.Type = value
		default:
			if n, ok := strings.CutPrefix(value, "budget:"); ok {
				t.Type = "budget"
				t.Budget, err = strconv.Atoi(n)
				if err != nil || t.Budget <= 0 {
					return cfg, sdk.NewInvalidParams("invalid thinking budget")
				}
			} else {
				t.Type, t.Effort = "effort", value
			}
		}
		cfg.Thinking = t
	default:
		return cfg, sdk.NewInvalidParams("unknown configuration option")
	}

	return cfg, nil
}
