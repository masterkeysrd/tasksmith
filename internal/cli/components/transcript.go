package components

import (
	"fmt"
	"time"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
)

func renderTranscriptItem(item store.TranscriptItem, _ int) trail.Node {
	switch item.Kind {
	case store.TranscriptKindHeader:
		if d, ok := item.Data.(store.HeaderData); ok {
			return Header(HeaderProps{HeaderData: d})
		}
	case store.TranscriptKindMessage:
		if m, ok := item.Data.(store.Message); ok {
			return Message(MessageProps{Message: m})
		}
	case store.TranscriptKindTimeline:
		if e, ok := item.Data.(api.TimelineEntry); ok {
			return TimelineEvent(e)
		}
	case store.TranscriptKindNotice:
		if d, ok := item.Data.(store.NoticeData); ok {
			return Notice(d)
		}
	case store.TranscriptKindError:
		if d, ok := item.Data.(store.ErrorData); ok {
			return ErrorBanner(d)
		}
	case store.TranscriptKindCommand:
		if d, ok := item.Data.(store.CommandData); ok {
			return CommandOutput(d)
		}
	case store.TranscriptKindContextUsage:
		if d, ok := item.Data.(store.ContextUsageData); ok {
			return ContextUsage(d)
		}
	default:
		return nil
	}
	return nil
}

var transcriptResizeDebounce = 150 * time.Millisecond

var afterTranscriptResize = func(d time.Duration, fn func()) func() {
	t := time.AfterFunc(d, fn)
	return func() { t.Stop() }
}

type transcriptGeneration struct {
	epoch, redraw int
}

type transcriptResizeSnapshot struct {
	width         int
	epoch         int
	hasScrollback bool
}

// Transcript is the host component that prints the header and finalized items.
// In draft mode (!Frozen), Header and draft command outputs remain live children.
// When Frozen, the snapshot Header and items are frozen into trail.Static and flushed into scrollback.
var Transcript = trail.SimpleFC("Transcript", func() trail.Node {
	app := trail.UseApp()
	liveHeader := store.UseHeaderData()
	epoch := store.UseTranscript(func(s store.TranscriptState) int {
		return s.Epoch
	})
	frozen := store.UseTranscript(func(s store.TranscriptState) bool {
		return s.Frozen
	})
	itemsPtr := store.UseTranscript(func(s store.TranscriptState) *[]store.TranscriptItem {
		return s.Items
	})

	var items []store.TranscriptItem
	if itemsPtr != nil {
		items = *itemsPtr
	}
	width := trail.UseWindowSize().Width
	hasScrollback := frozen && len(items) > 0
	lastWidth := kitex.UseRef(0)
	latest := kitex.UseRef(transcriptResizeSnapshot{})
	latest.Current = transcriptResizeSnapshot{
		width:         width,
		epoch:         epoch,
		hasScrollback: hasScrollback,
	}
	mounted := kitex.UseRef(true)
	kitex.UseEffectCleanup(func() func() {
		mounted.Current = true
		return func() { mounted.Current = false }
	}, []any{})

	redrawGen, setRedrawGen := kitex.UseState(0)
	generation := transcriptGeneration{epoch, redrawGen()}
	cleared, setCleared := kitex.UseState(generation)

	kitex.UseLayoutEffect(func() {
		if generation == cleared() {
			return
		}
		lastWidth.Current = width
		if app != nil {
			app.Clear()
			app.Engine().Post(func() { setCleared(generation) })
		} else {
			setCleared(generation)
		}
	}, []any{generation})

	kitex.UseEffectCleanup(func() func() {
		if !hasScrollback || lastWidth.Current == 0 {
			lastWidth.Current = width
			return nil
		}
		if width <= 0 || width == lastWidth.Current || app == nil {
			return nil
		}
		eng := app.Engine()
		targetWidth := width
		targetEpoch := epoch
		cancelled := false
		stopTimer := afterTranscriptResize(transcriptResizeDebounce, func() {
			eng.Post(func() {
				if cancelled || !mounted.Current {
					return
				}
				cur := latest.Current
				if !cur.hasScrollback || cur.width != targetWidth || cur.epoch != targetEpoch || lastWidth.Current == targetWidth {
					return
				}
				setRedrawGen(redrawGen() + 1)
			})
		})
		return func() {
			cancelled = true
			stopTimer()
		}
	}, []any{width, hasScrollback, epoch})

	var children []trail.Node
	if generation == cleared() {
		if !frozen {
			children = make([]trail.Node, 0, len(items)+1)
			children = append(children, Header(HeaderProps{HeaderData: liveHeader, AnimateMascot: true}))
			for i, item := range items {
				if n := renderTranscriptItem(item, i); n != nil {
					children = append(children, n)
				}
			}
		} else {
			if len(items) > 0 {
				children = []trail.Node{trail.Static(items, renderTranscriptItem)}
			}
		}
	}

	return trail.Box(trail.BoxProps{
		Key: fmt.Sprintf("transcript-%d-%d", cleared().epoch, cleared().redraw),
		Style: style.S().
			FlexDirection(style.FlexColumn).
			Width(style.Percent(100)),
	}, children...)
})
