package command

import (
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/repl/theme"
	"github.com/masterkeysrd/tasksmith/internal/ui/autocomplete"
	"github.com/masterkeysrd/tasksmith/internal/ui/components"
)

// CompletionViews returns command-specific completion views for the active mode.
func CompletionViews(activeMode api.PermissionMode) map[string]components.AutocompleteView {
	return map[string]components.AutocompleteView{
		"commands:mode": func(result autocomplete.Result, selected int) trail.Node {
			options := make([]components.Option, len(result.Candidates))
			for i, candidate := range result.Candidates {
				mode, ok := candidate.Data.(api.PermissionMode)
				options[i] = components.Option{
					Value: candidate.Value, Label: candidate.Label, Description: candidate.Description,
					Current: ok && mode == activeMode,
				}
			}
			return components.OptionList(components.OptionListProps{
				Options: options, Selected: selected, MaxVisible: components.DefaultAutocompleteMaxVisible,
				Styles: &components.OptionListStyles{
					Option:      theme.PickerOptionStyle,
					Selected:    theme.PickerSelectedStyle,
					Description: theme.StatusBarStyle,
				},
			})
		},
	}
}
