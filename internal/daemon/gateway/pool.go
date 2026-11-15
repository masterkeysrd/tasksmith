package gateway

import (
	"context"
	"fmt"
	"sync"
)

// Pool manages a thread-safe cache of Gateway instances keyed by their
// canonical configuration fingerprint (SHA-256).
type Pool interface {
	// Acquire returns an existing Gateway matching the configuration fingerprint,
	// or initializes and caches a new one.
	Acquire(ctx context.Context, cfg Config) (Gateway, error)

	// Close shuts down all cached gateways and releases all client resources.
	Close() error
}

type poolImpl struct {
	mu       sync.RWMutex
	gateways map[string]Gateway
	closed   bool
}

// NewPool creates an empty, content-addressable Gateway Pool.
func NewPool() Pool {
	return &poolImpl{
		gateways: make(map[string]Gateway),
	}
}

func (p *poolImpl) Acquire(ctx context.Context, cfg Config) (Gateway, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	key := cfg.Fingerprint()

	// Fast path: read lock check
	p.mu.RLock()
	if p.closed {
		p.mu.RUnlock()
		return nil, ErrGatewayClosed
	}
	if gw, ok := p.gateways[key]; ok {
		p.mu.RUnlock()
		return gw, nil
	}
	p.mu.RUnlock()

	// Slow path: write lock and initialize
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil, ErrGatewayClosed
	}

	// Double check after acquiring write lock
	if gw, ok := p.gateways[key]; ok {
		return gw, nil
	}

	gw, err := New(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("pool: failed to initialize gateway: %w", err)
	}

	p.gateways[key] = gw
	return gw, nil
}

func (p *poolImpl) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil
	}
	p.closed = true

	var firstErr error
	for key, gw := range p.gateways {
		if err := gw.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		delete(p.gateways, key)
	}

	return firstErr
}
