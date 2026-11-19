package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/masterkeysrd/tasksmith/api"
)

// TestCreateProviderCustomTypeDoesNotInheritPreset guards the secret-leak and
// dangling-default path: a custom provider named differently from its type may
// borrow the preset endpoint but must never copy the preset's auth (which
// would point an existing secret at a custom endpoint), config, default
// models, or model list.
func TestCreateProviderCustomTypeDoesNotInheritPreset(t *testing.T) {
	_, cli, wsDir := setupDaemonTest(t, nil)
	ctx := context.Background()

	created, err := cli.CreateProvider(ctx, api.CreateProviderRequest{
		Scope:    "workspace",
		Name:     "vllm",
		Type:     "openai",
		Endpoint: "http://localhost:8000/v1",
	})
	if err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}
	if created.Name != "vllm" {
		t.Fatalf("created = %+v, want vllm", created)
	}

	manifest, err := os.ReadFile(filepath.Join(wsDir, ".agents", "providers", "vllm.provider.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(manifest)
	if !strings.Contains(got, "endpoint: http://localhost:8000/v1") {
		t.Errorf("custom manifest lost the requested endpoint:\n%s", got)
	}
	for _, forbidden := range []string{"OPENAI_API_KEY", "defaultModels", "gpt-6", "store: false"} {
		if strings.Contains(got, forbidden) {
			t.Errorf("custom manifest inherited preset %q:\n%s", forbidden, got)
		}
	}
	if strings.Contains(got, "auth:") {
		t.Errorf("keyless custom manifest declares auth:\n%s", got)
	}
}

// TestCreateProviderHonorsNoneAuthType proves an explicit auth type "none"
// without a key or env var writes an unauthenticated manifest instead of
// silently falling through to the preset's auth.
func TestCreateProviderHonorsNoneAuthType(t *testing.T) {
	_, cli, wsDir := setupDaemonTest(t, nil)
	ctx := context.Background()

	if _, err := cli.CreateProvider(ctx, api.CreateProviderRequest{
		Scope: "workspace", Name: "local", Type: "ollama", AuthType: "none",
	}); err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}
	manifest, err := os.ReadFile(filepath.Join(wsDir, ".agents", "providers", "local.provider.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(manifest)
	if !strings.Contains(got, "type: none") {
		t.Errorf("manifest lacks auth type none:\n%s", got)
	}
	if strings.Contains(got, "OLLAMA_API_KEY") {
		t.Errorf("none-auth manifest still references a preset secret:\n%s", got)
	}
}

// TestCreateProviderPresetKeepsAuthScheme proves a preset-backed create with a
// pasted key preserves the preset's auth scheme and header (api-key +
// X-Api-Key for anthropic) rather than rewriting it to bearer, and routes the
// secret to the preset's own env var.
func TestCreateProviderPresetKeepsAuthScheme(t *testing.T) {
	_, cli, wsDir := setupDaemonTest(t, nil)
	ctx := context.Background()

	if _, err := cli.CreateProvider(ctx, api.CreateProviderRequest{
		Scope: "workspace", Name: "anthropic", Type: "anthropic", APIKey: "sk-ant-test",
	}); err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}
	manifest, err := os.ReadFile(filepath.Join(wsDir, ".agents", "providers", "anthropic.provider.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(manifest)
	if !strings.Contains(got, "type: api-key") || !strings.Contains(got, "header: X-Api-Key") {
		t.Errorf("preset auth scheme was clobbered:\n%s", got)
	}
	if strings.Contains(got, "type: bearer") {
		t.Errorf("preset auth rewritten to bearer:\n%s", got)
	}
	if !strings.Contains(got, "${env:ANTHROPIC_API_KEY}") {
		t.Errorf("preset env var not kept:\n%s", got)
	}
	envFile, err := os.ReadFile(filepath.Join(wsDir, ".env"))
	if err != nil {
		t.Fatalf("workspace .env not written: %v", err)
	}
	if !strings.Contains(string(envFile), "ANTHROPIC_API_KEY=sk-ant-test") {
		t.Errorf("secret not stored under the preset env var:\n%s", envFile)
	}
}

// TestCreateProviderCustomModelsDropPresetDefaults proves that replacing a
// preset's model list clears preset default-model references that no longer
// resolve instead of shipping a dangling completion default.
func TestCreateProviderCustomModelsDropPresetDefaults(t *testing.T) {
	_, cli, wsDir := setupDaemonTest(t, nil)
	ctx := context.Background()

	if _, err := cli.CreateProvider(ctx, api.CreateProviderRequest{
		Scope:  "workspace",
		Name:   "openai",
		Type:   "openai",
		Models: []api.ProviderModelRequest{{ID: "my-model", Name: "my-model"}},
	}); err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}
	manifest, err := os.ReadFile(filepath.Join(wsDir, ".agents", "providers", "openai.provider.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(manifest)
	if strings.Contains(got, "defaultModels") {
		t.Errorf("dangling preset defaults survived the model replacement:\n%s", got)
	}
	if !strings.Contains(got, "my-model") || strings.Contains(got, "gpt-6") {
		t.Errorf("model list not replaced as requested:\n%s", got)
	}
}
