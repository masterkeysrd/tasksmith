package components

import (
	"image/color"
	"time"

	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

const (
	logoLine1 = " ◥██████████◣"
	logoLine2 = "     ▐██▌    "
	logoLine3 = "    ◢████◣   "
)

// LogoLines returns the resting mascot rows paired with their per-row colors
// for callers rendering the mark outside the component tree, such as the
// post-REPL exit summary.
func LogoLines() ([]string, []color.Color) {
	colors := theme.BrandMarkColors()
	rows := mascotRows[mascotReady]
	return rows[:], []color.Color{colors.Top, colors.Center, colors.Base}
}

type mascotPose uint8

const (
	mascotReady mascotPose = iota
	mascotT
	mascotMorph
	mascotBlink
	mascotLeft
	mascotRight
)

const (
	mascotHelmet   = "  ▜████████▛ "
	mascotBoots    = "    ▟▛▀▀▜▙   "
	mascotIdleStep = 2
	mascotIdleWait = 4 * time.Second
)

var mascotRows = [...][3]string{
	mascotReady: {mascotHelmet, "     █▜▛█    ", mascotBoots},
	mascotT:     {logoLine1, logoLine2, logoLine3},
	mascotMorph: {mascotHelmet, logoLine2, mascotBoots},
	mascotBlink: {mascotHelmet, "     ████    ", mascotBoots},
	mascotLeft:  {mascotHelmet, "     ▛█▜█    ", mascotBoots},
	mascotRight: {mascotHelmet, "     █▛█▜    ", mascotBoots},
}

var mascotMark = trail.FC("MascotMark", func(pose mascotPose) trail.Node {
	scheme := theme.UseTheme()
	rows := mascotRows[pose]
	row := func(text string, fg color.Color) trail.Node {
		return trail.Span(trail.SpanProps{
			Style: style.S().WhiteSpace(style.WhiteSpacePre).Foreground(fg),
		}, trail.Text(text))
	}

	colors := scheme.Color.BrandMark
	return trail.Box(trail.BoxProps{
		Style: style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexColumn).
			MarginRight(3),
	}, row(rows[0], colors.Top), row(rows[1], colors.Center), row(rows[2], colors.Base))
})

var mascotAnimation = [...]struct {
	pose  mascotPose
	delay time.Duration
}{
	{mascotT, 400 * time.Millisecond},
	{mascotMorph, 300 * time.Millisecond},
	{mascotReady, mascotIdleWait},
	{mascotBlink, 140 * time.Millisecond},
	{mascotReady, 700 * time.Millisecond},
	{mascotLeft, 350 * time.Millisecond},
	{mascotRight, 350 * time.Millisecond},
}

type mascotAnimationState struct {
	step     int
	activity uint64
}

var animatedMascot = trail.FC("AnimatedMascot", func(idleWait time.Duration) trail.Node {
	getState, setState := kitex.UseState(mascotAnimationState{})
	state := getState()
	frame := mascotAnimation[state.step]
	if state.step == mascotIdleStep {
		frame.delay = idleWait
	}

	onInput := func() {
		setState(mascotAnimationState{step: mascotIdleStep, activity: getState().activity + 1})
	}
	trail.UseInput(func(_ string, _ event.KeyEvent) { onInput() })

	doc := kitex.UseDocument()
	kitex.UseEffectCleanup(func() func() {
		if d := doc(); d != nil {
			sub := d.AddEventListener(event.EventPaste, func(event.Event) { onInput() }, event.Capture())
			return sub.Cancel
		}
		return nil
	}, []any{})

	kitex.UseTimeout(func() {
		if getState() != state {
			return
		}
		next := state.step + 1
		if next == len(mascotAnimation) {
			next = mascotIdleStep
		}
		setState(mascotAnimationState{step: next, activity: state.activity})
	}, frame.delay, []any{state})

	return mascotMark(frame.pose)
})
