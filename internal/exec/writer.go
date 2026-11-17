package exec

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/masterkeysrd/tasksmith/api"
)

// turnTracker accumulates the assistant output of the in-flight turn from the
// daemon's event stream. Chunk deltas are merged per message ID exactly like
// the TUI does, and a confirmed assistant entry supersedes the streamed draft
// of the same ID so the reported text is always the persisted one.
type turnTracker struct {
	// streamed holds the merged text of still-streaming assistant drafts.
	streamed map[string]*strings.Builder
	// order preserves the order drafts first appeared.
	order []string
	// texts collects the persisted text of every assistant message of the
	// run, in order (a turn with tool calls produces several).
	texts []string
	// sawAssistant marks that at least one assistant message was observed.
	sawAssistant bool
}

func newTurnTracker() *turnTracker {
	return &turnTracker{streamed: make(map[string]*strings.Builder)}
}

// observe folds one daemon event into the tracker.
func (t *turnTracker) observe(ev api.Event) {
	switch p := ev.Payload.(type) {
	case api.ChunkEvent:
		t.addChunk(p.Chunk)
	case api.EntryAddedEvent:
		if p.Entry.Kind == api.TimelineKindCommand && p.Entry.Command != nil {
			if out := strings.TrimRight(p.Entry.Command.Output, "\n"); out != "" {
				t.texts = append(t.texts, out)
			}
			return
		}
		if p.Entry.Kind != api.TimelineKindMessage || p.Entry.Message == nil {
			return
		}
		msg := p.Entry.Message
		if msg.Role != api.RoleAssistant {
			return
		}
		t.sawAssistant = true
		if text := messageText(msg); text != "" {
			t.texts = append(t.texts, text)
		}
		delete(t.streamed, msg.ID)
		t.order = slices.DeleteFunc(t.order, func(id string) bool { return id == msg.ID })
	}
}

// addChunk merges an incremental assistant delta into its draft.
func (t *turnTracker) addChunk(chunk api.Chunk) {
	for _, b := range chunk.Content {
		if b.Type != api.BlockText || b.Text == "" {
			continue
		}
		sb, ok := t.streamed[chunk.ID]
		if !ok {
			sb = &strings.Builder{}
			t.streamed[chunk.ID] = sb
			t.order = append(t.order, chunk.ID)
		}
		sb.WriteString(b.Text)
		t.sawAssistant = true
	}
}

// Text returns the run's assistant output: the persisted messages when any
// were confirmed, otherwise whatever streamed without confirmation (a canceled
// turn keeps its partial reply).
func (t *turnTracker) Text() string {
	if len(t.texts) > 0 {
		return strings.Join(t.texts, "\n\n")
	}
	var out strings.Builder
	for _, id := range t.order {
		sb, ok := t.streamed[id]
		if !ok || sb.Len() == 0 {
			continue
		}
		if out.Len() > 0 {
			out.WriteString("\n\n")
		}
		out.WriteString(sb.String())
	}
	return out.String()
}

// messageText joins the text blocks of a persisted message.
func messageText(msg *api.Message) string {
	var b strings.Builder
	for _, block := range msg.Content {
		if block.Type != api.BlockText {
			continue
		}
		b.WriteString(block.Text)
	}
	return b.String()
}

// writer renders a run's output. Implementations own their framing; the runner
// hands them every event, every client-local command result, and the settled
// outcome.
type writer interface {
	// begin announces the resolved session before anything is sent.
	begin(header api.ExecHeaderFrame) error
	// event forwards one daemon event.
	event(ev api.Event) error
	// commandResult reports a client-local `/slash` outcome.
	commandResult(res api.ExecCommandResultFrame) error
	// delta streams an assistant text delta for chunkID (text mode only;
	// framing-aware writers render chunks through event instead).
	delta(chunkID, text string) error
	// finish writes the terminal result and flushes.
	finish(res api.ExecResult) error
}

// newTextWriter builds the writer for a run's output format. In text mode
// live=true streams assistant deltas as they arrive.
func newTextWriter(out io.Writer, format OutputFormat, live bool) writer {
	switch format {
	case OutputJSON:
		return &jsonWriter{out: out}
	case OutputJSONL:
		return &jsonlWriter{out: out}
	default:
		return &textWriter{out: out, live: live}
	}
}

// textWriter prints assistant text on stdout and nothing else; diagnostics go
// to stderr via the runner.
type textWriter struct {
	out         io.Writer
	live        bool
	wrote       bool
	lastChunkID string
	fin         bool
	result      api.ExecResult
}

func (w *textWriter) begin(api.ExecHeaderFrame) error { return nil }

func (w *textWriter) event(api.Event) error { return nil }

func (w *textWriter) commandResult(res api.ExecCommandResultFrame) error {
	if !res.OK {
		if res.Error != "" {
			return writeLine(w.out, res.Error)
		}
		return nil
	}
	if res.Output != "" {
		return writeLine(w.out, res.Output)
	}
	return nil
}

func (w *textWriter) delta(chunkID, text string) error {
	if !w.live || text == "" {
		return nil
	}
	if w.wrote && chunkID != "" && w.lastChunkID != "" && chunkID != w.lastChunkID {
		if _, err := io.WriteString(w.out, "\n\n"); err != nil {
			return err
		}
	}
	if chunkID != "" {
		w.lastChunkID = chunkID
	}
	w.wrote = true
	_, err := io.WriteString(w.out, text)
	return err
}

func (w *textWriter) finish(res api.ExecResult) error {
	w.result = res
	w.fin = true
	if !w.live {
		if res.Text != "" {
			if err := writeLine(w.out, res.Text); err != nil {
				return err
			}
			w.wrote = true
		}
		return nil
	}
	// Live streaming already printed the reply; close the line once.
	if w.wrote {
		_, err := io.WriteString(w.out, "\n")
		return err
	}
	if res.Text != "" {
		return writeLine(w.out, res.Text)
	}
	return nil
}

// jsonWriter buffers everything and prints one [api.ExecResult] envelope.
type jsonWriter struct {
	out io.Writer
}

func (w *jsonWriter) begin(api.ExecHeaderFrame) error { return nil }
func (w *jsonWriter) event(api.Event) error           { return nil }
func (w *jsonWriter) commandResult(api.ExecCommandResultFrame) error {
	return nil
}
func (w *jsonWriter) delta(string, string) error { return nil }

func (w *jsonWriter) finish(res api.ExecResult) error {
	return api.WriteNDJSON(w.out, res)
}

// jsonlWriter emits NDJSON frames: header, verbatim events, command results,
// and a terminal result frame.
type jsonlWriter struct {
	out io.Writer
}

func (w *jsonlWriter) begin(header api.ExecHeaderFrame) error {
	header.Type = api.ExecHeaderType
	return api.WriteNDJSON(w.out, header)
}

func (w *jsonlWriter) event(ev api.Event) error { return api.WriteEvent(w.out, ev) }

func (w *jsonlWriter) commandResult(res api.ExecCommandResultFrame) error {
	res.Type = api.ExecCommandResultType
	return api.WriteNDJSON(w.out, res)
}

func (w *jsonlWriter) delta(string, string) error { return nil }

func (w *jsonlWriter) finish(res api.ExecResult) error {
	res.Type = api.ExecResultType
	return api.WriteNDJSON(w.out, res)
}

// writeLine writes s followed by a newline.
func writeLine(w io.Writer, s string) error {
	_, err := fmt.Fprintln(w, s)
	return err
}
