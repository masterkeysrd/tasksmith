package runtime

import (
	"context"
	"sync"

	"github.com/masterkeysrd/loom/decision"
	"github.com/masterkeysrd/loom/embedding"
	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/profile"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway"
	"github.com/masterkeysrd/tasksmith/internal/daemon/models"
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

func (g *switchingGateway) LLM(ctx context.Context, model models.Model, cfg llm.ModelConfig) (*llm.Model, error) {
	return g.snapshot().LLM(ctx, model, cfg)
}
func (g *switchingGateway) Decision(ctx context.Context, model models.Model, cfg llm.ModelConfig) (*decision.Model, error) {
	return g.snapshot().Decision(ctx, model, cfg)
}
func (g *switchingGateway) Embedding(ctx context.Context, model models.Model) (*embedding.Model, error) {
	return g.snapshot().Embedding(ctx, model)
}
func (g *switchingGateway) ListProviders() []gateway.ProviderInfo {
	return g.snapshot().ListProviders()
}
func (g *switchingGateway) GetProfile(id string) (profile.Profile, bool) {
	return g.snapshot().GetProfile(id)
}
func (g *switchingGateway) Close() error { return nil }
