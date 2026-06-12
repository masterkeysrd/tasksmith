package highlight

import (
	"reflect"
	"testing"

	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/tui/colorscheme"
)

func TestHighlight(t *testing.T) {
	defer Reset()

	t.Run("SetAndGroups", func(t *testing.T) {
		Reset()
		g1 := Set("Normal")
		g2 := Set("Error", Link("Normal"))

		if g1.Name() != "Normal" {
			t.Errorf("g1.Name() = %q, want Normal", g1.Name())
		}
		if g2.Name() != "Error" {
			t.Errorf("g2.Name() = %q, want Error", g2.Name())
		}

		groups := Groups()
		if len(groups) != 2 {
			t.Errorf("len(Groups()) = %d, want 2", len(groups))
		}
		if groups[0].Name() != "Error" || groups[1].Name() != "Normal" {
			t.Errorf("Groups() returned %v in wrong order or with wrong names", groups)
		}
	})

	t.Run("StyleAndReload", func(t *testing.T) {
		Reset()
		g := Set("Normal")

		// Initial style is zero
		if !reflect.DeepEqual(Style(g), style.S()) {
			t.Errorf("Initial style not empty: %+v", Style(g))
		}

		// Reload with some colors
		Reload(map[string]colorscheme.ResolvedColor{
			"Normal": {Fg: "#ffffff", Bg: "#000000"},
		})

		s := Style(g)
		if !s.ForegroundOpt().IsSet() || !s.BackgroundOpt().IsSet() {
			t.Errorf("Style not updated after reload: %+v", s)
		}
	})
}
