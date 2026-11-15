package components

import (
	"testing"

	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/tasksmith/internal/tui/testutils"
)

// renderComponent renders nodes in a fixed-height terminal of the given width,
// re-rendering the same container so assertions can compare states.
func renderComponent(t *testing.T, width int) func(trail.Node) string {
	t.Helper()
	env := testutils.NewEnv(t, width, 24)
	return env.Render
}
