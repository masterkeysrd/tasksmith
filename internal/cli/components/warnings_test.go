package components

import (
	"strings"
	"testing"

	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/tui/testutils"
)

func TestWarningsView(t *testing.T) {
	store.ClearSlot()
	store.ClearTranscript()
	store.SetWorkspaceWarnings(nil)
	store.SetWorkspaceMCPServers(nil)
	t.Cleanup(func() {
		store.ClearSlot()
		store.ClearTranscript()
		store.SetWorkspaceWarnings(nil)
		store.SetWorkspaceMCPServers(nil)
	})

	store.AppendWarning("Old timeline warning")
	OpenWarnings()
	OpenWarnings()
	if q := store.GetSlot().Queue; q == nil || len(*q) != 1 || (*q)[0].Kind != store.SlotKindPicker {
		t.Fatalf("expected one picker after two opens, got %v", q)
	}

	app := testutils.NewApp(t, Slot(), 140, 24)
	down := key.Key{Code: key.KeyDown}
	up := key.Key{Code: key.KeyUp}
	enter := key.Key{Code: key.KeyEnter}
	escape := key.Key{Code: key.KeyEscape}
	if out := app.Press(enter); !strings.Contains(out, "0 unresolved") || !strings.Contains(out, "No unresolved warnings.") || strings.Contains(out, "Old timeline warning") {
		t.Fatalf("empty view after Enter:\n%s", out)
	}

	workspaceWarning := "Provider configuration warning\nKeep  **literal** spacing."
	auth := api.MCPServer{Name: "notes", Status: api.MCPStatusAuthRequired, AuthType: api.MCPAuthBearer, Error: `missing environment variable "NOTES_TOKEN"`}
	store.SetWorkspaceWarnings([]string{workspaceWarning, workspaceWarning})
	store.SetWorkspaceMCPServers([]api.MCPServer{auth})
	app.WaitFor(func(out string) bool {
		return strings.Contains(out, "2 unresolved") && strings.Contains(out, "❯ ⚠ Provider configuration warning") &&
			strings.Contains(out, `MCP server "notes"`) && strings.Contains(out, "Fix: /mcp restart notes")
	})

	for _, keys := range []struct {
		down, up key.Key
	}{
		{down, up},
		{key.Key{Code: 'j', Text: "j"}, key.Key{Code: 'k', Text: "k"}},
		{key.Key{Code: 'n', Text: "n", Mod: key.ModCtrl}, key.Key{Code: 'p', Text: "p", Mod: key.ModCtrl}},
	} {
		if out := app.Press(keys.down); !strings.Contains(out, `❯ ⚠ MCP server "notes"`) {
			t.Fatalf("%v did not select the MCP warning:\n%s", keys.down, out)
		}
		if out := app.Press(keys.up); !strings.Contains(out, "❯ ⚠ Provider configuration warning") {
			t.Fatalf("%v did not select the workspace warning:\n%s", keys.up, out)
		}
	}
	if out := app.Press(enter); !strings.Contains(out, "Source: Workspace") || !strings.Contains(out, "Keep  **literal** spacing.") {
		t.Fatalf("workspace details did not preserve raw warning text:\n%s", out)
	}
	if out := app.Press(escape); !strings.Contains(out, "❯ ⚠ Provider configuration warning") || store.GetSlot().Active == nil {
		t.Fatalf("Escape did not return to the warning list:\n%s", out)
	}

	app.Press(down)
	if out := app.Press(enter); !strings.Contains(out, "Source: MCP") || !strings.Contains(out, api.MCPServerWarning(auth)) {
		t.Fatalf("MCP details did not preserve authoritative remediation:\n%s", out)
	}
	if out := app.Press(key.Key{Code: 'r', Text: "r"}); !strings.Contains(out, api.MCPServerWarning(auth)) {
		t.Fatalf("unbound key should not trigger remediation or close details:\n%s", out)
	}

	auth.Status = api.MCPStatusConnected
	store.SetWorkspaceMCPServers([]api.MCPServer{auth})
	app.WaitFor(func(out string) bool {
		return strings.Contains(out, "1 unresolved") && !strings.Contains(out, "Source: MCP") && !strings.Contains(out, "NOTES_TOKEN")
	})
	out := app.Press(escape)
	if store.GetSlot().Active != nil || strings.Contains(out, "unresolved") {
		t.Fatalf("Escape did not dismiss the warning picker:\n%s", out)
	}
}

func TestWarningsViewShowsExplanationsAndFixes(t *testing.T) {
	store.ClearSlot()
	store.SetWorkspaceWarnings([]string{
		`provider "openai": missing required environment variable "OPENAI_API_KEY"`,
		"skill review: .agents/skills/review/SKILL.md has invalid front matter",
	})
	store.SetWorkspaceMCPServers([]api.MCPServer{{Name: "github", Status: api.MCPStatusAuthRequired, AuthType: api.MCPAuthOAuth2}})
	t.Cleanup(func() {
		store.ClearSlot()
		store.SetWorkspaceWarnings(nil)
		store.SetWorkspaceMCPServers(nil)
	})
	OpenWarnings()
	app := testutils.NewApp(t, Slot(), 100, 24)
	out := strings.Join(testutils.TrimmedLines(testutils.SurfaceTextKeepBlanks(app.Backend)), "\n")
	for _, want := range []string{
		"❯ ⚠ Provider \"openai\" is missing configuration\n    missing required environment variable \"OPENAI_API_KEY\"\n    Fix: Set the required variable. See /providers for provider configuration.",
		"  ⚠ skill review could not load\n    .agents/skills/review/SKILL.md has invalid front matter\n    Fix: Correct the front matter and reload the workspace.",
		"  ⚠ MCP server \"github\" requires authentication\n    Authorization is required before the server can connect.\n    Fix: /mcp auth github",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("warning card missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "See /providers for provider configuration.\n\n  ⚠ skill review") {
		t.Fatalf("expected a blank line between warning cards:\n%s", out)
	}
}

func TestWarningsViewWrapsDetailsAtNarrowWidth(t *testing.T) {
	store.ClearSlot()
	store.SetWorkspaceWarnings([]string{"A long unresolved warning message\nwith an authoritative remediation at the end: review notes."})
	t.Cleanup(func() {
		store.ClearSlot()
		store.SetWorkspaceWarnings(nil)
	})
	OpenWarnings()
	app := testutils.NewApp(t, Slot(), 32, 16)
	if out := app.Text(); !strings.Contains(out, "…") || strings.Contains(out, "review notes.") {
		t.Fatalf("expected a truncated list preview:\n%s", out)
	}
	if out := app.Press(key.Key{Code: key.KeyEnter}); !strings.Contains(strings.Join(strings.Fields(out), " "), "with an authoritative remediation at the end: review notes.") {
		t.Fatalf("narrow detail view lost the end of the warning:\n%s", out)
	}
}
