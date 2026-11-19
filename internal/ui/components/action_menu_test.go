package components

import (
	"fmt"
	"strings"
	"testing"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/tui/testutils"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

func TestMenuDigit(t *testing.T) {
	tests := []struct {
		name string
		key  key.Key
		want int
	}{
		{name: "first entry", key: key.Key{Text: "1"}, want: 1},
		{name: "last entry", key: key.Key{Text: "9"}, want: 9},
		{name: "code without text", key: key.Key{Code: '4'}, want: 4},
		{name: "zero means nothing", key: key.Key{Text: "0"}, want: 0},
		{name: "letter", key: key.Key{Text: "p"}, want: 0},
		{name: "modified digit", key: key.Key{Text: "2", Mod: key.ModCtrl}, want: 0},
		{name: "empty key", key: key.Key{}, want: 0},
	}
	for _, tt := range tests {
		if got := MenuDigit(tt.key); got != tt.want {
			t.Errorf("%s: MenuDigit(%+v) = %d, want %d", tt.name, tt.key, got, tt.want)
		}
	}
}

func TestActionMenuRenderingAndAlignedBadges(t *testing.T) {
	scheme := theme.Get()
	badgeStyle := style.S().Foreground(scheme.Color.Agent.Permission)

	env := testutils.NewEnv(t, 80, 12)
	env.Render(ActionMenu(ActionMenuProps{
		Items: []ActionMenuItem{
			{ID: "once", Label: "Yes, allow once", Shortcut: "y"},
			{ID: "short", Label: "Always allow bash(ls)", Badge: "[session]", BadgeStyle: &badgeStyle},
			{ID: "long", Label: "Always allow bash(git status *)", Badge: "[session]", BadgeStyle: &badgeStyle},
			{ID: "deny", Label: "No, deny", Shortcut: "n"},
		},
		InitialIndex: 0,
		Indent:       2,
	}))
	out := env.DumpText()

	lines := testutils.TrimmedLines(out)
	if len(lines) < 4 {
		t.Fatalf("expected 4 rows, got:\n%s", out)
	}
	if lines[0] != "  ❯ Yes, allow once" {
		t.Errorf("expected indented selected row 0, got %q", lines[0])
	}
	if lines[3] != "    No, deny" {
		t.Errorf("expected indented unselected row 3, got %q", lines[3])
	}

	col1 := strings.Index(lines[1], "[session]")
	col2 := strings.Index(lines[2], "[session]")
	if col1 < 0 || col2 < 0 || col1 != col2 {
		t.Errorf("expected aligned [session] badges (col1=%d, col2=%d):\n%s", col1, col2, out)
	}
}

func TestActionMenuEmptyRendersNothing(t *testing.T) {
	out := testutils.Render(t, ActionMenu(ActionMenuProps{}), 60, 10)
	if strings.TrimSpace(out) != "" {
		t.Fatalf("expected empty ActionMenu to render nothing, got %q", out)
	}
}

func TestActionMenuNumberedShortcutsAndNavigation(t *testing.T) {
	var changes []int
	var selections []string

	env := testutils.NewEnv(t, 80, 12)
	env.Render(ActionMenu(ActionMenuProps{
		Items: []ActionMenuItem{
			{ID: "tools", Label: "View tools (2)"},
			{ID: "prompts", Label: "Run prompt (2)", Shortcut: "p"},
			{ID: "reconnect", Label: "Reconnect", Shortcut: "r"},
		},
		Numbered: true,
		OnChange: func(index int) {
			changes = append(changes, index)
		},
		OnSelect: func(index int, item ActionMenuItem) {
			selections = append(selections, fmt.Sprintf("%d:%s", index, item.ID))
		},
	}))

	out := env.Text()
	for _, want := range []string{"❯ 1. View tools (2)", "  2. Run prompt (2)", "  3. Reconnect"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}

	// Digit '3' jumps cursor and fires OnSelect.
	out = env.Press(key.Key{Code: '3', Text: "3"})
	if !strings.Contains(out, "❯ 3. Reconnect") {
		t.Fatalf("expected cursor on item 3 after '3':\n%s", out)
	}

	// Out-of-range digit '9' must be inert.
	env.Press(key.Key{Code: '9', Text: "9"})

	// Case-insensitive shortcut 'P' jumps to item 1 and selects it.
	out = env.Press(key.Key{Code: 'p', Text: "P", Mod: key.ModShift})
	if !strings.Contains(out, "❯ 2. Run prompt (2)") {
		t.Fatalf("expected cursor on item 2 after 'P':\n%s", out)
	}

	// Ctrl+P moves up (must NOT trigger shortcut 'p'), then Enter selects item 0.
	env.Press(key.Key{Code: 'p', Text: "p", Mod: key.ModCtrl})
	env.Press(key.Key{Code: key.KeyEnter})

	wantSelections := "2:reconnect,1:prompts,0:tools"
	if got := strings.Join(selections, ","); got != wantSelections {
		t.Fatalf("selections = %q, want %q", got, wantSelections)
	}
	if len(changes) != 3 || changes[0] != 2 || changes[1] != 1 || changes[2] != 0 {
		t.Fatalf("changes = %v, want [2 1 0]", changes)
	}
}

func TestActionMenuControlledAndDisabled(t *testing.T) {
	var selected []string
	harness := trail.FC("ControlledMenuHarness", func(disabled bool) trail.Node {
		sel, setSel := kitex.UseState(0)
		idx := sel()
		return trail.Box(trail.BoxProps{Style: style.S().FlexDirection(style.FlexColumn)},
			trail.Text(fmt.Sprintf("cursor=%d", idx)),
			ActionMenu(ActionMenuProps{
				Items: []ActionMenuItem{
					{ID: "yes", Label: "Yes", Shortcut: "y"},
					{ID: "no", Label: "No", Shortcut: "n"},
				},
				Selected: &idx,
				Disabled: disabled,
				OnChange: setSel,
				OnSelect: func(_ int, item ActionMenuItem) {
					selected = append(selected, item.ID)
				},
			}),
		)
	})

	env := testutils.NewEnv(t, 60, 10)
	out := env.Render(harness(false))
	if !strings.Contains(out, "cursor=0") {
		t.Fatalf("expected cursor=0, got:\n%s", out)
	}

	out = env.Press(key.Key{Code: key.KeyDown})
	if !strings.Contains(out, "cursor=1") || !strings.Contains(out, "❯ No") {
		t.Fatalf("expected controlled cursor=1 after down, got:\n%s", out)
	}

	// While disabled, keys must be completely ignored.
	out = env.Render(harness(true))
	env.Press(key.Key{Code: 'y', Text: "y"})
	env.Press(key.Key{Code: key.KeyUp})
	env.Press(key.Key{Code: key.KeyEnter})
	if len(selected) != 0 {
		t.Fatalf("expected no selections while disabled, got %v", selected)
	}
	if !strings.Contains(env.Text(), "cursor=1") {
		t.Fatalf("expected cursor to remain at 1 while disabled, got:\n%s", out)
	}
}
