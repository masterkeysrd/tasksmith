package components

import (
	"testing"

	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/tasksmith/internal/tui/testutils"
)

func TestUseKeyMap(t *testing.T) {
	for _, tc := range []struct {
		name      string
		active    bool
		wantHints string
		wantCalls string
	}{
		{"active", true, "Keyboard: esc Return  a Accept  ? Help  mouse Click", "eah"},
		{"inactive", false, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := ""
			record := func(s string) func() { return func() { calls += s } }
			actions := []KeyAction{
				{Keys: []string{"esc", "escape"}, Hint: "esc", Label: "Return", OnPress: record("e")},
				{Keys: []string{"a"}, Label: "Accept", OnPress: record("a")},
				{Keys: []string{"a"}, OnPress: record("duplicate")},
				{Keys: []string{"d"}, Label: "Disabled", Disabled: true, OnPress: record("disabled")},
				{Keys: []string{"h"}, OnPress: record("h")},
				{Keys: []string{"?"}, Label: "Help"},
				{Hint: "mouse", Label: "Click", OnPress: record("no keys")},
				{Label: "no hint"},
			}
			probe := trail.SimpleFC("KeyMapProbe", func() trail.Node {
				bindings := UseKeyMap(actions, tc.active)
				if !tc.active && bindings != nil {
					t.Errorf("inactive bindings = %#v, want nil", bindings)
				}
				return KeyboardHint(KeyboardHintProps{Bindings: bindings})
			})
			if got := testutils.Render(t, probe(), 100, 10); got != tc.wantHints {
				t.Fatalf("got hints %q, want %q", got, tc.wantHints)
			}
			app := testutils.NewApp(t, probe(), 100, 10)
			app.Press(key.Key{Code: key.KeyEscape})
			app.Type("adh?z")
			if calls != tc.wantCalls {
				t.Fatalf("got handlers %q, want %q", calls, tc.wantCalls)
			}
		})
	}
}
