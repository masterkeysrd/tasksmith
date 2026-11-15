package components

import (
	"time"

	"github.com/masterkeysrd/kite/extras/kitex"
)

// quotaClockInterval bounds how stale a rendered quota countdown can get. The
// badge shows at best hour-and-minute precision, so a half-minute tick keeps
// it honest without re-rendering the whole status line more than needed.
const quotaClockInterval = 30 * time.Second

// useQuotaClock returns a wall-clock time that advances on a fixed interval.
// Countdowns derived from it keep ticking between daemon events, which only
// arrive after a turn ends.
func useQuotaClock() time.Time {
	now, setNow := kitex.UseState(time.Now())
	kitex.UseInterval(func() {
		setNow(time.Now())
	}, quotaClockInterval, nil)
	return now()
}
