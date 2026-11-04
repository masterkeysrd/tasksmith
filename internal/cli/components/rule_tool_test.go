package components

import (
	"strings"
	"testing"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
)

func TestLoadRuleToolWidget(t *testing.T) {
	call := &api.ToolCall{
		Name: "load_rule",
		Args: map[string]any{
			"rule": "go-error-handling",
		},
	}
	response := &api.Message{
		Name:              "load_rule",
		StructuredContent: []byte(`{"rule":"go-error-handling","path":".agents/rules/go-error-handling.rule.md","instructions":"secret-instructions"}`),
	}
	failure := &api.Message{
		Name: "load_rule", IsError: true,
		Content: []api.Block{{Type: api.BlockText, Text: "rule is not accessible\nadditional details"}},
	}

	for _, tc := range []struct {
		name      string
		tool      store.ToolPart
		want      string
		forbidden []string
	}{
		{"receiving", store.ToolPart{Call: call, Status: store.ToolReceiving}, "○ Loading Rule go-error-handling", nil},
		{"pending", store.ToolPart{Call: call, Status: store.ToolPending}, "○ Loading Rule go-error-handling", nil},
		{"running", store.ToolPart{Call: call, Status: store.ToolRunning}, "○ Loading Rule go-error-handling", nil},
		{"waiting", store.ToolPart{Call: call, Status: store.ToolWaitingPermission}, "○ Loading Rule go-error-handling (waiting for permission)", nil},
		{"interrupted", store.ToolPart{Call: call, Status: store.ToolInterrupted}, "○ Loading Rule go-error-handling (interrupted)", nil},
		{"succeeded", store.ToolPart{Call: call, Response: response, Status: store.ToolSucceeded}, "● Loaded Rule go-error-handling", []string{".agents/", "secret-instructions"}},
		{"truncated", store.ToolPart{Call: call, Response: &api.Message{Name: "load_rule", StructuredContent: []byte(`{"rule":"go-error-handling","instructions":"head","truncated":true}`)}, Status: store.ToolSucceeded}, "● Loaded Rule go-error-handling\n  ⎿  ⚠ Rule exceeds size limit (500 lines / 32 KB) and was truncated", nil},
		{"failed", store.ToolPart{Call: call, Response: failure, Status: store.ToolFailed}, "● Failed to load Rule go-error-handling\n  ⎿  ✗ rule is not accessible", []string{"additional details"}},
		{"error response", store.ToolPart{Call: call, Response: failure, Status: store.ToolSucceeded}, "● Failed to load Rule go-error-handling\n  ⎿  ✗ rule is not accessible", nil},
		{"missing name", store.ToolPart{Call: &api.ToolCall{Name: "load_rule"}, Status: store.ToolRunning}, "○ Loading Rule", nil},
		{"response only", store.ToolPart{Response: response, Status: store.ToolSucceeded}, "● Loaded Rule go-error-handling", nil},
		{"malformed output", store.ToolPart{Call: call, Response: &api.Message{Name: "load_rule", StructuredContent: []byte(`{`)}, Status: store.ToolSucceeded}, "● Loaded Rule go-error-handling", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			render := renderComponent(t, 100)
			got := render(MessageTool(MessageToolProps{Tool: tc.tool}))
			lines := strings.Split(got, "\n")
			for i := range lines {
				lines[i] = strings.TrimRight(lines[i], " ")
			}
			got = strings.Join(lines, "\n")
			if got != tc.want {
				t.Fatalf("rendered %q, want %q", got, tc.want)
			}
			for _, value := range tc.forbidden {
				if strings.Contains(got, value) {
					t.Fatalf("unexpected %q in %q", value, got)
				}
			}
		})
	}
}

func TestLoadRuleToolWidgetNarrow(t *testing.T) {
	render := renderComponent(t, 28)
	tool := store.ToolPart{
		Call:   &api.ToolCall{Name: "load_rule", Args: map[string]any{"rule": "a-very-long-rule-name-for-testing"}},
		Status: store.ToolSucceeded,
	}
	got := render(MessageTool(MessageToolProps{Tool: tool}))
	if !strings.Contains(got, "● Loaded Rule") || strings.Contains(got, "a-very-long-rule-name-for-testing") || strings.Contains(got, "\n") {
		t.Fatalf("unexpected narrow output: %q", got)
	}
}

func TestLoadRuleToolInTranscript(t *testing.T) {
	render := renderComponent(t, 100)
	tool := &store.ToolPart{
		Call:     &api.ToolCall{Name: "load_rule", Args: map[string]any{"rule": "go-error-handling"}},
		Response: &api.Message{Name: "load_rule", StructuredContent: []byte(`{"rule":"go-error-handling","instructions":"hidden"}`)},
		Status:   store.ToolSucceeded,
	}
	item := store.TranscriptItem{Kind: store.TranscriptKindMessage, Data: store.Message{
		ID: "assistant", Role: api.RoleAssistant,
		Parts: []store.MessagePart{{ID: "rule", Type: store.PartTool, Tool: tool}},
	}}
	got := render(renderTranscriptItem(item, 0))
	if !strings.Contains(got, "● Loaded Rule go-error-handling") || strings.Contains(got, "hidden") {
		t.Fatalf("unexpected transcript output: %q", got)
	}
}
