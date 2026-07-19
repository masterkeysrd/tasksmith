package autocomplete

import (
	"context"
	"sort"
	"strings"

	"github.com/masterkeysrd/tasksmith/internal/core/fuzzy"
)

// WorkflowSource implements autocomplete.Source for conversational slash commands.
type WorkflowSource struct{}

// NewWorkflowSource instantiates a new WorkflowSource.
func NewWorkflowSource() *WorkflowSource {
	return &WorkflowSource{}
}

// Name returns the unique identifier for this source.
func (s *WorkflowSource) Name() string {
	return "workflow"
}

// Query performs a fuzzy search across available workflow commands and maps them to Items.
func (s *WorkflowSource) Query(ctx context.Context, req QueryReq) ([]Item, error) {
	// The query might start with / or have / trimmed by the autocomplete parser.
	// Clean query to do matching.
	cleanQuery := strings.TrimPrefix(req.Query, "/")

	workflows := []struct {
		Name        string
		Description string
	}{
		{Name: "init", Description: "Initialize project context (AGENT.md)"},
		{Name: "create-skill", Description: "Interactively create a new Skill resource"},
		{Name: "create-tool", Description: "Interactively create a new Tool/MCP manifest"},
		{Name: "create-agent", Description: "Interactively create a new Agent manifest"},
		{Name: "manage-providers", Description: "Interactively manage model provider configurations"},
		{Name: "compact", Description: "Trigger forced compaction sweep on chat history"},
	}

	type scoredItem struct {
		item  Item
		score int
	}
	var scored []scoredItem

	for _, w := range workflows {
		// Try matching both with and without the leading slash.
		matched := false
		score := 0

		if cleanQuery == "" {
			matched = true
		} else {
			matched, score = fuzzy.Match(w.Name, cleanQuery)
		}

		if matched {
			scored = append(scored, scoredItem{
				item: Item{
					ID:          w.Name,
					Label:       "/" + w.Name,
					Sublabel:    w.Description,
					Badge:       "FLOW ",
					Kind:        "command",
					InsertValue: "/" + w.Name,
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
