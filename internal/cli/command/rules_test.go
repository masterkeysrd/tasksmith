package command

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
)

func TestRulesCommandArgsValidation(t *testing.T) {
	reg := NewRegistry()

	// 1. With arguments: must be rejected
	err := reg.Execute("/rules test")
	if err == nil {
		t.Fatal("expected error when running /rules with args, got nil")
	}
	if !strings.Contains(err.Error(), "does not accept arguments") {
		t.Fatalf("unexpected error message: %v", err)
	}

	// 2. With multiple arguments: must be rejected
	err = reg.Execute("/rules foo bar baz")
	if err == nil {
		t.Fatal("expected error when running /rules with multiple args, got nil")
	}

	// 3. Without arguments: must succeed and enqueue slot
	store.ClearSlot()
	defer store.ClearSlot()
	err = reg.Execute("/rules")
	if err != nil {
		t.Fatalf("unexpected error running /rules: %v", err)
	}
	st := store.GetSlot()
	if st.Active == nil || st.Active.Kind != store.SlotKindPicker {
		t.Fatalf("expected picker slot to be enqueued, got %+v", st.Active)
	}
}

func TestRuleScopeMapping(t *testing.T) {
	tests := []struct {
		rule api.Rule
		want string
	}{
		{rule: api.Rule{Namespace: "workspace"}, want: "Workspace"},
		{rule: api.Rule{Namespace: "local"}, want: "Project"},
		{rule: api.Rule{Namespace: "project"}, want: "Project"},
		{rule: api.Rule{Namespace: "user"}, want: "User"},
		{rule: api.Rule{Namespace: "system"}, want: "Builtin"},
		{rule: api.Rule{Namespace: "builtin"}, want: "Builtin"},
		{rule: api.Rule{Namespace: ""}, want: "Builtin"},
		{rule: api.Rule{Scope: "Workspace", Namespace: "system"}, want: "Workspace"},
		{rule: api.Rule{Namespace: "my-plugin"}, want: "My-plugin"},
	}

	for _, tt := range tests {
		got := RuleScope(tt.rule)
		if got != tt.want {
			t.Errorf("RuleScope(%+v) = %q, want %q", tt.rule, got, tt.want)
		}
	}
}

func TestRuleStatusBadge(t *testing.T) {
	loaded := map[string]bool{"go-error-handling": true}

	tests := []struct {
		name string
		rule api.Rule
		want string
	}{
		{name: "always", rule: api.Rule{Name: "security", Triggers: api.RuleTriggers{Always: true}}, want: "[Always]"},
		{name: "loaded conditional", rule: api.Rule{Name: "go-error-handling"}, want: "[Loaded]"},
		{name: "standby conditional", rule: api.Rule{Name: "bash-guard", Triggers: api.RuleTriggers{Tools: []string{"bash"}}}, want: "[Standby]"},
		{name: "always beats loaded", rule: api.Rule{Name: "go-error-handling", Triggers: api.RuleTriggers{Always: true}}, want: "[Always]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ruleStatusBadge(tt.rule, loaded)
			if got != tt.want {
				t.Errorf("ruleStatusBadge(%+v) = %q, want %q", tt.rule, got, tt.want)
			}
		})
	}
}

func TestRuleTriggersSummary(t *testing.T) {
	tests := []struct {
		name string
		rule api.Rule
		want string
	}{
		{name: "always only", rule: api.Rule{Triggers: api.RuleTriggers{Always: true}}, want: "always"},
		{name: "globs", rule: api.Rule{Triggers: api.RuleTriggers{Globs: []string{"**/*.go"}}}, want: "globs"},
		{name: "tools", rule: api.Rule{Triggers: api.RuleTriggers{Tools: []string{"bash"}}}, want: "tools"},
		{name: "keywords", rule: api.Rule{Triggers: api.RuleTriggers{Keywords: []string{"error"}}}, want: "keywords"},
		{name: "combined", rule: api.Rule{Triggers: api.RuleTriggers{Always: true, Globs: []string{"**/*.go"}, Tools: []string{"bash"}, Keywords: []string{"error"}}}, want: "always, globs, tools, keywords"},
		{name: "none", rule: api.Rule{}, want: "—"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ruleTriggersSummary(tt.rule)
			if got != tt.want {
				t.Errorf("ruleTriggersSummary(%+v) = %q, want %q", tt.rule, got, tt.want)
			}
		})
	}
}

func TestRulesBrowserEmptyAndPopulatedRendering(t *testing.T) {
	store.ClearSlot()
	defer store.ClearSlot()

	// 1. Empty state
	store.SetWorkspaceRules([]api.Rule{})
	out := renderTestNode(t, RulesBrowser("slot-1"))
	if !strings.Contains(out, "Rules (0 available)") || !strings.Contains(out, "No rules configured") {
		t.Fatalf("unexpected empty browser output:\n%s", out)
	}

	// 2. Populated state
	rules := []api.Rule{
		{
			Name:         "go-error-handling",
			Description:  "Go error handling",
			Namespace:    "workspace",
			Instructions: "# Go Errors\n\nAlways wrap and propagate errors.",
			Triggers:     api.RuleTriggers{Globs: []string{"**/*.go"}, Keywords: []string{"error"}},
			Priority:     50,
		},
		{
			Name:         "security-baseline",
			Description:  "Security guardrails",
			Namespace:    "system",
			Instructions: "Never log secrets.",
			Triggers:     api.RuleTriggers{Always: true},
			Priority:     100,
		},
		{
			Name:         "bash-guard",
			Description:  "Bash safety",
			Namespace:    "local",
			Instructions: "Confirm destructive commands.",
			Triggers:     api.RuleTriggers{Tools: []string{"bash"}},
		},
	}
	store.SetWorkspaceRules(rules)

	out = renderTestNode(t, RulesBrowser("slot-2"))

	if !strings.Contains(out, "Rules (3 available)") {
		t.Fatalf("expected title to show 3 available, got:\n%s", out)
	}
	if !strings.Contains(out, "go-error-handling") || !strings.Contains(out, "security-baseline") || !strings.Contains(out, "bash-guard") {
		t.Fatalf("expected all rule names in output, got:\n%s", out)
	}
	if !strings.Contains(out, "[Workspace]") || !strings.Contains(out, "[Project]") || !strings.Contains(out, "[Builtin]") {
		t.Fatalf("expected scope badges in output, got:\n%s", out)
	}
	if !strings.Contains(out, "[Always]") || !strings.Contains(out, "[Standby]") {
		t.Fatalf("expected status badges in output, got:\n%s", out)
	}
	if !strings.Contains(out, "Go error handling") {
		t.Fatalf("expected description column in output, got:\n%s", out)
	}
}

func TestRuleMarkdownViewerRendering(t *testing.T) {
	rl := api.Rule{
		Name:         "go-error-handling",
		Description:  "Go error handling",
		Namespace:    "workspace",
		Priority:     50,
		Triggers:     api.RuleTriggers{Globs: []string{"**/*.go"}, Keywords: []string{"error"}, UseWhen: "writing or reviewing Go code"},
		Instructions: "# Go Errors\n\n- Wrap errors with fmt.Errorf",
	}

	out := renderTestNode(t, ruleMarkdownViewer(ruleMarkdownProps{
		slotID:  "slot-3",
		rule:    rl,
		onClose: func() {},
	}))

	if !strings.Contains(out, "Rule: go-error-handling") {
		t.Fatalf("expected header to contain rule name, got:\n%s", out)
	}
	if !strings.Contains(out, "[Workspace]") {
		t.Fatalf("expected header to contain scope badge, got:\n%s", out)
	}
	if !strings.Contains(out, "Priority: 50") {
		t.Fatalf("expected header to contain priority, got:\n%s", out)
	}
	if !strings.Contains(out, "When to use: writing or reviewing Go code") {
		t.Fatalf("expected header to contain UseWhen, got:\n%s", out)
	}
	if !strings.Contains(out, "Go Errors") || !strings.Contains(out, "Wrap errors with fmt.Errorf") {
		t.Fatalf("expected markdown body to be rendered, got:\n%s", out)
	}
}

func TestRegisterRuleResourceDoesNotOverwriteBuiltinRules(t *testing.T) {
	reg := NewRegistry()

	// Builtin /rules must exist
	cmd, ok := reg.Lookup("rules")
	if !ok || cmd.Name != "rules" {
		t.Fatal("expected built-in /rules command to exist")
	}
	if cmd.Description != "List, inspect, and reload workspace rules" {
		t.Fatalf("unexpected /rules description: %q", cmd.Description)
	}
}

// loadRuleToolPart builds a finalized load_rule tool part for session tests.
func loadRuleToolPart(t *testing.T, id, ruleName string, succeeded bool) store.MessagePart {
	t.Helper()
	tool := store.ToolPart{
		ID:   id,
		Call: &api.ToolCall{ID: id + "-call", Name: "load_rule", Args: map[string]any{"rule": ruleName}},
	}
	if succeeded {
		tool.Status = store.ToolSucceeded
		out, err := json.Marshal(api.LoadRuleOutput{Rule: ruleName, Instructions: "do the thing"})
		if err != nil {
			t.Fatalf("marshal LoadRuleOutput: %v", err)
		}
		tool.Response = &api.Message{ID: id + "-res", StructuredContent: out}
	} else {
		tool.Status = store.ToolFailed
		tool.Response = &api.Message{ID: id + "-res", IsError: true}
	}
	return store.MessagePart{ID: id, Type: store.PartTool, Tool: &tool}
}

func TestLoadedRuleNamesFromSession(t *testing.T) {
	t.Cleanup(func() {
		store.ClearTranscript()
		store.ResetStream()
	})

	items := []store.TranscriptItem{
		{
			ID:   "rule-1",
			Kind: store.TranscriptKindTimeline,
			Data: api.TimelineEntry{
				Kind: api.TimelineKindRule,
				Rule: &api.RuleRecord{Rule: "Go-Globs", Trigger: "glob", Matched: "main.go"},
			},
		},
		{
			ID:   "msg-1",
			Kind: store.TranscriptKindMessage,
			Data: store.Message{ID: "m1", Parts: []store.MessagePart{loadRuleToolPart(t, "lr-ok", "security-audit", true)}},
		},
		{
			ID:   "msg-2",
			Kind: store.TranscriptKindTimeline,
			Data: api.TimelineEntry{Kind: api.TimelineKindRule},
		},
		{
			ID:   "msg-3",
			Kind: store.TranscriptKindNotice,
			Data: store.NoticeData{Text: "unrelated"},
		},
	}
	messages := []store.Message{
		{ID: "stream-1", Parts: []store.MessagePart{loadRuleToolPart(t, "lr-fail", "broken-rule", false)}},
	}

	loaded := loadedRuleNamesFromSession(&items, &messages)

	// Rule names are lower-cased for case-insensitive badge lookups.
	if !loaded["go-globs"] {
		t.Errorf("expected transcript rule entry 'Go-Globs' to be recorded case-insensitively, got %v", loaded)
	}
	if !loaded["security-audit"] {
		t.Errorf("expected successful load_rule tool part to be recorded, got %v", loaded)
	}
	// A failed load_rule call must not mark the rule as loaded.
	if loaded["broken-rule"] {
		t.Errorf("failed load_rule call should not mark rule as loaded, got %v", loaded)
	}
	// A rule timeline entry without a payload must not add an empty key.
	if _, ok := loaded[""]; ok {
		t.Errorf("nil rule record should not add an empty key, got %v", loaded)
	}
	if len(loaded) != 2 {
		t.Errorf("expected exactly 2 loaded rules, got %v", loaded)
	}
}

func TestRulesBrowserShowsLoadedBadgeFromSession(t *testing.T) {
	store.ClearSlot()
	store.ResetStream()
	t.Cleanup(func() {
		store.ClearSlot()
		store.ClearTranscript()
		store.ResetStream()
		store.SetWorkspaceRules(nil)
	})

	store.SetWorkspaceRules([]api.Rule{
		{Name: "go-globs", Description: "Go conventions", Namespace: "workspace", Triggers: api.RuleTriggers{Globs: []string{"**/*.go"}}},
		{Name: "idle-rule", Description: "Not active", Namespace: "workspace", Triggers: api.RuleTriggers{Keywords: []string{"nothing"}}},
	})
	store.ResetTranscript(store.TranscriptItem{
		ID:   "rule-entry",
		Kind: store.TranscriptKindTimeline,
		Data: api.TimelineEntry{Kind: api.TimelineKindRule, Rule: &api.RuleRecord{Rule: "go-globs", Trigger: "glob", Matched: "main.go"}},
	})

	out := renderTestNode(t, RulesBrowser("slot-loaded"))

	if !strings.Contains(out, "[Loaded]") {
		t.Fatalf("expected [Loaded] badge for session-active rule, got:\n%s", out)
	}
	if !strings.Contains(out, "[Standby]") {
		t.Fatalf("expected [Standby] badge for inactive rule, got:\n%s", out)
	}
}
