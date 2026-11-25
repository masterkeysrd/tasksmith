package gateway

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway/auth"
	"github.com/masterkeysrd/tasksmith/internal/daemon/metrics"
)

// Config represents the complete in-memory configuration passed to the Gateway.
type Config struct {
	// Providers lists the configured model providers.
	Providers []ProviderConfig `json:"providers"`

	// Env contains injected environment variables strictly keyed by name.
	// The gateway will only read variables explicitly referenced by a provider's Auth.Env.
	Env map[string]string `json:"env,omitempty"`

	// Metrics receives a record for every LLM call. It is not part of the
	// configuration fingerprint. A nil recorder disables LLM call metrics.
	Metrics metrics.Recorder `json:"-"`
}

// Fingerprint returns a canonical SHA-256 hex fingerprint of the effective configuration.
// It normalizes provider ordering and isolates only the environment variables
// that are actually referenced by provider authentication configurations.
func (c *Config) Fingerprint() string {
	if c == nil {
		return "nil"
	}

	// 1. Collect only the env keys referenced by any provider's credential
	// sources, and a content digest for each file or OAuth token store, so
	// rotating credentials or signing in rebuilds the gateway instead of reusing
	// a stale one.
	relevantEnv := make(map[string]string)
	relevantFiles := make(map[string]string)
	relevantOAuth := make(map[string]string)
	for _, p := range c.Providers {
		if p.Auth == nil {
			continue
		}
		collectSourceFingerprint(p.Name, p.Auth.Token, c.Env, relevantEnv, relevantFiles)
		collectSourceFingerprint(p.Name+":password", p.Auth.Password, c.Env, relevantEnv, relevantFiles)
		if p.Auth.Scheme() == auth.TypeOAuth2 {
			relevantOAuth[p.Name] = auth.CredentialRevision(p.Type, p.Name)
		}
	}

	// 2. Clone and normalize providers
	normalizedProviders := make([]ProviderConfig, len(c.Providers))
	copy(normalizedProviders, c.Providers)

	// Sort providers by Name
	sort.Slice(normalizedProviders, func(a, b int) bool {
		return normalizedProviders[a].Name < normalizedProviders[b].Name
	})

	// 3. Build canonical structure for JSON marshaling
	canonical := struct {
		Providers     []ProviderConfig  `json:"providers"`
		RelevantEnv   map[string]string `json:"relevant_env,omitempty"`
		RelevantFiles map[string]string `json:"relevant_files,omitempty"`
		RelevantOAuth map[string]string `json:"relevant_oauth,omitempty"`
	}{
		Providers:     normalizedProviders,
		RelevantEnv:   relevantEnv,
		RelevantFiles: relevantFiles,
		RelevantOAuth: relevantOAuth,
	}

	data, err := json.Marshal(canonical)
	if err != nil {
		// Fallback to basic string format if marshaling fails
		sum := sha256.Sum256([]byte(fmt.Sprintf("%+v", canonical)))
		return hex.EncodeToString(sum[:])
	}

	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Validate checks the configuration for gateway-level structural integrity
// (non-nil config, non-empty and unique provider names). Per-provider settings
// such as provider type and auth scheme are validated and isolated per provider
// during initialization so one misconfigured manifest cannot disable healthy
// providers in the same workspace.
func (c *Config) Validate() error {
	if c == nil {
		return fmt.Errorf("%w: config cannot be nil", ErrInvalidConfig)
	}

	providerNames := make(map[string]struct{}, len(c.Providers))
	for i, p := range c.Providers {
		name := strings.TrimSpace(p.Name)
		if name == "" {
			return fmt.Errorf("%w: provider at index %d is missing a name", ErrInvalidConfig, i)
		}
		if _, exists := providerNames[name]; exists {
			return fmt.Errorf("%w: duplicate provider name %q", ErrInvalidConfig, name)
		}
		providerNames[name] = struct{}{}
	}

	return nil
}

func collectSourceFingerprint(fileKey string, src *auth.CredentialSource, cfgEnv, relevantEnv, relevantFiles map[string]string) {
	if src == nil {
		return
	}
	if key := strings.TrimSpace(src.Env); key != "" {
		if val := strings.TrimSpace(cfgEnv[key]); val != "" {
			relevantEnv[key] = cfgEnv[key]
		} else {
			relevantEnv[key] = os.Getenv(key)
		}
	}
	if file := strings.TrimSpace(src.File); file != "" {
		relevantFiles[fileKey] = auth.CredentialDigest(file)
	}
}
