package components

import (
	"strings"
	"testing"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
)

func TestLoadSkillToolWidget(t *testing.T) {
	call := &api.ToolCall{
		Name: "load_skill",
		Args: map[string]any{
			"skill":     "postgres-migrations",
			"arguments": map[string]any{"token": "secret-argument"},
		},
	}
	response := &api.Message{
		Name:              "load_skill",
		StructuredContent: []byte(`{"skill":"postgres-migrations","path":".agents/skills/postgres-migrations","instructions":"secret-instructions"}`),
	}
	failure := &api.Message{
		Name: "load_skill", IsError: true,
		Content: []api.Block{{Type: api.BlockText, Text: "skill is not accessible\nadditional details"}},
	}

	for _, tc := range []struct {
		name      string
		tool      store.ToolPart
		want      string
		forbidden []string
	}{
		{"receiving", store.ToolPart{Call: call, Status: store.ToolReceiving}, "○ Loading Skill postgres-migrations", nil},
		{"pending", store.ToolPart{Call: call, Status: store.ToolPending}, "○ Loading Skill postgres-migrations", nil},
		{"running", store.ToolPart{Call: call, Status: store.ToolRunning}, "○ Loading Skill postgres-migrations", nil},
		{"waiting", store.ToolPart{Call: call, Status: store.ToolWaitingPermission}, "○ Loading Skill postgres-migrations (waiting for permission)", nil},
		{"interrupted", store.ToolPart{Call: call, Status: store.ToolInterrupted}, "○ Loading Skill postgres-migrations (interrupted)", nil},
		{"succeeded", store.ToolPart{Call: call, Response: response, Status: store.ToolSucceeded}, "● Loaded Skill postgres-migrations", []string{".agents/", "secret-instructions"}},
		{"failed", store.ToolPart{Call: call, Response: failure, Status: store.ToolFailed}, "● Failed to load Skill postgres-migrations\n  ⎿  ✗ skill is not accessible", []string{"additional details"}},
		{"error response", store.ToolPart{Call: call, Response: failure, Status: store.ToolSucceeded}, "● Failed to load Skill postgres-migrations\n  ⎿  ✗ skill is not accessible", nil},
		{"missing name", store.ToolPart{Call: &api.ToolCall{Name: "load_skill"}, Status: store.ToolRunning}, "○ Loading Skill", nil},
		{"response only", store.ToolPart{Response: response, Status: store.ToolSucceeded}, "● Loaded Skill postgres-migrations", nil},
		{"malformed output", store.ToolPart{Call: call, Response: &api.Message{Name: "load_skill", StructuredContent: []byte(`{`)}, Status: store.ToolSucceeded}, "● Loaded Skill postgres-migrations", nil},
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
			for _, value := range append(tc.forbidden, "secret-argument") {
				if strings.Contains(got, value) {
					t.Fatalf("unexpected %q in %q", value, got)
				}
			}
		})
	}
}

func TestLoadSkillToolWidgetNarrow(t *testing.T) {
	render := renderComponent(t, 28)
	tool := store.ToolPart{
		Call:   &api.ToolCall{Name: "load_skill", Args: map[string]any{"skill": "a-very-long-skill-name-for-testing"}},
		Status: store.ToolSucceeded,
	}
	got := render(MessageTool(MessageToolProps{Tool: tool}))
	if !strings.Contains(got, "● Loaded Skill") || strings.Contains(got, "a-very-long-skill-name-for-testing") || strings.Contains(got, "\n") {
		t.Fatalf("unexpected narrow output: %q", got)
	}
}

func TestLoadSkillToolInTranscript(t *testing.T) {
	render := renderComponent(t, 100)
	tool := &store.ToolPart{
		Call:     &api.ToolCall{Name: "load_skill", Args: map[string]any{"skill": "postgres-migrations"}},
		Response: &api.Message{Name: "load_skill", StructuredContent: []byte(`{"skill":"postgres-migrations","instructions":"hidden"}`)},
		Status:   store.ToolSucceeded,
	}
	item := store.TranscriptItem{Kind: store.TranscriptKindMessage, Data: store.Message{
		ID: "assistant", Role: api.RoleAssistant,
		Parts: []store.MessagePart{{ID: "skill", Type: store.PartTool, Tool: tool}},
	}}
	got := render(renderTranscriptItem(item, 0))
	if !strings.Contains(got, "● Loaded Skill postgres-migrations") || strings.Contains(got, "hidden") {
		t.Fatalf("unexpected transcript output: %q", got)
	}
}
