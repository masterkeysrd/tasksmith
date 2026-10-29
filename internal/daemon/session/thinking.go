package session

import (
	"fmt"
	"strconv"

	"github.com/masterkeysrd/tasksmith/api"
)

func AgentThinking(thinking map[string]any) *api.ThinkingSetting {
	if thinking == nil {
		return nil
	}
	if effort, ok := thinking["effort"].(string); ok && effort != "" {
		return &api.ThinkingSetting{Type: "effort", Effort: effort}
	}
	if value, err := strconv.Atoi(fmt.Sprint(thinking["budget_tokens"])); err == nil && value > 0 {
		return &api.ThinkingSetting{Type: "budget", Budget: value}
	}
	if value, ok := thinking["enabled"].(bool); ok {
		return &api.ThinkingSetting{Type: "toggle", Enabled: value}
	}
	if typ, _ := thinking["type"].(string); typ == "effort" {
		if effort, ok := thinking["default"].(string); ok && effort != "" {
			return &api.ThinkingSetting{Type: "effort", Effort: effort}
		}
	}
	return nil
}
