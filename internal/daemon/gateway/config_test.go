package gateway

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway/auth"
	"github.com/masterkeysrd/tasksmith/internal/daemon/models"
	"golang.org/x/oauth2"
)

func TestFingerprintTracksCredentialFileRotation(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "token")
	if err := os.WriteFile(file, []byte("token-v1"), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}

	providers := []ProviderConfig{
		{Name: "internal", Type: "openai", Endpoint: "https://llm.internal/v1", Auth: &auth.Config{Type: auth.TypeBearer, Token: &auth.CredentialSource{File: file}}},
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
		{Name: "internal", Type: "openai", Auth: &auth.Config{Type: auth.TypeBearer, Token: &auth.CredentialSource{File: file}}},
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

	credentials := &auth.Config{Type: auth.TypeBearer, Token: &auth.CredentialSource{File: file}}
	cfgA := Config{Providers: []ProviderConfig{{Name: "a", Type: "openai", Auth: credentials}}}
	cfgB := Config{Providers: []ProviderConfig{{Name: "a", Type: "openai", Auth: credentials}}}

	if cfgA.Fingerprint() != cfgB.Fingerprint() {
		t.Fatal("expected identical fingerprints for identical credential files")
	}
}

func TestUnknownAuthSchemeIsolatedPerProvider(t *testing.T) {
	valid := Config{Providers: []ProviderConfig{
		{Name: "a", Type: "openai", Auth: &auth.Config{Type: "  BEARER "}},
		{Name: "b", Type: "openai", Auth: &auth.Config{Type: auth.TypeNone}},
		{Name: "c", Type: "ollama"},
		{Name: "d", Type: "antigravity", Auth: &auth.Config{Type: auth.TypeOAuth2}},
	}}
	if err := valid.Validate(); err != nil {
		t.Fatalf("expected normalized and missing auth schemes to validate, got: %v", err)
	}

	invalid := Config{Providers: []ProviderConfig{
		{Name: "a", Type: "openai", Auth: &auth.Config{Type: "unknown_scheme"}},
	}}
	if err := invalid.Validate(); err != nil {
		t.Fatalf("structural Validate() should not fail on per-provider auth scheme: %v", err)
	}
	gw, err := New(context.Background(), invalid)
	if err != nil {
		t.Fatalf("New() should isolate unknown auth scheme instead of failing: %v", err)
	}
	t.Cleanup(func() { _ = gw.Close() })

	infos := gw.ListProviders()
	if len(infos) != 1 || !infos[0].InvalidConfig || !strings.Contains(infos[0].Warning, "unknown_scheme") {
		t.Fatalf("expected InvalidConfig warning naming unknown_scheme, got: %+v", infos)
	}
	_, err = gw.LLM(context.Background(), models.Model{ID: "a/gpt-4o", ProviderType: "openai"}, llm.ModelConfig{})
	if !errors.Is(err, ErrInvalidConfig) || !strings.Contains(err.Error(), "unknown_scheme") {
		t.Fatalf("expected ErrInvalidConfig naming unknown_scheme, got: %v", err)
	}
}

func TestOAuthConfigFingerprintTracksAccountIdentity(t *testing.T) {
	xdg.SetTestEnv(t.TempDir(), "tasksmith-test")

	gwCfg := Config{
		Providers: []ProviderConfig{{
			Name: "antigravity",
			Type: "antigravity",
			Auth: &auth.Config{Type: auth.TypeOAuth2, Token: &auth.CredentialSource{Env: "ANTIGRAVITY_TEST_TOKEN"}},
		}},
	}

	fpMissing := gwCfg.Fingerprint()

	tokAlice := &oauth2.Token{
		AccessToken:  "access-v1",
		RefreshToken: "refresh-alice",
		Expiry:       time.Now().Add(time.Hour).UTC(),
	}
	if err := auth.SaveProviderToken("antigravity", tokAlice, "alice@example.com"); err != nil {
		t.Fatalf("auth.SaveProviderToken(alice): %v", err)
	}
	fpAliceV1 := gwCfg.Fingerprint()
	if fpAliceV1 == fpMissing {
		t.Fatal("expected fingerprint to change after initial OAuth sign-in")
	}

	// Background access-token refresh with the same account + refresh token must
	// keep the fingerprint stable so pooled gateways are not needlessly rebuilt.
	tokAliceRefreshed := &oauth2.Token{
		AccessToken:  "access-v2",
		RefreshToken: "refresh-alice",
		Expiry:       time.Now().Add(2 * time.Hour).UTC(),
	}
	if err := auth.SaveProviderToken("antigravity", tokAliceRefreshed, "alice@example.com"); err != nil {
		t.Fatalf("auth.SaveProviderToken(alice refreshed): %v", err)
	}
	if fpAliceV2 := gwCfg.Fingerprint(); fpAliceV2 != fpAliceV1 {
		t.Fatalf("expected fingerprint to stay stable across access-token refresh: %s != %s", fpAliceV2, fpAliceV1)
	}

	// Signing in as or switching to another account must change the fingerprint.
	tokBob := &oauth2.Token{
		AccessToken:  "access-bob",
		RefreshToken: "refresh-bob",
		Expiry:       time.Now().Add(time.Hour).UTC(),
	}
	if err := auth.SaveProviderToken("antigravity", tokBob, "bob@example.com"); err != nil {
		t.Fatalf("auth.SaveProviderToken(bob): %v", err)
	}
	fpBob := gwCfg.Fingerprint()
	if fpBob == fpAliceV1 {
		t.Fatal("expected fingerprint to change when active OAuth account changes")
	}

	if err := auth.SwitchProviderAccount("antigravity", "alice@example.com"); err != nil {
		t.Fatalf("auth.SwitchProviderAccount(alice): %v", err)
	}
	if fpSwitched := gwCfg.Fingerprint(); fpSwitched != fpAliceV1 {
		t.Fatalf("expected fingerprint after switching back to alice to match alice's fingerprint: %s != %s", fpSwitched, fpAliceV1)
	}
}
