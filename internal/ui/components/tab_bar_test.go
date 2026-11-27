package components

import (
	"strings"
	"testing"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/tasksmith/internal/tui/testutils"
)

func TestTabBar(t *testing.T) {
	out := testutils.Render(t, TabBar(TabBarProps{
		Tabs: []TabItem{
			{Label: "Policy"},
			{Label: "Exclusions (2)"},
			{Label: "Effective"},
		},
		Active:   1,
		Numbered: true,
		Meta:     "scope: [Project] · Global",
	}), 80, 4)

	for _, want := range []string{"1 Policy", "[2 Exclusions (2)]", "3 Effective", "scope: [Project] · Global"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "[1 Policy]") || strings.Contains(out, "[3 Effective]") {
		t.Fatalf("only active tab should be bracketed:\n%s", out)
	}

	// Empty tab list renders nothing.
	if empty := strings.TrimSpace(testutils.Render(t, TabBar(TabBarProps{}), 40, 4)); empty != "" {
		t.Fatalf("expected empty output for zero tabs, got %q", empty)
	}

	// Custom MetaNode overrides plain Meta text.
	out = testutils.Render(t, TabBar(TabBarProps{
		Tabs:     []TabItem{{Label: "A"}, {Label: "B"}},
		Active:   0,
		Meta:     "ignored",
		MetaNode: Badge(BadgeProps{Label: "custom-meta", Tone: ToneWarning}),
	}), 60, 4)
	if !strings.Contains(out, "custom-meta") || strings.Contains(out, "ignored") {
		t.Fatalf("expected MetaNode override:\n%s", out)
	}
}

func TestTabKeyActions(t *testing.T) {
	view := trail.SimpleFC("TabKeyHarness", func() trail.Node {
		active, setActive := kitex.UseState(0)
		bindings := UseKeyMap(TabKeyActions(3, active(), setActive, false), true)
		return Panel(PanelProps{
			Title:    "Tabs",
			Bindings: bindings,
		}, TabBar(TabBarProps{
			Tabs: []TabItem{
				{Label: "One"},
				{Label: "Two"},
				{Label: "Three"},
			},
			Active:   active(),
			Numbered: true,
		}))
	})

	app := testutils.NewApp(t, view(), 60, 8)
	out := app.Text()
	if !strings.Contains(out, "[1 One]") || !strings.Contains(out, "tab / 1-3 Switch tab") {
		t.Fatalf("unexpected initial frame:\n%s", out)
	}

	app.Press(key.Key{Code: key.KeyTab})
	if out = app.Text(); !strings.Contains(out, "[2 Two]") {
		t.Fatalf("expected tab to advance to 2:\n%s", out)
	}

	app.Press(key.Key{Code: key.KeyTab, Mod: key.ModShift})
	if out = app.Text(); !strings.Contains(out, "[1 One]") {
		t.Fatalf("expected shift+tab to return to 1:\n%s", out)
	}

	app.Type("3")
	if out = app.Text(); !strings.Contains(out, "[3 Three]") {
		t.Fatalf("expected '3' to jump to tab 3:\n%s", out)
	}

	// Edge cases: count <= 1, disabled, > 9 tabs hint, and clamped out-of-bounds active.
	if got := TabKeyActions(1, 0, func(int) {}, false); got != nil {
		t.Fatalf("count=1 actions = %+v, want nil", got)
	}
	var jumped []int
	clamped := TabKeyActions(3, -2, func(i int) { jumped = append(jumped, i) }, false)
	clamped[0].OnPress() // tab from clamped 0 -> 1
	clamped[1].OnPress() // shift+tab from clamped 0 -> 2
	if len(jumped) != 2 || jumped[0] != 1 || jumped[1] != 2 {
		t.Fatalf("clamped negative active jumped = %v, want [1 2]", jumped)
	}
	many := TabKeyActions(12, 0, func(int) {}, false)
	if many[0].Hint != "tab / 1-9" {
		t.Fatalf("count=12 hint = %q, want tab / 1-9", many[0].Hint)
	}
	disabled := TabKeyActions(3, 0, func(int) {}, true)
	for _, a := range disabled {
		if !a.Disabled {
			t.Fatalf("expected all actions disabled: %+v", a)
		}
	}
}
