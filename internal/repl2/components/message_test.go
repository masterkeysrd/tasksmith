package components

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/repl2/store"
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

	// Bash tool running
	tool := store.ToolPart{
		ID:     "bash-1",
		Status: store.ToolRunning,
		Call: &api.ToolCall{
			ID:   "bash-1",
			Name: "bash",
			Args: map[string]any{"command": "go test ./..."},
		},
	}
	got := render(MessageTool(MessageToolProps{Tool: tool}))
	if !strings.Contains(got, "○ Running (go test ./...)") {
		t.Fatalf("unexpected bash running output: %q", got)
	}

	// Bash tool succeeded
	tool.Status = store.ToolSucceeded
	tool.Response = &api.Message{
		ID:         "resp-1",
		Role:       api.RoleTool,
		ToolCallID: "bash-1",
		Name:       "bash",
	}
	got = render(MessageTool(MessageToolProps{Tool: tool}))
	if !strings.Contains(got, "● Ran (go test ./...)") {
		t.Fatalf("unexpected bash completed output: %q", got)
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
}
