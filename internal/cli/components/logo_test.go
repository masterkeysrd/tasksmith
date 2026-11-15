package components

import (
	"strings"
	"testing"

	"github.com/masterkeysrd/tasksmith/internal/tui/testutils"
)

func TestLogoRender(t *testing.T) {
	env := testutils.NewEnv(t, 80, 24)
	text := env.Render(Logo())

	for _, want := range []string{
		"◥██████████◣",
		"▐██▌",
		"◢████◣",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("logo output missing %q, got:\n%s", want, text)
		}
	}

	frame := env.Backend.LastFrame().Surface
	if frame == nil {
		t.Fatal("expected rendered surface")
	}

	// Verify line 0 has top bar color
	cell0 := frame.CellAt(2, 0)
	if cell0.Fg != logoColorTop {
		t.Errorf("expected cell (2, 0) foreground %v, got %v", logoColorTop, cell0.Fg)
	}

	// Verify line 1 has center pillar color
	cell1 := frame.CellAt(6, 1)
	if cell1.Fg != logoColorCenter {
		t.Errorf("expected cell (6, 1) foreground %v, got %v", logoColorCenter, cell1.Fg)
	}

	// Verify line 2 has base color
	cell2 := frame.CellAt(6, 2)
	if cell2.Fg != logoColorBase {
		t.Errorf("expected cell (6, 2) foreground %v, got %v", logoColorBase, cell2.Fg)
	}
}
