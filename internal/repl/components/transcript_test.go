package components

import (
	"strings"
	"testing"

	"github.com/masterkeysrd/kite/backend/mock"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/ui/hooks"
)

func TestTranscriptPrintsItemsToScrollback(t *testing.T) {
	b := mock.New(80, 24)
	items := []trail.Node{
		Message(MessageProps{Message: hooks.Message{ID: "u", Role: api.RoleUser, Parts: []hooks.MessagePart{{
			ID: "u:0", Type: hooks.PartText, Text: &hooks.TextPart{Text: "Hello world"},
		}}}}),
		Message(MessageProps{Message: hooks.Message{ID: "a", Role: api.RoleAssistant, Parts: []hooks.MessagePart{
			{ID: "a:0", Type: hooks.PartThought, Thought: &hooks.ThoughtPart{Text: "Analyzing problem"}},
			{ID: "a:1", Type: hooks.PartText, Text: &hooks.TextPart{Text: "I can help with that"}},
		}}}),
	}
	app := trail.SimpleFC("TestApp", func() kitex.Node {
		return Transcript(TranscriptProps{Items: items})
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
	for _, want := range []string{"> Hello world", "● Thought (Analyzing problem)", "I can help with that"} {
		if !strings.Contains(out, want) {
			t.Errorf("transcript missing %q:\n%s", want, out)
		}
	}
}
