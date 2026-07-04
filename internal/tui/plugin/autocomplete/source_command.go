package autocomplete

import (
	"context"
	"sort"

	"github.com/masterkeysrd/tasksmith/internal/core/fuzzy"
	"github.com/masterkeysrd/tasksmith/internal/tui/command"
)

// CommandSource implements autocomplete.Source for registered TUI commands.
type CommandSource struct{}

// NewCommandSource instantiates a new CommandSource.
func NewCommandSource() *CommandSource {
	return &CommandSource{}
}

// Name returns the unique name for this source.
func (s *CommandSource) Name() string {
	return "command"
}

// Query fuzzy matches the input query against registered command names.
func (s *CommandSource) Query(ctx context.Context, query string) ([]Item, error) {
	cmds := command.List()

	type scoredItem struct {
		item  Item
		score int
	}
	var scored []scoredItem

	for _, cmd := range cmds {
		matched, score := fuzzy.Match(cmd, query)
		if matched {
			scored = append(scored, scoredItem{
				item: Item{
					ID:          cmd,
					Label:       cmd,
					Sublabel:    "System Command",
					Badge:       "CMD  ",
					Kind:        "command",
					InsertValue: cmd,
				},
				score: score,
			})
		}
	}

	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	items := make([]Item, len(scored))
	for i, sc := range scored {
		items[i] = sc.item
	}
	return items, nil
}
