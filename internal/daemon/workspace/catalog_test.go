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
