package store

import (
	"context"
	"slices"

	"github.com/masterkeysrd/kite/extras/kites"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
)

// OnboardingStep identifies the active screen in the onboarding or /setup wizard.
type OnboardingStep string

const (
	StepNone         OnboardingStep = ""
	StepTrust        OnboardingStep = "trust"
	StepProviderSrc  OnboardingStep = "provider_src"
	StepProviderPick OnboardingStep = "provider_pick"
	StepCredentials  OnboardingStep = "credentials"
	StepMode         OnboardingStep = "mode"
	StepTools        OnboardingStep = "tools"
	StepReview       OnboardingStep = "review"
	StepDone         OnboardingStep = "done"
)

// OnboardingState encapsulates the mutable draft state of the onboarding wizard.
type OnboardingState struct {
	Revision         int
	Active           bool
	IsSetupCmd       bool
	Step             OnboardingStep
	History          []OnboardingStep
	Workspace        string
	Status           api.WorkspaceStatus
	Scope            string // "user", "workspace", "project"
	SelectedProvider string
	APIKey           string
	Endpoint         string
	AuthType         string
	AuthEnv          string
	DefaultMode      api.PermissionMode
	DisabledTools    map[string]bool
	DisabledMCPs     map[string]bool
	Verifying        bool
	VerifyError      string
	OAuthAuthURL     string
	OAuthStatus      string // "idle", "authorizing", "authenticated", "error"
	OAuthAccount     string
	OAuthError       string
	OfferInit        bool
	Completed        bool
}

var onboarding = kites.Create(OnboardingState{
	DisabledTools: make(map[string]bool),
	DisabledMCPs:  make(map[string]bool),
	DefaultMode:   api.ModeDefault,
	Scope:         "workspace",
})

// UseOnboarding subscribes a component to a comparable slice of OnboardingState.
func UseOnboarding[U comparable](selector func(OnboardingState) U) U {
	return kites.Use(onboarding, selector)
}

// GetOnboarding returns a snapshot of the current OnboardingState.
func GetOnboarding() OnboardingState {
	return onboarding.Get()
}

func updateOnboarding(fn func(s *OnboardingState)) {
	onboarding.Update(func(s *OnboardingState) {
		s.Revision++
		fn(s)
	})
}

// StartOnboarding initializes the wizard with the workspace status.
func StartOnboarding(wsPath string, status api.WorkspaceStatus, isSetupCmd bool) {
	scope := "workspace"
	if len(status.GlobalProviders) == 0 {
		scope = "user"
	}

	firstStep := StepProviderPick
	if !status.IsTrusted || status.FingerprintChanged {
		firstStep = StepTrust
	} else if status.IsConfigured && len(status.MissingCredentials) > 0 {
		firstStep = StepCredentials
	} else if len(status.GlobalProviders) > 0 {
		firstStep = StepProviderSrc
	}

	onboarding.Set(func(prev OnboardingState) OnboardingState {
		return OnboardingState{
			Revision:         prev.Revision + 1,
			Active:           true,
			IsSetupCmd:       isSetupCmd,
			Step:             firstStep,
			History:          nil,
			Workspace:        wsPath,
			Status:           status,
			Scope:            scope,
			DefaultMode:      api.ModeDefault,
			DisabledTools:    make(map[string]bool),
			DisabledMCPs:     make(map[string]bool),
			SelectedProvider: defaultProviderFromStatus(status),
		}
	})
}

func defaultProviderFromStatus(status api.WorkspaceStatus) string {
	for _, p := range status.GlobalProviders {
		if p.HasKey || p.Name == "ollama" {
			return p.Name
		}
	}
	if len(status.GlobalProviders) > 0 {
		return status.GlobalProviders[0].Name
	}
	return "anthropic"
}

// SetOnboardingScope updates the draft provider scope ("user", "workspace", "project").
func SetOnboardingScope(scope string) {
	updateOnboarding(func(s *OnboardingState) {
		s.Scope = scope
	})
}

// SetOnboardingProvider selects a provider for the draft.
func SetOnboardingProvider(provider string) {
	updateOnboarding(func(s *OnboardingState) {
		s.SelectedProvider = provider
	})
}

// SetOnboardingCredentials updates the API key / endpoint for the draft.
func SetOnboardingCredentials(apiKey, endpoint, authType, authEnv string) {
	updateOnboarding(func(s *OnboardingState) {
		s.APIKey = apiKey
		s.Endpoint = endpoint
		s.AuthType = authType
		s.AuthEnv = authEnv
		s.VerifyError = ""
	})
}

// SetOnboardingOAuth updates interactive OAuth progress in the store.
func SetOnboardingOAuth(status, authURL, account, errMsg string) {
	updateOnboarding(func(s *OnboardingState) {
		s.OAuthStatus = status
		s.OAuthAuthURL = authURL
		s.OAuthAccount = account
		s.OAuthError = errMsg
	})
}

// SetOnboardingMode updates the default permission mode.
func SetOnboardingMode(mode api.PermissionMode) {
	updateOnboarding(func(s *OnboardingState) {
		s.DefaultMode = mode
	})
}

// ToggleOnboardingTool toggles a tool between enabled and disabled.
func ToggleOnboardingTool(tool string) {
	updateOnboarding(func(s *OnboardingState) {
		if s.DisabledTools == nil {
			s.DisabledTools = make(map[string]bool)
		}
		if s.DisabledTools[tool] {
			delete(s.DisabledTools, tool)
		} else {
			s.DisabledTools[tool] = true
		}
	})
}

// ToggleOnboardingMCP toggles a global MCP server between enabled and disabled.
func ToggleOnboardingMCP(mcp string) {
	updateOnboarding(func(s *OnboardingState) {
		if s.DisabledMCPs == nil {
			s.DisabledMCPs = make(map[string]bool)
		}
		if s.DisabledMCPs[mcp] {
			delete(s.DisabledMCPs, mcp)
		} else {
			s.DisabledMCPs[mcp] = true
		}
	})
}

// GoToStep transitions directly to targetStep while recording current step in history.
func GoToStep(targetStep OnboardingStep) {
	updateOnboarding(func(s *OnboardingState) {
		if s.Step != targetStep {
			s.History = append(s.History, s.Step)
			s.Step = targetStep
		}
	})
}

// GoBack pops the previous step from History, or cancels if history is empty.
func GoBack() bool {
	state := onboarding.Get()
	if len(state.History) == 0 {
		return false
	}
	prev := state.History[len(state.History)-1]
	updateOnboarding(func(s *OnboardingState) {
		s.History = s.History[:len(s.History)-1]
		s.Step = prev
	})
	return true
}

// FinishOnboarding calls client.InitWorkspace with the draft configuration and marks completed.
func FinishOnboarding(ctx context.Context, c *client.Client) error {
	state := onboarding.Get()

	var disabledTools []string
	for t, disabled := range state.DisabledTools {
		if disabled {
			disabledTools = append(disabledTools, t)
		}
	}
	slices.Sort(disabledTools)

	var disabledMcps []string
	for m, disabled := range state.DisabledMCPs {
		if disabled {
			disabledMcps = append(disabledMcps, m)
		}
	}
	slices.Sort(disabledMcps)

	req := api.InitWorkspaceRequest{
		CWD:           state.Workspace,
		Scope:         state.Scope,
		Provider:      state.SelectedProvider,
		APIKey:        state.APIKey,
		Endpoint:      state.Endpoint,
		AuthType:      state.AuthType,
		AuthEnv:       state.AuthEnv,
		DefaultMode:   state.DefaultMode,
		DisabledTools: disabledTools,
		DisabledMCPs:  disabledMcps,
	}

	if err := c.InitWorkspace(ctx, req); err != nil {
		return err
	}

	updateOnboarding(func(s *OnboardingState) {
		s.Step = StepDone
		s.Active = false
		s.Completed = true
		s.OfferInit = !s.Status.HasAgentsMD
	})

	return nil
}

// CloseOnboarding closes the wizard without writing.
func CloseOnboarding() {
	updateOnboarding(func(s *OnboardingState) {
		s.Active = false
		s.Step = StepNone
	})
}

// IsProviderConfigured reports whether p has a detected key or is local ollama.
func (s OnboardingState) IsProviderConfigured(providerName string) bool {
	for _, p := range s.Status.GlobalProviders {
		if p.Name == providerName {
			return p.HasKey || p.Name == "ollama"
		}
	}
	return false
}

// ProviderSummary returns the summary for a provider, if known.
func (s OnboardingState) ProviderSummary(providerName string) *api.ProviderSummary {
	for _, p := range s.Status.GlobalProviders {
		if p.Name == providerName {
			return &p
		}
	}
	return nil
}
