package components

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
	"github.com/masterkeysrd/tasksmith/internal/ui/format"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

func OpenWarnings() {
	store.EnqueueSlot(store.SlotItem{
		ID:   "slot-warnings",
		Kind: store.SlotKindPicker,
		Data: warningsView,
	})
}

const warningNavigationKeys = "↑/↓, j/k, Ctrl+P/N"

func warningPreview(warning store.Warning) (title, body, fix string) {
	message := warning.Message
	if warning.Source == "MCP" {
		if i := strings.LastIndex(message, " — run /mcp"); i >= 0 {
			fix = message[i+len(" — run "):]
			message = message[:i]
		}
		if heading, reason, ok := strings.Cut(message, " ("); ok {
			return heading, strings.TrimSuffix(reason, ")"), fix
		}
		if strings.HasSuffix(message, "requires authentication") {
			body = "Authorization is required before the server can connect."
		}
		return message, body, fix
	}
	if first, rest, ok := strings.Cut(message, "\n"); ok {
		return first, rest, ""
	}
	if subject, reason, ok := strings.Cut(message, ": "); ok {
		switch {
		case strings.HasPrefix(subject, `provider "`) && strings.HasPrefix(reason, "missing required environment variable "):
			return "Provider " + strings.TrimPrefix(subject, "provider ") + " is missing configuration", reason, "Set the required variable. See /providers for provider configuration."
		case strings.HasPrefix(subject, `provider "`) && strings.Contains(reason, " — run "):
			body, fix, _ = strings.Cut(reason, " — run ")
			return subject, body, fix
		case strings.HasPrefix(subject, `provider "`) && strings.Contains(reason, " — "):
			body, fix, _ = strings.Cut(reason, " — ")
			return subject, body, fix
		case strings.HasPrefix(strings.ToLower(subject), "skill ") && strings.Contains(strings.ToLower(reason), "front matter"):
			return subject + " could not load", reason, "Correct the front matter and reload the workspace."
		default:
			return subject, reason, ""
		}
	}
	return message, "", ""
}

func warningPreviewOption(warning store.Warning, width int, selected bool, scheme *theme.Scheme) uicomponents.Option {
	title, body, fix := warningPreview(warning)
	titleStyle := style.S().Bold(true).Foreground(scheme.Color.Text.Primary)
	if selected {
		titleStyle = titleStyle.Foreground(scheme.Color.Icon.Primary)
	}
	line := func(text string, sty style.Style) trail.Node {
		return trail.Box(trail.BoxProps{}, trail.Span(trail.SpanProps{Style: sty.WhiteSpace(style.WhiteSpacePre)},
			trail.Text(format.CompactSingleLine(text, width))))
	}
	lines := []trail.Node{line(title, titleStyle)}
	if body != "" {
		lines = append(lines, line(body, style.S().Foreground(scheme.Color.Text.Secondary)))
	}
	if fix != "" {
		lines = append(lines, line("Fix: "+fix, style.S().Foreground(scheme.Color.Text.Accent)))
	}
	return uicomponents.Option{
		Cells: []string{"", ""},
		Icon:  trail.Span(trail.SpanProps{Style: style.S().Foreground(scheme.Color.Text.Warning)}, trail.Text("⚠")),
		CellNodes: []trail.Node{nil, trail.Box(trail.BoxProps{
			Style: style.S().FlexDirection(style.FlexColumn).MarginBottom(1),
		}, lines...)},
	}
}

var warningsView = trail.FC("Warnings", func(slotID string) trail.Node {
	scheme := theme.UseTheme()
	window := trail.UseWindowSize()
	warnings := store.UseWarnings()
	cursor := uicomponents.UseCursor(len(warnings), 0)
	selected, setSelected := kitex.UseState(store.Warning{})
	detail := selected()
	inspecting := slices.Contains(warnings, detail)

	kitex.UseEffect(func() {
		if !inspecting {
			setSelected(store.Warning{})
		}
	}, []any{inspecting})

	trail.UseInput(func(_ string, k event.KeyEvent) {
		if cursor.HandleKey(k) {
			return
		}
		switch {
		case k.MatchString("enter") && len(warnings) > 0:
			setSelected(warnings[cursor.Index])
		case k.MatchString("esc", "escape"):
			store.RemoveSlot(slotID)
			store.AppendCommandOutput("/warnings", "Exited /warnings command")
		}
	}, trail.InputOptions{IsActive: !inspecting})

	title := trail.Span(trail.SpanProps{
		Style: style.S().Bold(true).Foreground(scheme.Color.Text.Warning),
	}, trail.Text(fmt.Sprintf("Warnings · %d unresolved", len(warnings))))
	if inspecting {
		return uicomponents.ScrollViewer(uicomponents.ScrollViewerProps{
			Header: []trail.Node{title},
			Meta: []trail.Node{trail.Span(trail.SpanProps{
				Style: style.S().Foreground(scheme.Color.Text.Muted),
			}, trail.Text("Source: "+detail.Source))},
			MaxHeight: max(1, min(20, window.Height-6)),
			Bindings: []uicomponents.KeyBinding{
				{Key: warningNavigationKeys, Label: "Scroll"},
				{Key: "esc", Label: "Back"},
			},
			OnClose: func() { setSelected(store.Warning{}) },
		}, trail.Span(trail.SpanProps{
			Style: style.S().WhiteSpace(style.WhiteSpacePreWrap).OverflowWrap(style.OverflowWrapAnywhere),
		}, trail.Text(detail.Message)))
	}

	options := make([]uicomponents.Option, 0, len(warnings))
	for i, warning := range warnings {
		options = append(options, warningPreviewOption(warning, max(1, window.Width-6), i == cursor.Index, scheme))
	}
	bindings := []uicomponents.KeyBinding{{Key: "esc", Label: "Close"}}
	if len(options) > 0 {
		bindings = []uicomponents.KeyBinding{
			{Key: warningNavigationKeys, Label: "Navigate"},
			{Key: "enter", Label: "Details"},
			{Key: "esc", Label: "Close"},
		}
	}
	return trail.Box(trail.BoxProps{Style: style.S().FlexDirection(style.FlexColumn)},
		title,
		uicomponents.OptionList(uicomponents.OptionListProps{
			Options:    options,
			Selected:   cursor.Index,
			EmptyText:  "No unresolved warnings.",
			Columns:    []uicomponents.Column{{Width: 4}, {}},
			MaxVisible: max(1, min(uicomponents.DefaultPickerMaxVisible, (window.Height-6)/4)),
		}),
		uicomponents.KeyboardHint(uicomponents.KeyboardHintProps{Bindings: bindings}),
	)
})
