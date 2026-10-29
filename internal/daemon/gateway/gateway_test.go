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
	loomopenai "github.com/masterkeysrd/loom/providers/openai"
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
	_, err = gw.ResolveLLM(ctx, "anthropic", "claude-3-7-sonnet")
	if !errors.Is(err, ErrMissingCredential) {
		t.Fatalf("expected ErrMissingCredential for ResolveLLM on unauthenticated provider, got %v", err)
	}
	_, err = gw.ResolveDecision(ctx, "anthropic", "claude-3-7-sonnet")
	if !errors.Is(err, ErrMissingCredential) {
		t.Fatalf("expected ErrMissingCredential for ResolveDecision on unauthenticated provider, got %v", err)
	}
	_, err = gw.ResolveEmbedding(ctx, "anthropic", "claude-3-7-sonnet")
	if !errors.Is(err, ErrMissingCredential) {
		t.Fatalf("expected ErrMissingCredential for ResolveEmbedding on unauthenticated provider, got %v", err)
	}

	// Calling ResolveLLM for healthy provider succeeds!
	healthyLLM, err := gw.ResolveLLM(ctx, "ollama", "llama3.2")
	if err != nil {
		t.Fatalf("expected healthy provider ResolveLLM to succeed, got %v", err)
	}
	if healthyLLM.Name() != "llama3.2" {
		t.Errorf("expected llama3.2, got %q", healthyLLM.Name())
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
	cfg2 := Config{
		Providers: []ProviderConfig{
			{
				Name:     "ollama",
				Type:     "ollama",
				Endpoint: "http://127.0.0.1:11435",
				Models: []ModelSpec{
					{ID: "llama3.2", Name: "Llama 3.2"},
				},
			},
		},
	}

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
			},
		},
	}

	gw, err := New(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to create gateway: %v", err)
	}
	defer gw.Close()

	// 1. Resolve LLM (Completion)
	llmModel, err := gw.ResolveLLM(ctx, "ollama", "llama3.2")
	if err != nil {
		t.Fatalf("failed to resolve LLM: %v", err)
	}
	if llmModel.Name() != "llama3.2" {
		t.Fatalf("expected resolved model name 'llama3.2', got %q", llmModel.Name())
	}

	// 2. Resolve Native Decision
	decModel, err := gw.ResolveDecision(ctx, "ollama", "nimble-v3")
	if err != nil {
		t.Fatalf("failed to resolve decision model: %v", err)
	}
	if decModel.Name() != "nimble-v3" {
		t.Fatalf("expected resolved decision model 'nimble-v3', got %q", decModel.Name())
	}

	// 3. Resolve Decision on a completion-only model -> Transparent LLM Adapter fallback
	adaptedDecModel, err := gw.ResolveDecision(ctx, "ollama", "llama3.2")
	if err != nil {
		t.Fatalf("failed to resolve adapted decision model on llama3.2: %v", err)
	}
	if adaptedDecModel.Name() != "llama3.2" {
		t.Fatalf("expected adapted decision model name 'llama3.2', got %q", adaptedDecModel.Name())
	}

	// 4. Resolve Embedding
	embModel, err := gw.ResolveEmbedding(ctx, "ollama", "nomic-embed-text")
	if err != nil {
		t.Fatalf("failed to resolve embedding model: %v", err)
	}
	if embModel.Name() != "nomic-embed-text" {
		t.Fatalf("expected resolved embedding model 'nomic-embed-text', got %q", embModel.Name())
	}

	// 5. Embedding resolution on non-embedding model fails
	_, err = gw.ResolveEmbedding(ctx, "ollama", "llama3.2")
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
	if _, err := gw.ResolveLLM(ctx, "ollama", "llama3.2"); !errors.Is(err, ErrGatewayClosed) {
		t.Fatalf("expected ErrGatewayClosed, got %v", err)
	}
}

type mockStreamProvider struct {
	llm.Provider
	streamErr error
	syncErr   error
}

func (m *mockStreamProvider) Name() string                    { return "mock" }
func (m *mockStreamProvider) ListProfiles() []profile.Profile { return nil }
func (m *mockStreamProvider) GetProfile(string) (profile.Profile, bool) {
	return profile.Profile{}, false
}
func (m *mockStreamProvider) SearchProfiles(string) []profile.Profile { return nil }
func (m *mockStreamProvider) OverrideProfile(string, profile.Profile) {}

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
	}

	model, err := gw.ResolveLLM(ctx, "mock", "gpt-6.1-terra")
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

type mockCapturingProvider struct {
	llm.Provider
	lastReq *llm.Request
}

func (m *mockCapturingProvider) Name() string                    { return "openai" }
func (m *mockCapturingProvider) ListProfiles() []profile.Profile { return nil }
func (m *mockCapturingProvider) GetProfile(string) (profile.Profile, bool) {
	return profile.Profile{}, false
}
func (m *mockCapturingProvider) SearchProfiles(string) []profile.Profile { return nil }
func (m *mockCapturingProvider) OverrideProfile(string, profile.Profile) {}

func (m *mockCapturingProvider) Stream(ctx context.Context, req *llm.Request) (llm.StreamResponse, error) {
	m.lastReq = req
	return func(yield func(message.AssistantChunk, error) bool) {
		yield(message.AssistantChunk{Done: true}, nil)
	}, nil
}

func TestOpenAIResponsesOptionsMiddleware_Defaults(t *testing.T) {
	ctx := context.Background()

	mock := &mockCapturingProvider{}
	storeDefault := false
	gw := &gatewayImpl{
		providers: map[string]*providerInstance{
			"openai": {
				cfg:     ProviderConfig{Name: "openai", Type: "openai"},
				llmProv: mock,
				openaiResponsesOpts: &loomopenai.ResponsesOptions{
					Store: &storeDefault,
				},
			},
		},
	}

	model, err := gw.ResolveLLM(ctx, "openai", "gpt-5.5")
	if err != nil {
		t.Fatalf("ResolveLLM failed: %v", err)
	}

	_, err = model.Invoke(ctx, []message.Message{message.NewUserText("Hello")})
	if err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}

	if mock.lastReq == nil {
		t.Fatal("expected request to be captured by mock provider")
	}

	ext, ok := mock.lastReq.Extensions[loomopenai.ResponsesExtensionID]
	if !ok {
		t.Fatalf("expected extension %q to be set", loomopenai.ResponsesExtensionID)
	}

	respOpts, ok := ext.(loomopenai.ResponsesOptions)
	if !ok {
		t.Fatalf("expected extension to be of type loomopenai.ResponsesOptions, got %T", ext)
	}

	if respOpts.Store == nil {
		t.Fatal("expected respOpts.Store to be non-nil")
	}
	if *respOpts.Store != false {
		t.Errorf("expected *respOpts.Store == false, got %v", *respOpts.Store)
	}
}

func TestOpenAIConfig_ParsingAndOptions(t *testing.T) {
	ctx := context.Background()

	// 1. camelCase options with store: true
	cfgCamel := Config{
		Providers: []ProviderConfig{
			{
				Name: "openai",
				Type: "openai",
				Auth: &AuthConfig{Env: "OPENAI_API_KEY"},
				Config: map[string]any{
					"store":                 true,
					"apiMode":               "responses",
					"usePreviousResponseID": true,
					"webSearch":             true,
					"codeInterpreter":       true,
					"fileSearch": map[string]any{
						"vectorStoreIDs": []any{"vs_1", "vs_2"},
						"maxNumResults":  20,
					},
				},
				Models: []ModelSpec{{ID: "gpt-5.5"}},
			},
		},
		Env: map[string]string{"OPENAI_API_KEY": "test-key"},
	}

	gw, err := New(ctx, cfgCamel)
	if err != nil {
		t.Fatalf("New(cfgCamel) failed: %v", err)
	}
	defer gw.Close()

	impl := gw.(*gatewayImpl)
	inst := impl.providers["openai"]
	if inst.openaiResponsesOpts == nil {
		t.Fatal("expected inst.openaiResponsesOpts to be populated")
	}
	opts := inst.openaiResponsesOpts
	if opts.Store == nil || *opts.Store != true {
		t.Errorf("expected Store=true, got %v", opts.Store)
	}
	if !opts.UsePreviousResponseID {
		t.Errorf("expected UsePreviousResponseID=true")
	}
	if !opts.WebSearch {
		t.Errorf("expected WebSearch=true")
	}
	if !opts.CodeInterpreter {
		t.Errorf("expected CodeInterpreter=true")
	}
	if opts.FileSearch == nil {
		t.Fatal("expected FileSearch to be non-nil")
	}
	if len(opts.FileSearch.VectorStoreIDs) != 2 || opts.FileSearch.VectorStoreIDs[0] != "vs_1" {
		t.Errorf("unexpected VectorStoreIDs: %v", opts.FileSearch.VectorStoreIDs)
	}
	if opts.FileSearch.MaxNumResults != 20 {
		t.Errorf("expected MaxNumResults=20, got %d", opts.FileSearch.MaxNumResults)
	}

	// 2. snake_case options
	cfgSnake := Config{
		Providers: []ProviderConfig{
			{
				Name: "openai-snake",
				Type: "openai",
				Auth: &AuthConfig{Env: "OPENAI_API_KEY"},
				Config: map[string]any{
					"store":                    false,
					"api_mode":                 "chat_completions",
					"use_previous_response_id": true,
					"web_search":               true,
					"code_interpreter":         true,
					"file_search": map[string]any{
						"vector_store_ids": []string{"vs_snake"},
						"max_num_results":  5,
					},
				},
				Models: []ModelSpec{{ID: "gpt-5.5"}},
			},
		},
		Env: map[string]string{"OPENAI_API_KEY": "test-key"},
	}

	gwSnake, err := New(ctx, cfgSnake)
	if err != nil {
		t.Fatalf("New(cfgSnake) failed: %v", err)
	}
	defer gwSnake.Close()

	implSnake := gwSnake.(*gatewayImpl)
	instSnake := implSnake.providers["openai-snake"]
	if instSnake.openaiResponsesOpts == nil {
		t.Fatal("expected instSnake.openaiResponsesOpts to be populated")
	}
	optsSnake := instSnake.openaiResponsesOpts
	if optsSnake.Store == nil || *optsSnake.Store != false {
		t.Errorf("expected Store=false, got %v", optsSnake.Store)
	}
	if !optsSnake.UsePreviousResponseID || !optsSnake.WebSearch || !optsSnake.CodeInterpreter {
		t.Errorf("expected all boolean options true on snake config")
	}
	if optsSnake.FileSearch == nil || len(optsSnake.FileSearch.VectorStoreIDs) != 1 || optsSnake.FileSearch.VectorStoreIDs[0] != "vs_snake" {
		t.Errorf("unexpected FileSearch on snake config: %+v", optsSnake.FileSearch)
	}
}

func TestOpenAIConfig_InvalidAPIMode(t *testing.T) {
	ctx := context.Background()

	cfg := Config{
		Providers: []ProviderConfig{
			{
				Name: "openai-invalid",
				Type: "openai",
				Config: map[string]any{
					"apiMode": "unsupported_mode",
				},
			},
		},
	}

	_, err := New(ctx, cfg)
	if err == nil {
		t.Fatal("expected error for invalid apiMode, got nil")
	}
	expectedSubstring := `invalid openai apiMode "unsupported_mode"`
	if !strings.Contains(err.Error(), expectedSubstring) {
		t.Errorf("expected error containing %q, got: %v", expectedSubstring, err)
	}
}

func TestGoogleGenAIAndGeminiProviderTypes(t *testing.T) {
	ctx := context.Background()

	cfg := Config{
		Providers: []ProviderConfig{
			{
				Name: "my-google-genai",
				Type: "google-genai",
				Auth: &AuthConfig{Env: "GOOGLE_API_KEY"},
				Models: []ModelSpec{
					{
						ID:   "gemini-3.1-pro",
						Name: "Gemini 3.1 Pro",
						Capabilities: &ModelCapabilities{
							Completion: new(true),
							Embedding:  new(true),
						},
					},
				},
			},
			{
				Name: "my-gemini",
				Type: "gemini",
				Auth: &AuthConfig{Env: "GEMINI_API_KEY"},
				Models: []ModelSpec{
					{
						ID:   "gemini-3.7-flash",
						Name: "Gemini 3.7 Flash",
						Capabilities: &ModelCapabilities{
							Completion: new(true),
							Embedding:  new(true),
						},
					},
				},
			},
		},
		Env: map[string]string{
			"GOOGLE_API_KEY": "fake-google-key",
			"GEMINI_API_KEY": "fake-gemini-key",
		},
	}

	gw, err := New(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to initialize gateway with google-genai and gemini types: %v", err)
	}
	defer gw.Close()
}

func TestGatewayModelProfile(t *testing.T) {
	ctx := context.Background()
	cfg := Config{
		Providers: []ProviderConfig{
			{
				Name: "test-openai",
				Type: "openai",
				Auth: &AuthConfig{Type: AuthTypeAPIKey},
				Models: []ModelSpec{
					{
						ID:   "custom-spec-model",
						Name: "Custom Spec Model",
						Limits: ModelLimits{
							Context: 256000,
							Output:  32000,
						},
					},
				},
			},
		},
		Env: map[string]string{
			"OPENAI_API_KEY": "fake-openai-key",
		},
	}

	gw, err := New(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to initialize gateway: %v", err)
	}
	defer gw.Close()

	// 1. Discovered from ModelSpec in manifest
	prof, ok := gw.ModelProfile("test-openai", "custom-spec-model")
	if !ok || prof == nil {
		t.Fatalf("expected profile for custom-spec-model, got ok=%v, prof=%v", ok, prof)
	}
	if prof.Limits.Context != 256000 || prof.Limits.Output != 32000 {
		t.Errorf("unexpected limits from spec: %+v", prof.Limits)
	}

	// 2. Discovered from underlying provider catalog (gpt-4o)
	profCatalog, okCatalog := gw.ModelProfile("test-openai", "gpt-4o")
	if okCatalog && profCatalog != nil {
		if profCatalog.Limits.Context <= 0 {
			t.Errorf("expected positive context limit for gpt-4o, got %d", profCatalog.Limits.Context)
		}
	}
}

func TestMergeProfile(t *testing.T) {
	// 1. Both nil returns nil
	if res := MergeProfile(nil, nil); res != nil {
		t.Fatalf("expected nil when both spec and base are nil, got %+v", res)
	}

	// 2. Base only returns clone of base
	base := &profile.Profile{
		ID:          "gpt-4o",
		Name:        "GPT-4o",
		Description: "Base description",
		Family:      "gpt-4",
		Knowledge:   "2024-10",
		Limits: profile.Limits{
			Context: 128000,
			Output:  16384,
		},
		Capabilities: profile.Capabilities{
			Completion: true,
			ToolCall:   true,
		},
	}
	clone := MergeProfile(nil, base)
	if clone == nil || clone.ID != "gpt-4o" || clone.Limits.Context != 128000 {
		t.Fatalf("unexpected clone from base: %+v", clone)
	}

	// 3. Spec only creates profile with limits and explicit capabilities
	completionTrue := true
	specOnly := &ModelSpec{
		ID:   "spec-model",
		Name: "Spec Model",
		Limits: ModelLimits{
			Context:    64000,
			Output:     8192,
			Dimensions: 1536,
		},
		Capabilities: &ModelCapabilities{
			Completion: &completionTrue,
		},
	}
	fromSpec := MergeProfile(specOnly, nil)
	if fromSpec == nil || fromSpec.ID != "spec-model" || fromSpec.Limits.Context != 64000 || fromSpec.Limits.Dimensions != 1536 {
		t.Fatalf("unexpected profile from spec: %+v", fromSpec)
	}
	if !fromSpec.Capabilities.Completion {
		t.Errorf("expected Completion capability to be true when explicitly configured")
	}

	// Spec only without capabilities leaves them false/zero-valued
	specNoCaps := &ModelSpec{ID: "no-caps"}
	fromNoCaps := MergeProfile(specNoCaps, nil)
	if fromNoCaps == nil || fromNoCaps.Capabilities.Completion {
		t.Errorf("expected Completion to be false when omitted on spec-only profile")
	}

	// 4. Base + Spec overrides
	completionFalse := false
	decisionTrue := true
	toolsFalse := false
	specOverride := &ModelSpec{
		Name: "Overridden Name",
		Limits: ModelLimits{
			Context:    200000,
			Output:     32000,
			Dimensions: 768,
		},
		Capabilities: &ModelCapabilities{
			Completion: &completionFalse,
			Decision:   &decisionTrue,
			Tools:      &toolsFalse,
		},
	}

	merged := MergeProfile(specOverride, base)
	if merged == nil {
		t.Fatalf("expected non-nil merged profile")
	}
	// Overridden fields
	if merged.Name != "Overridden Name" {
		t.Errorf("Name = %q, want 'Overridden Name'", merged.Name)
	}
	if merged.Limits.Context != 200000 || merged.Limits.Output != 32000 || merged.Limits.Dimensions != 768 {
		t.Errorf("Limits = %+v, want Context=200000, Output=32000, Dimensions=768", merged.Limits)
	}
	if merged.Capabilities.Completion {
		t.Errorf("expected Completion to be overridden to false")
	}
	if !merged.Capabilities.Decision {
		t.Errorf("expected Decision to be overridden to true")
	}
	if merged.Capabilities.ToolCall {
		t.Errorf("expected ToolCall to be overridden to false")
	}
	// Preserved base fields
	if merged.ID != "gpt-4o" {
		t.Errorf("ID = %q, want 'gpt-4o'", merged.ID)
	}
	if merged.Description != "Base description" {
		t.Errorf("Description = %q, want 'Base description'", merged.Description)
	}
	if merged.Family != "gpt-4" {
		t.Errorf("Family = %q, want 'gpt-4'", merged.Family)
	}
	if merged.Knowledge != "2024-10" {
		t.Errorf("Knowledge = %q, want '2024-10'", merged.Knowledge)
	}
}

func TestGatewayModelProfile_MergedOverrides(t *testing.T) {
	ctx := context.Background()
	cfg := Config{
		Providers: []ProviderConfig{
			{
				Name: "test-openai",
				Type: "openai",
				Auth: &AuthConfig{Type: AuthTypeAPIKey},
				Models: []ModelSpec{
					{
						ID:   "gpt-4o",
						Name: "Custom GPT-4o Override",
						Limits: ModelLimits{
							Context: 300000,
							Output:  40000,
						},
					},
				},
			},
		},
		Env: map[string]string{
			"OPENAI_API_KEY": "fake-openai-key",
		},
	}

	gw, err := New(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to initialize gateway: %v", err)
	}
	defer gw.Close()

	prof, ok := gw.ModelProfile("test-openai", "gpt-4o")
	if !ok || prof == nil {
		t.Fatalf("expected profile for test-openai/gpt-4o, got ok=%v, prof=%v", ok, prof)
	}
	if prof.Name != "Custom GPT-4o Override" {
		t.Errorf("Name = %q, want 'Custom GPT-4o Override'", prof.Name)
	}
	if prof.Limits.Context != 300000 || prof.Limits.Output != 40000 {
		t.Errorf("unexpected limits: %+v, want Context=300000, Output=40000", prof.Limits)
	}
	// Base profile description / family / modalities should still be present from catalog
	if prof.Family == "" && prof.Description == "" {
		t.Errorf("expected base catalog metadata to be preserved, got family=%q description=%q", prof.Family, prof.Description)
	}
}

func TestModelResolutionWithSlashInModelID(t *testing.T) {
	ctx := context.Background()

	cfg := Config{
		Providers: []ProviderConfig{
			{
				Name:     "mlx-serve",
				Type:     "ollama",
				Endpoint: "http://localhost:11234",
				Models: []ModelSpec{
					{
						ID:    "ddalcu/Qwen3.8-27B-MLX-Serve-4bit",
						Name:  "qwen3.8-27b-mlx-serve-4bit",
						Label: "Qwen 3.8 (MLX Serve)",
						Limits: ModelLimits{
							Context: 262144,
							Output:  32768,
						},
					},
				},
			},
		},
	}

	gw, err := New(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to initialize gateway: %v", err)
	}
	defer gw.Close()

	m, err := gw.ResolveLLM(ctx, "mlx-serve", "ddalcu/Qwen3.8-27B-MLX-Serve-4bit")
	if err != nil {
		t.Fatalf("ResolveLLM unexpected error: %v", err)
	}
	if m.Name() != "ddalcu/Qwen3.8-27B-MLX-Serve-4bit" {
		t.Errorf("ResolveLLM.Name() = %q, want %q", m.Name(), "ddalcu/Qwen3.8-27B-MLX-Serve-4bit")
	}
	prof, ok := gw.ModelProfile("mlx-serve", "ddalcu/Qwen3.8-27B-MLX-Serve-4bit")
	if !ok || prof == nil {
		t.Fatalf("ModelProfile returned false/nil")
	}
	if prof.Limits.Context != 262144 || prof.Limits.Output != 32768 {
		t.Errorf("ModelProfile limits = %+v, want 262144/32768", prof.Limits)
	}

	if _, err := gw.ResolveLLM(ctx, "unknown-prov", "some-model"); !errors.Is(err, ErrProviderNotFound) {
		t.Fatalf("expected ErrProviderNotFound for unknown provider, got: %v", err)
	}
}
