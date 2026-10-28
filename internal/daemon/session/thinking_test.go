package session

import (
	"testing"

	"github.com/masterkeysrd/loom/profile"
	"github.com/masterkeysrd/tasksmith/api"
)

func TestThinkingValidationAndConfig(t *testing.T) {
	prof := &profile.Profile{Capabilities: profile.Capabilities{ReasoningOptions: []profile.ReasoningOption{
		{Type: "effort", Values: []string{"minimal", "high"}},
		{Type: "budget_tokens", Min: 100, Max: 1000},
		{Type: "toggle"},
	}}}
	cases := []struct {
		name    string
		setting api.ThinkingSetting
		valid   bool
	}{
		{"effort", api.ThinkingSetting{Type: "effort", Effort: "minimal"}, true},
		{"invalid effort", api.ThinkingSetting{Type: "effort", Effort: "low"}, false},
		{"budget", api.ThinkingSetting{Type: "budget", Budget: 200}, true},
		{"budget below minimum", api.ThinkingSetting{Type: "budget", Budget: 99}, false},
		{"budget above maximum", api.ThinkingSetting{Type: "budget", Budget: 1001}, false},
		{"toggle off", api.ThinkingSetting{Type: "toggle", Enabled: false}, true},
		{"unknown", api.ThinkingSetting{Type: "other"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateThinking(&tc.setting, prof)
			if (err == nil) != tc.valid {
				t.Fatalf("validateThinking: %v", err)
			}
		})
	}
	if err := validateThinking(&api.ThinkingSetting{Type: "toggle"}, nil); err == nil {
		t.Fatal("unknown model must not accept a toggle")
	}
	if got := thinkingConfig(&api.ThinkingSetting{Type: "toggle", Enabled: false}); got == nil || got.Enabled {
		t.Fatalf("off toggle not preserved: %+v", got)
	}
	if got := thinkingConfig(&api.ThinkingSetting{Type: "budget", Budget: 200}); got == nil || !got.Enabled || got.Budget != 200 {
		t.Fatalf("budget not applied: %+v", got)
	}
	if got := thinkingConfig(&api.ThinkingSetting{Type: "effort", Effort: "minimal"}); got == nil || !got.Enabled || got.Effort != "minimal" {
		t.Fatalf("effort not applied: %+v", got)
	}
}
