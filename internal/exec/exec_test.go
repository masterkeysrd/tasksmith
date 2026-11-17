package exec

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/masterkeysrd/tasksmith/api"
)

func TestParseFormats(t *testing.T) {
	inTests := []struct {
		raw     string
		want    InputFormat
		wantErr bool
	}{
		{"", InputText, false},
		{"text", InputText, false},
		{"TEXT", InputText, false},
		{"jsonl", InputJSONL, false},
		{"  JSONL  ", InputJSONL, false},
		{"json", "", true},
	}
	for _, tt := range inTests {
		got, err := ParseInputFormat(tt.raw)
		if (err != nil) != tt.wantErr {
			t.Fatalf("ParseInputFormat(%q) err = %v, wantErr %v", tt.raw, err, tt.wantErr)
		}
		if got != tt.want {
			t.Fatalf("ParseInputFormat(%q) = %q, want %q", tt.raw, got, tt.want)
		}
	}

	outTests := []struct {
		raw     string
		want    OutputFormat
		wantErr bool
	}{
		{"", OutputText, false},
		{"text", OutputText, false},
		{"json", OutputJSON, false},
		{"jsonl", OutputJSONL, false},
		{"yaml", "", true},
	}
	for _, tt := range outTests {
		got, err := ParseOutputFormat(tt.raw)
		if (err != nil) != tt.wantErr {
			t.Fatalf("ParseOutputFormat(%q) err = %v, wantErr %v", tt.raw, err, tt.wantErr)
		}
		if got != tt.want {
			t.Fatalf("ParseOutputFormat(%q) = %q, want %q", tt.raw, got, tt.want)
		}
	}
}

// Non-slash prompts that happen to start with local command names ("help ...",
// "model ...", "usage ...", "compact ...", "exit ...") must never be hijacked
// as local slash commands.
func TestLocalCommandRequiresLeadingSlash(t *testing.T) {
	var out bytes.Buffer
	r := &run{
		opts: &Options{Stdout: &out, Stderr: &bytes.Buffer{}},
		w:    newTextWriter(&out, OutputText, true),
		sess: &api.Session{ID: "s-1"},
		quit: make(chan struct{}, 1),
	}

	plainPrompts := []string{
		"help me fix this bug",
		"model the database schema",
		"models should be immutable",
		"mode switching edge case",
		"agent orchestration design",
		"agents in parallel",
		"usage of context.WithCancel",
		"compact this slice in place",
		"exit early when nil",
		"quit the loop on EOF",
		"/",
	}
	for _, prompt := range plainPrompts {
		handled, err := r.localCommand(prompt)
		if handled || err != nil {
			t.Fatalf("localCommand(%q) = (%v, %v), want (false, nil)", prompt, handled, err)
		}
	}

	handled, err := r.localCommand("/help")
	if !handled || err != nil {
		t.Fatalf("localCommand(\"/help\") = (%v, %v), want (true, nil)", handled, err)
	}
	if !strings.Contains(out.String(), "exec local commands:") {
		t.Fatalf("expected /help output, got %q", out.String())
	}
}

func TestTextWriterSeparatesDistinctAssistantMessages(t *testing.T) {
	var buf bytes.Buffer
	w := newTextWriter(&buf, OutputText, true)

	if err := w.delta("msg-1", "Let me inspect the file."); err != nil {
		t.Fatal(err)
	}
	if err := w.delta("msg-1", " Reading now."); err != nil {
		t.Fatal(err)
	}
	if err := w.delta("msg-2", "Found the root cause."); err != nil {
		t.Fatal(err)
	}
	if err := w.finish(api.ExecResult{Status: api.ExecResultCompleted}); err != nil {
		t.Fatal(err)
	}

	want := "Let me inspect the file. Reading now.\n\nFound the root cause.\n"
	if buf.String() != want {
		t.Fatalf("textWriter output = %q, want %q", buf.String(), want)
	}
}

func TestJSONLWriterStampsResultFrameType(t *testing.T) {
	var buf bytes.Buffer
	w := newTextWriter(&buf, OutputJSONL, false)

	if err := w.finish(api.ExecResult{
		SessionID: "sess-1",
		Status:    api.ExecResultCompleted,
		Text:      "done",
	}); err != nil {
		t.Fatal(err)
	}

	var frame map[string]any
	if err := json.Unmarshal(buf.Bytes(), &frame); err != nil {
		t.Fatalf("decode terminal frame: %v", err)
	}
	if frame["type"] != api.ExecResultType {
		t.Fatalf("terminal jsonl frame type = %v, want %q", frame["type"], api.ExecResultType)
	}
}
