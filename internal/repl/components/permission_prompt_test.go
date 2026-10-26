package components

import (
	"strings"
	"testing"

	"github.com/masterkeysrd/kite/backend"
	"github.com/masterkeysrd/kite/backend/mock"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/repl/store"
)

func TestPermissionPromptRendering(t *testing.T) {
	store.ResetApp()
	defer store.ResetApp()

	req := api.PermissionRequest{
		ID:            "req-1",
		ToolCallID:    "call-1",
		ToolName:      "bash",
		Action:        api.ActionExec,
		Safety:        api.SafetyOutside,
		Target:        "git status",
		Summary:       "Execute git status",
		Diff:          "--- old\n+++ new\n@@ -1 +1 @@\n-old\n+new\n",
		GrantPatterns: []string{"bash(git status *)", "bash(git *)"},
	}

	b := mock.New(80, 24)
	app := trail.SimpleFC("TestPromptApp", func() kitex.Node {
		return PermissionPrompt(PermissionPromptProps{
			Request: req,
		})
	})

	inst, err := trail.New(app(), trail.WithBackend(b))
	if err != nil {
		t.Fatalf("trail.New: %v", err)
	}
	defer inst.Unmount()
	inst.Frame()

	out := surfaceText(b)

	// Check title and subtitle
	if !strings.Contains(out, "Permission required") {
		t.Errorf("expected 'Permission required' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "bash · git status") {
		t.Errorf("expected 'bash · git status' subtitle in output, got:\n%s", out)
	}

	// Check unified diff
	if !strings.Contains(out, "old") || !strings.Contains(out, "new") {
		t.Errorf("expected Diff in output, got:\n%s", out)
	}

	// Check option rows
	if !strings.Contains(out, "Yes, allow once") {
		t.Errorf("expected 'Yes, allow once' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "Always allow bash(git status *)") || !strings.Contains(out, "[session]") {
		t.Errorf("expected 'Always allow bash(git status *)' with [session] in output, got:\n%s", out)
	}
	if !strings.Contains(out, "Always allow bash(git *)") {
		t.Errorf("expected 'Always allow bash(git *)' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "No, deny") {
		t.Errorf("expected 'No, deny' in output, got:\n%s", out)
	}

	// Check KeyboardHint
	if !strings.Contains(out, "Keyboard:") || !strings.Contains(out, "↑/↓ Navigate") || !strings.Contains(out, "tab Scope") {
		t.Errorf("expected KeyboardHint in output, got:\n%s", out)
	}
}

func TestPermissionPromptTabScopeCycle(t *testing.T) {
	store.ResetApp()
	defer store.ResetApp()

	req := api.PermissionRequest{
		ID:            "req-1",
		ToolCallID:    "call-1",
		ToolName:      "bash",
		GrantPatterns: []string{"bash(go *)"},
	}

	b := mock.New(80, 24)
	app := trail.SimpleFC("TestPromptApp", func() kitex.Node {
		return PermissionPrompt(PermissionPromptProps{
			Request: req,
		})
	})

	inst, err := trail.New(app(), trail.WithBackend(b))
	if err != nil {
		t.Fatalf("trail.New: %v", err)
	}
	defer inst.Unmount()
	inst.Frame()

	// Initial scope: session
	out := surfaceText(b)
	if !strings.Contains(out, "[session]") {
		t.Errorf("expected [session] initially, got:\n%s", out)
	}

	// Press Tab -> project
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyTab}})
	inst.Frame()
	out = surfaceText(b)
	if !strings.Contains(out, "[project]") {
		t.Errorf("expected [project] after first Tab, got:\n%s", out)
	}

	// Press Tab -> global
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyTab}})
	inst.Frame()
	out = surfaceText(b)
	if !strings.Contains(out, "[global]") {
		t.Errorf("expected [global] after second Tab, got:\n%s", out)
	}

	// Press Tab -> session
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyTab}})
	inst.Frame()
	out = surfaceText(b)
	if !strings.Contains(out, "[session]") {
		t.Errorf("expected [session] after third Tab, got:\n%s", out)
	}
}

func TestPermissionPromptKeyShortcuts(t *testing.T) {
	store.ResetApp()
	defer store.ResetApp()

	req := api.PermissionRequest{
		ID:            "req-1",
		ToolCallID:    "call-1",
		ToolName:      "bash",
		GrantPatterns: []string{"bash(go *)"},
	}

	// 1. Press 'y' -> DecisionAllowOnce
	var dec1 api.PermissionDecision
	b := mock.New(80, 24)
	app1 := trail.SimpleFC("App1", func() kitex.Node {
		return PermissionPrompt(PermissionPromptProps{
			Request: req,
			OnDecision: func(_ api.PermissionRequest, dec api.PermissionDecision) {
				dec1 = dec
			},
		})
	})
	inst1, _ := trail.New(app1(), trail.WithBackend(b))
	inst1.Frame()
	inst1.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: 'y', Text: "y"}})
	if dec1.Kind != api.DecisionAllowOnce {
		t.Errorf("expected DecisionAllowOnce on 'y', got %v", dec1)
	}
	inst1.Unmount()

	// 2. Cycle to project scope, press 'a' -> DecisionAllowSession with scope project
	var dec2 api.PermissionDecision
	b2 := mock.New(80, 24)
	app2 := trail.SimpleFC("App2", func() kitex.Node {
		return PermissionPrompt(PermissionPromptProps{
			Request: req,
			OnDecision: func(_ api.PermissionRequest, dec api.PermissionDecision) {
				dec2 = dec
			},
		})
	})
	inst2, _ := trail.New(app2(), trail.WithBackend(b2))
	inst2.Frame()
	// Tab to project
	inst2.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyTab}})
	inst2.Frame()
	inst2.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: 'a', Text: "a"}})
	if dec2.Kind != api.DecisionAllowSession || dec2.GrantPattern != "bash(go *)" || dec2.Scope != api.ScopeProject {
		t.Errorf("expected DecisionAllowSession with scope project on 'a', got %+v", dec2)
	}
	inst2.Unmount()

	// 3. Press 'n' -> DecisionReject
	var dec3 api.PermissionDecision
	b3 := mock.New(80, 24)
	app3 := trail.SimpleFC("App3", func() kitex.Node {
		return PermissionPrompt(PermissionPromptProps{
			Request: req,
			OnDecision: func(_ api.PermissionRequest, dec api.PermissionDecision) {
				dec3 = dec
			},
		})
	})
	inst3, _ := trail.New(app3(), trail.WithBackend(b3))
	inst3.Frame()
	inst3.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: 'n', Text: "n"}})
	if dec3.Kind != api.DecisionReject {
		t.Errorf("expected DecisionReject on 'n', got %v", dec3)
	}
	inst3.Unmount()
}

func TestPermissionPromptNavigationAndEnter(t *testing.T) {
	store.ResetApp()
	defer store.ResetApp()

	req := api.PermissionRequest{
		ID:            "req-1",
		ToolCallID:    "call-1",
		ToolName:      "bash",
		GrantPatterns: []string{"bash(git status *)", "bash(git *)"},
	}

	// 1. Navigate down to second pattern row and press Enter
	var dec1 api.PermissionDecision
	b := mock.New(80, 24)
	app1 := trail.SimpleFC("App1", func() kitex.Node {
		return PermissionPrompt(PermissionPromptProps{
			Request: req,
			OnDecision: func(_ api.PermissionRequest, dec api.PermissionDecision) {
				dec1 = dec
			},
		})
	})
	inst1, _ := trail.New(app1(), trail.WithBackend(b))
	inst1.Frame()

	// Down from row 0 to row 1 (pattern 1)
	inst1.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyDown}})
	inst1.Frame()
	// Down from row 1 to row 2 (pattern 2: bash(git *))
	inst1.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyDown}})
	inst1.Frame()
	// Press Enter
	inst1.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyEnter}})
	if dec1.Kind != api.DecisionAllowSession || dec1.GrantPattern != "bash(git *)" || dec1.Scope != api.ScopeSession {
		t.Errorf("expected pattern 2 selected via Enter, got %+v", dec1)
	}
	inst1.Unmount()

	// 2. Navigate down to Deny row and press Enter
	var dec2 api.PermissionDecision
	b2 := mock.New(80, 24)
	app2 := trail.SimpleFC("App2", func() kitex.Node {
		return PermissionPrompt(PermissionPromptProps{
			Request: req,
			OnDecision: func(_ api.PermissionRequest, dec api.PermissionDecision) {
				dec2 = dec
			},
		})
	})
	inst2, _ := trail.New(app2(), trail.WithBackend(b2))
	inst2.Frame()

	// Down 3 times to Deny row (row 3)
	for range 3 {
		inst2.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyDown}})
		inst2.Frame()
	}
	// Up 1 time and Down 1 time to test Up navigation
	inst2.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyUp}})
	inst2.Frame()
	inst2.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyDown}})
	inst2.Frame()
	inst2.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyEnter}})
	if dec2.Kind != api.DecisionReject {
		t.Errorf("expected DecisionReject on Deny row Enter, got %+v", dec2)
	}
	inst2.Unmount()
}

func TestPermissionPromptFeedbackMode(t *testing.T) {
	store.ResetApp()
	defer store.ResetApp()

	req := api.PermissionRequest{
		ID:            "req-1",
		ToolCallID:    "call-1",
		ToolName:      "bash",
		GrantPatterns: []string{"bash(go *)"},
	}

	b := mock.New(80, 24)
	app := trail.SimpleFC("App", func() kitex.Node {
		return PermissionPrompt(PermissionPromptProps{
			Request: req,
		})
	})
	inst, _ := trail.New(app(), trail.WithBackend(b))
	inst.Frame()

	// Initially feedback mode is closed
	if strings.Contains(surfaceText(b), "Feedback:") {
		t.Errorf("did not expect Feedback: input initially")
	}

	// Press 'r' -> opens feedback mode
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: 'r', Text: "r"}})
	inst.Frame()

	txt := surfaceText(b)
	if !strings.Contains(txt, "Feedback:") {
		t.Errorf("expected Feedback: input on surface after 'r', got:\n%s", txt)
	}
	if !strings.Contains(txt, "enter Submit") || !strings.Contains(txt, "esc Cancel") {
		t.Errorf("expected feedback KeyboardHint on surface, got:\n%s", txt)
	}

	// Press Escape -> exits feedback mode
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyEscape}})
	inst.Frame()

	txt = surfaceText(b)
	if strings.Contains(txt, "Feedback:") {
		t.Errorf("expected Feedback: input to be dismissed after Escape, got:\n%s", txt)
	}
	if !strings.Contains(txt, "↑/↓ Navigate") {
		t.Errorf("expected standard KeyboardHint restored after Escape, got:\n%s", txt)
	}
}
