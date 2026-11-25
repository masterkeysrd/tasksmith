package components

import (
	"fmt"
	"image/color"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/ui/format"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

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

type statusField struct {
	full    string
	compact string
	color   color.Color
	bold    bool
}

type statusPart struct {
	id store.StatusLineSegmentID
	statusField
	text string
}

func statusContext(ctx api.ContextState, scheme *theme.Scheme) statusField {
	col := scheme.Color.Text.Secondary
	if ctx.MaxTokens == 0 {
		return statusField{}
	}
	if ctx.TriggerTokens > 0 {
		switch {
		case ctx.UsedTokens >= ctx.TriggerTokens:
			col = scheme.Color.Text.Error
		case ctx.UsedTokens >= (ctx.TriggerTokens*3)/4:
			col = scheme.Color.Text.Warning
		}
	}
	pct := fmt.Sprintf("%d%%", min(int(float64(ctx.UsedTokens)/float64(ctx.MaxTokens)*100), 100))
	return statusField{
		full:    fmt.Sprintf("Context %s/%s (%s)", format.Tokens(ctx.UsedTokens), format.Tokens(ctx.MaxTokens), pct),
		compact: pct,
		color:   col,
	}
}

func statusCost(usage store.UsageTotals, quota store.QuotaState, billed bool, now time.Time, scheme *theme.Scheme) statusField {
	col := scheme.Color.Text.Secondary
	if !billed {
		if usage.Calls == 0 {
			return statusField{}
		}
		cost := format.FormatCostNano(usage.CostNano, usage.UnpricedCalls > 0)
		return statusField{full: "Cost " + cost, compact: cost, color: col}
	}
	if !quota.Known {
		return statusField{}
	}
	if quota.Unlimited {
		return statusField{full: "Quota ∞", compact: "∞", color: col}
	}
	switch {
	case quota.RemainingPct <= 0:
		col = scheme.Color.Text.Error
	case quota.RemainingPct < 20:
		col = scheme.Color.Text.Warning
	}
	pct := fmt.Sprintf("%d%%", quota.RemainingPct)
	full := "Quota " + pct
	if until := quota.ResetAt.Sub(now); until > 0 {
		full += " (↻ " + format.Duration(until, format.Compact()) + ")"
	}
	return statusField{full: full, compact: pct, color: col}
}

func statusWarningCount(count int, scheme *theme.Scheme) statusField {
	if count == 0 {
		return statusField{}
	}
	short := fmt.Sprintf("⚠ %d", count)
	label := " warnings"
	if count == 1 {
		label = " warning"
	}
	return statusField{full: short + label, compact: short, color: scheme.Color.Text.Warning}
}

func planStatusRow(width int, compact bool, row []store.StatusLineSegmentID, fields map[store.StatusLineSegmentID]statusField, rightSeparator string) ([]statusPart, int) {
	parts := make([]statusPart, 0, len(row))
	for _, id := range row {
		field := fields[id]
		if field.full == "" {
			continue
		}
		text := field.full
		if compact && field.compact != "" {
			text = field.compact
		}
		parts = append(parts, statusPart{id: id, statusField: field, text: text})
	}
	gap := 2
	total := func() int {
		cells, visible := 0, 0
		for _, part := range parts {
			if part.text != "" {
				cells += ansi.StringWidth(part.text)
				visible++
			}
		}
		if rightSeparator != "" && visible > 1 {
			if parts[0].id == row[0] && parts[0].text != "" {
				return cells + gap + (visible-2)*ansi.StringWidth(rightSeparator)
			}
			return cells + (visible-1)*ansi.StringWidth(rightSeparator)
		}
		return cells + max(visible-1, 0)*gap
	}
	if width > 0 && total() > width {
		gap = 1
		for _, id := range []store.StatusLineSegmentID{
			store.StatusLineModel, store.StatusLineWarnings, store.StatusLineContext, store.StatusLineCost, store.StatusLineTokens, store.StatusLineMode,
		} {
			for i := range parts {
				if total() <= width {
					break
				}
				if p := &parts[i]; p.id == id && p.compact != "" && ansi.StringWidth(p.compact) < ansi.StringWidth(p.text) {
					p.text = p.compact
				}
			}
		}
		for _, id := range []store.StatusLineSegmentID{
			store.StatusLineEffort, store.StatusLineHint, store.StatusLineModel,
			store.StatusLineTokens, store.StatusLineCost, store.StatusLineContext,
		} {
			for i := range parts {
				if total() <= width {
					break
				}
				p := &parts[i]
				if p.id != id {
					continue
				}
				room := width - (total() - ansi.StringWidth(p.text))
				if (id == store.StatusLineHint || id == store.StatusLineModel) && room >= 8 {
					p.text = ansi.Truncate(p.text, room, "…")
				} else {
					p.text = ""
				}
			}
		}
		if total() > width {
			for i := range parts {
				p := &parts[i]
				if p.id == store.StatusLineMode {
					room := width - (total() - ansi.StringWidth(p.text))
					p.text = ansi.Truncate(p.text, min(width, max(room, 1)), "…")
					break
				}
			}
			if total() > width {
				for i := range parts {
					p := &parts[i]
					if p.id == store.StatusLineWarnings {
						room := width - (total() - ansi.StringWidth(p.text))
						if room < ansi.StringWidth(p.text) {
							p.text = ""
						}
						break
					}
				}
			}
		}
	}
	visible := parts[:0]
	for _, p := range parts {
		if p.text != "" {
			visible = append(visible, p)
		}
	}
	return visible, gap
}

func renderStatusRow(width, index int, compact bool, row []store.StatusLineSegmentID, fields map[store.StatusLineSegmentID]statusField, canOpenWarnings bool) trail.Node {
	rightSeparator := ""
	if index > 0 {
		rightSeparator = " · "
	}
	parts, gap := planStatusRow(width, compact, row, fields, rightSeparator)
	if len(parts) == 0 {
		return nil
	}
	rightFrom := 0
	if parts[0].id == row[0] {
		rightFrom = 1
	}
	rowStyle := style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(gap)
	rightStyle := rowStyle
	if rightSeparator != "" {
		rightStyle = rightStyle.Gap(0)
	}
	children := make([]trail.Node, 0, len(parts))
	for i, part := range parts {
		if i > rightFrom && rightSeparator != "" {
			children = append(children, trail.Span(trail.SpanProps{
				Style: style.S().WhiteSpace(style.WhiteSpacePre),
			}, trail.Text(rightSeparator)))
		}
		props := trail.SpanProps{Style: style.S().WhiteSpace(style.WhiteSpacePre).Foreground(part.color).Bold(part.bold)}
		if part.id == store.StatusLineWarnings && canOpenWarnings {
			props.OnClick = func(event.Event) { OpenWarnings() }
		}
		children = append(children, trail.Span(props, trail.Text(part.text)))
	}
	fullWidth := rowStyle.Width(style.Percent(100)).JustifyContent(style.JustifyBetween)
	if rightFrom == 0 {
		return trail.Box(trail.BoxProps{Style: rightStyle.Width(style.Percent(100)).JustifyContent(style.JustifyEnd)}, children...)
	}
	if len(children) == 1 {
		return trail.Box(trail.BoxProps{Style: fullWidth}, children[0])
	}
	right := trail.Box(trail.BoxProps{Style: rightStyle}, children[rightFrom:]...)
	return trail.Box(trail.BoxProps{Style: fullWidth}, children[0], right)
}

var DefaultStatusLine = trail.SimpleFC("DefaultStatusLine", func() trail.Node {
	scheme := theme.UseTheme()
	width := trail.UseWindowSize().Width
	cfg := store.UseStatusLine()
	ctrlCPending := store.UseApp(func(s store.AppState) bool { return s.CtrlCPending })
	hint := store.UseApp(func(s store.AppState) string { return s.Hint })
	hasSlot := store.UseHasActiveSlot()
	mode := store.UseMode()
	ctx := store.UseContext()
	usage := store.UseUsageTotals()
	quota := store.UseQuota()
	billed := store.UseSession(func(s store.SessionState) bool { return s.ModelQuotaBilled })
	quotaNow := useQuotaClock()
	ready := store.UseSession(func(s store.SessionState) bool { return s.Ready })
	model := store.UseSession(store.SessionState.ModelLabels)
	effort := store.UseSession(func(s store.SessionState) string { return s.Thinking })
	warningCount := len(store.UseWarnings())

	switch {
	case ctrlCPending:
		hint = store.CtrlCHint
	case hasSlot:
		hint = ""
	}
	secondary := scheme.Color.Text.Secondary
	fields := map[store.StatusLineSegmentID]statusField{
		store.StatusLineHint:     {full: hint, color: secondary},
		store.StatusLineCost:     statusCost(usage, quota, billed, quotaNow, scheme),
		store.StatusLineWarnings: statusWarningCount(warningCount, scheme),
	}
	if mode != "" {
		text := "[" + strings.ReplaceAll(string(mode), "_", " ") + "]"
		short := text
		if mode == api.ModeAcceptEdits {
			short = "[edits]"
		}
		fields[store.StatusLineMode] = statusField{full: text, compact: short, color: modeColor(scheme, mode), bold: true}
	}
	if ready {
		fields[store.StatusLineContext] = statusContext(ctx, scheme)
		fields[store.StatusLineModel] = statusField{full: model.Full, compact: model.Compact, color: secondary}
		effort = strings.Join(strings.Fields(effort), " ")
		if effort == "" || effort == store.ThinkingProviderDefault {
			effort = "default effort"
		}
		fields[store.StatusLineEffort] = statusField{full: effort, color: secondary}
	}
	if usage.Calls > 0 {
		text := fmt.Sprintf("↑%s ↓%s", format.Tokens(int(usage.InputTokens)), format.Tokens(int(usage.OutputTokens)))
		fields[store.StatusLineTokens] = statusField{full: "Tokens " + text, compact: text, color: secondary}
	}

	rows := make([]trail.Node, 0, len(cfg.Rows))
	for i, row := range cfg.Rows {
		if node := renderStatusRow(width, i, cfg.Compact, row, fields, !hasSlot); node != nil {
			rows = append(rows, node)
		}
	}
	if len(rows) == 0 {
		return nil
	}
	return trail.Box(trail.BoxProps{Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Width(style.Percent(100))}, rows...)
})
