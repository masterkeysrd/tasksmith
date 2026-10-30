package components

import (
	"fmt"

	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/ui/autocomplete"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

// AutocompleteView renders a source's candidates without changing their order.
type AutocompleteView func(autocomplete.Result, int) trail.Node

// defaultAutocompleteBindings are shown when a consumer does not supply its own.
var defaultAutocompleteBindings = []KeyBinding{
	{Key: "↑/↓", Label: "Navigate"},
	{Key: "Tab", Label: "Select"},
	{Key: "Esc", Label: "Dismiss"},
}

// AutocompleteProps presents the state resolved by UseAutocomplete.
type AutocompleteProps struct {
	State    AutocompleteState
	Views    map[string]AutocompleteView
	Bindings []KeyBinding // optional keyboard hint footer; falls back to defaultAutocompleteBindings
}

// Autocomplete renders suggestions without resolving or executing them.
var Autocomplete = trail.FC("Autocomplete", func(props AutocompleteProps) trail.Node {
	scheme := theme.UseTheme()
	state := props.State
	if !state.IsOpen {
		return nil
	}
	if state.Error != nil {
		return trail.Span(trail.SpanProps{
			Style: style.S().Foreground(scheme.Color.Text.Error),
		}, trail.Text(fmt.Sprintf("Autocomplete: %v", state.Error)))
	}
	if state.IsLoading {
		return trail.Text("Completing...")
	}

	var content trail.Node
	if view := props.Views[state.Result.Source]; view != nil {
		content = view(state.Result, state.Selected)
	} else {
		options := make([]Option, len(state.Result.Candidates))
		for i, candidate := range state.Result.Candidates {
			label := candidate.Label
			if label == "" {
				label = candidate.Value
			}
			options[i] = Option{
				Value: candidate.Value, Label: label, Description: candidate.Description,
			}
		}
		content = OptionList(OptionListProps{
			Options: options, Selected: state.Selected, MaxVisible: DefaultAutocompleteMaxVisible,
		})
	}

	// Only surface the keyboard hint when there are candidates to navigate.
	if len(state.Result.Candidates) == 0 {
		return content
	}

	bindings := props.Bindings
	if len(bindings) == 0 {
		bindings = defaultAutocompleteBindings
	}
	return trail.Box(trail.BoxProps{
		Style: style.S().FlexDirection(style.FlexColumn).Width(style.Percent(100)),
	}, content, KeyboardHint(KeyboardHintProps{Bindings: bindings}))
})
