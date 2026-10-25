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
	"github.com/masterkeysrd/tasksmith/internal/repl2/store"
)

func TestPermissionConfigEditorScreen1Scope(t *testing.T) {
	store.ResetApp()
	defer store.ResetApp()

	var closed bool
	b := mock.New(100, 24)
	app := trail.SimpleFC("TestApp", func() kitex.Node {
		return PermissionConfigEditor(PermissionConfigEditorProps{
			Permissions: api.PermissionsResponse{
				Project: api.Permissions{Allow: []string{"bash(go *)"}},
				Global:  api.Permissions{Deny: []string{"bash(rm *)"}},
			},
			OnClose: func() {
				closed = true
			},
		})
	})

	inst, err := trail.New(app(), trail.WithBackend(b))
	if err != nil {
		t.Fatalf("trail.New: %v", err)
	}
	defer inst.Unmount()
	inst.Frame()

	out := surfaceText(b)
	if !strings.Contains(out, "Permission Config Editor") {
		t.Errorf("expected title in output: %s", out)
	}
	if !strings.Contains(out, "Select a config scope to edit:") {
		t.Errorf("expected subtitle in output: %s", out)
	}
	if !strings.Contains(out, "❯ Project") || !strings.Contains(out, "Global") {
		t.Errorf("expected Project and Global in output: %s", out)
	}
	if !strings.Contains(out, "Rules that apply only to this project (highest priority)") {
		t.Errorf("expected Project description in output: %s", out)
	}
	if !strings.Contains(out, "Keyboard:") || !strings.Contains(out, "↑/↓ Navigate") || !strings.Contains(out, "enter Select") || !strings.Contains(out, "esc Close") {
		t.Errorf("expected KeyboardHint on Screen 1: %s", out)
	}

	// Press Down -> Global selected, description updates
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyDown}})
	inst.Frame()
	out = surfaceText(b)
	if !strings.Contains(out, "❯ Global") {
		t.Errorf("expected Global selected after Down: %s", out)
	}
	if !strings.Contains(out, "Rules that apply across all projects") {
		t.Errorf("expected Global description in output: %s", out)
	}

	// Press Up -> Project selected again
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyUp}})
	inst.Frame()
	out = surfaceText(b)
	if !strings.Contains(out, "❯ Project") {
		t.Errorf("expected Project selected after Up: %s", out)
	}

	// Press Esc -> triggers OnClose
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyEscape}})
	inst.Frame()
	if !closed {
		t.Errorf("expected OnClose to be called on Esc in Screen 1")
	}
}

func TestPermissionConfigEditorScreen2RulesAndTabs(t *testing.T) {
	store.ResetApp()
	defer store.ResetApp()

	var removedRule string
	var removedScope api.PermissionScope
	var removedEffect api.PermissionEffect

	b := mock.New(100, 24)
	app := trail.SimpleFC("TestApp", func() kitex.Node {
		return PermissionConfigEditor(PermissionConfigEditorProps{
			Permissions: api.PermissionsResponse{
				Project: api.Permissions{
					Allow: []string{"bash(go *)", "edit(internal/**)"},
					Deny:  []string{"bash(rm *)"},
				},
			},
			OnRemoveRule: func(scope api.PermissionScope, effect api.PermissionEffect, rule string) {
				removedScope = scope
				removedEffect = effect
				removedRule = rule
			},
		})
	})

	inst, err := trail.New(app(), trail.WithBackend(b))
	if err != nil {
		t.Fatalf("trail.New: %v", err)
	}
	defer inst.Unmount()
	inst.Frame()

	// Enter from Screen 1 into Screen 2 (Project scope)
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyEnter}})
	inst.Frame()

	out := surfaceText(b)
	if !strings.Contains(out, "Permissions — Project") {
		t.Errorf("expected Permissions — Project header: %s", out)
	}
	if !strings.Contains(out, "allowlist (2)") || !strings.Contains(out, "denylist (1)") {
		t.Errorf("expected tab counts: %s", out)
	}
	if !strings.Contains(out, "bash(go *)") || !strings.Contains(out, "edit(internal/**)") {
		t.Errorf("expected allowlist rules: %s", out)
	}
	if !strings.Contains(out, "Keyboard:") || !strings.Contains(out, "←/→ Switch View") || !strings.Contains(out, "a Add rule") || !strings.Contains(out, "d/⌫ Delete rule") {
		t.Errorf("expected KeyboardHint on Screen 2: %s", out)
	}

	// Switch tabs with Right -> denylist
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyRight}})
	inst.Frame()
	out = surfaceText(b)
	if !strings.Contains(out, "bash(rm *)") {
		t.Errorf("expected denylist rules: %s", out)
	}

	// Switch tabs back with Left -> allowlist
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyLeft}})
	inst.Frame()
	out = surfaceText(b)
	if !strings.Contains(out, "bash(go *)") {
		t.Errorf("expected allowlist rules again: %s", out)
	}

	// Delete rule using 'd' on first rule
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: 'd', Text: "d"}})
	inst.Frame()
	if removedScope != api.ScopeProject || removedEffect != api.EffectAllow || removedRule != "bash(go *)" {
		t.Errorf("expected OnRemoveRule for bash(go *), got scope=%s effect=%s rule=%s", removedScope, removedEffect, removedRule)
	}

	// Esc returns to Screen 1
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyEscape}})
	inst.Frame()
	out = surfaceText(b)
	if !strings.Contains(out, "Permission Config Editor") {
		t.Errorf("expected Screen 1 after Esc: %s", out)
	}
}

func TestPermissionConfigEditorScreen3AddAndEditRule(t *testing.T) {
	store.ResetApp()
	defer store.ResetApp()

	var addedScope api.PermissionScope
	var addedEffect api.PermissionEffect
	var addedRule string

	var editedScope api.PermissionScope
	var editedEffect api.PermissionEffect
	var editedOldRule, editedNewRule string

	b := mock.New(100, 24)
	app := trail.SimpleFC("TestApp", func() kitex.Node {
		return PermissionConfigEditor(PermissionConfigEditorProps{
			Permissions: api.PermissionsResponse{
				Project: api.Permissions{
					Allow: []string{"bash(go *)"},
				},
			},
			OnAddRule: func(scope api.PermissionScope, effect api.PermissionEffect, rule string) {
				addedScope = scope
				addedEffect = effect
				addedRule = rule
			},
			OnEditRule: func(scope api.PermissionScope, effect api.PermissionEffect, oldRule, newRule string) {
				editedScope = scope
				editedEffect = effect
				editedOldRule = oldRule
				editedNewRule = newRule
			},
		})
	})

	inst, err := trail.New(app(), trail.WithBackend(b))
	if err != nil {
		t.Fatalf("trail.New: %v", err)
	}
	defer inst.Unmount()
	inst.Frame()

	// Enter to Screen 2
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyEnter}})
	inst.Frame()

	// Press 'a' to enter Screen 3 Add Mode
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: 'a', Text: "a"}})
	inst.Frame()

	out := surfaceText(b)
	if !strings.Contains(out, "Add Rule — Project — allowlist") {
		t.Errorf("expected Add Rule title: %s", out)
	}
	if !strings.Contains(out, "Enter a permission rule:") {
		t.Errorf("expected Enter a permission rule subtitle: %s", out)
	}
	if !strings.Contains(out, "Format: tool(pattern)") {
		t.Errorf("expected format hint: %s", out)
	}
	if !strings.Contains(out, "Keyboard:") || !strings.Contains(out, "enter Save") || !strings.Contains(out, "esc Cancel") {
		t.Errorf("expected KeyboardHint on Screen 3: %s", out)
	}

	// Press Enter without text -> validation error
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyEnter}})
	inst.Frame()
	out = surfaceText(b)
	if !strings.Contains(out, "Rule must not be empty") {
		t.Errorf("expected empty rule error: %s", out)
	}

	// Type an invalid rule
	for _, r := range "badrule" {
		inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: r, Text: string(r)}})
	}
	inst.Frame()
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyEnter}})
	inst.Frame()
	out = surfaceText(b)
	if !strings.Contains(out, "Invalid rule format: expected tool(pattern)") {
		t.Errorf("expected invalid format error: %s", out)
	}

	// Clear and type a valid rule: "bash(test *)"
	// First Esc to cancel back to Screen 2
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyEscape}})
	inst.Frame()
	out = surfaceText(b)
	if !strings.Contains(out, "Permissions — Project") {
		t.Errorf("expected return to Screen 2: %s", out)
	}

	// Re-open Add Mode with 'a'
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: 'a', Text: "a"}})
	inst.Frame()
	for _, r := range "bash(test *)" {
		inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: r, Text: string(r)}})
	}
	inst.Frame()
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyEnter}})
	inst.Frame()

	if addedScope != api.ScopeProject || addedEffect != api.EffectAllow || addedRule != "bash(test *)" {
		t.Errorf("expected OnAddRule called, got scope=%s effect=%s rule=%s", addedScope, addedEffect, addedRule)
	}
	out = surfaceText(b)
	if !strings.Contains(out, "Permissions — Project") {
		t.Errorf("expected return to Screen 2 after add: %s", out)
	}

	// Press 'e' on the existing rule "bash(go *)" to enter Edit Mode
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: 'e', Text: "e"}})
	inst.Frame()
	out = surfaceText(b)
	if !strings.Contains(out, "Edit Rule — Project — allowlist") {
		t.Errorf("expected Edit Rule title: %s", out)
	}
	if !strings.Contains(out, "bash(go *)") {
		t.Errorf("expected existing rule populated in input: %s", out)
	}

	// Esc back to Screen 2
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyEscape}})
	inst.Frame()
	out = surfaceText(b)
	if !strings.Contains(out, "Permissions — Project") {
		t.Errorf("expected return to Screen 2 after Edit Esc: %s", out)
	}
	_ = editedOldRule
	_ = editedNewRule
	_ = editedScope
	_ = editedEffect
}
