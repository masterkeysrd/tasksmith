package components

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

// ActionMenuItem defines one selectable row in an [ActionMenu].
type ActionMenuItem struct {
	ID         string
	Label      string
	Shortcut   string
	Badge      string
	BadgeStyle *style.Style
}

// ActionMenuProps defines the properties for the [ActionMenu] component.
type ActionMenuProps struct {
	Items           []ActionMenuItem
	InitialIndex    int
	Selected        *int
	OnChange        func(index int)
	OnSelect        func(index int, item ActionMenuItem)
	Numbered        bool
	Indent          int
	Disabled        bool
	UnselectedStyle *style.Style
}

// MenuDigit maps an unmodified '1'..'9' key press to its 1-based menu index,
// returning 0 for any other key.
func MenuDigit(k key.Key) int {
	if k.Mod != 0 {
		return 0
	}
	text := k.Text
	if text == "" && k.Code >= '1' && k.Code <= '9' {
		text = string(k.Code)
	}
	if utf8.RuneCountInString(text) != 1 {
		return 0
	}
	digit, _ := utf8.DecodeRuneInString(text)
	if digit < '1' || digit > '9' {
		return 0
	}
	return int(digit - '0')
}

func matchMenuShortcut(input string, k event.KeyEvent, shortcut string) bool {
	if shortcut == "" {
		return false
	}
	if utf8.RuneCountInString(shortcut) == 1 {
		if k.Mod&^(key.ModShift|key.ModCapsLock) != 0 {
			return false
		}
		r, _ := utf8.DecodeRuneInString(shortcut)
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			return strings.EqualFold(input, shortcut) ||
				k.MatchString(strings.ToLower(shortcut), strings.ToUpper(shortcut))
		}
		return input == shortcut || k.MatchString(shortcut)
	}
	return k.MatchString(shortcut)
}

func actionMenuDisplayLabel(index int, item ActionMenuItem, numbered bool) string {
	if numbered {
		return fmt.Sprintf("%d. %s", index+1, item.Label)
	}
	return item.Label
}

// ActionMenu is an embeddable interactive selection menu supporting arrow/vi
// navigation, optional numbering, per-item letter shortcuts, and aligned badges.
var ActionMenu = trail.FC("ActionMenu", func(props ActionMenuProps) trail.Node {
	scheme := theme.UseTheme()
	cursor := UseCursor(len(props.Items), props.InitialIndex)

	cur := cursor.Index
	if props.Selected != nil {
		cur = ClampCursor(*props.Selected, len(props.Items))
	}

	setIndex := func(next int) {
		next = ClampCursor(next, len(props.Items))
		if props.Selected == nil {
			cursor.Set(next)
		}
		if next != cur && props.OnChange != nil {
			props.OnChange(next)
		}
	}

	trail.UseInput(func(input string, k event.KeyEvent) {
		if props.Disabled || len(props.Items) == 0 {
			return
		}

		for i, item := range props.Items {
			if matchMenuShortcut(input, k, item.Shortcut) {
				setIndex(i)
				if props.OnSelect != nil {
					props.OnSelect(i, item)
				}
				return
			}
		}

		if props.Numbered {
			if d := MenuDigit(k.Key); d >= 1 && d <= len(props.Items) {
				idx := d - 1
				setIndex(idx)
				if props.OnSelect != nil {
					props.OnSelect(idx, props.Items[idx])
				}
				return
			}
		}

		if next, handled := StepCursor(cur, len(props.Items), k); handled {
			if next != cur {
				setIndex(next)
			}
			return
		}

		if k.MatchString("enter") && props.OnSelect != nil {
			props.OnSelect(cur, props.Items[cur])
		}
	}, trail.InputOptions{IsActive: !props.Disabled})

	if len(props.Items) == 0 {
		return nil
	}

	selectedStyle := style.S().Bold(true).Foreground(scheme.Color.Icon.Primary).WhiteSpace(style.WhiteSpacePreWrap)
	unselectedStyle := style.S().Foreground(scheme.Color.Text.Primary).WhiteSpace(style.WhiteSpacePreWrap)
	if props.UnselectedStyle != nil {
		unselectedStyle = *props.UnselectedStyle
	}
	defaultBadgeStyle := style.S().Foreground(scheme.Color.Text.Muted).WhiteSpace(style.WhiteSpacePre)

	maxLabelWidth := 0
	for i, item := range props.Items {
		if item.Badge == "" {
			continue
		}
		w := utf8.RuneCountInString(actionMenuDisplayLabel(i, item, props.Numbered))
		if w > maxLabelWidth {
			maxLabelWidth = w
		}
	}

	indentStr := ""
	if props.Indent > 0 {
		indentStr = strings.Repeat(" ", props.Indent)
	}

	rows := make([]trail.Node, 0, len(props.Items))
	for i, item := range props.Items {
		displayLabel := actionMenuDisplayLabel(i, item, props.Numbered)
		prefix := "  "
		rowStyle := unselectedStyle
		prefixStyle := unselectedStyle.WhiteSpace(style.WhiteSpacePre)
		if i == cur {
			prefix = "❯ "
			rowStyle = selectedStyle
			prefixStyle = selectedStyle.WhiteSpace(style.WhiteSpacePre)
		}

		var spans []trail.Node
		if item.Badge != "" {
			padLen := max(maxLabelWidth-utf8.RuneCountInString(displayLabel)+3, 1)
			padding := strings.Repeat(" ", padLen)
			badgeSty := defaultBadgeStyle
			if item.BadgeStyle != nil {
				badgeSty = item.BadgeStyle.WhiteSpace(style.WhiteSpacePre)
			}
			spans = append(spans,
				trail.Span(trail.SpanProps{Style: prefixStyle}, trail.Text(indentStr+prefix)),
				trail.Span(trail.SpanProps{Style: rowStyle.WhiteSpace(style.WhiteSpacePre)}, trail.Text(displayLabel+padding)),
				trail.Span(trail.SpanProps{Style: badgeSty}, trail.Text(item.Badge)),
			)
		} else {
			spans = append(spans,
				trail.Span(trail.SpanProps{Style: prefixStyle}, trail.Text(indentStr+prefix)),
				trail.Span(trail.SpanProps{Style: rowStyle}, trail.Text(displayLabel)),
			)
		}

		rows = append(rows, trail.Box(trail.BoxProps{
			Style: style.S().FlexDirection(style.FlexRow).Width(style.Percent(100)),
		}, spans...))
	}

	return trail.Box(trail.BoxProps{
		Style: style.S().FlexDirection(style.FlexColumn).Width(style.Percent(100)),
	}, rows...)
})
