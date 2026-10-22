package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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

func insertedAboveText(b *mock.Backend) string {
	var sb strings.Builder
	for _, buf := range b.InsertedAbove {
		for y := 0; y < buf.Height; y++ {
			var line strings.Builder
			for x := 0; x < buf.Width; x++ {
				c := buf.CellAt(x, y)
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
	}
	return sb.String()
}

func TestHeaderComponent(t *testing.T) {
	b := mock.New(80, 24)
	app := trail.SimpleFC("TestApp", func() kitex.Node {
		return Header(HeaderProps{
			Version:  "0.1.0",
			Model:    "Claude 3.7 Sonnet",
			Thinking: "high",
			Path:     "/workspace/project",
		})
	})

	inst, err := trail.New(app(), trail.WithBackend(b))
	if err != nil {
		t.Fatalf("trail.New: %v", err)
	}
	inst.Frame()

	out := surfaceText(b)
	if !strings.Contains(out, "TaskSmith v0.1.0") {
		t.Errorf("line 1 missing 'TaskSmith v0.1.0':\n%s", out)
	}
	if !strings.Contains(out, "Claude 3.7 Sonnet with high effort") {
		t.Errorf("line 2 missing model/thinking:\n%s", out)
	}
	if !strings.Contains(out, "/workspace/project") {
		t.Errorf("line 3 missing path:\n%s", out)
	}

	// Helpers
	if got := NormalizeVersion("0.1.0"); got != "v0.1.0" {
		t.Errorf("NormalizeVersion('0.1.0') = %q, want 'v0.1.0'", got)
	}
	if got := NormalizeVersion(""); got != "v0.1.0" {
		t.Errorf("NormalizeVersion('') = %q, want 'v0.1.0'", got)
	}

	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		shortened := ShortenHomePath(filepath.Join(home, "src", "tasksmith"))
		if !strings.HasPrefix(shortened, "~") {
			t.Errorf("expected ~ prefix for home path, got %q", shortened)
		}
		if ShortenHomePath(home) != "~" {
			t.Errorf("expected ~ for exact home, got %q", ShortenHomePath(home))
		}
	}
}

func TestStatusBarComponent(t *testing.T) {
	// 1. Default mode: shows "<Model> · <Thinking>" without "default · "
	b1 := mock.New(80, 24)
	app1 := trail.SimpleFC("TestApp", func() kitex.Node {
		return StatusBar(StatusBarProps{
			State:    StateIdle,
			Mode:     api.ModeDefault,
			Model:    "Claude 3.7 Sonnet",
			Thinking: "high",
		})
	})
	inst1, _ := trail.New(app1(), trail.WithBackend(b1))
	inst1.Frame()
	if h := b1.LastFrame().Surface.Height; h != 1 {
		t.Errorf("expected StatusBar height 1, got %d:\n%s", h, surfaceText(b1))
	}
	out1 := surfaceText(b1)
	if !strings.Contains(out1, "Claude 3.7 Sonnet · high") {
		t.Errorf("expected model · thinking in status bar:\n%s", out1)
	}
	if strings.Contains(out1, "default · ") {
		t.Errorf("default mode should not be prefixed with 'default · ':\n%s", out1)
	}
	if !strings.Contains(out1, "shift+tab cycle mode") {
		t.Errorf("idle state hint missing on left:\n%s", out1)
	}

	// 2. Non-default mode: shows "<mode> · <Model> · <Thinking>"
	b2 := mock.New(80, 24)
	app2 := trail.SimpleFC("TestApp", func() kitex.Node {
		return StatusBar(StatusBarProps{
			State:    StateStreaming,
			Mode:     api.ModeAcceptEdits,
			Model:    "Claude 3.7 Sonnet",
			Thinking: "high",
		})
	})
	inst2, _ := trail.New(app2(), trail.WithBackend(b2))
	inst2.Frame()
	if h := b2.LastFrame().Surface.Height; h != 1 {
		t.Errorf("expected StatusBar height 1 in non-default mode, got %d:\n%s", h, surfaceText(b2))
	}
	out2 := surfaceText(b2)
	if !strings.Contains(out2, "accept_edits · Claude 3.7 Sonnet · high") {
		t.Errorf("expected mode · model · thinking:\n%s", out2)
	}
	if !strings.Contains(out2, "streaming • esc to cancel") {
		t.Errorf("streaming hint missing:\n%s", out2)
	}

	// 3. Custom hint override
	b3 := mock.New(80, 24)
	app3 := trail.SimpleFC("TestApp", func() kitex.Node {
		return StatusBar(StatusBarProps{
			State: StatePicker,
			Hint:  "custom hint override",
		})
	})
	inst3, _ := trail.New(app3(), trail.WithBackend(b3))
	inst3.Frame()
	out3 := surfaceText(b3)
	if !strings.Contains(out3, "custom hint override") {
		t.Errorf("expected custom hint:\n%s", out3)
	}

	// 4. StatePicker with empty hint: leftText is empty, right side displays model/thinking
	b4 := mock.New(80, 24)
	app4 := trail.SimpleFC("TestApp", func() kitex.Node {
		return StatusBar(StatusBarProps{
			State:    StatePicker,
			Model:    "Claude 3.7 Sonnet",
			Thinking: "high",
		})
	})
	inst4, _ := trail.New(app4(), trail.WithBackend(b4))
	inst4.Frame()
	out4 := surfaceText(b4)
	if !strings.Contains(out4, "Claude 3.7 Sonnet · high") {
		t.Errorf("expected model · thinking on right:\n%s", out4)
	}
	if strings.Contains(out4, "navigate") || strings.Contains(out4, "select") {
		t.Errorf("expected no hint on left in StatePicker, got:\n%s", out4)
	}
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

func TestMessageAndTranscriptComponents(t *testing.T) {
	b := mock.New(80, 24)
	items := []trail.Node{
		UserPrompt(UserPromptProps{Text: "Hello world"}),
		Notice(NoticeProps{Text: "Switched session"}),
		ErrorMessage(ErrorMessageProps{Text: "Something failed"}),
		DecisionSummary(DecisionSummaryProps{
			Request:  api.PermissionRequest{ToolName: "bash", Summary: "Run ls"},
			Decision: api.PermissionDecision{Kind: api.DecisionAllowOnce},
		}),
		Message(MessageProps{
			Message: api.Message{
				Role: api.RoleAssistant,
				Content: []api.Block{
					{Type: api.BlockReasoning, Text: "Analyzing problem"},
					{Type: api.BlockText, Text: "I can help with that"},
				},
			},
		}),
	}

	app := trail.SimpleFC("TestApp", func() kitex.Node {
		return Transcript(TranscriptProps{Items: items})
	})
	inst, _ := trail.New(app(), trail.WithBackend(b))
	inst.Frame()

	out := insertedAboveText(b)
	if !strings.Contains(out, "> Hello world") {
		t.Errorf("user prompt missing from static transcript:\n%s", out)
	}
	if !strings.Contains(out, "Switched session") {
		t.Errorf("notice item missing from static transcript:\n%s", out)
	}
	if !strings.Contains(out, "error: Something failed") {
		t.Errorf("error item missing from static transcript:\n%s", out)
	}
	if !strings.Contains(out, "✓ Allowed once: Run ls") {
		t.Errorf("decision item missing from static transcript:\n%s", out)
	}
	if !strings.Contains(out, "○ Thought (Analyzing problem)") || !strings.Contains(out, "I can help with that") {
		t.Errorf("assistant message missing from static transcript:\n%s", out)
	}
	if strings.Contains(out, "[assistant]") {
		t.Errorf("expected no [assistant] badge in output:\n%s", out)
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

func TestReasoningSingleLineTruncation(t *testing.T) {
	// 1. Helper function checks
	if res := FormatSingleLineReasoning(""); res != "" {
		t.Errorf("expected empty for empty input, got %q", res)
	}
	if res := FormatSingleLineReasoning("Single line thought"); res != "Single line thought" {
		t.Errorf("expected single line thought, got %q", res)
	}
	multi := "First line of thought\nSecond line details\nThird line conclusion"
	if res := FormatSingleLineReasoning(multi); res != "First line of thought..." {
		t.Errorf("expected 'First line of thought...', got %q", res)
	}
	dots := "Already dotted...\nNext line"
	if res := FormatSingleLineReasoning(dots); res != "Already dotted..." {
		t.Errorf("expected 'Already dotted...', got %q", res)
	}

	// 2. Component render check
	b := mock.New(80, 24)
	app := trail.SimpleFC("TestApp", func() kitex.Node {
		return trail.Box(trail.BoxProps{},
			Reasoning(ReasoningProps{Text: multi}),
		)
	})
	inst, _ := trail.New(app(), trail.WithBackend(b))
	inst.Frame()

	out := surfaceText(b)
	if !strings.Contains(out, "○ Thought (First line of thought...)") {
		t.Errorf("expected '○ Thought (First line of thought...)', got:\n%s", out)
	}
	if strings.Contains(out, "Second line") || strings.Contains(out, "Third line") {
		t.Errorf("multi-line details should not appear in collapsed reasoning output:\n%s", out)
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

func TestComposerFunctionalComponent(t *testing.T) {
	cycleCalled := false
	cancelCalled := false

	b := mock.New(80, 24)
	app := trail.SimpleFC("TestApp", func() kitex.Node {
		return composerForTest(ComposerProps{
			OnCycleMode: func() {
				cycleCalled = true
			},
			OnCancel: func() {
				cancelCalled = true
			},
		})
	})

	inst, err := trail.New(app(), trail.WithBackend(b))
	if err != nil {
		t.Fatalf("trail.New: %v", err)
	}
	inst.Frame()

	// Initial render contains prompt symbol and has height 3 (top border + 1 row + bottom border)
	out := surfaceText(b)
	if !strings.Contains(out, ">") {
		t.Errorf("composer missing '>':\n%s", out)
	}
	if h := b.LastFrame().Surface.Height; h != 3 {
		t.Errorf("expected initial composer height 3, got %d:\n%s", h, out)
	}

	// Space and Shift+A must NOT trigger OnCycleMode
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{
		Key: key.Key{Code: ' ', Text: " "},
	})
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{
		Key: key.Key{Code: 'a', Text: "A", Mod: key.ModShift},
	})
	if cycleCalled {
		t.Errorf("OnCycleMode should not be called on space or Shift+A")
	}

	// Shift+Enter inserts a newline and grows the composer height to 4
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{
		Key: key.Key{Code: key.KeyEnter, Mod: key.ModShift},
	})
	inst.Frame()
	if h := b.LastFrame().Surface.Height; h != 4 {
		t.Errorf("expected composer height 4 after Shift+Enter newline, got %d:\n%s", h, surfaceText(b))
	}

	// Send Shift+Tab key event
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{
		Key: key.Key{Code: key.KeyTab, Mod: key.ModShift},
	})
	if !cycleCalled {
		t.Errorf("expected OnCycleMode to be called on Shift+Tab")
	}

	// Send Escape key event
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{
		Key: key.Key{Code: key.KeyEscape},
	})
	if !cancelCalled {
		t.Errorf("expected OnCancel to be called on Escape")
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

func TestComposerSuggestArgsAndSelection(t *testing.T) {
	opts := []components.Option{
		{Value: "default", Label: "default", Description: "Allow safe reads", Current: true},
		{Value: "accept_edits", Label: "accept_edits", Description: "Allow safe reads and workspace file edits"},
		{Value: "plan", Label: "plan", Description: "Read-only mode"},
		{Value: "ask", Label: "ask", Description: "Ask for confirmation"},
		{Value: "auto", Label: "auto", Description: "Allow all actions"},
	}

	var submittedText string
	engine := composerCompletionEngine(t, autocomplete.Source{
		Name: "modes", Trigger: "/mode ", StartOnly: true,
		Complete: func(context.Context, autocomplete.Request) ([]autocomplete.Candidate, error) {
			candidates := make([]autocomplete.Candidate, len(opts))
			for i, opt := range opts {
				candidates[i] = autocomplete.Candidate{
					Value: opt.Value, Label: opt.Label, Description: opt.Description,
				}
			}
			return candidates, nil
		},
	})
	b := mock.New(80, 24)
	app := trail.SimpleFC("TestApp", func() kitex.Node {
		return composerForTest(ComposerProps{
			Autocomplete: engine,
			OnSubmit: func(text string) {
				submittedText = text
			},
		})
	})

	inst, err := trail.New(app(), trail.WithBackend(b))
	if err != nil {
		t.Fatalf("trail.New: %v", err)
	}
	inst.Frame()

	// 1. Type "/mode " into composer
	for _, ch := range "/mode " {
		inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{
			Key: key.Key{Code: ch, Text: string(ch)},
		})
	}
	awaitComposer(t, inst, func() bool { return strings.Contains(surfaceText(b), "accept_edits") })

	out := surfaceText(b)
	if !strings.Contains(out, "default") || !strings.Contains(out, "accept_edits") || !strings.Contains(out, "plan") {
		t.Fatalf("expected mode options table below composer, got:\n%s", out)
	}

	// Verify alignment: row 0 has "❯ default", row 1 has "  accept_edits"
	// Both label texts must start at the exact same X column
	frame := b.LastFrame().Surface
	if frame == nil {
		t.Fatalf("expected rendered frame")
	}

	var row0Y, row1Y int
	for y := 0; y < frame.Height; y++ {
		rowStr := ""
		for x := 0; x < frame.Width; x++ {
			rowStr += frame.CellAt(x, y).Content
		}
		if strings.Contains(rowStr, "default") && strings.Contains(rowStr, "❯") {
			row0Y = y
		}
		if strings.Contains(rowStr, "accept_edits") {
			row1Y = y
		}
	}

	// In row0, "default" starts at X=2 (after "❯ ")
	if frame.CellAt(0, row0Y).Content != "❯" || frame.CellAt(1, row0Y).Content != " " || frame.CellAt(2, row0Y).Content != "d" {
		t.Errorf("row0 cursor/label mismatch: [%q, %q, %q]",
			frame.CellAt(0, row0Y).Content, frame.CellAt(1, row0Y).Content, frame.CellAt(2, row0Y).Content)
	}
	// In row1, "accept_edits" starts at X=2 (after "  ")
	if frame.CellAt(0, row1Y).Content != " " || frame.CellAt(1, row1Y).Content != " " || frame.CellAt(2, row1Y).Content != "a" {
		t.Errorf("row1 cursor/label mismatch: [%q, %q, %q] - leading spaces must not be stripped",
			frame.CellAt(0, row1Y).Content, frame.CellAt(1, row1Y).Content, frame.CellAt(2, row1Y).Content)
	}

	// 2. Press Down -> moves selection to accept_edits
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{
		Key: key.Key{Code: key.KeyDown},
	})
	inst.Frame()

	frameDown := b.LastFrame().Surface
	// Now row0 has unselected "  default" starting at X=2
	if frameDown.CellAt(0, row0Y).Content != " " || frameDown.CellAt(1, row0Y).Content != " " || frameDown.CellAt(2, row0Y).Content != "d" {
		t.Errorf("after Down, row0 mismatch: [%q, %q, %q]",
			frameDown.CellAt(0, row0Y).Content, frameDown.CellAt(1, row0Y).Content, frameDown.CellAt(2, row0Y).Content)
	}
	// And row1 has selected "❯ accept_edits" starting at X=2
	if frameDown.CellAt(0, row1Y).Content != "❯" || frameDown.CellAt(1, row1Y).Content != " " || frameDown.CellAt(2, row1Y).Content != "a" {
		t.Errorf("after Down, row1 mismatch: [%q, %q, %q]",
			frameDown.CellAt(0, row1Y).Content, frameDown.CellAt(1, row1Y).Content, frameDown.CellAt(2, row1Y).Content)
	}

	// Accepting a suggestion updates the draft without submitting it.
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{
		Key: key.Key{Code: key.KeyEnter},
	})
	inst.Frame()
	if submittedText != "" || !strings.Contains(surfaceText(b), "> /mode accept_edits") {
		t.Fatalf("accepting a suggestion submitted %q or lost the draft:\n%s", submittedText, surfaceText(b))
	}
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyEnter}})
	inst.Frame()
	if submittedText != "/mode accept_edits" {
		t.Errorf("submittedText = %q, want '/mode accept_edits'", submittedText)
	}
}

func TestComposerSlashCommandNavigationAndSelection(t *testing.T) {
	engine := composerCompletionEngine(t, composerCommandSource())

	var submittedText string
	b := mock.New(80, 24)
	app := trail.SimpleFC("TestApp", func() kitex.Node {
		return composerForTest(ComposerProps{
			Autocomplete: engine,
			OnSubmit: func(text string) {
				submittedText = text
			},
		})
	})

	inst, err := trail.New(app(), trail.WithBackend(b))
	if err != nil {
		t.Fatalf("trail.New: %v", err)
	}
	inst.Frame()

	// 1. Type "/"
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{
		Key: key.Key{Code: '/', Text: "/"},
	})
	awaitComposer(t, inst, func() bool { return strings.Contains(surfaceText(b), "❯ /help") })

	out := surfaceText(b)
	if !strings.Contains(out, "❯ /help") {
		t.Fatalf("expected row 0 highlighted with '❯ /help', got:\n%s", out)
	}
	if !strings.Contains(out, "  /mode [name]") {
		t.Errorf("expected row 1 unselected '  /mode', got:\n%s", out)
	}

	// 2. Press Down -> moves selection to row 1 (/mode)
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{
		Key: key.Key{Code: key.KeyDown},
	})
	inst.Frame()

	outDown := surfaceText(b)
	if !strings.Contains(outDown, "  /help") || !strings.Contains(outDown, "❯ /mode [name]") {
		t.Fatalf("expected row 1 highlighted with '❯ /mode [name]', got:\n%s", outDown)
	}

	// Enter accepts the highlighted command; a second Enter submits it.
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{
		Key: key.Key{Code: key.KeyEnter},
	})
	inst.Frame()

	if submittedText != "" {
		t.Fatalf("accepting a command submitted %q", submittedText)
	}
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyEnter}})
	inst.Frame()
	if submittedText != "/mode" {
		t.Errorf("submittedText = %q, want '/mode'", submittedText)
	}
}

func TestComposerSlashCommandTabCompletion(t *testing.T) {
	engine := composerCompletionEngine(t, composerCommandSource())

	b := mock.New(80, 24)
	app := trail.SimpleFC("TestApp", func() kitex.Node {
		return composerForTest(ComposerProps{
			Autocomplete: engine,
		})
	})

	inst, err := trail.New(app(), trail.WithBackend(b))
	if err != nil {
		t.Fatalf("trail.New: %v", err)
	}
	inst.Frame()

	// 1. Type "/"
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{
		Key: key.Key{Code: '/', Text: "/"},
	})
	awaitComposer(t, inst, func() bool { return strings.Contains(surfaceText(b), "❯ /help") })

	// 2. Press Down -> selects /mode
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{
		Key: key.Key{Code: key.KeyDown},
	})
	inst.Frame()

	// 3. Press Tab -> completes "/mode "
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{
		Key: key.Key{Code: key.KeyTab},
	})
	inst.Frame()

	// 4. Type 'x' to verify the trailing space after tab completion
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{
		Key: key.Key{Code: 'x', Text: "x"},
	})
	inst.Frame()

	outTab := surfaceText(b)
	if !strings.Contains(outTab, "> /mode x") {
		t.Errorf("expected input to contain '> /mode x' after Tab and typing 'x', got:\n%s", outTab)
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
