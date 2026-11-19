package components

import (
	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
)

// Cursor holds the clamped index and movement helpers returned by [UseCursor].
type Cursor struct {
	Index     int
	Set       func(i int)
	Move      func(delta int)
	HandleKey func(k event.KeyEvent) bool
}

// ClampCursor constrains cur to [0, count-1], returning 0 when count <= 0 or cur < 0.
func ClampCursor(cur, count int) int {
	if count <= 0 || cur < 0 {
		return 0
	}
	if cur >= count {
		return count - 1
	}
	return cur
}

// StepCursor advances or retreats cur within [0, count-1] on vertical list
// navigation keys (up/k/ctrl+p and down/j/ctrl+n).
func StepCursor(cur, count int, k event.KeyEvent) (next int, handled bool) {
	cur = ClampCursor(cur, count)
	switch {
	case k.MatchString("up", "k", "ctrl+p"):
		return max(0, cur-1), true
	case k.MatchString("down", "j", "ctrl+n"):
		if count > 0 {
			return min(count-1, cur+1), true
		}
		return 0, true
	default:
		return cur, false
	}
}

// UseCursor manages a bounded list cursor inside a kitex/trail component.
func UseCursor(count, initial int) Cursor {
	raw, setRaw := kitex.UseState(initial)
	idx := ClampCursor(raw(), count)

	set := func(i int) {
		setRaw(ClampCursor(i, count))
	}

	return Cursor{
		Index: idx,
		Set:   set,
		Move: func(delta int) {
			set(idx + delta)
		},
		HandleKey: func(k event.KeyEvent) bool {
			next, handled := StepCursor(idx, count, k)
			if handled && next != idx {
				setRaw(next)
			}
			return handled
		},
	}
}
