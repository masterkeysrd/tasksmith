package gateway

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"strings"

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
	cfg                 ProviderConfig
	llmProv             llm.Provider
	decisionProv        decision.Provider
	embeddingProv       embedding.Provider
	authErr             error
	warning             string
	openaiResponsesOpts *loomopenai.ResponsesOptions
}

func initProvider(ctx context.Context, cfg ProviderConfig, env map[string]string) (*providerInstance, error) {
	inst := &providerInstance{cfg: cfg}

	switch cfg.Type {
	case "ollama":
		endpoint := cfg.Endpoint
		if endpoint == "" {
			endpoint = "http://127.0.0.1:11434"
		}
		if _, err := url.Parse(endpoint); err != nil {
			return nil, fmt.Errorf("gateway: invalid ollama endpoint %q: %w", endpoint, err)
		}
	case "openai":
		_, respOpts, err := parseOpenAIConfig(cfg.Name, cfg.Config)
		if err != nil {
			return nil, err
		}
		inst.openaiResponsesOpts = respOpts
	case "anthropic", "typesafe", "genai", "google-genai", "gemini", "antigravity":
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
		openaiOpts, _, _ := parseOpenAIConfig(cfg.Name, cfg.Config)
		var opts []openaiopt.RequestOption
		opts = append(opts, openaiopt.WithHTTPClient(httpClient))
		if token != "" {
			opts = append(opts, openaiopt.WithAPIKey(token))
		}
		if cfg.Endpoint != "" {
			opts = append(opts, openaiopt.WithBaseURL(cfg.Endpoint))
		}
		client := openai.NewClient(opts...)
		openaiProv := loomopenai.NewProvider(&client, openaiOpts...)
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

	case "genai", "google-genai", "gemini":
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

func parseOpenAIConfig(provName string, raw map[string]any) ([]loomopenai.Option, *loomopenai.ResponsesOptions, error) {
	var loomOpts []loomopenai.Option

	var apiModeVal string
	if v, ok := getMapValue(raw, "apiMode", "api_mode"); ok {
		if s, ok := v.(string); ok {
			apiModeVal = strings.TrimSpace(s)
		} else {
			return nil, nil, fmt.Errorf("gateway: provider %q: invalid openai apiMode %v (must be \"responses\" or \"chat_completions\")", provName, v)
		}
	}

	switch apiModeVal {
	case "":
		// auto-detect per model
	case "responses":
		loomOpts = append(loomOpts, loomopenai.WithAPIMode(loomopenai.APIModeResponses))
	case "chat_completions":
		loomOpts = append(loomOpts, loomopenai.WithAPIMode(loomopenai.APIModeChatCompletions))
	default:
		return nil, nil, fmt.Errorf("gateway: provider %q: invalid openai apiMode %q (must be \"responses\" or \"chat_completions\")", provName, apiModeVal)
	}

	if apiModeVal == "" {
		if v, ok := getMapValue(raw, "responsesAPI", "responses_api"); ok {
			if b, ok := v.(bool); ok {
				loomOpts = append(loomOpts, loomopenai.WithResponsesAPI(b))
			}
		}
	}

	storeVal := false
	if v, ok := getMapValue(raw, "store", "store"); ok {
		if b, ok := v.(bool); ok {
			storeVal = b
		}
	}

	respOpts := &loomopenai.ResponsesOptions{
		Store: &storeVal,
	}

	if v, ok := getMapValue(raw, "usePreviousResponseID", "use_previous_response_id"); ok {
		if b, ok := v.(bool); ok {
			respOpts.UsePreviousResponseID = b
		}
	}

	if v, ok := getMapValue(raw, "webSearch", "web_search"); ok {
		if b, ok := v.(bool); ok {
			respOpts.WebSearch = b
		}
	}

	if v, ok := getMapValue(raw, "codeInterpreter", "code_interpreter"); ok {
		if b, ok := v.(bool); ok {
			respOpts.CodeInterpreter = b
		}
	}

	if v, ok := getMapValue(raw, "fileSearch", "file_search"); ok {
		if fsMap, ok := v.(map[string]any); ok {
			fsc := &loomopenai.FileSearchConfig{}
			if vsIDs, ok := getMapValue(fsMap, "vectorStoreIDs", "vector_store_ids"); ok {
				switch val := vsIDs.(type) {
				case []string:
					fsc.VectorStoreIDs = val
				case []any:
					for _, item := range val {
						if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
							fsc.VectorStoreIDs = append(fsc.VectorStoreIDs, strings.TrimSpace(s))
						}
					}
				}
			}
			if maxResults, ok := getMapValue(fsMap, "maxNumResults", "max_num_results"); ok {
				switch n := maxResults.(type) {
				case int:
					fsc.MaxNumResults = int64(n)
				case int64:
					fsc.MaxNumResults = n
				case float64:
					fsc.MaxNumResults = int64(n)
				}
			}
			respOpts.FileSearch = fsc
		}
	}

	return loomOpts, respOpts, nil
}

func getMapValue(m map[string]any, camel, snake string) (any, bool) {
	if m == nil {
		return nil, false
	}
	if v, ok := m[camel]; ok {
		return v, true
	}
	if v, ok := m[snake]; ok {
		return v, true
	}
	return nil, false
}

func openAIResponsesOptionsMiddleware(defaults *loomopenai.ResponsesOptions) llm.Middleware {
	if defaults == nil {
		return nil
	}
	return func(next llm.Streamer) llm.Streamer {
		return func(ctx context.Context, req *llm.Request) (llm.StreamResponse, error) {
			if req != nil {
				extensions := make(map[string]llm.Extension, len(req.Extensions)+1)
				maps.Copy(extensions, req.Extensions)

				if ext, ok := extensions[loomopenai.ResponsesExtensionID]; ok {
					var opts loomopenai.ResponsesOptions
					switch v := ext.(type) {
					case loomopenai.ResponsesOptions:
						opts = v
					case *loomopenai.ResponsesOptions:
						if v != nil {
							opts = *v
						}
					}
					if opts.Store == nil && defaults.Store != nil {
						opts.Store = defaults.Store
					}
					if !opts.UsePreviousResponseID && defaults.UsePreviousResponseID {
						opts.UsePreviousResponseID = defaults.UsePreviousResponseID
					}
					if !opts.WebSearch && defaults.WebSearch {
						opts.WebSearch = defaults.WebSearch
					}
					if !opts.CodeInterpreter && defaults.CodeInterpreter {
						opts.CodeInterpreter = defaults.CodeInterpreter
					}
					if opts.FileSearch == nil && defaults.FileSearch != nil {
						opts.FileSearch = defaults.FileSearch
					}
					extensions[loomopenai.ResponsesExtensionID] = opts
				} else {
					var opts = *defaults
					extensions[loomopenai.ResponsesExtensionID] = opts
				}

				reqCopy := *req
				reqCopy.Extensions = extensions
				req = &reqCopy
			}
			return next(ctx, req)
		}
	}
}
