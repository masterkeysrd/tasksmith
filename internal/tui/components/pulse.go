package components

import (
	"strings"
	"time"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
)

// LoopStyle defines how the programmatically generated animations repeat.
type LoopStyle string

const (
	// LoopReset snaps back to the first frame immediately after reaching the end.
	LoopReset LoopStyle = "reset"

	// LoopBreathe plays the animation forward, then backward, creating a smooth breathing effect.
	LoopBreathe LoopStyle = "breathe"
)

// PulseProps defines the properties for the Pulse component.
type PulseProps struct {
	// Frames allows passing fully custom frame strings directly.
	// If set, it bypasses the auto-generation from Stages/Count.
	Frames []string

	// Stages represents the character progression for a single cell (e.g. empty to full).
	// Example: []string{"○", "⊙", "◎", "◉", "●"}
	// If Count > 0 and Stages is provided, we programmatically generate a staggered transition.
	Stages []string

	// Count is the number of cells/dots.
	// If Stages is empty, this defaults to the classic growing dot pulse.
	Count int

	// LoopStyle controls how the animation repeats (only used for Stages generation).
	// Defaults to LoopReset.
	LoopStyle LoopStyle

	// Interval specifies the duration between frame updates.
	// Defaults to 250ms if <= 0.
	Interval time.Duration

	// Style allows passing layout, padding, color, or margin overrides.
	Style style.Style
}

// Pulse renders a self-animated text indicator that breathes or pulses.
var Pulse = kitex.FC("Pulse", func(props PulseProps) kitex.Node {
	// Resolve fallback values
	count := props.Count
	if count <= 0 {
		count = 3
	}

	interval := props.Interval
	if interval <= 0 {
		interval = 250 * time.Millisecond
	}

	// Refs to cache the generated frames across renders
	framesRef := kitex.UseRef[[]string](nil)
	lastStagesRef := kitex.UseRef[[]string](nil)
	lastCountRef := kitex.UseRef(0)
	lastLoopStyleRef := kitex.UseRef[LoopStyle]("")
	lastFramesRef := kitex.UseRef[[]string](nil)

	// Helper to check if slices are identical to detect dependency changes
	slicesEqual := func(a, b []string) bool {
		if len(a) != len(b) {
			return false
		}
		for i := range a {
			if a[i] != b[i] {
				return false
			}
		}
		return true
	}

	// Regenerate only if frames are uninitialized or inputs have changed
	needsRegen := framesRef.Current == nil ||
		props.Count != lastCountRef.Current ||
		props.LoopStyle != lastLoopStyleRef.Current ||
		!slicesEqual(props.Stages, lastStagesRef.Current) ||
		!slicesEqual(props.Frames, lastFramesRef.Current)

	if needsRegen {
		// Update cache tracking refs
		lastStagesRef.Current = props.Stages
		lastCountRef.Current = props.Count
		lastLoopStyleRef.Current = props.LoopStyle
		lastFramesRef.Current = props.Frames

		// Resolve or generate the animation frames
		if len(props.Frames) > 0 {
			framesRef.Current = props.Frames
		} else if len(props.Stages) >= 2 {
			framesRef.Current = generateStaggeredFrames(props.Stages, count, props.LoopStyle)
		} else {
			// Classic growing dot pulse frames (length: count + 1)
			frames := make([]string, count+1)
			for i := 0; i < count; i++ {
				frames[i] = strings.Repeat("●", i+1) + strings.Repeat(" ", count-(i+1))
			}
			frames[count] = strings.Repeat(" ", count)
			framesRef.Current = frames
		}
	}

	frames := framesRef.Current
	frameCount := len(frames)
	currentFrame, setCurrentFrame := kitex.UseState(0)

	// Animation interval hook
	kitex.UseInterval(func() {
		if frameCount > 0 {
			setCurrentFrame((currentFrame() + 1) % frameCount)
		}
	}, interval, []any{frameCount, interval})

	if frameCount == 0 {
		return nil
	}

	// Clamp the current frame index defensively in case frames list changed length
	activeFrameIdx := currentFrame()
	if activeFrameIdx >= frameCount {
		activeFrameIdx = 0
	}

	return kitex.Box(kitex.BoxProps{Style: props.Style},
		kitex.Text(frames[activeFrameIdx]),
	)
})

// generateStaggeredFrames builds a sequential transition where each cell is filled one after another.
func generateStaggeredFrames(stages []string, count int, loopStyle LoopStyle) []string {
	if len(stages) < 2 || count <= 0 {
		return nil
	}
	empty := stages[0]
	full := stages[len(stages)-1]

	var frames []string

	// Helper to build a single frame string joining cells with space
	buildFrame := func(dotIdx int, stageIdx int) string {
		parts := make([]string, count)
		for i := range count {
			if i < dotIdx {
				parts[i] = full
			} else if i == dotIdx {
				parts[i] = stages[stageIdx]
			} else {
				parts[i] = empty
			}
		}
		return strings.Join(parts, " ")
	}

	// 1. Add the initial fully-empty frame
	frames = append(frames, buildFrame(0, 0))

	// 2. Forward path: loop through each dot and fill it stage-by-stage
	for d := range count {
		for s := 1; s < len(stages); s++ {
			frames = append(frames, buildFrame(d, s))
		}
	}

	// 3. Optional reverse path for breathing effect
	if loopStyle == LoopBreathe {
		// Go backwards from second-to-last frame down to the second frame
		for i := len(frames) - 2; i > 0; i-- {
			frames = append(frames, frames[i])
		}
	}

	return frames
}
