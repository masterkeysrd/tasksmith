package gateway

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/loom/profile"
	"github.com/masterkeysrd/tasksmith/api"
)

func ResolveSettings(prof EffectiveProfile, settings ModelSettings) (ResolvedConfig, error) {
	cfg := &llm.ModelConfig{}
	if settings.Config != nil {
		*cfg = *settings.Config
	}
	if prof.Profile != nil {
		if cfg.MaxTokens == 0 {
			cfg.MaxTokens = prof.Profile.Limits.Output
		}
		if cfg.ContextWindow == 0 {
			cfg.ContextWindow = prof.Profile.Limits.Context
		}
	}
	result := ResolvedConfig{Profile: prof, Config: cfg}
	setting := settings.SessionThinking
	if setting == nil {
		setting = settings.AgentThinking
	}
	var thinkingErr error
	if setting != nil {
		if err := validateThinking(setting, prof.Profile); err != nil {
			if settings.SessionThinking != nil {
				thinkingErr = err
			} else {
				result.Diagnostics = append(result.Diagnostics, Diagnostic{Code: "thinking_unsupported", Setting: "thinking", Message: fmt.Sprintf("Agent thinking setting is unavailable and will be omitted: %v", err)})
			}
			cfg.Thinking = nil
		} else {
			cfg.Thinking = thinkingConfig(setting)
		}
	}
	if cfg.Temperature != nil {
		if prof.ProviderType == "anthropic" && cfg.Thinking != nil && (cfg.Thinking.Budget > 0 || cfg.Thinking.Adaptive) {
			result.Diagnostics = append(result.Diagnostics, Diagnostic{Code: "temperature_thinking_conflict", Setting: "temperature", Message: "Temperature cannot be combined with Anthropic extended thinking and will be omitted."})
			cfg.Temperature = nil
		} else if !prof.TemperatureKnown {
			result.Diagnostics = append(result.Diagnostics, Diagnostic{Code: "temperature_unknown", Setting: "temperature", Message: "Temperature support is unknown; the configured value will be sent and may be rejected by the provider."})
		} else if prof.Profile != nil && !prof.Profile.Capabilities.Temperature {
			result.Diagnostics = append(result.Diagnostics, Diagnostic{Code: "temperature_unsupported", Setting: "temperature", Message: fmt.Sprintf("Temperature %g is not supported by this model and will be omitted.", *cfg.Temperature)})
			cfg.Temperature = nil
		}
	}
	return result, thinkingErr
}

func validateRequestMiddleware(prof EffectiveProfile) llm.Middleware {
	return func(next llm.Streamer) llm.Streamer {
		return func(ctx context.Context, req *llm.Request) (llm.StreamResponse, error) {
			if err := validateRequest(prof, req); err != nil {
				return nil, err
			}
			return next(ctx, req)
		}
	}
}

func validateRequest(prof EffectiveProfile, req *llm.Request) error {
	unsupported := func(capability, remedy string) error {
		return fmt.Errorf("%w: model %q does not support %s; %s", ErrUnsupportedCapability, req.Model, capability, remedy)
	}
	if prof.ToolsKnown && prof.Profile != nil && !prof.Profile.Capabilities.ToolCall {
		if len(req.Tools) > 0 {
			return unsupported("tools", "choose a model with tool support or disable the agent's tools")
		}
		for _, msg := range req.Messages {
			if msg.Role() == message.RoleTool {
				return unsupported("tool history", "choose a compatible model or start a new session")
			}
			for _, block := range msg.GetContent() {
				if _, ok := block.(*message.ToolCall); ok {
					return unsupported("tool history", "choose a compatible model or start a new session")
				}
			}
		}
	}
	if req.ResponseSchema != nil && prof.StructuredOutput != nil && !*prof.StructuredOutput {
		return unsupported("structured output", "choose a model with structured output support")
	}
	for i, msg := range req.Messages {
		for j, block := range msg.GetContent() {
			var modality profile.Modality
			var data []byte
			var url, mime string
			switch b := block.(type) {
			case *message.TextBlock:
				if b != nil && b.Text != "" && prof.Profile != nil && prof.Profile.Modalities.Inputs != nil && !slices.Contains(prof.Profile.Modalities.Inputs, profile.ModalityText) {
					return unsupported("text input", "choose a compatible model")
				}
				continue
			case *message.ThinkingBlock, *message.ToolCall:
				if msg.Role() != message.RoleAssistant {
					return fmt.Errorf("%w: model %q cannot safely send message %d block %d (%T) in role %q", ErrUnsupportedCapability, req.Model, i, j, block, msg.Role())
				}
				continue
			case *message.ImageBlock:
				if b != nil {
					modality, data, url, mime = profile.ModalityImage, b.Data, b.URL, b.MIMEType
				}
			case *message.AudioBlock:
				if b != nil {
					modality, data, url, mime = profile.ModalityAudio, b.Data, b.URL, b.MIMEType
				}
			case *message.VideoBlock:
				if b != nil {
					modality, data, url, mime = profile.ModalityVideo, b.Data, b.URL, b.MIMEType
				}
			case *message.DocumentBlock:
				if b != nil {
					data, url, mime = b.Data, b.URL, b.MIMEType
					if mime == "application/pdf" {
						modality = profile.ModalityPDF
					}
				}
			default:
				return fmt.Errorf("%w: model %q cannot safely send message %d block %d (%T)", ErrUnsupportedCapability, req.Model, i, j, block)
			}
			if prof.Profile != nil && modality != "" && prof.Profile.Modalities.Inputs != nil && !slices.Contains(prof.Profile.Modalities.Inputs, modality) {
				return unsupported(string(modality)+" input", "choose a compatible model or remove the input")
			}
			if msg.Role() != message.RoleUser || !adapterPreservesMedia(prof.ProviderType, block, data, url, mime) {
				return fmt.Errorf("%w: provider %q cannot safely send message %d block %d (%T); choose a compatible provider or remove the input", ErrUnsupportedCapability, prof.ProviderType, i, j, block)
			}
		}
	}
	return nil
}

func adapterPreservesMedia(provider string, block message.Block, data []byte, url, mime string) bool {
	if len(data) == 0 && url == "" {
		return false
	}
	switch provider {
	case "genai", "google-genai", "gemini", "antigravity":
		return true
	case "ollama":
		_, ok := block.(*message.ImageBlock)
		return ok && len(data) > 0
	case "anthropic":
		switch block.(type) {
		case *message.ImageBlock:
			return len(data) > 0
		case *message.DocumentBlock:
			return len(data) > 0 && mime == "application/pdf"
		}
	case "openai":
		_, ok := block.(*message.ImageBlock)
		return ok
	}
	return false
}

func validateThinking(setting *api.ThinkingSetting, prof *profile.Profile) error {
	if setting == nil || setting.Type == "default" {
		return nil
	}
	if prof == nil {
		return fmt.Errorf("%w: thinking capabilities are unavailable for this model", ErrInvalidConfig)
	}
	kind := setting.Type
	if kind == "budget" {
		kind = "budget_tokens"
	}
	for _, option := range prof.Capabilities.ReasoningOptions {
		if option.Type != kind && (kind != "budget_tokens" || option.Type != "budget") {
			continue
		}
		switch kind {
		case "effort":
			if setting.Effort == "" || !slices.Contains(option.Values, setting.Effort) {
				return fmt.Errorf("%w: unsupported effort %q (available: %s)", ErrInvalidConfig, setting.Effort, strings.Join(option.Values, ", "))
			}
		case "budget_tokens":
			if setting.Budget <= 0 || setting.Budget > 2147483647 || (option.Min > 0 && setting.Budget < option.Min) || (option.Max > 0 && setting.Budget > option.Max) {
				return fmt.Errorf("%w: thinking budget %d is outside the supported range (%d to %d tokens)", ErrInvalidConfig, setting.Budget, option.Min, option.Max)
			}
		case "toggle":
		default:
			return fmt.Errorf("%w: unknown thinking mode %q", ErrInvalidConfig, setting.Type)
		}
		return nil
	}
	return fmt.Errorf("%w: %s is not supported by the current model", ErrInvalidConfig, setting.Type)
}

func thinkingConfig(setting *api.ThinkingSetting) *llm.ThinkingConfig {
	if setting == nil {
		return nil
	}
	switch setting.Type {
	case "effort":
		return &llm.ThinkingConfig{Enabled: true, Effort: setting.Effort}
	case "budget", "budget_tokens":
		return &llm.ThinkingConfig{Enabled: true, Budget: setting.Budget}
	case "toggle":
		return &llm.ThinkingConfig{Enabled: setting.Enabled}
	}
	return nil
}

func (g *gatewayImpl) ResolveConfig(provider, modelID string, settings ModelSettings) (ResolvedConfig, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if g.closed {
		return ResolvedConfig{}, ErrGatewayClosed
	}
	inst, id, spec, err := g.lookupProviderAndModel(provider, modelID)
	if err != nil {
		return ResolvedConfig{}, err
	}
	return ResolveSettings(effectiveProfile(inst, id, spec), settings)
}
