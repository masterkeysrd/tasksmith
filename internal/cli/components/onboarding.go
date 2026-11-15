package components

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/loom/auth"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	uicomponents "github.com/masterkeysrd/tasksmith/internal/ui/components"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

// OnboardingWizardProps configures the interactive onboarding wizard.
type OnboardingWizardProps struct {
	Client   *client.Client
	OnFinish func()
	OnExit   func()
}

// OnboardingWizard renders the multi-step onboarding/setup wizard matching §4.
var OnboardingWizard = trail.FC("OnboardingWizard", func(props OnboardingWizardProps) trail.Node {
	_ = store.UseOnboarding(func(s store.OnboardingState) int { return s.Revision })
	step := store.UseOnboarding(func(s store.OnboardingState) store.OnboardingStep { return s.Step })
	active := store.UseOnboarding(func(s store.OnboardingState) bool { return s.Active })
	state := store.GetOnboarding()
	scheme := theme.UseTheme()

	if !active {
		return nil
	}

	frame := renderWizardHeader(state.Workspace, step, scheme)

	var content trail.Node
	switch step {
	case store.StepTrust:
		content = renderTrustStep(props, state)
	case store.StepProviderSrc:
		content = renderProviderSrcStep(props)
	case store.StepProviderPick:
		content = renderProviderPickStep(props, state)
	case store.StepCredentials:
		content = renderCredentialsStep(props, state)
	case store.StepMode:
		content = renderModeStep(props)
	case store.StepTools:
		content = renderToolsStep(props, state)
	case store.StepReview:
		content = renderReviewStep(props, state, scheme)
	default:
		content = nil
	}

	return trail.Box(trail.BoxProps{
		Style: style.S().FlexDirection(style.FlexColumn).Width(style.Percent(100)),
	},
		frame,
		content,
	)
})

func renderWizardHeader(wsPath string, step store.OnboardingStep, scheme *theme.Scheme) trail.Node {
	shortPath := ShortenHomePath(wsPath)

	stepOrder := []struct {
		step  store.OnboardingStep
		label string
	}{
		{store.StepTrust, "Trust"},
		{store.StepProviderPick, "Provider"},
		{store.StepMode, "Mode"},
		{store.StepTools, "Tools"},
		{store.StepReview, "Review"},
	}

	stepIndex := 0
	switch step {
	case store.StepTrust:
		stepIndex = 0
	case store.StepProviderSrc, store.StepProviderPick, store.StepCredentials:
		stepIndex = 1
	case store.StepMode:
		stepIndex = 2
	case store.StepTools:
		stepIndex = 3
	case store.StepReview:
		stepIndex = 4
	}

	var breadcrumbs []trail.Node
	for i, s := range stepOrder {
		if i > 0 {
			breadcrumbs = append(breadcrumbs, trail.Span(trail.SpanProps{
				Style: style.S().Foreground(scheme.Color.Text.Muted),
			}, trail.Text(" ─ ")))
		}
		dot := "○ "
		dotColor := scheme.Color.Text.Muted
		if i <= stepIndex {
			dot = "● "
			dotColor = scheme.Color.Text.Accent
		}
		breadcrumbs = append(breadcrumbs,
			trail.Span(trail.SpanProps{
				Style: style.S().Foreground(dotColor),
			}, trail.Text(dot)),
			trail.Span(trail.SpanProps{
				Style: style.S().Foreground(scheme.Color.Text.Primary),
			}, trail.Text(s.label)),
		)
	}

	titleBar := trail.Box(trail.BoxProps{
		Style: style.S().FlexDirection(style.FlexRow).MarginBottom(1),
	},
		trail.Span(trail.SpanProps{
			Style: style.S().Bold(true).Foreground(scheme.Color.Text.Primary),
		}, trail.Text("TaskSmith setup")),
		trail.Span(trail.SpanProps{
			Style: style.S().Foreground(scheme.Color.Text.Muted).MarginLeft(2),
		}, trail.Text(shortPath)),
	)

	border := style.RoundedBorder().Color(scheme.Color.Border.Default)
	return trail.Box(trail.BoxProps{
		Style: style.S().
			FlexDirection(style.FlexColumn).
			Border(border).
			Padding(1).
			MarginBottom(1),
	},
		titleBar,
		trail.Box(trail.BoxProps{
			Style: style.S().FlexDirection(style.FlexRow),
		}, breadcrumbs...),
	)
}

func renderTrustStep(props OnboardingWizardProps, state store.OnboardingState) trail.Node {
	return TrustPrompt(TrustPromptProps{
		Status: toTrustStatus(state.Status),
		OnSelect: func(decision TrustDecision) {
			ctx := context.Background()
			switch decision {
			case TrustDecisionFolder:
				_ = props.Client.TrustWorkspace(ctx, "folder")
				nextStepAfterTrust(state)
			case TrustDecisionParent:
				_ = props.Client.TrustWorkspace(ctx, "parent")
				nextStepAfterTrust(state)
			case TrustDecisionRestricted:
				// Proceed into restricted mode session
				if props.OnFinish != nil {
					props.OnFinish()
				}
			case TrustDecisionExit:
				if props.OnExit != nil {
					props.OnExit()
				}
			}
		},
		OnCancel: func() {
			if props.OnExit != nil {
				props.OnExit()
			}
		},
	})
}

func nextStepAfterTrust(state store.OnboardingState) {
	if state.Status.IsConfigured {
		if len(state.Status.MissingCredentials) > 0 {
			store.GoToStep(store.StepCredentials)
		} else {
			store.CloseOnboarding()
		}
	} else if len(state.Status.GlobalProviders) > 0 {
		store.GoToStep(store.StepProviderSrc)
	} else {
		store.GoToStep(store.StepProviderPick)
	}
}

func renderProviderSrcStep(props OnboardingWizardProps) trail.Node {
	options := []uicomponents.Option{
		{
			Value:       "global",
			Label:       "Use my global providers",
			Description: "Nothing provider-related is written to the repo (recommended)",
		},
		{
			Value:       "workspace",
			Label:       "Configure a provider for this workspace",
			Description: "Writes .agents/providers/ + WORKSPACE.md (shared with team)",
		},
		{
			Value:       "project",
			Label:       "Configure a provider for this project only",
			Description: "Writes <project>/.agents/providers/ (monorepo subproject)",
		},
	}

	return Picker(PickerProps{
		Title:    "How should this workspace connect to models?",
		Subtitle: "Choose how to configure model providers for this directory.",
		Options:  options,
		Bindings: []uicomponents.KeyBinding{
			{Key: "↑/↓", Label: "navigate"},
			{Key: "enter", Label: "select"},
			{Key: "esc", Label: "back"},
		},
		OnSelect: func(opt uicomponents.Option) {
			switch opt.Value {
			case "global":
				store.SetOnboardingScope("user")
				store.GoToStep(store.StepMode)
			case "project":
				store.SetOnboardingScope("project")
				store.GoToStep(store.StepProviderPick)
			default:
				store.SetOnboardingScope("workspace")
				store.GoToStep(store.StepProviderPick)
			}
		},
		OnCancel: func() {
			if !store.GoBack() && props.OnExit != nil {
				props.OnExit()
			}
		},
	})
}

func renderProviderPickStep(props OnboardingWizardProps, state store.OnboardingState) trail.Node {
	presetList := []struct {
		name string
		desc string
	}{
		{"anthropic", "claude-sonnet-5-5, opus-5-5, haiku-5-5"},
		{"openai", "gpt-6.1-sol, gpt-6-luna"},
		{"google-genai", "hydrogen, carbon, argon"},
		{"antigravity", "gemini via Cloud Code"},
		{"ollama", "local models (http://localhost:11434)"},
		{"typesafe", "jev-latest"},
		{"custom", "Custom endpoint…"},
	}

	var options []uicomponents.Option
	for _, p := range presetList {
		status := ""
		if summary := state.ProviderSummary(p.name); summary != nil && summary.HasKey {
			status = "✓ key detected"
			if summary.AuthType == "oauth2" {
				if summary.Account != "" {
					status = fmt.Sprintf("✓ %s", summary.Account)
				} else {
					status = "✓ OAuth authenticated"
				}
			}
		} else if p.name == "ollama" {
			status = "✓ running"
		}

		opt := uicomponents.Option{
			Value:       p.name,
			Label:       strings.ToUpper(p.name[:1]) + p.name[1:],
			Description: p.desc,
		}
		if status != "" {
			opt.Description = fmt.Sprintf("%s    [%s]", p.desc, status)
		}
		options = append(options, opt)
	}

	scopeHint := fmt.Sprintf("Scope: [%s]  (tab to change)", state.Scope)

	return Picker(PickerProps{
		Title:    "Choose a provider",
		Subtitle: scopeHint,
		Options:  options,
		Bindings: []uicomponents.KeyBinding{
			{Key: "↑/↓", Label: "navigate"},
			{Key: "tab", Label: "cycle scope"},
			{Key: "enter", Label: "select"},
			{Key: "esc", Label: "back"},
		},
		Keys: map[string]func(uicomponents.Option){
			"tab": func(_ uicomponents.Option) {
				switch state.Scope {
				case "user":
					store.SetOnboardingScope("workspace")
				case "workspace":
					store.SetOnboardingScope("project")
				default:
					store.SetOnboardingScope("user")
				}
			},
		},
		OnSelect: func(opt uicomponents.Option) {
			store.SetOnboardingProvider(opt.Value)
			summary := state.ProviderSummary(opt.Value)
			isOAuth := (summary != nil && summary.AuthType == "oauth2") || (state.AuthType == "oauth2")
			if opt.Value == "ollama" || (state.IsProviderConfigured(opt.Value) && !isOAuth) {
				store.GoToStep(store.StepMode)
			} else {
				store.GoToStep(store.StepCredentials)
			}
		},
		OnCancel: func() {
			if !store.GoBack() && props.OnExit != nil {
				props.OnExit()
			}
		},
	})
}

func renderCredentialsStep(props OnboardingWizardProps, state store.OnboardingState) trail.Node {
	prov := state.SelectedProvider
	if prov == "" && len(state.Status.MissingCredentials) > 0 {
		prov = state.Status.MissingCredentials[0]
	}

	summary := state.ProviderSummary(prov)
	if state.AuthType == "oauth2" || (summary != nil && summary.AuthType == "oauth2") {
		return renderOAuthCredentialsStep(props, state)
	}

	options := []uicomponents.Option{
		{
			Value:       "verify",
			Label:       "Verify & continue",
			Description: "Verify credentials against the provider API",
		},
		{
			Value:       "skip",
			Label:       "Skip verification",
			Description: "Proceed without testing credentials now",
		},
	}

	return Picker(PickerProps{
		Title:    fmt.Sprintf("Configure %s credentials", prov),
		Subtitle: "Enter the required API key or credentials.",
		Options:  options,
		InputActions: map[string]InputAction{
			"key": {
				Prompt: "API Key: ",
				OnSubmit: func(_ uicomponents.Option, val string) {
					store.SetOnboardingCredentials(val, state.Endpoint, state.AuthType, state.AuthEnv)
				},
			},
		},
		OnSelect: func(opt uicomponents.Option) {
			if opt.Value == "verify" && state.APIKey != "" {
				ctx := context.Background()
				resp, err := props.Client.VerifyProvider(ctx, api.VerifyProviderRequest{
					Provider: state.SelectedProvider,
					APIKey:   state.APIKey,
				})
				if err == nil && resp.Valid {
					store.GoToStep(store.StepMode)
					return
				}
			}
			store.GoToStep(store.StepMode)
		},
		OnCancel: func() {
			if !store.GoBack() && props.OnExit != nil {
				props.OnExit()
			}
		},
	})
}

type oauthWaiterProps struct {
	Client       *client.Client
	Provider     string
	OAuthAuthURL string
}

var oauthWaiter = trail.FC("OAuthWaiter", func(props oauthWaiterProps) trail.Node {
	kitex.UseInterval(func() {
		resp, err := props.Client.GetProviderAuthStatus(context.Background(), props.Provider)
		if err == nil && resp != nil {
			switch resp.Status {
			case "authenticated":
				store.SetOnboardingOAuth("authenticated", "", resp.Account, "")
				store.GoToStep(store.StepMode)
			case "error":
				store.SetOnboardingOAuth("error", "", "", resp.Error)
			}
		}
	}, 500*time.Millisecond, []any{})

	options := []uicomponents.Option{
		{
			Value:       "cancel",
			Label:       "Cancel authorization",
			Description: "Abort browser login and return to provider options",
		},
	}

	titleName := props.Provider
	if len(titleName) > 0 {
		titleName = strings.ToUpper(titleName[:1]) + titleName[1:]
	}

	subtitle := fmt.Sprintf("Waiting for %s authorization on http://localhost:51121...", titleName)
	if props.OAuthAuthURL != "" {
		subtitle = fmt.Sprintf("Waiting on localhost:51121. If browser did not open, visit:\n%s", props.OAuthAuthURL)
	}

	return Picker(PickerProps{
		Title:    fmt.Sprintf("Authenticating with %s", titleName),
		Subtitle: subtitle,
		Options:  options,
		OnSelect: func(_ uicomponents.Option) {
			store.SetOnboardingOAuth("idle", "", "", "")
		},
		OnCancel: func() {
			store.SetOnboardingOAuth("idle", "", "", "")
		},
	})
})

func renderOAuthCredentialsStep(props OnboardingWizardProps, state store.OnboardingState) trail.Node {
	prov := state.SelectedProvider
	if prov == "" && len(state.Status.MissingCredentials) > 0 {
		prov = state.Status.MissingCredentials[0]
	}

	titleName := prov
	if len(titleName) > 0 {
		titleName = strings.ToUpper(titleName[:1]) + titleName[1:]
	}

	envVar := state.AuthEnv
	if envVar == "" {
		envVar = strings.ToUpper(prov) + "_TOKEN"
	}

	if state.OAuthStatus == "authorizing" {
		return oauthWaiter(oauthWaiterProps{
			Client:       props.Client,
			Provider:     prov,
			OAuthAuthURL: state.OAuthAuthURL,
		})
	}

	summary := state.ProviderSummary(prov)
	isAuthenticated := (summary != nil && summary.HasKey) || state.OAuthStatus == "authenticated"
	currentAccount := ""
	if summary != nil && summary.Account != "" {
		currentAccount = summary.Account
	} else if state.OAuthAccount != "" {
		currentAccount = state.OAuthAccount
	}

	if isAuthenticated {
		accountDesc := fmt.Sprintf("Authenticated via %s OAuth", titleName)
		if currentAccount != "" {
			accountDesc = fmt.Sprintf("Active Account: %s", currentAccount)
		}

		options := []uicomponents.Option{
			{
				Value:       "continue",
				Label:       "Continue with active account",
				Description: accountDesc,
			},
			{
				Value:       "reauth",
				Label:       "Switch account / Re-authenticate",
				Description: "Opens browser to log in with a different account",
			},
			{
				Value:       "env",
				Label:       fmt.Sprintf("Use %s", envVar),
				Description: "Provide an explicit static token",
			},
		}

		return Picker(PickerProps{
			Title:    fmt.Sprintf("%s Credentials", titleName),
			Subtitle: fmt.Sprintf("✓ %s", accountDesc),
			Options:  options,
			InputActions: map[string]InputAction{
				"env": {
					Prompt: fmt.Sprintf("%s: ", envVar),
					OnSubmit: func(_ uicomponents.Option, val string) {
						store.SetOnboardingCredentials(val, state.Endpoint, "oauth2", envVar)
						store.GoToStep(store.StepMode)
					},
				},
			},
			OnSelect: func(opt uicomponents.Option) {
				switch opt.Value {
				case "continue":
					store.GoToStep(store.StepMode)
				case "reauth":
					store.SetOnboardingOAuth("authorizing", "", "", "")
					go func() {
						ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
						defer cancel()
						resp, err := props.Client.StartProviderAuth(ctx, prov)
						if err == nil && resp != nil {
							store.SetOnboardingOAuth("authorizing", resp.AuthURL, "", "")
							_ = auth.OpenBrowser(resp.AuthURL)
						} else if err != nil {
							store.SetOnboardingOAuth("error", "", "", err.Error())
						}
					}()
				}
			},
			OnCancel: func() {
				if !store.GoBack() && props.OnExit != nil {
					props.OnExit()
				}
			},
		})
	}

	options := []uicomponents.Option{
		{
			Value:       "login",
			Label:       fmt.Sprintf("Log in with %s (Browser)", titleName),
			Description: fmt.Sprintf("Opens your browser to sign in with %s OAuth (PKCE)", titleName),
		},
		{
			Value:       "env",
			Label:       fmt.Sprintf("Use %s", envVar),
			Description: "Provide an explicit static environment token",
		},
		{
			Value:       "skip",
			Label:       "Skip for now",
			Description: fmt.Sprintf("Configure %s credentials later", titleName),
		},
	}

	subtitle := fmt.Sprintf("Authenticate with %s to access its models.", titleName)
	if state.OAuthError != "" {
		subtitle = fmt.Sprintf("Authentication error: %s. Try again:", state.OAuthError)
	}

	return Picker(PickerProps{
		Title:    fmt.Sprintf("Configure %s Credentials", titleName),
		Subtitle: subtitle,
		Options:  options,
		InputActions: map[string]InputAction{
			"env": {
				Prompt: fmt.Sprintf("%s: ", envVar),
				OnSubmit: func(_ uicomponents.Option, val string) {
					store.SetOnboardingCredentials(val, state.Endpoint, "oauth2", envVar)
					store.GoToStep(store.StepMode)
				},
			},
		},
		OnSelect: func(opt uicomponents.Option) {
			switch opt.Value {
			case "login":
				store.SetOnboardingOAuth("authorizing", "", "", "")
				go func() {
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					resp, err := props.Client.StartProviderAuth(ctx, prov)
					if err == nil && resp != nil {
						store.SetOnboardingOAuth("authorizing", resp.AuthURL, "", "")
						_ = auth.OpenBrowser(resp.AuthURL)
					} else if err != nil {
						store.SetOnboardingOAuth("error", "", "", err.Error())
					}
				}()
			case "skip":
				store.GoToStep(store.StepMode)
			}
		},
		OnCancel: func() {
			if !store.GoBack() && props.OnExit != nil {
				props.OnExit()
			}
		},
	})
}

func renderModeStep(props OnboardingWizardProps) trail.Node {
	options := []uicomponents.Option{
		{
			Value:       string(api.ModeDefault),
			Label:       "Default",
			Description: "Read tools run; edits, shell and network ask (recommended)",
		},
		{
			Value:       string(api.ModeAcceptEdits),
			Label:       "Accept edits",
			Description: "File edits run; shell and network ask",
		},
		{
			Value:       string(api.ModePlan),
			Label:       "Plan",
			Description: "Read-only; mutating tools blocked",
		},
		{
			Value:       string(api.ModeAsk),
			Label:       "Ask",
			Description: "Ask before almost every tool call",
		},
		{
			Value:       string(api.ModeAuto),
			Label:       "Auto",
			Description: "Run everything except denied/excluded tools",
		},
	}

	return Picker(PickerProps{
		Title:    "Default permission mode",
		Subtitle: "Change any time with /mode or shift+tab (session only).",
		Options:  options,
		OnSelect: func(opt uicomponents.Option) {
			store.SetOnboardingMode(api.PermissionMode(opt.Value))
			store.GoToStep(store.StepTools)
		},
		OnCancel: func() {
			if !store.GoBack() && props.OnExit != nil {
				props.OnExit()
			}
		},
	})
}

func renderToolsStep(props OnboardingWizardProps, state store.OnboardingState) trail.Node {
	availableTools := []struct {
		name  string
		group string
	}{
		{"view", "Read"},
		{"ls", "Read"},
		{"glob", "Read"},
		{"grep", "Read"},
		{"tool_search", "Read"},
		{"edit", "Write"},
		{"write", "Write"},
		{"remove", "Write"},
		{"bash", "Execute"},
		{"web_fetch", "Network"},
		{"download", "Network"},
		{"invoke_agent", "Agents"},
		{"manage_agents", "Agents"},
		{"todos", "Agents"},
		{"tasks", "Agents"},
	}

	var options []uicomponents.Option
	for _, t := range availableTools {
		enabled := !state.DisabledTools[t.name]
		glyph := "[x] "
		if !enabled {
			glyph = "[ ] "
		}
		options = append(options, uicomponents.Option{
			Value:       t.name,
			Label:       glyph + t.name,
			Description: fmt.Sprintf("Group: %s", t.group),
		})
	}

	for _, m := range state.Status.GlobalMCPs {
		enabled := !state.DisabledMCPs[m]
		glyph := "[x] "
		if !enabled {
			glyph = "[ ] "
		}
		options = append(options, uicomponents.Option{
			Value:       "mcp:" + m,
			Label:       glyph + m,
			Description: "Group: MCP (Global server)",
		})
	}

	return Picker(PickerProps{
		Title:    "Which tools can agents use?",
		Subtitle: "space to toggle · enter to continue · esc to go back",
		Options:  options,
		Bindings: []uicomponents.KeyBinding{
			{Key: "space", Label: "toggle"},
			{Key: "enter", Label: "continue"},
			{Key: "esc", Label: "back"},
		},
		Keys: map[string]func(uicomponents.Option){
			"space": func(opt uicomponents.Option) {
				if after, ok := strings.CutPrefix(opt.Value, "mcp:"); ok {
					store.ToggleOnboardingMCP(after)
				} else {
					store.ToggleOnboardingTool(opt.Value)
				}
			},
		},
		OnSelect: func(opt uicomponents.Option) {
			store.GoToStep(store.StepReview)
		},
		OnCancel: func() {
			if !store.GoBack() && props.OnExit != nil {
				props.OnExit()
			}
		},
	})
}

func renderReviewStep(props OnboardingWizardProps, state store.OnboardingState, scheme *theme.Scheme) trail.Node {
	var changes []string
	if state.Scope != "user" {
		changes = append(changes, "+ WORKSPACE.md (defaultProvider: "+state.SelectedProvider+")")
		changes = append(changes, "+ .agents/providers/"+state.SelectedProvider+".yaml")
		if state.APIKey != "" {
			changes = append(changes, "~ .env (git-ignored automatically)")
			changes = append(changes, "~ .gitignore (+ .env)")
		}
	} else {
		changes = append(changes, "• ~/.config/tasksmith/agents/providers/"+state.SelectedProvider+".yaml")
		if state.APIKey != "" {
			changes = append(changes, "• ~/.config/tasksmith/.env")
		}
	}
	changes = append(changes, "• ~/.config/tasksmith/permissions.json (defaultMode: "+string(state.DefaultMode)+")")
	changes = append(changes, "• trust.json, workspace.json")

	options := []uicomponents.Option{
		{
			Value:       "write",
			Label:       "Write files and start",
			Description: "Apply configurations and begin session",
		},
		{
			Value:       "back",
			Label:       "Back",
			Description: "Return to previous screen to edit settings",
		},
	}

	var changeSpans []trail.Node
	for _, c := range changes {
		changeSpans = append(changeSpans, trail.Span(trail.SpanProps{
			Style: style.S().Foreground(scheme.Color.Text.Primary),
		}, trail.Text("  "+c)))
	}

	summaryNode := trail.Box(trail.BoxProps{
		Style: style.S().FlexDirection(style.FlexColumn).MarginBottom(1),
	}, changeSpans...)

	return Picker(PickerProps{
		Title:    "Ready to write",
		Subtitle: "Review configuration before writing to disk.",
		Header:   summaryNode,
		Options:  options,
		OnSelect: func(opt uicomponents.Option) {
			if opt.Value == "write" {
				ctx := context.Background()
				if err := store.FinishOnboarding(ctx, props.Client); err == nil {
					if props.OnFinish != nil {
						props.OnFinish()
					}
				}
			} else {
				_ = store.GoBack()
			}
		},
		OnCancel: func() {
			if !store.GoBack() && props.OnExit != nil {
				props.OnExit()
			}
		},
	})
}

func toTrustStatus(s api.WorkspaceStatus) api.TrustStatus {
	return api.TrustStatus{
		Path:               s.Path,
		IsTrusted:          s.IsTrusted,
		FingerprintChanged: s.FingerprintChanged,
		Executables:        s.Executables,
	}
}
