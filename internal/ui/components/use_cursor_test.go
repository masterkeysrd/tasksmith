package components

import (
	"fmt"
	"strings"
	"testing"

	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/tasksmith/internal/tui/testutils"
)

func TestClampCursor(t *testing.T) {
	tests := []struct {
		cur, count, want int
	}{
		{0, 0, 0},
		{3, 0, 0},
		{-1, 0, 0},
		{-2, 5, 0},
		{0, 5, 0},
		{4, 5, 4},
		{5, 5, 4},
		{99, 3, 2},
	}
	for _, tt := range tests {
		if got := ClampCursor(tt.cur, tt.count); got != tt.want {
			t.Errorf("ClampCursor(%d, %d) = %d, want %d", tt.cur, tt.count, got, tt.want)
		}
	}
}

func TestStepCursor(t *testing.T) {
	tests := []struct {
		name        string
		cur, count  int
		k           key.Key
		wantNext    int
		wantHandled bool
	}{
		{
			name:        "down from 0",
			cur:         0,
			count:       3,
			k:           key.Key{Code: key.KeyDown},
			wantNext:    1,
			wantHandled: true,
		},
		{
			name:        "j from 1",
			cur:         1,
			count:       3,
			k:           key.Key{Code: 'j', Text: "j"},
			wantNext:    2,
			wantHandled: true,
		},
		{
			name:        "ctrl+n clamps at bottom",
			cur:         2,
			count:       3,
			k:           key.Key{Code: 'n', Text: "n", Mod: key.ModCtrl},
			wantNext:    2,
			wantHandled: true,
		},
		{
			name:        "up from 2",
			cur:         2,
			count:       3,
			k:           key.Key{Code: key.KeyUp},
			wantNext:    1,
			wantHandled: true,
		},
		{
			name:        "k from 1",
			cur:         1,
			count:       3,
			k:           key.Key{Code: 'k', Text: "k"},
			wantNext:    0,
			wantHandled: true,
		},
		{
			name:        "ctrl+p clamps at 0",
			cur:         0,
			count:       3,
			k:           key.Key{Code: 'p', Text: "p", Mod: key.ModCtrl},
			wantNext:    0,
			wantHandled: true,
		},
		{
			name:        "down on empty list stays 0 and handled",
			cur:         2,
			count:       0,
			k:           key.Key{Code: key.KeyDown},
			wantNext:    0,
			wantHandled: true,
		},
		{
			name:        "clamps out of bounds cur before stepping",
			cur:         10,
			count:       3,
			k:           key.Key{Code: key.KeyUp},
			wantNext:    1,
			wantHandled: true,
		},
		{
			name:        "unrelated key not handled",
			cur:         1,
			count:       3,
			k:           key.Key{Code: key.KeyEnter},
			wantNext:    1,
			wantHandled: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			next, handled := StepCursor(tt.cur, tt.count, *event.NewKeyEvent(event.EventKeyPress, tt.k))
			if next != tt.wantNext || handled != tt.wantHandled {
				t.Errorf("StepCursor(%d, %d, %+v) = (%d, %v), want (%d, %v)",
					tt.cur, tt.count, tt.k, next, handled, tt.wantNext, tt.wantHandled)
			}
		})
	}
}

func TestUseCursorComponentInteraction(t *testing.T) {
	type harnessProps struct {
		count   int
		initial int
	}
	harness := trail.FC("CursorHarness", func(props harnessProps) trail.Node {
		cursor := UseCursor(props.count, props.initial)
		trail.UseInput(func(_ string, k event.KeyEvent) {
			if cursor.HandleKey(k) {
				return
			}
			switch {
			case k.MatchString("0"):
				cursor.Set(0)
			case k.MatchString("g"):
				cursor.Move(2)
			}
		})
		return trail.Text(fmt.Sprintf("index=%d", cursor.Index))
	})

	env := testutils.NewEnv(t, 40, 8)
	out := env.Render(harness(harnessProps{count: 4, initial: 1}))
	if !strings.Contains(out, "index=1") {
		t.Fatalf("expected initial index=1, got:\n%s", out)
	}

	out = env.Press(key.Key{Code: key.KeyDown})
	if !strings.Contains(out, "index=2") {
		t.Fatalf("expected index=2 after down, got:\n%s", out)
	}

	out = env.Press(key.Key{Code: 'k', Text: "k"})
	if !strings.Contains(out, "index=1") {
		t.Fatalf("expected index=1 after k, got:\n%s", out)
	}

	out = env.Press(key.Key{Code: 'g', Text: "g"})
	if !strings.Contains(out, "index=3") {
		t.Fatalf("expected index=3 after Move(2), got:\n%s", out)
	}

	// Shrinking count clamps rendered index without crashing.
	out = env.Render(harness(harnessProps{count: 2, initial: 1}))
	if !strings.Contains(out, "index=1") {
		t.Fatalf("expected clamped index=1 when count shrinks to 2, got:\n%s", out)
	}

	out = env.Press(key.Key{Code: '0', Text: "0"})
	if !strings.Contains(out, "index=0") {
		t.Fatalf("expected index=0 after Set(0), got:\n%s", out)
	}
}
