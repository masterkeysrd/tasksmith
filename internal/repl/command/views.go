package command

import (
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/ui/autocomplete"
	uicomponents "github.com/masterkeysrd/tasksmith/internal/ui/components"
)

// CompletionViews returns command-specific completion views for the active mode.
func CompletionViews(activeMode api.PermissionMode) map[string]uicomponents.AutocompleteView {
	return map[string]uicomponents.AutocompleteView{
		"commands:mode": func(result autocomplete.Result, selected int) trail.Node {
			options := make([]uicomponents.Option, len(result.Candidates))
			for i, candidate := range result.Candidates {
				mode, ok := candidate.Data.(api.PermissionMode)
				options[i] = uicomponents.Option{
					Value:       candidate.Value,
					Label:       candidate.Label,
					Description: candidate.Description,
					Current:     ok && mode == activeMode,
				}
			}
			return uicomponents.OptionList(uicomponents.OptionListProps{
				Options:    options,
				Selected:   selected,
				MaxVisible: uicomponents.DefaultAutocompleteMaxVisible,
			})
		},
	}
}
