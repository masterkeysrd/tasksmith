package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/masterkeysrd/kite/backend"
	"github.com/masterkeysrd/kite/backend/mock"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/tasksmith/api"
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
	items := []TranscriptItem{
		PromptItem("Hello world"),
		NoticeItem("Switched session"),
		ErrorItem("Something failed"),
		DecisionItem(
			api.PermissionRequest{ToolName: "bash", Summary: "Run ls"},
			api.PermissionDecision{Kind: api.DecisionAllowOnce},
		),
		MessageItem(api.Message{
			Role: api.RoleAssistant,
			Content: []api.Block{
				{Type: api.BlockReasoning, Text: "Analyzing problem"},
				{Type: api.BlockText, Text: "I can help with that"},
			},
		}),
	}

	app := trail.SimpleFC("TestApp", func() kitex.Node {
		return Transcript(items)
	})
	inst, _ := trail.New(app(), trail.WithBackend(b))
	inst.Frame()

	out := insertedAboveText(b)
	if !strings.Contains(out, "❯ Hello world") {
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
	if !strings.Contains(out, "[reasoning] Analyzing problem") || !strings.Contains(out, "I can help with that") {
		t.Errorf("assistant message missing from static transcript:\n%s", out)
	}
}

func TestComposerFunctionalComponent(t *testing.T) {
	cycleCalled := false
	cancelCalled := false

	b := mock.New(80, 24)
	app := trail.SimpleFC("TestApp", func() kitex.Node {
		return Composer(ComposerProps{
			Suggest: func(prefix string) []Suggestion {
				if prefix == "/h" {
					return []Suggestion{{Usage: "/help", Description: "Show commands"}}
				}
				return nil
			},
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
	opts := []Option{
		{Value: "default", Label: "default", Description: "Allow safe reads", Current: true},
		{Value: "accept_edits", Label: "accept_edits", Description: "Allow safe reads and workspace file edits"},
		{Value: "plan", Label: "plan", Description: "Read-only mode"},
		{Value: "ask", Label: "ask", Description: "Ask for confirmation"},
		{Value: "auto", Label: "auto", Description: "Allow all actions"},
	}

	var submittedText string
	b := mock.New(80, 24)
	app := trail.SimpleFC("TestApp", func() kitex.Node {
		return Composer(ComposerProps{
			SuggestArgs: func(rawLine string) ([]Option, bool) {
				if strings.HasPrefix(rawLine, "/mode ") {
					return opts, true
				}
				return nil, false
			},
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
	inst.Frame()

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

	// 3. Press Enter -> submits "/mode accept_edits"
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{
		Key: key.Key{Code: key.KeyEnter},
	})
	inst.Frame()
	if submittedText != "/mode accept_edits" {
		t.Errorf("submittedText = %q, want '/mode accept_edits'", submittedText)
	}
}
