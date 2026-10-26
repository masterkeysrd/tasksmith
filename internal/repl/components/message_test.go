package components

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/repl/store"
)

func TestMessageUserAndSystemLayouts(t *testing.T) {
	for _, test := range []struct {
		role     api.Role
		expected string
	}{
		{api.RoleUser, "> Keep **this** literal."},
		{api.RoleSystem, "[system] Keep **this** literal."},
	} {
		t.Run(string(test.role), func(t *testing.T) {
			render := renderComponent(t, 100)
			message := store.Message{
				ID:   "message",
				Role: test.role,
				Parts: []store.MessagePart{
					{
						ID:   "text",
						Type: store.PartText,
						Text: &store.TextPart{Text: "Keep **this** literal."},
					},
				},
			}
			if got := render(Message(MessageProps{Message: message})); got != test.expected {
				t.Fatalf("message = %q, want %q", got, test.expected)
			}
		})
	}
}

func TestMessageAssistantPreservesPartOrder(t *testing.T) {
	render := renderComponent(t, 100)
	message := store.Message{
		ID:   "assistant",
		Role: api.RoleAssistant,
		Parts: []store.MessagePart{
			{
				ID:   "text-1",
				Type: store.PartText,
				Text: &store.TextPart{Text: "**Before** the tool."},
			},
			{
				ID:   "tool",
				Type: store.PartTool,
				Tool: &store.ToolPart{
					ID:     "call",
					Status: store.ToolRunning,
					Call:   &api.ToolCall{ID: "call", Name: "view", Args: map[string]any{"path": "file.go"}},
				},
			},
			{
				ID:   "thought",
				Type: store.PartThought,
				Thought: &store.ThoughtPart{
					Text:      "Checking the output",
					Streaming: true,
					Signature: "not visible",
				},
			},
			{
				ID:   "text-2",
				Type: store.PartText,
				Text: &store.TextPart{Text: "After the tool."},
			},
		},
	}
	before, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	got := render(Message(MessageProps{Message: message}))
	end := 0
	for _, expected := range []string{
		"Before the tool.",
		"○ Reading \ue627 file.go",
		"○ Thinking (Checking the output)",
		"After the tool.",
	} {
		index := strings.Index(got[end:], expected)
		if index < 0 {
			t.Fatalf("missing or reordered %q in %q", expected, got)
		}
		end += index + len(expected)
	}
	if strings.Contains(got, "**") || strings.Contains(got, "not visible") {
		t.Fatalf("markdown markup or thought signature leaked into output: %q", got)
	}
	after, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("message rendering mutated the view model")
	}
}

func TestMessageToolGenericRenderers(t *testing.T) {
	render := renderComponent(t, 100)

	// Custom tool running
	tool := store.ToolPart{
		ID:     "custom-1",
		Status: store.ToolRunning,
		Call: &api.ToolCall{
			ID:   "custom-1",
			Name: "custom_deploy",
			Args: map[string]any{"name": "prod-v2"},
		},
	}
	got := render(MessageTool(MessageToolProps{Tool: tool}))
	if !strings.Contains(got, "○ Calling custom_deploy (prod-v2)") {
		t.Fatalf("unexpected custom tool running output: %q", got)
	}

	// Custom tool succeeded
	tool.Status = store.ToolSucceeded
	tool.Response = &api.Message{
		ID:         "resp-1",
		Role:       api.RoleTool,
		ToolCallID: "custom-1",
		Name:       "custom_deploy",
	}
	got = render(MessageTool(MessageToolProps{Tool: tool}))
	if !strings.Contains(got, "● Called custom_deploy (prod-v2)") {
		t.Fatalf("unexpected custom tool completed output: %q", got)
	}
}

func TestNoticeAndErrorBannerAndCommandOutput(t *testing.T) {
	render := renderComponent(t, 100)

	// Notice
	got := render(Notice(store.NoticeData{Text: "Switched workspace"}))
	if !strings.Contains(got, "Switched workspace") {
		t.Fatalf("unexpected notice output: %q", got)
	}

	// ErrorBanner
	got = render(ErrorBanner(store.ErrorData{Title: "Failure", Message: "Something broke"}))
	if !strings.Contains(got, "Failure: Something broke") {
		t.Fatalf("unexpected error banner output: %q", got)
	}

	// CommandOutput
	got = render(CommandOutput(store.CommandData{Command: "/mode", Output: "Switched to plan"}))
	if !strings.Contains(got, "> /mode") || !strings.Contains(got, "⎿") || !strings.Contains(got, "Switched to plan") {
		t.Fatalf("unexpected command output: %q", got)
	}

	// TimelineEvent
	got = render(TimelineEvent(api.TimelineEntry{
		Kind: api.TimelineKindMode,
		Mode: &api.ModeChange{From: api.ModeDefault, To: api.ModePlan},
	}))
	if !strings.Contains(got, "Mode set to plan") {
		t.Fatalf("unexpected timeline event output: %q", got)
	}

	got = render(TimelineEvent(api.TimelineEntry{
		Kind:  api.TimelineKindAgent,
		Agent: &api.AgentChange{To: "coder"},
	}))
	if !strings.Contains(got, "Agent set to coder") {
		t.Fatalf("unexpected agent timeline event output: %q", got)
	}

	got = render(TimelineEvent(api.TimelineEntry{
		Kind: api.TimelineKindInterrupted,
	}))
	if !strings.Contains(got, "Interrupted by user") {
		t.Fatalf("unexpected interrupted timeline event output: %q", got)
	}
}

func TestViewToolWidgetSucceededAndNarrowTruncation(t *testing.T) {
	viewOut := api.ViewOutput{
		Path:       "internal/repl/components/message_tools.go",
		StartLine:  10,
		EndLine:    29,
		TotalLines: 100,
	}
	outData, err := json.Marshal(viewOut)
	if err != nil {
		t.Fatal(err)
	}

	tool := store.ToolPart{
		ID:     "view-1",
		Status: store.ToolSucceeded,
		Call: &api.ToolCall{
			ID:   "view-1",
			Name: "view",
			Args: map[string]any{"path": "internal/repl/components/message_tools.go"},
		},
		Response: &api.Message{
			ID:                "resp-1",
			Role:              api.RoleTool,
			ToolCallID:        "view-1",
			Name:              "view",
			StructuredContent: outData,
		},
	}

	// 1. Wide render (width 120): full path and suffix visible
	renderWide := renderComponent(t, 120)
	gotWide := renderWide(MessageTool(MessageToolProps{Tool: tool}))
	if !strings.Contains(gotWide, "● Read") {
		t.Fatalf("expected '● Read', got %q", gotWide)
	}
	if !strings.Contains(gotWide, "internal/repl/components/message_tools.go") {
		t.Fatalf("expected full path, got %q", gotWide)
	}
	if !strings.Contains(gotWide, "(lines 10-29 of 100)") {
		t.Fatalf("expected line range suffix, got %q", gotWide)
	}

	// 2. Narrow render (width 60): directory prefix truncated with ellipsis, filename & suffix intact
	renderNarrow := renderComponent(t, 60)
	gotNarrow := renderNarrow(MessageTool(MessageToolProps{Tool: tool}))
	if !strings.Contains(gotNarrow, "● Read") {
		t.Fatalf("expected '● Read', got %q", gotNarrow)
	}
	if !strings.Contains(gotNarrow, "/message_tools.go") {
		t.Fatalf("expected filename '/message_tools.go' to remain intact, got %q", gotNarrow)
	}
	if !strings.Contains(gotNarrow, "(lines 10-29 of 100)") {
		t.Fatalf("expected suffix '(lines 10-29 of 100)' to remain intact, got %q", gotNarrow)
	}
	if !strings.Contains(gotNarrow, "…") {
		t.Fatalf("expected ellipsis '…' in truncated directory prefix, got %q", gotNarrow)
	}
}

func TestLsToolWidgetRunningAndSucceeded(t *testing.T) {
	// 1. Running state with recursive=true
	toolRunning := store.ToolPart{
		ID:     "ls-1",
		Status: store.ToolRunning,
		Call: &api.ToolCall{
			ID:   "ls-1",
			Name: "ls",
			Args: map[string]any{"path": "src", "recursive": true},
		},
	}
	render := renderComponent(t, 100)
	got := render(MessageTool(MessageToolProps{Tool: toolRunning}))
	if !strings.Contains(got, "○ Listing") || !strings.Contains(got, "src") || !strings.Contains(got, "(recursive)") {
		t.Fatalf("unexpected ls running output: %q", got)
	}

	// 2. Succeeded state with 11 entries, recursive, truncated
	lsOut := api.LsOutput{
		Path:      "src",
		Entries:   make([]api.LsEntry, 11),
		Truncated: true,
	}
	outData, err := json.Marshal(lsOut)
	if err != nil {
		t.Fatal(err)
	}
	toolSucceeded := store.ToolPart{
		ID:     "ls-2",
		Status: store.ToolSucceeded,
		Call: &api.ToolCall{
			ID:   "ls-2",
			Name: "ls",
			Args: map[string]any{"path": "src", "recursive": true},
		},
		Response: &api.Message{
			ID:                "resp-2",
			Role:              api.RoleTool,
			ToolCallID:        "ls-2",
			Name:              "ls",
			StructuredContent: outData,
		},
	}
	got = render(MessageTool(MessageToolProps{Tool: toolSucceeded}))
	if !strings.Contains(got, "● Listed") || !strings.Contains(got, "(11 entries, recursive, truncated)") {
		t.Fatalf("unexpected ls succeeded output: %q", got)
	}
}

func TestGlobToolWidget(t *testing.T) {
	globOut := api.GlobOutput{
		Matches: []string{"a.go", "b.go", "c.go"},
	}
	outData, err := json.Marshal(globOut)
	if err != nil {
		t.Fatal(err)
	}

	tool := store.ToolPart{
		ID:     "glob-1",
		Status: store.ToolSucceeded,
		Call: &api.ToolCall{
			ID:   "glob-1",
			Name: "glob",
			Args: map[string]any{"pattern": "*.go", "path": "src"},
		},
		Response: &api.Message{
			ID:                "resp-glob",
			Role:              api.RoleTool,
			ToolCallID:        "glob-1",
			Name:              "glob",
			StructuredContent: outData,
		},
	}
	render := renderComponent(t, 100)
	got := render(MessageTool(MessageToolProps{Tool: tool}))
	if !strings.Contains(got, "● Found *.go in") || !strings.Contains(got, "src") || !strings.Contains(got, "(3 files)") {
		t.Fatalf("unexpected glob output: %q", got)
	}

	// Single file match
	globOutSingle := api.GlobOutput{Matches: []string{"a.go"}}
	tool.Response.StructuredContent, _ = json.Marshal(globOutSingle)
	got = render(MessageTool(MessageToolProps{Tool: tool}))
	if !strings.Contains(got, "(1 file)") {
		t.Fatalf("expected '(1 file)', got %q", got)
	}
}

func TestGrepToolWidget(t *testing.T) {
	grepOut := api.GrepOutput{
		Matches:   make([]api.GrepMatch, 8),
		Truncated: true,
	}
	outData, err := json.Marshal(grepOut)
	if err != nil {
		t.Fatal(err)
	}

	tool := store.ToolPart{
		ID:     "grep-1",
		Status: store.ToolSucceeded,
		Call: &api.ToolCall{
			ID:   "grep-1",
			Name: "grep",
			Args: map[string]any{"pattern": "TODO", "include": "*.go", "path": "pkg"},
		},
		Response: &api.Message{
			ID:                "resp-grep",
			Role:              api.RoleTool,
			ToolCallID:        "grep-1",
			Name:              "grep",
			StructuredContent: outData,
		},
	}
	render := renderComponent(t, 100)
	got := render(MessageTool(MessageToolProps{Tool: tool}))
	if !strings.Contains(got, "● Searched \"TODO\" (*.go) in") || !strings.Contains(got, "pkg") || !strings.Contains(got, "(8 matches, truncated)") {
		t.Fatalf("unexpected grep output: %q", got)
	}

	// 0 matches
	grepOutZero := api.GrepOutput{Matches: []api.GrepMatch{}}
	tool.Response.StructuredContent, _ = json.Marshal(grepOutZero)
	got = render(MessageTool(MessageToolProps{Tool: tool}))
	if !strings.Contains(got, "(0 matches)") {
		t.Fatalf("expected '(0 matches)', got %q", got)
	}
}

func TestFailedToolCallErrorSubline(t *testing.T) {
	tool := store.ToolPart{
		ID:     "view-fail",
		Status: store.ToolFailed,
		Call: &api.ToolCall{
			ID:   "view-fail",
			Name: "view",
			Args: map[string]any{"path": "missing.txt"},
		},
		Response: &api.Message{
			ID:         "resp-fail",
			Role:       api.RoleTool,
			ToolCallID: "view-fail",
			Name:       "view",
			IsError:    true,
			Content: []api.Block{
				{Type: api.BlockText, Text: "open missing.txt: no such file or directory\ngoroutine stack trace..."},
			},
		},
	}
	render := renderComponent(t, 100)
	got := render(MessageTool(MessageToolProps{Tool: tool}))
	if !strings.Contains(got, "● Read") || !strings.Contains(got, "missing.txt") {
		t.Fatalf("expected header line, got %q", got)
	}
	if !strings.Contains(got, "  ⎿  ✗ open missing.txt: no such file or directory") {
		t.Fatalf("expected error sub-line, got %q", got)
	}
	if strings.Contains(got, "goroutine stack trace") {
		t.Fatalf("expected only first line of error in sub-line, got %q", got)
	}
}

func TestWriteToolWidget(t *testing.T) {
	render := renderComponent(t, 100)

	// Running
	toolRunning := store.ToolPart{
		ID:     "write-1",
		Status: store.ToolRunning,
		Call: &api.ToolCall{
			ID:   "write-1",
			Name: "write",
			Args: map[string]any{"path": "pkg/newfile.go"},
		},
	}
	got := render(MessageTool(MessageToolProps{Tool: toolRunning}))
	if !strings.Contains(got, "○ Writing") || !strings.Contains(got, "newfile.go") {
		t.Fatalf("unexpected write running output: %q", got)
	}

	// Succeeded (+15 -3)
	out := api.WriteOutput{
		Path:      "pkg/newfile.go",
		Additions: 15,
		Deletions: 3,
	}
	outData, _ := json.Marshal(out)
	toolSucceeded := store.ToolPart{
		ID:     "write-2",
		Status: store.ToolSucceeded,
		Call:   toolRunning.Call,
		Response: &api.Message{
			ID:                "resp-w",
			Role:              api.RoleTool,
			ToolCallID:        "write-2",
			Name:              "write",
			StructuredContent: outData,
		},
	}
	got = render(MessageTool(MessageToolProps{Tool: toolSucceeded}))
	if !strings.Contains(got, "● Wrote") || !strings.Contains(got, "(+15 -3)") {
		t.Fatalf("unexpected write succeeded output: %q", got)
	}

	// Succeeded additions only (+42)
	outAdd := api.WriteOutput{Path: "pkg/newfile.go", Additions: 42}
	toolSucceeded.Response.StructuredContent, _ = json.Marshal(outAdd)
	got = render(MessageTool(MessageToolProps{Tool: toolSucceeded}))
	if !strings.Contains(got, "(+42)") {
		t.Fatalf("expected '(+42)', got %q", got)
	}

	// Succeeded deletions only (-7)
	outDel := api.WriteOutput{Path: "pkg/newfile.go", Deletions: 7}
	toolSucceeded.Response.StructuredContent, _ = json.Marshal(outDel)
	got = render(MessageTool(MessageToolProps{Tool: toolSucceeded}))
	if !strings.Contains(got, "(-7)") {
		t.Fatalf("expected '(-7)', got %q", got)
	}
}

func TestEditToolWidget(t *testing.T) {
	render := renderComponent(t, 100)

	// Running
	toolRunning := store.ToolPart{
		ID:     "edit-1",
		Status: store.ToolRunning,
		Call: &api.ToolCall{
			ID:   "edit-1",
			Name: "edit",
			Args: map[string]any{"path": "src/main.go"},
		},
	}
	got := render(MessageTool(MessageToolProps{Tool: toolRunning}))
	if !strings.Contains(got, "○ Editing") || !strings.Contains(got, "main.go") {
		t.Fatalf("unexpected edit running output: %q", got)
	}

	// Succeeded (+3 -2) with 1 replacement
	outSingle := api.EditOutput{
		Path:         "src/main.go",
		Replacements: 1,
		Additions:    3,
		Deletions:    2,
	}
	outData, _ := json.Marshal(outSingle)
	toolSucceeded := store.ToolPart{
		ID:     "edit-2",
		Status: store.ToolSucceeded,
		Call:   toolRunning.Call,
		Response: &api.Message{
			ID:                "resp-e",
			Role:              api.RoleTool,
			ToolCallID:        "edit-2",
			Name:              "edit",
			StructuredContent: outData,
		},
	}
	got = render(MessageTool(MessageToolProps{Tool: toolSucceeded}))
	if !strings.Contains(got, "● Edited") || !strings.Contains(got, "(+3 -2)") || strings.Contains(got, "replacements") {
		t.Fatalf("unexpected single replacement output: %q", got)
	}

	// Succeeded (+10 -5, 4 replacements)
	outMulti := api.EditOutput{
		Path:         "src/main.go",
		Replacements: 4,
		Additions:    10,
		Deletions:    5,
	}
	toolSucceeded.Response.StructuredContent, _ = json.Marshal(outMulti)
	got = render(MessageTool(MessageToolProps{Tool: toolSucceeded}))
	if !strings.Contains(got, "(+10 -5, 4 replacements)") {
		t.Fatalf("expected '(+10 -5, 4 replacements)', got %q", got)
	}

	// Replacements only without line count changes
	outReplOnly := api.EditOutput{
		Path:         "src/main.go",
		Replacements: 3,
	}
	toolSucceeded.Response.StructuredContent, _ = json.Marshal(outReplOnly)
	got = render(MessageTool(MessageToolProps{Tool: toolSucceeded}))
	if !strings.Contains(got, "(3 replacements)") {
		t.Fatalf("expected '(3 replacements)', got %q", got)
	}
}

func TestRemoveToolWidget(t *testing.T) {
	render := renderComponent(t, 100)

	// Running
	toolRunning := store.ToolPart{
		ID:     "rm-1",
		Status: store.ToolRunning,
		Call: &api.ToolCall{
			ID:   "rm-1",
			Name: "remove",
			Args: map[string]any{"path": "temp.txt"},
		},
	}
	got := render(MessageTool(MessageToolProps{Tool: toolRunning}))
	if !strings.Contains(got, "○ Removing") || !strings.Contains(got, "temp.txt") {
		t.Fatalf("unexpected remove running output: %q", got)
	}

	// Succeeded (-25)
	out := api.RemoveOutput{
		Path:      "temp.txt",
		Deletions: 25,
	}
	outData, _ := json.Marshal(out)
	toolSucceeded := store.ToolPart{
		ID:     "rm-2",
		Status: store.ToolSucceeded,
		Call:   toolRunning.Call,
		Response: &api.Message{
			ID:                "resp-rm",
			Role:              api.RoleTool,
			ToolCallID:        "rm-2",
			Name:              "remove",
			StructuredContent: outData,
		},
	}
	got = render(MessageTool(MessageToolProps{Tool: toolSucceeded}))
	if !strings.Contains(got, "● Removed") || !strings.Contains(got, "(-25)") {
		t.Fatalf("unexpected remove succeeded output: %q", got)
	}
}

func TestBashToolWidget(t *testing.T) {
	render := renderComponent(t, 100)

	// Running with Description and Dir
	toolRunning := store.ToolPart{
		ID:     "bash-1",
		Status: store.ToolRunning,
		Call: &api.ToolCall{
			ID:   "bash-1",
			Name: "bash",
			Args: map[string]any{
				"command":     "npm test",
				"description": "Run frontend tests",
				"dir":         "frontend",
			},
		},
	}
	got := render(MessageTool(MessageToolProps{Tool: toolRunning}))
	if !strings.Contains(got, "○ Running Run frontend tests") {
		t.Fatalf("expected running header with description, got %q", got)
	}
	if !strings.Contains(got, "⎿") || !strings.Contains(got, "npm test") || !strings.Contains(got, "(in frontend)") {
		t.Fatalf("expected command line with dir, got %q", got)
	}

	// Succeeded with duration (1.5s)
	outSucc := api.BashOutput{
		DurationMs: 1500,
		ExitCode:   0,
		Status:     "completed",
	}
	outData, _ := json.Marshal(outSucc)
	toolSucceeded := store.ToolPart{
		ID:     "bash-2",
		Status: store.ToolSucceeded,
		Call:   toolRunning.Call,
		Response: &api.Message{
			ID:                "resp-b",
			Role:              api.RoleTool,
			ToolCallID:        "bash-2",
			Name:              "bash",
			StructuredContent: outData,
		},
	}
	got = render(MessageTool(MessageToolProps{Tool: toolSucceeded}))
	if !strings.Contains(got, "● Ran Run frontend tests") || !strings.Contains(got, "(1.5s)") {
		t.Fatalf("unexpected bash succeeded output: %q", got)
	}

	// Failed exit code (exit 1, 250ms)
	outFail := api.BashOutput{
		DurationMs: 250,
		ExitCode:   1,
		Status:     "failed",
	}
	toolSucceeded.Response.StructuredContent, _ = json.Marshal(outFail)
	got = render(MessageTool(MessageToolProps{Tool: toolSucceeded}))
	if !strings.Contains(got, "(exit 1, 250ms)") {
		t.Fatalf("expected exit code and duration, got %q", got)
	}

	// Background task running
	outBg := api.BashOutput{
		TaskID: "task-bg-123",
		Status: "running",
	}
	toolSucceeded.Response.StructuredContent, _ = json.Marshal(outBg)
	got = render(MessageTool(MessageToolProps{Tool: toolSucceeded}))
	if !strings.Contains(got, "○ Running Run frontend tests") || !strings.Contains(got, "(background: task-bg-123)") {
		t.Fatalf("expected background task output, got %q", got)
	}

	// Multi-line description and command sanitization
	toolMultiline := store.ToolPart{
		ID:     "bash-3",
		Status: store.ToolRunning,
		Call: &api.ToolCall{
			ID:   "bash-3",
			Name: "bash",
			Args: map[string]any{
				"command":     "echo 1\necho 2",
				"description": "Multi\nline desc",
			},
		},
	}
	got = render(MessageTool(MessageToolProps{Tool: toolMultiline}))
	if !strings.Contains(got, "○ Running Multi line desc") {
		t.Fatalf("expected sanitized multi-line description, got %q", got)
	}
	if !strings.Contains(got, "echo 1 echo 2") {
		t.Fatalf("expected sanitized multi-line command, got %q", got)
	}
}

func TestWebFetchToolWidget(t *testing.T) {
	render := renderComponent(t, 100)

	// Running
	toolRunning := store.ToolPart{
		ID:     "wf-1",
		Status: store.ToolRunning,
		Call: &api.ToolCall{
			ID:   "wf-1",
			Name: "web_fetch",
			Args: map[string]any{"url": "https://example.com/api/data"},
		},
	}
	got := render(MessageTool(MessageToolProps{Tool: toolRunning}))
	if !strings.Contains(got, "○ Fetching") || !strings.Contains(got, "https://example.com/api/data") {
		t.Fatalf("unexpected web_fetch running output: %q", got)
	}

	// Succeeded with status and content-type
	out := api.WebFetchOutput{
		URL:         "https://example.com/api/data",
		StatusCode:  200,
		ContentType: "application/json",
	}
	outData, _ := json.Marshal(out)
	toolSucceeded := store.ToolPart{
		ID:     "wf-2",
		Status: store.ToolSucceeded,
		Call:   toolRunning.Call,
		Response: &api.Message{
			ID:                "resp-wf",
			Role:              api.RoleTool,
			ToolCallID:        "wf-2",
			Name:              "web_fetch",
			StructuredContent: outData,
		},
	}
	got = render(MessageTool(MessageToolProps{Tool: toolSucceeded}))
	if !strings.Contains(got, "● Fetched") || !strings.Contains(got, "(200, application/json)") {
		t.Fatalf("unexpected web_fetch succeeded output: %q", got)
	}

	// Status code only
	outCodeOnly := api.WebFetchOutput{StatusCode: 204}
	toolSucceeded.Response.StructuredContent, _ = json.Marshal(outCodeOnly)
	got = render(MessageTool(MessageToolProps{Tool: toolSucceeded}))
	if !strings.Contains(got, "(204)") {
		t.Fatalf("expected '(204)', got %q", got)
	}

	// HTTP 404 (error status)
	outErr := api.WebFetchOutput{StatusCode: 404}
	toolSucceeded.Response.StructuredContent, _ = json.Marshal(outErr)
	got = render(MessageTool(MessageToolProps{Tool: toolSucceeded}))
	if !strings.Contains(got, "(404)") {
		t.Fatalf("expected '(404)', got %q", got)
	}

	// Content-type with charset stripped
	outCharset := api.WebFetchOutput{
		StatusCode:  200,
		ContentType: "text/html; charset=utf-8",
	}
	toolSucceeded.Response.StructuredContent, _ = json.Marshal(outCharset)
	got = render(MessageTool(MessageToolProps{Tool: toolSucceeded}))
	if !strings.Contains(got, "(200, text/html)") {
		t.Fatalf("expected '(200, text/html)', got %q", got)
	}
}

func TestDownloadToolWidget(t *testing.T) {
	render := renderComponent(t, 100)

	// Running
	toolRunning := store.ToolPart{
		ID:     "dl-1",
		Status: store.ToolRunning,
		Call: &api.ToolCall{
			ID:   "dl-1",
			Name: "download",
			Args: map[string]any{"path": "build/binary.tar.gz"},
		},
	}
	got := render(MessageTool(MessageToolProps{Tool: toolRunning}))
	if !strings.Contains(got, "○ Downloading") || !strings.Contains(got, "binary.tar.gz") {
		t.Fatalf("unexpected download running output: %q", got)
	}

	// Succeeded
	out := api.DownloadOutput{
		Path:         "build/binary.tar.gz",
		BytesWritten: 2 * 1024 * 1024,
	}
	outData, _ := json.Marshal(out)
	toolSucceeded := store.ToolPart{
		ID:     "dl-2",
		Status: store.ToolSucceeded,
		Call:   toolRunning.Call,
		Response: &api.Message{
			ID:                "resp-dl",
			Role:              api.RoleTool,
			ToolCallID:        "dl-2",
			Name:              "download",
			StructuredContent: outData,
		},
	}
	got = render(MessageTool(MessageToolProps{Tool: toolSucceeded}))
	if !strings.Contains(got, "● Downloaded") || !strings.Contains(got, "(2 MB)") {
		t.Fatalf("unexpected download succeeded output: %q", got)
	}
}

func TestTasksToolWidget(t *testing.T) {
	render := renderComponent(t, 100)

	// Action "list" - running & succeeded
	toolList := store.ToolPart{
		ID:     "tasks-1",
		Status: store.ToolRunning,
		Call: &api.ToolCall{
			ID:   "tasks-1",
			Name: "tasks",
			Args: map[string]any{"action": "list"},
		},
	}
	got := render(MessageTool(MessageToolProps{Tool: toolList}))
	if !strings.Contains(got, "○ Listing tasks") {
		t.Fatalf("expected '○ Listing tasks', got %q", got)
	}

	outList := api.TasksOutput{Tasks: make([]api.TasksTask, 3)}
	outData, _ := json.Marshal(outList)
	toolList.Status = store.ToolSucceeded
	toolList.Response = &api.Message{
		ID:                "resp-t-1",
		Role:              api.RoleTool,
		ToolCallID:        "tasks-1",
		Name:              "tasks",
		StructuredContent: outData,
	}
	got = render(MessageTool(MessageToolProps{Tool: toolList}))
	if !strings.Contains(got, "● Listed tasks (3 tasks)") {
		t.Fatalf("expected '● Listed tasks (3 tasks)', got %q", got)
	}

	// Action "status"
	toolStatus := store.ToolPart{
		ID:     "tasks-2",
		Status: store.ToolSucceeded,
		Call: &api.ToolCall{
			ID:   "tasks-2",
			Name: "tasks",
			Args: map[string]any{"action": "status", "task_id": "t-101"},
		},
		Response: &api.Message{
			ID:                "resp-t-2",
			Role:              api.RoleTool,
			ToolCallID:        "tasks-2",
			Name:              "tasks",
			StructuredContent: []byte(`{"status":"running"}`),
		},
	}
	got = render(MessageTool(MessageToolProps{Tool: toolStatus}))
	if !strings.Contains(got, "● Checked task t-101 (running)") {
		t.Fatalf("expected checked task output, got %q", got)
	}

	// Action "kill"
	toolKill := store.ToolPart{
		ID:     "tasks-3",
		Status: store.ToolSucceeded,
		Call: &api.ToolCall{
			ID:   "tasks-3",
			Name: "tasks",
			Args: map[string]any{"action": "kill", "task_id": "t-101"},
		},
		Response: &api.Message{
			ID:                "resp-t-3",
			Role:              api.RoleTool,
			ToolCallID:        "tasks-3",
			Name:              "tasks",
			StructuredContent: []byte(`{"status":"canceled"}`),
		},
	}
	got = render(MessageTool(MessageToolProps{Tool: toolKill}))
	if !strings.Contains(got, "● Killed task t-101 (canceled)") {
		t.Fatalf("expected killed task output, got %q", got)
	}

	// Action "send_input"
	toolInput := store.ToolPart{
		ID:     "tasks-4",
		Status: store.ToolSucceeded,
		Call: &api.ToolCall{
			ID:   "tasks-4",
			Name: "tasks",
			Args: map[string]any{"action": "send_input", "task_id": "t-101"},
		},
		Response: &api.Message{
			ID:         "resp-t-4",
			Role:       api.RoleTool,
			ToolCallID: "tasks-4",
			Name:       "tasks",
		},
	}
	got = render(MessageTool(MessageToolProps{Tool: toolInput}))
	if !strings.Contains(got, "● Sent input to t-101") {
		t.Fatalf("expected sent input output, got %q", got)
	}
}
