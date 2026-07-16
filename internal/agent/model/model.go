package model

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/llm/anthropic"
	"github.com/masterkeysrd/loom/llm/genai"
	"github.com/masterkeysrd/loom/llm/ollama"
	"github.com/masterkeysrd/loom/llm/openai"
	"github.com/masterkeysrd/warp"

	anthropicclient "github.com/anthropics/anthropic-sdk-go"
	anthropicoption "github.com/anthropics/anthropic-sdk-go/option"
	ollamaapi "github.com/ollama/ollama/api"
	openaiclient "github.com/openai/openai-go/v3"
	openaioption "github.com/openai/openai-go/v3/option"
	googlegenai "google.golang.org/genai"
)

// SessionSettings represents the structured configuration overrides for a session.
type SessionSettings struct {
	AgentName    string                  `json:"agent_name,omitempty"`
	ProviderName string                  `json:"provider_name,omitempty"`
	ModelName    string                  `json:"model_name,omitempty"`
	Temperature  *float64                `json:"temperature,omitempty"`
	Thinking     *SessionThinkingSetting `json:"thinking,omitempty"`
}

// SessionThinkingSetting represents the thinking configuration overrides inside SessionSettings.
type SessionThinkingSetting struct {
	Enabled  *bool   `json:"enabled,omitempty"`
	Budget   *int    `json:"budget,omitempty"`
	Effort   *string `json:"effort,omitempty"`
	Adaptive *bool   `json:"adaptive,omitempty"`
}

// CreateProvider instantiates a Loom llm.Provider based on the Warp model provider configuration.
func CreateProvider(ctx context.Context, p *warp.ModelProvider) (llm.Provider, error) {
	token, err := resolveAuthToken(p)
	if err != nil {
		return nil, err
	}

	var loomProvider llm.Provider

	switch p.Spec.Type {
	case "ollama":
		var u *url.URL
		if p.Spec.Endpoint != "" {
			u, err = url.Parse(p.Spec.Endpoint)
			if err != nil {
				return nil, fmt.Errorf("invalid ollama endpoint: %w", err)
			}
		} else {
			u, _ = url.Parse("http://localhost:11434")
		}

		httpClient := newHTTPClient(p.Spec.Headers)
		client := ollamaapi.NewClient(u, httpClient)
		loomProvider = (&loomollama.Provider{}).NewProvider(client, u)

	case "openai":
		var opts []openaioption.RequestOption
		if token != "" {
			opts = append(opts, openaioption.WithAPIKey(token))
		} else if p.Spec.Auth != nil && p.Spec.Auth.Env != "" {
			return nil, fmt.Errorf("authentication environment variable %q is empty or not set", p.Spec.Auth.Env)
		}
		if p.Spec.Endpoint != "" {
			opts = append(opts, openaioption.WithBaseURL(p.Spec.Endpoint))
		}
		if len(p.Spec.Headers) > 0 {
			opts = append(opts, openaioption.WithHTTPClient(newHTTPClient(p.Spec.Headers)))
		}
		client := openaiclient.NewClient(opts...)
		loomProvider = loomopenai.NewProvider(&client)

	case "anthropic":
		var opts []anthropicoption.RequestOption
		if token != "" {
			opts = append(opts, anthropicoption.WithAPIKey(token))
		} else if p.Spec.Auth != nil && p.Spec.Auth.Env != "" {
			return nil, fmt.Errorf("authentication environment variable %q is empty or not set", p.Spec.Auth.Env)
		}
		if p.Spec.Endpoint != "" {
			opts = append(opts, anthropicoption.WithBaseURL(p.Spec.Endpoint))
		}
		if len(p.Spec.Headers) > 0 {
			opts = append(opts, anthropicoption.WithHTTPClient(newHTTPClient(p.Spec.Headers)))
		}
		client := anthropicclient.NewClient(opts...)
		loomProvider = loomanthropic.NewProvider(&client)

	case "google-genai":
		config := &googlegenai.ClientConfig{
			APIKey:     token,
			HTTPClient: newHTTPClient(p.Spec.Headers),
		}
		if p.Spec.Endpoint != "" {
			config.HTTPOptions.BaseURL = p.Spec.Endpoint
		}
		if config.APIKey == "" {
			if key := os.Getenv("GEMINI_API_KEY"); key != "" {
				config.APIKey = key
			} else if key := os.Getenv("GOOGLE_API_KEY"); key != "" {
				config.APIKey = key
			} else if p.Spec.Auth != nil && p.Spec.Auth.Env != "" {
				return nil, fmt.Errorf("authentication environment variable %q is empty or not set", p.Spec.Auth.Env)
			}
		}
		loomProvider, err = loomgenai.NewProvider(ctx, config)

	default:
		return nil, fmt.Errorf("unknown provider type: %s", p.Spec.Type)
	}

	if err != nil {
		return nil, err
	}

	// Override or enrich Loom profiles using Warp configurations
	for _, m := range p.Spec.Models {
		// Get existing profile or initialize a new one if not found in catalog
		prof, found := loomProvider.GetProfile(m.ID)
		if !found {
			prof = llm.ModelProfile{
				ID:   m.ID,
				Name: m.Name,
			}
			prof.Capabilities.Temperature = true // Default custom/dynamic models to support temperature
		}

		// Ollama provider models always support temperature by default
		if p.Spec.Type == "ollama" {
			prof.Capabilities.Temperature = true
		}

		// Apply Warp overrides
		if m.Label != "" {
			prof.Name = m.Label
		} else if m.Name != "" {
			prof.Name = m.Name
		}

		if m.Limits.Context > 0 {
			prof.Limits.Context = m.Limits.Context
		}
		if m.Limits.Output > 0 {
			prof.Limits.Output = m.Limits.Output
		}

		if m.Capabilities != nil {
			if m.Capabilities.Attachment != nil {
				prof.Capabilities.Attachment = *m.Capabilities.Attachment
			}
			if m.Capabilities.Tools != nil {
				prof.Capabilities.ToolCall = *m.Capabilities.Tools
			}
			if m.Capabilities.Temperature != nil {
				prof.Capabilities.Temperature = *m.Capabilities.Temperature
			}

			// Map thinking capabilities (reasoning options)
			if len(m.Capabilities.Thinking) > 0 {
				prof.Capabilities.Reasoning = true
				prof.Capabilities.ReasoningOptions = nil
				for _, t := range m.Capabilities.Thinking {
					var values []string
					if t.Type == "effort" {
						values = t.AllowedEfforts
					}
					prof.Capabilities.ReasoningOptions = append(prof.Capabilities.ReasoningOptions, llm.ReasoningOption{
						Type:   t.Type,
						Values: values,
					})
				}
			} else {
				prof.Capabilities.Reasoning = false
				prof.Capabilities.ReasoningOptions = nil
			}

			// Map modalities
			if m.Capabilities.Modalities != nil {
				prof.Modalities.Inputs = nil
				for _, in := range m.Capabilities.Modalities.Input {
					prof.Modalities.Inputs = append(prof.Modalities.Inputs, llm.Modality(in))
				}
				prof.Modalities.Outputs = nil
				for _, out := range m.Capabilities.Modalities.Output {
					prof.Modalities.Outputs = append(prof.Modalities.Outputs, llm.Modality(out))
				}
			}
		}

		if m.Cost != nil {
			prof.Pricing.Input = m.Cost.Input
			prof.Pricing.Output = m.Cost.Output
			prof.Pricing.CacheRead = m.Cost.CacheRead
			prof.Pricing.CacheWrite = m.Cost.CacheWrite

			// Map tiered pricing limits
			if len(m.Cost.Tiers) > 0 {
				prof.Pricing.TieredLimits = nil
				for _, tier := range m.Cost.Tiers {
					prof.Pricing.TieredLimits = append(prof.Pricing.TieredLimits, llm.TierPricing{
						Input:      tier.Input,
						Output:     tier.Output,
						CacheRead:  tier.CacheRead,
						CacheWrite: tier.CacheWrite,
						TierLimit:  tier.Tier.Size,
					})
				}
			} else {
				prof.Pricing.TieredLimits = nil
			}
		}

		// Register the enriched profile into Loom's catalog
		loomProvider.OverrideProfile(m.ID, prof)
	}

	return loomProvider, nil
}

// DefaultProvider returns the default Loom provider (ollama).
func DefaultProvider() (llm.Provider, error) {
	return loomollama.NewDefaultProvider()
}

// Config wraps parameters needed to instantiate a configured Loom LLM model.
type Config struct {
	Provider      llm.Provider
	ModelName     string
	ModelProvider *warp.ModelProvider
	Agent         *warp.ResolvedAgent
	Settings      SessionSettings
}

// New instantiates a Loom model with thinking configurations compiled
// from model provider defaults and agent overrides.
func New(ctx context.Context, cfg Config) (*llm.Model, error) {
	var thinkingConfig *llm.ThinkingConfig

	// 1. Look for default thinking capability from the provider config
	var defaultThinking *warp.ProviderModelThinkingCapability
	if cfg.ModelProvider != nil {
		for _, m := range cfg.ModelProvider.Spec.Models {
			if m.ID == cfg.ModelName && m.Capabilities != nil {
				for _, t := range m.Capabilities.Thinking {
					if t.IsDefault {
						defaultThinking = &t
						break
					}
				}
				break
			}
		}
	}

	if defaultThinking != nil {
		thinkingConfig = &llm.ThinkingConfig{}
		switch defaultThinking.Type {
		case "toggle":
			if val, ok := defaultThinking.Default.(bool); ok {
				thinkingConfig.Enabled = val
			} else {
				thinkingConfig.Enabled = true
			}
		case "adaptive":
			if val, ok := defaultThinking.Default.(bool); ok {
				thinkingConfig.Adaptive = val
			} else {
				thinkingConfig.Adaptive = true
			}
		case "effort":
			if eff, ok := defaultThinking.Default.(string); ok {
				thinkingConfig.Effort = eff
			}
		case "budget":
			if budgetVal, ok := defaultThinking.Default.(float64); ok {
				thinkingConfig.Budget = int(budgetVal)
			} else if budgetVal, ok := defaultThinking.Default.(int); ok {
				thinkingConfig.Budget = budgetVal
			}
		}
	}

	// 2. Override with AgentSpec thinking configuration if present
	if cfg.Agent != nil && cfg.Agent.Agent != nil && len(cfg.Agent.Agent.Spec.Thinking) > 0 {
		if thinkingConfig == nil {
			thinkingConfig = &llm.ThinkingConfig{}
		}
		var enabledVal any
		if val, exists := cfg.Agent.Agent.Spec.Thinking["enabled"]; exists {
			enabledVal = val
		} else if val, exists := cfg.Agent.Agent.Spec.Thinking["toggle"]; exists {
			enabledVal = val
		}
		if enabledVal != nil {
			if bVal, ok := enabledVal.(bool); ok {
				thinkingConfig.Enabled = bVal
			}
		}
		if val, exists := cfg.Agent.Agent.Spec.Thinking["adaptive"]; exists {
			if bVal, ok := val.(bool); ok {
				thinkingConfig.Adaptive = bVal
			}
		}
		if val, exists := cfg.Agent.Agent.Spec.Thinking["effort"]; exists {
			if sVal, ok := val.(string); ok {
				thinkingConfig.Effort = sVal
			}
		}
		if val, exists := cfg.Agent.Agent.Spec.Thinking["budget"]; exists {
			if budgetVal, ok := val.(float64); ok {
				thinkingConfig.Budget = int(budgetVal)
			} else if budgetVal, ok := val.(int); ok {
				thinkingConfig.Budget = budgetVal
			}
		}
	}

	// 2b. Override with Session Settings if present
	if cfg.Settings.Thinking != nil {
		if thinkingConfig == nil {
			thinkingConfig = &llm.ThinkingConfig{}
		}
		if cfg.Settings.Thinking.Enabled != nil {
			thinkingConfig.Enabled = *cfg.Settings.Thinking.Enabled
		}
		if cfg.Settings.Thinking.Adaptive != nil {
			thinkingConfig.Adaptive = *cfg.Settings.Thinking.Adaptive
		}
		if cfg.Settings.Thinking.Effort != nil {
			thinkingConfig.Effort = *cfg.Settings.Thinking.Effort
		}
		if cfg.Settings.Thinking.Budget != nil {
			thinkingConfig.Budget = *cfg.Settings.Thinking.Budget
		}
	}

	// 3. Compile the model configuration
	var modelConfig *llm.ModelConfig
	if thinkingConfig != nil {
		modelConfig = &llm.ModelConfig{
			Thinking: thinkingConfig,
		}
	}

	// 4. Set ContextWindow and MaxTokens from Warp specs
	var contextWindow, maxTokens int
	if cfg.ModelProvider != nil {
		for _, m := range cfg.ModelProvider.Spec.Models {
			if m.ID == cfg.ModelName {
				contextWindow = m.Limits.Context
				maxTokens = m.Limits.Output
				break
			}
		}
	}

	// Fallback to Loom's profile catalog values if 0
	if cfg.Provider != nil && (contextWindow == 0 || maxTokens == 0) {
		if prof, found := cfg.Provider.GetProfile(cfg.ModelName); found {
			if contextWindow == 0 {
				contextWindow = prof.Limits.Context
			}
			if maxTokens == 0 {
				maxTokens = prof.Limits.Output
			}
		}
	}

	if contextWindow > 0 {
		if modelConfig == nil {
			modelConfig = &llm.ModelConfig{}
		}
		modelConfig.ContextWindow = contextWindow
	}

	if maxTokens > 0 {
		if modelConfig == nil {
			modelConfig = &llm.ModelConfig{}
		}
		modelConfig.MaxTokens = maxTokens
	}

	// Check if temperature is supported by the resolved model profile
	tempSupported := true
	if cfg.Provider != nil {
		if prof, found := cfg.Provider.GetProfile(cfg.ModelName); found {
			tempSupported = prof.Capabilities.Temperature
		}
	}

	if tempSupported {
		if cfg.Agent != nil && cfg.Agent.Agent != nil && cfg.Agent.Agent.Spec.Temperature > 0 {
			tempVal := float32(cfg.Agent.Agent.Spec.Temperature)
			if modelConfig == nil {
				modelConfig = &llm.ModelConfig{}
			}
			modelConfig.Temperature = &tempVal
		}

		// Overlay temperature from session settings if present
		if cfg.Settings.Temperature != nil {
			tVal := float32(*cfg.Settings.Temperature)
			if modelConfig == nil {
				modelConfig = &llm.ModelConfig{}
			}
			modelConfig.Temperature = &tVal
		}
	}

	return llm.NewModel(cfg.Provider, cfg.ModelName, modelConfig)
}

type headerTransport struct {
	underlying http.RoundTripper
	headers    map[string]string
}

func (t *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	cloned := req.Clone(req.Context())
	for k, v := range t.headers {
		cloned.Header.Set(k, v)
	}
	transport := t.underlying
	if transport == nil {
		transport = http.DefaultTransport
	}
	return transport.RoundTrip(cloned)
}

func newHTTPClient(headers map[string]string) *http.Client {
	if len(headers) == 0 {
		return http.DefaultClient
	}
	expandedHeaders := make(map[string]string, len(headers))
	for k, v := range headers {
		expandedHeaders[k] = os.ExpandEnv(v)
	}
	return &http.Client{
		Transport: &headerTransport{
			underlying: http.DefaultTransport,
			headers:    expandedHeaders,
		},
	}
}

func resolveAuthToken(p *warp.ModelProvider) (string, error) {
	if p.Spec.Auth == nil {
		return "", nil
	}

	// 1. Resolve from Environment Variable
	if p.Spec.Auth.Env != "" {
		if val := os.Getenv(p.Spec.Auth.Env); val != "" {
			return val, nil
		}
	}

	// 2. Resolve from File Path
	if p.Spec.Auth.File != "" {
		filePath := p.Spec.Auth.File
		data, err := os.ReadFile(filePath)
		if err != nil {
			return "", fmt.Errorf("failed to read auth file %q: %w", filePath, err)
		}
		return strings.TrimSpace(string(data)), nil
	}

	return "", nil
}
