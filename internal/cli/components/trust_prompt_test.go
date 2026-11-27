package components

import (
	"strings"
	"testing"

	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/tui/testutils"
)

func TestInlineTrustFailureAndRestrictedChoice(t *testing.T) {
	initStatusLineTest(t)
	store.SetWorkspaceRestricted(true)
	c, err := client.New("missing.sock")
	if err != nil {
		t.Fatal(err)
	}
	store.SetClient(c)
	store.EnqueueSlot(store.SlotItem{ID: "workspace-trust", Kind: store.SlotKindTrust, Data: api.WorkspaceStatus{Path: "repo"}})
	app := testutils.NewApp(t, Slot(), 100, 32)
	app.Press(key.Key{Code: key.KeyEnter})
	app.WaitFor(func(text string) bool { return strings.Contains(text, "Could not trust workspace:") })
	app.Press(key.Key{Code: key.KeyDown})
	app.Press(key.Key{Code: key.KeyDown})
	app.Press(key.Key{Code: key.KeyEnter})
	if store.GetSlot().Active != nil || !store.GetApp().IsRestricted {
		t.Fatal("choosing restricted must dismiss only the prompt")
	}
}

func TestTrustPromptRendersDecisionsExecutableLine(t *testing.T) {
	out := testutils.Render(t, TrustPrompt(TrustPromptProps{
		Status: api.TrustStatus{
			Path: "/workspace/repo",
			Executables: api.ExecutableConfigSummary{
				Decisions: []string{".agents/decisions/permission-reviewer.decision.md"},
			},
		},
	}), 100, 24)

	for _, want := range []string{
		"/workspace/repo",
		"This project ships executable configuration:",
		"▸ 1 decision(s)",
		".agents/decisions/permission-reviewer.decision.md",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in TrustPrompt output:\n%s", want, out)
		}
	}
}
