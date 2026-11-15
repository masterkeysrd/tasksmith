package components

import (
	"cmp"
	"fmt"
	"image/color"
	"strings"

	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/core/strutils"
	"github.com/masterkeysrd/tasksmith/internal/ui/format"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

// StatusLineProps defines the properties for the StatusLine layout.
type StatusLineProps struct {
	Children []trail.Node
}

// SegmentProps defines the properties shared by status segments.
type SegmentProps struct {
	// Separator is rendered after the segment, only when the segment itself renders.
	Separator string
	// Text overrides the segment content. When empty, segments that derive
	// their text from stores compute it themselves.
	Text string
}

// statusSeparator joins adjacent segments on the default status line.
const statusSeparator = " · "

// statusEllipsis marks hint text shortened to fit the terminal width.
const statusEllipsis = "…"

// hintEllipsisBudget keeps at least this many cells of the hint visible when
// truncating, so a squeezed hint still conveys its first word.
const hintEllipsisBudget = 8

func modeColor(scheme *theme.Scheme, mode api.PermissionMode) color.Color {
	switch mode {
	case api.ModeAcceptEdits:
		return scheme.Color.Mode.AcceptEdits
	case api.ModePlan:
		return scheme.Color.Mode.Plan
	case api.ModeAsk:
		return scheme.Color.Mode.Ask
	case api.ModeAuto:
		return scheme.Color.Mode.Auto
	default:
		return scheme.Color.Mode.Default
	}
}

// statusText renders text in the given color.
func statusText(text string, col color.Color, bold bool) trail.Node {
	spanStyle := style.S().WhiteSpace(style.WhiteSpacePre).Foreground(col)
	if bold {
		spanStyle = spanStyle.Bold(true)
	}
	return trail.Span(trail.SpanProps{Style: spanStyle}, trail.Text(text))
}

// segment appends the separator to a rendered segment and hides empty ones.
func segment(props SegmentProps, content trail.Node, text string, sepColor color.Color) trail.Node {
	if text == "" {
		return nil
	}
	if props.Separator == "" {
		return content
	}
	return trail.Box(trail.BoxProps{},
		content,
		statusText(props.Separator, sepColor, false),
	)
}

// contextText renders full context usage (e.g. "109.5k/400.0k (27%)").
func contextText(ctx api.ContextState) string {
	if ctx.MaxTokens == 0 {
		return ""
	}
	pct := min(int(float64(ctx.UsedTokens)/float64(ctx.MaxTokens)*100), 100)
	return fmt.Sprintf("%s/%s (%d%%)", format.Tokens(ctx.UsedTokens), format.Tokens(ctx.MaxTokens), pct)
}

// contextTextCompact renders only the usage percentage (e.g. "27%").
func contextTextCompact(ctx api.ContextState) string {
	if ctx.MaxTokens == 0 {
		return ""
	}
	pct := min(int(float64(ctx.UsedTokens)/float64(ctx.MaxTokens)*100), 100)
	return fmt.Sprintf("%d%%", pct)
}

// usageText renders the session cost with input and output tokens
// (e.g. "$0.42 · ↑128.4k ↓8.3k"). A leading "≥" marks costs that are a lower
// bound because some calls had no pricing.
func usageText(usage store.UsageTotals) string {
	if usage.Calls == 0 {
		return ""
	}
	return fmt.Sprintf("%s · ↑%s ↓%s", format.FormatCostNano(usage.CostNano, usage.UnpricedCalls > 0),
		format.Tokens(int(usage.InputTokens)), format.Tokens(int(usage.OutputTokens)))
}

// modelText renders "<model> · <thinking>".
func modelText(modelLabel, thinking string) string {
	model := cmp.Or(strings.TrimSpace(modelLabel), "default")
	effort := cmp.Or(strings.TrimSpace(thinking), store.ThinkingProviderDefault)
	return fmt.Sprintf("%s · %s", model, effort)
}

// modelTextCompact renders only the model name.
func modelTextCompact(modelLabel string) string {
	return cmp.Or(strings.TrimSpace(modelLabel), "default")
}

// statusPlan holds the visible text of every configured segment after the
// responsive planner applied its fixed hiding ladder for the current width.
// A segment absent from the configuration never appears here.
type statusPlan struct {
	Texts map[store.StatusLineSegmentID]string
}

// statusSegment is one configured segment with its full and compact text
// variants as measured for the current state.
type statusSegment struct {
	id       store.StatusLineSegmentID
	full     string
	compact  string
	resolved string
}

// planStatusLine decides the visible text of each configured segment so the
// line fits width cells. Segments outside the configuration are excluded;
// within it, the fixed ladder compacts the model, drops usage, ellipsizes
// then drops the hint, and compacts then drops context. The permission mode
// is never hidden. A non-positive width keeps everything visible, matching
// unknown-width terminals. When compact is true, every segment starts from
// its compact variant.
func planStatusLine(width int, compact bool, segments []store.StatusLineSegmentID, hint, mode, ctxFull, ctxCompact, usage, modelFull, modelCompact string) statusPlan {
	textFor := func(id store.StatusLineSegmentID) (string, string) {
		switch id {
		case store.StatusLineHint:
			return hint, ""
		case store.StatusLineMode:
			return mode, mode
		case store.StatusLineContext:
			return ctxFull, ctxCompact
		case store.StatusLineUsage:
			return usage, usage
		case store.StatusLineModel:
			return modelFull, modelCompact
		}
		return "", ""
	}

	planSegs := make([]statusSegment, 0, len(segments))
	for _, id := range segments {
		full, comp := textFor(id)
		resolved := full
		if compact && comp != "" {
			resolved = comp
		}
		planSegs = append(planSegs, statusSegment{id: id, full: full, compact: comp, resolved: resolved})
	}

	out := func() statusPlan {
		texts := make(map[store.StatusLineSegmentID]string, len(planSegs))
		for _, s := range planSegs {
			texts[s.id] = s.resolved
		}
		return statusPlan{Texts: texts}
	}

	if width <= 0 {
		return out()
	}
	total := func() int {
		sum := 0
		parts := 0
		for _, s := range planSegs {
			if s.resolved == "" {
				continue
			}
			if parts > 0 {
				sum += len([]rune(statusSeparator))
			}
			sum += len([]rune(s.resolved))
			parts++
		}
		return sum
	}
	if total() <= width {
		return out()
	}

	// Fixed ladder, cheapest information first. Only the permission mode is
	// protected: it must stay visible because it is safety-relevant.
	steps := []store.StatusLineSegmentID{
		store.StatusLineModel,
		store.StatusLineUsage,
		store.StatusLineHint,
		store.StatusLineContext,
	}
	for _, id := range steps {
		for i := range planSegs {
			if planSegs[i].id != id {
				continue
			}
			seg := &planSegs[i]
			if seg.resolved == "" {
				break
			}
			if id == store.StatusLineHint {
				room := width - (total() - len([]rune(seg.resolved)))
				if room >= hintEllipsisBudget {
					if trunc, did := strutils.TruncateLine(seg.resolved, room-1, statusEllipsis); did {
						seg.resolved = trunc
					}
					if total() <= width {
						return out()
					}
				}
			} else if len([]rune(seg.compact)) < len([]rune(seg.resolved)) {
				seg.resolved = seg.compact
				if total() <= width {
					return out()
				}
			}
			seg.resolved = ""
			if total() <= width {
				return out()
			}
		}
	}
	return out()
}

// HintSegment shows the shortcut hint or Ctrl+C exit confirmation from store.App.
var HintSegment = trail.FC("HintSegment", func(props SegmentProps) trail.Node {
	scheme := theme.UseTheme()
	ctrlCPending := store.UseApp(func(s store.AppState) bool {
		return s.CtrlCPending
	})
	hint := store.UseApp(func(s store.AppState) string {
		return s.Hint
	})
	hasSlot := store.UseHasActiveSlot()

	text := props.Text
	if text == "" {
		text = hint
	}
	if ctrlCPending {
		text = store.CtrlCHint
	}
	if !ctrlCPending && hasSlot {
		return nil
	}
	return segment(props, statusText(text, scheme.Color.Text.Secondary, false), text, scheme.Color.Text.Secondary)
})

// ModeSegment shows a colored badge for non-default permission modes.
var ModeSegment = trail.FC("ModeSegment", func(props SegmentProps) trail.Node {
	scheme := theme.UseTheme()
	mode := store.UseMode()
	text := props.Text
	if text == "" {
		if mode == "" || mode == api.ModeDefault {
			return nil
		}
		text = string(mode)
	}
	return segment(props, statusText(text, modeColor(scheme, mode), true), text, scheme.Color.Text.Secondary)
})

// ContextSegment shows compact context usage (e.g. "109.5k/400.0k (27%)") once session context is known.
var ContextSegment = trail.FC("ContextSegment", func(props SegmentProps) trail.Node {
	scheme := theme.UseTheme()
	ctx := store.UseContext()
	text := props.Text
	if text == "" {
		text = contextText(ctx)
	}
	if text == "" {
		return nil
	}

	col := scheme.Color.Text.Secondary
	if ctx.MaxTokens > 0 && ctx.TriggerTokens > 0 {
		if ctx.UsedTokens >= ctx.TriggerTokens {
			col = scheme.Color.Text.Error
		} else if ctx.UsedTokens >= (ctx.TriggerTokens*3)/4 {
			col = scheme.Color.Text.Warning
		}
	}

	return segment(props, statusText(text, col, false), text, scheme.Color.Text.Secondary)
})

// UsageSegment shows the session cost with input and output tokens (e.g. "$0.42 · ↑128.4k ↓8.3k")
// once the session has recorded LLM calls. A leading "≥" marks costs that are
// a lower bound because some calls had no pricing.
var UsageSegment = trail.FC("UsageSegment", func(props SegmentProps) trail.Node {
	scheme := theme.UseTheme()
	usage := store.UseUsageTotals()
	text := props.Text
	if text == "" {
		text = usageText(usage)
	}
	if text == "" {
		return nil
	}
	return segment(props, statusText(text, scheme.Color.Text.Secondary, false), text, scheme.Color.Text.Secondary)
})

// ModelSegment shows "<model> · <thinking>" once session model info is ready.
var ModelSegment = trail.FC("ModelSegment", func(props SegmentProps) trail.Node {
	scheme := theme.UseTheme()
	ready := store.UseSession(func(s store.SessionState) bool {
		return s.Ready
	})
	modelLabel := store.UseSession(func(s store.SessionState) string {
		return s.ModelLabel
	})
	thinking := store.UseSession(func(s store.SessionState) string {
		return s.Thinking
	})
	text := props.Text
	if text == "" {
		if !ready {
			return nil
		}
		text = modelText(modelLabel, thinking)
	}
	if text == "" {
		return nil
	}
	return segment(props, statusText(text, scheme.Color.Text.Secondary, false), text, scheme.Color.Text.Secondary)
})

// StatusLine lays its children out horizontally across the full width.
var StatusLine = trail.FCC("StatusLine", func(props StatusLineProps) trail.Node {
	return trail.Box(trail.BoxProps{
		Style: style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexRow).
			AlignItems(style.AlignCenter).
			Width(style.Percent(100)),
	}, props.Children...)
})

// DefaultStatusLine renders the configured REPL status line. The first
// configured segment sits on the far left, the rest are right-aligned after a
// spacer. On narrow terminals planStatusLine compacts or hides low-priority
// segments so nothing collides or clips.
var DefaultStatusLine = trail.SimpleFC("DefaultStatusLine", func() trail.Node {
	winSize := trail.UseWindowSize()
	cfg := store.UseStatusLine()

	ctrlCPending := store.UseApp(func(s store.AppState) bool {
		return s.CtrlCPending
	})
	hint := store.UseApp(func(s store.AppState) string {
		return s.Hint
	})
	hasSlot := store.UseHasActiveSlot()
	mode := store.UseMode()
	ctx := store.UseContext()
	usage := store.UseUsageTotals()
	ready := store.UseSession(func(s store.SessionState) bool {
		return s.Ready
	})
	modelLabel := store.UseSession(func(s store.SessionState) string {
		return s.ModelLabel
	})
	thinking := store.UseSession(func(s store.SessionState) string {
		return s.Thinking
	})

	hintText := hint
	if ctrlCPending {
		hintText = store.CtrlCHint
	}
	if hasSlot && !ctrlCPending {
		hintText = ""
	}

	modeStr := ""
	if mode != "" && mode != api.ModeDefault {
		modeStr = string(mode)
	}

	var ctxFull, ctxCompact, modelFull, modelCompact string
	if ready {
		ctxFull = contextText(ctx)
		ctxCompact = contextTextCompact(ctx)
		modelFull = modelText(modelLabel, thinking)
		modelCompact = modelTextCompact(modelLabel)
	}

	plan := planStatusLine(winSize.Width, cfg.Compact, cfg.Segments, hintText, modeStr, ctxFull, ctxCompact, usageText(usage), modelFull, modelCompact)

	if len(cfg.Segments) == 0 {
		return StatusLine(StatusLineProps{})
	}

	renderSegment := func(id store.StatusLineSegmentID, sep string) trail.Node {
		text := plan.Texts[id]
		if text == "" {
			return nil
		}
		props := SegmentProps{
			Text:      text,
			Separator: sep,
		}
		switch id {
		case store.StatusLineHint:
			return HintSegment(props)
		case store.StatusLineMode:
			return ModeSegment(props)
		case store.StatusLineContext:
			return ContextSegment(props)
		case store.StatusLineUsage:
			return UsageSegment(props)
		case store.StatusLineModel:
			return ModelSegment(props)
		}
		return nil
	}

	var leftNode trail.Node
	if len(cfg.Segments) > 0 {
		leftNode = renderSegment(cfg.Segments[0], "")
	}

	var rightVisible []store.StatusLineSegmentID
	if len(cfg.Segments) > 1 {
		for _, id := range cfg.Segments[1:] {
			if plan.Texts[id] != "" {
				rightVisible = append(rightVisible, id)
			}
		}
	}

	children := make([]trail.Node, 0, len(cfg.Segments)+1)
	if leftNode != nil {
		children = append(children, leftNode)
	}

	if len(rightVisible) > 0 {
		children = append(children, trail.Spacer())
		for i, id := range rightVisible {
			sep := ""
			if i < len(rightVisible)-1 {
				sep = statusSeparator
			}
			if n := renderSegment(id, sep); n != nil {
				children = append(children, n)
			}
		}
	}
	return StatusLine(StatusLineProps{}, children...)
})
