package auth

import (
	"context"
	"encoding/base64"
	"github.com/masterkeysrd/tasksmith/internal/core/netutils"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// captureTransport runs one request through the gateway transport on top of a
// server that records the headers it actually received.
func captureTransport(t *testing.T, auth *Config, token string, custom map[string]string, providerType string) http.Header {
	t.Helper()

	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)

	transport := BuildHTTPTransport(nil, auth, token, custom, providerType)
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

func TestProviderRedirectOrigins(t *testing.T) {
	foreign := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("foreign origin contacted") }))
	defer foreign.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/same":
			http.Redirect(w, r, "/target", http.StatusFound)
		case "/cross":
			http.Redirect(w, r, foreign.URL, http.StatusFound)
		case "/target":
			if r.Header.Get("Authorization") != "Bearer test" {
				t.Error("missing same-origin credential")
			}
		}
	}))
	defer origin.Close()
	client := &http.Client{
		Transport:     BuildHTTPTransport(nil, &Config{Type: TypeBearer}, "test", nil, "openai"),
		CheckRedirect: netutils.CheckSameOriginRedirect,
	}
	for _, path := range []string{"/same", "/cross"} {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, origin.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := client.Do(req)
		if resp != nil {
			_ = resp.Body.Close()
		}
		if (err != nil) != (path == "/cross") {
			t.Fatalf("%s: %v", path, err)
		}
	}
}

func TestResolveCredentialNoneSkipsSources(t *testing.T) {
	t.Setenv("GATEWAY_NONE_KEY", "secret-from-env")

	dir := t.TempDir()
	file := filepath.Join(dir, "token")
	if err := os.WriteFile(file, []byte("secret-from-file\n"), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}

	auth := &Config{Type: TypeNone, Token: &CredentialSource{Env: "GATEWAY_NONE_KEY", File: file}}
	token, err := ResolveCredential(auth, nil)
	if err != nil {
		t.Fatalf("ResolveCredential: %v", err)
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

	token, err := ResolveCredential(&Config{Type: TypeBearer, Token: &CredentialSource{File: "~/.config/tasksmith/token"}}, nil)
	if err != nil {
		t.Fatalf("ResolveCredential: %v", err)
	}
	if token != "tilde-token" {
		t.Fatalf("token = %q, want %q", token, "tilde-token")
	}
}

func TestResolveCredentialMissingFile(t *testing.T) {
	_, err := ResolveCredential(&Config{Type: TypeBearer, Token: &CredentialSource{File: "~/definitely-not-here/token"}}, nil)
	if err == nil {
		t.Fatal("expected error for missing credential file")
	}
	if !strings.Contains(err.Error(), "definitely-not-here") {
		t.Fatalf("error should name the configured path, got: %v", err)
	}
}

func TestResolveCredentialBasicPasswordSource(t *testing.T) {
	dir := t.TempDir()
	pwFile := filepath.Join(dir, "pw")
	if err := os.WriteFile(pwFile, []byte("secret-from-file\n"), 0o600); err != nil {
		t.Fatalf("write pw: %v", err)
	}

	gotFile, err := ResolveCredential(&Config{
		Type:     TypeBasic,
		Username: "alice",
		Password: &CredentialSource{File: pwFile},
	}, nil)
	if err != nil {
		t.Fatalf("ResolveCredential(basic file): %v", err)
	}
	if gotFile != "alice:secret-from-file" {
		t.Fatalf("got %q, want %q", gotFile, "alice:secret-from-file")
	}

	gotEnv, err := ResolveCredential(&Config{
		Type:     TypeBasic,
		Username: "alice",
		Password: &CredentialSource{Env: "BASIC_PASS"},
	}, map[string]string{"BASIC_PASS": "env-pass"})
	if err != nil {
		t.Fatalf("ResolveCredential(basic env): %v", err)
	}
	if gotEnv != "alice:env-pass" {
		t.Fatalf("got %q, want %q", gotEnv, "alice:env-pass")
	}
}

func TestTransportAuthSchemes(t *testing.T) {
	tests := []struct {
		name       string
		auth       *Config
		token      string
		wantHeader string
		wantValue  string
	}{
		{
			name:       "omitted type defaults to bearer authorization",
			auth:       &Config{},
			token:      "tok",
			wantHeader: "Authorization",
			wantValue:  "Bearer tok",
		},
		{
			name:       "bearer",
			auth:       &Config{Type: TypeBearer},
			token:      "tok",
			wantHeader: "Authorization",
			wantValue:  "Bearer tok",
		},
		{
			name:       "bearer with custom header",
			auth:       &Config{Type: TypeBearer, Header: "Proxy-Authorization"},
			token:      "tok",
			wantHeader: "Proxy-Authorization",
			wantValue:  "Bearer tok",
		},
		{
			name:       "api-key sends raw token",
			auth:       &Config{Type: TypeAPIKey},
			token:      "tok",
			wantHeader: "Authorization",
			wantValue:  "tok",
		},
		{
			name:       "api-key with custom header",
			auth:       &Config{Type: TypeAPIKey, Header: "X-Api-Key"},
			token:      "tok",
			wantHeader: "X-Api-Key",
			wantValue:  "tok",
		},
		{
			name:       "basic encodes credentials",
			auth:       &Config{Type: TypeBasic},
			token:      "user:pass",
			wantHeader: "Authorization",
			wantValue:  "Basic " + base64.StdEncoding.EncodeToString([]byte("user:pass")),
		},
		{
			name:       "uppercase scheme is normalized",
			auth:       &Config{Type: "  BEARER "},
			token:      "tok",
			wantHeader: "Authorization",
			wantValue:  "Bearer tok",
		},
		{
			name:       "oauth2 with explicit token defaults to bearer authorization",
			auth:       &Config{Type: TypeOAuth2},
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
	got := captureTransport(t, &Config{Type: TypeNone}, "ignored", nil, "openai")
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

	transport := BuildHTTPTransport(nil, &Config{Type: TypeBasic}, "user:pass", nil, "openai")
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
		auth         *Config
		emptyToken   bool
		providerType string
		sdkHeaders   map[string]string
		wantKept     []string
		wantDropped  []string
	}{
		{
			name:         "api-key keeps configured vendor header and drops conflicting SDK header",
			auth:         &Config{Type: TypeAPIKey, Header: "X-Api-Key"},
			providerType: "anthropic",
			sdkHeaders:   map[string]string{"X-Api-Key": "tok", "Authorization": "Bearer stale"},
			wantKept:     []string{"X-Api-Key"},
			wantDropped:  []string{"Authorization"},
		},
		{
			name:         "custom header removes vendor credential header",
			auth:         &Config{Type: TypeBearer, Header: "X-Auth"},
			providerType: "anthropic",
			sdkHeaders:   map[string]string{"X-Api-Key": "tok", "Authorization": "Bearer stale"},
			wantKept:     nil,
			wantDropped:  []string{"X-Api-Key"},
		},
		{
			name:         "none removes the google key header",
			auth:         &Config{Type: TypeNone},
			providerType: "google-genai",
			sdkHeaders:   map[string]string{"x-goog-api-key": "auth-none"},
			wantKept:     nil,
			wantDropped:  []string{"x-goog-api-key"},
		},
		{
			name:         "oauth2 keeps sdk bearer authorization header",
			auth:         &Config{Type: TypeOAuth2},
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
			transport := BuildHTTPTransport(nil, tc.auth, token, nil, tc.providerType)
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
