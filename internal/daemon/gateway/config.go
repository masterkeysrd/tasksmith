package gateway

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/masterkeysrd/tasksmith/internal/daemon/metrics"
	"os"
	"sort"
	"strings"
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

	// 1. Collect only the env keys referenced by any provider's Auth.Env
	relevantEnv := make(map[string]string)
	for _, p := range c.Providers {
		if p.Auth != nil && p.Auth.Env != "" {
			if val := strings.TrimSpace(c.Env[p.Auth.Env]); val != "" {
				relevantEnv[p.Auth.Env] = c.Env[p.Auth.Env]
			} else {
				relevantEnv[p.Auth.Env] = os.Getenv(p.Auth.Env)
			}
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
		Providers   []ProviderConfig  `json:"providers"`
		RelevantEnv map[string]string `json:"relevant_env,omitempty"`
	}{
		Providers:   normalizedProviders,
		RelevantEnv: relevantEnv,
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

// Validate checks the configuration for structural integrity (provider names, types, uniqueness).
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

		pType := strings.TrimSpace(p.Type)
		if pType == "" {
			return fmt.Errorf("%w: provider %q is missing a type", ErrInvalidConfig, name)
		}
	}

	return nil
}
