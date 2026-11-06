package gateway

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/masterkeysrd/loom/embedding"
	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/internal/daemon/models"
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
