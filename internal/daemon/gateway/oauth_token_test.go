package gateway

import (
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"golang.org/x/oauth2"
)

func TestProviderTokenLifecycleAndMultiAccount(t *testing.T) {
	xdg.SetTestEnv(t.TempDir(), "tasksmith-test")
	prov := "testoauth"

	// 1. Initial state: not exists
	if HasProviderToken(prov) {
		t.Fatal("expected HasProviderToken to be false initially")
	}
	tok, email, err := LoadProviderToken(prov)
	if err == nil || tok != nil {
		t.Fatalf("expected error on load non-existent token, got tok=%v, email=%q, err=%v", tok, email, err)
	}

	// 2. Save first account
	tok1 := &oauth2.Token{
		AccessToken:  "access_token_1",
		RefreshToken: "refresh_token_1",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(1 * time.Hour).UTC(),
	}
	if err := SaveProviderToken(prov, tok1, "alice@example.com"); err != nil {
		t.Fatalf("SaveProviderToken failed: %v", err)
	}

	if !HasProviderToken(prov) {
		t.Fatal("expected HasProviderToken to be true after save")
	}

	loadedTok, activeEmail, err := LoadProviderToken(prov)
	if err != nil {
		t.Fatalf("LoadProviderToken failed: %v", err)
	}
	if loadedTok.AccessToken != "access_token_1" || activeEmail != "alice@example.com" {
		t.Fatalf("unexpected loaded token: %+v, email: %s", loadedTok, activeEmail)
	}

	// 3. Save second account
	tok2 := &oauth2.Token{
		AccessToken:  "access_token_2",
		RefreshToken: "refresh_token_2",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(2 * time.Hour).UTC(),
	}
	if err := SaveProviderToken(prov, tok2, "bob@example.com"); err != nil {
		t.Fatalf("SaveProviderToken failed for second account: %v", err)
	}

	accounts, active, err := ListProviderAccounts(prov)
	if err != nil {
		t.Fatalf("ListProviderAccounts failed: %v", err)
	}
	if len(accounts) != 2 {
		t.Fatalf("expected 2 accounts, got %d", len(accounts))
	}
	if active != "bob@example.com" {
		t.Fatalf("expected active account to be bob@example.com, got %s", active)
	}

	// 4. Switch back to first account
	if err := SwitchProviderAccount(prov, "alice@example.com"); err != nil {
		t.Fatalf("SwitchProviderAccount failed: %v", err)
	}

	loadedTok, activeEmail, err = LoadProviderToken(prov)
	if err != nil {
		t.Fatalf("LoadProviderToken after switch failed: %v", err)
	}
	if activeEmail != "alice@example.com" || loadedTok.AccessToken != "access_token_1" {
		t.Fatalf("expected alice active with access_token_1, got %s and %s", activeEmail, loadedTok.AccessToken)
	}
}

func TestExtractEmailFromToken(t *testing.T) {
	// 1. Nil token
	if got := ExtractEmailFromToken(nil); got != "" {
		t.Fatalf("expected empty for nil, got %q", got)
	}

	// 2. Token without id_token
	tokWithoutID := &oauth2.Token{AccessToken: "abc"}
	if got := ExtractEmailFromToken(tokWithoutID); got != "" {
		t.Fatalf("expected empty for missing id_token, got %q", got)
	}

	// 3. Token with valid id_token JWT
	payloadJSON, _ := json.Marshal(map[string]any{
		"email": "developer@example.com",
		"sub":   "12345",
	})
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString(payloadJSON)
	fakeJWT := header + "." + payload + ".fakesignature"

	tokWithID := (&oauth2.Token{AccessToken: "xyz"}).WithExtra(map[string]any{
		"id_token": fakeJWT,
	})

	if got := ExtractEmailFromToken(tokWithID); got != "developer@example.com" {
		t.Fatalf("expected developer@example.com, got %q", got)
	}
}

func TestOAuthDriverRegistry(t *testing.T) {
	if !HasOAuthDriver("antigravity") {
		t.Fatal("expected antigravity oauth driver to be registered by default")
	}

	driver, ok := GetOAuthDriver("antigravity")
	if !ok || driver == nil {
		t.Fatalf("expected GetOAuthDriver(antigravity) to return valid driver, got %v", driver)
	}

	cfg := driver.OAuthConfig()
	if cfg == nil || cfg.ClientID == "" {
		t.Fatalf("expected valid oauth config from antigravity driver, got %+v", cfg)
	}
}
