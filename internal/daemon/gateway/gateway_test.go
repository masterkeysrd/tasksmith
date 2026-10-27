package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/loom/profile"
	"github.com/openai/openai-go/v3"
)

func TestConfigFingerprint(t *testing.T) {
	cfg1 := Config{
		Providers: []ProviderConfig{
			{
				Name: "ollama",
				Type: "ollama",
				Models: []ModelSpec{
					{ID: "llama3.2", Name: "Llama 3.2"},
					{ID: "nimble-v3", Name: "Nimble Decision", Capabilities: &ModelCapabilities{Decision: new(true)}},
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
					{ID: "nimble-v3", Name: "Nimble Decision", Capabilities: &ModelCapabilities{Decision: new(true)}},
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

	// 1. Missing credentials: New() must succeed lazily, with warning populated
	cfgMissing := Config{
		Providers: []ProviderConfig{
			{
				Name: "anthropic",
				Type: "anthropic",
				Auth: &AuthConfig{Env: "ANTHROPIC_API_KEY"},
				Models: []ModelSpec{
					{
						ID:   "claude-3-7-sonnet",
						Name: "Claude 3.7 Sonnet",
						Capabilities: &ModelCapabilities{
							Completion: new(true),
						},
					},
				},
			},
			{
				Name:     "ollama",
				Type:     "ollama",
				Endpoint: "http://127.0.0.1:11434",
				Models: []ModelSpec{
					{
						ID:   "llama3.2",
						Name: "Llama 3.2",
						Capabilities: &ModelCapabilities{
							Completion: new(true),
							Decision:   new(true),
							Embedding:  new(true),
						},
					},
				},
				DefaultModels: map[ModelRole]string{
					RoleCompletion: "llama3.2",
				},
			},
		},
		Env: map[string]string{
			"OTHER_KEY": "foo",
		},
	}

	gw, err := New(ctx, cfgMissing)
	if err != nil {
		t.Fatalf("expected New() to succeed lazily with missing provider credentials, got: %v", err)
	}
	defer gw.Close()

	// Verify ListProviders includes warning on unauthenticated provider
	providers := gw.ListProviders()
	var anthropicInfo, ollamaInfo *ProviderInfo
	for i := range providers {
		if providers[i].Name == "anthropic" {
			anthropicInfo = &providers[i]
		}
		if providers[i].Name == "ollama" {
			ollamaInfo = &providers[i]
		}
	}
	if anthropicInfo == nil || anthropicInfo.Warning == "" {
		t.Fatalf("expected warning on anthropic provider, got info: %+v", anthropicInfo)
	}
	expectedWarning := `provider "anthropic": missing required environment variable "ANTHROPIC_API_KEY"`
	if anthropicInfo.Warning != expectedWarning {
		t.Errorf("anthropic warning = %q, want %q", anthropicInfo.Warning, expectedWarning)
	}
	if ollamaInfo == nil || ollamaInfo.Warning != "" {
		t.Errorf("expected no warning on ollama provider, got: %q", ollamaInfo.Warning)
	}

	// Calling ResolveLLM for unauthenticated provider returns ErrMissingCredential
	_, err = gw.ResolveLLM(ctx, "anthropic/claude-3-7-sonnet")
	if !errors.Is(err, ErrMissingCredential) {
		t.Fatalf("expected ErrMissingCredential for ResolveLLM on unauthenticated provider, got %v", err)
	}
	_, err = gw.ResolveDecision(ctx, "anthropic/claude-3-7-sonnet")
	if !errors.Is(err, ErrMissingCredential) {
		t.Fatalf("expected ErrMissingCredential for ResolveDecision on unauthenticated provider, got %v", err)
	}
	_, err = gw.ResolveEmbedding(ctx, "anthropic/claude-3-7-sonnet")
	if !errors.Is(err, ErrMissingCredential) {
		t.Fatalf("expected ErrMissingCredential for ResolveEmbedding on unauthenticated provider, got %v", err)
	}

	// Calling ResolveLLM for healthy provider succeeds!
	healthyLLM, err := gw.ResolveLLM(ctx, "ollama/llama3.2")
	if err != nil {
		t.Fatalf("expected healthy provider ResolveLLM to succeed, got %v", err)
	}
	if healthyLLM.Name() != "llama3.2" {
		t.Errorf("expected llama3.2, got %q", healthyLLM.Name())
	}

	// Default role resolution prefers the healthy provider over the unauthenticated provider
	defaultLLM, err := gw.ResolveLLM(ctx, "")
	if err != nil {
		t.Fatalf("expected default ResolveLLM to route to healthy provider, got %v", err)
	}
	if defaultLLM.Name() != "llama3.2" {
		t.Errorf("expected default LLM llama3.2, got %q", defaultLLM.Name())
	}

	// 2. Test os.Getenv fallback via t.Setenv
	t.Setenv("ANTHROPIC_API_KEY", "env-fallback-secret")
	gwWithHostEnv, err := New(ctx, Config{
		Providers: []ProviderConfig{
			{
				Name: "anthropic",
				Type: "anthropic",
				Auth: &AuthConfig{Env: "ANTHROPIC_API_KEY"},
				Models: []ModelSpec{
					{ID: "claude-3-7-sonnet", Name: "Claude 3.7 Sonnet"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("expected New() to succeed with host env fallback, got: %v", err)
	}
	defer gwWithHostEnv.Close()

	hostProviders := gwWithHostEnv.ListProviders()
	if len(hostProviders) != 1 || hostProviders[0].Warning != "" {
		t.Errorf("expected no warning when credential is provided via os.Getenv, got: %+v", hostProviders)
	}

	// Verify fingerprint includes the host env fallback
	fp := (&Config{
		Providers: []ProviderConfig{
			{
				Name: "anthropic",
				Type: "anthropic",
				Auth: &AuthConfig{Env: "ANTHROPIC_API_KEY"},
			},
		},
	}).Fingerprint()
	if fp == "" || fp == "nil" {
		t.Fatalf("expected valid fingerprint with os.Getenv fallback, got %q", fp)
	}
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
							Completion: new(true),
							Decision:   new(false),
							Embedding:  new(false),
						},
					},
					{
						ID:   "nimble-v3",
						Name: "Nimble v3",
						Capabilities: &ModelCapabilities{
							Completion: new(false),
							Decision:   new(true),
							Embedding:  new(false),
						},
					},
					{
						ID:   "nomic-embed-text",
						Name: "Nomic Embed",
						Capabilities: &ModelCapabilities{
							Completion: new(false),
							Decision:   new(false),
							Embedding:  new(true),
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

type mockStreamProvider struct {
	llm.Provider
	streamErr error
	syncErr   error
}

func (m *mockStreamProvider) Name() string                             { return "mock" }
func (m *mockStreamProvider) ListProfiles() []profile.Profile          { return nil }
func (m *mockStreamProvider) GetProfile(string) (profile.Profile, bool) { return profile.Profile{}, false }
func (m *mockStreamProvider) SearchProfiles(string) []profile.Profile  { return nil }
func (m *mockStreamProvider) OverrideProfile(string, profile.Profile)  {}

func (m *mockStreamProvider) Stream(ctx context.Context, req *llm.Request) (llm.StreamResponse, error) {
	if m.syncErr != nil {
		return nil, m.syncErr
	}
	return func(yield func(message.AssistantChunk, error) bool) {
		if m.streamErr != nil {
			yield(message.AssistantChunk{}, m.streamErr)
			return
		}
		yield(message.AssistantChunk{
			Content: []message.Block{&message.TextBlock{Text: "ok"}},
			Done:    true,
		}, nil)
	}, nil
}

func TestOpenAIErrorEnrichment(t *testing.T) {
	ctx := context.Background()

	openaiErr := &openai.Error{
		StatusCode: 400,
		Message:    "Invalid model 'gpt-6.1-terra'",
	}

	// Verify that the SDK's raw Error() sanitizes away the message
	if strings.Contains(openaiErr.Error(), "Invalid model 'gpt-6.1-terra'") {
		t.Fatalf("expected openai.Error.Error() to be sanitized, but it contained the message: %q", openaiErr.Error())
	}

	wrappedErr := fmt.Errorf("stream error: %w", openaiErr)

	mockProv := &mockStreamProvider{
		streamErr: wrappedErr,
	}

	gw := &gatewayImpl{
		providers: map[string]*providerInstance{
			"mock": {
				cfg:     ProviderConfig{Name: "mock", Type: "openai"},
				llmProv: mockProv,
			},
		},
		providerList: []*providerInstance{
			{
				cfg:     ProviderConfig{Name: "mock", Type: "openai"},
				llmProv: mockProv,
			},
		},
	}

	model, err := gw.ResolveLLM(ctx, "mock/gpt-6.1-terra")
	if err != nil {
		t.Fatalf("ResolveLLM failed: %v", err)
	}

	// Invoke the model which drains the stream and returns the stream error
	_, err = model.Invoke(ctx, []message.Message{message.NewUserText("Hello")})
	if err == nil {
		t.Fatal("expected error from model.Invoke, got nil")
	}

	// Verify err.Error() contains the enriched message
	if !strings.Contains(err.Error(), "Invalid model 'gpt-6.1-terra'") {
		t.Errorf("expected err.Error() to contain 'Invalid model \\'gpt-6.1-terra\\'', got: %q", err.Error())
	}

	// Verify errors.As succeeds for *openai.Error
	var target *openai.Error
	if !errors.As(err, &target) || target == nil {
		t.Fatalf("expected errors.As(err, &target) to succeed, got %v", err)
	}
	if target.StatusCode != 400 {
		t.Errorf("target.StatusCode = %d, want 400", target.StatusCode)
	}
	if target.Message != "Invalid model 'gpt-6.1-terra'" {
		t.Errorf("target.Message = %q, want 'Invalid model \\'gpt-6.1-terra\\''", target.Message)
	}
}

func TestAnthropicErrorEnrichment(t *testing.T) {
	rawJSON := `{"type":"error","error":{"type":"invalid_request_error","message":"model not found: claude-custom-4"}}`
	var anthropicErr anthropic.Error
	if err := json.Unmarshal([]byte(rawJSON), &anthropicErr); err != nil {
		t.Fatalf("json.Unmarshal anthropic.Error: %v", err)
	}

	wrapped := fmt.Errorf("provider failure: %w", &anthropicErr)
	enriched := formatProviderError(wrapped)

	if !strings.Contains(enriched.Error(), "model not found: claude-custom-4") {
		t.Errorf("expected enriched anthropic error to contain message, got: %q", enriched.Error())
	}

	var target *anthropic.Error
	if !errors.As(enriched, &target) || target == nil {
		t.Fatalf("expected errors.As for *anthropic.Error to succeed")
	}
}
