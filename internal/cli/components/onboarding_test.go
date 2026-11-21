package components

import (
	"strings"
	"testing"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/tui/testutils"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

func TestOnboardingHeaderAnimatesMascotAndShowsWorkspace(t *testing.T) {
	app := testutils.NewApp(t, renderWizardHeader("/workspace/example", store.StepTrust, theme.Get()), 80, 24)
	if out := testutils.SurfaceText(app.Backend); !strings.Contains(out, "◥██████████◣") {
		t.Fatalf("setup header did not start with the T:\n%s", out)
	}
	out := app.WaitFor(func(text string) bool { return strings.Contains(text, "█▜▛█") })
	lines := testutils.TrimmedLines(out)
	for i, line := range lines {
		if !strings.Contains(line, "TaskSmith setup") {
			continue
		}
		if i+2 >= len(lines) || !strings.Contains(line, "▜████████▛") || strings.Contains(line, "/workspace/example") ||
			!strings.Contains(lines[i+1], "█▜▛█") || !strings.Contains(lines[i+1], "/workspace/example") ||
			!strings.Contains(lines[i+2], "▟▛▀▀▜▙") {
			t.Fatalf("expected the mascot beside the setup title and workspace on separate rows:\n%s", out)
		}
		return
	}
	t.Fatalf("setup title missing:\n%s", out)
}

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
