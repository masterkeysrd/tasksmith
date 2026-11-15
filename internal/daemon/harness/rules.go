package harness

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/uuid"
	"github.com/masterkeysrd/tasksmith/internal/daemon/tools"
	"github.com/masterkeysrd/warp"
)

// RuleMatcher drives smart conditional rule matching for the active agent. It
// surfaces keyword-matched rule suggestions on user messages, auto-injects
// tool/path-triggered rules into <system_reminders>, and produces RuleLoaded
// events. Explicit @rule: and AttachmentRule resolution lives in [AttachmentResolver].
// A nil RuleMatcher (or one built from a rule-free agent) is a no-op.
type RuleMatcher struct {
	resolved  *warp.ResolvedAgent
	workspace *warp.Workspace
	project   *warp.Project
	globals   map[string]any
}

// NewRuleMatcher constructs a RuleMatcher for the resolved agent. It returns nil
// when the agent resolves no rules, so callers can treat a nil matcher as a
// no-op without a separate guard.
func NewRuleMatcher(resolved *warp.ResolvedAgent, workspace *warp.Workspace, project *warp.Project, globals map[string]any) *RuleMatcher {
	if resolved == nil || len(resolved.Rules) == 0 {
		return nil
	}
	return &RuleMatcher{resolved: resolved, workspace: workspace, project: project, globals: globals}
}

func renderWarpRule(rl *warp.Rule, ws *warp.Workspace, project *warp.Project, agent *warp.Agent, globals map[string]any) string {
	if rl == nil {
		return ""
	}
	rendered, err := rl.Render(&warp.RuleRenderOptions{
		Workspace: ws,
		Project:   project,
		Agent:     agent,
		Globals:   globals,
	})
	if err != nil {
		return strings.TrimSpace(rl.Spec.Instructions)
	}
	return strings.TrimSpace(rendered)
}

func matchWarpResourceKey(res warp.Resource, namespace, lowerKey string) bool {
	if res == nil || lowerKey == "" {
		return false
	}
	name := res.GetName()
	if strings.EqualFold(name, lowerKey) {
		return true
	}
	if namespace != "" && strings.EqualFold(namespace+"/"+name, lowerKey) {
		return true
	}
	if ns := res.GetNamespace(); ns != "" && strings.EqualFold(ns+"/"+name, lowerKey) {
		return true
	}
	if q := res.QualifiedName(); q != "" && strings.EqualFold(q, lowerKey) {
		return true
	}
	return false
}

// render renders a rule's instructions as a template, falling back to the raw
// instructions on error.
func (m *RuleMatcher) render(rl *warp.Rule) string {
	if m == nil {
		return renderWarpRule(rl, nil, nil, nil, nil)
	}
	return renderWarpRule(rl, m.workspace, m.project, m.resolved.Agent, m.globals)
}

// lookupRule resolves a rule by its short or qualified name (case-insensitive).
func (m *RuleMatcher) lookupRule(name string) (*warp.Rule, bool) {
	if m == nil || m.resolved == nil {
		return nil, false
	}
	key := strings.ToLower(strings.TrimSpace(name))
	if key == "" {
		return nil, false
	}
	for i := range m.resolved.Rules {
		rl := &m.resolved.Rules[i]
		if matchWarpResourceKey(rl, rl.Namespace, key) {
			return rl, true
		}
	}
	return nil, false
}

// SuggestUserRules inspects a genuine user message for keyword triggers on the
// agent's conditional rules, returning a formatted suggestion section (when any
// rule matched) and the names of the newly suggested rules.
func (m *RuleMatcher) SuggestUserRules(msg message.Message, state State) (string, []string) {
	if m == nil {
		return "", nil
	}
	user, ok := msg.(*message.User)
	if !ok || user == nil || SystemKindOf(user) != "" {
		return "", nil
	}
	text := user.GetContent().Text()
	if strings.TrimSpace(text) == "" {
		return "", nil
	}

	var suggested []string
	var bullets []string
	seenSuggested := make(map[string]struct{})
	lower := strings.ToLower(text)

	for i := range m.resolved.Rules {
		rl := &m.resolved.Rules[i]
		if rl.Spec.Triggers.Always || len(rl.Spec.Triggers.Keywords) == 0 {
			continue
		}
		name := rl.Metadata.Name
		key := strings.ToLower(name)
		if state.HasLoadedRule(name) || state.HasSuggestedRule(name) {
			continue
		}
		if _, dup := seenSuggested[key]; dup {
			continue
		}
		if !matchesKeywords(rl.Spec.Triggers.Keywords, lower) {
			continue
		}
		seenSuggested[key] = struct{}{}
		bullet := fmt.Sprintf("- **%s**: %s", name, rl.Metadata.Description)
		if useWhen := strings.TrimSpace(rl.Spec.Triggers.UseWhen); useWhen != "" {
			bullet += fmt.Sprintf(" (Use when: %s)", useWhen)
		}
		bullets = append(bullets, bullet)
		suggested = append(suggested, name)
	}

	if len(bullets) == 0 {
		return "", nil
	}
	section := "The following workspace rules may be relevant to your current task. Call load_rule to apply any that match:\n" + strings.Join(bullets, "\n")
	return section, suggested
}

// MatchToolBatch inspects a batch of executed tool calls and their results for
// tool-name and path triggers, returning a <system_reminders> message (when
// anything auto-loaded) and the rules that were loaded.
func (m *RuleMatcher) MatchToolBatch(calls []*message.ToolCall, plans map[string]*tools.Plan, results []*message.Tool, state State) (*message.User, []RuleLoaded) {
	if m == nil || len(results) == 0 {
		return nil, nil
	}

	var loaded []RuleLoaded
	// rendered tracks the rules that should appear in the reminder. Rules loaded
	// via an explicit load_rule call are recorded (so they are not re-emitted on
	// later turns) but excluded from the reminder to avoid duplicating content
	// the user already received.
	var rendered []RuleLoaded
	seen := make(map[string]struct{})

	addLoaded := func(rl *warp.Rule, trigger, matched, path string, emit bool) {
		if rl == nil {
			return
		}
		key := strings.ToLower(rl.Metadata.Name)
		if _, dup := seen[key]; dup {
			return
		}
		if state.HasLoadedRule(rl.Metadata.Name) {
			return
		}
		seen[key] = struct{}{}
		ev := RuleLoaded{
			ID:      uuid.NewString(),
			Rule:    rl.Metadata.Name,
			Trigger: trigger,
			Matched: matched,
			Path:    path,
		}
		loaded = append(loaded, ev)
		if emit {
			rendered = append(rendered, ev)
		}
	}

	// 1. Successful load_rule results mark the rule as loaded without emitting a
	//    duplicate reminder.
	for _, res := range results {
		if res == nil || res.IsError || res.Name != "load_rule" {
			continue
		}
		if out, ok := res.StructuredContent.(api.LoadRuleOutput); ok && strings.TrimSpace(out.Rule) != "" {
			if rl, ok := m.lookupRule(out.Rule); ok {
				addLoaded(rl, "manual", out.Rule, out.Path, false)
			}
		}
	}

	// 2. Tool-name triggers.
	for _, tc := range calls {
		for _, rl := range m.resolved.RulesForTool(tc.Name) {
			if rl.Spec.Triggers.Always {
				continue
			}
			addLoaded(&rl, "tool", tc.Name, "", true)
		}
	}

	// 3. Path triggers from plan targets/paths and structured tool outputs.
	paths := m.collectPaths(calls, plans, results)
	for _, p := range paths {
		rel := normalizePathForRules(p)
		if rel == "" {
			continue
		}
		for _, rl := range m.resolved.RulesForPath(rel) {
			if rl.Spec.Triggers.Always {
				continue
			}
			addLoaded(&rl, "glob", rel, rel, true)
		}
	}

	if len(loaded) == 0 {
		return nil, nil
	}

	slices.SortFunc(rendered, func(a, b RuleLoaded) int {
		ra, _ := m.lookupRule(a.Rule)
		rb, _ := m.lookupRule(b.Rule)
		pa, pb := 0, 0
		if ra != nil {
			pa = ra.Spec.Priority
		}
		if rb != nil {
			pb = rb.Spec.Priority
		}
		if pa != pb {
			return pb - pa
		}
		if a.Rule < b.Rule {
			return -1
		}
		if a.Rule > b.Rule {
			return 1
		}
		return 0
	})

	if len(rendered) == 0 {
		return nil, loaded
	}

	var sb strings.Builder
	sb.WriteString("<system_reminders>\n")
	for _, ev := range rendered {
		rl, _ := m.lookupRule(ev.Rule)
		fmt.Fprintf(&sb, "<rule name=%q path=%q>\n%s\n</rule>\n", ev.Rule, ev.Path, m.render(rl))
	}
	sb.WriteString("</system_reminders>")

	names := make([]string, 0, len(loaded))
	for _, ev := range loaded {
		names = append(names, ev.Rule)
	}
	reminder := message.NewUserTextMeta(sb.String(), map[string]any{
		MetaSystemKind: SystemKindReminder,
		MetaRuleNames:  names,
	})
	return reminder, loaded
}

// collectPaths gathers candidate workspace-relative paths from the tool call
// plans and the structured outputs of the executed results.
func (m *RuleMatcher) collectPaths(calls []*message.ToolCall, plans map[string]*tools.Plan, results []*message.Tool) []string {
	var paths []string
	seen := make(map[string]struct{})
	add := func(p string) {
		p = normalizePathForRules(p)
		if p == "" {
			return
		}
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		paths = append(paths, p)
	}

	for _, tc := range calls {
		if plan, ok := plans[tc.ID]; ok && plan != nil {
			if plan.Target != "" {
				add(plan.Target)
			}
			for _, p := range plan.Paths {
				add(p)
			}
		}
	}
	for _, res := range results {
		if res == nil {
			continue
		}
		switch out := res.StructuredContent.(type) {
		case api.GlobOutput:
			for _, p := range out.Matches {
				add(p)
			}
		case api.GrepOutput:
			for _, mm := range out.Matches {
				add(mm.Path)
			}
		case api.LsOutput:
			for _, e := range out.Entries {
				add(e.Path)
			}
		}
	}
	return paths
}

// matchesKeywords reports whether any keyword matches the lower-cased text on a
// word boundary (case-insensitive).
func matchesKeywords(keywords []string, lowerText string) bool {
	for _, kw := range keywords {
		kw = strings.TrimSpace(kw)
		if kw == "" {
			continue
		}
		re := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(kw) + `\b`)
		if re.MatchString(lowerText) {
			return true
		}
	}
	return false
}

// normalizePathForRules trims and slash-normalizes a path for rule glob matching.
func normalizePathForRules(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	return strings.ReplaceAll(p, "\\", "/")
}

// ruleLoadedNames returns the rule names referenced by a set of RuleLoaded events.
func ruleLoadedNames(loaded []RuleLoaded) []string {
	if len(loaded) == 0 {
		return nil
	}
	names := make([]string, 0, len(loaded))
	for _, ev := range loaded {
		names = append(names, ev.Rule)
	}
	return names
}
