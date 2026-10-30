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
	"github.com/masterkeysrd/tasksmith/internal/daemon/models"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
	"github.com/openai/openai-go/v3"
)

func TestConfigFingerprint(t *testing.T) {
	cfg1 := Config{
		Providers: []ProviderConfig{
			{
				Name: "ollama",
				Type: "ollama",
			},
			{
				Name: "anthropic",
				Type: "anthropic",
				Auth: &AuthConfig{Env: "ANTHROPIC_API_KEY"},
			},
		},
		Env: map[string]string{
			"ANTHROPIC_API_KEY": "sk-ant-12345",
			"UNRELATED_VAR":     "should_not_affect_hash",
		},
	}

	cfg2 := Config{
		Providers: []ProviderConfig{
			{
				Name: "anthropic",
				Type: "anthropic",
				Auth: &AuthConfig{Env: "ANTHROPIC_API_KEY"},
			},
			{
				Name: "ollama",
				Type: "ollama",
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

	t.Setenv("ANTHROPIC_API_KEY", "")

	// 1. Missing credentials: New() must succeed lazily, with warning populated
	cfgMissing := Config{
		Providers: []ProviderConfig{
			{
				Name: "anthropic",
				Type: "anthropic",
				Auth: &AuthConfig{Env: "ANTHROPIC_API_KEY"},
			},
			{
				Name:     "ollama",
				Type:     "ollama",
				Endpoint: "http://127.0.0.1:11434",
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
	_, err = gw.LLM(ctx, models.Model{ID: "anthropic/claude-3-7-sonnet"}, llm.ModelConfig{})
	if !errors.Is(err, ErrMissingCredential) {
		t.Fatalf("expected ErrMissingCredential for ResolveLLM on unauthenticated provider, got %v", err)
	}
	_, err = gw.Decision(ctx, models.Model{ID: "anthropic/claude-3-7-sonnet"}, llm.ModelConfig{})
	if !errors.Is(err, ErrMissingCredential) {
		t.Fatalf("expected ErrMissingCredential for ResolveDecision on unauthenticated provider, got %v", err)
	}
	_, err = gw.Embedding(ctx, models.Model{ID: "anthropic/claude-3-7-sonnet"})
	if !errors.Is(err, ErrMissingCredential) {
		t.Fatalf("expected ErrMissingCredential for ResolveEmbedding on unauthenticated provider, got %v", err)
	}

	// Calling ResolveLLM for healthy provider succeeds!
	healthyLLM, err := gw.LLM(ctx, models.Model{ID: "ollama/llama3.2", ProviderType: "ollama"}, llm.ModelConfig{})
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
			},
		},
	}

	gw, err := New(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to create gateway: %v", err)
	}
	defer gw.Close()

	completion := models.Model{ID: "ollama/llama3.2", Capabilities: models.Capabilities{Completion: new(true)}}
	native := models.Model{ID: "ollama/nimble-v3", Capabilities: models.Capabilities{Completion: new(false), Decision: new(true)}, Pricing: &models.Pricing{Input: 2}}
	embeddingModel := models.Model{ID: "ollama/nomic-embed-text", Capabilities: models.Capabilities{Embedding: new(true)}, Pricing: &models.Pricing{Input: 3}}
	llmModel, err := gw.LLM(ctx, completion, llm.ModelConfig{})
	if err != nil {
		t.Fatalf("failed to resolve LLM: %v", err)
	}
	if llmModel.Name() != "llama3.2" {
		t.Fatalf("expected resolved model name 'llama3.2', got %q", llmModel.Name())
	}

	// 2. Resolve Native Decision
	decModel, err := gw.Decision(ctx, native, llm.ModelConfig{})
	if err != nil {
		t.Fatalf("failed to resolve decision model: %v", err)
	}
	if decModel.Name() != "nimble-v3" || decModel.Profile() == nil || decModel.Profile().Pricing.Input != 2 {
		t.Fatalf("unexpected decision model: %q, %+v", decModel.Name(), decModel.Profile())
	}

	// 3. Resolve Decision on a completion-only model -> Transparent LLM Adapter fallback
	adaptedDecModel, err := gw.Decision(ctx, completion, llm.ModelConfig{})
	if err != nil {
		t.Fatalf("failed to resolve adapted decision model on llama3.2: %v", err)
	}
	if adaptedDecModel.Name() != "llama3.2" {
		t.Fatalf("expected adapted decision model name 'llama3.2', got %q", adaptedDecModel.Name())
	}

	// 4. Resolve Embedding
	embModel, err := gw.Embedding(ctx, embeddingModel)
	if err != nil {
		t.Fatalf("failed to resolve embedding model: %v", err)
	}
	if embModel.Name() != "nomic-embed-text" || embModel.Profile() == nil || embModel.Profile().Pricing.Input != 3 {
		t.Fatalf("unexpected embedding model: %q, %+v", embModel.Name(), embModel.Profile())
	}

	// 5. Embedding resolution on non-embedding model fails
	_, err = gw.Embedding(ctx, completion)
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
	if _, err := gw.LLM(ctx, models.Model{ID: "ollama/llama3.2", ProviderType: "ollama"}, llm.ModelConfig{}); !errors.Is(err, ErrGatewayClosed) {
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

	model, err := gw.LLM(ctx, models.Model{ID: "mock/gpt-6.1-terra", ProviderType: "openai"}, llm.ModelConfig{})
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

	model, err := gw.LLM(ctx, models.Model{ID: "openai/gpt-5.5", ProviderType: "openai"}, llm.ModelConfig{})
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
			},
			{
				Name: "my-gemini",
				Type: "gemini",
				Auth: &AuthConfig{Env: "GEMINI_API_KEY"},
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

	merged := models.Merge("test-openai/custom-spec-model", "openai", &workspace.ProviderModel{ID: "custom-spec-model", Name: "Custom Spec Model", Limits: workspace.ModelLimits{Context: 256000, Output: 32000}}, nil)
	if merged.Limits.Context != 256000 || merged.Limits.Output != 32000 {
		t.Errorf("unexpected limits from spec: %+v", merged.Limits)
	}

	profCatalog, okCatalog := gw.GetProfile("test-openai/gpt-4o")
	if okCatalog && profCatalog.Limits.Context <= 0 {
		t.Errorf("expected positive context limit for gpt-4o, got %d", profCatalog.Limits.Context)
	}
}

func TestCatalogLLMUsesIsolatedProfiles(t *testing.T) {
	ctx := context.Background()
	gw, err := New(ctx, Config{Providers: []ProviderConfig{{Name: "openai", Type: "openai", Auth: &AuthConfig{Type: AuthTypeAPIKey}}}, Env: map[string]string{"OPENAI_API_KEY": "fake"}})
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	factory := gw.(*gatewayImpl)
	model := models.Model{ID: "openai/gpt-4o", ProviderType: "openai", Limits: models.Limits{Context: 1234, Output: 123}, Pricing: &models.Pricing{Input: 7}}
	first, err := factory.LLM(ctx, model, llm.ModelConfig{})
	if err != nil {
		t.Fatal(err)
	}
	model.Pricing = &models.Pricing{Input: 9}
	second, err := factory.LLM(ctx, model, llm.ModelConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if first.Profile().Pricing.Input != 7 || second.Profile().Pricing.Input != 9 || first.Profile().Limits.Context != 1234 {
		t.Fatalf("profiles not isolated: %+v / %+v", first.Profile(), second.Profile())
	}
	base, ok := gw.GetProfile("openai/gpt-4o")
	if !ok || base.Pricing.Input == 7 || base.Pricing.Input == 9 {
		t.Fatalf("shared provider was modified: %+v", base)
	}
}

func TestGatewayBaseProfiles(t *testing.T) {
	ctx := context.Background()
	cfg := Config{
		Providers: []ProviderConfig{
			{Name: "test-openai", Type: "openai", Auth: &AuthConfig{Type: AuthTypeAPIKey}},
		},
		Env: map[string]string{"OPENAI_API_KEY": "fake-openai-key"},
	}
	gw, err := New(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to initialize gateway: %v", err)
	}
	defer gw.Close()

	prof, ok := gw.GetProfile("test-openai/gpt-4o")
	if !ok {
		t.Fatal("expected base profile for test-openai/gpt-4o")
	}
	if prof.ID == "" {
		t.Fatalf("expected non-empty profile id, got %+v", prof)
	}

	if _, ok := gw.GetProfile("gpt-4o"); ok {
		t.Fatal("unqualified profile ID should be rejected")
	}
	if _, ok := gw.GetProfile("test-openai/gpt-4o"); !ok {
		t.Fatal("qualified profile ID should be routed")
	}

	// Unknown provider/model returns false.
	if _, ok := gw.GetProfile("nope/model"); ok {
		t.Fatal("GetProfile should fail for unknown provider")
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

	base, ok := gw.GetProfile("test-openai/gpt-4o")
	if !ok {
		t.Fatal("expected built-in profile")
	}
	prof := models.Merge("test-openai/gpt-4o", "openai", &workspace.ProviderModel{ID: "gpt-4o", Name: "Custom GPT-4o Override", Limits: workspace.ModelLimits{Context: 300000, Output: 40000}}, &base)
	if prof.Name != "Custom GPT-4o Override" {
		t.Errorf("Name = %q, want 'Custom GPT-4o Override'", prof.Name)
	}
	if prof.Limits.Context != 300000 || prof.Limits.Output != 40000 {
		t.Errorf("unexpected limits: %+v, want Context=300000, Output=40000", prof.Limits)
	}
	if prof.Capabilities.Tools == nil || *prof.Capabilities.Tools != base.Capabilities.ToolCall {
		t.Fatalf("built-in capability lost: %+v", prof.Capabilities)
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
			},
		},
	}

	gw, err := New(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to initialize gateway: %v", err)
	}
	defer gw.Close()

	effective := models.Merge("mlx-serve/ddalcu/Qwen3.8-27B-MLX-Serve-4bit", "ollama", &workspace.ProviderModel{ID: "ddalcu/Qwen3.8-27B-MLX-Serve-4bit", Limits: workspace.ModelLimits{Context: 262144, Output: 32768}}, nil)
	m, err := gw.LLM(ctx, effective, llm.ModelConfig{})
	if err != nil {
		t.Fatalf("LLM unexpected error: %v", err)
	}
	if m.Name() != "ddalcu/Qwen3.8-27B-MLX-Serve-4bit" {
		t.Errorf("LLM.Name() = %q, want %q", m.Name(), "ddalcu/Qwen3.8-27B-MLX-Serve-4bit")
	}
	if prof := m.Profile(); prof == nil || prof.Limits.Context != 262144 || prof.Limits.Output != 32768 {
		t.Errorf("model profile = %+v, want limits 262144/32768", prof)
	}

	if _, err := gw.LLM(ctx, models.Model{ID: "unknown-prov/some-model"}, llm.ModelConfig{}); !errors.Is(err, ErrProviderNotFound) {
		t.Fatalf("expected ErrProviderNotFound for unknown provider, got: %v", err)
	}
}
