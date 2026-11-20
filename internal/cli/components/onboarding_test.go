package components

import (
	"strings"
	"testing"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/tui/testutils"
)

func TestOnboardingShowsChatGPTBrowserSignIn(t *testing.T) {
	props := OnboardingWizardProps{}
	picker := testutils.Render(t, renderProviderPickStep(props, store.OnboardingState{}), 100, 30)
	for _, description := range []string{"sign in with ChatGPT", "gpt-6.2-sol, gpt-6.2-astra", "neon, hydrogen, carbon, argon"} {
		if !strings.Contains(picker, description) {
			t.Fatalf("provider picker omitted %q:\n%s", description, picker)
		}
	}
	credentials := testutils.Render(t, renderCredentialsStep(props, store.OnboardingState{
		SelectedProvider: "codex",
		Status: api.WorkspaceStatus{GlobalProviders: []api.ProviderSummary{{
			Name: "codex", AuthType: "oauth2", BrowserAuthLabel: "Continue with ChatGPT",
		}}},
	}), 100, 30)
	if !strings.Contains(credentials, "Continue with ChatGPT") || !strings.Contains(credentials, "Skip for now") ||
		strings.Contains(credentials, "CODEX_TOKEN") {
		t.Fatalf("expected browser sign-in:\n%s", credentials)
	}
	waiting := testutils.Render(t, oauthWaiter(oauthWaiterProps{Provider: "codex"}), 100, 30)
	if !strings.Contains(waiting, "Waiting for Codex authorization in the browser") {
		t.Fatalf("expected browser status:\n%s", waiting)
	}
}
