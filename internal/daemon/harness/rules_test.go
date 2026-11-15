package harness

import (
	"strings"
	"testing"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/models"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
	"github.com/masterkeysrd/warp"
)

// mkTestRule builds a conditional rule with the given name, priority, triggers,
// and instructions for use in RuleMatcher tests.
func mkTestRule(name string, priority int, trig warp.RuleTriggers, instructions string) *warp.Rule {
	return &warp.Rule{
		BaseResource: warp.BaseResource{
			APIVersion: warp.APIVersion,
			Kind:       warp.KindRule,
			Namespace:  warp.NamespaceWorkspace,
			Metadata:   warp.Metadata{Name: name, Description: "desc for " + name},
		},
		Spec: warp.RuleSpec{Priority: priority, Triggers: trig, Instructions: instructions},
	}
}

// newTestMatcher builds a RuleMatcher over a resolved agent holding the given
// rules, with no workspace/project/globals (rendering falls back to raw
// instructions).
func newTestMatcher(t *testing.T, rules ...*warp.Rule) *RuleMatcher {
	t.Helper()
	rs := make([]warp.Rule, 0, len(rules))
	for _, r := range rules {
		rs = append(rs, *r)
	}
	resolved := &warp.ResolvedAgent{
		Agent: &warp.Agent{BaseResource: warp.BaseResource{
			APIVersion: warp.APIVersion,
			Kind:       warp.KindAgent,
			Metadata:   warp.Metadata{Name: "main"},
		}},
		Rules: rs,
	}
	return NewRuleMatcher(resolved, nil, nil, nil)
}

func TestRuleMatcherNilIsNoOp(t *testing.T) {
	var m *RuleMatcher
	if sec, suggested := m.SuggestUserRules(message.NewUserText("hello"), State{}); sec != "" || suggested != nil {
		t.Error("nil matcher should return empty suggestions")
	}
	if _, loaded := m.MatchToolBatch(nil, nil, nil, State{}); loaded != nil {
		t.Error("nil matcher should return nil loaded")
	}
}

func TestRuleMatcherNewRuleMatcherRuleFreeIsNil(t *testing.T) {
	resolved := &warp.ResolvedAgent{Agent: &warp.Agent{}}
	if NewRuleMatcher(resolved, nil, nil, nil) != nil {
		t.Error("rule-free agent should yield a nil matcher")
	}
	if NewRuleMatcher(nil, nil, nil, nil) != nil {
		t.Error("nil resolved agent should yield a nil matcher")
	}
}

func TestRuleMatcherUserMessageKeywordSuggests(t *testing.T) {
	rule := mkTestRule("go-error-handling", 50, warp.RuleTriggers{
		Keywords: []string{"error", "fmt"},
		UseWhen:  "When handling errors.",
	}, "Always wrap and propagate errors in Go.")
	m := newTestMatcher(t, rule)

	// "error" appears as a whole word; keyword matching is case-insensitive.
	section, suggested := m.SuggestUserRules(message.NewUserText("I need to fix the ERROR handling here."), State{})
	if section == "" {
		t.Fatalf("expected a suggestion section for keyword match")
	}
	if len(suggested) != 1 || suggested[0] != "go-error-handling" {
		t.Errorf("unexpected suggestions: %v", suggested)
	}
	if !strings.Contains(section, "go-error-handling") {
		t.Errorf("suggestion section missing rule name, got:\n%s", section)
	}
}

func TestRuleMatcherKeywordWordBoundary(t *testing.T) {
	rule := mkTestRule("go-error-handling", 50, warp.RuleTriggers{
		Keywords: []string{"error"},
	}, "Always wrap errors.")
	m := newTestMatcher(t, rule)

	// "error" is a substring of "errorless" but not a whole word, so it must
	// not match.
	_, suggested := m.SuggestUserRules(message.NewUserText("This is an errorless design."), State{})
	if len(suggested) != 0 {
		t.Errorf("substring should not match on word boundary; got %v", suggested)
	}
}

func TestRuleMatcherToolTrigger(t *testing.T) {
	rule := mkTestRule("bash-guard", 10, warp.RuleTriggers{
		Tools: []string{"bash"},
	}, "Guard bash invocations.")
	m := newTestMatcher(t, rule)

	calls := []*message.ToolCall{{ID: "c1", Name: "bash"}}
	results := []*message.Tool{{
		Base:              message.Base{ID: "r1"},
		ToolCallID:        "c1",
		Name:              "bash",
		Content:           message.Content{&message.TextBlock{Text: "ok"}},
		StructuredContent: api.BashOutput{Output: "ok"},
	}}
	reminder, loaded := m.MatchToolBatch(calls, nil, results, State{})
	if reminder == nil {
		t.Fatalf("expected a reminder for tool trigger")
	}
	if len(loaded) != 1 || loaded[0].Rule != "bash-guard" || loaded[0].Trigger != "tool" {
		t.Errorf("unexpected loaded events: %+v", loaded)
	}
	if !strings.Contains(reminder.GetContent().Text(), "Guard bash invocations") {
		t.Errorf("reminder missing instructions, got:\n%s", reminder.GetContent().Text())
	}
}

func TestRuleMatcherPathTriggerFromPlanPaths(t *testing.T) {
	rule := mkTestRule("go-globs", 20, warp.RuleTriggers{
		Globs: []string{"**/*.go"},
	}, "Go file conventions.")
	m := newTestMatcher(t, rule)

	// A bash plan surfaces a workspace-relative path via plan.Paths.
	calls := []*message.ToolCall{{ID: "c1", Name: "bash"}}
	plan := &tools.Plan{ToolName: "bash", Target: "cat src/main.go", Paths: []string{"src/main.go"}}
	results := []*message.Tool{{
		Base:              message.Base{ID: "r1"},
		ToolCallID:        "c1",
		Name:              "bash",
		Content:           message.Content{&message.TextBlock{Text: "ok"}},
		StructuredContent: api.BashOutput{Output: "ok"},
	}}
	reminder, loaded := m.MatchToolBatch(calls, map[string]*tools.Plan{"c1": plan}, results, State{})
	if reminder == nil {
		t.Fatalf("expected a reminder for path trigger via plan.Paths")
	}
	if len(loaded) != 1 || loaded[0].Rule != "go-globs" || loaded[0].Trigger != "glob" {
		t.Errorf("unexpected loaded events: %+v", loaded)
	}
}

func TestRuleMatcherPathTriggerFromStructuredOutput(t *testing.T) {
	rule := mkTestRule("go-globs", 20, warp.RuleTriggers{
		Globs: []string{"**/*.go"},
	}, "Go file conventions.")
	m := newTestMatcher(t, rule)

	// A grep result surfaces matching file paths via its structured output.
	calls := []*message.ToolCall{{ID: "c1", Name: "grep"}}
	results := []*message.Tool{{
		Base:              message.Base{ID: "r1"},
		ToolCallID:        "c1",
		Name:              "grep",
		Content:           message.Content{&message.TextBlock{Text: "1:match"}},
		StructuredContent: api.GrepOutput{Matches: []api.GrepMatch{{Path: "internal/foo.go", Line: 1}}},
	}}
	reminder, loaded := m.MatchToolBatch(calls, nil, results, State{})
	if reminder == nil {
		t.Fatalf("expected a reminder for path trigger via grep output")
	}
	if len(loaded) != 1 || loaded[0].Rule != "go-globs" || loaded[0].Trigger != "glob" {
		t.Errorf("unexpected loaded events: %+v", loaded)
	}
}

func TestRuleMatcherGlobOutputTrigger(t *testing.T) {
	rule := mkTestRule("go-globs", 20, warp.RuleTriggers{
		Globs: []string{"**/*.go"},
	}, "Go file conventions.")
	m := newTestMatcher(t, rule)

	calls := []*message.ToolCall{{ID: "c1", Name: "glob"}}
	results := []*message.Tool{{
		Base:              message.Base{ID: "r1"},
		ToolCallID:        "c1",
		Name:              "glob",
		Content:           message.Content{&message.TextBlock{Text: "src/a.go"}},
		StructuredContent: api.GlobOutput{Matches: []string{"src/a.go"}},
	}}
	reminder, loaded := m.MatchToolBatch(calls, nil, results, State{})
	if reminder == nil {
		t.Fatalf("expected a reminder for path trigger via glob output")
	}
	if len(loaded) != 1 || loaded[0].Rule != "go-globs" {
		t.Errorf("unexpected loaded events: %+v", loaded)
	}
}

func TestRuleMatcherDedupAcrossTurns(t *testing.T) {
	rule := mkTestRule("bash-guard", 10, warp.RuleTriggers{
		Tools: []string{"bash"},
	}, "Guard bash invocations.")
	m := newTestMatcher(t, rule)

	calls := []*message.ToolCall{{ID: "c1", Name: "bash"}}
	results := []*message.Tool{{
		Base:              message.Base{ID: "r1"},
		ToolCallID:        "c1",
		Name:              "bash",
		Content:           message.Content{&message.TextBlock{Text: "ok"}},
		StructuredContent: api.BashOutput{Output: "ok"},
	}}

	// First turn loads the rule.
	_, loaded := m.MatchToolBatch(calls, nil, results, State{})
	if len(loaded) != 1 {
		t.Fatalf("first turn should load the rule, got %+v", loaded)
	}

	// Second turn: the rule is already loaded, so nothing new is loaded and no
	// reminder is emitted.
	state := State{}
	state = state.RecordLoadedRules(loaded[0].Rule)
	reminder, loaded2 := m.MatchToolBatch(calls, nil, results, state)
	if reminder != nil {
		t.Errorf("already-loaded rule should not produce a reminder, got:\n%s", reminder.GetContent().Text())
	}
	if len(loaded2) != 0 {
		t.Errorf("already-loaded rule should not be loaded again, got %+v", loaded2)
	}
}

func TestRuleMatcherLoadRuleMarksWithoutReminder(t *testing.T) {
	rule := mkTestRule("go-error-handling", 50, warp.RuleTriggers{
		Keywords: []string{"error"},
	}, "Always wrap and propagate errors in Go.")
	m := newTestMatcher(t, rule)

	// A successful load_rule result marks the rule as loaded but must not emit a
	// duplicate reminder (the user already received the content).
	results := []*message.Tool{{
		Base:              message.Base{ID: "r1"},
		ToolCallID:        "c1",
		Name:              "load_rule",
		Content:           message.Content{&message.TextBlock{Text: "instructions"}},
		StructuredContent: api.LoadRuleOutput{Rule: "go-error-handling", Path: "rules/go-error-handling.rule.md"},
	}}
	reminder, loaded := m.MatchToolBatch(nil, nil, results, State{})
	if reminder != nil {
		t.Errorf("load_rule result should not emit a duplicate reminder, got:\n%s", reminder.GetContent().Text())
	}
	if len(loaded) != 1 || loaded[0].Rule != "go-error-handling" {
		t.Fatalf("load_rule result should record the rule as loaded, got %+v", loaded)
	}

	// A subsequent tool/path trigger for the same rule is now deduplicated.
	calls := []*message.ToolCall{{ID: "c2", Name: "grep"}}
	results2 := []*message.Tool{{
		Base:              message.Base{ID: "r2"},
		ToolCallID:        "c2",
		Name:              "grep",
		Content:           message.Content{&message.TextBlock{Text: "x"}},
		StructuredContent: api.GrepOutput{Matches: []api.GrepMatch{{Path: "main.go"}}},
	}}
	// go-error-handling has no globs, so this would not match anyway; use a
	// rule with a glob to prove dedup.
	globRule := mkTestRule("go-error-handling", 50, warp.RuleTriggers{Globs: []string{"**/*.go"}, Keywords: []string{"error"}}, "Always wrap and propagate errors in Go.")
	m2 := newTestMatcher(t, globRule)
	state := State{}
	state = state.RecordLoadedRules("go-error-handling")
	reminder2, loaded2 := m2.MatchToolBatch(calls, nil, results2, state)
	if reminder2 != nil {
		t.Errorf("glob trigger for already-loaded rule should be deduplicated, got:\n%s", reminder2.GetContent().Text())
	}
	if len(loaded2) != 0 {
		t.Errorf("already-loaded rule should not be re-loaded, got %+v", loaded2)
	}
}

func TestRuleMatcherCompactionResetsRules(t *testing.T) {
	state := State{
		LoadedRules:    []string{"go-error-handling"},
		LoadedSkills:   []string{"release-runbook"},
		SuggestedRules: []string{"bash-guard"},
	}
	reset := state.ApplyCompaction(StaticContext{}, models.Limits{})
	if reset.LoadedRules != nil {
		t.Errorf("compaction should reset LoadedRules, got %v", reset.LoadedRules)
	}
	if reset.LoadedSkills != nil {
		t.Errorf("compaction should reset LoadedSkills, got %v", reset.LoadedSkills)
	}
	if reset.SuggestedRules != nil {
		t.Errorf("compaction should reset SuggestedRules, got %v", reset.SuggestedRules)
	}
}

func TestRuleMatcherStateCopyClonesRuleSlices(t *testing.T) {
	state := State{
		LoadedRules:    []string{"go-error-handling"},
		SuggestedRules: []string{"bash-guard"},
	}
	clone := state.Copy()
	clone.LoadedRules = append(clone.LoadedRules, "new-rule")
	clone.SuggestedRules = append(clone.SuggestedRules, "new-suggested")
	if len(state.LoadedRules) != 1 {
		t.Errorf("Copy should not share the LoadedRules backing array, got %v", state.LoadedRules)
	}
	if len(state.SuggestedRules) != 1 {
		t.Errorf("Copy should not share the SuggestedRules backing array, got %v", state.SuggestedRules)
	}
}

func TestRuleMatcherRecordDedupCaseInsensitive(t *testing.T) {
	state := State{}
	state = state.RecordLoadedRules("Go-Error-Handling")
	state = state.RecordLoadedRules("go-error-handling")
	if len(state.LoadedRules) != 1 {
		t.Errorf("case-insensitive dedup failed, got %v", state.LoadedRules)
	}
	if !state.HasLoadedRule("GO-ERROR-HANDLING") {
		t.Error("HasLoadedRule should be case-insensitive")
	}

	state2 := State{}
	state2 = state2.RecordSuggestedRules("bash-guard")
	state2 = state2.RecordSuggestedRules("BASH-GUARD")
	if len(state2.SuggestedRules) != 1 {
		t.Errorf("case-insensitive suggested dedup failed, got %v", state2.SuggestedRules)
	}
	if !state2.HasSuggestedRule("bash-guard") {
		t.Error("HasSuggestedRule should be case-insensitive")
	}
}

func TestRuleMatcherPriorityOrdering(t *testing.T) {
	high := mkTestRule("high-prio", 100, warp.RuleTriggers{Tools: []string{"bash"}}, "High priority.")
	low := mkTestRule("low-prio", 10, warp.RuleTriggers{Tools: []string{"bash"}}, "Low priority.")
	m := newTestMatcher(t, low, high)

	calls := []*message.ToolCall{{ID: "c1", Name: "bash"}}
	results := []*message.Tool{{
		Base:              message.Base{ID: "r1"},
		ToolCallID:        "c1",
		Name:              "bash",
		Content:           message.Content{&message.TextBlock{Text: "ok"}},
		StructuredContent: api.BashOutput{Output: "ok"},
	}}
	reminder, _ := m.MatchToolBatch(calls, nil, results, State{})
	if reminder == nil {
		t.Fatalf("expected a reminder")
	}
	text := reminder.GetContent().Text()
	highIdx := strings.Index(text, "High priority.")
	lowIdx := strings.Index(text, "Low priority.")
	if highIdx < 0 || lowIdx < 0 {
		t.Fatalf("reminder missing rules, got:\n%s", text)
	}
	if highIdx > lowIdx {
		t.Errorf("higher-priority rule should render first, got:\n%s", text)
	}
}

func TestRuleMatcherGenuineUserOnly(t *testing.T) {
	rule := mkTestRule("go-error-handling", 50, warp.RuleTriggers{Keywords: []string{"error"}}, "Always wrap errors.")
	m := newTestMatcher(t, rule)

	// A synthetic reminder message (SystemKindOf != "") must be ignored.
	reminder := message.NewUserTextMeta("error", map[string]any{MetaSystemKind: SystemKindReminder})
	if sec, _ := m.SuggestUserRules(reminder, State{}); sec != "" {
		t.Error("synthetic reminder should not be re-matched")
	}
	// A non-user message must be ignored.
	if sec, _ := m.SuggestUserRules(&message.Assistant{Content: message.Content{&message.TextBlock{Text: "error"}}}, State{}); sec != "" {
		t.Error("non-user message should not be matched")
	}
}
