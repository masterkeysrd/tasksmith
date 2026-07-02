package autocomplete

import (
	"context"
	"sync"

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

	return allItems, nil
}
