package components

import (
	"strings"
	"testing"

	"github.com/masterkeysrd/kite/backend/mock"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/tui/testutils"
)

func TestSlotRender(t *testing.T) {
	store.ClearSlot()
	defer store.ClearSlot()

	// When no active slot item, Slot node is instantiated
	node := Slot()
	if node == nil {
		t.Fatal("expected Slot() component node to not be nil")
	}

	kinds := []store.SlotKind{
		store.SlotKindPermission,
		store.SlotKindQuestion,
		store.SlotKindPlanReview,
		store.SlotKindPicker,
		store.SlotKindAutocomplete,
		store.SlotKind("unknown"),
	}

	for _, kind := range kinds {
		t.Run(string(kind), func(t *testing.T) {
			store.ClearSlot()
			store.EnqueueSlot(store.SlotItem{
				ID:   "test-item",
				Kind: kind,
				Data: "data",
			})
			n := Slot()
			if n == nil {
				t.Fatalf("expected Slot() component node to not be nil for kind %s", kind)
			}
		})
	}
}

func TestSlotRendersPermissionPrompt(t *testing.T) {
	store.ClearSlot()
	defer store.ClearSlot()

	req := api.PermissionRequest{
		ID:            "p1",
		ToolName:      "bash",
		Target:        "ls -la",
		Summary:       "List directory",
		Action:        api.ActionExec,
		Safety:        api.SafetyOutside,
		GrantPatterns: []string{"bash(ls *)"},
	}

	store.EnqueueSlot(store.SlotItem{
		ID:   "perm-p1",
		Kind: store.SlotKindPermission,
		Data: req,
	})

	b := mock.New(80, 24)
	app := trail.SimpleFC("TestSlotApp", func() kitex.Node {
		return Slot()
	})

	inst, err := trail.New(app(), trail.WithBackend(b))
	if err != nil {
		t.Fatalf("trail.New: %v", err)
	}
	defer inst.Unmount()
	inst.Frame()

	out := testutils.SurfaceText(b)
	if !strings.Contains(out, "Permission required") {
		t.Errorf("expected 'Permission required' in rendered Slot output, got:\n%s", out)
	}
	if !strings.Contains(out, "bash · ls -la") {
		t.Errorf("expected 'bash · ls -la' in rendered Slot output, got:\n%s", out)
	}
	if !strings.Contains(out, "Yes, allow once") {
		t.Errorf("expected 'Yes, allow once' in rendered Slot output, got:\n%s", out)
	}
	if !strings.Contains(out, "Keyboard:") {
		t.Errorf("expected KeyboardHint in rendered Slot output, got:\n%s", out)
	}
}

func TestSlotAcceptEditsOption(t *testing.T) {
	for _, tt := range []struct {
		name   string
		mode   api.PermissionMode
		action api.ActionType
		safety api.SafetyLevel
		bypass bool
		want   bool
	}{
		{"default", api.ModeDefault, api.ActionWrite, api.SafetySafe, false, true},
		{"plan", api.ModePlan, api.ActionWrite, api.SafetySafe, false, true},
		{"ask outside write", api.ModeAsk, api.ActionWrite, api.SafetyOutside, false, true},
		{"accept edits", api.ModeAcceptEdits, api.ActionWrite, api.SafetySafe, false, false},
		{"auto", api.ModeAuto, api.ActionWrite, api.SafetySafe, false, false},
		{"command", api.ModeDefault, api.ActionExec, api.SafetySafe, false, false},
		{"unsafe write", api.ModeDefault, api.ActionWrite, api.SafetyUnsafe, false, false},
		{"sandbox bypass", api.ModeDefault, api.ActionWrite, api.SafetySafe, true, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			store.ResetSession()
			store.ClearSlot()
			t.Cleanup(store.ResetSession)
			t.Cleanup(store.ClearSlot)
			store.SetSessionMode(tt.mode)
			store.EnqueueSlot(store.SlotItem{
				ID:   "perm-write",
				Kind: store.SlotKindPermission,
				Data: api.PermissionRequest{
					ID:            "write",
					ToolName:      "write",
					Action:        tt.action,
					Safety:        tt.safety,
					BypassSandbox: tt.bypass,
				},
			})

			out := testutils.NewApp(t, Slot(), 100, 24).Text()
			for _, text := range []string{"Yes, and switch to accept_edits", "e Accept edits"} {
				if strings.Contains(out, text) != tt.want {
					t.Errorf("want %q present=%t, got:\n%s", text, tt.want, out)
				}
			}
		})
	}
}

func TestSlotRendersPlanReviewPrompt(t *testing.T) {
	store.ClearSlot()
	defer store.ClearSlot()

	req := samplePlanReviewRequest()
	store.EnqueueSlot(store.SlotItem{
		ID:   "plan_review-" + req.ID,
		Kind: store.SlotKindPlanReview,
		Data: req,
	})

	b := mock.New(90, 30)
	app := trail.SimpleFC("TestSlotPlanApp", func() kitex.Node {
		return Slot()
	})
	inst, err := trail.New(app(), trail.WithBackend(b))
	if err != nil {
		t.Fatalf("trail.New: %v", err)
	}
	defer inst.Unmount()
	inst.Frame()

	out := testutils.SurfaceText(b)
	if !strings.Contains(out, "Plan review · plans/feature.md") || !strings.Contains(out, "Approve & auto-accept edits") {
		t.Errorf("expected PlanReviewPrompt in rendered Slot output, got:\n%s", out)
	}
}
