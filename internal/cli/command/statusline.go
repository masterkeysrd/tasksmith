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
		Description: "Configure the REPL status line segments, rows, and compact mode",
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

type statusLineSegmentLabel struct {
	id          store.StatusLineSegmentID
	description string
}

// statusLineSegmentLabels describes every known segment for the editor view.
var statusLineSegmentLabels = []statusLineSegmentLabel{
	{store.StatusLineHint, "shortcut hint / cancel prompt"},
	{store.StatusLineMode, "permission mode badge"},
	{store.StatusLineModel, "active model"},
	{store.StatusLineEffort, "thinking effort"},
	{store.StatusLineWarnings, "status warnings"},
	{store.StatusLineContext, "context window usage"},
	{store.StatusLineCost, "session cost or remaining quota"},
	{store.StatusLineTokens, "session input and output tokens"},
}

func statusLineEditorSegments(rows [][]store.StatusLineSegmentID) []store.StatusLineSegmentID {
	segments := make([]store.StatusLineSegmentID, 0, len(statusLineSegmentLabels))
	for _, row := range rows {
		segments = append(segments, row...)
	}
	for _, segment := range statusLineSegmentLabels {
		if !slices.Contains(segments, segment.id) {
			segments = append(segments, segment.id)
		}
	}
	return segments
}

func statusLineSegmentPosition(rows [][]store.StatusLineSegmentID, id store.StatusLineSegmentID) (row, index int) {
	for r, segments := range rows {
		if i := slices.Index(segments, id); i >= 0 {
			return r, i
		}
	}
	return -1, -1
}

// StatusLineEditor renders an interactive status line configuration view
// inside <Slot />. Edits apply live (the rendered status line below previews
// them); s saves to statusline.json, esc discards and restores.
var StatusLineEditor = trail.FC("StatusLineEditor", func(slotID string) trail.Node {
	scheme := theme.UseTheme()

	original := kitex.UseRef(store.GetStatusLine())
	issues := kitex.UseRef(store.GetStatusLineDropped())
	draft, setDraft := kitex.UseState(original.Current)
	all := statusLineEditorSegments(draft().Rows)
	cursor := uicomponents.UseCursor(len(all), 0)

	trail.UseInput(func(_ string, k event.KeyEvent) {
		if cursor.HandleKey(k) {
			return
		}
		id := all[cursor.Index]
		cfg := draft()
		cfg.Rows = slices.Clone(cfg.Rows)
		row, index := statusLineSegmentPosition(cfg.Rows, id)
		switch {
		case k.MatchString("esc", "escape"):
			if _, err := store.ApplyStatusLine(original.Current); err != nil {
				store.AppendError("/statusline", "Failed to restore status line: "+err.Error())
				return
			}
			store.RemoveSlot(slotID)
			store.AppendCommandOutput("/statusline", "Exited /statusline without saving.")
			return
		case k.MatchString("space", "enter", "t"):
			if row < 0 {
				defaultRow, _ := statusLineSegmentPosition(store.DefaultStatusLineRows, id)
				cfg.Rows[defaultRow] = append(slices.Clone(cfg.Rows[defaultRow]), id)
			} else {
				if len(cfg.Rows[0])+len(cfg.Rows[1]) == 1 {
					return
				}
				cfg.Rows[row] = slices.Delete(slices.Clone(cfg.Rows[row]), index, index+1)
			}
		case k.MatchString("left", "h", "right", "l"):
			if row < 0 {
				return
			}
			target := index + 1
			if k.MatchString("left", "h") {
				target = index - 1
			}
			if target < 0 || target >= len(cfg.Rows[row]) {
				return
			}
			segments := slices.Clone(cfg.Rows[row])
			segments[index], segments[target] = segments[target], segments[index]
			cfg.Rows[row] = segments
		case k.MatchString("r"):
			if row < 0 {
				return
			}
			other := 1 - row
			cfg.Rows[row] = slices.Delete(slices.Clone(cfg.Rows[row]), index, index+1)
			cfg.Rows[other] = append(slices.Clone(cfg.Rows[other]), id)
		case k.MatchString("c"):
			cfg.Compact = !cfg.Compact
		case k.MatchString("s"):
			dropped, err := store.SaveStatusLine(cfg)
			if err != nil {
				store.AppendError("/statusline", "Failed to save statusline.json: "+err.Error())
				return
			}
			msg := "Status line saved to statusline.json."
			if len(dropped) > 0 {
				msg += " Ignored entries: " + strings.Join(dropped, ", ") + "."
			}
			store.RemoveSlot(slotID)
			store.AppendCommandOutput("/statusline", msg)
			return
		default:
			return
		}
		if _, err := store.ApplyStatusLine(cfg); err != nil {
			store.AppendError("/statusline", "Failed to preview status line: "+err.Error())
			return
		}
		setDraft(cfg)
		cursor.Set(slices.Index(statusLineEditorSegments(cfg.Rows), id))
	})

	options := make([]uicomponents.Option, 0, len(all))
	for _, id := range all {
		i := slices.IndexFunc(statusLineSegmentLabels, func(segment statusLineSegmentLabel) bool {
			return segment.id == id
		})
		options = append(options, uicomponents.Option{
			Value: string(id),
			Cells: []string{string(id), statusLineSegmentLabels[i].description},
		})
	}

	titleStyle := style.S().Bold(true).Foreground(scheme.Color.Text.Primary)
	compactStyle := style.S().Foreground(scheme.Color.Mode.Plan)
	var diagnostics trail.Node
	if len(issues.Current) > 0 {
		diagnostics = trail.Box(trail.BoxProps{}, trail.Span(trail.SpanProps{
			Style: style.S().Foreground(scheme.Color.Text.Warning).OverflowWrap(style.OverflowWrapAnywhere),
		}, trail.Text("Ignored entries: "+strings.Join(issues.Current, ", "))))
	}
	return trail.Box(trail.BoxProps{
		Style: style.S().FlexDirection(style.FlexColumn).Width(style.Percent(100)),
	},
		trail.Box(trail.BoxProps{Style: titleStyle}, trail.Text("Status line configuration")),
		diagnostics,
		uicomponents.OptionList(uicomponents.OptionListProps{
			Options:  options,
			Selected: cursor.Index,
			Columns:  []uicomponents.Column{{Width: 12}, {}},
			GroupLabel: func(opt uicomponents.Option) string {
				row, _ := statusLineSegmentPosition(draft().Rows, store.StatusLineSegmentID(opt.Value))
				if row < 0 {
					return "Disabled"
				}
				return fmt.Sprintf("Row %d", row+1)
			},
		}),
		trail.Box(trail.BoxProps{Style: compactStyle}, trail.Text(fmt.Sprintf("compact: %s (c to toggle)", onOff(draft().Compact)))),
		uicomponents.KeyboardHint(uicomponents.KeyboardHintProps{
			Bindings: []uicomponents.KeyBinding{
				{Key: "↑/↓", Label: "Select"},
				{Key: "Space", Label: "Toggle segment"},
				{Key: "←/→", Label: "Reorder in row"},
				{Key: "r", Label: "Move to other row"},
				{Key: "C", Label: "Compact"},
				{Key: "S", Label: "Save"},
				{Key: "Esc", Label: "Discard & exit"},
			},
		}),
	)
})

func onOff(v bool) string {
	if v {
		return "on"
	}
	return "off"
}
