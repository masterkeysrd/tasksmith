package components

import (
	"strings"
	"testing"

	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/tasksmith/internal/tui/testutils"
	uicomponents "github.com/masterkeysrd/tasksmith/internal/ui/components"
)

// TestPickerFilterSafeKeysKeepsPrintableKeysInFilter guards the provider model
// catalog: with FilterSafeKeys set, printable keystrokes (including ones bound
// as actions elsewhere, like "s") must reach the filter box instead of firing
// actions, and only the ctrl chord commits.
func TestPickerFilterSafeKeysKeepsPrintableKeysInFilter(t *testing.T) {
	var selected []string
	committed := 0

	options := []uicomponents.Option{
		{Value: "sonnet", Label: "Claude Sonnet"},
		{Value: "opus", Label: "Claude Opus"},
	}

	// Without FilterSafeKeys, "s" fires the bound action and never types.
	unsafe := testutils.NewEnv(t, 90, 24)
	unsafe.Render(Picker(PickerProps{
		Title:      "Unsafe",
		Filterable: true,
		Options:    options,
		OnSelect:   func(opt uicomponents.Option) { selected = append(selected, opt.Value) },
		Keys: map[string]func(uicomponents.Option){
			"s": func(opt uicomponents.Option) { selected = append(selected, "action:"+opt.Value) },
		},
	}))
	out := unsafe.Press(key.Key{Code: 's', Text: "s"})
	if strings.Contains(out, "Filter:s") {
		t.Fatalf("plain picker should intercept 's', not type it:\\n%s", out)
	}
	if len(selected) != 1 || selected[0] != "action:sonnet" {
		t.Fatalf("unsafe picker 's' handling = %v, want the bound action", selected)
	}
	selected = nil

	// With FilterSafeKeys the same keystrokes type into the filter, and only
	// the ctrl chord runs the commit action.
	env := testutils.NewEnv(t, 90, 24)
	press := func(code rune, text string, mod key.Mod) string {
		return env.Press(key.Key{Code: code, Text: text, Mod: mod})
	}
	env.Render(Picker(PickerProps{
		Title:          "Safe",
		Filterable:     true,
		FilterSafeKeys: true,
		Options:        options,
		OnSelect:       func(opt uicomponents.Option) { selected = append(selected, opt.Value) },
		Keys:           map[string]func(uicomponents.Option){"ctrl+s": func(uicomponents.Option) { committed++ }},
	}))
	out = env.Type("son")
	if !strings.Contains(out, "son") {
		t.Fatalf("filter-safe picker must type 'son' into the filter:\\n%s", out)
	}
	if strings.Contains(out, "Claude Opus") {
		t.Fatalf("filter did not narrow the listing:\\n%s", out)
	}
	if len(selected) != 0 || committed != 0 {
		t.Fatalf("printable keys must not fire actions (selected=%v committed=%d)", selected, committed)
	}

	press(key.KeyEnter, "", 0)
	if len(selected) != 1 || selected[0] != "sonnet" {
		t.Fatalf("enter select = %v, want [sonnet]", selected)
	}
	press('s', "", key.ModCtrl)
	if committed != 1 {
		t.Fatalf("ctrl+s commits = %d, want 1", committed)
	}
}

// TestPickerInputActionDisabledReasonNotices proves a DisabledReason steers the
// user with a notice instead of opening a dead input prompt.
func TestPickerInputActionDisabledReasonNotices(t *testing.T) {
	env := testutils.NewEnv(t, 90, 24)
	env.Render(Picker(PickerProps{
		Title: "Form",
		Options: []uicomponents.Option{
			{Value: "name", Label: "Name", Description: "prov"},
			{Value: "submit", Label: "Create provider"},
		},
		InputActions: map[string]InputAction{
			"e": {
				Prompt:         "New value:",
				DisabledReason: func(opt uicomponents.Option) string { return "The " + opt.Label + " row has no free-text value." },
			},
		},
	}))

	// Row one (Name) is disabled: 'e' shows the reason rather than a prompt.
	out := env.Press(key.Key{Code: 'e', Text: "e"})
	if !strings.Contains(out, "Name row has no free-text value") {
		t.Fatalf("disabled input action did not surface its reason:\\n%s", out)
	}
	if strings.Contains(out, "New value:") {
		t.Fatalf("disabled input action opened a prompt:\\n%s", out)
	}
}
