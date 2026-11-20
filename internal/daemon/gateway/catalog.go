package gateway

import (
	"context"
	"sync"
	"time"

	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/profile"
)

const modelDiscoveryTimeout = 10 * time.Second

// profileDiscovery loads the catalog for clients whose GetProfile method
// only reads a cache. Delay retries after an error so resolving a manifest
// with several models does not issue one failed request per model.
type profileDiscovery struct {
	lister llm.ModelLister

	mu      sync.Mutex
	retryAt time.Time
}

func (d *profileDiscovery) find(id string) (profile.Profile, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if time.Now().Before(d.retryAt) {
		return profile.Profile{}, false
	}

	ctx, cancel := context.WithTimeout(context.Background(), modelDiscoveryTimeout)
	defer cancel()
	models, err := d.lister.ListModels(ctx)
	if err != nil {
		d.retryAt = time.Now().Add(modelDiscoveryTimeout)
		return profile.Profile{}, false
	}
	for _, p := range models {
		if p.ID == id {
			return p, true
		}
	}
	return profile.Profile{}, false
}

func (g *gatewayImpl) ListModels(ctx context.Context, provider string) ([]profile.Profile, bool, error) {
	g.mu.RLock()
	inst, closed := g.providers[provider], g.closed
	g.mu.RUnlock()
	if closed {
		return nil, false, ErrGatewayClosed
	}
	if inst == nil || inst.authErr != nil {
		return nil, false, nil
	}

	clients := inst.clients()
	for _, client := range clients {
		if lister, ok := client.(llm.ModelLister); ok {
			ctx, cancel := context.WithTimeout(ctx, modelDiscoveryTimeout)
			defer cancel()
			models, err := lister.ListModels(ctx)
			return models, true, err
		}
	}
	for _, client := range clients {
		if lister, ok := client.(interface{ ListProfiles() []profile.Profile }); ok {
			if profiles := lister.ListProfiles(); len(profiles) > 0 {
				return profiles, true, nil
			}
		}
	}
	return nil, false, nil
}
