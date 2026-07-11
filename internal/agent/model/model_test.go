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
