package gateway

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"golang.org/x/oauth2"
)

var tokenStoreMu sync.RWMutex

// OAuthToken encapsulates persisted OAuth2 token details, the active account email,
// and any previously authorized accounts for a provider.
type OAuthToken struct {
	AccessToken  string                  `json:"access_token"`
	TokenType    string                  `json:"token_type,omitempty"`
	RefreshToken string                  `json:"refresh_token,omitempty"`
	Expiry       time.Time               `json:"expiry"`
	Email        string                  `json:"email,omitempty"`
	Accounts     map[string]OAuthAccount `json:"accounts,omitempty"`
}

// OAuthAccount stores credentials and metadata for an authorized account.
type OAuthAccount struct {
	Email        string    `json:"email"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	Expiry       time.Time `json:"expiry"`
	LastUsedAt   time.Time `json:"last_used_at"`
}

// Type aliases for backwards compatibility.
type AntigravityToken = OAuthToken
type AntigravityAccount = OAuthAccount

// ProviderTokenPath returns the absolute path to ~/.local/share/tasksmith/credentials/<provider>.json.
func ProviderTokenPath(provider string) (string, error) {
	dir, err := xdg.SubDataDir("credentials")
	if err != nil {
		return "", fmt.Errorf("resolve credentials dir: %w", err)
	}
	clean := strings.ToLower(strings.TrimSpace(provider))
	if clean == "" {
		return "", errors.New("gateway: provider name cannot be empty")
	}
	return filepath.Join(dir, clean+".json"), nil
}

// SaveProviderToken persists an OAuth2 token and associates it with the active account.
// It also records the account in the multi-account registry.
func SaveProviderToken(provider string, tok *oauth2.Token, account string) error {
	if tok == nil {
		return fmt.Errorf("gateway: token cannot be nil for provider %q", provider)
	}

	tokenPath, err := ProviderTokenPath(provider)
	if err != nil {
		return err
	}

	tokenStoreMu.Lock()
	defer tokenStoreMu.Unlock()

	var existing OAuthToken
	if data, rErr := os.ReadFile(tokenPath); rErr == nil {
		_ = json.Unmarshal(data, &existing)
	}

	if existing.Accounts == nil {
		existing.Accounts = make(map[string]OAuthAccount)
	}

	if account == "" {
		account = ExtractEmailFromToken(tok)
	}
	if account == "" && existing.Email != "" {
		account = existing.Email
	}

	now := time.Now().UTC()
	refreshToken := tok.RefreshToken
	if refreshToken == "" && account != "" {
		if prevAcc, ok := existing.Accounts[account]; ok && prevAcc.RefreshToken != "" {
			refreshToken = prevAcc.RefreshToken
		} else if existing.RefreshToken != "" {
			refreshToken = existing.RefreshToken
		}
	}

	existing.AccessToken = tok.AccessToken
	existing.TokenType = tok.TokenType
	existing.RefreshToken = refreshToken
	existing.Expiry = tok.Expiry
	existing.Email = account

	if account != "" {
		existing.Accounts[account] = OAuthAccount{
			Email:        account,
			AccessToken:  tok.AccessToken,
			RefreshToken: refreshToken,
			Expiry:       tok.Expiry,
			LastUsedAt:   now,
		}
	}

	dir := filepath.Dir(tokenPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("gateway: create token directory: %w", err)
	}

	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return fmt.Errorf("gateway: marshal token: %w", err)
	}

	if err := os.WriteFile(tokenPath, data, 0o600); err != nil {
		return fmt.Errorf("gateway: write token: %w", err)
	}

	return nil
}

// LoadProviderToken reads the persisted OAuth2 token and active account email.
func LoadProviderToken(provider string) (*oauth2.Token, string, error) {
	tokenPath, err := ProviderTokenPath(provider)
	if err != nil {
		return nil, "", err
	}

	tokenStoreMu.RLock()
	defer tokenStoreMu.RUnlock()

	data, err := os.ReadFile(tokenPath)
	if err != nil {
		return nil, "", err
	}

	var stored OAuthToken
	if err := json.Unmarshal(data, &stored); err != nil {
		return nil, "", fmt.Errorf("gateway: parse token: %w", err)
	}

	tok := &oauth2.Token{
		AccessToken:  stored.AccessToken,
		TokenType:    stored.TokenType,
		RefreshToken: stored.RefreshToken,
		Expiry:       stored.Expiry,
	}

	return tok, stored.Email, nil
}

// ListProviderAccounts returns all stored accounts and the active email.
func ListProviderAccounts(provider string) ([]OAuthAccount, string, error) {
	tokenPath, err := ProviderTokenPath(provider)
	if err != nil {
		return nil, "", err
	}

	tokenStoreMu.RLock()
	defer tokenStoreMu.RUnlock()

	data, err := os.ReadFile(tokenPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", nil
		}
		return nil, "", err
	}

	var stored OAuthToken
	if err := json.Unmarshal(data, &stored); err != nil {
		return nil, "", err
	}

	var accounts []OAuthAccount
	for _, acc := range stored.Accounts {
		accounts = append(accounts, acc)
	}

	return accounts, stored.Email, nil
}

// SwitchProviderAccount switches the active token to a previously authorized account.
func SwitchProviderAccount(provider string, account string) error {
	tokenPath, err := ProviderTokenPath(provider)
	if err != nil {
		return err
	}

	tokenStoreMu.Lock()
	defer tokenStoreMu.Unlock()

	data, err := os.ReadFile(tokenPath)
	if err != nil {
		return err
	}

	var stored OAuthToken
	if err := json.Unmarshal(data, &stored); err != nil {
		return err
	}

	acc, ok := stored.Accounts[account]
	if !ok {
		return fmt.Errorf("gateway: account %q not found for provider %q", account, provider)
	}

	acc.LastUsedAt = time.Now().UTC()
	stored.Accounts[account] = acc
	stored.Email = acc.Email
	stored.AccessToken = acc.AccessToken
	stored.RefreshToken = acc.RefreshToken
	stored.Expiry = acc.Expiry

	out, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(tokenPath, out, 0o600)
}

// HasProviderToken reports whether an on-disk OAuth token exists for provider.
func HasProviderToken(provider string) bool {
	tok, _, err := LoadProviderToken(provider)
	return err == nil && tok != nil && (tok.AccessToken != "" || tok.RefreshToken != "")
}

// oauthCredentialDigest returns a stable hex digest of the stored OAuth identity
// for provider so Config.Fingerprint changes on sign-in, sign-out, or account
// switch without rebuilding pooled gateways on routine access-token refreshes.
func oauthCredentialDigest(provider string) string {
	tokenPath, err := ProviderTokenPath(provider)
	if err != nil {
		return "invalid-provider"
	}

	tokenStoreMu.RLock()
	data, err := os.ReadFile(tokenPath)
	tokenStoreMu.RUnlock()
	if err != nil {
		if os.IsNotExist(err) {
			return "missing"
		}
		return "unreadable"
	}

	var stored OAuthToken
	if err := json.Unmarshal(data, &stored); err != nil {
		return "corrupt"
	}
	if stored.AccessToken == "" && stored.RefreshToken == "" {
		return "empty"
	}

	// Key on (email, refresh_token) when a refresh token is present so background
	// access-token rotation keeps the same gateway instance; fall back to
	// access_token only for static/non-refreshable tokens.
	identity := stored.RefreshToken
	if identity == "" {
		identity = stored.AccessToken
	}
	payload := fmt.Sprintf("email=%s\ncredential=%s\nhas_access=%t",
		stored.Email, identity, stored.AccessToken != "")
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

// Antigravity backwards compatibility helpers:

// AntigravityTokenPath returns the absolute path to antigravity.json.
func AntigravityTokenPath() (string, error) {
	return ProviderTokenPath("antigravity")
}

// SaveAntigravityToken persists an Antigravity token.
func SaveAntigravityToken(tok *oauth2.Token, email string) error {
	return SaveProviderToken("antigravity", tok, email)
}

// LoadAntigravityToken reads the Antigravity token.
func LoadAntigravityToken() (*oauth2.Token, string, error) {
	return LoadProviderToken("antigravity")
}

// ListAntigravityAccounts lists Antigravity accounts.
func ListAntigravityAccounts() ([]AntigravityAccount, string, error) {
	return ListProviderAccounts("antigravity")
}

// SwitchAntigravityAccount switches the active Antigravity account.
func SwitchAntigravityAccount(email string) error {
	return SwitchProviderAccount("antigravity", email)
}

// ExtractEmailFromToken extracts the email address from a Google OAuth2 token if an id_token was included.
func ExtractEmailFromToken(tok *oauth2.Token) string {
	if tok == nil {
		return ""
	}

	rawIDToken := tok.Extra("id_token")
	if rawIDToken == nil {
		return ""
	}

	idTokenStr, ok := rawIDToken.(string)
	if !ok || idTokenStr == "" {
		return ""
	}

	parts := strings.Split(idTokenStr, ".")
	if len(parts) < 2 {
		return ""
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		payload, err = base64.URLEncoding.DecodeString(parts[1])
		if err != nil {
			return ""
		}
	}

	var claims struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(payload, &claims); err == nil && claims.Email != "" {
		return claims.Email
	}

	return ""
}
