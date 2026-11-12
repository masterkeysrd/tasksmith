package command

import (
	"fmt"
	"slices"
	"strings"

	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	uicomponents "github.com/masterkeysrd/tasksmith/internal/ui/components"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

func (r *Registry) registerStatusLineCommands() {
	r.Register(Command{
		Name:        "statusline",
		Aliases:     []string{"status"},
		Usage:       "/statusline",
		Description: "Configure the REPL status line segments, order, and compact mode",
		Handler:     handleStatusLine,
	})
}

func handleStatusLine(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("/statusline does not accept arguments; edit %s directly to configure it non-interactively", "statusline.json")
	}
	store.EnqueueSlot(store.SlotItem{
		Kind: store.SlotKindPicker,
		Data: StatusLineEditor,
	})
	return nil
}

// statusLineSegmentLabels describes every known segment for the editor view.
var statusLineSegmentLabels = []struct {
	id          store.StatusLineSegmentID
	label       string
	description string
}{
	{store.StatusLineHint, "hint", "shortcut hint / cancel prompt"},
	{store.StatusLineMode, "mode", "permission mode badge"},
	{store.StatusLineContext, "context", "context window usage"},
	{store.StatusLineUsage, "usage", "session cost and tokens"},
	{store.StatusLineModel, "model", "model and thinking effort"},
}

// StatusLineEditor renders an interactive status line configuration view
// inside <Slot />. Edits apply live (the rendered status line below previews
// them); s saves to statusline.json, esc discards and restores.
var StatusLineEditor = trail.FC("StatusLineEditor", func(slotID string) trail.Node {
	scheme := theme.UseTheme()

	original := kitex.UseRef(store.GetStatusLine())
	draft, setDraft := kitex.UseState(original.Current)
	cursor, setCursor := kitex.UseState(0)

	// Apply the draft live so the real status line previews the edits.
	kitex.UseEffect(func() {
		_, _ = store.ApplyStatusLine(draft())
	}, []any{draft()})

	// rows lists enabled segments in order followed by disabled ones.
	rows := func() []store.StatusLineSegmentID {
		enabled := draft().Segments
		var disabled []store.StatusLineSegmentID
		for _, seg := range statusLineSegmentLabels {
			if !slices.Contains(enabled, seg.id) {
				disabled = append(disabled, seg.id)
			}
		}
		return append(slices.Clone(enabled), disabled...)
	}

	labelFor := func(id store.StatusLineSegmentID) (string, string) {
		for _, seg := range statusLineSegmentLabels {
			if seg.id == id {
				return seg.label, seg.description
			}
		}
		return string(id), ""
	}

	trail.UseInput(func(_ string, k event.KeyEvent) {
		all := rows()
		cur := min(cursor(), len(all)-1)
		switch {
		case k.MatchString("esc", "escape"):
			_, _ = store.ApplyStatusLine(original.Current)
			store.RemoveSlot(slotID)
			store.AppendCommandOutput("/statusline", "Exited /statusline without saving.")
		case k.MatchString("up", "k"):
			if cur > 0 {
				setCursor(cur - 1)
			}
		case k.MatchString("down", "j"):
			if cur < len(all)-1 {
				setCursor(cur + 1)
			}
		case k.MatchString("space", "enter", "t"):
			id := all[cur]
			cfg := draft()
			if i := slices.Index(cfg.Segments, id); i >= 0 {
				cfg.Segments = slices.Delete(slices.Clone(cfg.Segments), i, i+1)
			} else {
				cfg.Segments = append(slices.Clone(cfg.Segments), id)
			}
			if len(cfg.Segments) == 0 {
				break
			}
			setDraft(cfg)
			setCursor(min(cur, len(statusLineSegmentLabels)-1))
		case k.MatchString("left", "h"):
			cfg := draft()
			if i := slices.Index(cfg.Segments, all[cur]); i > 0 {
				cfg.Segments = slices.Clone(cfg.Segments)
				cfg.Segments[i-1], cfg.Segments[i] = cfg.Segments[i], cfg.Segments[i-1]
				setDraft(cfg)
				setCursor(cur - 1)
			}
		case k.MatchString("right", "l"):
			cfg := draft()
			if i := slices.Index(cfg.Segments, all[cur]); i >= 0 && i < len(cfg.Segments)-1 {
				cfg.Segments = slices.Clone(cfg.Segments)
				cfg.Segments[i], cfg.Segments[i+1] = cfg.Segments[i+1], cfg.Segments[i]
				setDraft(cfg)
				setCursor(cur + 1)
			}
		case k.MatchString("c"):
			cfg := draft()
			cfg.Compact = !cfg.Compact
			_, _ = store.ApplyStatusLine(cfg)
			setDraft(cfg)
		case k.MatchString("s"):
			dropped, err := store.SaveStatusLine(draft())
			if err != nil {
				store.AppendError("/statusline", "Failed to save statusline.json: "+err.Error())
				return
			}
			msg := "Status line saved to statusline.json."
			if len(dropped) > 0 {
				msg += " Ignored unknown segments: " + strings.Join(dropped, ", ") + "."
			}
			store.RemoveSlot(slotID)
			store.AppendCommandOutput("/statusline", msg)
		}
	})

	titleStyle := style.S().Bold(true).Foreground(scheme.Color.Text.Primary)
	enabledStyle := style.S().Foreground(scheme.Color.Text.Primary)
	disabledStyle := style.S().Foreground(scheme.Color.Text.Muted)
	cursorStyle := style.S().Foreground(scheme.Color.Icon.Primary)
	compactStyle := style.S().Foreground(scheme.Color.Mode.Plan)

	all := rows()
	cur := min(cursor(), len(all)-1)

	var nodes []trail.Node
	nodes = append(nodes, trail.Box(trail.BoxProps{Style: titleStyle},
		trail.Text("Status line configuration"),
	))
	for i, id := range all {
		label, desc := labelFor(id)
		enabled := slices.Contains(draft().Segments, id)
		rowStyle := disabledStyle
		if enabled {
			rowStyle = enabledStyle
		}
		marker := "  "
		if i == cur {
			marker = "> "
		}
		prefix := "   "
		if enabled {
			prefix = fmt.Sprintf("%d. ", slices.Index(draft().Segments, id)+1)
		}
		suffix := ""
		if !enabled {
			suffix = " (disabled)"
		}
		nodes = append(nodes, trail.Box(trail.BoxProps{},
			trail.Span(trail.SpanProps{Style: cursorStyle}, trail.Text(marker)),
			trail.Span(trail.SpanProps{Style: style.S().Foreground(scheme.Color.Text.Muted)}, trail.Text(prefix)),
			trail.Span(trail.SpanProps{Style: rowStyle},
				trail.Text(fmt.Sprintf("%-8s %s%s", label, desc, suffix))),
		))
	}
	nodes = append(nodes, trail.Box(trail.BoxProps{Style: compactStyle},
		trail.Text(fmt.Sprintf("compact: %s (c to toggle)", onOff(draft().Compact))),
	))
	nodes = append(nodes, uicomponents.KeyboardHint(uicomponents.KeyboardHintProps{
		Bindings: []uicomponents.KeyBinding{
			{Key: "↑/↓", Label: "Move"},
			{Key: "Space", Label: "Toggle segment"},
			{Key: "←/→", Label: "Reorder"},
			{Key: "C", Label: "Compact"},
			{Key: "S", Label: "Save"},
			{Key: "Esc", Label: "Discard & exit"},
		},
	}))

	return trail.Box(trail.BoxProps{
		Style: style.S().FlexDirection(style.FlexColumn).Width(style.Percent(100)),
	}, nodes...)
})

func onOff(v bool) string {
	if v {
		return "on"
	}
	return "off"
}
