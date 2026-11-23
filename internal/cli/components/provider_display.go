package components

import "strings"

func ProviderDisplayName(name, displayName string) string {
	if label := strings.TrimSpace(displayName); label != "" && label != name {
		return label
	}
	switch name {
	case "anthropic":
		return "Anthropic"
	case "openai":
		return "OpenAI"
	case "codex":
		return "Codex"
	case "claudecode":
		return "Claude Code"
	case "github-copilot":
		return "GitHub Copilot"
	case "google-genai":
		return "Google GenAI"
	case "antigravity":
		return "Antigravity"
	case "ollama":
		return "Ollama"
	case "typesafe":
		return "TypeSafe"
	case "custom":
		return "Custom"
	default:
		return name
	}
}
