package workspace

import (
	"slices"
	"strings"
	"testing"

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
