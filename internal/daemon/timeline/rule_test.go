package timeline

import (
	"reflect"
	"testing"

	"github.com/masterkeysrd/tasksmith/api"
)

func TestRuleRecordRoundTrip(t *testing.T) {
	tl, _, ctx := openTestTimeline(t, "rules")
	record := api.RuleRecord{Rule: "go-error-handling", Trigger: "manual", Matched: "@rule:go-error-handling", Path: ".agents/rules/go-error-handling.rule.md"}
	entry := NewRuleEntry(record)
	if entry.Kind != api.TimelineKindRule {
		t.Fatalf("entry kind = %q, want %q", entry.Kind, api.TimelineKindRule)
	}
	if err := tl.Append(ctx, entry); err != nil {
		t.Fatal(err)
	}
	got, err := tl.Get(ctx, entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != api.TimelineKindRule || !reflect.DeepEqual(got.Rule, &record) {
		t.Fatalf("unexpected rule record: %+v", got.Rule)
	}
	clone := got.Clone()
	clone.Rule.Rule = "changed"
	if got.Rule.Rule != "go-error-handling" {
		t.Fatal("clone aliases original rule record")
	}
}

func TestRuleRecordList(t *testing.T) {
	tl, _, ctx := openTestTimeline(t, "rules-list")
	entries := []*Entry{
		NewRuleEntry(api.RuleRecord{Rule: "security-baseline", Trigger: "glob", Matched: "internal/daemon/server/server.go", Path: ".agents/rules/security-baseline.rule.md"}),
		NewRuleEntry(api.RuleRecord{Rule: "bash-guard", Trigger: "tool", Matched: "bash"}),
	}
	for _, e := range entries {
		if err := tl.Append(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	list, err := tl.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != len(entries) {
		t.Fatalf("List returned %d entries, want %d", len(list), len(entries))
	}
	for _, e := range list {
		if e.Kind != api.TimelineKindRule || e.Rule == nil {
			t.Fatalf("unexpected entry: %+v", e)
		}
	}
	if list[0].Rule.Rule != "security-baseline" || list[1].Rule.Rule != "bash-guard" {
		t.Fatalf("unexpected rule ordering: %q, %q", list[0].Rule.Rule, list[1].Rule.Rule)
	}
}

func TestRuleRecordInvalidEntry(t *testing.T) {
	tl, _, ctx := openTestTimeline(t, "rules-invalid")
	entry := &Entry{
		TimelineEntry: api.TimelineEntry{
			Kind: api.TimelineKindRule,
		},
	}
	if err := tl.Append(ctx, entry); !reflect.DeepEqual(err, ErrInvalidEntry) {
		t.Fatalf("Append(rule, nil Rule) error = %v, want %v", err, ErrInvalidEntry)
	}
}
