package components

import (
	"strings"
	"testing"

	"github.com/masterkeysrd/kite/element"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/testenv"
)

func renderComponent(t *testing.T, width int) func(trail.Node) string {
	t.Helper()
	env := testenv.Default(width, 24)
	t.Cleanup(env.Close)
	container := element.NewBox(env.Document())
	env.Mount(container)
	return func(node trail.Node) string {
		kitex.Render(node, container)
		env.Flush()
		return strings.TrimSpace(env.DumpText())
	}
}
