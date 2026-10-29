package session

import (
	"errors"
	"fmt"

	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/tasksmith/internal/daemon/gateway"
)

func (s *Session) modelSettings(config SessionConfig) gateway.ModelSettings {
	settings := gateway.ModelSettings{Config: &llm.ModelConfig{}, SessionThinking: config.Thinking}
	if agent, ok := s.workspace.GetAgent(config.Agent); ok && agent != nil {
		if agent.Temperature != nil {
			value := float32(*agent.Temperature)
			settings.Config.Temperature = &value
		}
		settings.AgentThinking = AgentThinking(agent.Thinking)
	}
	return settings
}

func (s *Session) resolveModelConfig(config SessionConfig) (gateway.ResolvedConfig, error) {
	provider, model := resolveProviderAndModel(s.workspace, config.Model)
	return s.gateway.ResolveConfig(provider, model, s.modelSettings(config))
}

func (s *Session) modelDiagnostics(config SessionConfig) []gateway.Diagnostic {
	resolved, err := s.resolveModelConfig(config)
	for i := range resolved.Diagnostics {
		resolved.Diagnostics[i].Message = fmt.Sprintf("Agent %q, model %q: %s", config.Agent, config.Model, resolved.Diagnostics[i].Message)
	}
	if err != nil && config.Thinking != nil && errors.Is(err, gateway.ErrInvalidConfig) {
		return append(resolved.Diagnostics, gateway.Diagnostic{Code: "thinking_override_unsupported", Setting: "thinking", Message: fmt.Sprintf("The thinking override is unavailable for model %q: %v", config.Model, err)})
	}
	if err != nil {
		return []gateway.Diagnostic{{Code: "model_config_unavailable", Message: fmt.Sprintf("Model configuration is unavailable: %v", err)}}
	}
	return resolved.Diagnostics
}
