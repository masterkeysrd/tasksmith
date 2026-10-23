package components

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/tools/builtin"
	"github.com/masterkeysrd/tasksmith/internal/ui/hooks"
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
			render := renderMessagePart(t, 100)
			message := hooks.Message{ID: "message", Role: test.role, Parts: []hooks.MessagePart{{
				ID: "text", Type: hooks.PartText, Text: &hooks.TextPart{Text: "Keep **this** literal."},
			}}}
			if got := render(Message(MessageProps{Message: message})); got != test.expected {
				t.Fatalf("message = %q, want %q", got, test.expected)
			}
		})
	}
}

func TestMessageAssistantPreservesPartOrder(t *testing.T) {
	render := renderMessagePart(t, 100)
	message := hooks.Message{ID: "assistant", Role: api.RoleAssistant, Parts: []hooks.MessagePart{
		{ID: "text-1", Type: hooks.PartText, Text: &hooks.TextPart{Text: "**Before** the tool."}},
		{ID: "tool", Type: hooks.PartTool, Tool: &hooks.ToolPart{
			ID: "call", Status: hooks.ToolRunning,
			Call: &api.ToolCall{ID: "call", Name: "view", Args: map[string]any{"path": "file.go"}},
		}},
		{ID: "thought", Type: hooks.PartThought, Thought: &hooks.ThoughtPart{
			Text: "Checking the output", Streaming: true, Signature: "not visible",
		}},
		{ID: "text-2", Type: hooks.PartText, Text: &hooks.TextPart{Text: "After the tool."}},
	}}
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
	nodes := messageParts(message)
	for i, part := range message.Parts {
		if nodes[i].Key() != part.ID {
			t.Errorf("part %d key = %q, want %q", i, nodes[i].Key(), part.ID)
		}
	}
}

func TestMessageUpdatesStreamingParts(t *testing.T) {
	render := renderMessagePart(t, 100)
	message := hooks.Message{ID: "assistant", Role: api.RoleAssistant, Streaming: true, Parts: []hooks.MessagePart{
		{ID: "thought", Type: hooks.PartThought, Thought: &hooks.ThoughtPart{Text: "Inspecting", Streaming: true}},
		{ID: "text", Type: hooks.PartText, Text: &hooks.TextPart{Text: "A partial answer"}},
	}}
	if got := render(Message(MessageProps{Message: message})); !strings.Contains(got, "○ Thinking (Inspecting)") ||
		!strings.Contains(got, "A partial answer") {
		t.Fatalf("streaming message = %q", got)
	}
	message.Streaming = false
	message.Parts[0].Thought = &hooks.ThoughtPart{Text: "Inspected"}
	message.Parts[1].Text = &hooks.TextPart{Text: "The **complete** answer"}
	got := render(Message(MessageProps{Message: message}))
	if !strings.Contains(got, "● Thought (Inspected)") || !strings.Contains(got, "The complete answer") ||
		strings.Contains(got, "partial") {
		t.Fatalf("final message = %q", got)
	}
}

func TestMessageRendersUnmatchedToolResponse(t *testing.T) {
	render := renderMessagePart(t, 100)
	response := viewResponse(t, builtin.ViewOutput{Path: "orphan.go", StartLine: 1, EndLine: 2})
	response.Name = "view"
	message := hooks.Message{ID: "result", Role: api.RoleTool, Parts: []hooks.MessagePart{{
		ID: "tool", Type: hooks.PartTool, Tool: &hooks.ToolPart{
			ID: "call", Response: response, Status: hooks.ToolSucceeded,
		},
	}}}
	if got := render(Message(MessageProps{Message: message})); got != "● Read \ue627 orphan.go (lines 1-2)" {
		t.Fatalf("unmatched tool response = %q", got)
	}
	if message.Role != api.RoleTool || message.Parts[0].Tool.Call != nil {
		t.Fatal("unmatched result was given a fabricated author or call")
	}
}

func TestMessageEmptyPartsHaveNoChrome(t *testing.T) {
	for _, role := range []api.Role{api.RoleUser, api.RoleAssistant, api.RoleSystem, api.RoleTool} {
		t.Run(string(role), func(t *testing.T) {
			render := renderMessagePart(t, 100)
			message := hooks.Message{ID: "empty", Role: role}
			if got := render(Message(MessageProps{Message: message})); got != "" {
				t.Fatalf("empty message = %q", got)
			}
			message.Parts = []hooks.MessagePart{
				{ID: "text", Type: hooks.PartText, Text: &hooks.TextPart{Text: " \n\t "}},
				{ID: "thought", Type: hooks.PartThought, Thought: &hooks.ThoughtPart{Text: " \n "}},
			}
			if got := render(Message(MessageProps{Message: message})); got != "" {
				t.Fatalf("blank message = %q", got)
			}
		})
	}
}

func TestMessageNarrowWidthPreservesText(t *testing.T) {
	for _, test := range []struct {
		role   api.Role
		prefix string
	}{
		{api.RoleUser, "> "},
		{api.RoleAssistant, ""},
		{api.RoleSystem, "[system] "},
	} {
		t.Run(string(test.role), func(t *testing.T) {
			render := renderMessagePart(t, 24)
			text := "A message that must wrap without losing words."
			message := hooks.Message{ID: "message", Role: test.role, Parts: []hooks.MessagePart{{
				ID: "text", Type: hooks.PartText, Text: &hooks.TextPart{Text: text},
			}}}
			got := render(Message(MessageProps{Message: message}))
			if !strings.HasPrefix(got, test.prefix) || !strings.Contains(got, "\n") {
				t.Fatalf("message did not retain its prefix or wrap: %q", got)
			}
			if strings.Join(strings.Fields(got), " ") != test.prefix+text {
				t.Fatalf("wrapping lost message text: %q", got)
			}
		})
	}
}

func TestMessageReportsUnsupportedKinds(t *testing.T) {
	render := renderMessagePart(t, 100)
	if got := render(Message(MessageProps{Message: hooks.Message{Role: "unknown"}})); !strings.Contains(got, `unsupported role "unknown"`) {
		t.Fatalf("unsupported role was hidden: %q", got)
	}
	message := hooks.Message{ID: "message", Role: api.RoleAssistant, Parts: []hooks.MessagePart{{
		ID: "unknown", Type: "unknown",
	}}}
	if got := render(Message(MessageProps{Message: message})); !strings.Contains(got, `unsupported type "unknown"`) {
		t.Fatalf("unsupported part was hidden: %q", got)
	}
}
