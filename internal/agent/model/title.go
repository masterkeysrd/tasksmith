package model

import (
	"context"
	"fmt"
	"strings"

	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/tasksmith/internal/agent/prompt"
	"github.com/masterkeysrd/warp"
)

// TitleParams holds the arguments needed to generate a chat session title.
type TitleParams struct {
	FirstUserMsg  string
	Provider      llm.Provider
	ModelName     string
	ModelProvider *warp.ModelProvider
	TitleAgent    *warp.ResolvedAgent
}

// GenerateTitle creates a short, descriptive title for a chat session based on the first user message.
func GenerateTitle(ctx context.Context, params TitleParams) (string, error) {
	if params.FirstUserMsg == "" {
		return "", fmt.Errorf("empty user message")
	}

	// 1. Construct the lightweight model config for title generation
	titleModel, err := New(ctx, Config{
		Provider:      params.Provider,
		ModelName:     params.ModelName,
		ModelProvider: params.ModelProvider,
		Agent:         params.TitleAgent,
	})
	if err != nil {
		return "", fmt.Errorf("failed to create title model: %w", err)
	}

	// 2. Render the title-generator agent prompt
	systemPrompt, err := prompt.RenderAgent(params.TitleAgent, nil, nil, nil)
	if err != nil {
		return "", fmt.Errorf("failed to render title generator system prompt: %w", err)
	}

	// Truncate message to avoid large prompt size
	truncated := params.FirstUserMsg
	if len(truncated) > 1000 {
		truncated = truncated[:1000] + "..."
	}

	userPrompt := fmt.Sprintf("Generate a title for a chat that starts with this message:\n\n%s", truncated)

	genMessages := []message.Message{
		message.NewSystemText(systemPrompt),
		message.NewUserText(userPrompt),
	}

	// Call the model
	asstResp, err := titleModel.Invoke(ctx, genMessages)
	if err != nil {
		return "", err
	}

	newTitle := strings.TrimSpace(asstResp.GetContent().Text())
	newTitle = strings.Trim(newTitle, `"'`+"`"+`*`)
	newTitle = strings.TrimSpace(newTitle)

	if newTitle == "" {
		return "", fmt.Errorf("empty response from model")
	}

	return newTitle, nil
}
