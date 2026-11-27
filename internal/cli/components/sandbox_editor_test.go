package components

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/tui/testutils"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

func healthySandboxResponse() api.SandboxResponse {
	tr := true
	fa := false
	return api.SandboxResponse{
		Project: api.SandboxSettings{
			Mode:              api.SandboxNative,
			FailIfUnavailable: &fa,
			ExcludedCommands:  []string{"git commit *"},
		},
		Global: api.SandboxSettings{
			AllowUnsandboxed: &tr,
		},
		Effective: api.SandboxSettings{
			Mode:              api.SandboxNative,
			FailIfUnavailable: &fa,
			AllowUnsandboxed:  &tr,
			AutoAllowBash:     &fa,
		},
		Availability: api.SandboxAvailability{Available: true, Backend: "seatbelt"},
		ResolvedPaths: api.SandboxResolvedPaths{
			WorkDir:    "/repo",
			AllowWrite: []string{"/repo"},
			DenyWrite:  []string{"/repo/.agents"},
		},
	}
}

// renderSandboxEditor mounts the editor in a running trail loop so key presses
// repaint on their own and app.WaitFor can observe state transitions.
func renderSandboxEditor(t *testing.T, props SandboxConfigEditorProps) *testutils.App {
	t.Helper()
	editor := trail.SimpleFC("SandboxEditorTestApp", func() kitex.Node {
		return SandboxConfigEditor(props)
	})
	app := testutils.StartApp(t, editor(), 100, 30)
	t.Cleanup(app.Stop)
	app.AwaitFrame(testutils.DefaultWaitTimeout)
	return app
}

// knobRecorder synchronises callback capture across the engine goroutine.
type knobRecorder struct {
	mu      sync.Mutex
	knobs   []SandboxPolicyKnob
	values  []*bool
	scopes  []api.PermissionScope
	modes   []api.SandboxMode
	modeScp []api.PermissionScope
	added   []string
	edited  [][2]string
	editScp []api.PermissionScope
	removed []string
	closes  int
}

func (r *knobRecorder) knob(scope api.PermissionScope, knob SandboxPolicyKnob, value *bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.scopes = append(r.scopes, scope)
	r.knobs = append(r.knobs, knob)
	r.values = append(r.values, value)
}

func (r *knobRecorder) mode(scope api.PermissionScope, mode api.SandboxMode) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.modeScp = append(r.modeScp, scope)
	r.modes = append(r.modes, mode)
}

func (r *knobRecorder) add(_ api.PermissionScope, pattern string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.added = append(r.added, pattern)
}

func (r *knobRecorder) edit(scope api.PermissionScope, oldPattern, newPattern string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.edited = append(r.edited, [2]string{oldPattern, newPattern})
	r.editScp = append(r.editScp, scope)
}

func (r *knobRecorder) remove(_ api.PermissionScope, pattern string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.removed = append(r.removed, pattern)
}

func (r *knobRecorder) close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closes++
}

func (r *knobRecorder) knobCalls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.knobs)
}

func (r *knobRecorder) lastKnob() (api.PermissionScope, SandboxPolicyKnob, *bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.knobs) == 0 {
		return "", "", nil
	}
	i := len(r.knobs) - 1
	return r.scopes[i], r.knobs[i], r.values[i]
}

func (r *knobRecorder) modeCalls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.modes)
}

func (r *knobRecorder) lastMode() (api.PermissionScope, api.SandboxMode) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.modes) == 0 {
		return "", ""
	}
	i := len(r.modes) - 1
	return r.modeScp[i], r.modes[i]
}

func (r *knobRecorder) addedPatterns() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.added...)
}

func (r *knobRecorder) editCalls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.edited)
}

func (r *knobRecorder) lastEdit() (api.PermissionScope, string, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.edited) == 0 {
		return "", "", ""
	}
	i := len(r.edited) - 1
	return r.editScp[i], r.edited[i][0], r.edited[i][1]
}

func (r *knobRecorder) removedPatterns() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.removed...)
}

func (r *knobRecorder) closeCalls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closes
}

func TestSandboxConfigEditorPolicyTabHealthy(t *testing.T) {
	store.ResetApp()
	defer store.ResetApp()

	app := renderSandboxEditor(t, SandboxConfigEditorProps{
		Response:    healthySandboxResponse(),
		SessionMode: api.SandboxNative,
	})
	out := app.Text()

	for _, want := range []string{
		"Sandbox",
		"[1 Policy]",
		"2 Exclusions (1)",
		"3 Effective",
		"scope: [Project] · Global",
		"Active session:    native sandbox",
		"Host backend:      ✔ seatbelt available",
		"Workspace trust:   ✔ trusted",
		"Setting",
		"Project override",
		"Default mode",
		"● native",
		"Fail if unavailable",
		"Keyboard:",
		"←/→ Tab",
		"s Scope",
		"↑/↓ Move",
		"space Toggle",
		"x Reset",
		"esc Close",
		"key: mode",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output:\n%s", want, out)
		}
	}

	// A blank spacer row separates the tab strip from the tab content.
	rawWithBlanks := testutils.SurfaceTextKeepBlanks(app.Backend)
	lines := testutils.TrimmedLines(rawWithBlanks)
	tabLine := -1
	for i, l := range lines {
		if strings.Contains(l, "[1 Policy]") {
			tabLine = i
			break
		}
	}
	if tabLine < 0 || tabLine+2 >= len(lines) {
		t.Fatalf("expected tab strip followed by spacer and body:\n%s", rawWithBlanks)
	}
	if strings.TrimSpace(lines[tabLine+1]) != "" {
		t.Fatalf("expected blank spacer row under tab strip, got %q:\n%s", lines[tabLine+1], rawWithBlanks)
	}
}

func TestSandboxConfigEditorNarrowScreenWrapsChromeAndHint(t *testing.T) {
	store.ResetApp()
	defer store.ResetApp()

	editor := trail.SimpleFC("SandboxEditorNarrowApp", func() kitex.Node {
		return SandboxConfigEditor(SandboxConfigEditorProps{
			Response:    healthySandboxResponse(),
			SessionMode: api.SandboxNative,
		})
	})
	app := testutils.StartApp(t, editor(), 54, 30)
	t.Cleanup(app.Stop)
	app.AwaitFrame(testutils.DefaultWaitTimeout)
	out := app.Text()

	for _, want := range []string{
		"[1 Policy]",
		"scope: [Project] · Global",
		"←/→ Tab",
		"s Scope",
		"↑/↓ Move",
		"space Toggle",
		"x Reset",
		"esc Close",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q intact on narrow screen (width 54):\n%s", want, out)
		}
	}
}

func TestSandboxConfigEditorUnavailableRemediation(t *testing.T) {
	store.ResetApp()
	defer store.ResetApp()

	resp := healthySandboxResponse()
	remediation := "on Ubuntu 24.04, unprivileged user namespaces are confined by AppArmor; " +
		"install bubblewrap with apt if it is missing, or relax the restriction with " +
		"sudo sysctl -w kernel.apparmor_restrict_unprivileged_userns=0"
	resp.Availability = api.SandboxAvailability{
		Available:   false,
		Backend:     "bubblewrap",
		Error:       "bwrap: setting up uid map: Permission denied",
		Remediation: remediation,
	}
	resp.Effective.Mode = api.SandboxLocal

	app := renderSandboxEditor(t, SandboxConfigEditorProps{Response: resp})
	out := app.Text()

	if !strings.Contains(out, "Host backend:      ⚠ bubblewrap unavailable") {
		t.Fatalf("expected backend warning in output:\n%s", out)
	}
	if !strings.Contains(out, "error: bwrap: setting up uid map: Permission denied") {
		t.Fatalf("expected availability error line in output:\n%s", out)
	}
	// The remediation must wrap rather than truncate: the sysctl command sits
	// far past a single line's width and must remain fully visible.
	joined := strings.Join(testutils.TrimmedLines(out), " ")
	if !strings.Contains(joined, "Fix: on Ubuntu 24.04") ||
		!strings.Contains(joined, "kernel.apparmor_restrict_unprivileged_userns=0") {
		t.Fatalf("expected full remediation command visible after wrapping:\n%s", out)
	}
}

func TestSandboxConfigEditorTrustBanner(t *testing.T) {
	store.ResetApp()
	defer store.ResetApp()

	resp := healthySandboxResponse()
	resp.Restricted = true

	app := renderSandboxEditor(t, SandboxConfigEditorProps{Response: resp})
	out := app.Text()

	if !strings.Contains(out, "Workspace trust:   ⚠ untrusted (unsandboxed execution disabled)") {
		t.Fatalf("expected untrusted banner in output:\n%s", out)
	}
}

func TestSandboxConfigEditorToggleKnobAndMode(t *testing.T) {
	store.ResetApp()
	defer store.ResetApp()

	rec := &knobRecorder{}
	props := SandboxConfigEditorProps{
		Response:     healthySandboxResponse(),
		OnSetMode:    rec.mode,
		OnToggleKnob: rec.knob,
	}
	app := renderSandboxEditor(t, props)

	// Cursor starts on the mode row: enter cycles native -> local.
	app.Press(key.Key{Code: key.KeyEnter})
	app.WaitFor(func(string) bool { return rec.modeCalls() == 1 })
	scope, mode := rec.lastMode()
	if mode != api.SandboxLocal || scope != api.ScopeProject {
		t.Fatalf("expected mode switch to local on project scope, got mode=%q scope=%q", mode, scope)
	}

	// Move to the autoAllowBash knob (row 3): its description line only
	// renders under the cursor, so it proves the move landed before toggling.
	app.Press(key.Key{Code: key.KeyDown})
	app.WaitFor(func(s string) bool { return strings.Contains(s, "Fail session creation") })
	app.Press(key.Key{Code: key.KeyDown})
	app.WaitFor(func(s string) bool { return strings.Contains(s, "Allow unsandboxed command execution") })
	app.Press(key.Key{Code: key.KeyDown})
	app.WaitFor(func(s string) bool { return strings.Contains(s, "Auto-approve safe sandboxed bash plans") })
	app.Press(key.Key{Code: key.KeyEnter})
	app.WaitFor(func(string) bool { return rec.knobCalls() == 1 })
	kScope, knob, val := rec.lastKnob()
	// Effective autoAllowBash is false and the scope value is unset, so the
	// toggle must request true.
	if knob != KnobAutoAllowBash || val == nil || !*val || kScope != api.ScopeProject {
		t.Fatalf("expected autoAllowBash toggle to true on project scope, got knob=%q val=%v scope=%q", knob, val, kScope)
	}
}

func TestSandboxConfigEditorSpaceTogglesKnob(t *testing.T) {
	store.ResetApp()
	defer store.ResetApp()

	rec := &knobRecorder{}
	props := SandboxConfigEditorProps{
		Response:     healthySandboxResponse(),
		OnToggleKnob: rec.knob,
	}
	app := renderSandboxEditor(t, props)

	// Row 1 is failIfUnavailable (explicitly false in Project): space must
	// toggle it the same way enter/t do.
	app.Press(key.Key{Code: key.KeyDown})
	app.WaitFor(func(s string) bool { return strings.Contains(s, "Fail session creation") })
	app.Press(key.Key{Code: ' ', Text: " "})
	app.WaitFor(func(string) bool { return rec.knobCalls() == 1 })
	knobScope, knob, val := rec.lastKnob()
	if knob != KnobFailIfUnavailable || val == nil || !*val || knobScope != api.ScopeProject {
		t.Fatalf("expected space to toggle failIfUnavailable to true, got scope=%q knob=%q val=%v", knobScope, knob, val)
	}
}

func TestSandboxConfigEditorModeCyclesThreeStates(t *testing.T) {
	store.ResetApp()
	defer store.ResetApp()

	rec := &knobRecorder{}

	// The editor derives the next cycle step from props, mirroring the real
	// view where a successful PUT re-renders with the stored mode. This
	// harness applies each OnSetMode call back into the response.
	resp := healthySandboxResponse()
	harness := trail.SimpleFC("SandboxEditorModeCycleApp", func() kitex.Node {
		mode, setMode := kitex.UseState(resp.Project.Mode)
		current := resp
		current.Project.Mode = mode()
		return SandboxConfigEditor(SandboxConfigEditorProps{
			Response: current,
			OnSetMode: func(_ api.PermissionScope, next api.SandboxMode) {
				rec.mode(api.ScopeProject, next)
				setMode(next)
			},
		})
	})
	app := testutils.StartApp(t, harness(), 100, 30)
	t.Cleanup(app.Stop)
	app.AwaitFrame(testutils.DefaultWaitTimeout)

	// Project mode is native: enter -> local, enter -> inherit (unset).
	app.Press(key.Key{Code: key.KeyEnter})
	app.WaitFor(func(string) bool { return rec.modeCalls() == 1 })
	if _, mode := rec.lastMode(); mode != api.SandboxLocal {
		t.Fatalf("expected first cycle step to be local, got %q", mode)
	}
	// Wait for the harness to re-render with the applied mode before cycling
	// again, so the editor derives the next step from fresh props.
	app.WaitFor(func(s string) bool {
		return strings.Contains(s, "Default mode") && strings.Contains(s, "local")
	})
	app.Press(key.Key{Code: key.KeyEnter})
	app.WaitFor(func(string) bool { return rec.modeCalls() == 2 })
	if _, mode := rec.lastMode(); mode != "" {
		t.Fatalf("expected second cycle step to clear the override (inherit), got %q", mode)
	}
	// With the override cleared the row reads "inherited".
	app.WaitFor(func(s string) bool { return strings.Contains(s, "Default mode") && strings.Contains(s, "inherited") })
}

func TestSandboxConfigEditorToggleUsesScopeValueNotEffective(t *testing.T) {
	store.ResetApp()
	defer store.ResetApp()

	// Project pins autoAllowBash true, so effective is true while Global is
	// explicitly false. Toggling in Global scope must flip Global's own value
	// to true, not keep re-sending false because effective reads true.
	resp := healthySandboxResponse()
	tr := true
	fa := false
	resp.Project.AutoAllowBash = &tr
	resp.Global.AutoAllowBash = &fa
	resp.Effective.AutoAllowBash = &tr

	rec := &knobRecorder{}
	props := SandboxConfigEditorProps{
		Response:     resp,
		OnToggleKnob: rec.knob,
	}
	app := renderSandboxEditor(t, props)

	app.Press(key.Key{Code: 's', Text: "s"}) // -> Global scope
	app.WaitFor(func(s string) bool { return strings.Contains(s, "· [Global]") })
	app.Press(key.Key{Code: key.KeyDown})
	app.WaitFor(func(s string) bool { return strings.Contains(s, "Fail session creation") })
	app.Press(key.Key{Code: key.KeyDown})
	app.WaitFor(func(s string) bool { return strings.Contains(s, "Allow unsandboxed command execution") })
	app.Press(key.Key{Code: key.KeyDown})
	app.WaitFor(func(s string) bool { return strings.Contains(s, "Auto-approve safe sandboxed bash plans") })
	app.Press(key.Key{Code: key.KeyEnter})
	app.WaitFor(func(string) bool { return rec.knobCalls() == 1 })
	kScope, knob, val := rec.lastKnob()
	if kScope != api.ScopeGlobal || knob != KnobAutoAllowBash || val == nil || !*val {
		t.Fatalf("expected Global autoAllowBash toggle to true (flipping Global's own false), got scope=%q knob=%q val=%v", kScope, knob, val)
	}
}

func TestSandboxConfigEditorXClearsKnobOverride(t *testing.T) {
	store.ResetApp()
	defer store.ResetApp()

	rec := &knobRecorder{}
	props := SandboxConfigEditorProps{
		Response:     healthySandboxResponse(),
		OnToggleKnob: rec.knob,
	}
	app := renderSandboxEditor(t, props)

	// Row 1 is failIfUnavailable (explicitly false in Project): x must clear
	// the override (nil), not write an explicit false.
	app.Press(key.Key{Code: key.KeyDown})
	app.WaitFor(func(s string) bool { return strings.Contains(s, "Fail session creation") })
	app.Press(key.Key{Code: 'x', Text: "x"})
	app.WaitFor(func(string) bool { return rec.knobCalls() == 1 })
	_, knob, val := rec.lastKnob()
	if knob != KnobFailIfUnavailable || val != nil {
		t.Fatalf("expected x to clear the failIfUnavailable override to nil, got knob=%q val=%v", knob, val)
	}
}

func TestSandboxConfigEditorBackspaceClearsKnobOverride(t *testing.T) {
	store.ResetApp()
	defer store.ResetApp()

	rec := &knobRecorder{}
	props := SandboxConfigEditorProps{
		Response:     healthySandboxResponse(),
		OnToggleKnob: rec.knob,
	}
	app := renderSandboxEditor(t, props)

	app.Press(key.Key{Code: key.KeyDown})
	app.WaitFor(func(s string) bool { return strings.Contains(s, "Fail session creation") })
	app.Press(key.Key{Code: key.KeyBackspace})
	app.WaitFor(func(string) bool { return rec.knobCalls() == 1 })
	_, knob, val := rec.lastKnob()
	if knob != KnobFailIfUnavailable || val != nil {
		t.Fatalf("expected backspace to clear the failIfUnavailable override to nil, got knob=%q val=%v", knob, val)
	}
}

func TestSandboxConfigEditorLockedKnobRefused(t *testing.T) {
	store.ResetApp()
	defer store.ResetApp()

	rec := &knobRecorder{}
	props := SandboxConfigEditorProps{
		Response: healthySandboxResponse(),
		LockedKnobs: map[api.PermissionScope]map[SandboxPolicyKnob]string{
			api.ScopeProject: {KnobFailIfUnavailable: "global scope pins it on"},
			api.ScopeGlobal:  {},
		},
		OnToggleKnob: rec.knob,
	}
	app := renderSandboxEditor(t, props)

	// Move onto failIfUnavailable: only the cursor row's footer names its
	// JSON key, so waiting on it proves the move landed.
	app.Press(key.Key{Code: key.KeyDown})
	out := app.WaitFor(func(s string) bool { return strings.Contains(s, "key: failIfUnavailable") })
	if !strings.Contains(out, "locked (global scope pins it on)") {
		t.Fatalf("expected project failIfUnavailable row:\n%s", out)
	}

	app.Press(key.Key{Code: key.KeyEnter})
	app.Press(key.Key{Code: 'x', Text: "x"})
	if rec.knobCalls() != 0 {
		t.Fatalf("locked knob must not fire a toggle, got %d", rec.knobCalls())
	}

	// The same knob stays editable in Global scope (locks are scope-aware):
	// switching scopes repaints, then the toggle fires.
	app.Press(key.Key{Code: 's', Text: "s"})
	app.WaitFor(func(s string) bool { return strings.Contains(s, "· [Global]") })
	app.Press(key.Key{Code: key.KeyDown})
	app.WaitFor(func(s string) bool { return strings.Contains(s, "Fail session creation") })
	app.Press(key.Key{Code: key.KeyEnter})
	app.WaitFor(func(string) bool { return rec.knobCalls() == 1 })
	kScope, knob, _ := rec.lastKnob()
	if kScope != api.ScopeGlobal || knob != KnobFailIfUnavailable {
		t.Fatalf("expected global failIfUnavailable toggle, got scope=%q knob=%q", kScope, knob)
	}
}

func TestSandboxConfigEditorScopeSwitch(t *testing.T) {
	store.ResetApp()
	defer store.ResetApp()

	rec := &knobRecorder{}
	props := SandboxConfigEditorProps{
		Response:  healthySandboxResponse(),
		OnSetMode: rec.mode,
	}
	app := renderSandboxEditor(t, props)

	app.Press(key.Key{Code: 's', Text: "s"})
	app.WaitFor(func(s string) bool { return strings.Contains(s, "· [Global]") })
	app.Press(key.Key{Code: key.KeyEnter})
	app.WaitFor(func(string) bool { return rec.modeCalls() == 1 })
	scope, _ := rec.lastMode()
	if scope != api.ScopeGlobal {
		t.Fatalf("expected mode change on global scope, got scope=%q", scope)
	}
}

func TestSandboxConfigEditorShiftTabWrapsToEffective(t *testing.T) {
	store.ResetApp()
	defer store.ResetApp()

	app := renderSandboxEditor(t, SandboxConfigEditorProps{Response: healthySandboxResponse()})

	app.Press(key.Key{Code: key.KeyTab, Mod: key.ModShift})
	out := app.WaitFor(func(s string) bool { return strings.Contains(s, "[3 Effective]") })
	if !strings.Contains(out, "Posture") {
		t.Fatalf("expected effective tab after shift+tab wrap:\n%s", out)
	}
	// The read-only tab has no fake cursor and no scope chrome.
	if strings.Contains(out, "❯") || strings.Contains(out, "scope:") {
		t.Fatalf("expected no cursor or scope chrome on effective tab:\n%s", out)
	}
}

func TestSandboxConfigEditorNumberKeysSwitchTabs(t *testing.T) {
	store.ResetApp()
	defer store.ResetApp()

	app := renderSandboxEditor(t, SandboxConfigEditorProps{Response: healthySandboxResponse()})

	app.Press(key.Key{Code: '3', Text: "3"})
	out := app.WaitFor(func(s string) bool { return strings.Contains(s, "[3 Effective]") })
	if !strings.Contains(out, "Posture") {
		t.Fatalf("expected effective tab after pressing 3:\n%s", out)
	}
	app.Press(key.Key{Code: '2', Text: "2"})
	out = app.WaitFor(func(s string) bool { return strings.Contains(s, "[2 Exclusions (1)]") })
	if !strings.Contains(out, "Excluded commands — Project (1)") {
		t.Fatalf("expected exclusions tab after pressing 2:\n%s", out)
	}
}

func TestSandboxConfigEditorExcludedTabAddRemove(t *testing.T) {
	store.ResetApp()
	defer store.ResetApp()

	rec := &knobRecorder{}
	props := SandboxConfigEditorProps{
		Response:         healthySandboxResponse(),
		OnAddExcluded:    rec.add,
		OnRemoveExcluded: rec.remove,
	}
	app := renderSandboxEditor(t, props)

	app.Press(key.Key{Code: key.KeyTab}) // -> exclusions tab
	app.WaitFor(func(s string) bool { return strings.Contains(s, "Excluded commands — Project (1)") })

	// Remove the selected pattern.
	app.Press(key.Key{Code: 'd', Text: "d"})
	app.WaitFor(func(string) bool { return len(rec.removedPatterns()) == 1 })
	if removed := rec.removedPatterns(); removed[0] != "git commit *" {
		t.Fatalf("expected removal of \"git commit *\", got %v", removed)
	}

	// Add a new pattern through the guided input. The examples line appears
	// only once the inline input has mounted and taken focus, so typing after
	// this point lands in the field.
	app.Press(key.Key{Code: 'a', Text: "a"})
	app.WaitFor(func(s string) bool { return strings.Contains(s, "Examples: git commit *") })
	app.Type("docker 123sde *")
	app.Press(key.Key{Code: key.KeyEnter})
	out := app.WaitFor(func(s string) bool { return len(rec.addedPatterns()) == 1 })
	if !strings.Contains(out, "Keyboard:") {
		t.Fatalf("expected input to close after submit:\n%s", out)
	}
	if added := rec.addedPatterns(); added[0] != "docker 123sde *" {
		t.Fatalf("expected add of \"docker 123sde *\", got %v", added)
	}
}

func TestSandboxConfigEditorExcludedTabEditInPlace(t *testing.T) {
	store.ResetApp()
	defer store.ResetApp()

	rec := &knobRecorder{}
	props := SandboxConfigEditorProps{
		Response:       healthySandboxResponse(),
		OnEditExcluded: rec.edit,
	}
	app := renderSandboxEditor(t, props)

	app.Press(key.Key{Code: key.KeyTab})
	app.WaitFor(func(s string) bool { return strings.Contains(s, "Excluded commands — Project (1)") })

	// e opens the input seeded with the selected pattern.
	app.Press(key.Key{Code: 'e', Text: "e"})
	out := app.WaitFor(func(s string) bool {
		return strings.Contains(s, "Edit excluded command pattern (Project):") &&
			strings.Contains(s, "git commit *")
	})
	if !strings.Contains(out, "enter Save") || !strings.Contains(out, "esc Cancel") {
		t.Fatalf("expected guided input footer:\n%s", out)
	}

	// Typing appends to the seeded pattern; enter submits the edit.
	app.Type("x")
	app.Press(key.Key{Code: key.KeyEnter})
	app.WaitFor(func(string) bool { return rec.editCalls() == 1 })
	scope, oldPattern, newPattern := rec.lastEdit()
	if scope != api.ScopeProject || oldPattern != "git commit *" || newPattern != "git commit *x" {
		t.Fatalf("expected in-place edit git commit * -> git commit *x on project, got scope=%q old=%q new=%q",
			scope, oldPattern, newPattern)
	}
}

func TestSandboxConfigEditorExcludedTabInheritedFromGlobal(t *testing.T) {
	store.ResetApp()
	defer store.ResetApp()

	resp := healthySandboxResponse()
	resp.Global.ExcludedCommands = []string{"make release"}

	app := renderSandboxEditor(t, SandboxConfigEditorProps{Response: resp})
	app.Press(key.Key{Code: key.KeyTab})
	out := app.WaitFor(func(s string) bool {
		return strings.Contains(s, "Inherited from Global (1) — press s to edit Global")
	})
	if !strings.Contains(out, "make release") {
		t.Fatalf("expected inherited pattern listed:\n%s", out)
	}

	// In Global scope the section is the editable list, not a read-only
	// inheritance echo.
	app.Press(key.Key{Code: 's', Text: "s"})
	out = app.WaitFor(func(s string) bool {
		return !strings.Contains(s, "Inherited from Global") &&
			strings.Contains(s, "Excluded commands — Global (1)")
	})
	if !strings.Contains(out, "make release") {
		t.Fatalf("expected pattern listed as editable in Global scope:\n%s", out)
	}
}

func TestSandboxConfigEditorExcludedTabInactiveWarning(t *testing.T) {
	store.ResetApp()
	defer store.ResetApp()

	resp := healthySandboxResponse()
	off := false
	resp.Effective.AllowUnsandboxed = &off

	app := renderSandboxEditor(t, SandboxConfigEditorProps{Response: resp})
	app.Press(key.Key{Code: key.KeyTab})
	app.WaitFor(func(s string) bool {
		return strings.Contains(s, "⚠ Inactive: \"Allow unsandboxed\" is off")
	})
}

func TestSandboxConfigEditorEffectiveTab(t *testing.T) {
	store.ResetApp()
	defer store.ResetApp()

	props := SandboxConfigEditorProps{Response: healthySandboxResponse()}
	app := renderSandboxEditor(t, props)

	app.Press(key.Key{Code: '3', Text: "3"})
	out := app.WaitFor(func(s string) bool { return strings.Contains(s, "Working directory:") })
	for _, want := range []string{
		"Posture",
		"Mode:",
		"native",
		"Network:              open",
		"Excluded commands:    none",
		"Allow write:          1",
		"/repo",
		"Deny write:           1",
		"/repo/.agents",
		"Deny read:            none",
		"Allow read:           none",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in effective tab:\n%s", want, out)
		}
	}
}

func TestSandboxConfigEditorEffectiveTabColors(t *testing.T) {
	store.ResetApp()
	defer store.ResetApp()

	resp := healthySandboxResponse()
	resp.Effective.Network = api.SandboxNetworkSettings{
		Mode:           api.SandboxNetworkAllowlist,
		AllowedDomains: []string{"api.github.com"},
		DeniedDomains:  []string{"evil.example"},
	}
	resp.Effective.ExcludedCommands = []string{"git commit *"}

	app := renderSandboxEditor(t, SandboxConfigEditorProps{Response: resp})
	app.Press(key.Key{Code: '3', Text: "3"})
	app.WaitFor(func(s string) bool { return strings.Contains(s, "Working directory:") })

	scheme := theme.Get()
	frame := app.Backend.LastFrame().Surface
	if frame == nil {
		t.Fatal("expected painted surface")
	}

	cellFg := func(linePrefix string, col int) any {
		t.Helper()
		for y := 0; y < frame.Height; y++ {
			var sb strings.Builder
			for x := 0; x < frame.Width; x++ {
				c := frame.CellAt(x, y)
				if c.Content != "" {
					sb.WriteString(c.Content)
				} else {
					sb.WriteByte(' ')
				}
			}
			if strings.HasPrefix(strings.TrimRight(sb.String(), " "), linePrefix) {
				return frame.CellAt(col, y).Fg
			}
		}
		t.Fatalf("line with prefix %q not found in surface:\n%s", linePrefix, testutils.SurfaceText(app.Backend))
		return nil
	}

	if got := cellFg("Posture", 0); got != scheme.Color.Icon.Primary {
		t.Errorf("expected Posture heading fg %v, got %v", scheme.Color.Icon.Primary, got)
	}
	if got := cellFg("Mode:", 0); got != scheme.Color.Text.Secondary {
		t.Errorf("expected Mode label fg %v, got %v", scheme.Color.Text.Secondary, got)
	}
	if got := cellFg("Mode:", policyLabelCol); got != scheme.Color.Text.Success {
		t.Errorf("expected native mode value fg %v, got %v", scheme.Color.Text.Success, got)
	}
	if got := cellFg("Fail if unavailable:", policyLabelCol); got != scheme.Color.Text.Muted {
		t.Errorf("expected off knob fg %v, got %v", scheme.Color.Text.Muted, got)
	}
	if got := cellFg("Allow unsandboxed:", policyLabelCol); got != scheme.Color.Text.Warning {
		t.Errorf("expected Allow unsandboxed on fg %v, got %v", scheme.Color.Text.Warning, got)
	}
	if got := cellFg("Network:", policyLabelCol); got != scheme.Color.Text.Accent {
		t.Errorf("expected allowlist network fg %v, got %v", scheme.Color.Text.Accent, got)
	}
	if got := cellFg("  allow api.github.com", 2); got != scheme.Color.Text.Success {
		t.Errorf("expected allow domain verb fg %v, got %v", scheme.Color.Text.Success, got)
	}
	if got := cellFg("  allow api.github.com", 8); got != scheme.Color.Text.Primary {
		t.Errorf("expected allow domain host fg %v, got %v", scheme.Color.Text.Primary, got)
	}
	if got := cellFg("  deny evil.example", 2); got != scheme.Color.Text.Error {
		t.Errorf("expected deny domain verb fg %v, got %v", scheme.Color.Text.Error, got)
	}
	if got := cellFg("Excluded commands:", policyLabelCol); got != scheme.Color.Text.Warning {
		t.Errorf("expected Excluded commands count fg %v, got %v", scheme.Color.Text.Warning, got)
	}
	if got := cellFg("  git commit *", 2); got != scheme.Color.Text.Warning {
		t.Errorf("expected excluded pattern fg %v, got %v", scheme.Color.Text.Warning, got)
	}
	if got := cellFg("Filesystem", 0); got != scheme.Color.Icon.Primary {
		t.Errorf("expected Filesystem heading fg %v, got %v", scheme.Color.Icon.Primary, got)
	}
	if got := cellFg("Working directory:", policyLabelCol); got != scheme.Color.Text.Accent {
		t.Errorf("expected working directory fg %v, got %v", scheme.Color.Text.Accent, got)
	}
	if got := cellFg("Allow write:", policyLabelCol); got != scheme.Color.Text.Success {
		t.Errorf("expected Allow write count fg %v, got %v", scheme.Color.Text.Success, got)
	}
	if got := cellFg("  /repo", 2); got != scheme.Color.Text.Success {
		t.Errorf("expected Allow write path fg %v, got %v", scheme.Color.Text.Success, got)
	}
	if got := cellFg("Deny write:", policyLabelCol); got != scheme.Color.Text.Error {
		t.Errorf("expected Deny write count fg %v, got %v", scheme.Color.Text.Error, got)
	}
	if got := cellFg("  /repo/.agents", 2); got != scheme.Color.Text.Error {
		t.Errorf("expected Deny write path fg %v, got %v", scheme.Color.Text.Error, got)
	}
	if got := cellFg("Deny read:", policyLabelCol); got != scheme.Color.Text.Muted {
		t.Errorf("expected Deny read none fg %v, got %v", scheme.Color.Text.Muted, got)
	}

	gotSpans := truncateSandboxSpans([]sandboxSpan{
		{text: "Mode:                 ", role: lineSecondary},
		{text: "native", role: lineSuccess},
	}, 24)
	if len(gotSpans) != 2 || gotSpans[0].text != "Mode:                 " || gotSpans[1].text != "na…" || gotSpans[1].role != lineSuccess {
		t.Fatalf("unexpected truncated spans: %+v", gotSpans)
	}
}

func TestSandboxConfigEditorEffectiveTabShortensHome(t *testing.T) {
	store.ResetApp()
	defer store.ResetApp()
	home := t.TempDir()
	t.Setenv("HOME", home)

	repoPath := filepath.Join(home, "projects", "repo")
	wantShort := filepath.Join("~", "projects", "repo")
	resp := healthySandboxResponse()
	resp.ResolvedPaths.WorkDir = repoPath
	resp.ResolvedPaths.AllowWrite = []string{repoPath}

	app := renderSandboxEditor(t, SandboxConfigEditorProps{Response: resp})
	app.Press(key.Key{Code: '3', Text: "3"})
	out := app.WaitFor(func(s string) bool { return strings.Contains(s, wantShort) })
	if strings.Contains(out, repoPath) {
		t.Fatalf("expected home-shortened paths, raw path still visible:\n%s", out)
	}
}

func TestSandboxConfigEditorEffectiveTabScrolls(t *testing.T) {
	store.ResetApp()
	defer store.ResetApp()

	resp := healthySandboxResponse()
	// Enough entries to overflow the viewport so the scroll windowing and its
	// line-count footer become observable, and a tail section G can land on.
	for i := range 30 {
		resp.ResolvedPaths.DenyRead = append(resp.ResolvedPaths.DenyRead, fmt.Sprintf("/secrets/deep/path-%02d", i))
	}
	resp.ResolvedPaths.AllowRead = []string{"/secrets/pub"}
	app := renderSandboxEditor(t, SandboxConfigEditorProps{Response: resp})

	app.Press(key.Key{Code: '3', Text: "3"})
	out := app.WaitFor(func(s string) bool { return strings.Contains(s, "lines 1-") })
	if strings.Contains(out, "/secrets/pub") {
		t.Fatalf("expected tail row hidden at top of viewport:\n%s", out)
	}

	// Jump to the bottom with G; the tail rows must now be visible.
	app.Press(key.Key{Code: 'G', Text: "G"})
	out = app.WaitFor(func(s string) bool { return strings.Contains(s, "/secrets/pub") })
	if !strings.Contains(out, "Allow read:") {
		t.Fatalf("expected last section visible after G:\n%s", out)
	}

	// g returns to the top.
	app.Press(key.Key{Code: 'g', Text: "g"})
	app.WaitFor(func(s string) bool {
		return strings.Contains(s, "lines 1-") && !strings.Contains(s, "/secrets/pub")
	})
}

func TestSandboxConfigEditorCloseCallback(t *testing.T) {
	store.ResetApp()
	defer store.ResetApp()

	rec := &knobRecorder{}
	props := SandboxConfigEditorProps{
		Response: healthySandboxResponse(),
		OnClose:  rec.close,
	}
	app := renderSandboxEditor(t, props)

	app.Press(key.Key{Code: key.KeyEscape})
	app.WaitFor(func(string) bool { return rec.closeCalls() == 1 })
}
