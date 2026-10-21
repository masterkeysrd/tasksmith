package ui

import (
	"cmp"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/masterkeysrd/tasksmith/internal/repl/theme"
)

// DefaultPickerStatusBar is the default navigation hint displayed at the bottom of a picker.
const DefaultPickerStatusBar = "↑/↓ or j/k to select • enter to confirm • esc to cancel"

// PickerCanceledMsg indicates that a picker interaction was dismissed without making a selection.
type PickerCanceledMsg struct{}

// Option represents a selectable item in an interactive command picker or inline argument filter.
type Option struct {
	Value       string
	Label       string
	Description string
	Current     bool
}

// Picker manages interactive selection for a slash command.
type Picker struct {
	Title     string
	Options   []Option
	StatusBar string
	OnSelect  func(opt Option) tea.Msg
	Keys      map[string]func(opt Option) tea.Msg
	cursor    int
}

// NewPicker constructs an initialized [Picker] with the cursor positioned at the
// currently active option, or 0 if none is marked current.
func NewPicker(title string, opts []Option, onSelect func(opt Option) tea.Msg) Picker {
	cursor := 0
	for i, opt := range opts {
		if opt.Current {
			cursor = i
			break
		}
	}

	return Picker{
		Title:     title,
		Options:   opts,
		StatusBar: DefaultPickerStatusBar,
		OnSelect:  onSelect,
		cursor:    cursor,
	}
}

// Cursor returns the currently highlighted option index.
func (p Picker) Cursor() int {
	return p.cursor
}

// SetCursor explicitly sets the cursor position, clamped within bounds.
func (p *Picker) SetCursor(c int) {
	if len(p.Options) == 0 {
		p.cursor = 0
		return
	}
	if c < 0 {
		c = 0
	}
	if c >= len(p.Options) {
		c = len(p.Options) - 1
	}
	p.cursor = c
}

// CursorUp moves the picker cursor up by one item.
func (p *Picker) CursorUp() {
	if p.cursor > 0 {
		p.cursor--
	}
}

// CursorDown moves the picker cursor down by one item.
func (p *Picker) CursorDown() {
	if p.cursor < len(p.Options)-1 {
		p.cursor++
	}
}

// Selected returns the currently highlighted [Option] and true, or false if empty.
func (p Picker) Selected() (Option, bool) {
	if len(p.Options) == 0 || p.cursor < 0 || p.cursor >= len(p.Options) {
		return Option{}, false
	}
	return p.Options[p.cursor], true
}

// Select invokes OnSelect for the currently highlighted option, or returns nil.
func (p Picker) Select() tea.Msg {
	if p.OnSelect == nil {
		return nil
	}
	opt, ok := p.Selected()
	if !ok {
		return nil
	}
	return p.OnSelect(opt)
}

// Update processes navigation and selection key events for the picker.
func (p Picker) Update(msg tea.Msg) (Picker, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return p, nil
	}

	keyStr := keyMsg.String()
	switch {
	case keyStr == "ctrl+c" || keyStr == "esc" || keyMsg.Code == tea.KeyEscape:
		return p, func() tea.Msg { return PickerCanceledMsg{} }

	case keyStr == "up" || keyStr == "k" || keyMsg.Code == tea.KeyUp:
		p.CursorUp()
		return p, nil

	case keyStr == "down" || keyStr == "j" || keyMsg.Code == tea.KeyDown:
		p.CursorDown()
		return p, nil

	case keyStr == "enter" || keyMsg.Code == tea.KeyEnter || keyStr == "ctrl+j" || keyMsg.Code == '\n' || keyMsg.Code == 10 || keyMsg.Code == 13:
		if ev := p.Select(); ev != nil {
			return p, func() tea.Msg { return ev }
		}
		return p, func() tea.Msg { return PickerCanceledMsg{} }

	default:
		if p.Keys != nil {
			if handler, found := p.Keys[keyStr]; found {
				if opt, ok := p.Selected(); ok {
					ev := handler(opt)
					if ev != nil {
						return p, func() tea.Msg { return ev }
					}
				}
			}
		}
	}
	return p, nil
}

// Render formats the picker title, option list, and navigation footer.
func (p Picker) Render() string {
	var rows []string
	if p.Title != "" {
		rows = append(rows, theme.PermHeaderStyle.Render(p.Title))
	}

	if optsBlock := FormatOptions(p.Options, p.cursor); optsBlock != "" {
		rows = append(rows, optsBlock)
	}

	hint := cmp.Or(p.StatusBar, DefaultPickerStatusBar)
	rows = append(rows, theme.StatusBarStyle.Render(hint))

	return strings.Join(rows, "\n")
}

// FormatOptions renders a list of [Option] rows with a highlighted cursor indicator.
func FormatOptions(opts []Option, cursor int) string {
	if len(opts) == 0 {
		return ""
	}

	rows := make([]string, len(opts))
	for i, opt := range opts {
		label := cmp.Or(opt.Label, opt.Value)
		desc := opt.Description
		if opt.Current {
			if desc != "" {
				desc += " • current"
			} else {
				desc = "current"
			}
		}

		if i == cursor {
			row := theme.OptionSelectedStyle.Render(fmt.Sprintf("❯ %-16s", label))
			if desc != "" {
				row += " " + theme.StatusBarStyle.Render(desc)
			}
			rows[i] = row
			continue
		}

		row := theme.OptionNormalStyle.Render(fmt.Sprintf("  %-16s", label))
		if desc != "" {
			row += " " + theme.StatusBarStyle.Render(desc)
		}
		rows[i] = row
	}

	return strings.Join(rows, "\n")
}
