package cli

import (
	"fmt"
	"image/color"
	"strings"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/components"
	"github.com/masterkeysrd/tasksmith/internal/ui/format"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

// ExitSummaryInput carries everything the post-REPL goodbye block renders.
type ExitSummaryInput struct {
	SessionID string
	Usage     api.SessionUsage
	// Quota is optional; when set the cost row renders remaining quota
	// instead of dollar cost.
	Quota *api.ModelQuota
	// Color enables ANSI true-color output; disable for pipes and NO_COLOR.
	Color bool
}

const (
	exitLabelWidth = 8
	// exitLogoWidth matches the pre-padded width of components.LogoLines().
	exitLogoWidth = 13
)

// FormatExitSummary renders the goodbye block: the ASCII brand mark in the
// left gutter beside aligned Changes, Cost/Quota, Tokens, and Resume rows.
// It is plain-string output (the app runtime has already stopped), so colors
// come from the static theme snapshot rather than the reactive hook.
func FormatExitSummary(in ExitSummaryInput) string {
	if in.SessionID == "" {
		return ""
	}

	scheme := theme.Get()
	paint := func(c color.Color, s string) string {
		if !in.Color {
			return s
		}
		return ansiForeground(c, s)
	}
	label := func(s string) string {
		return paint(scheme.Color.Text.Muted, fmt.Sprintf("%-*s", exitLabelWidth, s))
	}

	logoLines, logoColors := components.LogoLines()
	const gutter = "   "

	rows := exitRows(in, scheme, paint, label)

	var b strings.Builder
	lines := max(len(logoLines), len(rows))
	for i := range lines {
		var left string
		if i < len(logoLines) {
			left = paint(logoColors[i], logoLines[i])
		} else {
			left = strings.Repeat(" ", exitLogoWidth)
		}
		var right string
		if i < len(rows) {
			right = rows[i]
		}
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(left + gutter + right)
	}
	return b.String()
}

type paintFunc func(color.Color, string) string

func exitRows(in ExitSummaryInput, scheme *theme.Scheme, paint paintFunc, label func(string) string) []string {
	u := in.Usage

	changes := fmt.Sprintf("+%d", u.Changes.LinesAdded)
	changes = paint(scheme.Color.Diff.AddedText, changes)
	removed := fmt.Sprintf(" -%d", u.Changes.LinesRemoved)
	changes += paint(scheme.Color.Diff.RemovedText, removed)
	if u.Changes.Files > 0 {
		changes += paint(scheme.Color.Text.Muted, fmt.Sprintf(" (%d files)", u.Changes.Files))
	}

	costLabel, costValue := exitCostRow(in, scheme, paint)

	tokens := fmt.Sprintf("↑ %s", format.Tokens(int(u.Totals.Tokens.Input)))
	if u.Totals.Tokens.CacheRead > 0 {
		cached := fmt.Sprintf(" (%s cached", format.Tokens(int(u.Totals.Tokens.CacheRead)))
		if u.Totals.Tokens.CacheWrite > 0 {
			cached += fmt.Sprintf(", %s written", format.Tokens(int(u.Totals.Tokens.CacheWrite)))
		}
		cached += ")"
		tokens += paint(scheme.Color.Text.Muted, cached)
	}
	tokens += fmt.Sprintf(" • ↓ %s", format.Tokens(int(u.Totals.Tokens.Output)))

	resume := fmt.Sprintf("tasksmith --session %s", in.SessionID)

	return []string{
		label("Changes") + changes,
		label(costLabel) + costValue,
		label("Tokens") + tokens,
		label("Resume") + resume,
	}
}

func exitCostRow(in ExitSummaryInput, scheme *theme.Scheme, paint paintFunc) (string, string) {
	duration := format.DurationMilis(in.Usage.Totals.DurationMS)

	if q := in.Quota; q != nil {
		pct := q.RemainingFraction * 100
		value := fmt.Sprintf("%.0f%%", pct)
		if !q.ResetAt.IsZero() {
			until := q.ResetAt.Sub(time.Now().UTC())
			value += paint(scheme.Color.Text.Muted, fmt.Sprintf(" (↻ %s)", format.Duration(until)))
		}
		switch {
		case pct <= 0:
			value = paint(scheme.Color.Text.Error, value)
		case pct < 20:
			value = paint(scheme.Color.Text.Warning, value)
		}
		return "Quota", value + paint(scheme.Color.Text.Muted, fmt.Sprintf(" (%s)", duration))
	}

	allUnpriced := in.Usage.Totals.UnpricedCalls > 0 && in.Usage.Totals.UnpricedCalls == in.Usage.Totals.Calls
	if in.Usage.Totals.Calls == 0 || allUnpriced {
		return "Cost", paint(scheme.Color.Text.Muted, fmt.Sprintf("— (%s)", duration))
	}
	cost := format.FormatCostNano(in.Usage.Totals.CostNano, in.Usage.Totals.UnpricedCalls > 0)
	return "Cost", cost + paint(scheme.Color.Text.Muted, fmt.Sprintf(" (%s)", duration))
}

func ansiForeground(c color.Color, s string) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm%s\x1b[0m", r>>8, g>>8, b>>8, s)
}
