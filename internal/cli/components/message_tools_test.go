package components

import (
	"strings"
	"testing"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/tui/testutils"
)

func TestToolCardAutoApprovedSucceeded(t *testing.T) {
	tool := store.ToolPart{
		ID:     "tc-1",
		Status: store.ToolSucceeded,
		Call: &api.ToolCall{
			ID:   "tc-1",
			Name: "bash",
			Args: map[string]any{
				"description": "Clean build dir",
				"command":     "rm -rf build",
			},
		},
		Response: &api.Message{
			ID:                "m-1",
			Role:              api.RoleTool,
			ToolCallID:        "tc-1",
			Name:              "bash",
			StructuredContent: []byte(`{"exit_code":0,"duration_ms":35,"status":"completed"}`),
			PermissionReview: &api.PermissionReview{
				RequestID:     "req-1",
				Source:        "decision_model",
				Verdict:       "approve_once",
				Model:         "typesafe/jev-latest",
				DecisionScope: "system",
				RubricDigest:  "deadbeef",
				Probability:   0.96,
				Threshold:     0.85,
				LatencyMs:     42,
			},
		},
	}

	out := testutils.Render(t, BashToolWidget(BashToolWidgetProps{Tool: tool}), 120, 24)
	for _, want := range []string{
		"●",
		"Ran",
		"Clean build dir",
		"[Auto-approved]",
		"  ⎿  ",
		"rm -rf build",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in rendered auto-approved card:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Reviewer:") || strings.Contains(out, "  ├  ") {
		t.Fatalf("succeeded bash card must render single ⎿ subline without Reviewer row:\n%s", out)
	}
}

func TestToolCardAutoApprovedFailedKeepsExitStatusBadgeAndTreeConnectors(t *testing.T) {
	tool := store.ToolPart{
		ID:     "tc-2",
		Status: store.ToolFailed,
		Call: &api.ToolCall{
			ID:   "tc-2",
			Name: "bash",
			Args: map[string]any{
				"description": "Build binary",
				"command":     "go build ./...",
			},
		},
		Response: &api.Message{
			ID:                "m-2",
			Role:              api.RoleTool,
			ToolCallID:        "tc-2",
			Name:              "bash",
			IsError:           true,
			Content:           []api.Block{{Type: api.BlockText, Text: "cmd/main.go:12: undefined: run"}},
			StructuredContent: []byte(`{"exit_code":1,"duration_ms":1200,"status":"failed"}`),
			PermissionReview: &api.PermissionReview{
				RequestID:     "req-2",
				Source:        "decision_model",
				Verdict:       "approve_once",
				Model:         "openai/gpt-5-mini",
				DecisionScope: "workspace",
				RubricDigest:  "abcd1234",
				Probability:   0.97,
				Threshold:     0.85,
				LatencyMs:     65,
			},
		},
	}

	out := testutils.Render(t, BashToolWidget(BashToolWidgetProps{Tool: tool}), 120, 24)
	for _, want := range []string{
		"● Ran Build binary (exit 1, 1.2s) [Auto-approved]",
		"  ├  ",
		"go build ./...",
		"  ⎿  ✗ cmd/main.go:12: undefined: run",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in failed auto-approved card:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Reviewer:") {
		t.Fatalf("failed card must not render separate Reviewer subline:\n%s", out)
	}
}

func TestToolCardHumanApprovedAndLegacyOmitBadge(t *testing.T) {
	humanApproved := store.ToolPart{
		ID:     "tc-3",
		Status: store.ToolSucceeded,
		Call: &api.ToolCall{
			ID:   "tc-3",
			Name: "write",
			Args: map[string]any{"path": "/tmp/outside.txt"},
		},
		Response: &api.Message{
			ID:         "m-3",
			Role:       api.RoleTool,
			ToolCallID: "tc-3",
			Name:       "write",
		},
	}
	outHuman := testutils.Render(t, WriteToolWidget(WriteToolWidgetProps{Tool: humanApproved}), 100, 24)
	if strings.Contains(outHuman, "[Auto-approved]") || strings.Contains(outHuman, "Reviewer:") {
		t.Fatalf("human-approved card must not show auto-approved badge:\n%s", outHuman)
	}

	legacyInFlight := store.ToolPart{
		ID:     "tc-4",
		Status: store.ToolRunning,
		Call: &api.ToolCall{
			ID:   "tc-4",
			Name: "write",
			Args: map[string]any{"path": "foo.txt"},
		},
	}
	outLegacy := testutils.Render(t, WriteToolWidget(WriteToolWidgetProps{Tool: legacyInFlight}), 100, 24)
	if strings.Contains(outLegacy, "[Auto-approved]") || strings.Contains(outLegacy, "Reviewer:") {
		t.Fatalf("in-flight/legacy card without response must not show auto-approved badge:\n%s", outLegacy)
	}
}

func TestToolCardAutoApprovedFileToolSingleLine(t *testing.T) {
	tool := store.ToolPart{
		ID:     "tc-5",
		Status: store.ToolSucceeded,
		Call: &api.ToolCall{
			ID:   "tc-5",
			Name: "remove",
			Args: map[string]any{"path": "stale.txt"},
		},
		Response: &api.Message{
			ID:         "m-5",
			Role:       api.RoleTool,
			ToolCallID: "tc-5",
			Name:       "remove",
			PermissionReview: &api.PermissionReview{
				RequestID:     "req-5",
				Source:        "decision_model",
				Verdict:       "approve_once",
				Model:         "typesafe/jev-latest",
				DecisionScope: "system",
				RubricDigest:  "cafebabe",
				Probability:   0.96,
				Threshold:     0.85,
				LatencyMs:     30,
			},
		},
	}

	out := strings.TrimRight(testutils.Render(t, RemoveToolWidget(RemoveToolWidgetProps{Tool: tool}), 110, 24), "\n")
	if strings.Contains(out, "\n") || strings.Contains(out, "⎿") || strings.Contains(out, "├") {
		t.Fatalf("auto-approved file tool should stay on a single line:\n%s", out)
	}
	if !strings.Contains(out, "[Auto-approved]") {
		t.Fatalf("missing [Auto-approved] badge:\n%s", out)
	}
}

func TestTimelineNoticeHostAccessAutoApproved(t *testing.T) {
	entry := api.TimelineEntry{
		ID:   "tl-net-1",
		Kind: api.TimelineKindPermission,
		Permission: &api.PermissionRecord{
			Request: api.PermissionRequest{
				ID:       "perm-net-1",
				ToolName: "net",
				Action:   api.ActionNet,
				Target:   "registry.npmjs.org",
			},
			Decision: api.PermissionDecision{RequestID: "perm-net-1", Kind: api.DecisionAllowOnce},
			Review: &api.PermissionReview{
				RequestID:     "perm-net-1",
				Source:        "decision_model",
				Verdict:       "approve_once",
				Model:         "typesafe/jev-latest",
				DecisionScope: "system",
				Probability:   0.98,
				Threshold:     0.85,
			},
		},
	}

	out := testutils.Render(t, TimelineEvent(entry), 100, 10)
	if !strings.Contains(out, "Host access auto-approved · registry.npmjs.org (typesafe/jev-latest)") {
		t.Fatalf("unexpected net auto-approval notice:\n%s", out)
	}

	// Regular permission entries without net+review render nothing.
	humanEntry := api.TimelineEntry{
		ID:   "tl-perm-human",
		Kind: api.TimelineKindPermission,
		Permission: &api.PermissionRecord{
			Request:  api.PermissionRequest{ID: "p-h", ToolName: "bash"},
			Decision: api.PermissionDecision{RequestID: "p-h", Kind: api.DecisionAllowOnce},
		},
	}
	outHuman := testutils.Render(t, TimelineEvent(humanEntry), 100, 10)
	if strings.TrimSpace(outHuman) != "" {
		t.Fatalf("human permission entry should render empty, got:\n%s", outHuman)
	}
}

func TestToolCardReadsToolPartPermissionReview(t *testing.T) {
	tool := store.ToolPart{
		ID:     "tc-plan-1",
		Status: store.ToolSucceeded,
		Call: &api.ToolCall{
			ID:   "tc-plan-1",
			Name: "exit_plan_mode",
			Args: map[string]any{"path": "plans/feat.md"},
		},
		PermissionReview: &api.PermissionReview{
			RequestID:     "req-plan-1",
			Source:        "decision_model",
			Verdict:       "approve_once",
			Model:         "typesafe/jev-latest",
			DecisionScope: "system",
			RubricDigest:  "faceb00c",
			Probability:   0.95,
			Threshold:     0.85,
			LatencyMs:     1500,
		},
		Response: &api.Message{
			ID:                "m-plan-1",
			Role:              api.RoleTool,
			ToolCallID:        "tc-plan-1",
			Name:              "exit_plan_mode",
			StructuredContent: []byte(`{"approved":true,"mode":"accept_edits","plan_path":"plans/feat.md"}`),
		},
	}

	out := testutils.Render(t, PlanToolRenderer(tool), 120, 24)
	for _, want := range []string{
		"Approved plan plans/feat.md [Auto-approved]",
		"  ⎿  Switched to accept_edits mode",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in PlanToolRenderer output:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Reviewer:") {
		t.Fatalf("PlanToolRenderer must not render separate Reviewer subline:\n%s", out)
	}
}
