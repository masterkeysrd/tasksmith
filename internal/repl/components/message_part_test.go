package components

import (
	"strings"
	"testing"

	"github.com/masterkeysrd/kite/element"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/testenv"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/tools/builtin"
	"github.com/masterkeysrd/tasksmith/internal/ui/hooks"
)

func renderMessagePart(t *testing.T, width int) func(trail.Node) string {
	t.Helper()
	env := testenv.Default(width, 12)
	t.Cleanup(env.Close)
	container := element.NewBox(env.Document())
	env.Mount(container)
	return func(node trail.Node) string {
		kitex.Render(node, container)
		env.Flush()
		return strings.TrimSpace(env.DumpText())
	}
}

func TestMessageToolDispatchesViewRenderer(t *testing.T) {
	render := renderMessagePart(t, 100)
	tool := hooks.ToolPart{
		Call: &api.ToolCall{ID: "call", Name: "view", Args: map[string]any{
			"path": "file.go", "offset": 2, "limit": 3,
		}},
		Status: hooks.ToolRunning,
	}
	if got := render(MessageTool(MessageToolProps{Tool: tool})); got != "○ Reading \ue627 file.go (lines 2-4)" {
		t.Fatalf("running tool = %q", got)
	}
	tool.Response = viewResponse(t, builtin.ViewOutput{
		Path: "file.go", StartLine: 2, EndLine: 3,
	})
	tool.Status = hooks.ToolSucceeded
	if got := render(MessageTool(MessageToolProps{Tool: tool})); got != "● Read \ue627 file.go (lines 2-3)" {
		t.Fatalf("completed tool = %q", got)
	}
	tool.Response.Name = "view"
	tool.Call = nil
	if got := render(MessageTool(MessageToolProps{Tool: tool})); got != "● Read \ue627 file.go (lines 2-3)" {
		t.Fatalf("unmatched response = %q", got)
	}
}

func TestMessageTextRendersMarkdownAndUpdates(t *testing.T) {
	render := renderMessagePart(t, 100)
	if got := render(MessageText(MessageTextProps{Text: "Hello **world** and `code`."})); got != "Hello world and code." {
		t.Fatalf("markdown text = %q", got)
	}
	if got := render(MessageText(MessageTextProps{Text: "Updated *message*."})); got != "Updated message." {
		t.Fatalf("updated markdown text = %q", got)
	}
	if got := render(MessageText(MessageTextProps{Text: " \n\t "})); got != "" {
		t.Fatalf("empty text = %q", got)
	}
}

func TestMessageTextWrapsWithinWidth(t *testing.T) {
	render := renderMessagePart(t, 24)
	got := render(MessageText(MessageTextProps{Text: "First words followed by more words on another line."}))
	if !strings.Contains(got, "\n") {
		t.Fatalf("message did not wrap: %q", got)
	}
	if strings.Join(strings.Fields(got), " ") != "First words followed by more words on another line." {
		t.Fatalf("wrapping lost message text: %q", got)
	}
}

func TestMessageThoughtStreamingAndCompleted(t *testing.T) {
	render := renderMessagePart(t, 100)
	props := MessageThoughtProps{Text: "Inspecting the relevant files", Streaming: true}
	if got := render(MessageThought(props)); got != "○ Thinking (Inspecting the relevant files)" {
		t.Fatalf("streaming thought = %q", got)
	}
	props.Streaming = false
	if got := render(MessageThought(props)); got != "● Thought (Inspecting the relevant files)" {
		t.Fatalf("completed thought = %q", got)
	}
	props.Text = "\n  First thought. \nSecond thought\n"
	if got := render(MessageThought(props)); got != "● Thought (First thought...)" {
		t.Fatalf("multiline thought = %q", got)
	}
	if got := render(MessageThought(MessageThoughtProps{Text: " \n\t "})); got != "" {
		t.Fatalf("empty thought = %q", got)
	}
}

func TestMessageThoughtNarrowWidth(t *testing.T) {
	render := renderMessagePart(t, 30)
	got := render(MessageThought(MessageThoughtProps{
		Text: "A long thought that should stay on a single compact line", Streaming: true,
	}))
	if !strings.HasPrefix(got, "○ Thinking (") || !strings.HasSuffix(got, ")") {
		t.Fatalf("thought status or delimiter was clipped: %q", got)
	}
	if strings.Contains(got, "\n") {
		t.Fatalf("thought summary wrapped: %q", got)
	}
}
