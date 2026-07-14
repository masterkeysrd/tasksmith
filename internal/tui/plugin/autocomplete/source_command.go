package autocomplete

import (
	"context"
	"sort"
	"strings"

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

// ParseCommandLine parses the command line input up to the cursor to determine
// the active command name and any completed/in-progress arguments.
func ParseCommandLine(text string) (cmdName string, args []string) {
	text = strings.TrimLeft(text, " \t")
	if text == "" {
		return "", nil
	}

	// We want to preserve empty trailing arguments if the input ends with a space.
	endsWithSpace := strings.HasSuffix(text, " ") || strings.HasSuffix(text, "\t")

	parts := strings.Fields(text)
	if len(parts) == 0 {
		return "", nil
	}

	cmdName = strings.TrimPrefix(parts[0], ":")
	cmdName = strings.TrimPrefix(cmdName, "/")
	if len(parts) > 1 {
		args = parts[1:]
	}

	if endsWithSpace {
		args = append(args, "")
	}

	return cmdName, args
}

// Query fuzzy matches the input query against registered command names or their subcommands/arguments.
func (s *CommandSource) Query(ctx context.Context, req QueryReq) ([]Item, error) {
	cmdName, args := ParseCommandLine(req.FullText)
	if len(args) == 0 {
		return s.queryCommandNames(ctx, req)
	}

	completer := command.GetCompleter(cmdName)
	if completer == nil {
		return nil, nil
	}

	suggestions := completer(ctx, args)
	lastArg := args[len(args)-1]

	type scoredItem struct {
		item  Item
		score int
	}
	var scored []scoredItem

	for _, sugg := range suggestions {
		matched := true
		score := 0
		if lastArg != "" {
			matched, score = fuzzy.Match(sugg.Label, lastArg)
		}

		if matched {
			badge := sugg.Badge
			if badge == "" {
				badge = "ARG  "
			}
			scored = append(scored, scoredItem{
				item: Item{
					ID:          cmdName + ":" + sugg.Label,
					Label:       sugg.Label,
					Sublabel:    sugg.Sublabel,
					Badge:       badge,
					Kind:        "keyword",
					InsertValue: sugg.Label,
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

func (s *CommandSource) queryCommandNames(ctx context.Context, req QueryReq) ([]Item, error) {
	cmds := command.List()

	type scoredItem struct {
		item  Item
		score int
	}
	var scored []scoredItem

	for _, cmd := range cmds {
		matched, score := fuzzy.Match(cmd, req.Query)
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
