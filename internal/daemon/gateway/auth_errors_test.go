package gateway

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	loomauth "github.com/masterkeysrd/loom/auth"
	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	"github.com/openai/openai-go/v3"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway/auth"
)

type recordingAuthObserver struct {
	mu        sync.Mutex
	failures  []recordedAuthFailure
	successes []string
}

type recordedAuthFailure struct {
	providerName string
	providerType string
	state        auth.HealthState
	revision     string
	reason       string
}

func (r *recordingAuthObserver) RecordAuthFailure(providerName, providerType string, state auth.HealthState, revision, reason string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failures = append(r.failures, recordedAuthFailure{
		providerName: providerName,
		providerType: providerType,
		state:        state,
		revision:     revision,
		reason:       reason,
	})
}

func (r *recordingAuthObserver) RecordAuthSuccess(providerName string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.successes = append(r.successes, providerName)
}

func TestClassifyAuthErrorAndProviderAuthError(t *testing.T) {
	t.Run("codex_401_attributed_to_codex_not_openai", func(t *testing.T) {
		obs := &recordingAuthObserver{}
		inst := &providerInstance{
			cfg: ProviderConfig{
				Name: "codex",
				Type: "codex",
				Auth: &auth.Config{Type: auth.TypeOAuth2},
			},
		}
		rawErr := &openai.Error{
			StatusCode: http.StatusUnauthorized,
			Message:    "Provided authentication token is expired",
		}
		wrapped := wrapProviderAuthError(inst, obs, rawErr)

		pae, ok := ClassifyAuthError(wrapped)
		if !ok {
			t.Fatal("expected ClassifyAuthError to match")
		}
		if pae.Provider != "codex" || pae.Kind != AuthErrorExpired || !pae.OAuth {
			t.Fatalf("ProviderAuthError = %+v", pae)
		}
		msg := pae.UserMessage()
		if !strings.Contains(msg, `"codex"`) || !strings.Contains(msg, "/login codex") {
			t.Fatalf("UserMessage() = %q, want codex and /login codex", msg)
		}
		if strings.Contains(msg, "OpenAI API") {
			t.Fatalf("UserMessage() leaked OpenAI API: %q", msg)
		}
		if len(obs.failures) != 1 || obs.failures[0].providerName != "codex" || obs.failures[0].state != auth.HealthExpired {
			t.Fatalf("recorded failures = %+v", obs.failures)
		}
	})

	t.Run("api_key_providers_report_invalid_auth", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			prov ProviderConfig
			err  error
		}{
			{
				name: "openai",
				prov: ProviderConfig{Name: "work-openai", Type: "openai", Auth: &auth.Config{Type: auth.TypeBearer}},
				err:  &openai.Error{StatusCode: http.StatusUnauthorized, Message: "Incorrect API key"},
			},
			{
				name: "anthropic",
				prov: ProviderConfig{Name: "anthropic", Type: "anthropic", Auth: &auth.Config{Type: auth.TypeAPIKey}},
				err: &anthropic.Error{
					StatusCode: http.StatusUnauthorized,
					Request:    httptest.NewRequest(http.MethodPost, "https://api.anthropic.com/v1/messages", nil),
					Response:   &http.Response{StatusCode: http.StatusUnauthorized, Status: "401 Unauthorized"},
				},
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				obs := &recordingAuthObserver{}
				inst := &providerInstance{cfg: tc.prov, credRevision: auth.SecretRevision("bad-key")}
				wrapped := wrapProviderAuthError(inst, obs, tc.err)
				pae, ok := ClassifyAuthError(wrapped)
				if !ok || pae.Provider != tc.prov.Name || pae.Kind != AuthErrorInvalid || pae.OAuth {
					t.Fatalf("ProviderAuthError = %+v, ok=%v", pae, ok)
				}
				if string(pae.Kind) != api.ErrCodeProviderAuthInvalid {
					t.Fatalf("Kind = %q, want %q", pae.Kind, api.ErrCodeProviderAuthInvalid)
				}
				if len(obs.failures) != 1 || obs.failures[0].state != auth.HealthInvalid || obs.failures[0].revision != inst.credRevision {
					t.Fatalf("recorded failures = %+v", obs.failures)
				}
			})
		}
	})

	t.Run("missing_credential_preserves_unwrap_and_does_not_mark_expired", func(t *testing.T) {
		obs := &recordingAuthObserver{}
		inst := &providerInstance{
			cfg: ProviderConfig{Name: "codex", Type: "codex", Auth: &auth.Config{Type: auth.TypeOAuth2}},
		}
		missing := fmt.Errorf("%w: run tasksmith auth login codex", ErrMissingCredential)
		wrapped := wrapProviderAuthError(inst, obs, missing)
		if !errors.Is(wrapped, ErrMissingCredential) {
			t.Fatalf("errors.Is(ErrMissingCredential) = false for %v", wrapped)
		}
		if len(obs.failures) != 0 {
			t.Fatalf("ErrMissingCredential should not record runtime expired_auth, got %+v", obs.failures)
		}
	})

	t.Run("context_canceled_never_classified_as_auth_error", func(t *testing.T) {
		err := fmt.Errorf("antigravity: acquire token: %w", context.Canceled)
		if pae, ok := ClassifyAuthError(err); ok {
			t.Fatalf("context.Canceled classified as auth error: %+v", pae)
		}
		dlErr := fmt.Errorf("antigravity: acquire token: %w", context.DeadlineExceeded)
		if pae, ok := ClassifyAuthError(dlErr); ok {
			t.Fatalf("context.DeadlineExceeded classified as auth error: %+v", pae)
		}
	})

	t.Run("middleware_records_auth_failure_and_success", func(t *testing.T) {
		obs := &recordingAuthObserver{}
		inst := &providerInstance{
			cfg: ProviderConfig{Name: "codex", Type: "codex", Auth: &auth.Config{Type: auth.TypeOAuth2}},
		}
		mw := errorEnrichingLLMMiddleware(nil, inst, obs)

		failingStreamer := mw(func(context.Context, *llm.Request) (llm.StreamResponse, error) {
			return func(yield func(message.AssistantChunk, error) bool) {
				yield(message.AssistantChunk{}, loomauth.ErrAuthRequired)
			}, nil
		})
		stream, err := failingStreamer(context.Background(), &llm.Request{})
		if err != nil {
			t.Fatalf("unexpected immediate error: %v", err)
		}
		var streamErr error
		for _, sErr := range stream {
			streamErr = sErr
		}
		if pae, ok := ClassifyAuthError(streamErr); !ok || pae.Provider != "codex" || pae.Kind != AuthErrorExpired {
			t.Fatalf("stream error = %v, want codex ProviderAuthError", streamErr)
		}
		if len(obs.failures) != 1 || obs.failures[0].state != auth.HealthExpired {
			t.Fatalf("failures = %+v", obs.failures)
		}

		okStreamer := mw(func(context.Context, *llm.Request) (llm.StreamResponse, error) {
			return func(yield func(message.AssistantChunk, error) bool) {
				yield(message.AssistantChunk{}, nil)
			}, nil
		})
		okStream, err := okStreamer(context.Background(), &llm.Request{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for _, sErr := range okStream {
			if sErr != nil {
				t.Fatalf("unexpected stream error: %v", sErr)
			}
		}
		if len(obs.successes) != 1 || obs.successes[0] != "codex" {
			t.Fatalf("successes = %+v, want [codex]", obs.successes)
		}
	})
}
