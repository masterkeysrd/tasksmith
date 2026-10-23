package components

import (
	"strings"
	"testing"

	"github.com/masterkeysrd/kite/backend/mock"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/repl/ui"
	"github.com/masterkeysrd/tasksmith/internal/ui/hooks"
)

func TestTranscriptPrintsEntriesInOrder(t *testing.T) {
	b := mock.New(80, 24)
	entries := []TranscriptEntry{
		HeaderEntry(ui.HeaderProps{Version: "v1", Model: "Test Model", Thinking: "high", Path: "/work"}),
		MessageEntry(hooks.Message{ID: "u", Role: api.RoleUser, Parts: []hooks.MessagePart{{
			ID: "u:0", Type: hooks.PartText, Text: &hooks.TextPart{Text: "Hello world"},
		}}}),
		NoticeEntry(ui.NoticeProps{Text: "Switched session"}),
		MessageEntry(hooks.Message{ID: "a", Role: api.RoleAssistant, Parts: []hooks.MessagePart{
			{ID: "a:0", Type: hooks.PartThought, Thought: &hooks.ThoughtPart{Text: "Analyzing problem"}},
			{ID: "a:1", Type: hooks.PartText, Text: &hooks.TextPart{Text: "I can help with that"}},
		}}),
		ErrorEntry(ui.ErrorMessageProps{Text: "Something failed"}),
		DecisionEntry(ui.DecisionSummaryProps{
			Request:  api.PermissionRequest{ToolName: "bash", Summary: "Run ls"},
			Decision: api.PermissionDecision{Kind: api.DecisionAllowOnce},
		}),
		CommandEntry(ui.CommandOutputProps{Command: "/mode", Output: "Switched to plan mode."}),
	}
	app := trail.SimpleFC("TestApp", func() kitex.Node {
		return Transcript(TranscriptProps{Entries: entries})
	})
	inst, _ := trail.New(app(), trail.WithBackend(b))
	inst.Frame()

	var sb strings.Builder
	for _, buf := range b.InsertedAbove {
		for y := 0; y < buf.Height; y++ {
			for x := 0; x < buf.Width; x++ {
				c := buf.CellAt(x, y)
				if c.Content == "" {
					sb.WriteByte(' ')
				} else {
					sb.WriteString(c.Content)
				}
			}
			sb.WriteByte('\n')
		}
	}
	out := sb.String()
	last := -1
	for _, want := range []string{
		"TaskSmith v1", "Test Model", "> Hello world", "Switched session",
		"● Thought (Analyzing problem)", "I can help with that", "error: Something failed",
		"✓ Allowed once: Run ls", "> /mode",
	} {
		i := strings.Index(out, want)
		if i < 0 {
			t.Fatalf("transcript missing %q:\n%s", want, out)
		}
		if i < last {
			t.Errorf("%q printed out of order:\n%s", want, out)
		}
		last = i
	}
}
