package gateway

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/profile"
)

const (
	modelDiscoveryTimeout = 10 * time.Second
	modelDiscoveryTTL     = time.Minute
)

type profileDiscovery struct {
	lister interface {
		ListModels(context.Context) ([]profile.Profile, error)
	}

	mu      sync.Mutex
	refresh chan struct{}
	models  []profile.Profile
	err     error
	expires time.Time
}

func (d *profileDiscovery) load(ctx context.Context) ([]profile.Profile, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		d.mu.Lock()
		if time.Now().Before(d.expires) {
			models, err := d.models, d.err
			d.mu.Unlock()
			return models, err
		}
		if wait := d.refresh; wait != nil {
			d.mu.Unlock()
			select {
			case <-wait:
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		d.refresh = make(chan struct{})
		d.mu.Unlock()

		probeCtx, cancel := context.WithTimeout(ctx, modelDiscoveryTimeout)
		models, err := d.lister.ListModels(probeCtx)
		if probeCtx.Err() != nil && err == nil {
			err = probeCtx.Err()
		}
		cancel()

		d.mu.Lock()
		if callerErr := ctx.Err(); callerErr != nil {
			err = callerErr
		} else {
			ttl := modelDiscoveryTTL
			if err != nil {
				ttl = modelDiscoveryTimeout
			}
			d.models, d.err, d.expires = models, err, time.Now().Add(ttl)
		}
		close(d.refresh)
		d.refresh = nil
		d.mu.Unlock()
		return models, err
	}
}

func (d *profileDiscovery) find(ctx context.Context, id string) (profile.Profile, bool) {
	models, err := d.load(ctx)
	if err == nil {
		for _, p := range models {
			if p.ID == id {
				return p, true
			}
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
			return inst.catalogProfiles(models), true, err
		}
	}
	for _, client := range clients {
		if lister, ok := client.(interface{ ListProfiles() []profile.Profile }); ok {
			if profiles := lister.ListProfiles(); len(profiles) > 0 {
				return inst.catalogProfiles(profiles), true, nil
			}
		}
	}
	return nil, false, nil
}

func (inst *providerInstance) catalogProfiles(profiles []profile.Profile) []profile.Profile {
	if inst.isSubscriptionCLI() {
		profiles = slices.Clone(profiles)
		for i := range profiles {
			profiles[i] = inst.catalogProfile(profiles[i])
		}
	}
	return profiles
}
