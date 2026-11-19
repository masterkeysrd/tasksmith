package gateway

import (
	"github.com/masterkeysrd/loom/profile"
	loomanthropic "github.com/masterkeysrd/loom/providers/anthropic"
	loomgenai "github.com/masterkeysrd/loom/providers/genai"
	loomopenai "github.com/masterkeysrd/loom/providers/openai"
)

// TypeCatalog returns loom's built-in model catalog (models.dev metadata:
// context window, pricing, capabilities) for a provider type, and whether that
// type ships a static catalog at all.
//
// The zero-value loom providers are used deliberately: their catalogs are
// compile-time generated static tables, so browsing what a provider *offers*
// never needs a credential, an endpoint, or a network round trip. Types whose
// catalog is discovered live (ollama lists installed models; antigravity
// refreshes from the signed-in account) report false so callers fall back to
// live discovery or a preset list instead of an empty answer that looks like
// "no models exist".
func TypeCatalog(providerType string) ([]profile.Profile, bool) {
	switch providerType {
	case "anthropic":
		return (&loomanthropic.Provider{}).ListProfiles(), true
	case "openai":
		return (&loomopenai.Provider{}).ListProfiles(), true
	case "genai", "google-genai", "gemini":
		return (&loomgenai.Provider{}).ListProfiles(), true
	default:
		return nil, false
	}
}
