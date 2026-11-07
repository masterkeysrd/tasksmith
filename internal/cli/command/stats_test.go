package command

import (
	"strings"
	"testing"

	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/tui/testutils"
)

func TestStatsCommandRegistration(t *testing.T) {
	reg := NewRegistry()

	cmd, ok := reg.Lookup("stats")
	if !ok || cmd.Name != "stats" {
		t.Fatalf("expected /stats to be registered, ok=%v cmd=%+v", ok, cmd)
	}
	if cmd.Usage != "/stats [session|workspace|global]" {
		t.Errorf("unexpected usage: %q", cmd.Usage)
	}
	if cmd.Description == "" {
		t.Error("expected a non-empty description")
	}
}

func TestStatsCommandArgs(t *testing.T) {
	store.ClearSlot()
	t.Cleanup(store.ClearSlot)

	reg := NewRegistry()

	// A bare /stats opens the browser on the Session tab.
	if err := reg.Execute("/stats"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	active := store.GetSlot().Active
	if active == nil || active.Kind != store.SlotKindPicker {
		t.Fatalf("expected picker slot, got %+v", active)
	}
	if _, ok := active.Data.(func(string) trail.Node); !ok {
		t.Errorf("expected a slot view constructor, got %T", active.Data)
	}
	store.ClearSlot()

	for _, scope := range []string{"session", "workspace", "global", "GLOBAL"} {
		if err := reg.Execute("/stats " + scope); err != nil {
			t.Errorf("Execute(\"/stats %s\"): %v", scope, err)
		}
		store.ClearSlot()
	}

	for _, tc := range []struct{ line, wantError string }{
		{"/stats bogus", "unknown stats view"},
		{"/stats session extra", "stats: /stats [session|workspace|global]"},
	} {
		err := reg.Execute(tc.line)
		if err == nil || !strings.Contains(err.Error(), tc.wantError) {
			t.Errorf("Execute(%q) error = %v, want it to contain %q", tc.line, err, tc.wantError)
		}
		store.ClearSlot()
	}
}

func TestStatsTabBar(t *testing.T) {
	bar := statsTabBar(usageTabWorkspace, statsRanges[1])
	if !strings.Contains(bar, "[2 Workspace]") {
		t.Errorf("active tab should be bracketed: %q", bar)
	}
	if !strings.Contains(bar, "1 Session") || !strings.Contains(bar, "3 Global") {
		t.Errorf("inactive tabs should keep their digit: %q", bar)
	}
	if strings.Contains(bar, "[1 Session]") || strings.Contains(bar, "[3 Global]") {
		t.Errorf("only one tab may be active: %q", bar)
	}
	if !strings.Contains(bar, "range: 7d") {
		t.Errorf("expected the active range: %q", bar)
	}
}

// seedStatsSession gives the view a session so the Session tab renders rather
// than reporting that none is active.
func seedStatsSession(t *testing.T) {
	t.Helper()
	store.SetSession(&api.Session{ID: "sess-stats", Config: api.SessionConfig{Mode: api.ModeDefault}})
	t.Cleanup(func() { store.SetSession(nil) })
}

// TestStatsBrowserChromeAndUnavailableClient checks that the framing renders
// with no data behind it. No client is wired in tests, so the body reports the
// unavailable client instead of reaching the network.
func TestStatsBrowserChromeAndUnavailableClient(t *testing.T) {
	out := testutils.Render(t, statsBrowser(statsBrowserProps{slotID: "slot-stats-render", tab: usageTabWorkspace}), 100, 40)

	if !strings.Contains(out, "Stats") {
		t.Errorf("title missing:\n%s", out)
	}
	if !strings.Contains(out, "[2 Workspace]") {
		t.Errorf("workspace tab should be active:\n%s", out)
	}
	if !strings.Contains(out, "range: today") {
		t.Errorf("expected the default range:\n%s", out)
	}
	if !strings.Contains(out, "Client unavailable") {
		t.Errorf("expected the no-client state to be shown:\n%s", out)
	}
	if !strings.Contains(out, "esc") || !strings.Contains(out, "Range") {
		t.Errorf("expected keyboard hints:\n%s", out)
	}
}

func TestStatsBrowserNoSession(t *testing.T) {
	store.ResetSession()
	t.Cleanup(store.ResetSession)

	out := testutils.Render(t, statsBrowser(statsBrowserProps{slotID: "slot-stats-nosession", tab: usageTabSession}), 100, 30)
	if !strings.Contains(out, "No active session.") {
		t.Errorf("expected the no-session state:\n%s", out)
	}
	// The chrome must not claim throughput the view never measured.
	for _, unwanted := range []string{"tok/s", "Per model"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("no-session view should not render %q:\n%s", unwanted, out)
		}
	}
}

func TestStatsBrowserKeys(t *testing.T) {
	seedStatsSession(t)
	store.ClearSlot()
	t.Cleanup(store.ClearSlot)

	env := testutils.NewEnv(t, 100, 40)
	env.Render(statsBrowser(statsBrowserProps{slotID: "slot-stats-keys", tab: usageTabSession}))

	// t cycles the range, and the tab bar reports it.
	out := env.Press(key.Key{Code: 't', Text: "t"})
	if !strings.Contains(out, "range: 7d") {
		t.Fatalf("pressing t did not advance the range:\n%s", out)
	}
	out = env.Press(key.Key{Code: 't', Text: "t"})
	if !strings.Contains(out, "range: 30d") {
		t.Fatalf("second t did not advance the range:\n%s", out)
	}
	// Four presses wrap back to the first range rather than running off the end.
	env.Press(key.Key{Code: 't', Text: "t"})
	out = env.Press(key.Key{Code: 't', Text: "t"})
	if !strings.Contains(out, "range: today") {
		t.Fatalf("range did not wrap around:\n%s", out)
	}

	// Digits jump to a tab; arrows walk it.
	out = env.Press(key.Key{Code: '3', Text: "3"})
	if !strings.Contains(out, "[3 Global]") {
		t.Fatalf("pressing 3 did not select Global:\n%s", out)
	}
	out = env.Press(key.Key{Code: key.KeyRight})
	if !strings.Contains(out, "[1 Session]") {
		t.Fatalf("right arrow did not wrap to Session:\n%s", out)
	}
	out = env.Press(key.Key{Code: key.KeyLeft})
	if !strings.Contains(out, "[3 Global]") {
		t.Fatalf("left arrow did not wrap back to Global:\n%s", out)
	}

	// A key that is bound to nothing must leave the view untouched.
	before := env.Press(key.Key{Code: 'z', Text: "z"})
	after := env.Press(key.Key{Code: 'z', Text: "z"})
	if before != after {
		t.Errorf("unbound key changed the view:\n%s\n%s", before, after)
	}

	// esc dismisses the slot it owns.
	env.Press(key.Key{Code: key.KeyEscape})
	if active := store.GetSlot().Active; active != nil && active.ID == "slot-stats-keys" {
		t.Errorf("esc left the stats slot open: %+v", active)
	}
}

func TestStatsBrowserScrollStaysInBounds(t *testing.T) {
	seedStatsSession(t)
	store.ClearSlot()
	t.Cleanup(store.ClearSlot)

	env := testutils.NewEnv(t, 100, 20)
	first := env.Render(statsBrowser(statsBrowserProps{slotID: "slot-stats-scroll", tab: usageTabSession}))

	// Scrolling past the top of a short view is inert, not a crash or a wrap.
	for range 5 {
		env.Press(key.Key{Code: key.KeyUp})
		env.Press(key.Key{Code: 'p', Text: "p", Mod: key.ModCtrl})
	}
	if got := env.Text(); got != first {
		t.Errorf("scrolling up at the top changed the view:\n%s", got)
	}
	for range 40 {
		env.Press(key.Key{Code: key.KeyDown})
		env.Press(key.Key{Code: 'n', Text: "n", Mod: key.ModCtrl})
	}
	if got := env.Text(); got != first {
		t.Errorf("scrolling past the bottom changed the view:\n%s", got)
	}
}
