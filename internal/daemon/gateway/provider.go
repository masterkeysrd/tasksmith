package gateway

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/anthropics/anthropic-sdk-go"
	anthropicopt "github.com/anthropics/anthropic-sdk-go/option"
	"github.com/masterkeysrd/loom/decision"
	"github.com/masterkeysrd/loom/embedding"
	"github.com/masterkeysrd/loom/llm"
	loomanthropic "github.com/masterkeysrd/loom/providers/anthropic"
	loomantigravity "github.com/masterkeysrd/loom/providers/antigravity"
	loomgenai "github.com/masterkeysrd/loom/providers/genai"
	loomollama "github.com/masterkeysrd/loom/providers/ollama"
	loomopenai "github.com/masterkeysrd/loom/providers/openai"
	loomtypesafe "github.com/masterkeysrd/loom/providers/typesafe"
	"github.com/ollama/ollama/api"
	"github.com/openai/openai-go/v3"
	openaiopt "github.com/openai/openai-go/v3/option"
	"google.golang.org/genai"
)

type providerInstance struct {
	cfg           ProviderConfig
	llmProv       llm.Provider
	decisionProv  decision.Provider
	embeddingProv embedding.Provider
	models        map[string]ModelSpec
	authErr       error
	warning       string
}

func initProvider(ctx context.Context, cfg ProviderConfig, env map[string]string) (*providerInstance, error) {
	modelsMap := make(map[string]ModelSpec, len(cfg.Models))
	for _, m := range cfg.Models {
		modelsMap[m.ID] = m
	}

	inst := &providerInstance{
		cfg:    cfg,
		models: modelsMap,
	}

	switch cfg.Type {
	case "ollama":
		endpoint := cfg.Endpoint
		if endpoint == "" {
			endpoint = "http://127.0.0.1:11434"
		}
		if _, err := url.Parse(endpoint); err != nil {
			return nil, fmt.Errorf("gateway: invalid ollama endpoint %q: %w", endpoint, err)
		}
	case "openai", "anthropic", "typesafe", "genai", "antigravity":
		// valid provider types
	default:
		return nil, fmt.Errorf("%w: unknown provider type %q", ErrInvalidConfig, cfg.Type)
	}

	token, err := resolveCredential(cfg.Auth, env)
	if err != nil {
		if errors.Is(err, ErrMissingCredential) {
			if cfg.Auth != nil && cfg.Auth.Env != "" {
				inst.authErr = fmt.Errorf("%w: provider %q requires environment variable %q", ErrMissingCredential, cfg.Name, cfg.Auth.Env)
				inst.warning = fmt.Sprintf("provider %q: missing required environment variable %q", cfg.Name, cfg.Auth.Env)
			} else {
				inst.authErr = fmt.Errorf("provider %q auth error: %w", cfg.Name, err)
				inst.warning = inst.authErr.Error()
			}
			return inst, nil
		}
		return nil, fmt.Errorf("provider %q auth error: %w", cfg.Name, err)
	}

	transport := buildHTTPTransport(nil, cfg.Auth, token, cfg.Headers)
	httpClient := &http.Client{Transport: transport}

	switch cfg.Type {
	case "ollama":
		endpoint := cfg.Endpoint
		if endpoint == "" {
			endpoint = "http://127.0.0.1:11434"
		}
		u, err := url.Parse(endpoint)
		if err != nil {
			return nil, fmt.Errorf("gateway: invalid ollama endpoint %q: %w", endpoint, err)
		}
		client := api.NewClient(u, httpClient)
		ollamaProv := (&loomollama.Provider{}).NewProvider(client, u)
		inst.llmProv = ollamaProv
		inst.decisionProv = ollamaProv
		inst.embeddingProv = ollamaProv

	case "openai":
		var opts []openaiopt.RequestOption
		opts = append(opts, openaiopt.WithHTTPClient(httpClient))
		if token != "" {
			opts = append(opts, openaiopt.WithAPIKey(token))
		}
		if cfg.Endpoint != "" {
			opts = append(opts, openaiopt.WithBaseURL(cfg.Endpoint))
		}
		client := openai.NewClient(opts...)
		openaiProv := loomopenai.NewProvider(&client)
		inst.llmProv = openaiProv
		inst.decisionProv = openaiProv
		inst.embeddingProv = openaiProv

	case "anthropic":
		var opts []anthropicopt.RequestOption
		opts = append(opts, anthropicopt.WithHTTPClient(httpClient))
		if token != "" {
			opts = append(opts, anthropicopt.WithAPIKey(token))
		}
		if cfg.Endpoint != "" {
			opts = append(opts, anthropicopt.WithBaseURL(cfg.Endpoint))
		}
		client := anthropic.NewClient(opts...)
		anthropicProv := loomanthropic.NewProvider(&client)
		inst.llmProv = anthropicProv

	case "typesafe":
		var opts []loomtypesafe.Option
		if token != "" {
			opts = append(opts, loomtypesafe.WithAPIKey(token))
		}
		if cfg.Endpoint != "" {
			opts = append(opts, loomtypesafe.WithBaseURL(cfg.Endpoint))
		}
		opts = append(opts, loomtypesafe.WithHTTPClient(httpClient))
		typesafeProv, err := loomtypesafe.NewProvider(opts...)
		if err != nil {
			return nil, fmt.Errorf("gateway: typesafe provider error: %w", err)
		}
		inst.decisionProv = typesafeProv

	case "genai":
		clientCfg := &genai.ClientConfig{
			APIKey:     token,
			Backend:    genai.BackendGeminiAPI,
			HTTPClient: httpClient,
		}
		genaiProv, err := loomgenai.NewProvider(ctx, clientCfg)
		if err != nil {
			return nil, fmt.Errorf("gateway: genai provider error: %w", err)
		}
		inst.llmProv = genaiProv
		inst.embeddingProv = genaiProv

	case "antigravity":
		tp := loomantigravity.StaticToken(token)
		antigravityProv, err := loomantigravity.NewProvider(&loomantigravity.Config{
			TokenProvider: tp,
			BaseURL:       cfg.Endpoint,
			HTTPClient:    httpClient,
		})
		if err != nil {
			return nil, fmt.Errorf("gateway: antigravity provider error: %w", err)
		}
		inst.llmProv = antigravityProv

	default:
		return nil, fmt.Errorf("%w: unknown provider type %q", ErrInvalidConfig, cfg.Type)
	}

	return inst, nil
}
