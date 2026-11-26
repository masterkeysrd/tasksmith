package components

import (
	"image/color"
	"strings"
	"testing"

	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/tui/testutils"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

func TestShellSurfaces(t *testing.T) {
	const cmd = "rm -rf build"
	c := theme.Get().Color
	widget := func(cmd string) trail.Node {
		return BashToolWidget(BashToolWidgetProps{Tool: store.ToolPart{Call: &api.ToolCall{Name: "bash", Args: map[string]any{"command": cmd, "description": "Clean"}}}})
	}
	for _, tc := range []struct {
		name string
		node trail.Node
		base color.Color
	}{
		{"permission", PermissionPrompt(PermissionPromptProps{Request: api.PermissionRequest{ToolName: "bash", Target: cmd}}), c.Text.Primary},
		{"transcript", BashCommand(api.CommandRecord{Command: cmd}), c.Text.Primary},
		{"widget", widget(cmd), c.Text.Secondary},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := testutils.NewEnv(t, 80, 20)
			env.Render(tc.node)
			for text, want := range map[string]color.Color{"rm -rf": c.Syntax.Function, "-rf": c.Syntax.Type, "build": tc.base} {
				cell, _ := questionCell(t, env.Backend, text)
				if cell.Fg != want {
					t.Errorf("%q foreground = %v, want %v", text, cell.Fg, want)
				}
			}
		})
	}
	t.Run("narrow", func(t *testing.T) {
		out := testutils.Render(t, widget("rm -rf "+strings.Repeat("界", 30)+"TAIL"), 24, 8)
		lines := testutils.TrimmedLines(out)
		if len(lines) != 2 || !strings.Contains(out, "rm -rf 界") || !strings.Contains(out, "…") || strings.Contains(out, "TAIL") || strings.ContainsRune(out, '\uFFFD') {
			t.Fatalf("unexpected narrow preview:\n%s", out)
		}
	})
	t.Run("narrow container and tabs stay single line", func(t *testing.T) {
		boxed := trail.Box(trail.BoxProps{Style: style.S().Width(style.Cells(28))}, widget("git commit -m \"hello\t"+strings.Repeat("world ", 10)+"\""))
		out := testutils.Render(t, boxed, 80, 8)
		lines := testutils.TrimmedLines(out)
		if len(lines) != 2 || !strings.Contains(lines[1], "…") {
			t.Fatalf("expected single truncated command line in narrower box, got %d lines:\n%s", len(lines), out)
		}
	})
}
