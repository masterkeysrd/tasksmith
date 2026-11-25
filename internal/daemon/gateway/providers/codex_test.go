package providers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	loomauth "github.com/masterkeysrd/loom/auth"
	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	loomcodex "github.com/masterkeysrd/loom/providers/codex"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway/auth"
	"golang.org/x/oauth2"
)

func TestCodexRegistrationAndHostID(t *testing.T) {
	xdg.SetTestEnv(t.TempDir(), "tasksmith-codex-test")
	t.Cleanup(xdg.ClearCache)
	driver := newCodexDriver()
	hostID, err := driver.hostID()
	if err != nil {
		t.Fatal(err)
	}
	if err := driver.Save(loomcodex.Credentials{
		HostID: hostID, ClientID: "test-client", Subject: "test-subject",
		Scopes: []string{"chatgpt.tokens.use.direct"},
		Token:  oauth2.Token{AccessToken: "fixture-access", Expiry: time.Now().Add(time.Hour)},
	}); err != nil {
		t.Fatal(err)
	}
	path, err := xdg.SubDataDir("credentials", CodexName, "registration.json")
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("registration mode = %o, want 600", got)
	}
	if deleted, err := driver.Logout(); err != nil || !deleted {
		t.Fatalf("Logout() = %v, %v", deleted, err)
	}
	if got, err := driver.hostID(); err != nil || got != hostID {
		t.Fatalf("host ID changed on logout: %q, %v", got, err)
	}
}

type codexRoundTripFunc func(*http.Request) (*http.Response, error)

func (f codexRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestCodex401RefreshesOnceAndReplaysStream(t *testing.T) {
	xdg.SetTestEnv(t.TempDir(), "tasksmith-codex-401-test")
	t.Cleanup(xdg.ClearCache)
	t.Cleanup(func() { _, _ = codexOAuth.Logout() })

	var rejectRefresh atomic.Bool
	var refreshCount atomic.Int32
	var responseCalls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/accounts/oauth/token":
			refreshCount.Add(1)
			if rejectRefresh.Load() {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"refresh token revoked"}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"fresh-codex-access","refresh_token":"refresh-2","token_type":"Bearer","expires_in":3600}`))
		case "/v1/responses":
			responseCalls.Add(1)
			authHeader := r.Header.Get("Authorization")
			if authHeader != "Bearer fresh-codex-access" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":{"message":"Provided authentication token is expired"}}`))
				return
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			var payload map[string]any
			if err := json.Unmarshal(body, &payload); err != nil || payload["model"] != "gpt-5.3-codex" {
				http.Error(w, "bad replayed body", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"Recovered!\"}\n\n" +
				"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp-1\"}}\n\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	// Route auth.openai.com and api.openai.com to the local httptest.Server.
	origClient := http.DefaultClient
	t.Cleanup(func() { http.DefaultClient = origClient })
	http.DefaultClient = &http.Client{
		Transport: codexRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			cloned := req.Clone(req.Context())
			cloned.URL.Scheme = "http"
			cloned.URL.Host = strings.TrimPrefix(srv.URL, "http://")
			return srv.Client().Transport.RoundTrip(cloned)
		}),
	}

	if err := codexOAuth.Save(loomcodex.Credentials{
		HostID:   loomcodex.NewHostID(),
		ClientID: "test-client",
		Subject:  "sub-1",
		Email:    "dev@example.test",
		Scopes:   []string{"chatgpt.tokens.use.direct"},
		Token: oauth2.Token{
			AccessToken:  "stale-codex-access",
			RefreshToken: "refresh-1",
			Expiry:       time.Now().Add(time.Hour),
		},
	}); err != nil {
		t.Fatal(err)
	}

	prov, err := NewCodex(CodexName, &auth.Config{Type: auth.TypeOAuth2}, false)
	if err != nil {
		t.Fatalf("NewCodex: %v", err)
	}
	model, err := llm.NewModel(prov, "gpt-5.3-codex", &llm.ModelConfig{})
	if err != nil {
		t.Fatalf("llm.NewModel: %v", err)
	}

	reply, err := model.Invoke(context.Background(), message.MessageList{message.NewUserText("Hi")})
	if err != nil {
		t.Fatalf("Invoke after 401 refresh failed: %v", err)
	}
	if got := reply.Content.Text(); got != "Recovered!" {
		t.Fatalf("reply = %q, want Recovered!", got)
	}
	if refreshCount.Load() != 1 {
		t.Fatalf("refreshCount = %d, want 1", refreshCount.Load())
	}
	if responseCalls.Load() != 2 {
		t.Fatalf("responseCalls = %d, want 2 (initial 401 + replay)", responseCalls.Load())
	}

	// Now force another 401 while the token endpoint returns invalid_grant.
	rejectRefresh.Store(true)
	if err := codexOAuth.Save(loomcodex.Credentials{
		HostID:   loomcodex.NewHostID(),
		ClientID: "test-client",
		Subject:  "sub-1",
		Email:    "dev@example.test",
		Scopes:   []string{"chatgpt.tokens.use.direct"},
		Token: oauth2.Token{
			AccessToken:  "revoked-access",
			RefreshToken: "revoked-refresh",
			Expiry:       time.Now().Add(time.Hour),
		},
	}); err != nil {
		t.Fatal(err)
	}
	prov2, err := NewCodex(CodexName, &auth.Config{Type: auth.TypeOAuth2}, false)
	if err != nil {
		t.Fatalf("NewCodex 2: %v", err)
	}
	model2, err := llm.NewModel(prov2, "gpt-5.3-codex", &llm.ModelConfig{})
	if err != nil {
		t.Fatalf("llm.NewModel 2: %v", err)
	}
	if _, err := model2.Invoke(context.Background(), message.MessageList{message.NewUserText("Hi again")}); !errors.Is(err, loomauth.ErrAuthRequired) {
		t.Fatalf("Invoke on invalid_grant = %v, want loomauth.ErrAuthRequired", err)
	}
	if email, ok := codexOAuth.Account(); !ok || email != "dev@example.test" {
		t.Fatalf("Account() after invalid_grant = %q, %v (must preserve email)", email, ok)
	}
}
