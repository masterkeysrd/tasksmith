package components

import (
	"testing"

	"github.com/masterkeysrd/tasksmith/internal/tui/testutils"
)

func TestBadge(t *testing.T) {
	for _, tc := range []struct {
		props BadgeProps
		want  string
	}{
		{BadgeProps{Icon: "●", Label: "Ready", Tone: ToneSuccess}, "● Ready"},
		{BadgeProps{Icon: "○"}, "○"},
		{BadgeProps{Label: "Offline"}, "Offline"},
		{BadgeProps{}, ""},
	} {
		if got := testutils.Render(t, Badge(tc.props), 24, 3); got != tc.want {
			t.Errorf("rendered %q, want %q", got, tc.want)
		}
	}
}
