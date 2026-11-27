package components

import (
	"strings"
	"testing"

	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/tasksmith/internal/tui/testutils"
)

func TestKVList(t *testing.T) {
	props := KVListProps{
		Indent: 2,
		Items: []KVItem{
			{Label: "ID", Value: "123"},
			{Label: "名前", Value: "ignored", ValueNode: trail.Text("custom"), OmitEmpty: true},
			{Label: "not visible", OmitEmpty: true},
			{Label: "Flag"},
		},
	}
	out := testutils.Render(t, KVList(props), 30, 8)
	for _, want := range []string{"ID:   123", "名 前 : custom", "  Flag:"} {
		if !strings.Contains(out, want) || strings.Contains(out, "not visible") {
			t.Fatalf("expected %q in:\n%s", want, out)
		}
	}
	props.MinLabelWidth = 6
	props.Items = []KVItem{{Label: "ID", Value: "one two three"}}
	out = testutils.Render(t, KVList(props), 18, 8)
	if !strings.Contains(out, "ID:     one two") || !strings.Contains(out, "\n          three") {
		t.Fatalf("unexpected wrapped value:\n%s", out)
	}

	// Narrow terminal: wide label must cap at 50% so value remains visible.
	out = testutils.Render(t, KVList(KVListProps{
		Indent: 2,
		Items: []KVItem{
			{Label: "VeryLongConfigurationLabel", Value: "ok-val"},
			{Label: "", Value: "continuation"},
		},
	}), 22, 8)
	if !strings.Contains(out, "ok-val") {
		t.Fatalf("expected value to remain visible on narrow viewport:\n%s", out)
	}
	if strings.Contains(out, ": continuation") {
		t.Fatalf("expected no stray colon on empty label:\n%s", out)
	}

	// All-empty labels omit the label column and colon completely.
	out = testutils.Render(t, KVList(KVListProps{
		Items: []KVItem{{Value: "standalone"}},
	}), 20, 4)
	if strings.TrimSpace(out) != "standalone" {
		t.Fatalf("unexpected empty-label output: %q", out)
	}
}
