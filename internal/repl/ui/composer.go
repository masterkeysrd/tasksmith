package ui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/masterkeysrd/tasksmith/internal/repl/theme"
)

// Suggestion describes an autocomplete hint for a slash command.
type Suggestion struct {
	Usage       string
	Description string
}

// Composer handles user prompt input and history navigation in the REPL.
type Composer struct {
	input        textinput.Model
	history      []string
	historyIdx   int
	historyDraft string
}

// NewComposer constructs an initialized [Composer] ready for user input.
func NewComposer() Composer {
	ti := textinput.New()
	ti.Prompt = "❯ "
	ti.Placeholder = "Ask TaskSmith or type /help..."
	ti.Focus()

	return Composer{
		input: ti,
	}
}

// Value returns the current text in the composer input field.
func (c Composer) Value() string {
	return c.input.Value()
}

// SetValue sets the composer text and moves the cursor to the end.
func (c *Composer) SetValue(val string) {
	c.input.SetValue(val)
	c.input.CursorEnd()
}

// Reset clears the input text field.
func (c *Composer) Reset() {
	c.input.Reset()
}

// Focus focuses the text input.
func (c *Composer) Focus() tea.Cmd {
	return c.input.Focus()
}

// Blur blurs the text input.
func (c *Composer) Blur() {
	c.input.Blur()
}

// View renders the prompt input field.
func (c Composer) View() string {
	return c.input.View()
}

// Update forwards Bubble Tea messages to the internal [textinput.Model].
func (c Composer) Update(msg tea.Msg) (Composer, tea.Cmd) {
	var cmd tea.Cmd
	c.input, cmd = c.input.Update(msg)
	return c, cmd
}

// InHistory reports whether the composer is currently navigating previous history items.
func (c Composer) InHistory() bool {
	return c.historyIdx < len(c.history)
}

// NavigateHistory steps backward (delta < 0) or forward (delta > 0) through command history.
func (c Composer) NavigateHistory(delta int) Composer {
	if len(c.history) == 0 {
		return c
	}

	if delta < 0 {
		if c.historyIdx == len(c.history) {
			c.historyDraft = c.input.Value()
		}
		if c.historyIdx > 0 {
			c.historyIdx--
			c.input.SetValue(c.history[c.historyIdx])
			c.input.CursorEnd()
		}
		return c
	}

	if c.historyIdx < len(c.history)-1 {
		c.historyIdx++
		c.input.SetValue(c.history[c.historyIdx])
		c.input.CursorEnd()
		return c
	}

	if c.historyIdx == len(c.history)-1 {
		c.historyIdx = len(c.history)
		c.input.SetValue(c.historyDraft)
		c.input.CursorEnd()
	}
	return c
}

// Submit records the current prompt in history if non-empty, clears the input, and returns the text.
func (c *Composer) Submit() string {
	text := strings.TrimSpace(c.input.Value())
	if text == "" {
		return ""
	}

	c.history = append(c.history, text)
	c.historyIdx = len(c.history)
	c.historyDraft = ""
	c.input.Reset()

	return text
}

// FormatSuggestions renders inline slash-command autocomplete hints.
func FormatSuggestions(suggestions []Suggestion) string {
	if len(suggestions) == 0 {
		return ""
	}
	var lines []string
	for _, s := range suggestions {
		lines = append(lines, theme.StatusBarStyle.Render(fmt.Sprintf("  %-38s %s", s.Usage, s.Description)))
	}
	return strings.Join(lines, "\n")
}
