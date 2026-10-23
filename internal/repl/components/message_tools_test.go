package components

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/masterkeysrd/kite/element"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/testenv"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/ui/hooks"
)

func renderViewTool(t *testing.T, width int) func(hooks.ToolPart) string {
	t.Helper()
	env := testenv.Default(width, 12)
	t.Cleanup(env.Close)
	container := element.NewBox(env.Document())
	env.Mount(container)
	return func(tool hooks.ToolPart) string {
		kitex.Render(ViewToolRenderer(tool), container)
		env.Flush()
		return strings.TrimSpace(env.DumpText())
	}
}

func viewResponse(t *testing.T, output api.ViewOutput) *api.Message {
	t.Helper()
	data, err := json.Marshal(output)
	if err != nil {
		t.Fatal(err)
	}
	return &api.Message{
		ID: "response", Role: api.RoleTool, ToolCallID: "call",
		StructuredContent: data,
	}
}

func TestViewToolRendererRunningAndCompleted(t *testing.T) {
	render := renderViewTool(t, 100)
	call := &api.ToolCall{ID: "call", Name: "view", Args: map[string]any{
		"path": "requested/main.go", "offset": float64(10), "limit": float64(20),
	}}
	running := hooks.ToolPart{ID: "call", Call: call, Status: hooks.ToolRunning}
	before, err := json.Marshal(running)
	if err != nil {
		t.Fatal(err)
	}
	if got := render(running); got != "○ Reading \ue627 requested/main.go (lines 10-29)" {
		t.Fatalf("running read = %q", got)
	}
	completed := running
	completed.Status = hooks.ToolSucceeded
	completed.Response = viewResponse(t, api.ViewOutput{
		Path: "resolved/main.go", StartLine: 10, EndLine: 15, TotalLines: 15,
		Content: "must not dump the file contents",
	})
	if got := render(completed); got != "● Read \ue627 resolved/main.go (lines 10-15)" {
		t.Fatalf("completed read = %q", got)
	}
	after, err := json.Marshal(running)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("renderer mutated the tool view model")
	}
}

func TestViewToolRendererRequestedRanges(t *testing.T) {
	tests := []struct {
		name     string
		args     map[string]any
		expected string
	}{
		{"unspecified", map[string]any{"path": "file.go"}, "○ Reading \ue627 file.go"},
		{"offset", map[string]any{"path": "file.go", "offset": 12}, "○ Reading \ue627 file.go (from line 12)"},
		{"limit", map[string]any{"path": "file.go", "limit": 5}, "○ Reading \ue627 file.go (lines 1-5)"},
		{"single line", map[string]any{"path": "file.go", "offset": 3, "limit": 1}, "○ Reading \ue627 file.go (line 3)"},
		{"default offset", map[string]any{"path": "file.go", "offset": -1, "limit": 5}, "○ Reading \ue627 file.go (lines 1-5)"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			render := renderViewTool(t, 100)
			got := render(hooks.ToolPart{Call: &api.ToolCall{Args: test.args}, Status: hooks.ToolRunning})
			if got != test.expected {
				t.Fatalf("requested range = %q, want %q", got, test.expected)
			}
		})
	}
	render := renderViewTool(t, 100)
	got := render(hooks.ToolPart{Status: hooks.ToolRunning, Call: &api.ToolCall{Args: map[string]any{
		"path": "file.go", "offset": math.MaxInt, "limit": math.MaxInt,
	}}})
	if strings.Contains(got, "lines -") || !strings.Contains(got, "○ Reading \ue627 file.go") {
		t.Fatalf("large requested range overflowed: %q", got)
	}
}

func TestViewToolRendererPartialPendingAndInterrupted(t *testing.T) {
	render := renderViewTool(t, 100)
	for _, tool := range []hooks.ToolPart{
		{Status: hooks.ToolReceiving},
		{Status: hooks.ToolReceiving, Call: &api.ToolCall{Name: "view"}},
		{Status: hooks.ToolReceiving, Call: &api.ToolCall{Name: "view", Args: map[string]any{}}},
	} {
		if got := render(tool); got != "○ Reading" {
			t.Fatalf("partial read = %q", got)
		}
	}
	for _, test := range []struct {
		status   hooks.ToolStatus
		expected string
	}{
		{hooks.ToolPending, "○ Read file.go (pending)"},
		{hooks.ToolWaitingPermission, "○ Read file.go (waiting for permission)"},
		{hooks.ToolInterrupted, "○ Read file.go (interrupted)"},
		{hooks.ToolFailed, "○ Read file.go (failed without a result)"},
	} {
		got := render(hooks.ToolPart{
			Status: test.status, Call: &api.ToolCall{Args: map[string]any{"path": "file.go"}},
		})
		if got != test.expected {
			t.Errorf("%s = %q, want %q", test.status, got, test.expected)
		}
	}
}

func TestViewToolRendererUnmatchedAndEmptyResults(t *testing.T) {
	render := renderViewTool(t, 100)
	got := render(hooks.ToolPart{Response: viewResponse(t, api.ViewOutput{
		Path: "unmatched.go", StartLine: 2, EndLine: 2, TotalLines: 2,
	})})
	if got != "● Read \ue627 unmatched.go (line 2)" {
		t.Fatalf("unmatched result = %q", got)
	}
	got = render(hooks.ToolPart{Response: viewResponse(t, api.ViewOutput{
		Path: "empty.go",
	})})
	if got != "● Read \ue627 empty.go" {
		t.Fatalf("empty file rendered a range: %q", got)
	}
	got = render(hooks.ToolPart{
		Call: &api.ToolCall{Args: map[string]any{"path": "file.go", "offset": 100}},
		Response: viewResponse(t, api.ViewOutput{
			Path: "file.go", TotalLines: 10,
		}),
	})
	if got != "● Read \ue627 file.go" {
		t.Fatalf("read beyond EOF rendered the requested range: %q", got)
	}
	got = render(hooks.ToolPart{
		Status: hooks.ToolRunning,
		Response: viewResponse(t, api.ViewOutput{
			Path: "finished.go", StartLine: 1, EndLine: 2, TotalLines: 2,
		}),
	})
	if got != "● Read \ue627 finished.go (lines 1-2)" {
		t.Fatalf("persisted response did not finish the running indicator: %q", got)
	}
}

func TestViewToolRendererErrors(t *testing.T) {
	render := renderViewTool(t, 120)
	got := render(hooks.ToolPart{
		Call: &api.ToolCall{Args: map[string]any{"path": "missing.go"}}, Status: hooks.ToolFailed,
		Response: &api.Message{IsError: true, Content: []api.Block{
			{Type: api.BlockText, Text: "file not found\ncomplete error details"},
		}},
	})
	for _, expected := range []string{"● Read failed missing.go", "file not found", "complete error details"} {
		if !strings.Contains(got, expected) {
			t.Errorf("missing %q in error output: %q", expected, got)
		}
	}
	for _, raw := range []string{``, `{`, `[]`, `{"path":42}`} {
		got := render(hooks.ToolPart{Response: &api.Message{StructuredContent: json.RawMessage(raw)}})
		if !strings.Contains(got, "Unable to render read result") || strings.Contains(got, "● Read") {
			t.Errorf("malformed result %q looked successful: %q", raw, got)
		}
	}
	for _, args := range []map[string]any{{"path": 42}, {"offset": "wrong"}, {"path": math.NaN()}} {
		got := render(hooks.ToolPart{Call: &api.ToolCall{Args: args}, Status: hooks.ToolRunning})
		if !strings.Contains(got, "Unable to render read arguments") {
			t.Errorf("invalid arguments were hidden: %q", got)
		}
	}
}

func TestViewToolRendererUsesStructuredContent(t *testing.T) {
	render := renderViewTool(t, 100)
	response := viewResponse(t, api.ViewOutput{
		Path: "structured.go", StartLine: 2, EndLine: 3,
	})
	response.Content = []api.Block{{Type: api.BlockText, Text: "not a structured tool result"}}
	if got := render(hooks.ToolPart{Response: response}); got != "● Read \ue627 structured.go (lines 2-3)" {
		t.Fatalf("structured result = %q", got)
	}
	response.StructuredContent = nil
	response.Content[0].Text = `{"path":"text.go","start_line":2,"end_line":3}`
	if got := render(hooks.ToolPart{Response: response}); !strings.Contains(got, "Unable to render read result") {
		t.Fatalf("missing structured content fell back to text: %q", got)
	}
}

func TestViewToolRendererNarrowWidth(t *testing.T) {
	render := renderViewTool(t, 30)
	got := render(hooks.ToolPart{
		Status: hooks.ToolRunning,
		Call: &api.ToolCall{Args: map[string]any{
			"path": "a/very/long/directory/with/a/long/file.go", "offset": 1, "limit": 50,
		}},
	})
	if !strings.HasPrefix(got, "○ Reading ") {
		t.Fatalf("running indicator was clipped: %q", got)
	}
	if strings.Contains(got, "\n") {
		t.Fatalf("compact read summary wrapped: %q", got)
	}
}
