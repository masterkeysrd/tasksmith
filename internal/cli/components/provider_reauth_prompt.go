package components

import (
	"fmt"
	"strings"

	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	uicomponents "github.com/masterkeysrd/tasksmith/internal/ui/components"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

// ProviderReauthPromptProps configures the inline re-authentication prompt
// shown in <Slot /> when a turn fails because an OAuth provider session expired.
type ProviderReauthPromptProps struct {
	Request   store.ProviderReauthRequest
	OnSignIn  func()
	OnDismiss func()
}

// ProviderReauthPrompt offers a one-step sign-in action or dismissal when an
// OAuth provider reports an expired session during a turn.
var ProviderReauthPrompt = trail.FC("ProviderReauthPrompt", func(props ProviderReauthPromptProps) trail.Node {
	scheme := theme.UseTheme()

	display := ProviderDisplayName(props.Request.Provider, "")
	if display == "" {
		display = "provider"
	}
	message := strings.TrimSpace(props.Request.Message)
	if message == "" {
		message = fmt.Sprintf("Authentication for %s has expired.", display)
	}

	header := trail.Box(trail.BoxProps{
		Style: style.S().FlexDirection(style.FlexColumn).MarginBottom(1),
	},
		trail.Span(trail.SpanProps{
			Style: style.S().Foreground(scheme.Color.Text.Warning).WhiteSpace(style.WhiteSpacePreWrap),
		}, trail.Text(message)),
	)

	options := []uicomponents.Option{
		{
			Value:       "signin",
			Label:       fmt.Sprintf("Sign in to %s now", display),
			Description: "Open the provider sign-in flow",
		},
		{
			Value:       "dismiss",
			Label:       "Dismiss",
			Description: "Stay in the conversation",
		},
	}

	return Picker(PickerProps{
		Title:  fmt.Sprintf("%s sign-in required", display),
		Header: header,
		Bindings: []uicomponents.KeyBinding{
			{Key: "↑/↓", Label: "navigate"},
			{Key: "enter", Label: "select"},
			{Key: "esc", Label: "dismiss"},
		},
		Options: options,
		OnSelect: func(opt uicomponents.Option) {
			if opt.Value == "signin" {
				if props.OnSignIn != nil {
					props.OnSignIn()
				}
				return
			}
			if props.OnDismiss != nil {
				props.OnDismiss()
			}
		},
		OnCancel: props.OnDismiss,
	})
})
