package ui

import (
	"cmp"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
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
	Columns     []string
	Current     bool
}

// ConfirmAction defines an inline confirmation prompt triggered by a custom key.
type ConfirmAction struct {
	Prompt    func(opt Option) string
	OnConfirm func(opt Option) tea.Msg
}

// InputAction defines an inline text input prompt triggered by a custom key.
type InputAction struct {
	Prompt       string
	InitialValue func(opt Option) string
	OnSubmit     func(opt Option, value string) tea.Msg
}

type pickerMode int

const (
	pickerModeNormal pickerMode = iota
	pickerModeConfirm
	pickerModeInput
)

// Picker manages interactive selection for a slash command.
type Picker struct {
	Title          string
	Options        []Option
	StatusBar      string
	OnSelect       func(opt Option) tea.Msg
	Keys           map[string]func(opt Option) tea.Msg
	ConfirmActions map[string]ConfirmAction
	InputActions   map[string]InputAction
	Filterable     bool

	filterInput textinput.Model
	cursor      int
	width       int

	mode          pickerMode
	confirmPrompt string
	confirmAction ConfirmAction
	targetOpt     Option
	inputPrompt   string
	inputAction   InputAction
	actionInput   textinput.Model
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

	ti := textinput.New()
	ti.Prompt = "❯ "
	ti.Placeholder = "Type to filter..."
	ti.Focus()
	ti.SetWidth(max(80-4, 20))

	return Picker{
		Title:       title,
		Options:     opts,
		StatusBar:   DefaultPickerStatusBar,
		OnSelect:    onSelect,
		filterInput: ti,
		cursor:      cursor,
		width:       80,
	}
}

// FilterValue returns the current filter query string.
func (p Picker) FilterValue() string {
	return p.filterInput.Value()
}

// SetFilterValue sets the filter query string.
func (p *Picker) SetFilterValue(val string) {
	p.filterInput.SetValue(val)
}

// FilteredOptions returns the list of options matching the active filter query, or all options if none.
func (p Picker) FilteredOptions() []Option {
	query := strings.ToLower(strings.TrimSpace(p.filterInput.Value()))
	if !p.Filterable || query == "" {
		return p.Options
	}

	var matches []Option
	for _, opt := range p.Options {
		if strings.Contains(strings.ToLower(opt.Label), query) ||
			strings.Contains(strings.ToLower(opt.Value), query) ||
			strings.Contains(strings.ToLower(opt.Description), query) {
			matches = append(matches, opt)
			continue
		}
		matchedCol := false
		for _, col := range opt.Columns {
			if strings.Contains(strings.ToLower(col), query) {
				matchedCol = true
				break
			}
		}
		if matchedCol {
			matches = append(matches, opt)
		}
	}
	return matches
}

// Width returns the configured layout width for the picker.
func (p Picker) Width() int {
	return cmp.Or(p.width, 80)
}

// SetWidth sets the picker layout width.
func (p *Picker) SetWidth(w int) {
	if w > 0 {
		p.width = w
		p.filterInput.SetWidth(max(w-4, 20))
		p.actionInput.SetWidth(max(w-4, 20))
	}
}

// Cursor returns the currently highlighted option index.
func (p Picker) Cursor() int {
	return p.cursor
}

// SetCursor explicitly sets the cursor position, clamped within bounds of FilteredOptions().
func (p *Picker) SetCursor(c int) {
	filtered := p.FilteredOptions()
	if len(filtered) == 0 {
		p.cursor = 0
		return
	}
	if c < 0 {
		c = 0
	}
	if c >= len(filtered) {
		c = len(filtered) - 1
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
	filtered := p.FilteredOptions()
	if p.cursor < len(filtered)-1 {
		p.cursor++
	}
}

// Selected returns the currently highlighted [Option] and true, or false if empty.
func (p Picker) Selected() (Option, bool) {
	filtered := p.FilteredOptions()
	if len(filtered) == 0 || p.cursor < 0 || p.cursor >= len(filtered) {
		return Option{}, false
	}
	return filtered[p.cursor], true
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

// InConfirmMode reports whether the picker is displaying an inline action confirmation prompt.
func (p Picker) InConfirmMode() bool {
	return p.mode == pickerModeConfirm
}

// InInputMode reports whether the picker is displaying an inline action text input prompt.
func (p Picker) InInputMode() bool {
	return p.mode == pickerModeInput
}

func normalizePickerKey(msg tea.KeyPressMsg) string {
	keyStr := strings.ToLower(msg.String())
	if keyStr == "ctrl+j" || msg.Code == '\n' || msg.Code == 10 || msg.Code == 13 || msg.Code == tea.KeyEnter {
		return "enter"
	}
	if keyStr == "esc" || keyStr == "ctrl+c" || msg.Code == tea.KeyEscape {
		return "esc"
	}

	switch msg.Code {
	case tea.KeyF1:
		return "f1"
	case tea.KeyF2:
		return "f2"
	case tea.KeyF3:
		return "f3"
	case tea.KeyF4:
		return "f4"
	case tea.KeyF5:
		return "f5"
	case tea.KeyF6:
		return "f6"
	case tea.KeyF7:
		return "f7"
	case tea.KeyF8:
		return "f8"
	case tea.KeyF9:
		return "f9"
	case tea.KeyF10:
		return "f10"
	case tea.KeyF11:
		return "f11"
	case tea.KeyF12:
		return "f12"
	case tea.KeyUp:
		return "up"
	case tea.KeyDown:
		return "down"
	default:
		return keyStr
	}
}

// Update processes navigation and selection key events for the picker.
func (p Picker) Update(msg tea.Msg) (Picker, tea.Cmd) {
	if ws, ok := msg.(tea.WindowSizeMsg); ok {
		p.SetWidth(ws.Width)
		return p, nil
	}

	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return p, nil
	}

	normKey := normalizePickerKey(keyMsg)
	keyStr := keyMsg.String()

	switch p.mode {
	case pickerModeConfirm:
		switch {
		case normKey == "y" || normKey == "enter":
			p.mode = pickerModeNormal
			if p.confirmAction.OnConfirm != nil {
				if ev := p.confirmAction.OnConfirm(p.targetOpt); ev != nil {
					return p, func() tea.Msg { return ev }
				}
			}
			return p, nil
		case normKey == "n" || normKey == "esc" || keyStr == "ctrl+c":
			p.mode = pickerModeNormal
			return p, nil
		default:
			return p, nil
		}

	case pickerModeInput:
		switch {
		case normKey == "esc" || keyStr == "ctrl+c":
			p.mode = pickerModeNormal
			return p, nil
		case normKey == "enter" || keyMsg.Code == '\n' || keyMsg.Code == 10 || keyMsg.Code == 13:
			val := strings.TrimSpace(p.actionInput.Value())
			p.mode = pickerModeNormal
			if val != "" && p.inputAction.OnSubmit != nil {
				if ev := p.inputAction.OnSubmit(p.targetOpt, val); ev != nil {
					return p, func() tea.Msg { return ev }
				}
			}
			return p, nil
		default:
			var cmd tea.Cmd
			p.actionInput, cmd = p.actionInput.Update(keyMsg)
			return p, cmd
		}

	default: // pickerModeNormal
		switch {
		case normKey == "esc" || keyStr == "ctrl+c":
			return p, func() tea.Msg { return PickerCanceledMsg{} }

		case normKey == "up" || (!p.Filterable && (keyStr == "k" || keyStr == "ctrl+p")):
			p.CursorUp()
			return p, nil

		case normKey == "down" || (!p.Filterable && (keyStr == "j" || keyStr == "ctrl+n")):
			p.CursorDown()
			return p, nil

		case normKey == "enter" || keyMsg.Code == '\n' || keyMsg.Code == 10 || keyMsg.Code == 13:
			if ev := p.Select(); ev != nil {
				return p, func() tea.Msg { return ev }
			}
			return p, func() tea.Msg { return PickerCanceledMsg{} }

		default:
			// 1. Check ConfirmActions
			if act, found := p.ConfirmActions[normKey]; found {
				if opt, ok := p.Selected(); ok {
					p.mode = pickerModeConfirm
					p.targetOpt = opt
					p.confirmAction = act
					if act.Prompt != nil {
						p.confirmPrompt = act.Prompt(opt)
					} else {
						p.confirmPrompt = fmt.Sprintf("Confirm action on %q?", opt.Label)
					}
					return p, nil
				}
			}

			// 2. Check InputActions
			if act, found := p.InputActions[normKey]; found {
				if opt, ok := p.Selected(); ok {
					p.mode = pickerModeInput
					p.targetOpt = opt
					p.inputAction = act
					p.inputPrompt = act.Prompt
					initVal := ""
					if act.InitialValue != nil {
						initVal = act.InitialValue(opt)
					}
					ti := textinput.New()
					ti.Prompt = ""
					ti.SetValue(initVal)
					ti.CursorEnd()
					ti.Focus()
					ti.SetWidth(max(p.Width()-len(p.inputPrompt)-10, 20))
					p.actionInput = ti
					return p, nil
				}
			}

			// 3. Check Keys
			if handler, found := p.Keys[normKey]; found {
				if opt, ok := p.Selected(); ok {
					ev := handler(opt)
					if ev != nil {
						return p, func() tea.Msg { return ev }
					}
				}
				return p, nil
			}
			if handler, found := p.Keys[keyStr]; found {
				if opt, ok := p.Selected(); ok {
					ev := handler(opt)
					if ev != nil {
						return p, func() tea.Msg { return ev }
					}
				}
				return p, nil
			}

			// 4. Live filtering
			if p.Filterable {
				oldVal := p.filterInput.Value()
				var cmd tea.Cmd
				p.filterInput, cmd = p.filterInput.Update(keyMsg)
				if p.filterInput.Value() != oldVal {
					p.cursor = 0
				}
				return p, cmd
			}
		}
	}

	return p, nil
}

// Render formats the picker title, filter input, option list, and navigation footer.
func (p Picker) Render() string {
	var rows []string
	if p.Title != "" {
		rows = append(rows, theme.PermHeaderStyle.Render(p.Title))
	}

	if p.Filterable {
		rows = append(rows, p.filterInput.View())
	}

	filtered := p.FilteredOptions()
	if len(filtered) == 0 {
		rows = append(rows, theme.StatusBarStyle.Render("  No matching options."))
	} else if optsBlock := FormatOptions(filtered, p.cursor); optsBlock != "" {
		rows = append(rows, optsBlock)
	}

	rows = append(rows, "")

	var statusBar string
	switch p.mode {
	case pickerModeConfirm:
		statusBar = theme.PermHeaderStyle.Render(p.confirmPrompt) + " " + theme.StatusBarStyle.Render("(y to confirm • n/esc to cancel)")
	case pickerModeInput:
		statusBar = theme.PermHeaderStyle.Render(p.inputPrompt) + p.actionInput.View() + " " + theme.StatusBarStyle.Render("(enter to save • esc to cancel)")
	default:
		hint := cmp.Or(p.StatusBar, DefaultPickerStatusBar)
		statusBar = theme.StatusBarStyle.Render(hint)
	}
	rows = append(rows, statusBar)

	width := cmp.Or(p.width, 80)
	return theme.PickerTopBorderStyle.Width(width).Render(strings.Join(rows, "\n"))
}

// FormatOptions renders a list of [Option] rows with a highlighted cursor indicator.
func FormatOptions(opts []Option, cursor int) string {
	if len(opts) == 0 {
		return ""
	}

	tableRows := make([][]string, len(opts))
	for i, opt := range opts {
		var cols []string
		if len(opt.Columns) > 0 {
			cols = make([]string, len(opt.Columns))
			copy(cols, opt.Columns)
		} else {
			cols = []string{cmp.Or(opt.Label, opt.Value), opt.Description}
		}

		if i == cursor {
			cols[0] = "❯ " + cols[0]
		} else {
			cols[0] = "  " + cols[0]
		}

		if opt.Current {
			lastIdx := len(cols) - 1
			if cols[lastIdx] != "" {
				cols[lastIdx] += " • current"
			} else {
				cols[lastIdx] = "current"
			}
		}
		tableRows[i] = cols
	}

	t := table.New().
		BorderTop(false).
		BorderBottom(false).
		BorderLeft(false).
		BorderRight(false).
		BorderColumn(false).
		BorderRow(false).
		BorderHeader(false).
		Rows(tableRows...).
		StyleFunc(func(row, col int) lipgloss.Style {
			if col == 0 {
				if row == cursor {
					return theme.OptionSelectedStyle.PaddingRight(2)
				}
				return theme.OptionNormalStyle.PaddingRight(2)
			}
			return theme.StatusBarStyle.PaddingRight(2)
		})

	return strings.TrimRight(t.Render(), "\n")
}
