package gateway

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/masterkeysrd/loom/decision"
	"github.com/masterkeysrd/loom/embedding"
	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/profile"
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
			Models:   inst.cfg.Models,
			Warning:  inst.warning,
		})
	}
	return res
}

func (g *gatewayImpl) ResolveLLM(ctx context.Context, provider, modelID string, opts ...LLMOption) (*llm.Model, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if g.closed {
		return nil, ErrGatewayClosed
	}

	options := &llmResolveOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(options)
		}
	}

	inst, modelID, spec, err := g.lookupProviderAndModel(provider, modelID)
	if err != nil {
		return nil, err
	}

	if inst.authErr != nil {
		return nil, inst.authErr
	}

	if inst.llmProv == nil {
		return nil, fmt.Errorf("%w: provider %q does not support chat/completion", ErrUnsupportedCapability, inst.cfg.Name)
	}
	if spec != nil && !spec.Capabilities.SupportsCompletion() {
		return nil, fmt.Errorf("%w: model %q does not support completion", ErrUnsupportedCapability, modelID)
	}

	model, err := llm.NewModel(inst.llmProv, modelID, options.config)
	if err != nil {
		return nil, fmt.Errorf("gateway: failed to create llm model: %w", err)
	}

	model = model.WithMiddleware(errorEnrichingLLMMiddleware())
	if mw := openAIResponsesOptionsMiddleware(inst.openaiResponsesOpts); mw != nil {
		model = model.WithMiddleware(mw)
	}
	if len(options.middleware) > 0 {
		model = model.WithMiddleware(options.middleware...)
	}

	return model, nil
}

func (g *gatewayImpl) ResolveDecision(ctx context.Context, provider, modelID string, opts ...DecisionOption) (*decision.Model, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if g.closed {
		return nil, ErrGatewayClosed
	}

	options := &decisionResolveOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(options)
		}
	}

	inst, modelID, spec, err := g.lookupProviderAndModel(provider, modelID)
	if err != nil {
		return nil, err
	}

	if inst.authErr != nil {
		return nil, inst.authErr
	}

	// 1. Native decision provider check
	if inst.decisionProv != nil && (spec == nil || spec.Capabilities.SupportsDecision()) {
		model, err := decision.NewModel(inst.decisionProv, modelID, options.config)
		if err != nil {
			return nil, fmt.Errorf("gateway: failed to create native decision model: %w", err)
		}
		model = model.WithMiddleware(errorEnrichingDecisionMiddleware())
		if len(options.middleware) > 0 {
			model = model.WithMiddleware(options.middleware...)
		}
		return model, nil
	}

	// 2. Fallback to LLM adapter
	if inst.llmProv == nil {
		return nil, fmt.Errorf("%w: provider %q does not support decision evaluation or completion fallback", ErrUnsupportedCapability, inst.cfg.Name)
	}
	if spec != nil && !spec.Capabilities.SupportsCompletion() {
		return nil, fmt.Errorf("%w: model %q supports neither decision nor completion", ErrUnsupportedCapability, modelID)
	}

	llmModel, err := llm.NewModel(inst.llmProv, modelID, nil)
	if err != nil {
		return nil, fmt.Errorf("gateway: failed to create underlying llm for decision adapter: %w", err)
	}
	llmModel = llmModel.WithMiddleware(errorEnrichingLLMMiddleware())
	if mw := openAIResponsesOptionsMiddleware(inst.openaiResponsesOpts); mw != nil {
		llmModel = llmModel.WithMiddleware(mw)
	}

	decModel, err := decision.NewFromLLM(llmModel, options.config)
	if err != nil {
		return nil, fmt.Errorf("gateway: failed to adapt llm into decision model: %w", err)
	}

	decModel = decModel.WithMiddleware(errorEnrichingDecisionMiddleware())
	if len(options.middleware) > 0 {
		decModel = decModel.WithMiddleware(options.middleware...)
	}

	return decModel, nil
}

func (g *gatewayImpl) ResolveEmbedding(ctx context.Context, provider, modelID string, opts ...EmbeddingOption) (*embedding.Model, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if g.closed {
		return nil, ErrGatewayClosed
	}

	options := &embeddingResolveOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(options)
		}
	}

	inst, modelID, spec, err := g.lookupProviderAndModel(provider, modelID)
	if err != nil {
		return nil, err
	}

	if inst.authErr != nil {
		return nil, inst.authErr
	}

	if inst.embeddingProv == nil {
		return nil, fmt.Errorf("%w: provider %q does not support vector embeddings", ErrUnsupportedCapability, inst.cfg.Name)
	}
	if spec != nil && !spec.Capabilities.SupportsEmbedding() {
		return nil, fmt.Errorf("%w: model %q does not support embeddings", ErrUnsupportedCapability, modelID)
	}

	model, err := embedding.NewModel(inst.embeddingProv, modelID, options.config)
	if err != nil {
		return nil, fmt.Errorf("gateway: failed to create embedding model: %w", err)
	}

	model = model.WithMiddleware(errorEnrichingEmbeddingMiddleware())
	if len(options.middleware) > 0 {
		model = model.WithMiddleware(options.middleware...)
	}

	return model, nil
}

func (g *gatewayImpl) lookupProviderAndModel(provider, modelID string) (*providerInstance, string, *ModelSpec, error) {
	provider = strings.TrimSpace(provider)
	modelID = strings.TrimSpace(modelID)
	if provider == "" {
		return nil, "", nil, fmt.Errorf("%w: provider name is required", ErrProviderNotFound)
	}
	if modelID == "" {
		return nil, "", nil, fmt.Errorf("%w: model ID is required", ErrModelNotFound)
	}

	inst, ok := g.providers[provider]
	if !ok {
		return nil, "", nil, fmt.Errorf("%w: %q", ErrProviderNotFound, provider)
	}

	var specPtr *ModelSpec
	if spec, hasSpec := inst.models[modelID]; hasSpec {
		specPtr = &spec
	}
	return inst, modelID, specPtr, nil
}

// MergeProfile merges workspace ModelSpec overrides (name, limits, capabilities) on top of
// a provider's base model profile. If both spec and base are nil, it returns nil.
func MergeProfile(spec *ModelSpec, base *profile.Profile) *profile.Profile {
	if spec == nil && base == nil {
		return nil
	}
	var out profile.Profile
	if base != nil {
		out = *base
	}
	if spec == nil {
		return &out
	}

	if spec.ID != "" && out.ID == "" {
		out.ID = spec.ID
	}
	if spec.Name != "" {
		out.Name = spec.Name
	} else if spec.Label != "" && out.Name == "" {
		out.Name = spec.Label
	}
	if spec.Limits.Context > 0 {
		out.Limits.Context = spec.Limits.Context
	}
	if spec.Limits.Output > 0 {
		out.Limits.Output = spec.Limits.Output
	}
	if spec.Limits.Dimensions > 0 {
		out.Limits.Dimensions = spec.Limits.Dimensions
	}
	if spec.Capabilities != nil {
		if len(spec.Capabilities.Thinking) > 0 {
			out.Capabilities.Reasoning = true
			out.Capabilities.ReasoningOptions = append([]profile.ReasoningOption(nil), spec.Capabilities.Thinking...)
		}
		if spec.Capabilities.Completion != nil {
			out.Capabilities.Completion = *spec.Capabilities.Completion
		}
		if spec.Capabilities.Decision != nil {
			out.Capabilities.Decision = *spec.Capabilities.Decision
		}
		if spec.Capabilities.Embedding != nil {
			out.Capabilities.Embedding = *spec.Capabilities.Embedding
		}
		if spec.Capabilities.Attachment != nil {
			out.Capabilities.Attachment = *spec.Capabilities.Attachment
		}
		if spec.Capabilities.Tools != nil {
			out.Capabilities.ToolCall = *spec.Capabilities.Tools
		}
		if len(spec.Capabilities.Dimensions) > 0 && out.Limits.Dimensions == 0 {
			out.Limits.Dimensions = spec.Capabilities.Dimensions[0]
		}
	}
	return &out
}

// ModelProfile looks up the discovered model profile from the matching provider or catalog,
// merging any workspace ModelSpec overrides configured for the model.
func (g *gatewayImpl) ModelProfile(provider, modelID string) (*profile.Profile, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	inst, modelID, spec, err := g.lookupProviderAndModel(provider, modelID)
	if err != nil || inst == nil {
		return nil, false
	}

	var base *profile.Profile
	if pp, ok := inst.llmProv.(profile.Provider); ok && pp != nil {
		if prof, found := pp.GetProfile(modelID); found {
			base = &prof
		}
	}
	if base == nil {
		if pp, ok := inst.decisionProv.(profile.Provider); ok && pp != nil {
			if prof, found := pp.GetProfile(modelID); found {
				base = &prof
			}
		}
	}
	if base == nil {
		if pp, ok := inst.embeddingProv.(profile.Provider); ok && pp != nil {
			if prof, found := pp.GetProfile(modelID); found {
				base = &prof
			}
		}
	}

	if base == nil && spec == nil {
		return nil, false
	}

	merged := MergeProfile(spec, base)
	if merged == nil {
		return nil, false
	}
	if merged.ID == "" {
		merged.ID = modelID
	}
	return merged, true
}
