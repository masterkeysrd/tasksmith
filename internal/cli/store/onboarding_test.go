package store

import (
	"testing"

	"github.com/masterkeysrd/tasksmith/api"
)

func TestStartOnboardingSteps(t *testing.T) {
	// 1. Untrusted workspace starts at StepTrust
	untrustedStatus := api.WorkspaceStatus{
		Path:      "/workspace/test",
		IsTrusted: false,
	}
	StartOnboarding("/workspace/test", untrustedStatus, false)
	s := GetOnboarding()
	if !s.Active || s.Step != StepTrust {
		t.Fatalf("expected StepTrust, got %+v", s)
	}

	// 2. Trusted but missing credentials starts at StepCredentials
	missingCredsStatus := api.WorkspaceStatus{
		Path:               "/workspace/test",
		IsTrusted:          true,
		IsConfigured:       true,
		MissingCredentials: []string{"ANTHROPIC_API_KEY"},
	}
	StartOnboarding("/workspace/test", missingCredsStatus, false)
	s = GetOnboarding()
	if s.Step != StepCredentials {
		t.Fatalf("expected StepCredentials, got %+v", s)
	}

	// 3. Trusted unconfigured with global providers starts at StepProviderSrc
	globalProvStatus := api.WorkspaceStatus{
		Path:         "/workspace/test",
		IsTrusted:    true,
		IsConfigured: false,
		GlobalProviders: []api.ProviderSummary{
			{Name: "anthropic", HasKey: true},
		},
	}
	StartOnboarding("/workspace/test", globalProvStatus, false)
	s = GetOnboarding()
	if s.Step != StepProviderSrc {
		t.Fatalf("expected StepProviderSrc, got %+v", s)
	}

	// 4. Trusted unconfigured without global providers starts at StepProviderPick
	emptyStatus := api.WorkspaceStatus{
		Path:         "/workspace/test",
		IsTrusted:    true,
		IsConfigured: false,
	}
	StartOnboarding("/workspace/test", emptyStatus, false)
	s = GetOnboarding()
	if s.Step != StepProviderPick || s.Scope != "user" {
		t.Fatalf("expected StepProviderPick with Scope=user, got %+v", s)
	}
}

func TestOnboardingNavigationAndToggles(t *testing.T) {
	status := api.WorkspaceStatus{
		Path:      "/workspace/test",
		IsTrusted: true,
	}
	StartOnboarding("/workspace/test", status, false)

	// Step navigation
	GoToStep(StepMode)
	s := GetOnboarding()
	if s.Step != StepMode || len(s.History) != 1 {
		t.Fatalf("expected StepMode with 1 history entry, got %+v", s)
	}

	GoToStep(StepTools)
	s = GetOnboarding()
	if s.Step != StepTools || len(s.History) != 2 {
		t.Fatalf("expected StepTools with 2 history entries, got %+v", s)
	}

	// Back
	if !GoBack() {
		t.Fatal("expected GoBack to succeed")
	}
	s = GetOnboarding()
	if s.Step != StepMode {
		t.Fatalf("expected to return to StepMode, got %v", s.Step)
	}

	// Toggles
	ToggleOnboardingTool("bash")
	if !GetOnboarding().DisabledTools["bash"] {
		t.Fatal("expected bash to be disabled")
	}
	ToggleOnboardingTool("bash")
	if GetOnboarding().DisabledTools["bash"] {
		t.Fatal("expected bash to be enabled after second toggle")
	}

	ToggleOnboardingMCP("github")
	if !GetOnboarding().DisabledMCPs["github"] {
		t.Fatal("expected github MCP to be disabled")
	}
}

func TestSetOnboardingOAuth(t *testing.T) {
	status := api.WorkspaceStatus{
		Path:      "/workspace/test",
		IsTrusted: true,
	}
	StartOnboarding("/workspace/test", status, false)

	SetOnboardingOAuth("authorizing", "https://accounts.google.com/o/oauth2/v2/auth?client_id=xyz", "", "")
	s := GetOnboarding()
	if s.OAuthStatus != "authorizing" || s.OAuthAuthURL == "" {
		t.Fatalf("expected authorizing status, got %+v", s)
	}

	SetOnboardingOAuth("authenticated", "", "developer@example.com", "")
	s = GetOnboarding()
	if s.OAuthStatus != "authenticated" || s.OAuthAccount != "developer@example.com" {
		t.Fatalf("expected authenticated with account, got %+v", s)
	}

	SetOnboardingOAuth("error", "", "", "auth failed")
	s = GetOnboarding()
	if s.OAuthStatus != "error" || s.OAuthError != "auth failed" {
		t.Fatalf("expected error status, got %+v", s)
	}
}
