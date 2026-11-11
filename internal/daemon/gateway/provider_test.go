package gateway

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/masterkeysrd/loom/embedding"
	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	loomantigravity "github.com/masterkeysrd/loom/providers/antigravity"
	"github.com/masterkeysrd/tasksmith/internal/daemon/metrics"
	"github.com/masterkeysrd/tasksmith/internal/daemon/models"
	"github.com/masterkeysrd/tasksmith/internal/daemon/scope"
)

// captureUpstream serves a canned non-streaming chat completion and records the
// headers of the request that produced it.
func captureUpstream(t *testing.T, providerType string) (*httptest.Server, func() string) {
	t.Helper()

	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		if providerType == "anthropic" {
			_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-5","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"chatcmpl-1","object":"chat.completion","model":"gpt-5.5","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	t.Cleanup(srv.Close)

	header := func() string {
		var b strings.Builder
		for _, name := range []string{"Authorization", "X-Api-Key", "Api-Key", "X-Auth", "Proxy-Authorization"} {
			if v := got.Get(name); v != "" {
				b.WriteString(name)
				b.WriteString("=")
				b.WriteString(v)
				b.WriteString(";")
			}
		}
		return b.String()
	}
	return srv, header
}

func TestProviderAuthHeadersEndToEnd(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name         string
		providerType string
		auth         *AuthConfig
		envKey       string
		want         string
		deny         []string
	}{
		{
			name:         "openai env sends bearer",
			providerType: "openai",
			auth:         &AuthConfig{Type: AuthTypeEnv, Env: "E2E_KEY"},
			envKey:       "E2E_KEY",
			want:         "Authorization=Bearer sk-e2e;",
		},
		{
			name:         "openai basic replaces the bearer default",
			providerType: "openai",
			auth:         &AuthConfig{Type: AuthTypeBasic, Env: "E2E_KEY"},
			envKey:       "E2E_KEY",
			want:         "Authorization=Basic " + base64.StdEncoding.EncodeToString([]byte("sk-e2e")) + ";",
		},
		{
			name:         "openai api-key sends the raw token",
			providerType: "openai",
			auth:         &AuthConfig{Type: AuthTypeAPIKey, Env: "E2E_KEY"},
			envKey:       "E2E_KEY",
			want:         "Authorization=sk-e2e;",
		},
		{
			name:         "openai custom header drops the SDK bearer",
			providerType: "openai",
			auth:         &AuthConfig{Type: AuthTypeAPIKey, Header: "X-Auth", Env: "E2E_KEY"},
			envKey:       "E2E_KEY",
			want:         "X-Auth=sk-e2e;",
		},
		{
			name:         "anthropic env keeps the vendor key header",
			providerType: "anthropic",
			auth:         &AuthConfig{Type: AuthTypeEnv, Env: "E2E_KEY"},
			envKey:       "E2E_KEY",
			want:         "Authorization=Bearer sk-e2e;X-Api-Key=sk-e2e;",
		},
		{
			name:         "anthropic bearer drops the vendor key header",
			providerType: "anthropic",
			auth:         &AuthConfig{Type: AuthTypeBearer, Env: "E2E_KEY"},
			envKey:       "E2E_KEY",
			want:         "Authorization=Bearer sk-e2e;",
		},
		{
			name:         "anthropic custom header drops the vendor key",
			providerType: "anthropic",
			auth:         &AuthConfig{Type: AuthTypeBasic, Header: "X-Auth", Env: "E2E_KEY"},
			envKey:       "E2E_KEY",
			want:         "X-Auth=Basic " + base64.StdEncoding.EncodeToString([]byte("sk-e2e")) + ";",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(tc.envKey, "sk-e2e")

			srv, seen := captureUpstream(t, tc.providerType)

			gw, err := New(ctx, Config{
				Providers: []ProviderConfig{{
					Name:     "e2e",
					Type:     tc.providerType,
					Endpoint: srv.URL,
					Auth:     tc.auth,
				}},
			})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			defer func() { _ = gw.Close() }()

			inst := gw.(*gatewayImpl).providers["e2e"]
			if inst.authErr != nil {
				t.Fatalf("unexpected auth error: %v", inst.authErr)
			}
			if inst.llmProv == nil {
				t.Fatal("provider has no llm implementation")
			}

			stream, err := inst.llmProv.Stream(ctx, &llm.Request{
				Model:    "e2e/model",
				Messages: []message.Message{message.NewUserText("hi")},
			})
			if err != nil {
				t.Fatalf("Stream: %v", err)
			}
			for range stream {
			}

			if got := seen(); got != tc.want {
				t.Fatalf("auth headers = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestProviderAuthNoneSendsNoCredential(t *testing.T) {
	ctx := context.Background()

	t.Run("openai", func(t *testing.T) {
		srv, seen := captureUpstream(t, "openai")

		gw, err := New(ctx, Config{
			Providers: []ProviderConfig{{
				Name:     "local",
				Type:     "openai",
				Endpoint: srv.URL,
				Auth:     &AuthConfig{Type: AuthTypeNone},
			}},
		})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		defer func() { _ = gw.Close() }()

		inst := gw.(*gatewayImpl).providers["local"]
		if inst.authErr != nil {
			t.Fatalf("unexpected auth error: %v", inst.authErr)
		}

		stream, err := inst.llmProv.Stream(ctx, &llm.Request{
			Model:    "local/model",
			Messages: []message.Message{message.NewUserText("hi")},
		})
		if err != nil {
			t.Fatalf("Stream: %v", err)
		}
		for range stream {
		}

		if got := seen(); got != "" {
			t.Fatalf("expected no credential headers with auth.type none, got %q", got)
		}
	})

	t.Run("google-genai drops the SDK placeholder", func(t *testing.T) {
		var observedPath, apiKeyHeader string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			observedPath = r.URL.Path
			apiKeyHeader = r.Header.Get("x-goog-api-key")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"embeddings":[]}`))
		}))
		defer srv.Close()

		gw, err := New(ctx, Config{
			Providers: []ProviderConfig{{
				Name:     "proxy",
				Type:     "gemini",
				Endpoint: srv.URL + "/v1beta",
				Auth:     &AuthConfig{Type: AuthTypeNone},
			}},
		})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		defer func() { _ = gw.Close() }()

		inst := gw.(*gatewayImpl).providers["proxy"]
		if inst.authErr != nil {
			t.Fatalf("unexpected auth error: %v", inst.authErr)
		}

		if _, err := inst.embeddingProv.Embed(ctx, &embedding.Request{
			Model:  "text-embedding-004",
			Inputs: []embedding.Input{{Text: "hello"}},
		}); err != nil {
			t.Fatalf("Embed against test endpoint: %v", err)
		}

		if !strings.HasPrefix(observedPath, "/v1beta/models/") {
			t.Fatalf("request path = %q, want it under /v1beta/models/", observedPath)
		}
		if apiKeyHeader != "" {
			t.Fatalf("x-goog-api-key = %q, want the SDK placeholder stripped", apiKeyHeader)
		}
	})
}

func TestProviderMissingCredentialWarningNamesFile(t *testing.T) {
	cfg := Config{
		Providers: []ProviderConfig{{
			Name: "internal",
			Type: "openai",
			Auth: &AuthConfig{Type: AuthTypeBearer, File: "~/absent/token"},
		}},
	}

	gw, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("expected lazy initialization, got: %v", err)
	}
	defer func() { _ = gw.Close() }()

	infos := gw.ListProviders()
	if len(infos) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(infos))
	}
	if !strings.Contains(infos[0].Warning, "absent/token") {
		t.Fatalf("warning should name the credential file, got: %q", infos[0].Warning)
	}

	if _, err := gw.LLM(context.Background(), models.Model{ID: "internal/gpt-5.5", ProviderType: "openai"}, llm.ModelConfig{}); err == nil {
		t.Fatal("expected LLM resolution to fail for a provider without credentials")
	}
}

func TestAntigravityProviderConfigEndToEnd(t *testing.T) {
	ctx := context.Background()
	t.Setenv("AG_TEST_TOKEN", "test-ag-token")

	var (
		gotUserAgentHeader  string
		gotBillingProject   string
		gotEnvelopeProject  string
		gotEnvelopeModel    string
		gotEnvelopeAgentStr string
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1internal:fetchAvailableModels":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"models": map[string]any{
					"gemini-4-carbon-high": map[string]any{
						"displayName":      "Gemini 4 Carbon (High)",
						"maxTokens":        1048576,
						"maxOutputTokens":  65536,
						"supportsThinking": true,
					},
				},
			})
		case "/v1internal:streamGenerateContent":
			gotUserAgentHeader = r.Header.Get("User-Agent")
			gotBillingProject = r.Header.Get("X-Goog-User-Project")
			var env struct {
				Project   string `json:"project"`
				Model     string `json:"model"`
				UserAgent string `json:"userAgent"`
			}
			_ = json.NewDecoder(r.Body).Decode(&env)
			gotEnvelopeProject = env.Project
			gotEnvelopeModel = env.Model
			gotEnvelopeAgentStr = env.UserAgent

			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ok\"}]},\"finishReason\":\"STOP\"}]}}\n\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	gw, err := New(ctx, Config{
		Providers: []ProviderConfig{{
			Name:     "antigravity",
			Type:     "antigravity",
			Endpoint: srv.URL,
			Auth:     &AuthConfig{Type: AuthTypeOAuth2, Env: "AG_TEST_TOKEN"},
			Headers:  map[string]string{"User-Agent": "antigravity/cli/9.9.9 (test)"},
			Config: map[string]any{
				"companionProject": "companion-proj-123",
				"projectId":        "billing-proj-456",
				"aliases": map[string]any{
					"carbon": "gemini-4-carbon-high",
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = gw.Close() }()

	// Verify compacted canonical profile resolves from per-effort backend endpoint.
	prof, ok := gw.GetProfile("antigravity/gemini-4-carbon")
	if !ok {
		t.Fatal("expected GetProfile(antigravity/gemini-4-carbon) to succeed")
	}
	if prof.Limits.Context != 1048576 {
		t.Fatalf("profile context = %d, want 1048576", prof.Limits.Context)
	}

	inst := gw.(*gatewayImpl).providers["antigravity"]
	stream, err := inst.llmProv.Stream(ctx, &llm.Request{
		Model:    "carbon",
		Messages: []message.Message{message.NewUserText("ping")},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for _, sErr := range stream {
		if sErr != nil {
			t.Fatalf("stream chunk error: %v", sErr)
		}
	}

	if gotUserAgentHeader != "antigravity/cli/9.9.9 (test)" {
		t.Errorf("User-Agent header = %q, want %q", gotUserAgentHeader, "antigravity/cli/9.9.9 (test)")
	}
	if gotEnvelopeAgentStr != "antigravity/cli/9.9.9 (test)" {
		t.Errorf("envelope userAgent = %q, want %q", gotEnvelopeAgentStr, "antigravity/cli/9.9.9 (test)")
	}
	if gotBillingProject != "billing-proj-456" {
		t.Errorf("X-Goog-User-Project = %q, want %q", gotBillingProject, "billing-proj-456")
	}
	if gotEnvelopeProject != "companion-proj-123" {
		t.Errorf("envelope project = %q, want %q", gotEnvelopeProject, "companion-proj-123")
	}
	if gotEnvelopeModel != "gemini-4-carbon-high" {
		t.Errorf("envelope model = %q, want %q", gotEnvelopeModel, "gemini-4-carbon-high")
	}

	// Invalid config field type should fail fast at gateway creation.
	if _, err := New(ctx, Config{
		Providers: []ProviderConfig{{
			Name:   "antigravity",
			Type:   "antigravity",
			Config: map[string]any{"companionProject": 123},
		}},
	}); err == nil {
		t.Fatal("expected invalid companionProject type to fail")
	}
}

func TestAntigravitySessionAffinityMiddleware(t *testing.T) {
	ctx := context.Background()
	t.Setenv("AG_TEST_TOKEN", "test-ag-token")

	var (
		gotUserPromptID string
		gotSessionID    string
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1internal:fetchAvailableModels":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"models": map[string]any{
					"gemini-4-carbon": map[string]any{
						"displayName":     "Gemini 4 Carbon",
						"maxTokens":       1048576,
						"maxOutputTokens": 65536,
					},
				},
			})
		case "/v1internal:streamGenerateContent":
			var env struct {
				UserPromptID string `json:"user_prompt_id"`
				Request      struct {
					SessionID string `json:"session_id"`
				} `json:"request"`
			}
			_ = json.NewDecoder(r.Body).Decode(&env)
			gotUserPromptID = env.UserPromptID
			gotSessionID = env.Request.SessionID

			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ok\"}]},\"finishReason\":\"STOP\"}]}}\n\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	gw, err := New(ctx, Config{
		Providers: []ProviderConfig{{
			Name:     "antigravity",
			Type:     "antigravity",
			Endpoint: srv.URL,
			Auth:     &AuthConfig{Type: AuthTypeOAuth2, Env: "AG_TEST_TOKEN"},
			Config:   map[string]any{"companionProject": "companion-proj-123"},
		}},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = gw.Close() }()

	completion := true
	llmModel, err := gw.LLM(ctx, models.Model{
		ID:           "antigravity/gemini-4-carbon",
		ProviderType: "antigravity",
		Capabilities: models.Capabilities{Completion: &completion},
	}, llm.ModelConfig{})
	if err != nil {
		t.Fatalf("gw.LLM: %v", err)
	}

	// 1. Chat turn injects SessionID and TurnID from daemon scope.
	chatCtx := scope.With(ctx, scope.Scope{
		SessionID: "sess-abc-123",
		TurnID:    "turn-xyz-456",
		Purpose:   string(metrics.PurposeChat),
	})
	if _, err := llmModel.Invoke(chatCtx, []message.Message{message.NewUserText("hello")}); err != nil {
		t.Fatalf("Invoke(chat): %v", err)
	}
	if gotSessionID != "sess-abc-123" {
		t.Errorf("chat request.session_id = %q, want %q", gotSessionID, "sess-abc-123")
	}
	if gotUserPromptID != "turn-xyz-456" {
		t.Errorf("chat user_prompt_id = %q, want %q", gotUserPromptID, "turn-xyz-456")
	}

	// 2. Background title generation does not pin the chat SessionID so concurrent
	// title prompts never collide with the session's sticky KV-cache slot.
	titleCtx := scope.With(ctx, scope.Scope{
		SessionID: "sess-abc-123",
		Purpose:   string(metrics.PurposeTitle),
	})
	if _, err := llmModel.Invoke(titleCtx, []message.Message{message.NewUserText("title")}); err != nil {
		t.Fatalf("Invoke(title): %v", err)
	}
	if gotSessionID != "" {
		t.Errorf("title request.session_id = %q, want empty", gotSessionID)
	}
	if gotUserPromptID != "" {
		t.Errorf("title user_prompt_id = %q, want empty", gotUserPromptID)
	}

	// 3. Explicit per-call Session extension takes precedence over scope defaults.
	if _, err := llmModel.Invoke(
		chatCtx,
		[]message.Message{message.NewUserText("override")},
		llm.WithExtensionOption(loomantigravity.Session{ID: "custom-sess", UserPromptID: "custom-prompt"}),
	); err != nil {
		t.Fatalf("Invoke(override): %v", err)
	}
	if gotSessionID != "custom-sess" {
		t.Errorf("override request.session_id = %q, want %q", gotSessionID, "custom-sess")
	}
	if gotUserPromptID != "custom-prompt" {
		t.Errorf("override user_prompt_id = %q, want %q", gotUserPromptID, "custom-prompt")
	}
}
