package model

import (
	"context"
	"fmt"
	"strings"

	loomantigravity "github.com/masterkeysrd/loom/llm/antigravity"
)

// Login initiates an interactive authentication flow for the specified provider.
func Login(ctx context.Context, provider string) error {
	switch strings.ToLower(provider) {
	case "antigravity":
		tokenPath, err := AntigravityTokenPath()
		if err != nil {
			return err
		}
		oauthProvider, err := loomantigravity.NewOAuth2TokenProvider(loomantigravity.OAuth2Config{
			TokenFile: tokenPath,
		})
		if err != nil {
			return fmt.Errorf("failed to create antigravity oauth provider: %w", err)
		}

		fmt.Println("Initiating Google Antigravity OAuth login...")
		if err := oauthProvider.Login(ctx); err != nil {
			return fmt.Errorf("login failed: %w", err)
		}

		fmt.Printf("Login successful! Token saved to %s\n", tokenPath)
		return nil
	default:
		return fmt.Errorf("interactive login is not supported for provider %q", provider)
	}
}
