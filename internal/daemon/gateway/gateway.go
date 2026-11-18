package gateway

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/masterkeysrd/loom/decision"
	"github.com/masterkeysrd/loom/embedding"
	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/profile"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/models"
)

type gatewayImpl struct {
	cfg       Config
	providers map[string]*providerInstance
	bgCancel  context.CancelFunc
	mu        sync.RWMutex
	closed    bool
}

// New constructs and initializes a new Gateway from the provided configuration.
func New(ctx context.Context, cfg Config) (Gateway, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	// Providers outlive the caller's request context (e.g. a cached GatewayPool
	// entry initialized during an HTTP call), so long-lived background helpers
	// such as OAuth token refreshers bind to a gateway-owned lifecycle context.
	bgCtx, bgCancel := context.WithCancel(context.Background())
	providers := make(map[string]*providerInstance, len(cfg.Providers))

	for _, pCfg := range cfg.Providers {
		inst, err := initProvider(bgCtx, pCfg, cfg.Env)
		if err != nil {
			if len(cfg.Providers) > 1 {
				inst = &providerInstance{
					cfg:     pCfg,
					authErr: err,
					warning: fmt.Sprintf("provider %q disabled: %v", pCfg.Name, err),
				}
			} else {
				bgCancel()
				return nil, fmt.Errorf("gateway: failed to initialize provider %q: %w", pCfg.Name, err)
			}
		}
		providers[pCfg.Name] = inst
	}

	return &gatewayImpl{
		cfg:       cfg,
		providers: providers,
		bgCancel:  bgCancel,
	}, nil
}

func (g *gatewayImpl) Close() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.closed {
		return nil
	}
	g.closed = true
	if g.bgCancel != nil {
		g.bgCancel()
	}
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

// Quota projects loom's llm.QuotaProvider onto a TaskSmith catalog model.
// Providers that do not implement QuotaProvider, or that have no quota data
// for the model, yield (nil, nil) rather than an error: quota is optional
// metadata, and a missing answer must not fail the caller's flow.
func (g *gatewayImpl) Quota(ctx context.Context, m models.Model) (*api.ModelQuota, error) {
	g.mu.RLock()
	if g.closed {
		g.mu.RUnlock()
		return nil, ErrGatewayClosed
	}
	inst, ok := g.providers[m.ID.Provider()]
	g.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrProviderNotFound, m.ID.Provider())
	}
	if inst.authErr != nil {
		return nil, inst.authErr
	}

	qp, ok := inst.llmProv.(llm.QuotaProvider)
	if !ok || inst.llmProv == nil {
		return nil, nil
	}
	buckets, found, err := qp.GetQuota(ctx, m.ID.Name())
	if err != nil {
		return nil, err
	}
	if !found || len(buckets) == 0 {
		return nil, nil
	}

	outBuckets := make([]api.QuotaBucket, 0, len(buckets))
	for _, b := range buckets {
		outBuckets = append(outBuckets, toAPIQuotaBucket(b))
	}

	q := &api.ModelQuota{
		Model:    string(m.ID),
		Provider: inst.cfg.Name,
		Buckets:  outBuckets,
	}
	if _, account, terr := LoadProviderToken(inst.cfg.Name); terr == nil && account != "" {
		q.Account = account
	}
	return q, nil
}

func (g *gatewayImpl) QuotaSnapshots(ctx context.Context) (*api.QuotaSnapshotResponse, error) {
	g.mu.RLock()
	if g.closed {
		g.mu.RUnlock()
		return nil, ErrGatewayClosed
	}
	type providerEntry struct {
		name string
		qp   llm.QuotaProvider
	}
	entries := make([]providerEntry, 0, len(g.providers))
	for name, inst := range g.providers {
		if inst == nil || inst.authErr != nil || inst.llmProv == nil {
			continue
		}
		if qp, ok := inst.llmProv.(llm.QuotaProvider); ok {
			entries = append(entries, providerEntry{name: name, qp: qp})
		}
	}
	g.mu.RUnlock()

	slices.SortFunc(entries, func(a, b providerEntry) int {
		return strings.Compare(a.name, b.name)
	})

	resp := &api.QuotaSnapshotResponse{}
	for _, entry := range entries {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		snap, err := entry.qp.GetQuotaSnapshot(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			continue
		}
		if len(snap.Buckets) == 0 && len(snap.Models) == 0 {
			continue
		}

		apiSnap := api.ProviderQuotaSnapshot{
			Provider:    entry.name,
			CollectedAt: snap.CollectedAt,
			Buckets:     make(map[string]api.QuotaBucket, len(snap.Buckets)),
			Models:      make(map[string][]string, len(snap.Models)),
		}
		if _, account, terr := LoadProviderToken(entry.name); terr == nil && account != "" {
			apiSnap.Account = account
		}
		for id, b := range snap.Buckets {
			apiSnap.Buckets[id] = toAPIQuotaBucket(b)
		}
		for modelID, bucketIDs := range snap.Models {
			qualified := modelID
			if !strings.Contains(qualified, "/") {
				qualified = entry.name + "/" + modelID
			}
			apiSnap.Models[qualified] = slices.Clone(bucketIDs)
		}
		resp.Providers = append(resp.Providers, apiSnap)
	}
	return resp, nil
}

func toAPIQuotaBucket(b llm.QuotaBucket) api.QuotaBucket {
	out := api.QuotaBucket{
		ID:                b.ID,
		Scope:             b.Scope,
		Window:            b.Window,
		Unit:              api.QuotaUnit(b.Unit),
		Limit:             b.Limit,
		Used:              b.Used,
		RemainingFraction: b.RemainingFraction,
		Unlimited:         b.Unlimited,
		Overage:           b.Overage,
	}
	if b.ResetAt != nil && !b.ResetAt.IsZero() {
		t := *b.ResetAt
		out.ResetAt = &t
	}
	return out
}

// quotaBilled reports whether calls on this provider should be booked against
// a subscription window: the provider must report quota, and the model must
// carry no per-token price. A priced model that also reports quota is still
// billed per token, so its cost stays meaningful.
func (inst *providerInstance) quotaBilled(m models.Model) bool {
	if inst == nil || inst.authErr != nil || inst.llmProv == nil || m.Pricing != nil {
		return false
	}
	_, ok := inst.llmProv.(llm.QuotaProvider)
	return ok
}

// QuotaCapable reports whether the model's provider implements
// [llm.QuotaProvider]. It is a pure capability check and never contacts the
// provider, so it is safe to call while projecting a model catalog.
func (g *gatewayImpl) QuotaCapable(m models.Model) bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if g.closed {
		return false
	}
	inst, ok := g.providers[m.ID.Provider()]
	if !ok || inst.authErr != nil || inst.llmProv == nil {
		return false
	}
	_, ok = inst.llmProv.(llm.QuotaProvider)
	return ok
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
	model = model.WithMiddleware(errorEnrichingLLMMiddleware(g.quotaReaderFor(m)))
	if mw := openAIResponsesOptionsMiddleware(inst.openaiResponsesOpts); mw != nil {
		model = model.WithMiddleware(mw)
	}
	if inst.cfg.Type == "antigravity" {
		model = model.WithMiddleware(antigravitySessionMiddleware())
	}
	if inst.cfg.Type == "anthropic" {
		model = model.WithMiddleware(anthropicCachingMiddleware())
	}
	model = model.WithMiddleware(llm.DeferredToolsMiddleware(nil), llm.InlineToolsMiddleware(nil))
	model = model.WithMiddleware(metricsLLMMiddleware(m, g.cfg.Metrics, inst.quotaBilled(m)))
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
