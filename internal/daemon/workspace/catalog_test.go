package workspace

import (
	"slices"
	"strings"
	"testing"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/warp"
)

// TestCatalogRules verifies that Catalog.Rules() and Catalog.GetRule() expose the
// full api.Rule schema (Triggers, Priority, Scope, Instructions, Tokens) for
// effective rules, that Catalog.HasConditionalRules reports the presence of
// non-always rules, and that a rule declaring an unsupported triggers.decision
// surfaces a non-fatal diagnostic warning.
func TestCatalogRules(t *testing.T) {
	ws := &warp.Workspace{
		Def: &warp.WorkspaceDef{
			BaseResource: warp.BaseResource{
				APIVersion: warp.APIVersion,
				Kind:       warp.KindWorkspace,
				Metadata:   warp.Metadata{Name: "rules-ws"},
			},
			Spec: warp.WorkspaceDefSpec{
				DefaultAgent: "main",
			},
		},
	}

	mkRule := func(name string, trig warp.RuleTriggers, priority int, instructions string) *warp.Rule {
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

	ruleAlways := mkRule("rule-always", warp.RuleTriggers{Always: true}, 10, "Always follow this rule.")
	ruleConditional := mkRule("rule-conditional", warp.RuleTriggers{
		Globs:    []string{"**/*.go", "!**/vendor/**"},
		Keywords: []string{"golang", "fmt"},
		Tools:    []string{"bash"},
		UseWhen:  "When editing Go source files.",
	}, 50, "Follow this conditional rule.")
	ruleDecision := mkRule("rule-decision", warp.RuleTriggers{Decision: "dec"}, 100, "Decision-gated rule.")

	// The rule's triggers.decision references a valid decision resource so the
	// registry validates cleanly; the catalog itself still warns that the
	// decision trigger is unsupported.
	dec := &warp.Decision{
		BaseResource: warp.BaseResource{
			APIVersion: warp.APIVersion,
			Kind:       warp.KindDecision,
			Namespace:  warp.NamespaceWorkspace,
			Metadata:   warp.Metadata{Name: "dec"},
		},
		Spec: warp.DecisionSpec{},
	}

	// The conditional rule's triggers.tools references this tool so the registry
	// validates cleanly.
	bashTool := &warp.Tool{
		BaseResource: warp.BaseResource{
			APIVersion: warp.APIVersion,
			Kind:       warp.KindTool,
			Namespace:  warp.NamespaceWorkspace,
			Metadata:   warp.Metadata{Name: "bash", Description: "run shell commands"},
		},
		Spec: warp.ToolSpec{Annotations: &warp.ToolAnnotation{IsReadOnly: false}},
	}

	mainAgent := &warp.Agent{
		BaseResource: warp.BaseResource{
			APIVersion: warp.APIVersion,
			Kind:       warp.KindAgent,
			Namespace:  warp.NamespaceWorkspace,
			Metadata:   warp.Metadata{Name: "main"},
		},
		Spec: warp.AgentSpec{},
	}

	reg := warp.NewRegistry(ws)
	for _, res := range []warp.Resource{
		ruleAlways, ruleConditional, ruleDecision, dec, bashTool, mainAgent,
	} {
		reg.Set(res.QualifiedName(), res)
	}
	if err := reg.Validate(); err != nil {
		t.Fatalf("registry validation failed: %v", err)
	}

	cat := NewCatalog(reg, "", "")

	// 1. Rules() returns all effective rules sorted by name with the full schema.
	rules := cat.Rules()
	if len(rules) != 3 {
		t.Fatalf("Rules() returned %d rules, want 3: %+v", len(rules), rules)
	}
	gotNames := make([]string, len(rules))
	for i, r := range rules {
		gotNames[i] = r.Name
	}
	if !slices.Equal(gotNames, []string{"rule-always", "rule-conditional", "rule-decision"}) {
		t.Errorf("Rules() order = %v, want [rule-always rule-conditional rule-decision]", gotNames)
	}

	// rule-always: unconditionally active.
	if !rules[0].Triggers.Always {
		t.Errorf("rule-always Triggers.Always = false, want true")
	}
	if rules[0].Priority != 10 {
		t.Errorf("rule-always Priority = %d, want 10", rules[0].Priority)
	}
	if rules[0].Scope != "Workspace" {
		t.Errorf("rule-always Scope = %q, want Workspace", rules[0].Scope)
	}
	if rules[0].Instructions != "Always follow this rule." {
		t.Errorf("rule-always Instructions = %q", rules[0].Instructions)
	}
	if rules[0].Tokens <= 0 {
		t.Errorf("rule-always Tokens = %d, want > 0", rules[0].Tokens)
	}

	// rule-conditional: conditional triggers are surfaced in full.
	if rules[1].Triggers.Always {
		t.Errorf("rule-conditional Triggers.Always = true, want false")
	}
	if !slices.Equal(rules[1].Triggers.Globs, []string{"**/*.go", "!**/vendor/**"}) {
		t.Errorf("rule-conditional Globs = %v", rules[1].Triggers.Globs)
	}
	if !slices.Equal(rules[1].Triggers.Keywords, []string{"golang", "fmt"}) {
		t.Errorf("rule-conditional Keywords = %v", rules[1].Triggers.Keywords)
	}
	if !slices.Equal(rules[1].Triggers.Tools, []string{"bash"}) {
		t.Errorf("rule-conditional Tools = %v", rules[1].Triggers.Tools)
	}
	if rules[1].Triggers.UseWhen != "When editing Go source files." {
		t.Errorf("rule-conditional UseWhen = %q", rules[1].Triggers.UseWhen)
	}
	if rules[1].Priority != 50 {
		t.Errorf("rule-conditional Priority = %d, want 50", rules[1].Priority)
	}

	// 2. GetRule resolves by short name and by qualified name.
	got, ok := cat.GetRule("rule-conditional")
	if !ok {
		t.Fatal("GetRule(rule-conditional) not found")
	}
	if got.Name != "rule-conditional" || got.Priority != 50 {
		t.Errorf("GetRule(rule-conditional) = %+v, want name=rule-conditional priority=50", got.Rule)
	}

	qName := ruleConditional.QualifiedName()
	if _, ok := cat.GetRule(qName); !ok {
		t.Errorf("GetRule(%q) not found", qName)
	}

	if _, ok := cat.GetRule("does-not-exist"); ok {
		t.Error("GetRule(does-not-exist) unexpectedly found")
	}

	// 3. triggers.decision surfaces a non-fatal diagnostic warning.
	warnings := cat.Warnings()
	var foundDecisionWarning bool
	for _, w := range warnings {
		if strings.Contains(w, "rule-decision") && strings.Contains(w, "triggers.decision") {
			foundDecisionWarning = true
			break
		}
	}
	if !foundDecisionWarning {
		t.Errorf("expected a triggers.decision warning for rule-decision, got %v", warnings)
	}

	// 4. HasConditionalRules reports the presence of non-always rules.
	conditional := &warp.ResolvedAgent{Agent: mainAgent, Rules: []warp.Rule{*ruleConditional}}
	if !cat.HasConditionalRules(conditional) {
		t.Error("HasConditionalRules with a non-always rule = false, want true")
	}
	allAlways := &warp.ResolvedAgent{Agent: mainAgent, Rules: []warp.Rule{*ruleAlways}}
	if cat.HasConditionalRules(allAlways) {
		t.Error("HasConditionalRules with only always rules = true, want false")
	}
	if cat.HasConditionalRules(nil) {
		t.Error("HasConditionalRules(nil) = true, want false")
	}
}

// TestCatalogToolSnapshots verifies that Catalog.ToolSnapshots() exposes the full
// api.Tool schema (scope, command, JSON schemas, annotations, and token cost) for
// both built-in and custom effective tools, and that GetToolSnapshot resolves by
// short name and qualified name while GetTool keeps returning the warp definition.
func TestCatalogToolSnapshots(t *testing.T) {
	ws := &warp.Workspace{
		Def: &warp.WorkspaceDef{
			BaseResource: warp.BaseResource{
				APIVersion: warp.APIVersion,
				Kind:       warp.KindWorkspace,
				Metadata:   warp.Metadata{Name: "tools-ws"},
			},
			Spec: warp.WorkspaceDefSpec{DefaultAgent: "main"},
		},
	}

	builtinTool := &warp.Tool{
		BaseResource: warp.BaseResource{
			APIVersion: warp.APIVersion,
			Kind:       warp.KindTool,
			Namespace:  warp.NamespaceSystem,
			Metadata:   warp.Metadata{Name: "view", DisplayName: "View File", Description: "read a file"},
		},
		Spec: warp.ToolSpec{
			Instructions: "Read the contents of a text file.",
			Annotations:  &warp.ToolAnnotation{IsReadOnly: true, IsIdempotent: true, UserHint: "Read file: %s"},
			InputSchema:  map[string]any{"type": "object", "required": []any{"path"}},
		},
	}

	customTool := &warp.Tool{
		BaseResource: warp.BaseResource{
			APIVersion: warp.APIVersion,
			Kind:       warp.KindTool,
			Namespace:  warp.NamespaceWorkspace,
			Metadata:   warp.Metadata{Name: "linter", DisplayName: "Linter", Description: "lint the workspace"},
		},
		Spec: warp.ToolSpec{
			Instructions: "Run the project linter.",
			Command:      []string{"golangci-lint", "run"},
			Annotations:  &warp.ToolAnnotation{IsDangerous: true, IsOpenWorld: true, UserHint: "Lint workspace"},
			OutputSchema: map[string]any{"type": "string"},
		},
	}

	mainAgent := &warp.Agent{
		BaseResource: warp.BaseResource{
			APIVersion: warp.APIVersion,
			Kind:       warp.KindAgent,
			Namespace:  warp.NamespaceWorkspace,
			Metadata:   warp.Metadata{Name: "main"},
		},
		Spec: warp.AgentSpec{},
	}

	reg := warp.NewRegistry(ws)
	for _, res := range []warp.Resource{builtinTool, customTool, mainAgent} {
		reg.Set(res.QualifiedName(), res)
	}
	if err := reg.Validate(); err != nil {
		t.Fatalf("registry validation failed: %v", err)
	}

	cat := NewCatalog(reg, "", "")

	// 1. ToolSnapshots() returns every effective tool sorted by name.
	snaps := cat.ToolSnapshots()
	if len(snaps) != 2 {
		t.Fatalf("ToolSnapshots() returned %d tools, want 2: %+v", len(snaps), snaps)
	}
	if snaps[0].Name != "linter" || snaps[1].Name != "view" {
		t.Fatalf("ToolSnapshots() order = [%s %s], want [linter view]", snaps[0].Name, snaps[1].Name)
	}

	// 2. Custom workspace tool carries command, scope, schemas, and annotations.
	linter := snaps[0]
	if linter.DisplayName != "Linter" || linter.Description != "lint the workspace" {
		t.Errorf("linter metadata = %+v", linter)
	}
	if linter.Namespace != warp.NamespaceWorkspace || linter.Scope != "Workspace" {
		t.Errorf("linter scope = %q/%q, want workspace/Workspace", linter.Namespace, linter.Scope)
	}
	if !slices.Equal(linter.Command, []string{"golangci-lint", "run"}) {
		t.Errorf("linter Command = %v, want [golangci-lint run]", linter.Command)
	}
	if linter.OutputSchema["type"] != "string" {
		t.Errorf("linter OutputSchema = %v, want type string", linter.OutputSchema)
	}
	if !linter.Annotations.IsDangerous || !linter.Annotations.IsOpenWorld || linter.Annotations.IsReadOnly {
		t.Errorf("linter annotations = %+v, want dangerous+openWorld only", linter.Annotations)
	}
	if linter.Annotations.UserHint != "Lint workspace" {
		t.Errorf("linter annotations UserHint = %q", linter.Annotations.UserHint)
	}
	if linter.Tokens <= 0 {
		t.Errorf("linter Tokens = %d, want > 0", linter.Tokens)
	}

	// 3. Built-in tool reports the Builtin scope and read-only annotations.
	view := snaps[1]
	if view.Namespace != warp.NamespaceSystem || view.Scope != "Builtin" {
		t.Errorf("view scope = %q/%q, want system/Builtin", view.Namespace, view.Scope)
	}
	if !view.Annotations.IsReadOnly || !view.Annotations.IsIdempotent {
		t.Errorf("view annotations = %+v, want readOnly+idempotent", view.Annotations)
	}
	if view.Annotations.UserHint != "Read file: %s" {
		t.Errorf("view annotations UserHint = %q", view.Annotations.UserHint)
	}
	if len(view.InputSchema) == 0 || view.OutputSchema != nil {
		t.Errorf("view schemas = in:%v out:%v", view.InputSchema, view.OutputSchema)
	}
	if len(view.Command) != 0 {
		t.Errorf("view Command = %v, want empty for host-native tool", view.Command)
	}
	if view.Tokens <= 0 {
		t.Errorf("view Tokens = %d, want > 0", view.Tokens)
	}

	// 4. Snapshots carry the defining warp.Tool, and lookups resolve by short
	// name and qualified name.
	if linter.Def != customTool {
		t.Errorf("linter Def = %v, want the defining warp.Tool", linter.Def)
	}
	snap, ok := cat.GetToolSnapshot("view")
	if !ok || snap.Name != "view" || snap.Scope != "Builtin" {
		t.Errorf("GetToolSnapshot(view) = %+v, ok=%v", snap, ok)
	}
	if _, ok := cat.GetToolSnapshot(customTool.QualifiedName()); !ok {
		t.Errorf("GetToolSnapshot(%q) not found", customTool.QualifiedName())
	}
	if _, ok := cat.GetToolSnapshot("does-not-exist"); ok {
		t.Error("GetToolSnapshot(does-not-exist) unexpectedly found")
	}

	// 5. Tools()/GetTool still surface the warp definitions.
	if defs := cat.Tools(); len(defs) != 2 || defs[0] != customTool || defs[1] != builtinTool {
		t.Errorf("Tools() returned unexpected definitions: %+v", defs)
	}
	if def, ok := cat.GetTool("linter"); !ok || def != customTool {
		t.Errorf("GetTool(linter) = %v, ok=%v", def, ok)
	}
	if names := cat.ToolNames(); !slices.Equal(names, []string{"linter", "view"}) {
		t.Errorf("ToolNames() = %v, want [linter view]", names)
	}
}

// TestCatalogToolStatuses verifies that ToolStatuses classifies every effective
// tool for a resolved agent as eager, deferred, denied, or inactive, matching
// what the session actually loads, and that an agentless workspace reports
// every tool as eager.
func TestCatalogToolStatuses(t *testing.T) {
	ws := &warp.Workspace{
		Def: &warp.WorkspaceDef{
			BaseResource: warp.BaseResource{
				APIVersion: warp.APIVersion,
				Kind:       warp.KindWorkspace,
				Metadata:   warp.Metadata{Name: "statuses-ws"},
			},
			Spec: warp.WorkspaceDefSpec{DefaultAgent: "main"},
		},
	}

	mkTool := func(namespace, name string, annotations *warp.ToolAnnotation) *warp.Tool {
		return &warp.Tool{
			BaseResource: warp.BaseResource{
				APIVersion: warp.APIVersion,
				Kind:       warp.KindTool,
				Namespace:  namespace,
				Metadata:   warp.Metadata{Name: name, Description: "desc for " + name},
			},
			Spec: warp.ToolSpec{Annotations: annotations},
		}
	}

	viewTool := mkTool(warp.NamespaceSystem, "view", &warp.ToolAnnotation{IsReadOnly: true})
	linterTool := mkTool(warp.NamespaceWorkspace, "linter", &warp.ToolAnnotation{IsDangerous: true})
	searchTool := mkTool(warp.NamespaceSystem, "tool_search", &warp.ToolAnnotation{IsReadOnly: true})
	subagentTool := mkTool(warp.NamespaceSystem, "invoke_agent", &warp.ToolAnnotation{IsReadOnly: true})

	agentNamed := func(name string, policies *warp.Policies) *warp.Agent {
		return &warp.Agent{
			BaseResource: warp.BaseResource{
				APIVersion: warp.APIVersion,
				Kind:       warp.KindAgent,
				Namespace:  warp.NamespaceWorkspace,
				Metadata:   warp.Metadata{Name: name},
			},
			Spec: warp.AgentSpec{Tools: []string{"view"}, Policies: policies},
		}
	}

	reg := warp.NewRegistry(ws)
	for _, res := range []warp.Resource{viewTool, linterTool, searchTool, subagentTool, agentNamed("main", nil)} {
		reg.Set(res.QualifiedName(), res)
	}
	if err := reg.Validate(); err != nil {
		t.Fatalf("registry validation failed: %v", err)
	}

	cat := NewCatalog(reg, "", "")

	// 1. An agentless workspace loads every effective tool eagerly.
	if statuses := cat.ToolStatuses(nil); len(statuses) != 4 {
		t.Fatalf("ToolStatuses(nil) = %v, want 4 entries", statuses)
	} else {
		for name, status := range statuses {
			if status != api.ToolStatusEager {
				t.Errorf("agentless status for %q = %q, want eager", name, status)
			}
		}
	}

	// 2. Listed tools are eager, unlisted policy-allowed tools are deferred,
	// tool_search is auto-activated while anything is deferred, and a
	// conditional built-in whose precondition is unmet is neither.
	resolved := &warp.ResolvedAgent{Agent: agentNamed("main", nil), Tools: []*warp.Tool{viewTool}}
	statuses := cat.ToolStatuses(resolved)
	want := map[string]api.ToolStatus{
		"view":         api.ToolStatusEager,
		"linter":       api.ToolStatusDeferred,
		"tool_search":  api.ToolStatusEager,
		"invoke_agent": api.ToolStatusInactive,
	}
	if len(statuses) != len(want) {
		t.Fatalf("ToolStatuses returned %d entries, want %d: %v", len(statuses), len(want), statuses)
	}
	for name, wantStatus := range want {
		if got := statuses[name]; got != wantStatus {
			t.Errorf("status for %q = %q, want %q", name, got, wantStatus)
		}
	}

	// 3. A policy ceiling denial surfaces as denied rather than deferred.
	denyPolicies := &warp.Policies{Tools: &warp.ToolPolicies{Exclude: []string{"linter"}}}
	denied := cat.ToolStatuses(&warp.ResolvedAgent{Agent: agentNamed("strict", denyPolicies), Tools: []*warp.Tool{viewTool}})
	if got := denied["linter"]; got != api.ToolStatusDenied {
		t.Errorf("excluded tool status = %q, want denied", got)
	}
	if got := denied["view"]; got != api.ToolStatusEager {
		t.Errorf("listed tool status = %q, want eager", got)
	}
}

// TestPartitionAgentToolsLoadRule verifies that load_rule is auto-activated as an
// eager tool whenever the resolved agent has conditional rules (even when the
// agent omits it from spec.tools), is never placed in the deferred partition, and
// is omitted entirely when the agent has only always-active rules or explicitly
// denies load_rule via its tool policy ceiling.
func TestPartitionAgentToolsLoadRule(t *testing.T) {
	ws := &warp.Workspace{
		Def: &warp.WorkspaceDef{
			BaseResource: warp.BaseResource{
				APIVersion: warp.APIVersion,
				Kind:       warp.KindWorkspace,
				Metadata:   warp.Metadata{Name: "load-rule-ws"},
			},
			Spec: warp.WorkspaceDefSpec{DefaultAgent: "main"},
		},
	}

	mkRule := func(name string, trig warp.RuleTriggers, priority int, instructions string) *warp.Rule {
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

	ruleAlways := mkRule("rule-always", warp.RuleTriggers{Always: true}, 10, "Always follow this rule.")
	ruleConditional := mkRule("rule-conditional", warp.RuleTriggers{Globs: []string{"**/*.go"}}, 50, "Follow this conditional rule.")

	loadRuleTool := &warp.Tool{
		BaseResource: warp.BaseResource{
			APIVersion: warp.APIVersion,
			Kind:       warp.KindTool,
			Namespace:  warp.NamespaceWorkspace,
			Metadata:   warp.Metadata{Name: "load_rule", Description: "load a rule"},
		},
		Spec: warp.ToolSpec{Annotations: &warp.ToolAnnotation{IsReadOnly: true}},
	}

	// main agent that does NOT list load_rule in spec.tools.
	mainAgent := &warp.Agent{
		BaseResource: warp.BaseResource{
			APIVersion: warp.APIVersion,
			Kind:       warp.KindAgent,
			Namespace:  warp.NamespaceWorkspace,
			Metadata:   warp.Metadata{Name: "main"},
		},
		Spec: warp.AgentSpec{Tools: []string{"bash"}},
	}

	// denyAgent explicitly excludes load_rule via its tool policy ceiling.
	denyAgent := &warp.Agent{
		BaseResource: warp.BaseResource{
			APIVersion: warp.APIVersion,
			Kind:       warp.KindAgent,
			Namespace:  warp.NamespaceWorkspace,
			Metadata:   warp.Metadata{Name: "deny"},
		},
		Spec: warp.AgentSpec{
			Tools:    []string{"bash"},
			Policies: &warp.Policies{Tools: &warp.ToolPolicies{Exclude: []string{"load_rule"}}},
		},
	}

	bashTool := &warp.Tool{
		BaseResource: warp.BaseResource{
			APIVersion: warp.APIVersion,
			Kind:       warp.KindTool,
			Namespace:  warp.NamespaceWorkspace,
			Metadata:   warp.Metadata{Name: "bash", Description: "run shell commands"},
		},
		Spec: warp.ToolSpec{Annotations: &warp.ToolAnnotation{IsReadOnly: false}},
	}

	reg := warp.NewRegistry(ws)
	for _, res := range []warp.Resource{
		ruleAlways, ruleConditional, loadRuleTool, mainAgent, denyAgent, bashTool,
	} {
		reg.Set(res.QualifiedName(), res)
	}
	if err := reg.Validate(); err != nil {
		t.Fatalf("registry validation failed: %v", err)
	}

	cat := NewCatalog(reg, "", "")

	// 1. Conditional rules => load_rule auto-activated as eager even though the
	// agent omits it from spec.tools, and it is never deferred.
	resolvedCond := &warp.ResolvedAgent{Agent: mainAgent, Tools: []*warp.Tool{bashTool}, Rules: []warp.Rule{*ruleConditional}}
	part := cat.PartitionAgentTools(resolvedCond)
	if !slices.Contains(part.Eager, "load_rule") {
		t.Errorf("conditional rules: load_rule not in Eager = %v", part.Eager)
	}
	if slices.Contains(part.Deferred, "load_rule") {
		t.Errorf("conditional rules: load_rule must never be deferred, got Deferred = %v", part.Deferred)
	}

	// 2. Only always-active rules => load_rule omitted from both partitions.
	resolvedAlways := &warp.ResolvedAgent{Agent: mainAgent, Tools: []*warp.Tool{bashTool}, Rules: []warp.Rule{*ruleAlways}}
	partAlways := cat.PartitionAgentTools(resolvedAlways)
	if slices.Contains(partAlways.Eager, "load_rule") {
		t.Errorf("always-only rules: load_rule unexpectedly in Eager = %v", partAlways.Eager)
	}
	if slices.Contains(partAlways.Deferred, "load_rule") {
		t.Errorf("always-only rules: load_rule unexpectedly in Deferred = %v", partAlways.Deferred)
	}

	// 3. Explicit exclude => load_rule is not auto-activated.
	resolvedDeny := &warp.ResolvedAgent{Agent: denyAgent, Tools: []*warp.Tool{bashTool}, Rules: []warp.Rule{*ruleConditional}}
	partDeny := cat.PartitionAgentTools(resolvedDeny)
	if slices.Contains(partDeny.Eager, "load_rule") {
		t.Errorf("excluded load_rule: unexpectedly in Eager = %v", partDeny.Eager)
	}
	if slices.Contains(partDeny.Deferred, "load_rule") {
		t.Errorf("excluded load_rule: unexpectedly in Deferred = %v", partDeny.Deferred)
	}
}

// TestConditionalTemplateRules verifies that ConditionalTemplateRules returns
// only the non-always rules (sorted by priority descending then name ascending)
// with their rendered instructions and trigger fields, that AgentRenderGlobals
// exposes them under "ConditionalRules", and that a nil resolved agent yields
// nil.
func TestConditionalTemplateRules(t *testing.T) {
	ws := &warp.Workspace{
		Def: &warp.WorkspaceDef{
			BaseResource: warp.BaseResource{
				APIVersion: warp.APIVersion,
				Kind:       warp.KindWorkspace,
				Metadata:   warp.Metadata{Name: "cond-rules-ws"},
			},
			Spec: warp.WorkspaceDefSpec{DefaultAgent: "main"},
		},
	}

	mkRule := func(name string, trig warp.RuleTriggers, priority int, instructions string) *warp.Rule {
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

	always := mkRule("always-rule", warp.RuleTriggers{Always: true}, 10, "Always.")
	condLow := mkRule("cond-low", warp.RuleTriggers{Keywords: []string{"kw"}}, 10, "Low priority conditional.")
	condHigh := mkRule("cond-high", warp.RuleTriggers{Globs: []string{"**/*.go"}}, 90, "High priority conditional.")

	mainAgent := &warp.Agent{
		BaseResource: warp.BaseResource{
			APIVersion: warp.APIVersion,
			Kind:       warp.KindAgent,
			Namespace:  warp.NamespaceWorkspace,
			Metadata:   warp.Metadata{Name: "main"},
		},
		Spec: warp.AgentSpec{},
	}

	reg := warp.NewRegistry(ws)
	for _, res := range []warp.Resource{always, condLow, condHigh, mainAgent} {
		reg.Set(res.QualifiedName(), res)
	}
	if err := reg.Validate(); err != nil {
		t.Fatalf("registry validation failed: %v", err)
	}

	cat := NewCatalog(reg, "", "")

	// 1. ConditionalTemplateRules excludes always rules and sorts by priority
	//    descending, then name ascending.
	resolved := &warp.ResolvedAgent{Agent: mainAgent, Rules: []warp.Rule{*always, *condLow, *condHigh}}
	cond := cat.ConditionalTemplateRules(resolved)
	if len(cond) != 2 {
		t.Fatalf("ConditionalTemplateRules returned %d rules, want 2: %+v", len(cond), cond)
	}
	if cond[0].Name != "cond-high" || cond[1].Name != "cond-low" {
		t.Errorf("ConditionalTemplateRules order = [%s %s], want [cond-high cond-low]", cond[0].Name, cond[1].Name)
	}
	if cond[0].Instructions != "High priority conditional." {
		t.Errorf("cond-high Instructions = %q, want rendered body", cond[0].Instructions)
	}
	if !slices.Equal(cond[0].Globs, []string{"**/*.go"}) {
		t.Errorf("cond-high Globs = %v, want [**/*.go]", cond[0].Globs)
	}
	if len(cond[1].Keywords) != 1 || cond[1].Keywords[0] != "kw" {
		t.Errorf("cond-low Keywords = %v, want [kw]", cond[1].Keywords)
	}

	// 2. AgentRenderGlobals exposes the conditional rules under "ConditionalRules".
	globals := cat.AgentRenderGlobals(resolved)
	cr, ok := globals["ConditionalRules"]
	if !ok {
		t.Fatal("AgentRenderGlobals missing ConditionalRules key")
	}
	if _, ok := cr.([]templateRule); !ok {
		t.Errorf("ConditionalRules = %T, want []templateRule", cr)
	}

	// 3. Nil resolved returns nil.
	if got := cat.ConditionalTemplateRules(nil); got != nil {
		t.Errorf("ConditionalTemplateRules(nil) = %v, want nil", got)
	}
}

// TestCatalogMCPSnapshots verifies that Catalog.MCPSnapshots(), Catalog.MCPs(),
// and Catalog.GetMCPSnapshot()/GetMCP() expose the manifest-derived api.MCPServer
// presentation (scope, transport, command or endpoint, auth type, portable path)
// for every effective MCP server, defaulting an unset transport to stdio and
// reporting a disconnected status until an MCP runtime is attached.
func TestCatalogMCPSnapshots(t *testing.T) {
	ws := &warp.Workspace{
		Def: &warp.WorkspaceDef{
			BaseResource: warp.BaseResource{
				APIVersion: warp.APIVersion,
				Kind:       warp.KindWorkspace,
				Metadata:   warp.Metadata{Name: "mcp-ws"},
			},
			Spec: warp.WorkspaceDefSpec{DefaultAgent: "main"},
		},
	}

	mainAgent := &warp.Agent{
		BaseResource: warp.BaseResource{
			APIVersion: warp.APIVersion,
			Kind:       warp.KindAgent,
			Namespace:  warp.NamespaceWorkspace,
			Metadata:   warp.Metadata{Name: "main"},
		},
		Spec: warp.AgentSpec{},
	}

	stdioMCP := &warp.MCP{
		BaseResource: warp.BaseResource{
			APIVersion: warp.APIVersion,
			Kind:       warp.KindMCP,
			Namespace:  warp.NamespaceWorkspace,
			Metadata:   warp.Metadata{Name: "github", DisplayName: "GitHub", Description: "GitHub MCP server"},
			Path:       "/ws/.agents/github.mcp.yaml",
		},
		Spec: warp.MCPSpec{
			Command: []string{"npx", "-y", "@modelcontextprotocol/server-github"},
		},
	}

	httpMCP := &warp.MCP{
		BaseResource: warp.BaseResource{
			APIVersion: warp.APIVersion,
			Kind:       warp.KindMCP,
			Namespace:  warp.NamespaceWorkspace,
			Metadata:   warp.Metadata{Name: "linear", Description: "Linear MCP server"},
		},
		Spec: warp.MCPSpec{
			Type:     warp.MCPTransportHTTP,
			Endpoint: "https://mcp.linear.app/sse",
			Auth:     &warp.ProviderAuth{Type: "oauth2"},
		},
	}

	reg := warp.NewRegistry(ws)
	for _, res := range []warp.Resource{mainAgent, stdioMCP, httpMCP} {
		reg.Set(res.QualifiedName(), res)
	}
	if err := reg.Validate(); err != nil {
		t.Fatalf("registry validation failed: %v", err)
	}

	cat := NewCatalog(reg, "/ws", "/ws")

	snaps := cat.MCPs()
	if len(snaps) != 2 {
		t.Fatalf("MCPs() returned %d servers, want 2", len(snaps))
	}

	listed := cat.MCPSnapshots()
	if len(listed) != 2 || listed[0].Name != "github" || listed[1].Name != "linear" {
		t.Fatalf("MCPSnapshots() = %+v", listed)
	}

	github := listed[0]
	if github.Transport != string(warp.MCPTransportStdio) {
		t.Errorf("github transport = %q, want stdio default", github.Transport)
	}
	if !slices.Equal(github.Command, []string{"npx", "-y", "@modelcontextprotocol/server-github"}) {
		t.Errorf("github command = %v", github.Command)
	}
	if github.Status != api.MCPStatusDisconnected {
		t.Errorf("github status = %q, want %q", github.Status, api.MCPStatusDisconnected)
	}
	if github.Scope != "Workspace" || github.Namespace != warp.NamespaceWorkspace {
		t.Errorf("github scope/namespace = %q/%q", github.Scope, github.Namespace)
	}
	if github.DisplayName != "GitHub" || github.Description != "GitHub MCP server" {
		t.Errorf("github metadata = %+v", github)
	}
	if github.AuthType != "" {
		t.Errorf("github authType = %q, want empty", github.AuthType)
	}
	if github.Endpoint != "" {
		t.Errorf("github endpoint = %q, want empty", github.Endpoint)
	}
	if github.Def != stdioMCP {
		t.Errorf("github snapshot must carry its warp.MCP definition")
	}

	linear := listed[1]
	if linear.Transport != string(warp.MCPTransportHTTP) || linear.Endpoint != "https://mcp.linear.app/sse" {
		t.Errorf("linear transport/endpoint = %q %q", linear.Transport, linear.Endpoint)
	}
	if linear.AuthType != "oauth2" {
		t.Errorf("linear authType = %q, want oauth2", linear.AuthType)
	}
	if len(linear.Command) != 0 {
		t.Errorf("expected no command for an HTTP server, got %v", linear.Command)
	}

	// Lookups resolve by short name and qualified name; unknown names miss.
	if byQ, ok := cat.GetMCPSnapshot(stdioMCP.QualifiedName()); !ok || byQ.Name != "github" {
		t.Errorf("GetMCPSnapshot(qualified) = (%+v, %v)", byQ, ok)
	}
	if def, ok := cat.GetMCP("linear"); !ok || def != httpMCP {
		t.Errorf("GetMCP(linear) = (%v, %v)", def, ok)
	}
	if _, ok := cat.GetMCPSnapshot("missing"); ok {
		t.Errorf("expected unknown MCP name to miss")
	}
	if _, ok := cat.GetMCPSnapshot("   "); ok {
		t.Errorf("expected blank MCP name to miss")
	}
	if _, ok := cat.GetMCP(stdioMCP.QualifiedName()); !ok {
		t.Errorf("expected GetMCP to resolve qualified names")
	}
}

// mcpCatalogFixture builds a workspace holding one stdio MCP server, one plain
// workspace tool, and agents exercising MCP tool loading, then simulates runtime
// discovery so the catalog holds the server's materialized tools.
type mcpCatalogFixture struct {
	catalog *Catalog
	mcp     *warp.MCP
	echo    *warp.Tool
	blocked *warp.Tool
}

func newMCPCatalog(t *testing.T, policies *warp.MCPPolicies, agentTools ...[]string) *mcpCatalogFixture {
	t.Helper()

	ws := &warp.Workspace{
		Def: &warp.WorkspaceDef{
			BaseResource: warp.BaseResource{
				APIVersion: warp.APIVersion,
				Kind:       warp.KindWorkspace,
				Metadata:   warp.Metadata{Name: "mcp-tools-ws"},
			},
			Spec: warp.WorkspaceDefSpec{DefaultAgent: "main"},
		},
	}

	server := &warp.MCP{
		BaseResource: warp.BaseResource{
			APIVersion: warp.APIVersion,
			Kind:       warp.KindMCP,
			Namespace:  warp.NamespaceWorkspace,
			Metadata:   warp.Metadata{Name: "notes", Description: "Notes MCP server"},
			Path:       "/ws/.agents/notes.mcp.yaml",
		},
		Spec: warp.MCPSpec{
			Command:  []string{"notes-mcp"},
			Policies: policies,
		},
	}

	bashTool := &warp.Tool{
		BaseResource: warp.BaseResource{
			APIVersion: warp.APIVersion,
			Kind:       warp.KindTool,
			Namespace:  warp.NamespaceWorkspace,
			Metadata:   warp.Metadata{Name: "bash", Description: "run shell commands"},
		},
		Spec: warp.ToolSpec{Annotations: &warp.ToolAnnotation{IsReadOnly: false}},
	}

	resources := []warp.Resource{server, bashTool}
	// One agent per spec.tools list, named main, explicit, and prefixed in order.
	names := []string{"main", "explicit", "prefixed"}
	for i, tools := range agentTools {
		resources = append(resources, &warp.Agent{
			BaseResource: warp.BaseResource{
				APIVersion: warp.APIVersion,
				Kind:       warp.KindAgent,
				Namespace:  warp.NamespaceWorkspace,
				Metadata:   warp.Metadata{Name: names[i]},
			},
			Spec: warp.AgentSpec{Tools: tools},
		})
	}

	// Discovery reports the materialized names the manager publishes, matching
	// what the registry expects for a live server.
	echo := &warp.Tool{
		BaseResource: warp.BaseResource{
			APIVersion: warp.APIVersion,
			Kind:       warp.KindTool,
			Metadata:   warp.Metadata{Name: "notes__echo", Description: "Echo text back"},
		},
		Spec: warp.ToolSpec{
			InputSchema: map[string]any{"type": "object"},
			Annotations: &warp.ToolAnnotation{IsReadOnly: true},
		},
	}
	destructive := &warp.Tool{
		BaseResource: warp.BaseResource{
			APIVersion: warp.APIVersion,
			Kind:       warp.KindTool,
			Metadata:   warp.Metadata{Name: "notes__delete_everything", Description: "Delete everything"},
		},
	}

	reg := warp.NewRegistry(ws)
	for _, res := range resources {
		reg.Set(res.QualifiedName(), res)
	}
	// Registering discovery before validation mirrors a workspace reload that
	// happens after a server has already connected.
	if err := reg.SetMCPTools(server.QualifiedName(), []*warp.Tool{echo, destructive}); err != nil {
		t.Fatalf("register discovered MCP tools: %v", err)
	}
	// Validation failures stay non-fatal here, exactly as Catalog.populate treats
	// them: an agent naming a tool as "mcp__<server>__<tool>" is reported as a
	// dangling reference by warp because only the materialized name is registered,
	// yet the workspace must still open and the alias must still select the tool.
	if err := reg.Validate(); err != nil && !strings.Contains(err.Error(), "mcp__notes__echo") {
		t.Fatalf("registry validation failed: %v", err)
	}

	cat := NewCatalog(reg, "/ws", "/ws")
	if err := cat.Refresh(); err != nil {
		t.Fatalf("catalog refresh: %v", err)
	}
	return &mcpCatalogFixture{catalog: cat, mcp: server, echo: echo, blocked: destructive}
}

// TestCatalogMCPSnapshotScope verifies a discovered MCP tool is listed with the
// server it came from as its scope and the declaring manifest's namespace, while a
// regular workspace tool keeps the plain workspace scope.
func TestCatalogMCPSnapshotScope(t *testing.T) {
	f := newMCPCatalog(t, nil, []string{"*"})

	snap, ok := f.catalog.GetToolSnapshot("notes__echo")
	if !ok {
		t.Fatalf("discovered MCP tool missing from catalog: %+v", f.catalog.ToolNames())
	}
	if snap.Scope != "MCP (notes)" {
		t.Errorf("MCP tool scope = %q, want %q", snap.Scope, "MCP (notes)")
	}
	if snap.Namespace != warp.NamespaceWorkspace {
		t.Errorf("MCP tool namespace = %q, want %q", snap.Namespace, warp.NamespaceWorkspace)
	}
	if !snap.Annotations.IsReadOnly {
		t.Errorf("discovery annotations must be carried: %+v", snap.Annotations)
	}

	bash, ok := f.catalog.GetToolSnapshot("bash")
	if !ok {
		t.Fatalf("workspace tool missing from catalog")
	}
	if bash.Scope == "MCP (notes)" || bash.Scope == "" {
		t.Errorf("regular tool scope = %q, want a non-MCP scope", bash.Scope)
	}
}

// TestPartitionAgentToolsMCPDeferral verifies MCP tools stay deferred under a
// wildcard spec.tools so they do not crowd out the initial prompt, and become
// eager only when the agent names one outright, written either as
// "<server>__<tool>" or with the optional "mcp__" prefix.
func TestPartitionAgentToolsMCPDeferral(t *testing.T) {
	f := newMCPCatalog(t, nil,
		[]string{"*"},
		[]string{"bash", "notes__echo"},
		[]string{"*", "mcp__notes__echo"},
	)

	// 1. Wildcard: bash is eager, MCP tools are deferred but still reachable.
	wildcard, err := f.catalog.ResolveAgent("main")
	if err != nil {
		t.Fatalf("ResolveAgent(main): %v", err)
	}
	part := f.catalog.PartitionAgentTools(wildcard)
	if slices.Contains(part.Eager, "notes__echo") {
		t.Errorf("MCP tool must not be eager under a wildcard: Eager = %v", part.Eager)
	}
	if !slices.Contains(part.Deferred, "notes__echo") {
		t.Errorf("MCP tool must be deferred so tool_search can find it: Deferred = %v", part.Deferred)
	}
	if !slices.Contains(part.Eager, "bash") {
		t.Errorf("regular tool should stay eager under a wildcard: Eager = %v", part.Eager)
	}

	// 2. Naming the materialized name promotes it to eager.
	named, err := f.catalog.ResolveAgent("explicit")
	if err != nil {
		t.Fatalf("ResolveAgent(explicit): %v", err)
	}
	partNamed := f.catalog.PartitionAgentTools(named)
	if !slices.Contains(partNamed.Eager, "notes__echo") {
		t.Errorf("explicitly named MCP tool must be eager: Eager = %v", partNamed.Eager)
	}
	if slices.Contains(partNamed.Deferred, "notes__echo") {
		t.Errorf("an eager tool must not also be deferred: Deferred = %v", partNamed.Deferred)
	}
	if slices.Contains(partNamed.Eager, "notes__delete_everything") {
		t.Errorf("an unnamed sibling MCP tool must stay deferred: Eager = %v", partNamed.Eager)
	}

	// 3. The "mcp__" prefix form users write in the directory also promotes it.
	prefixed, err := f.catalog.ResolveAgent("prefixed")
	if err != nil {
		t.Fatalf("ResolveAgent(prefixed): %v", err)
	}
	partPrefixed := f.catalog.PartitionAgentTools(prefixed)
	if !slices.Contains(partPrefixed.Eager, "notes__echo") {
		t.Errorf("mcp__-prefixed tool name must match the materialized name: Eager = %v", partPrefixed.Eager)
	}
}

// TestCatalogDeniedMCPTools verifies tools the server exposes but spec.policies
// prohibits are recorded as policy-blocked, reported with a denied status, and
// replaced rather than accumulated when a server re-discovers.
func TestCatalogDeniedMCPTools(t *testing.T) {
	f := newMCPCatalog(t,
		&warp.MCPPolicies{Tools: &warp.ResourceFilter{Exclude: []string{"delete_*"}}},
		[]string{"*"},
	)

	f.catalog.SetDeniedMCPTools(f.mcp, []*warp.Tool{f.blocked})

	denied := f.catalog.DeniedMCPToolSnapshots()
	if len(denied) != 1 || denied[0].Name != "notes__delete_everything" {
		t.Fatalf("DeniedMCPToolSnapshots() = %+v", denied)
	}
	if denied[0].Status != api.ToolStatusDenied {
		t.Errorf("denied status = %q, want %q", denied[0].Status, api.ToolStatusDenied)
	}
	if denied[0].Namespace != warp.NamespaceWorkspace {
		t.Errorf("denied namespace = %q, want the declaring manifest's namespace", denied[0].Namespace)
	}
	if denied[0].Scope != "MCP (notes)" {
		t.Errorf("denied scope = %q, want %q", denied[0].Scope, "MCP (notes)")
	}

	// A blocked tool must never appear among the effective, callable tools.
	if slices.Contains(f.catalog.ToolNames(), "notes__delete_everything") {
		t.Errorf("a policy-blocked tool must not be effective: %v", f.catalog.ToolNames())
	}
	// ...but it must be visible as denied so the UI can explain the restriction.
	resolved, err := f.catalog.ResolveAgent("main")
	if err != nil {
		t.Fatalf("ResolveAgent(main): %v", err)
	}
	statuses := f.catalog.ToolStatuses(resolved)
	if statuses["notes__delete_everything"] != api.ToolStatusDenied {
		t.Errorf("blocked MCP tool status = %q, want %q",
			statuses["notes__delete_everything"], api.ToolStatusDenied)
	}
	if statuses["notes__echo"] != api.ToolStatusDeferred {
		t.Errorf("permitted MCP tool status = %q, want %q",
			statuses["notes__echo"], api.ToolStatusDeferred)
	}

	// Re-discovery without the blocked tool must clear the stale entry.
	f.catalog.SetDeniedMCPTools(f.mcp, nil)
	if got := f.catalog.DeniedMCPToolSnapshots(); len(got) != 0 {
		t.Errorf("clearing a server's blocked tools left %+v", got)
	}
}

// TestCatalogAgentContextMCPToolTokens verifies an eager MCP tool's token cost is
// attributed to the MCP bucket instead of the system or custom tool buckets.
func TestCatalogAgentContextMCPToolTokens(t *testing.T) {
	f := newMCPCatalog(t, nil, []string{"bash", "notes__echo"})

	echo, ok := f.catalog.GetToolSnapshot("notes__echo")
	if !ok {
		t.Fatalf("MCP tool missing from catalog")
	}
	bash, ok := f.catalog.GetToolSnapshot("bash")
	if !ok {
		t.Fatalf("workspace tool missing from catalog")
	}
	if echo.Tokens <= 0 || bash.Tokens <= 0 {
		t.Fatalf("tool snapshots must carry token costs: mcp=%d bash=%d", echo.Tokens, bash.Tokens)
	}

	breakdown := f.catalog.AgentContext("main")
	if breakdown.MCPTools != echo.Tokens {
		t.Errorf("MCP bucket = %d, want the eager MCP tool's %d tokens", breakdown.MCPTools, echo.Tokens)
	}
	// The deferred sibling must not be counted at all, and the MCP tool's cost must
	// not leak into the custom bucket alongside the plain workspace tool.
	if breakdown.CustomTools != bash.Tokens {
		t.Errorf("custom bucket = %d, want only the workspace tool's %d tokens", breakdown.CustomTools, bash.Tokens)
	}
}

// TestPartitionAgentToolsReadMCPResource verifies read_mcp_resource is activated
// only while the workspace actually exposes a readable MCP resource: it is eager
// even when the agent omits it, never deferred, omitted while no resource is
// readable, and never overrides an agent that denies it.
func TestPartitionAgentToolsReadMCPResource(t *testing.T) {
	ws := &warp.Workspace{
		Def: &warp.WorkspaceDef{
			BaseResource: warp.BaseResource{
				APIVersion: warp.APIVersion,
				Kind:       warp.KindWorkspace,
				Metadata:   warp.Metadata{Name: "read-resource-ws"},
			},
			Spec: warp.WorkspaceDefSpec{DefaultAgent: "main"},
		},
	}

	resourceTool := &warp.Tool{
		BaseResource: warp.BaseResource{
			APIVersion: warp.APIVersion,
			Kind:       warp.KindTool,
			Namespace:  warp.NamespaceSystem,
			Metadata:   warp.Metadata{Name: "read_mcp_resource", Description: "read an MCP resource"},
		},
		Spec: warp.ToolSpec{Annotations: &warp.ToolAnnotation{IsReadOnly: true}},
	}
	bashTool := &warp.Tool{
		BaseResource: warp.BaseResource{
			APIVersion: warp.APIVersion,
			Kind:       warp.KindTool,
			Namespace:  warp.NamespaceWorkspace,
			Metadata:   warp.Metadata{Name: "bash", Description: "run shell commands"},
		},
		Spec: warp.ToolSpec{Annotations: &warp.ToolAnnotation{IsReadOnly: false}},
	}

	// main omits read_mcp_resource from spec.tools, as every shipped agent does:
	// no agent author can know whether the servers it talks to expose resources.
	mainAgent := &warp.Agent{
		BaseResource: warp.BaseResource{
			APIVersion: warp.APIVersion,
			Kind:       warp.KindAgent,
			Namespace:  warp.NamespaceWorkspace,
			Metadata:   warp.Metadata{Name: "main"},
		},
		Spec: warp.AgentSpec{Tools: []string{"bash"}},
	}
	denyAgent := &warp.Agent{
		BaseResource: warp.BaseResource{
			APIVersion: warp.APIVersion,
			Kind:       warp.KindAgent,
			Namespace:  warp.NamespaceWorkspace,
			Metadata:   warp.Metadata{Name: "deny"},
		},
		Spec: warp.AgentSpec{
			Tools:    []string{"bash"},
			Policies: &warp.Policies{Tools: &warp.ToolPolicies{Exclude: []string{"read_mcp_resource"}}},
		},
	}

	reg := warp.NewRegistry(ws)
	for _, res := range []warp.Resource{resourceTool, bashTool, mainAgent, denyAgent} {
		reg.Set(res.QualifiedName(), res)
	}
	if err := reg.Validate(); err != nil {
		t.Fatalf("registry validation failed: %v", err)
	}
	cat := NewCatalog(reg, "", "")

	// 1. No runtime probe installed: a workspace without an MCP runtime must not
	// spend context on a tool with nothing to read.
	partNone := cat.PartitionAgentTools(&warp.ResolvedAgent{Agent: mainAgent, Tools: []*warp.Tool{bashTool}})
	if slices.Contains(partNone.Eager, "read_mcp_resource") {
		t.Errorf("no readable resource: read_mcp_resource unexpectedly in Eager = %v", partNone.Eager)
	}
	if slices.Contains(partNone.Deferred, "read_mcp_resource") {
		t.Errorf("read_mcp_resource must never be deferred, got Deferred = %v", partNone.Deferred)
	}
	if statuses := cat.ToolStatuses(&warp.ResolvedAgent{Agent: mainAgent, Tools: []*warp.Tool{bashTool}}); statuses["read_mcp_resource"] != api.ToolStatusInactive {
		t.Errorf("no readable resource: status = %q, want inactive", statuses["read_mcp_resource"])
	}

	// 2. A readable resource appears: the tool becomes eager without any manifest
	// edit, because discovery is what makes it useful.
	readable := false
	cat.SetMCPResourceProbe(func() bool { return readable })
	if cat.HasAllowedMCPResources() {
		t.Fatal("HasAllowedMCPResources = true while the probe reports false")
	}
	readable = true
	if !cat.HasAllowedMCPResources() {
		t.Fatal("HasAllowedMCPResources = false while the probe reports true")
	}
	resolved := &warp.ResolvedAgent{Agent: mainAgent, Tools: []*warp.Tool{bashTool}}
	partReadable := cat.PartitionAgentTools(resolved)
	if !slices.Contains(partReadable.Eager, "read_mcp_resource") {
		t.Errorf("readable resource: read_mcp_resource missing from Eager = %v", partReadable.Eager)
	}
	if slices.Contains(partReadable.Deferred, "read_mcp_resource") {
		t.Errorf("read_mcp_resource must never be deferred, got Deferred = %v", partReadable.Deferred)
	}
	if statuses := cat.ToolStatuses(resolved); statuses["read_mcp_resource"] != api.ToolStatusEager {
		t.Errorf("readable resource: status = %q, want eager", statuses["read_mcp_resource"])
	}

	// 3. An agent that denies it stays denied; activation never overrides policy.
	partDeny := cat.PartitionAgentTools(&warp.ResolvedAgent{Agent: denyAgent, Tools: []*warp.Tool{bashTool}})
	if slices.Contains(partDeny.Eager, "read_mcp_resource") {
		t.Errorf("excluded read_mcp_resource: unexpectedly in Eager = %v", partDeny.Eager)
	}
	if slices.Contains(partDeny.Deferred, "read_mcp_resource") {
		t.Errorf("excluded read_mcp_resource: unexpectedly in Deferred = %v", partDeny.Deferred)
	}

	// 4. Clearing the probe (detaching the runtime) deactivates it again.
	cat.SetMCPResourceProbe(nil)
	partCleared := cat.PartitionAgentTools(resolved)
	if slices.Contains(partCleared.Eager, "read_mcp_resource") {
		t.Errorf("cleared probe: read_mcp_resource unexpectedly in Eager = %v", partCleared.Eager)
	}
}
