package gateway

import (
	"context"
	"errors"
	"testing"
)

func boolPtr(b bool) *bool {
	return &b
}

func TestConfigFingerprint(t *testing.T) {
	cfg1 := Config{
		Providers: []ProviderConfig{
			{
				Name: "ollama",
				Type: "ollama",
				Models: []ModelSpec{
					{ID: "llama3.2", Name: "Llama 3.2"},
					{ID: "nimble-v3", Name: "Nimble Decision", Capabilities: &ModelCapabilities{Decision: boolPtr(true)}},
				},
			},
			{
				Name: "anthropic",
				Type: "anthropic",
				Auth: &AuthConfig{Env: "ANTHROPIC_API_KEY"},
				Models: []ModelSpec{
					{ID: "claude-3-7-sonnet", Name: "Claude 3.7 Sonnet"},
				},
			},
		},
		DefaultModels: map[ModelRole]string{
			RoleCompletion: "claude-3-7-sonnet",
			RoleDecision:   "nimble-v3",
		},
		Env: map[string]string{
			"ANTHROPIC_API_KEY": "sk-ant-12345",
			"UNRELATED_VAR":     "should_not_affect_hash",
		},
	}

	// cfg2 has providers and models in reversed order, and different irrelevant env vars
	cfg2 := Config{
		Providers: []ProviderConfig{
			{
				Name: "anthropic",
				Type: "anthropic",
				Auth: &AuthConfig{Env: "ANTHROPIC_API_KEY"},
				Models: []ModelSpec{
					{ID: "claude-3-7-sonnet", Name: "Claude 3.7 Sonnet"},
				},
			},
			{
				Name: "ollama",
				Type: "ollama",
				Models: []ModelSpec{
					{ID: "nimble-v3", Name: "Nimble Decision", Capabilities: &ModelCapabilities{Decision: boolPtr(true)}},
					{ID: "llama3.2", Name: "Llama 3.2"},
				},
			},
		},
		DefaultModels: map[ModelRole]string{
			RoleCompletion: "claude-3-7-sonnet",
			RoleDecision:   "nimble-v3",
		},
		Env: map[string]string{
			"ANTHROPIC_API_KEY": "sk-ant-12345",
			"SOME_OTHER_VAR":    "different_value",
		},
	}

	fp1 := cfg1.Fingerprint()
	fp2 := cfg2.Fingerprint()

	if fp1 != fp2 {
		t.Fatalf("expected identical fingerprints for equivalent configs, got %q vs %q", fp1, fp2)
	}

	// Modifying relevant auth credential MUST change fingerprint
	cfg3 := cfg1
	cfg3.Env = map[string]string{
		"ANTHROPIC_API_KEY": "sk-ant-ROTATED",
	}
	if cfg3.Fingerprint() == fp1 {
		t.Fatalf("expected fingerprint to change when ANTHROPIC_API_KEY changes")
	}
}

func TestStrictAuthValidation(t *testing.T) {
	ctx := context.Background()

	cfgMissing := Config{
		Providers: []ProviderConfig{
			{
				Name: "anthropic",
				Type: "anthropic",
				Auth: &AuthConfig{Env: "ANTHROPIC_API_KEY"},
			},
		},
		Env: map[string]string{
			"OTHER_KEY": "foo",
		},
	}

	_, err := New(ctx, cfgMissing)
	if err == nil {
		t.Fatalf("expected error when required Auth.Env key is missing from Env map")
	}
	if !errors.Is(err, ErrMissingCredential) {
		t.Fatalf("expected ErrMissingCredential, got %v", err)
	}

	// With required key provided
	cfgValid := Config{
		Providers: []ProviderConfig{
			{
				Name: "anthropic",
				Type: "anthropic",
				Auth: &AuthConfig{Env: "ANTHROPIC_API_KEY"},
			},
		},
		Env: map[string]string{
			"ANTHROPIC_API_KEY": "test-key",
		},
	}

	gw, err := New(ctx, cfgValid)
	if err != nil {
		t.Fatalf("unexpected error with valid credentials: %v", err)
	}
	defer gw.Close()
}

func TestPoolContentAddressable(t *testing.T) {
	ctx := context.Background()
	pool := NewPool()
	defer pool.Close()

	cfg := Config{
		Providers: []ProviderConfig{
			{
				Name:     "ollama",
				Type:     "ollama",
				Endpoint: "http://127.0.0.1:11434",
				Models: []ModelSpec{
					{ID: "llama3.2", Name: "Llama 3.2"},
				},
			},
		},
	}

	gw1, err := pool.Acquire(ctx, cfg)
	if err != nil {
		t.Fatalf("unexpected acquire error: %v", err)
	}

	gw2, err := pool.Acquire(ctx, cfg)
	if err != nil {
		t.Fatalf("unexpected acquire error: %v", err)
	}

	if gw1 != gw2 {
		t.Fatalf("expected pool to return the exact same Gateway instance for identical configuration")
	}

	// Change configuration -> new gateway
	cfg2 := cfg
	cfg2.DefaultModels = map[ModelRole]string{RoleCompletion: "llama3.2"}

	gw3, err := pool.Acquire(ctx, cfg2)
	if err != nil {
		t.Fatalf("unexpected acquire error for cfg2: %v", err)
	}
	if gw3 == gw1 {
		t.Fatalf("expected different gateway instance for different config")
	}
}

func TestModelResolution(t *testing.T) {
	ctx := context.Background()

	cfg := Config{
		Providers: []ProviderConfig{
			{
				Name:     "ollama",
				Type:     "ollama",
				Endpoint: "http://127.0.0.1:11434",
				Models: []ModelSpec{
					{
						ID:   "llama3.2",
						Name: "Llama 3.2",
						Capabilities: &ModelCapabilities{
							Completion: boolPtr(true),
							Decision:   boolPtr(false),
							Embedding:  boolPtr(false),
						},
					},
					{
						ID:   "nimble-v3",
						Name: "Nimble v3",
						Capabilities: &ModelCapabilities{
							Completion: boolPtr(false),
							Decision:   boolPtr(true),
							Embedding:  boolPtr(false),
						},
					},
					{
						ID:   "nomic-embed-text",
						Name: "Nomic Embed",
						Capabilities: &ModelCapabilities{
							Completion: boolPtr(false),
							Decision:   boolPtr(false),
							Embedding:  boolPtr(true),
						},
					},
				},
				DefaultModels: map[ModelRole]string{
					RoleCompletion: "llama3.2",
					RoleDecision:   "nimble-v3",
					RoleEmbedding:  "nomic-embed-text",
				},
			},
		},
	}

	gw, err := New(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to create gateway: %v", err)
	}
	defer gw.Close()

	// 1. Resolve LLM (Completion)
	llmModel, err := gw.ResolveLLM(ctx, "")
	if err != nil {
		t.Fatalf("failed to resolve default LLM: %v", err)
	}
	if llmModel.Name() != "llama3.2" {
		t.Fatalf("expected resolved model name 'llama3.2', got %q", llmModel.Name())
	}

	// 2. Resolve Native Decision
	decModel, err := gw.ResolveDecision(ctx, "")
	if err != nil {
		t.Fatalf("failed to resolve default decision model: %v", err)
	}
	if decModel.Name() != "nimble-v3" {
		t.Fatalf("expected resolved decision model 'nimble-v3', got %q", decModel.Name())
	}

	// 3. Resolve Decision on a completion-only model -> Transparent LLM Adapter fallback
	adaptedDecModel, err := gw.ResolveDecision(ctx, "llama3.2")
	if err != nil {
		t.Fatalf("failed to resolve adapted decision model on llama3.2: %v", err)
	}
	if adaptedDecModel.Name() != "llama3.2" {
		t.Fatalf("expected adapted decision model name 'llama3.2', got %q", adaptedDecModel.Name())
	}

	// 4. Resolve Embedding
	embModel, err := gw.ResolveEmbedding(ctx, "")
	if err != nil {
		t.Fatalf("failed to resolve default embedding model: %v", err)
	}
	if embModel.Name() != "nomic-embed-text" {
		t.Fatalf("expected resolved embedding model 'nomic-embed-text', got %q", embModel.Name())
	}

	// 5. Embedding resolution on non-embedding model fails
	_, err = gw.ResolveEmbedding(ctx, "llama3.2")
	if err == nil {
		t.Fatalf("expected error when resolving embedding on non-embedding model")
	}
	if !errors.Is(err, ErrUnsupportedCapability) {
		t.Fatalf("expected ErrUnsupportedCapability, got %v", err)
	}

	// 6. Test provider list
	providers := gw.ListProviders()
	if len(providers) != 1 || providers[0].Name != "ollama" {
		t.Fatalf("unexpected provider list: %+v", providers)
	}
}

func TestGatewayClose(t *testing.T) {
	ctx := context.Background()
	cfg := Config{
		Providers: []ProviderConfig{
			{
				Name: "ollama",
				Type: "ollama",
				Models: []ModelSpec{
					{ID: "llama3.2"},
				},
			},
		},
	}

	gw, err := New(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to init: %v", err)
	}

	if err := gw.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	// Post-close resolution should fail
	if _, err := gw.ResolveLLM(ctx, "llama3.2"); !errors.Is(err, ErrGatewayClosed) {
		t.Fatalf("expected ErrGatewayClosed, got %v", err)
	}
}
