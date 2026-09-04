package model_test

import (
	"context"
	"os"
	"testing"

	"github.com/masterkeysrd/tasksmith/internal/agent/model"
	"github.com/masterkeysrd/warp"
)

func TestCreateProvider_EnvExpansion(t *testing.T) {
	// Set test environment variable
	if err := os.Setenv("TEST_AUTH_TOKEN", "my-secret-key-123"); err != nil {
		t.Fatalf("failed to set env var: %v", err)
	}
	defer os.Unsetenv("TEST_AUTH_TOKEN")

	provider := &warp.ModelProvider{
		BaseResource: warp.BaseResource{
			APIVersion: warp.APIVersion,
			Kind:       warp.KindModelProvider,
			Metadata: warp.Metadata{
				Name: "test-provider",
			},
		},
		Spec: warp.ModelProviderSpec{
			Type:     "openai",
			Endpoint: "https://api.openai.com/v1",
			Headers: map[string]string{
				"Authorization":   "Bearer $TEST_AUTH_TOKEN",
				"X-Custom-Header": "Val-${TEST_AUTH_TOKEN}",
			},
		},
	}

	// Create the provider - validates HTTP client and header building compiles and runs without issues
	lp, err := model.CreateProvider(context.Background(), provider)
	if err != nil {
		t.Fatalf("CreateProvider failed: %v", err)
	}
	if lp == nil {
		t.Fatal("expected non-nil Loom provider")
	}
}

func TestNew_ProfileFallback(t *testing.T) {
	providerSpec := &warp.ModelProvider{
		BaseResource: warp.BaseResource{
			APIVersion: warp.APIVersion,
			Kind:       warp.KindModelProvider,
			Metadata: warp.Metadata{
				Name: "test-provider",
			},
		},
		Spec: warp.ModelProviderSpec{
			Type: "openai",
			Models: []warp.ProviderModel{
				{
					ID:   "gpt-4",
					Name: "gpt-4",
					Limits: warp.ProviderModelLimits{
						Context: 8192,
						Output:  2048,
					},
				},
			},
		},
	}

	lp, err := model.CreateProvider(context.Background(), providerSpec)
	if err != nil {
		t.Fatalf("CreateProvider failed: %v", err)
	}

	// Create model instance using our helper
	m, err := model.New(context.Background(), model.Config{
		Provider:      lp,
		ModelName:     "gpt-4",
		ModelProvider: providerSpec,
	})
	if err != nil {
		t.Fatalf("model.New failed: %v", err)
	}

	if m == nil {
		t.Fatal("expected non-nil model")
	}
}

func TestCreateProvider_Antigravity(t *testing.T) {
	boolTrue := true
	providerSpec := &warp.ModelProvider{
		BaseResource: warp.BaseResource{
			APIVersion: warp.APIVersion,
			Kind:       warp.KindModelProvider,
			Metadata: warp.Metadata{
				Name: "antigravity",
			},
		},
		Spec: warp.ModelProviderSpec{
			Type:     "antigravity",
			Endpoint: "https://daily-cloudcode-pa.googleapis.com",
			Models: []warp.ProviderModel{
				{
					ID:   "gemini-3.8-flash",
					Name: "gemini-3.8-flash",
					Limits: warp.ProviderModelLimits{
						Context: 1048576,
						Output:  65536,
					},
					Capabilities: &warp.ProviderModelCapabilities{
						Tools: &boolTrue,
						Thinking: []warp.ProviderModelThinkingCapability{
							{
								Type:           "effort",
								Default:        "high",
								IsDefault:      true,
								AllowedEfforts: []string{"low", "medium", "high"},
							},
						},
					},
				},
				{
					ID:   "uncataloged-model",
					Name: "uncataloged-model",
				},
			},
		},
	}

	lp, err := model.CreateProvider(context.Background(), providerSpec)
	if err != nil {
		t.Fatalf("CreateProvider failed for antigravity: %v", err)
	}
	if lp == nil {
		t.Fatal("expected non-nil Loom provider for antigravity")
	}

	prof, found := lp.GetProfile("gemini-3.8-flash")
	if !found {
		t.Fatal("expected to find gemini-3.8-flash profile")
	}
	if prof.Limits.Context != 1048576 {
		t.Errorf("expected context limit 1048576, got %d", prof.Limits.Context)
	}
	if !prof.Capabilities.ToolCall {
		t.Error("expected ToolCall capability to be true")
	}
	if !prof.Capabilities.Reasoning {
		t.Error("expected Reasoning capability to be true")
	}

	// Verify uncataloged model without capabilities was NOT registered as an override,
	// preventing permanent shadowing of upstream dynamic discovery.
	_, foundUncataloged := lp.GetProfile("uncataloged-model")
	if foundUncataloged {
		t.Error("expected uncataloged model without capabilities to NOT be registered in overrides")
	}
}

func TestLogin_UnsupportedProvider(t *testing.T) {
	err := model.Login(context.Background(), "unknown-provider")
	if err == nil {
		t.Fatal("expected error for unsupported provider, got nil")
	}
}

func TestAntigravityTokenPath(t *testing.T) {
	path, err := model.AntigravityTokenPath()
	if err != nil {
		t.Fatalf("unexpected error resolving antigravity token path: %v", err)
	}
	if path == "" {
		t.Fatal("expected non-empty token path")
	}
}

func TestCreateProvider_Antigravity_OAuthTokenFile(t *testing.T) {
	// Create a temporary OAuth JSON token file
	tmpDir := t.TempDir()
	tokenFile := tmpDir + "/antigravity_token.json"
	oauthJSON := `{"access_token":"ya29.test-access-token","token_type":"Bearer","refresh_token":"1//test-refresh-token"}`
	if err := os.WriteFile(tokenFile, []byte(oauthJSON), 0600); err != nil {
		t.Fatalf("failed to write mock oauth token file: %v", err)
	}

	providerSpec := &warp.ModelProvider{
		BaseResource: warp.BaseResource{
			APIVersion: warp.APIVersion,
			Kind:       warp.KindModelProvider,
			Metadata: warp.Metadata{
				Name: "antigravity",
			},
		},
		Spec: warp.ModelProviderSpec{
			Type:     "antigravity",
			Endpoint: "https://daily-cloudcode-pa.googleapis.com",
			Auth: &warp.ProviderAuth{
				Type: "oauth2",
				File: tokenFile,
			},
			Models: []warp.ProviderModel{
				{
					ID:   "gemini-3.8-flash",
					Name: "gemini-3.8-flash",
				},
			},
		},
	}

	// CreateProvider should succeed and initialize OAuth2 token provider without treating JSON file as static token string
	lp, err := model.CreateProvider(context.Background(), providerSpec)
	if err != nil {
		t.Fatalf("CreateProvider failed with oauth token file: %v", err)
	}
	if lp == nil {
		t.Fatal("expected non-nil provider")
	}

	// Non-existent OAuth token file should also succeed during CreateProvider (lazy load before login)
	nonExistentSpec := &warp.ModelProvider{
		BaseResource: warp.BaseResource{
			APIVersion: warp.APIVersion,
			Kind:       warp.KindModelProvider,
			Metadata: warp.Metadata{
				Name: "antigravity",
			},
		},
		Spec: warp.ModelProviderSpec{
			Type:     "antigravity",
			Endpoint: "https://daily-cloudcode-pa.googleapis.com",
			Auth: &warp.ProviderAuth{
				Type: "oauth2",
				File: tmpDir + "/non_existent_token.json",
			},
		},
	}
	lp2, err := model.CreateProvider(context.Background(), nonExistentSpec)
	if err != nil {
		t.Fatalf("CreateProvider failed with non-existent oauth token file: %v", err)
	}
	if lp2 == nil {
		t.Fatal("expected non-nil provider")
	}
}

func TestCreateProvider_Antigravity_BearerFile(t *testing.T) {
	tmpDir := t.TempDir()
	tokenFile := tmpDir + "/static_token.txt"
	if err := os.WriteFile(tokenFile, []byte("ya29.static-bearer-string"), 0600); err != nil {
		t.Fatalf("failed to write static token file: %v", err)
	}

	providerSpec := &warp.ModelProvider{
		BaseResource: warp.BaseResource{
			APIVersion: warp.APIVersion,
			Kind:       warp.KindModelProvider,
			Metadata: warp.Metadata{
				Name: "antigravity",
			},
		},
		Spec: warp.ModelProviderSpec{
			Type:     "antigravity",
			Endpoint: "https://daily-cloudcode-pa.googleapis.com",
			Auth: &warp.ProviderAuth{
				Type: "bearer",
				File: tokenFile,
			},
		},
	}

	lp, err := model.CreateProvider(context.Background(), providerSpec)
	if err != nil {
		t.Fatalf("CreateProvider failed with bearer token file: %v", err)
	}
	if lp == nil {
		t.Fatal("expected non-nil provider")
	}
}



