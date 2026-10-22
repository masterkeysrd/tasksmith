package ui

import (
	"strings"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
)

// AutocompleteProps defines the properties for the Autocomplete component.
type AutocompleteProps struct {
	Value       string
	Suggest     func(prefix string) []Suggestion
	SuggestArgs func(rawLine string) ([]Option, bool)
	Selected    int
}

// AutocompleteMatch represents the resolved autocomplete query and results.
type AutocompleteMatch struct {
	IsArgs      bool
	CmdName     string
	ArgOptions  []Option
	Suggestions []Suggestion
}

// MatchAutocomplete inspects input line and computes active autocomplete matches.
func MatchAutocomplete(val string, suggest func(string) []Suggestion, suggestArgs func(string) ([]Option, bool)) AutocompleteMatch {
	trimmedLeft := strings.TrimLeft(val, " \t")
	if !strings.HasPrefix(trimmedLeft, "/") {
		return AutocompleteMatch{}
	}

	if strings.Contains(trimmedLeft, " ") {
		if suggestArgs != nil {
			cmdPart, _, _ := strings.Cut(trimmedLeft, " ")
			if opts, ok := suggestArgs(val); ok && len(opts) > 0 {
				return AutocompleteMatch{
					IsArgs:     true,
					CmdName:    cmdPart,
					ArgOptions: opts,
				}
			}
		}
		return AutocompleteMatch{}
	}

	if suggest != nil {
		if suggs := suggest(trimmedLeft); len(suggs) > 0 {
			return AutocompleteMatch{
				Suggestions: suggs,
			}
		}
	}
	return AutocompleteMatch{}
}

// AutocompleteController provides state and handlers for keyboard autocomplete interactions.
type AutocompleteController struct {
	Match  AutocompleteMatch
	Cursor int
	Up     func() bool
	Down   func() bool
	Tab    func() (string, bool)
	Enter  func() (string, bool)
}

// useAutocomplete is a custom hook that manages matching, cursor position, and keyboard selection.
func useAutocomplete(val string, suggest func(string) []Suggestion, suggestArgs func(string) ([]Option, bool)) AutocompleteController {
	cursor, setCursor := kitex.UseState(0)

	match := kitex.UseMemo(func() AutocompleteMatch {
		return MatchAutocomplete(val, suggest, suggestArgs)
	}, []any{val})

	// Clamping & default selection on options change
	kitex.UseEffect(func() {
		if match.IsArgs && len(match.ArgOptions) > 0 {
			trimmedLeft := strings.TrimLeft(val, " \t")
			_, argPart, _ := strings.Cut(trimmedLeft, " ")
			if strings.TrimSpace(argPart) == "" {
				for i, opt := range match.ArgOptions {
					if opt.Current {
						setCursor(i)
						return
					}
				}
				setCursor(0)
			} else {
				if cur := cursor(); cur >= len(match.ArgOptions) {
					setCursor(max(0, len(match.ArgOptions)-1))
				}
			}
		}
	}, []any{val, len(match.ArgOptions)})

	cur := cursor()
	if len(match.ArgOptions) > 0 {
		if cur >= len(match.ArgOptions) {
			cur = len(match.ArgOptions) - 1
		}
		if cur < 0 {
			cur = 0
		}
	} else {
		cur = 0
	}

	up := func() bool {
		if match.IsArgs && len(match.ArgOptions) > 0 {
			if cur > 0 {
				setCursor(cur - 1)
			}
			return true
		}
		return false
	}

	down := func() bool {
		if match.IsArgs && len(match.ArgOptions) > 0 {
			if cur < len(match.ArgOptions)-1 {
				setCursor(cur + 1)
			}
			return true
		}
		return false
	}

	tab := func() (string, bool) {
		if match.IsArgs && len(match.ArgOptions) > 0 {
			selectedOpt := match.ArgOptions[cur]
			return match.CmdName + " " + selectedOpt.Value + " ", true
		}
		if len(match.Suggestions) > 0 {
			parts := strings.Fields(match.Suggestions[0].Usage)
			if len(parts) > 0 {
				return parts[0] + " ", true
			}
		}
		return "", false
	}

	enter := func() (string, bool) {
		if match.IsArgs && len(match.ArgOptions) > 0 {
			selectedOpt := match.ArgOptions[cur]
			return match.CmdName + " " + selectedOpt.Value, true
		}
		return "", false
	}

	return AutocompleteController{
		Match:  match,
		Cursor: cur,
		Up:     up,
		Down:   down,
		Tab:    tab,
		Enter:  enter,
	}
}

// Autocomplete is a trail functional component rendering inline argument or slash-command suggestions.
var Autocomplete = trail.FC("Autocomplete", func(props AutocompleteProps) trail.Node {
	match := MatchAutocomplete(props.Value, props.Suggest, props.SuggestArgs)
	if match.IsArgs && len(match.ArgOptions) > 0 {
		return OptionList(OptionListProps{
			Options:  match.ArgOptions,
			Selected: props.Selected,
		})
	}
	if len(match.Suggestions) > 0 {
		opts := make([]Option, len(match.Suggestions))
		for i, s := range match.Suggestions {
			opts[i] = Option{
				Label:       s.Usage,
				Description: s.Description,
			}
		}
		return OptionList(OptionListProps{
			Options:  opts,
			Selected: -1,
		})
	}
	return nil
})
