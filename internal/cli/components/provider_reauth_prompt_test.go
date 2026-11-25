package components

import (
	"strings"
	"testing"

	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/tui/testutils"
)

func TestProviderReauthPromptViaSlot(t *testing.T) {
	store.ClearSlot()
	t.Cleanup(func() {
		store.ClearSlot()
		store.SetProviderAuthLauncher(nil)
	})

	var launched string
	store.SetProviderAuthLauncher(func(provider string) {
		launched = provider
	})

	req := store.ProviderReauthRequest{
		Provider: "codex",
		Message:  `Authentication for provider "codex" has expired. Run /login codex to sign in again.`,
	}
	store.EnqueueSlot(store.SlotItem{
		ID:   "reauth-codex",
		Kind: store.SlotKindProviderReauth,
		Data: req,
	})

	app := testutils.NewApp(t, Slot(), 100, 24)
	out := app.WaitFor(func(s string) bool {
		return strings.Contains(s, "Codex sign-in required") &&
			strings.Contains(s, "Sign in to Codex now") &&
			strings.Contains(s, "Dismiss")
	})
	if !strings.Contains(out, `Authentication for provider "codex" has expired`) {
		t.Fatalf("expected expired message in prompt output:\n%s", out)
	}

	app.Press(key.Key{Code: key.KeyEnter})
	if launched != "codex" {
		t.Fatalf("LaunchProviderAuth called with %q, want codex", launched)
	}
	if store.GetSlot().Active != nil {
		t.Fatalf("expected slot removed after Enter, got %+v", store.GetSlot().Active)
	}

	// Re-enqueue and verify Esc dismisses without launching sign-in.
	launched = ""
	store.EnqueueSlot(store.SlotItem{
		ID:   "reauth-codex",
		Kind: store.SlotKindProviderReauth,
		Data: req,
	})
	app.WaitFor(func(s string) bool { return strings.Contains(s, "Sign in to Codex now") })
	app.Press(key.Key{Code: key.KeyEscape})
	if launched != "" {
		t.Fatalf("Esc should not launch sign-in, got %q", launched)
	}
	if store.GetSlot().Active != nil {
		t.Fatalf("expected slot removed after Esc, got %+v", store.GetSlot().Active)
	}
}

func TestWarningPreviewProviderRuntimeAuth(t *testing.T) {
	title, body, fix := warningPreview(store.Warning{
		Source:  "Workspace",
		Message: `provider "codex": authentication session expired — run /login codex to re-authenticate`,
	})
	if title != `provider "codex"` || body != "authentication session expired" || fix != "/login codex to re-authenticate" {
		t.Fatalf("warningPreview(expired) = (%q, %q, %q)", title, body, fix)
	}

	title, body, fix = warningPreview(store.Warning{
		Source:  "Workspace",
		Message: `provider "openai": API credential rejected (401 Unauthorized) — update your key or check /providers`,
	})
	if title != `provider "openai"` || body != "API credential rejected (401 Unauthorized)" || fix != "update your key or check /providers" {
		t.Fatalf("warningPreview(invalid) = (%q, %q, %q)", title, body, fix)
	}
}
