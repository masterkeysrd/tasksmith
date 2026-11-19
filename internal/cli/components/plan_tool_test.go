package components

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
)

func TestPlanToolWidgets(t *testing.T) {
	rawRender := renderComponent(t, 100)
	render := func(node trail.Node) string {
		lines := strings.Split(rawRender(node), "\n")
		for i := range lines {
			lines[i] = strings.TrimRight(lines[i], " ")
		}
		return strings.Join(lines, "\n")
	}

	enterRunning := store.ToolPart{
		Status: store.ToolRunning,
		Call:   &api.ToolCall{Name: "enter_plan_mode"},
	}
	if got := render(MessageTool(MessageToolProps{Tool: enterRunning})); got != "○ Entering plan mode" {
		t.Fatalf("enter running = %q", got)
	}

	enterDone := store.ToolPart{
		Status:   store.ToolSucceeded,
		Call:     &api.ToolCall{Name: "enter_plan_mode"},
		Response: &api.Message{StructuredContent: json.RawMessage(`{"mode":"plan","plans_dir":"/tmp/plans","instructions":"..."}`)},
	}
	if got := render(MessageTool(MessageToolProps{Tool: enterDone})); got != "● Entered plan mode" {
		t.Fatalf("enter done = %q", got)
	}

	exitApproved := store.ToolPart{
		Status:   store.ToolSucceeded,
		Call:     &api.ToolCall{Name: "exit_plan_mode", Args: map[string]any{"path": "plans/feat.md"}},
		Response: &api.Message{StructuredContent: json.RawMessage(`{"approved":true,"mode":"accept_edits","plan_path":"plans/feat.md"}`)},
	}
	gotApproved := render(MessageTool(MessageToolProps{Tool: exitApproved}))
	if !strings.Contains(gotApproved, "● Approved plan plans/feat.md") || !strings.Contains(gotApproved, "Switched to accept_edits mode") {
		t.Fatalf("exit approved = %q", gotApproved)
	}

	exitKeep := store.ToolPart{
		Status:   store.ToolSucceeded,
		Call:     &api.ToolCall{Name: "exit_plan_mode", Args: map[string]any{"path": "plans/feat.md"}},
		Response: &api.Message{StructuredContent: json.RawMessage(`{"approved":false,"mode":"plan","plan_path":"plans/feat.md","feedback":"Include tests"}`)},
	}
	gotKeep := render(MessageTool(MessageToolProps{Tool: exitKeep}))
	if !strings.Contains(gotKeep, "● Kept planning plans/feat.md") || !strings.Contains(gotKeep, "Include tests") {
		t.Fatalf("exit keep = %q", gotKeep)
	}
}
