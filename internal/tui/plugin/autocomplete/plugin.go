package autocomplete

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/masterkeysrd/tasksmith/internal/core/fuzzy"
	"golang.org/x/sync/errgroup"
)

// Deps defines the dependencies required by the autocomplete plugin.
type Deps struct {
	Sources []Source
}

// Plugin coordinates the list of autocomplete sources.
type Plugin struct {
	sources []Source
}

var (
	globalPlugin *Plugin
	pluginMu     sync.RWMutex
)

// SetPlugin sets the global autocomplete plugin instance.
func SetPlugin(p *Plugin) {
	pluginMu.Lock()
	defer pluginMu.Unlock()
	globalPlugin = p
}

// GetPlugin retrieves the global autocomplete plugin instance.
func GetPlugin() *Plugin {
	pluginMu.RLock()
	defer pluginMu.RUnlock()
	return globalPlugin
}

// NewPlugin instantiates a new autocomplete Plugin with the provided dependencies.
func NewPlugin(deps Deps) *Plugin {
	return &Plugin{
		sources: deps.Sources,
	}
}

// Sources returns the list of configured autocomplete sources.
func (p *Plugin) Sources() []Source {
	return p.sources
}

// Query searches the specified sources concurrently for autocomplete matches based on the QueryReq.
func (p *Plugin) Query(ctx context.Context, req QueryReq) ([]Item, error) {
	var mu sync.Mutex
	var allItems []Item

	g, ctx := errgroup.WithContext(ctx)

	for _, name := range req.Sources {
		// Find the source matching the name
		var targetSource Source
		for _, s := range p.sources {
			if s.Name() == name {
				targetSource = s
				break
			}
		}

		if targetSource == nil {
			continue
		}

		// Query each matched source concurrently
		src := targetSource
		g.Go(func() error {
			items, err := src.Query(ctx, req.Query)
			if err != nil {
				// We don't fail the whole operation if a single source fails
				return nil
			}

			mu.Lock()
			allItems = append(allItems, items...)
			mu.Unlock()
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	// Rank and sort the combined output globally if there is a query
	if req.Query != "" && len(allItems) > 0 {
		type scoredItem struct {
			item  Item
			score int
		}
		scored := make([]scoredItem, len(allItems))
		for i, item := range allItems {
			score := scoreAutocompleteItem(item, req.Query)
			scored[i] = scoredItem{item: item, score: score}
		}
		sort.SliceStable(scored, func(i, j int) bool {
			return scored[i].score > scored[j].score
		})

		ranked := make([]Item, len(allItems))
		for i, s := range scored {
			ranked[i] = s.item
		}
		allItems = ranked
	}

	return allItems, nil
}

func scoreAutocompleteItem(item Item, query string) int {
	queryLower := strings.ToLower(query)
	labelLower := strings.ToLower(item.Label)

	// Base score using fuzzy matching on the label (name)
	_, score := fuzzy.Match(item.Label, query)

	// Exact match boost
	if labelLower == queryLower {
		score += 200
	} else if strings.HasPrefix(labelLower, queryLower) {
		// Prefix match boost
		score += 100
	} else if strings.Contains(labelLower, queryLower) {
		// Substring match boost
		score += 50
	}

	// Also fuzzy match on ID/Path (e.g. for directory or nested path searches)
	if matchedID, idScore := fuzzy.Match(item.ID, query); matchedID {
		if idScore > score {
			score = idScore
		}
	}

	// Source/Kind specific tuning:
	// Prioritize files/folders slightly over deep external symbols unless exact matched
	if item.Kind == "file" || item.Kind == "directory" {
		score += 10
	}

	return score
}
