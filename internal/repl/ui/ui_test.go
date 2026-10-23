package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/masterkeysrd/kite/backend"
	"github.com/masterkeysrd/kite/backend/mock"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/ui/autocomplete"
	"github.com/masterkeysrd/tasksmith/internal/ui/components"
)

func surfaceText(b *mock.Backend) string {
	frame := b.LastFrame().Surface
	if frame == nil {
		return ""
	}
	var sb strings.Builder
	for y := 0; y < frame.Height; y++ {
		var line strings.Builder
		for x := 0; x < frame.Width; x++ {
			c := frame.CellAt(x, y)
			if c.Content != "" {
				line.WriteString(c.Content)
			} else {
				line.WriteByte(' ')
			}
		}
		trimmed := strings.TrimRight(line.String(), " ")
		if trimmed != "" {
			sb.WriteString(trimmed)
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func TestSpinnerAndDiffComponents(t *testing.T) {
	b := mock.New(80, 24)
	app := trail.SimpleFC("TestApp", func() kitex.Node {
		return trail.Fragment(
			Spinner(SpinnerProps{Text: "Resolving packages..."}),
			Diff(DiffProps{Text: "--- a/f\n+++ b/f\n@@ -1 +1 @@\n-old\n+new"}),
		)
	})
	inst, _ := trail.New(app(), trail.WithBackend(b))
	inst.Frame()
	out := surfaceText(b)
	if !strings.Contains(out, "Resolving packages...") {
		t.Errorf("spinner text missing:\n%s", out)
	}
	if !strings.Contains(out, "--- a/f") || !strings.Contains(out, "+new") {
		t.Errorf("diff lines missing:\n%s", out)
	}
}

func TestChangesListComponent(t *testing.T) {
	b := mock.New(80, 24)
	app := trail.SimpleFC("TestApp", func() kitex.Node {
		return trail.Box(trail.BoxProps{},
			ChangesList(ChangesListProps{
				Changes: []api.FileChange{
					{ID: "c1", Kind: "modify", Path: "main.go", Additions: 10, Deletions: 2},
				},
			}),
		)
	})
	inst, _ := trail.New(app(), trail.WithBackend(b))
	inst.Frame()

	out := surfaceText(b)
	if !strings.Contains(out, "Tracked file changes:") || !strings.Contains(out, "main.go") || !strings.Contains(out, "+10/-2") {
		t.Errorf("ChangesList component missing from output:\n%s", out)
	}
}

func TestCommandOutputComponent(t *testing.T) {
	b := mock.New(80, 24)
	app := trail.SimpleFC("TestApp", func() kitex.Node {
		return trail.Box(trail.BoxProps{
			Style: style.S().FlexDirection(style.FlexColumn),
		},
			CommandOutput(CommandOutputProps{
				Command: "/mode plan",
				Output:  "Switched to plan mode.",
			}),
			CommandOutput(CommandOutputProps{
				Command: "/sessions",
				Output:  "Exited /sessions command",
			}),
		)
	})
	inst, _ := trail.New(app(), trail.WithBackend(b))
	inst.Frame()

	out := surfaceText(b)
	if !strings.Contains(out, "> /mode plan") || !strings.Contains(out, "⎿  Switched to plan mode.") {
		t.Errorf("CommandOutput mode output missing:\n%s", out)
	}
	if !strings.Contains(out, "> /sessions") || !strings.Contains(out, "⎿  Exited /sessions command") {
		t.Errorf("CommandOutput sessions output missing:\n%s", out)
	}
}

func TestOptionListComponent(t *testing.T) {
	b := mock.New(80, 24)

	// 1. Empty with EmptyText
	appEmpty := trail.SimpleFC("AppEmpty", func() kitex.Node {
		return components.OptionList(components.OptionListProps{
			Options:   nil,
			Selected:  -1,
			EmptyText: "  No matching options.",
		})
	})
	inst1, _ := trail.New(appEmpty(), trail.WithBackend(b))
	inst1.Frame()
	out1 := surfaceText(b)
	if !strings.Contains(out1, "No matching options.") {
		t.Errorf("expected empty text, got:\n%s", out1)
	}

	// 2. Render options with selection and current badge
	b2 := mock.New(80, 24)
	appOpts := trail.SimpleFC("AppOpts", func() kitex.Node {
		return components.OptionList(components.OptionListProps{
			Options: []components.Option{
				{Value: "opt1", Label: "First", Description: "First desc", Current: true},
				{Value: "opt2", Label: "Second", Description: "Second desc"},
			},
			Selected: 0,
		})
	})
	inst2, _ := trail.New(appOpts(), trail.WithBackend(b2))
	inst2.Frame()
	out2 := surfaceText(b2)
	if !strings.Contains(out2, "❯ First") || !strings.Contains(out2, "First desc • current") {
		t.Errorf("expected selected row and current badge, got:\n%s", out2)
	}
	if !strings.Contains(out2, "  Second") {
		t.Errorf("expected second row, got:\n%s", out2)
	}
}

func TestAutocompleteComponent(t *testing.T) {
	b := mock.New(80, 24)

	app := trail.SimpleFC("AppAuto", func() kitex.Node {
		return trail.Box(trail.BoxProps{},
			components.Autocomplete(components.AutocompleteProps{
				State: components.AutocompleteState{
					IsOpen: true,
					Result: autocomplete.Result{Candidates: []autocomplete.Candidate{
						{Value: "mode", Label: "/mode [name]", Description: "Change mode"},
					}},
				},
			}),
			components.Autocomplete(components.AutocompleteProps{
				State: components.AutocompleteState{
					IsOpen: true,
					Result: autocomplete.Result{Request: autocomplete.Request{Source: "modes"}},
				},
				Views: map[string]components.AutocompleteView{
					"modes": func(autocomplete.Result, int) trail.Node {
						return components.OptionList(components.OptionListProps{
							Options: []components.Option{{Value: "plan", Label: "plan", Description: "Plan mode", Current: true}},
						})
					},
				},
			}),
		)
	})
	inst, _ := trail.New(app(), trail.WithBackend(b))
	inst.Frame()

	out := surfaceText(b)
	if !strings.Contains(out, "/mode [name]") || !strings.Contains(out, "Change mode") {
		t.Errorf("expected command autocomplete hint in output:\n%s", out)
	}
	if !strings.Contains(out, "❯ plan") || !strings.Contains(out, "Plan mode • current") {
		t.Errorf("expected argument autocomplete options in output:\n%s", out)
	}
}

func TestApprovalFunctionalComponent(t *testing.T) {
	req := api.PermissionRequest{
		Action:   api.ActionWrite,
		Safety:   api.SafetySafe,
		ToolName: "write_file",
		Summary:  "Write main.go",
		Target:   "main.go",
	}

	var decided api.PermissionDecision
	b := mock.New(80, 24)
	app := trail.SimpleFC("TestApp", func() kitex.Node {
		return Approval(ApprovalProps{
			Request: req,
			OnDecision: func(_ api.PermissionRequest, dec api.PermissionDecision) {
				decided = dec
			},
		})
	})

	inst, _ := trail.New(app(), trail.WithBackend(b))
	inst.Frame()

	out := surfaceText(b)
	if !strings.Contains(out, "[Permission Required]") || !strings.Contains(out, "Write main.go") {
		t.Errorf("approval card output missing header or summary:\n%s", out)
	}

	// Press 'y' -> Allow once
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{
		Key: key.Key{Code: 'y', Text: "y"},
	})
	if decided.Kind != api.DecisionAllowOnce {
		t.Errorf("decision kind = %v, want DecisionAllowOnce", decided.Kind)
	}
}

func TestOptionListPagination(t *testing.T) {
	opts := make([]components.Option, 8)
	for i := range 8 {
		opts[i] = components.Option{
			Value:       fmt.Sprintf("cmd%d", i),
			Label:       fmt.Sprintf("/cmd%d", i),
			Description: fmt.Sprintf("Command %d", i),
		}
	}

	// 1. Initially at Selected: 0 -> shows "  ↓ 3 more" at bottom and NO "↑ ... more" at top
	b1 := mock.New(80, 24)
	app1 := trail.SimpleFC("App1", func() kitex.Node {
		return components.OptionList(components.OptionListProps{
			Options:    opts,
			Selected:   0,
			MaxVisible: 5,
		})
	})
	inst1, _ := trail.New(app1(), trail.WithBackend(b1))
	inst1.Frame()
	out1 := surfaceText(b1)
	if !strings.Contains(out1, "↓ 3 more") {
		t.Errorf("expected '↓ 3 more' at bottom, got:\n%s", out1)
	}
	if strings.Contains(out1, "↑") {
		t.Errorf("expected no top overflow at selected 0, got:\n%s", out1)
	}
	if !strings.Contains(out1, "❯ /cmd0") || !strings.Contains(out1, "  /cmd4") {
		t.Errorf("expected visible range [0:5], got:\n%s", out1)
	}
	if strings.Contains(out1, "/cmd5") {
		t.Errorf("expected /cmd5 to be hidden, got:\n%s", out1)
	}

	// 2. Selected: 5 -> shows "  ↑ 1 more" at top AND "  ↓ 2 more" at bottom
	b2 := mock.New(80, 24)
	app2 := trail.SimpleFC("App2", func() kitex.Node {
		return components.OptionList(components.OptionListProps{
			Options:    opts,
			Selected:   5,
			MaxVisible: 5,
		})
	})
	inst2, _ := trail.New(app2(), trail.WithBackend(b2))
	inst2.Frame()
	out2 := surfaceText(b2)
	if !strings.Contains(out2, "↑ 1 more") {
		t.Errorf("expected '↑ 1 more' at top, got:\n%s", out2)
	}
	if !strings.Contains(out2, "↓ 2 more") {
		t.Errorf("expected '↓ 2 more' at bottom, got:\n%s", out2)
	}
	if !strings.Contains(out2, "❯ /cmd5") {
		t.Errorf("expected /cmd5 to be selected, got:\n%s", out2)
	}

	// 3. Selected: 7 (last item) -> shows "  ↑ 3 more" at top, and NO "↓ ... more" at bottom
	b3 := mock.New(80, 24)
	app3 := trail.SimpleFC("App3", func() kitex.Node {
		return components.OptionList(components.OptionListProps{
			Options:    opts,
			Selected:   7,
			MaxVisible: 5,
		})
	})
	inst3, _ := trail.New(app3(), trail.WithBackend(b3))
	inst3.Frame()
	out3 := surfaceText(b3)
	if !strings.Contains(out3, "↑ 3 more") {
		t.Errorf("expected '↑ 3 more' at top, got:\n%s", out3)
	}
	if strings.Contains(out3, "↓") {
		t.Errorf("expected no bottom overflow at selected 7, got:\n%s", out3)
	}
	if !strings.Contains(out3, "❯ /cmd7") {
		t.Errorf("expected /cmd7 to be selected, got:\n%s", out3)
	}
}
