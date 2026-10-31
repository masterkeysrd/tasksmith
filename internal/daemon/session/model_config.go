package session

import (
	"errors"
	"fmt"

	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/daemon/models"
)

func (s *Session) modelSettings(config SessionConfig) models.Settings {
	settings := models.Settings{
		Thinking:       config.Thinking,
		StrictThinking: config.Thinking != nil,
	}
	if agent, ok := s.workspace.Catalog().GetAgent(config.Agent); ok {
		if agent.Def != nil && agent.Def.Spec.Temperature != nil {
			value := float32(*agent.Def.Spec.Temperature)
			settings.Temperature = &value
		}
		if settings.Thinking == nil {
			settings.Thinking = agent.Thinking
			settings.StrictThinking = false
		}
	}
	return settings
}

func (s *Session) resolveModelConfig(config SessionConfig) (llm.ModelConfig, []api.SessionDiagnostic, error) {
	model, ok := s.catalog().Get(config.Model)
	if !ok {
		return llm.ModelConfig{}, nil, fmt.Errorf("session: model %q unavailable", config.Model)
	}
	return model.Configure(s.modelSettings(config))
}

func (s *Session) modelDiagnostics(config SessionConfig) []api.SessionDiagnostic {
	_, diagnostics, err := s.resolveModelConfig(config)
	for i := range diagnostics {
		diagnostics[i].Message = fmt.Sprintf("Agent %q, model %q: %s", config.Agent, config.Model, diagnostics[i].Message)
	}
	if err != nil && config.Thinking != nil && errors.Is(err, models.ErrInvalidConfig) {
		return append(diagnostics, api.SessionDiagnostic{Code: "thinking_override_unsupported", Setting: "thinking", Message: fmt.Sprintf("The thinking override is unavailable for model %q: %v", config.Model, err)})
	}
	if err != nil {
		return []api.SessionDiagnostic{{Code: "model_config_unavailable", Message: fmt.Sprintf("Model configuration is unavailable: %v", err)}}
	}
	return diagnostics
}
