package components

import (
	"slices"
	"strings"
	"testing"

	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/testutils"
)

func aliasEditorTestProps() ModelAliasEditorProps {
	return ModelAliasEditorProps{
		Provider:     "provapi",
		ModelID:      "fast-model",
		ModelName:    "fast-model",
		ManifestPath: "~/.config/tasksmith/agents/providers/provapi.provider.yaml",
		Scope:        "User",
		DefaultModel: "mid-model",
		Assignments: []api.AliasAssignment{
			{Alias: "default", Provider: "provapi", Model: "mid-model"},
			{Alias: "fast", Provider: "provapi", Model: "fast-model"},
			{Alias: "smart", Provider: "other", Model: "o1"},
			{Alias: "mytier", Provider: "provapi", Model: "fast-model"},
		},
	}
}

func TestModelAliasEditorRendersStates(t *testing.T) {
	env := testutils.NewEnv(t, 110, 30)
	out := env.Render(ModelAliasEditor(aliasEditorTestProps()))

	for _, want := range []string{
		"Model aliases · provapi/fast-model",
		"[User]",
		"~/.config/tasksmith/agents/providers/provapi.provider.yaml",
		"[x] @fast",
		"assigned to this model",
		"[ ] @smart",
		"currently other/o1",
		"[x] @mytier",
		"currently provapi/mid-model",
		"Add custom alias",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered output lacks %q:\n%s", want, out)
		}
	}
}

func TestModelAliasEditorToggleAndSave(t *testing.T) {
	env := testutils.NewEnv(t, 110, 30)
	var saved []api.AssignModelAliasesRequest
	props := aliasEditorTestProps()
	props.OnSave = func(req api.AssignModelAliasesRequest) { saved = append(saved, req) }
	env.Render(ModelAliasEditor(props))

	// Cursor starts on @default; space stages it, save must request DefaultHere.
	out := env.Press(key.Key{Code: ' ', Text: " "})
	if !strings.Contains(out, "[x] @default") {
		t.Fatalf("space did not check @default:\n%s", out)
	}
	env.Press(key.Key{Code: key.KeyEnter})
	if len(saved) != 1 {
		t.Fatalf("OnSave calls = %d, want 1", len(saved))
	}
	req := saved[0]
	if !req.DefaultHere || req.Provider != "provapi" || req.Model != "fast-model" {
		t.Fatalf("save request = %+v, want provapi/fast-model with DefaultHere", req)
	}
	if strings.Join(req.Aliases, ",") != "fast,mytier" {
		t.Fatalf("save aliases = %v, want [fast mytier]", req.Aliases)
	}

	// Un-toggling @fast drops it from the exact saved set.
	saved = nil
	env.Press(key.Key{Code: 'j', Text: "j"}) // cursor -> @fast
	env.Press(key.Key{Code: ' ', Text: " "})
	env.Press(key.Key{Code: key.KeyEnter})
	if len(saved) != 1 || strings.Join(saved[0].Aliases, ",") != "mytier" {
		t.Fatalf("after un-toggle save = %+v, want aliases [mytier]", saved)
	}
}

func TestModelAliasEditorCustomAliasInput(t *testing.T) {
	env := testutils.NewEnv(t, 110, 30)
	props := aliasEditorTestProps()
	props.OnSave = func(api.AssignModelAliasesRequest) {}
	env.Render(ModelAliasEditor(props))

	env.Press(key.Key{Code: 'n', Text: "n"})
	out := env.Text()
	if strings.Contains(out, "Add custom alias…") {
		t.Fatalf("n did not open the inline alias input:\n%s", out)
	}

	// A standard alias name is refused with a notice instead of added.
	env.Type("smart")
	env.Press(key.Key{Code: key.KeyEnter})
	out = env.Text()
	if !strings.Contains(out, "standard alias") {
		t.Fatalf("missing standard-alias notice:\n%s", out)
	}

	// A fresh lowercase alias lands as a checked "new alias" row.
	env.Press(key.Key{Code: 'n', Text: "n"})
	env.Type("worktier")
	env.Press(key.Key{Code: key.KeyEnter})
	out = env.Text()
	if !strings.Contains(out, "[x] @worktier") || !strings.Contains(out, "new alias") {
		t.Fatalf("custom alias row missing:\n%s", out)
	}
}

// TestModelAliasEditorDuplicateSemantics covers a manual-YAML collision where
// another model wins the alias: opening the editor on the losing model must
// still show the alias as owned here (checking it saves), and the "assigns only
// here" reassurance appears only while the row is actually checked.
func TestModelAliasEditorDuplicateSemantics(t *testing.T) {
	env := testutils.NewEnv(t, 120, 30)
	var saved []api.AssignModelAliasesRequest
	props := ModelAliasEditorProps{
		Provider:  "provb",
		ModelID:   "b2",
		ModelName: "b2",
		Scope:     "Workspace",
		Assignments: []api.AliasAssignment{
			{Alias: "dup", Provider: "prova", Model: "a1", Duplicate: true, Duplicates: []string{"provb/b2"}},
		},
		OnSave: func(req api.AssignModelAliasesRequest) { saved = append(saved, req) },
	}
	out := env.Render(ModelAliasEditor(props))
	if !strings.Contains(out, "[x] @dup") {
		t.Fatalf("duplicate loser not shown as owned here:\\n%s", out)
	}
	if !strings.Contains(out, "⚠ duplicate on prova/a1 — saving assigns only here") {
		t.Fatalf("missing checked-duplicate warning:\\n%s", out)
	}

	// Move the cursor from @default down past the other four standard rows to
	// @dup, then uncheck it: both the checkmark and the suffix must disappear,
	// and saving must omit the alias.
	for range 5 {
		env.Press(key.Key{Code: 'j', Text: "j"})
	}
	env.Press(key.Key{Code: ' ', Text: " "})
	out = env.Text()
	if !strings.Contains(out, "[ ] @dup") {
		t.Fatalf("space did not uncheck the duplicate row:\\n%s", out)
	}
	if strings.Contains(out, "saving assigns only here") {
		t.Fatalf("suffix must not render while unchecked:\\n%s", out)
	}
	if !strings.Contains(out, "⚠ duplicate on prova/a1") {
		t.Fatalf("unchecked duplicate must still warn:\\n%s", out)
	}
	env.Press(key.Key{Code: key.KeyEnter})
	if len(saved) != 1 || len(saved[0].Aliases) != 0 {
		t.Fatalf("save after unchecking duplicate = %+v, want empty alias set", saved)
	}
}

// TestModelAliasEditorWorkspaceOverrideBeatsDuplicate proves the WORKSPACE.md
// override outranks a manifest collision: a workspace-owned alias that also
// collides across manifests keeps the override warning (saving cannot move it)
// and never shows the "duplicate on … saving assigns only here" reassurance.
func TestModelAliasEditorWorkspaceOverrideBeatsDuplicate(t *testing.T) {
	env := testutils.NewEnv(t, 120, 30)
	props := aliasEditorTestProps()
	props.Assignments = []api.AliasAssignment{
		{Alias: "default", Provider: "provapi", Model: "mid-model", WorkspaceDefault: true},
		{Alias: "teamlead", Provider: "other", Model: "o1", WorkspaceDefault: true,
			Duplicate: true, Duplicates: []string{"provapi/fast-model"}},
	}
	props.OnSave = func(api.AssignModelAliasesRequest) {}
	out := env.Render(ModelAliasEditor(props))

	if !strings.Contains(out, "⚠ WORKSPACE.md overrides @default") {
		t.Fatalf("missing workspace override warning on @default:\n%s", out)
	}
	if strings.Contains(out, "saving assigns only here") {
		t.Fatalf("duplicate reassurance must not render under a workspace override:\n%s", out)
	}

	// A typed alias that WORKSPACE.md redirects shows the override warning
	// rather than a "moves from" promise the save cannot keep.
	env.Press(key.Key{Code: 'n', Text: "n"})
	env.Type("teamlead")
	env.Press(key.Key{Code: key.KeyEnter})
	out = env.Text()
	if !strings.Contains(out, "[x] @teamlead") || !strings.Contains(out, "⚠ WORKSPACE.md overrides @teamlead") {
		t.Fatalf("typed workspace-overridden alias missing override warning:\n%s", out)
	}
	if strings.Contains(out, "moves from other/o1") {
		t.Fatalf("typed workspace-overridden alias promised a move:\n%s", out)
	}
}

// TestModelAliasEditorTypedForeignAliasStagesMove proves a custom alias owned
// by another model can be moved in one step: typing it at the n prompt adds a
// checked "moves from" row instead of rejecting the name.
func TestModelAliasEditorTypedForeignAliasStagesMove(t *testing.T) {
	env := testutils.NewEnv(t, 110, 30)
	var saved []api.AssignModelAliasesRequest
	props := aliasEditorTestProps()
	props.Assignments = append(props.Assignments,
		api.AliasAssignment{Alias: "teamlead", Provider: "other", Model: "o1"})
	props.OnSave = func(req api.AssignModelAliasesRequest) { saved = append(saved, req) }
	env.Render(ModelAliasEditor(props))

	env.Press(key.Key{Code: 'n', Text: "n"})
	env.Type("teamlead")
	out := env.Press(key.Key{Code: key.KeyEnter})
	if strings.Contains(out, "already belongs to") {
		t.Fatalf("foreign custom alias was rejected instead of staged:\\n%s", out)
	}
	if !strings.Contains(out, "[x] @teamlead") || !strings.Contains(out, "moves from other/o1") {
		t.Fatalf("typed foreign alias missing staged-move row:\\n%s", out)
	}

	env.Press(key.Key{Code: key.KeyEnter})
	if len(saved) != 1 {
		t.Fatalf("OnSave calls = %d, want 1", len(saved))
	}
	if !slices.Contains(saved[0].Aliases, "teamlead") {
		t.Fatalf("save aliases = %v, want teamlead included", saved[0].Aliases)
	}
}

func TestModelAliasEditorCustomInputOwnsShortcuts(t *testing.T) {
	env := testutils.NewEnv(t, 110, 30)
	props := aliasEditorTestProps()
	var saves, cancels int
	props.OnSave = func(api.AssignModelAliasesRequest) { saves++ }
	props.OnCancel = func() { cancels++ }
	env.Render(ModelAliasEditor(props))
	env.KeyPress("n")
	env.Type("njk12")
	out := env.KeyPress("enter")
	if saves != 0 || !strings.Contains(out, "[x] @njk12") {
		t.Fatalf("prompt submitted to checklist: saves=%d\n%s", saves, out)
	}
	env.KeyPress("n")
	out = env.KeyPress("esc")
	if cancels != 0 || !strings.Contains(out, "Add custom alias") {
		t.Fatalf("prompt escape dismissed checklist: cancels=%d\n%s", cancels, out)
	}
	for range len(StandardModelAliases) + 1 {
		env.KeyPress("down")
	}
	out = env.KeyPress("space")
	if !strings.Contains(out, "[ ] @njk12") {
		t.Fatalf("new alias did not uncheck:\n%s", out)
	}
}

func TestModelAliasEditorEscapeCancels(t *testing.T) {
	env := testutils.NewEnv(t, 110, 30)
	canceled := false
	props := aliasEditorTestProps()
	props.OnCancel = func() { canceled = true }
	env.Render(ModelAliasEditor(props))

	env.Press(key.Key{Code: key.KeyEscape})
	if !canceled {
		t.Fatal("esc did not invoke OnCancel")
	}
}

func TestModelAliasEditorLoadingAndError(t *testing.T) {
	env := testutils.NewEnv(t, 110, 30)
	props := aliasEditorTestProps()
	props.Loading = true
	props.ErrorMessage = "daemon: connection refused"
	out := env.Render(ModelAliasEditor(props))
	if !strings.Contains(out, "Loading alias assignments") {
		t.Errorf("missing loading placeholder:\n%s", out)
	}
	if !strings.Contains(out, "daemon: connection refused") {
		t.Errorf("missing error message:\n%s", out)
	}
}
