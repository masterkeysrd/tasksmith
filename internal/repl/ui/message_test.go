package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/masterkeysrd/kite/backend/mock"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/tasksmith/api"
)

func renderNodeToText(node trail.Node) string {
	return renderNodeToTextWithWidth(node, 120)
}

func renderNodeToTextWithWidth(node trail.Node, width int) string {
	if node == nil {
		return ""
	}
	b := mock.New(width, 24)
	app := trail.SimpleFC("TestApp", func() kitex.Node {
		return trail.Box(trail.BoxProps{}, node)
	})
	inst, _ := trail.New(app(), trail.WithBackend(b))
	inst.Frame()
	return surfaceText(b)
}

func TestReasoningStates(t *testing.T) {
	t.Run("Running", func(t *testing.T) {
		out := renderNodeToText(Reasoning(ReasoningProps{
			Text:    "working through options",
			Running: true,
		}))
		expected := "○ Thinking (working through options)"
		if !strings.Contains(out, expected) {
			t.Errorf("expected %q, got:\n%s", expected, out)
		}
	})

	t.Run("FinishedWithDuration", func(t *testing.T) {
		out := renderNodeToText(Reasoning(ReasoningProps{
			Text:     "working through options",
			Duration: 6200 * time.Millisecond,
			Running:  false,
		}))
		expected := "○ Thought for 6.2s (working through options)"
		if !strings.Contains(out, expected) {
			t.Errorf("expected %q, got:\n%s", expected, out)
		}
	})

	t.Run("FinishedWithoutDuration", func(t *testing.T) {
		out := renderNodeToText(Reasoning(ReasoningProps{
			Text:     "working through options",
			Duration: 0,
			Running:  false,
		}))
		expected := "○ Thought (working through options)"
		if !strings.Contains(out, expected) {
			t.Errorf("expected %q, got:\n%s", expected, out)
		}
	})
}

func TestToolCategoryAndArgExtraction(t *testing.T) {
	tests := []struct {
		call     api.ToolCall
		wantCat  string
		wantArg  string
	}{
		{
			call:    api.ToolCall{Name: "bash", Args: map[string]any{"command": "git status"}},
			wantCat: "bash",
			wantArg: "git status",
		},
		{
			call:    api.ToolCall{Name: "view", Args: map[string]any{"path": "/a/b/c/main.go"}},
			wantCat: "explore",
			wantArg: "main.go",
		},
		{
			call:    api.ToolCall{Name: "ls", Args: map[string]any{"path": "./internal"}},
			wantCat: "explore",
			wantArg: "./internal",
		},
		{
			call:    api.ToolCall{Name: "glob", Args: map[string]any{"pattern": "*.go"}},
			wantCat: "explore",
			wantArg: "*.go",
		},
		{
			call:    api.ToolCall{Name: "grep", Args: map[string]any{"pattern": "TODO"}},
			wantCat: "search",
			wantArg: "TODO",
		},
		{
			call:    api.ToolCall{Name: "edit", Args: map[string]any{"path": "file.go"}},
			wantCat: "edit",
			wantArg: "file.go",
		},
		{
			call:    api.ToolCall{Name: "write", Args: map[string]any{"path": "out.txt"}},
			wantCat: "write",
			wantArg: "out.txt",
		},
		{
			call:    api.ToolCall{Name: "remove", Args: map[string]any{"path": "old.txt"}},
			wantCat: "remove",
			wantArg: "old.txt",
		},
		{
			call:    api.ToolCall{Name: "web_fetch", Args: map[string]any{"url": "https://example.com"}},
			wantCat: "fetch",
			wantArg: "https://example.com",
		},
		{
			call:    api.ToolCall{Name: "custom", Args: map[string]any{"name": "foo"}},
			wantCat: "custom",
			wantArg: "foo",
		},
	}

	for _, tt := range tests {
		cat := toolCategory(tt.call.Name)
		if cat != tt.wantCat {
			t.Errorf("toolCategory(%q) = %q, want %q", tt.call.Name, cat, tt.wantCat)
		}
		arg := toolPrimaryArg(tt.call)
		if arg != tt.wantArg {
			t.Errorf("toolPrimaryArg(%q) = %q, want %q", tt.call.Name, arg, tt.wantArg)
		}
	}
}

func TestGroupToolCalls(t *testing.T) {
	calls := []api.ToolCall{
		{Name: "view", Args: map[string]any{"path": "a.go"}},
		{Name: "ls", Args: map[string]any{"path": "dir"}},
		{Name: "bash", Args: map[string]any{"command": "echo 1"}},
		{Name: "bash", Args: map[string]any{"command": "echo 2"}},
		{Name: "grep", Args: map[string]any{"pattern": "p"}},
		{Name: "view", Args: map[string]any{"path": "b.go"}},
	}

	groups := GroupToolCalls(calls)
	if len(groups) != 4 {
		t.Fatalf("expected 4 groups, got %d", len(groups))
	}
	if len(groups[0]) != 2 || groups[0][0].Name != "view" || groups[0][1].Name != "ls" {
		t.Errorf("group 0 unexpected: %+v", groups[0])
	}
	if len(groups[1]) != 2 || groups[1][0].Name != "bash" || groups[1][1].Name != "bash" {
		t.Errorf("group 1 unexpected: %+v", groups[1])
	}
	if len(groups[2]) != 1 || groups[2][0].Name != "grep" {
		t.Errorf("group 2 unexpected: %+v", groups[2])
	}
	if len(groups[3]) != 1 || groups[3][0].Name != "view" {
		t.Errorf("group 3 unexpected: %+v", groups[3])
	}
}

func TestFormatToolGroupSummary(t *testing.T) {
	// 1. Bash
	bash1 := []api.ToolCall{{Name: "bash", Args: map[string]any{"command": "make test"}}}
	if s := FormatToolGroupSummary(bash1, true); s != "Running (make test)" {
		t.Errorf("bash1 running: %q", s)
	}
	if s := FormatToolGroupSummary(bash1, false); s != "Ran (make test)" {
		t.Errorf("bash1 finished: %q", s)
	}
	bashN := []api.ToolCall{
		{Name: "bash", Args: map[string]any{"command": "cmd1"}},
		{Name: "bash", Args: map[string]any{"command": "cmd2"}},
		{Name: "bash", Args: map[string]any{"command": "cmd3"}},
	}
	if s := FormatToolGroupSummary(bashN, true); s != "Running 3 commands (+2 · cmd3)" {
		t.Errorf("bashN running: %q", s)
	}
	if s := FormatToolGroupSummary(bashN, false); s != "Ran 3 commands (+2 · cmd3)" {
		t.Errorf("bashN finished: %q", s)
	}

	// 2. Explore
	exp1 := []api.ToolCall{{Name: "view", Args: map[string]any{"path": "/pkg/main.go"}}}
	if s := FormatToolGroupSummary(exp1, true); s != "Exploring 1 file (main.go)" {
		t.Errorf("exp1 running: %q", s)
	}
	if s := FormatToolGroupSummary(exp1, false); s != "Explored 1 file (main.go)" {
		t.Errorf("exp1 finished: %q", s)
	}
	expN := []api.ToolCall{
		{Name: "view", Args: map[string]any{"path": "a.go"}},
		{Name: "ls", Args: map[string]any{"path": "dir"}},
	}
	if s := FormatToolGroupSummary(expN, true); s != "Exploring 2 files (+1 · dir)" {
		t.Errorf("expN running: %q", s)
	}
	if s := FormatToolGroupSummary(expN, false); s != "Explored 2 files (+1 · dir)" {
		t.Errorf("expN finished: %q", s)
	}

	// 3. Search
	search1 := []api.ToolCall{{Name: "grep", Args: map[string]any{"pattern": "error"}}}
	if s := FormatToolGroupSummary(search1, true); s != "Searching (error)" {
		t.Errorf("search1 running: %q", s)
	}
	if s := FormatToolGroupSummary(search1, false); s != "Searched (error)" {
		t.Errorf("search1 finished: %q", s)
	}
	searchN := []api.ToolCall{
		{Name: "grep", Args: map[string]any{"pattern": "p1"}},
		{Name: "grep", Args: map[string]any{"pattern": "p2"}},
	}
	if s := FormatToolGroupSummary(searchN, true); s != "Searching 2 patterns (+1 · p2)" {
		t.Errorf("searchN running: %q", s)
	}
	if s := FormatToolGroupSummary(searchN, false); s != "Searched 2 patterns (+1 · p2)" {
		t.Errorf("searchN finished: %q", s)
	}

	// 4. Edit
	edit1 := []api.ToolCall{{Name: "edit", Args: map[string]any{"path": "app.go"}}}
	if s := FormatToolGroupSummary(edit1, true); s != "Editing (app.go)" {
		t.Errorf("edit1 running: %q", s)
	}
	if s := FormatToolGroupSummary(edit1, false); s != "Edited (app.go)" {
		t.Errorf("edit1 finished: %q", s)
	}

	// 5. Write
	write1 := []api.ToolCall{{Name: "write", Args: map[string]any{"path": "new.go"}}}
	if s := FormatToolGroupSummary(write1, true); s != "Writing (new.go)" {
		t.Errorf("write1 running: %q", s)
	}
	if s := FormatToolGroupSummary(write1, false); s != "Wrote (new.go)" {
		t.Errorf("write1 finished: %q", s)
	}

	// 6. Remove
	rem1 := []api.ToolCall{{Name: "remove", Args: map[string]any{"path": "old.go"}}}
	if s := FormatToolGroupSummary(rem1, true); s != "Removing (old.go)" {
		t.Errorf("rem1 running: %q", s)
	}
	if s := FormatToolGroupSummary(rem1, false); s != "Removed (old.go)" {
		t.Errorf("rem1 finished: %q", s)
	}

	// 7. Fetch
	fetch1 := []api.ToolCall{{Name: "web_fetch", Args: map[string]any{"url": "https://go.dev"}}}
	if s := FormatToolGroupSummary(fetch1, true); s != "Fetching (https://go.dev)" {
		t.Errorf("fetch1 running: %q", s)
	}
	if s := FormatToolGroupSummary(fetch1, false); s != "Fetched (https://go.dev)" {
		t.Errorf("fetch1 finished: %q", s)
	}

	// 8. Custom
	cust1 := []api.ToolCall{{Name: "my_tool", Args: map[string]any{"name": "x"}}}
	if s := FormatToolGroupSummary(cust1, true); s != "Calling my_tool (x)" {
		t.Errorf("cust1 running: %q", s)
	}
	if s := FormatToolGroupSummary(cust1, false); s != "Called my_tool (x)" {
		t.Errorf("cust1 finished: %q", s)
	}
}

func TestToolCallAndGroupRendering(t *testing.T) {
	t.Run("RunningToolCall", func(t *testing.T) {
		out := renderNodeToText(ToolCall(ToolCallProps{
			ToolCall: api.ToolCall{Name: "bash", Args: map[string]any{"command": "go build"}},
			Running:  true,
		}))
		if !strings.Contains(out, "○ Running (go build)") {
			t.Errorf("expected '○ Running (go build)', got:\n%s", out)
		}
	})

	t.Run("FinishedToolCall", func(t *testing.T) {
		out := renderNodeToText(ToolCall(ToolCallProps{
			ToolCall: api.ToolCall{Name: "bash", Args: map[string]any{"command": "go build"}},
			Running:  false,
		}))
		if !strings.Contains(out, "● Ran (go build)") {
			t.Errorf("expected '● Ran (go build)', got:\n%s", out)
		}
	})

	t.Run("ToolCallWithError", func(t *testing.T) {
		out := renderNodeToText(ToolCall(ToolCallProps{
			ToolCall:  api.ToolCall{Name: "bash", Args: map[string]any{"command": "exit 1"}},
			Running:   false,
			ErrorText: "command failed with exit code 1\nextra details here",
		}))
		if !strings.Contains(out, "● Ran (exit 1)") {
			t.Errorf("expected '● Ran (exit 1)', got:\n%s", out)
		}
		if !strings.Contains(out, "⎿  ✗ command failed with exit code 1") {
			t.Errorf("expected indented error line, got:\n%s", out)
		}
		if strings.Contains(out, "extra details") {
			t.Errorf("expected only first line of error in compact error, got:\n%s", out)
		}
	})
}

func TestRoleToolMessageBehavior(t *testing.T) {
	t.Run("SuppressesNonErrorOutput", func(t *testing.T) {
		out := renderNodeToText(Message(MessageProps{
			Message: api.Message{
				Role:       api.RoleTool,
				ToolCallID: "tc1",
				IsError:    false,
				Content:    []api.Block{{Type: api.BlockText, Text: "file contents line 1\nline 2\nline 3"}},
			},
		}))
		if strings.TrimSpace(out) != "" {
			t.Errorf("expected empty output for non-error RoleTool message, got %q", out)
		}
	})

	t.Run("RendersErrorOutput", func(t *testing.T) {
		node := Message(MessageProps{
			Message: api.Message{
				Role:       api.RoleTool,
				ToolCallID: "tc1",
				IsError:    true,
				Content:    []api.Block{{Type: api.BlockText, Text: "permission denied: /etc/shadow\nmore info"}},
			},
		})
		out := renderNodeToText(node)
		if !strings.Contains(out, "⎿  ✗ permission denied: /etc/shadow") {
			t.Errorf("expected compact indented error line, got:\n%s", out)
		}
	})
}

func TestReasoningNarrowViewportKeepsClosingParenthesis(t *testing.T) {
	out := renderNodeToTextWithWidth(Reasoning(ReasoningProps{
		Text:    "this is a very long reasoning string that will definitely exceed thirty columns in total length",
		Running: true,
	}), 35)

	firstLine := strings.Split(strings.TrimSpace(out), "\n")[0]
	if !strings.HasSuffix(firstLine, ")") {
		t.Errorf("expected narrow reasoning line to end with ')', got: %q", firstLine)
	}
	if !strings.HasPrefix(firstLine, "○ Thinking (") {
		t.Errorf("expected narrow reasoning line to start with '○ Thinking (', got: %q", firstLine)
	}
}

func TestToolGroupNarrowViewportKeepsClosingParenthesis(t *testing.T) {
	out := renderNodeToTextWithWidth(ToolCall(ToolCallProps{
		ToolCall: api.ToolCall{Name: "bash", Args: map[string]any{"command": "very long command with lots of arguments that overflows"}},
		Running:  false,
	}), 35)

	firstLine := strings.Split(strings.TrimSpace(out), "\n")[0]
	if !strings.HasSuffix(firstLine, ")") {
		t.Errorf("expected narrow tool group line to end with ')', got: %q", firstLine)
	}
	if !strings.HasPrefix(firstLine, "● Ran (") {
		t.Errorf("expected narrow tool group line to start with '● Ran (', got: %q", firstLine)
	}
}

func TestRenderHistoryMessagesCoalescing(t *testing.T) {
	hist := []api.Message{
		{
			Role: api.RoleUser,
			Content: []api.Block{
				{Type: api.BlockText, Text: "Check status"},
			},
		},
		{
			Role: api.RoleAssistant,
			Content: []api.Block{
				{Type: api.BlockToolCall, ToolCall: &api.ToolCall{ID: "tc1", Name: "bash", Args: map[string]any{"command": "git status"}}},
			},
		},
		{
			Role:       api.RoleTool,
			ToolCallID: "tc1",
			Content:    []api.Block{{Type: api.BlockText, Text: "On branch main"}},
		},
		{
			Role: api.RoleAssistant,
			Content: []api.Block{
				{Type: api.BlockToolCall, ToolCall: &api.ToolCall{ID: "tc2", Name: "bash", Args: map[string]any{"command": "git diff"}}},
			},
		},
		{
			Role:       api.RoleTool,
			ToolCallID: "tc2",
			Content:    []api.Block{{Type: api.BlockText, Text: ""}},
		},
		{
			Role: api.RoleAssistant,
			Content: []api.Block{
				{Type: api.BlockText, Text: "Workspace is clean."},
			},
		},
	}

	nodes := RenderHistoryMessages(hist)
	var sb strings.Builder
	for _, n := range nodes {
		sb.WriteString(renderNodeToText(n))
		sb.WriteString("\n")
	}
	out := sb.String()

	if !strings.Contains(out, "● Ran 2 commands (+1 · git diff)") {
		t.Errorf("expected coalesced tool calls in history output, got:\n%s", out)
	}
	if !strings.Contains(out, "Workspace is clean.") {
		t.Errorf("expected assistant text in history output, got:\n%s", out)
	}
	if strings.Contains(out, "On branch main") {
		t.Errorf("expected raw tool output to be suppressed in history, got:\n%s", out)
	}
}

