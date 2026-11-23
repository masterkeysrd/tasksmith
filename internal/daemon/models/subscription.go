package models

import "github.com/masterkeysrd/loom/profile"

func subscriptionPresetProfile(id ID, providerType string) *profile.Profile {
	name := id.Name()
	inputs := []profile.Modality{profile.ModalityText}
	attachment := false
	switch providerType {
	case "claudecode":
		switch name {
		case "claude-haiku-5-5", "claude-sonnet-5-5", "claude-opus-5-5", "claude-fable-5-2":
		default:
			return nil
		}
	case "github-copilot":
		switch name {
		case "gpt-6-luna", "gpt-6.2-sol", "gpt-6.1-terra", "gpt-6.2-astra":
			inputs = append(inputs, profile.ModalityImage, profile.ModalityPDF)
			attachment = true
		default:
			return nil
		}
	default:
		return nil
	}
	return &profile.Profile{
		ID: name,
		Capabilities: profile.Capabilities{
			Completion: true,
			ToolCall:   true,
			Attachment: attachment,
		},
		Modalities: profile.Modalities{Inputs: inputs, Outputs: []profile.Modality{profile.ModalityText}},
	}
}
