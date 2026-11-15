package gateway

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/masterkeysrd/loom/embedding"
)

// captureTransport runs one request through the gateway transport on top of a
// server that records the headers it actually received.
func captureTransport(t *testing.T, auth *AuthConfig, token string, custom map[string]string, providerType string) http.Header {
	t.Helper()

	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)

	transport := buildHTTPTransport(nil, auth, token, custom, providerType)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := (&http.Client{Transport: transport}).Do(req)
	if err != nil {
		t.Fatalf("round trip: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	return got
}

func TestResolveCredentialNoneSkipsSources(t *testing.T) {
	t.Setenv("GATEWAY_NONE_KEY", "secret-from-env")

	dir := t.TempDir()
	file := filepath.Join(dir, "token")
	if err := os.WriteFile(file, []byte("secret-from-file\n"), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}

	auth := &AuthConfig{Type: AuthTypeNone, Env: "GATEWAY_NONE_KEY", File: file}
	token, err := resolveCredential(auth, nil)
	if err != nil {
		t.Fatalf("resolveCredential: %v", err)
	}
	if token != "" {
		t.Fatalf("expected no credential for scheme none, got %q", token)
	}
}

func TestResolveCredentialExpandsHomePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := os.MkdirAll(filepath.Join(home, ".config", "tasksmith"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".config", "tasksmith", "token"), []byte(" tilde-token \n"), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}

	token, err := resolveCredential(&AuthConfig{Type: AuthTypeBearer, File: "~/.config/tasksmith/token"}, nil)
	if err != nil {
		t.Fatalf("resolveCredential: %v", err)
	}
	if token != "tilde-token" {
		t.Fatalf("token = %q, want %q", token, "tilde-token")
	}
}

func TestResolveCredentialMissingFile(t *testing.T) {
	_, err := resolveCredential(&AuthConfig{Type: AuthTypeBearer, File: "~/definitely-not-here/token"}, nil)
	if err == nil {
		t.Fatal("expected error for missing credential file")
	}
	if !strings.Contains(err.Error(), "definitely-not-here") {
		t.Fatalf("error should name the configured path, got: %v", err)
	}
}

func TestTransportAuthSchemes(t *testing.T) {
	tests := []struct {
		name       string
		auth       *AuthConfig
		token      string
		wantHeader string
		wantValue  string
	}{
		{
			name:       "env defaults to bearer authorization",
			auth:       &AuthConfig{Type: AuthTypeEnv},
			token:      "tok",
			wantHeader: "Authorization",
			wantValue:  "Bearer tok",
		},
		{
			name:       "omitted type defaults to bearer authorization",
			auth:       &AuthConfig{},
			token:      "tok",
			wantHeader: "Authorization",
			wantValue:  "Bearer tok",
		},
		{
			name:       "env with custom header",
			auth:       &AuthConfig{Type: AuthTypeEnv, Header: "Proxy-Authorization"},
			token:      "tok",
			wantHeader: "Proxy-Authorization",
			wantValue:  "Bearer tok",
		},
		{
			name:       "bearer",
			auth:       &AuthConfig{Type: AuthTypeBearer},
			token:      "tok",
			wantHeader: "Authorization",
			wantValue:  "Bearer tok",
		},
		{
			name:       "api-key sends raw token",
			auth:       &AuthConfig{Type: AuthTypeAPIKey},
			token:      "tok",
			wantHeader: "Authorization",
			wantValue:  "tok",
		},
		{
			name:       "api-key with custom header",
			auth:       &AuthConfig{Type: AuthTypeAPIKey, Header: "X-Api-Key"},
			token:      "tok",
			wantHeader: "X-Api-Key",
			wantValue:  "tok",
		},
		{
			name:       "basic encodes credentials",
			auth:       &AuthConfig{Type: AuthTypeBasic},
			token:      "user:pass",
			wantHeader: "Authorization",
			wantValue:  "Basic " + base64.StdEncoding.EncodeToString([]byte("user:pass")),
		},
		{
			name:       "uppercase scheme is normalized",
			auth:       &AuthConfig{Type: "  BEARER "},
			token:      "tok",
			wantHeader: "Authorization",
			wantValue:  "Bearer tok",
		},
		{
			name:       "oauth2 with explicit token defaults to bearer authorization",
			auth:       &AuthConfig{Type: AuthTypeOAuth2},
			token:      "tok",
			wantHeader: "Authorization",
			wantValue:  "Bearer tok",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := captureTransport(t, tc.auth, tc.token, nil, "openai")
			if v := got.Get(tc.wantHeader); v != tc.wantValue {
				t.Fatalf("header %s = %q, want %q", tc.wantHeader, v, tc.wantValue)
			}
		})
	}
}

func TestTransportNoneSendsNoAuth(t *testing.T) {
	got := captureTransport(t, &AuthConfig{Type: AuthTypeNone}, "ignored", nil, "openai")
	if v := got.Get("Authorization"); v != "" {
		t.Fatalf("expected no Authorization header, got %q", v)
	}
}

// The transport is the last layer before the wire, so a configured auth header
// must beat whatever the provider SDK attached.
func TestTransportOverridesSDKAuthHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if v := r.Header.Get("Authorization"); v != "Basic "+base64.StdEncoding.EncodeToString([]byte("user:pass")) {
			t.Errorf("Authorization = %q, want gateway basic credentials", v)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	transport := buildHTTPTransport(nil, &AuthConfig{Type: AuthTypeBasic}, "user:pass", nil, "openai")
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	// Simulate the SDK's own credential header.
	req.Header.Set("Authorization", "Bearer sdk-default")

	resp, err := (&http.Client{Transport: transport}).Do(req)
	if err != nil {
		t.Fatalf("round trip: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
}

func TestTransportDropsRedundantSDKHeaders(t *testing.T) {
	tests := []struct {
		name         string
		auth         *AuthConfig
		emptyToken   bool
		providerType string
		sdkHeaders   map[string]string
		wantKept     []string
		wantDropped  []string
	}{
		{
			name:         "vendor default keeps the SDK header",
			auth:         &AuthConfig{Type: AuthTypeEnv},
			providerType: "anthropic",
			sdkHeaders:   map[string]string{"X-Api-Key": "tok", "Authorization": "Bearer stale"},
			wantKept:     []string{"X-Api-Key"},
			wantDropped:  nil,
		},
		{
			name:         "custom header removes vendor credential header",
			auth:         &AuthConfig{Type: AuthTypeBearer, Header: "X-Auth"},
			providerType: "anthropic",
			sdkHeaders:   map[string]string{"X-Api-Key": "tok", "Authorization": "Bearer stale"},
			wantKept:     nil,
			wantDropped:  []string{"X-Api-Key"},
		},
		{
			name:         "none removes the google key header",
			auth:         &AuthConfig{Type: AuthTypeNone},
			providerType: "google-genai",
			sdkHeaders:   map[string]string{"x-goog-api-key": "auth-none"},
			wantKept:     nil,
			wantDropped:  []string{"x-goog-api-key"},
		},
		{
			name:         "oauth2 keeps sdk bearer authorization header",
			auth:         &AuthConfig{Type: AuthTypeOAuth2},
			emptyToken:   true,
			providerType: "antigravity",
			sdkHeaders:   map[string]string{"Authorization": "Bearer dynamic-oauth-token"},
			wantKept:     []string{"Authorization"},
			wantDropped:  nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got http.Header
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = r.Header.Clone()
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{}`))
			}))
			defer srv.Close()

			token := "tok"
			if tc.emptyToken {
				token = ""
			}
			transport := buildHTTPTransport(nil, tc.auth, token, nil, tc.providerType)
			req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
			if err != nil {
				t.Fatalf("build request: %v", err)
			}
			for k, v := range tc.sdkHeaders {
				req.Header.Set(k, v)
			}

			resp, err := (&http.Client{Transport: transport}).Do(req)
			if err != nil {
				t.Fatalf("round trip: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()

			for _, name := range tc.wantKept {
				if got.Get(name) == "" {
					t.Errorf("expected header %s to survive", name)
				}
			}
			for _, name := range tc.wantDropped {
				if got.Get(name) != "" {
					t.Errorf("expected header %s to be stripped, got %q", name, got.Get(name))
				}
			}
		})
	}
}

func TestFingerprintTracksCredentialFileRotation(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "token")
	if err := os.WriteFile(file, []byte("token-v1"), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}

	providers := []ProviderConfig{
		{Name: "internal", Type: "openai", Endpoint: "https://llm.internal/v1", Auth: &AuthConfig{Type: AuthTypeBearer, File: file}},
	}
	cfg := Config{Providers: providers}

	before := cfg.Fingerprint()

	if err := os.WriteFile(file, []byte("token-v2"), 0o600); err != nil {
		t.Fatalf("rotate token: %v", err)
	}
	after := cfg.Fingerprint()

	if before == after {
		t.Fatal("expected fingerprint to change after rotating the credential file")
	}

	// The digest must not be the secret itself.
	if strings.Contains(before, "token-v1") {
		t.Fatal("fingerprint payload must not contain the credential")
	}
}

func TestFingerprintChangesWhenCredentialFileAppears(t *testing.T) {
	file := filepath.Join(t.TempDir(), "token")
	cfg := Config{Providers: []ProviderConfig{
		{Name: "internal", Type: "openai", Auth: &AuthConfig{Type: AuthTypeBearer, File: file}},
	}}

	before := cfg.Fingerprint()
	if err := os.WriteFile(file, []byte("token-v1"), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	if cfg.Fingerprint() == before {
		t.Fatal("expected fingerprint to change once the credential file exists")
	}
}

func TestFingerprintStableForSameCredential(t *testing.T) {
	file := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(file, []byte("token-v1\n"), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}

	auth := &AuthConfig{Type: AuthTypeBearer, File: file}
	cfgA := Config{Providers: []ProviderConfig{{Name: "a", Type: "openai", Auth: auth}}}
	cfgB := Config{Providers: []ProviderConfig{{Name: "a", Type: "openai", Auth: auth}}}

	if cfgA.Fingerprint() != cfgB.Fingerprint() {
		t.Fatal("expected identical fingerprints for identical credential files")
	}
}

func TestValidateRejectsUnknownAuthScheme(t *testing.T) {
	valid := Config{Providers: []ProviderConfig{
		{Name: "a", Type: "openai", Auth: &AuthConfig{Type: "  BEARER "}},
		{Name: "b", Type: "openai", Auth: &AuthConfig{Type: AuthTypeNone}},
		{Name: "c", Type: "ollama"},
		{Name: "d", Type: "antigravity", Auth: &AuthConfig{Type: AuthTypeOAuth2}},
	}}
	if err := valid.Validate(); err != nil {
		t.Fatalf("expected normalized and missing auth schemes to validate, got: %v", err)
	}

	invalid := Config{Providers: []ProviderConfig{
		{Name: "a", Type: "openai", Auth: &AuthConfig{Type: "unknown_scheme"}},
	}}
	err := invalid.Validate()
	if err == nil {
		t.Fatal("expected unknown auth scheme to fail validation")
	}
	if !strings.Contains(err.Error(), "unknown_scheme") {
		t.Fatalf("error should name the offending scheme, got: %v", err)
	}
}

func TestSplitGenAIEndpoint(t *testing.T) {
	tests := []struct {
		endpoint   string
		wantBase   string
		wantAPIVer string
	}{
		{endpoint: "", wantBase: "", wantAPIVer: ""},
		{endpoint: "   ", wantBase: "", wantAPIVer: ""},
		{endpoint: "https://generativelanguage.googleapis.com/v1beta", wantBase: "https://generativelanguage.googleapis.com", wantAPIVer: "v1beta"},
		{endpoint: "https://generativelanguage.googleapis.com/v1beta/", wantBase: "https://generativelanguage.googleapis.com", wantAPIVer: "v1beta"},
		{endpoint: "https://proxy.example.com/v1", wantBase: "https://proxy.example.com", wantAPIVer: "v1"},
		{endpoint: "https://proxy.example.com/v1alpha", wantBase: "https://proxy.example.com", wantAPIVer: "v1alpha"},
		{endpoint: "https://proxy.example.com", wantBase: "https://proxy.example.com", wantAPIVer: ""},
		{endpoint: "https://proxy.example.com/relay", wantBase: "https://proxy.example.com/relay", wantAPIVer: ""},
		// "rev1" ends in "v1" but is not a version segment.
		{endpoint: "https://proxy.example.com/rev1", wantBase: "https://proxy.example.com/rev1", wantAPIVer: ""},
	}

	for _, tc := range tests {
		t.Run(tc.endpoint, func(t *testing.T) {
			base, apiVersion := splitGenAIEndpoint(tc.endpoint)
			if base != tc.wantBase || apiVersion != tc.wantAPIVer {
				t.Fatalf("splitGenAIEndpoint(%q) = (%q, %q), want (%q, %q)",
					tc.endpoint, base, apiVersion, tc.wantBase, tc.wantAPIVer)
			}
		})
	}
}

func TestInitProviderGenAIUsesCustomEndpoint(t *testing.T) {
	ctx := context.Background()

	var observedPath, authHeader, apiKeyHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observedPath = r.URL.Path
		authHeader = r.Header.Get("X-Auth")
		apiKeyHeader = r.Header.Get("x-goog-api-key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"embeddings":[]}`))
	}))
	defer srv.Close()

	gw, err := New(ctx, Config{
		Providers: []ProviderConfig{
			{
				Name:     "proxy",
				Type:     "google-genai",
				Endpoint: srv.URL + "/v1beta",
				Auth:     &AuthConfig{Type: AuthTypeAPIKey, Header: "X-Auth", Env: "GW_PROXY_KEY"},
			},
		},
		Env: map[string]string{"GW_PROXY_KEY": "proxy-key"},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = gw.Close() }()

	inst := gw.(*gatewayImpl).providers["proxy"]
	if inst == nil {
		t.Fatal("proxy provider was not initialized")
	}
	if inst.authErr != nil {
		t.Fatalf("unexpected auth error: %v", inst.authErr)
	}
	if inst.embeddingProv == nil {
		t.Fatal("expected genai embedding provider to be constructed against the custom endpoint")
	}

	if _, err := inst.embeddingProv.Embed(ctx, &embedding.Request{
		Model:  "text-embedding-004",
		Inputs: []embedding.Input{{Text: "hello"}},
	}); err != nil {
		t.Fatalf("Embed against test endpoint: %v", err)
	}

	// The endpoint's version segment must be used once, not doubled.
	if !strings.HasPrefix(observedPath, "/v1beta/models/") {
		t.Fatalf("request path = %q, want it under /v1beta/models/", observedPath)
	}
	if strings.Contains(observedPath, "/v1beta/v1beta") {
		t.Fatalf("request path = %q, API version was appended twice", observedPath)
	}
	if authHeader != "proxy-key" {
		t.Fatalf("X-Auth = %q, want %q", authHeader, "proxy-key")
	}
	if apiKeyHeader != "" {
		t.Fatalf("x-goog-api-key = %q, want it stripped when a custom auth header is configured", apiKeyHeader)
	}
}
