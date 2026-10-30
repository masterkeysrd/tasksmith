package gateway

import (
	"context"
	"fmt"
	"slices"

	"github.com/masterkeysrd/loom/llm"
	"github.com/masterkeysrd/loom/message"
	"github.com/masterkeysrd/loom/profile"
	"github.com/masterkeysrd/tasksmith/internal/daemon/models"
)

func validateRequestMiddleware(prof models.Model) llm.Middleware {
	return func(next llm.Streamer) llm.Streamer {
		return func(ctx context.Context, req *llm.Request) (llm.StreamResponse, error) {
			if err := validateRequest(prof, req); err != nil {
				return nil, err
			}
			return next(ctx, req)
		}
	}
}

func validateRequest(prof models.Model, req *llm.Request) error {
	unsupported := func(capability, remedy string) error {
		return fmt.Errorf("%w: model %q does not support %s; %s", ErrUnsupportedCapability, req.Model, capability, remedy)
	}
	if prof.Capabilities.Tools != nil && !*prof.Capabilities.Tools {
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
	if req.ResponseSchema != nil && prof.Capabilities.StructuredOutput != nil && !*prof.Capabilities.StructuredOutput {
		return unsupported("structured output", "choose a model with structured output support")
	}
	for i, msg := range req.Messages {
		for j, block := range msg.GetContent() {
			var modality profile.Modality
			var data []byte
			var url, mime string
			switch b := block.(type) {
			case *message.TextBlock:
				if b != nil && b.Text != "" && prof.Modalities.Input != nil && !slices.Contains(prof.Modalities.Input, string(profile.ModalityText)) {
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
			if modality != "" && prof.Modalities.Input != nil && !slices.Contains(prof.Modalities.Input, string(modality)) {
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
