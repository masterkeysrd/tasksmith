package gateway

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/masterkeysrd/loom/decision"
	"github.com/masterkeysrd/loom/embedding"
	"github.com/masterkeysrd/loom/llm"
)

type gatewayImpl struct {
	cfg          Config
	providers    map[string]*providerInstance
	providerList []*providerInstance
	mu           sync.RWMutex
	closed       bool
}

// New constructs and initializes a new Gateway from the provided configuration.
func New(ctx context.Context, cfg Config) (Gateway, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	providers := make(map[string]*providerInstance, len(cfg.Providers))
	providerList := make([]*providerInstance, 0, len(cfg.Providers))

	for _, pCfg := range cfg.Providers {
		inst, err := initProvider(ctx, pCfg, cfg.Env)
		if err != nil {
			return nil, fmt.Errorf("gateway: failed to initialize provider %q: %w", pCfg.Name, err)
		}
		providers[pCfg.Name] = inst
		providerList = append(providerList, inst)
	}

	return &gatewayImpl{
		cfg:          cfg,
		providers:    providers,
		providerList: providerList,
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

	res := make([]ProviderInfo, 0, len(g.providerList))
	for _, inst := range g.providerList {
		res = append(res, ProviderInfo{
			Name:          inst.cfg.Name,
			Type:          inst.cfg.Type,
			Endpoint:      inst.cfg.Endpoint,
			DefaultModels: inst.cfg.DefaultModels,
			Models:        inst.cfg.Models,
			Warning:       inst.warning,
		})
	}
	return res
}

func (g *gatewayImpl) ResolveLLM(ctx context.Context, explicit string, opts ...LLMOption) (*llm.Model, error) {
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

	inst, modelID, spec, err := g.findModelAndProvider(RoleCompletion, explicit)
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
	if len(options.middleware) > 0 {
		model = model.WithMiddleware(options.middleware...)
	}

	return model, nil
}

func (g *gatewayImpl) ResolveDecision(ctx context.Context, explicit string, opts ...DecisionOption) (*decision.Model, error) {
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

	inst, modelID, spec, err := g.findModelAndProvider(RoleDecision, explicit)
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

func (g *gatewayImpl) ResolveEmbedding(ctx context.Context, explicit string, opts ...EmbeddingOption) (*embedding.Model, error) {
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

	inst, modelID, spec, err := g.findModelAndProvider(RoleEmbedding, explicit)
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

// findModelAndProvider resolves a model identifier and its hosting provider using
// role fallback precedence: explicit -> config default for role -> provider default for role -> catalog capability check.
func (g *gatewayImpl) findModelAndProvider(role ModelRole, explicit string) (*providerInstance, string, *ModelSpec, error) {
	explicit = strings.TrimSpace(explicit)

	// Case 1: Explicit target given
	if explicit != "" {
		// Format: "provider/model"
		if parts := strings.SplitN(explicit, "/", 2); len(parts) == 2 {
			provName, modelID := parts[0], parts[1]
			inst, ok := g.providers[provName]
			if !ok {
				return nil, "", nil, fmt.Errorf("%w: %q", ErrProviderNotFound, provName)
			}
			spec, hasSpec := inst.models[modelID]
			var specPtr *ModelSpec
			if hasSpec {
				specPtr = &spec
			}
			return inst, modelID, specPtr, nil
		}

		// Bare model ID: Search all providers for a match, preferring healthy providers
		for _, inst := range g.providerList {
			if inst.authErr == nil {
				if spec, ok := inst.models[explicit]; ok {
					return inst, explicit, &spec, nil
				}
			}
		}
		for _, inst := range g.providerList {
			if inst.authErr != nil {
				if spec, ok := inst.models[explicit]; ok {
					return inst, explicit, &spec, nil
				}
			}
		}

		// If no provider explicitly registered the model ID, route to the first healthy provider, or first provider
		for _, inst := range g.providerList {
			if inst.authErr == nil {
				return inst, explicit, nil, nil
			}
		}
		if len(g.providerList) > 0 {
			return g.providerList[0], explicit, nil, nil
		}
		return nil, "", nil, fmt.Errorf("%w: no providers registered", ErrProviderNotFound)
	}

	// Case 2: Config-level default for role
	if defaultModel, ok := g.cfg.DefaultModels[role]; ok && strings.TrimSpace(defaultModel) != "" {
		return g.findModelAndProvider(role, defaultModel)
	}

	// Case 3 (healthy): Provider-level default for role on healthy providers
	for _, inst := range g.providerList {
		if inst.authErr != nil {
			continue
		}
		if def, ok := inst.cfg.DefaultModels[role]; ok && strings.TrimSpace(def) != "" {
			if targetInst, mID, spec, err := g.findModelAndProvider(role, def); err == nil && targetInst.authErr == nil {
				return targetInst, mID, spec, nil
			}
		}
		if role == RoleCompletion && strings.TrimSpace(inst.cfg.DefaultModel) != "" {
			if targetInst, mID, spec, err := g.findModelAndProvider(role, inst.cfg.DefaultModel); err == nil && targetInst.authErr == nil {
				return targetInst, mID, spec, nil
			}
		}
	}

	// Case 4 (healthy): Search model catalogs on healthy providers
	for _, inst := range g.providerList {
		if inst.authErr != nil {
			continue
		}
		for _, m := range inst.cfg.Models {
			switch role {
			case RoleCompletion:
				if m.Capabilities.SupportsCompletion() && inst.llmProv != nil {
					spec := m
					return inst, m.ID, &spec, nil
				}
			case RoleDecision:
				if m.Capabilities.SupportsDecision() && inst.decisionProv != nil {
					spec := m
					return inst, m.ID, &spec, nil
				}
			case RoleEmbedding:
				if m.Capabilities.SupportsEmbedding() && inst.embeddingProv != nil {
					spec := m
					return inst, m.ID, &spec, nil
				}
			}
		}
	}

	// Healthy Decision adapter fallback: If role is Decision and no native decision model found among healthy providers,
	// fallback to completion model on healthy providers
	if role == RoleDecision {
		if inst, mID, spec, err := g.findModelAndProvider(RoleCompletion, ""); err == nil && inst.authErr == nil {
			return inst, mID, spec, nil
		}
	}

	// Fallback Case 3: Provider-level default for role on unauthenticated providers
	for _, inst := range g.providerList {
		if inst.authErr == nil {
			continue
		}
		if def, ok := inst.cfg.DefaultModels[role]; ok && strings.TrimSpace(def) != "" {
			return g.findModelAndProvider(role, def)
		}
		if role == RoleCompletion && strings.TrimSpace(inst.cfg.DefaultModel) != "" {
			return g.findModelAndProvider(role, inst.cfg.DefaultModel)
		}
	}

	// Fallback Case 4: Search model catalogs on unauthenticated providers
	for _, inst := range g.providerList {
		if inst.authErr == nil {
			continue
		}
		for _, m := range inst.cfg.Models {
			switch role {
			case RoleCompletion:
				if m.Capabilities.SupportsCompletion() && providerSupportsRole(inst.cfg.Type, RoleCompletion) {
					spec := m
					return inst, m.ID, &spec, nil
				}
			case RoleDecision:
				if m.Capabilities.SupportsDecision() && providerSupportsRole(inst.cfg.Type, RoleDecision) {
					spec := m
					return inst, m.ID, &spec, nil
				}
			case RoleEmbedding:
				if m.Capabilities.SupportsEmbedding() && providerSupportsRole(inst.cfg.Type, RoleEmbedding) {
					spec := m
					return inst, m.ID, &spec, nil
				}
			}
		}
	}

	// Fallback Case 5: If role is Decision, fallback to completion default for adapter on unauthenticated providers
	if role == RoleDecision {
		return g.findModelAndProvider(RoleCompletion, "")
	}

	return nil, "", nil, fmt.Errorf("%w: no default model found for role %q", ErrModelNotFound, role)
}

func providerSupportsRole(pType string, role ModelRole) bool {
	switch role {
	case RoleCompletion:
		return pType == "ollama" || pType == "openai" || pType == "anthropic" || pType == "genai" || pType == "antigravity"
	case RoleDecision:
		return pType == "ollama" || pType == "openai" || pType == "typesafe"
	case RoleEmbedding:
		return pType == "ollama" || pType == "openai" || pType == "genai"
	default:
		return false
	}
}
