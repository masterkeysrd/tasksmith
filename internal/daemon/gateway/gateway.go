package gateway

import (
	"context"
	"fmt"
	"sync"

	"github.com/masterkeysrd/loom/decision"
	"github.com/masterkeysrd/loom/embedding"
	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/profile"
	"github.com/masterkeysrd/tasksmith/internal/daemon/models"
)

type gatewayImpl struct {
	cfg       Config
	providers map[string]*providerInstance
	mu        sync.RWMutex
	closed    bool
}

// New constructs and initializes a new Gateway from the provided configuration.
func New(ctx context.Context, cfg Config) (Gateway, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	providers := make(map[string]*providerInstance, len(cfg.Providers))

	for _, pCfg := range cfg.Providers {
		inst, err := initProvider(ctx, pCfg, cfg.Env)
		if err != nil {
			return nil, fmt.Errorf("gateway: failed to initialize provider %q: %w", pCfg.Name, err)
		}
		providers[pCfg.Name] = inst
	}

	return &gatewayImpl{
		cfg:       cfg,
		providers: providers,
	}, nil
}

func (g *gatewayImpl) Close() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.closed {
		return nil
	}
	g.closed = true
	return nil
}

func (g *gatewayImpl) ListProviders() []ProviderInfo {
	g.mu.RLock()
	defer g.mu.RUnlock()

	res := make([]ProviderInfo, 0, len(g.cfg.Providers))
	for _, pCfg := range g.cfg.Providers {
		inst, ok := g.providers[pCfg.Name]
		if !ok {
			continue
		}
		res = append(res, ProviderInfo{
			Name:     inst.cfg.Name,
			Type:     inst.cfg.Type,
			Endpoint: inst.cfg.Endpoint,
			Warning:  inst.warning,
		})
	}
	return res
}

func (g *gatewayImpl) LLM(ctx context.Context, m models.Model, cfg llm.ModelConfig) (*llm.Model, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if g.closed {
		return nil, ErrGatewayClosed
	}
	if !m.ID.Valid() {
		return nil, fmt.Errorf("%w: invalid model ID %q", ErrModelNotFound, m.ID)
	}
	inst, ok := g.providers[m.ID.Provider()]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrProviderNotFound, m.ID.Provider())
	}
	id := m.ID.Name()
	if inst.authErr != nil {
		return nil, inst.authErr
	}
	if inst.llmProv == nil || !m.Capabilities.SupportsCompletion() {
		return nil, fmt.Errorf("%w: model %q does not support completion", ErrUnsupportedCapability, m.ID)
	}
	model, err := llm.NewModel(inst.llmProv, id, &cfg)
	if err != nil {
		return nil, fmt.Errorf("gateway: failed to create llm model: %w", err)
	}
	p := m.Profile()
	model = model.WithProfile(&p)
	model = model.WithMiddleware(errorEnrichingLLMMiddleware())
	if mw := openAIResponsesOptionsMiddleware(inst.openaiResponsesOpts); mw != nil {
		model = model.WithMiddleware(mw)
	}
	model = model.WithMiddleware(validateRequestMiddleware(m))
	model = model.WithMiddleware(metricsLLMMiddleware(m, g.cfg.Metrics))
	return model, nil
}

func (g *gatewayImpl) Decision(ctx context.Context, m models.Model, cfg llm.ModelConfig) (*decision.Model, error) {
	if !m.ID.Valid() {
		return nil, fmt.Errorf("%w: invalid model ID %q", ErrModelNotFound, m.ID)
	}
	g.mu.RLock()
	if g.closed {
		g.mu.RUnlock()
		return nil, ErrGatewayClosed
	}
	inst, ok := g.providers[m.ID.Provider()]
	if !ok {
		g.mu.RUnlock()
		return nil, fmt.Errorf("%w: %q", ErrProviderNotFound, m.ID.Provider())
	}
	if inst.authErr != nil {
		g.mu.RUnlock()
		return nil, inst.authErr
	}
	provider := inst.decisionProv
	g.mu.RUnlock()
	if provider != nil && m.Capabilities.SupportsDecision() {
		decisionCfg := &decision.ModelConfig{Options: cfg.Options}
		if cfg.Extensions != nil {
			decisionCfg.Extensions = make(map[string]decision.Extension, len(cfg.Extensions))
			for key, extension := range cfg.Extensions {
				decisionCfg.Extensions[key] = extension
			}
		}
		model, err := decision.NewModel(provider, m.ID.Name(), decisionCfg)
		if err != nil {
			return nil, fmt.Errorf("gateway: failed to create decision model: %w", err)
		}
		p := m.Profile()
		return model.WithProfile(&p).WithMiddleware(errorEnrichingDecisionMiddleware()), nil
	}
	completion, err := g.LLM(ctx, m, cfg)
	if err != nil {
		return nil, err
	}
	model, err := decision.NewFromLLM(completion, nil)
	if err != nil {
		return nil, fmt.Errorf("gateway: failed to adapt completion model: %w", err)
	}
	return model.WithMiddleware(errorEnrichingDecisionMiddleware()), nil
}

func (g *gatewayImpl) Embedding(ctx context.Context, m models.Model) (*embedding.Model, error) {
	if !m.ID.Valid() {
		return nil, fmt.Errorf("%w: invalid model ID %q", ErrModelNotFound, m.ID)
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	if g.closed {
		return nil, ErrGatewayClosed
	}
	inst, ok := g.providers[m.ID.Provider()]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrProviderNotFound, m.ID.Provider())
	}
	if inst.authErr != nil {
		return nil, inst.authErr
	}
	if inst.embeddingProv == nil || !m.Capabilities.SupportsEmbedding() {
		return nil, fmt.Errorf("%w: model %q does not support embeddings", ErrUnsupportedCapability, m.ID)
	}
	model, err := embedding.NewModel(inst.embeddingProv, m.ID.Name(), nil)
	if err != nil {
		return nil, fmt.Errorf("gateway: failed to create embedding model: %w", err)
	}
	p := m.Profile()
	return model.WithProfile(&p).WithMiddleware(errorEnrichingEmbeddingMiddleware()), nil
}

func (inst *providerInstance) profileForModel(modelID string) (*profile.Profile, bool) {
	if pp, ok := inst.llmProv.(profile.Provider); ok && pp != nil {
		if prof, found := pp.GetProfile(modelID); found {
			return &prof, true
		}
	}
	if pp, ok := inst.decisionProv.(profile.Provider); ok && pp != nil {
		if prof, found := pp.GetProfile(modelID); found {
			return &prof, true
		}
	}
	if pp, ok := inst.embeddingProv.(profile.Provider); ok && pp != nil {
		if prof, found := pp.GetProfile(modelID); found {
			return &prof, true
		}
	}
	return nil, false
}

func (g *gatewayImpl) GetProfile(qualifiedID string) (profile.Profile, bool) {
	id := models.ID(qualifiedID)
	if !id.Valid() {
		return profile.Profile{}, false
	}
	g.mu.RLock()
	defer g.mu.RUnlock()

	inst, ok := g.providers[id.Provider()]
	if !ok {
		return profile.Profile{}, false
	}
	prof, ok := inst.profileForModel(id.Name())
	if !ok {
		return profile.Profile{}, false
	}
	return *prof, true
}
