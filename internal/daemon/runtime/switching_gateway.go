package runtime

import (
	"context"
	"sync"

	"github.com/masterkeysrd/loom/decision"
	"github.com/masterkeysrd/loom/embedding"
	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/profile"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway"
)

type switchingGateway struct {
	mu      sync.RWMutex
	current gateway.Gateway
}

func (g *switchingGateway) set(next gateway.Gateway) {
	g.mu.Lock()
	g.current = next
	g.mu.Unlock()
}

func (g *switchingGateway) snapshot() gateway.Gateway {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.current
}

func (g *switchingGateway) ResolveLLM(ctx context.Context, provider, model string, opts ...gateway.LLMOption) (*llm.Model, error) {
	return g.snapshot().ResolveLLM(ctx, provider, model, opts...)
}
func (g *switchingGateway) ResolveDecision(ctx context.Context, provider, model string, opts ...gateway.DecisionOption) (*decision.Model, error) {
	return g.snapshot().ResolveDecision(ctx, provider, model, opts...)
}
func (g *switchingGateway) ResolveEmbedding(ctx context.Context, provider, model string, opts ...gateway.EmbeddingOption) (*embedding.Model, error) {
	return g.snapshot().ResolveEmbedding(ctx, provider, model, opts...)
}
func (g *switchingGateway) ListProviders() []gateway.ProviderInfo {
	return g.snapshot().ListProviders()
}
func (g *switchingGateway) ModelProfile(provider, model string) (*profile.Profile, bool) {
	return g.snapshot().ModelProfile(provider, model)
}
func (g *switchingGateway) ResolveConfig(provider, model string, settings gateway.ModelSettings) (gateway.ResolvedConfig, error) {
	return g.snapshot().ResolveConfig(provider, model, settings)
}
func (g *switchingGateway) Close() error { return nil }
