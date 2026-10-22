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

// AutocompleteProps presents the state resolved by UseAutocomplete.
type AutocompleteProps struct {
	State AutocompleteState
	Views map[string]AutocompleteView
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
	if view := props.Views[state.Result.Source]; view != nil {
		return view(state.Result, state.Selected)
	}
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
	return OptionList(OptionListProps{
		Options: options, Selected: state.Selected, MaxVisible: DefaultAutocompleteMaxVisible,
	})
})
