package components

import (
	"strings"
	"testing"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
)

func TestToolSearchToolWidget(t *testing.T) {
	call := &api.ToolCall{Name: "tool_search", Args: map[string]any{"query": "download a file"}}
	matches := &api.Message{
		Name:              "tool_search",
		StructuredContent: []byte(`{"tools":["download","web_fetch"]}`),
	}
	single := &api.Message{
		Name:              "tool_search",
		StructuredContent: []byte(`{"tools":["download"]}`),
	}
	empty := &api.Message{
		Name:              "tool_search",
		StructuredContent: []byte(`{"tools":[]}`),
	}
	failure := &api.Message{
		Name: "tool_search", IsError: true,
		Content: []api.Block{{Type: api.BlockText, Text: "search index unavailable\nadditional details"}},
	}

	for _, tc := range []struct {
		name      string
		tool      store.ToolPart
		want      string
		forbidden []string
	}{
		{"running", store.ToolPart{Call: call, Status: store.ToolRunning}, `○ Searching tools "download a file"`, nil},
		{"waiting", store.ToolPart{Call: call, Status: store.ToolWaitingPermission}, `○ Searching tools "download a file" (waiting for permission)`, nil},
		{"interrupted", store.ToolPart{Call: call, Status: store.ToolInterrupted}, `○ Searching tools "download a file" (interrupted)`, nil},
		{"found two", store.ToolPart{Call: call, Response: matches, Status: store.ToolSucceeded}, `● Searched tools "download a file" (2 tools: download, web_fetch)`, nil},
		{"found one", store.ToolPart{Call: call, Response: single, Status: store.ToolSucceeded}, `● Searched tools "download a file" (1 tool: download)`, nil},
		{"no matches", store.ToolPart{Call: call, Response: empty, Status: store.ToolSucceeded}, `● Searched tools "download a file" (no matches)`, nil},
		{"failed", store.ToolPart{Call: call, Response: failure, Status: store.ToolFailed}, `● Failed to search tools "download a file"
  ⎿  ✗ search index unavailable`, []string{"additional details"}},
		{"error response", store.ToolPart{Call: call, Response: failure, Status: store.ToolSucceeded}, `● Failed to search tools "download a file"
  ⎿  ✗ search index unavailable`, nil},
		{"malformed output", store.ToolPart{Call: call, Response: &api.Message{Name: "tool_search", StructuredContent: []byte(`{`)}, Status: store.ToolSucceeded}, `● Searched tools "download a file" (no matches)`, nil},
		{"missing query", store.ToolPart{Call: &api.ToolCall{Name: "tool_search"}, Status: store.ToolRunning}, "○ Searching tools", nil},
		{"response only", store.ToolPart{Response: matches, Status: store.ToolSucceeded}, "● Searched tools (2 tools: download, web_fetch)", nil},
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

func TestToolSearchToolWidgetNarrow(t *testing.T) {
	render := renderComponent(t, 30)
	tool := store.ToolPart{
		Call:     &api.ToolCall{Name: "tool_search", Args: map[string]any{"query": "render a long list of matches"}},
		Response: &api.Message{Name: "tool_search", StructuredContent: []byte(`{"tools":["github_create_branch","github_create_pull_request","github_delete_file"]}`)},
		Status:   store.ToolSucceeded,
	}
	got := render(MessageTool(MessageToolProps{Tool: tool}))
	if !strings.Contains(got, "● Searched tools") || strings.Contains(got, "github_delete_file") || strings.Contains(got, "\n") {
		t.Fatalf("unexpected narrow output: %q", got)
	}
}

func TestToolSearchToolInTranscript(t *testing.T) {
	render := renderComponent(t, 100)
	tool := &store.ToolPart{
		Call:     &api.ToolCall{Name: "tool_search", Args: map[string]any{"query": "search the web"}},
		Response: &api.Message{Name: "tool_search", StructuredContent: []byte(`{"tools":["web_fetch"]}`)},
		Status:   store.ToolSucceeded,
	}
	item := store.TranscriptItem{Kind: store.TranscriptKindMessage, Data: store.Message{
		ID: "assistant", Role: api.RoleAssistant,
		Parts: []store.MessagePart{{ID: "search", Type: store.PartTool, Tool: tool}},
	}}
	got := render(renderTranscriptItem(item, 0))
	if !strings.Contains(got, `● Searched tools "search the web" (1 tool: web_fetch)`) {
		t.Fatalf("unexpected transcript output: %q", got)
	}
}
