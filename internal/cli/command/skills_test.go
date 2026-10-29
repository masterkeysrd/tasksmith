package command

import (
	"fmt"
	"strings"
	"testing"

	"github.com/masterkeysrd/kite/backend"
	"github.com/masterkeysrd/kite/backend/mock"
	"github.com/masterkeysrd/kite/element"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/kite/testenv"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
)

func renderTestNode(t *testing.T, node trail.Node) string {
	t.Helper()
	env := testenv.Default(100, 24)
	t.Cleanup(env.Close)
	container := element.NewBox(env.Document())
	env.Mount(container)
	kitex.Render(node, container)
	env.Flush()
	return strings.TrimSpace(env.DumpText())
}

func TestSkillsCommandArgsValidation(t *testing.T) {
	reg := NewRegistry()

	// 1. With arguments: must be rejected
	err := reg.Execute("/skills test")
	if err == nil {
		t.Fatal("expected error when running /skills with args, got nil")
	}
	if !strings.Contains(err.Error(), "does not accept arguments") {
		t.Fatalf("unexpected error message: %v", err)
	}

	// 2. With multiple arguments: must be rejected
	err = reg.Execute("/skills foo bar baz")
	if err == nil {
		t.Fatal("expected error when running /skills with multiple args, got nil")
	}

	// 3. Without arguments: must succeed and enqueue slot
	store.ClearSlot()
	defer store.ClearSlot()
	err = reg.Execute("/skills")
	if err != nil {
		t.Fatalf("unexpected error running /skills: %v", err)
	}
	st := store.GetSlot()
	if st.Active == nil || st.Active.Kind != store.SlotKindPicker {
		t.Fatalf("expected picker slot to be enqueued, got %+v", st.Active)
	}
}

func TestSkillScopeMapping(t *testing.T) {
	tests := []struct {
		skill api.Skill
		want  string
	}{
		{skill: api.Skill{Namespace: "workspace"}, want: "Workspace"},
		{skill: api.Skill{Namespace: "local"}, want: "Project"},
		{skill: api.Skill{Namespace: "project"}, want: "Project"},
		{skill: api.Skill{Namespace: "user"}, want: "User"},
		{skill: api.Skill{Namespace: "system"}, want: "Builtin"},
		{skill: api.Skill{Namespace: "builtin"}, want: "Builtin"},
		{skill: api.Skill{Namespace: ""}, want: "Builtin"},
		{skill: api.Skill{Scope: "Workspace", Namespace: "system"}, want: "Workspace"},
		{skill: api.Skill{Namespace: "my-plugin"}, want: "My-plugin"},
	}

	for _, tt := range tests {
		got := SkillScope(tt.skill)
		if got != tt.want {
			t.Errorf("SkillScope(%+v) = %q, want %q", tt.skill, got, tt.want)
		}
	}
}

func TestSkillsBrowserEmptyAndPopulatedRendering(t *testing.T) {
	store.ClearSlot()
	defer store.ClearSlot()

	// 1. Empty state
	store.SetWorkspaceSkills([]api.Skill{})
	out := renderTestNode(t, SkillsBrowser("slot-1"))
	if !strings.Contains(out, "Skills (0 available)") || !strings.Contains(out, "No skills configured") {
		t.Fatalf("unexpected empty browser output:\n%s", out)
	}

	// 2. Populated state
	skills := []api.Skill{
		{
			Name:         "go-style",
			Description:  "Go coding idioms and style standards",
			Namespace:    "workspace",
			Instructions: "# Go Style Guide\n\nAlways handle errors.",
		},
		{
			Name:         "pr-review",
			Description:  "Pull request checklist",
			Namespace:    "local",
			Instructions: "Review all commits carefully.",
		},
		{
			Name:         "git-release",
			Description:  "Release helper",
			Namespace:    "system",
			Instructions: strings.Repeat("Release instructions step. ", 100),
		},
	}
	store.SetWorkspaceSkills(skills)

	out = renderTestNode(t, SkillsBrowser("slot-2"))

	if !strings.Contains(out, "Skills (3 available)") {
		t.Fatalf("expected title to show 3 available, got:\n%s", out)
	}
	if !strings.Contains(out, "go-style") || !strings.Contains(out, "pr-review") || !strings.Contains(out, "git-release") {
		t.Fatalf("expected all skill names in output, got:\n%s", out)
	}
	if !strings.Contains(out, "[Workspace]") || !strings.Contains(out, "[Project]") || !strings.Contains(out, "[Builtin]") {
		t.Fatalf("expected scope badges in output, got:\n%s", out)
	}
	if !strings.Contains(out, "Go coding idioms and style standards") {
		t.Fatalf("expected separate description column in output, got:\n%s", out)
	}
	// Verify that tokens and description are separated by space (not smashed together)
	if strings.Contains(out, "~10 tokGo") {
		t.Fatalf("expected space between token estimate and description, but got smashed text:\n%s", out)
	}
}

func TestSkillMarkdownViewerRendering(t *testing.T) {
	sk := api.Skill{
		Name:         "go-style",
		Description:  "Go coding idioms",
		Namespace:    "workspace",
		UseWhen:      "writing or reviewing Go code",
		ArgumentHint: "/go-style [pkg]",
		Instructions: "# Go Guidelines\n\n- Write unit tests\n- Wrap errors with fmt.Errorf",
	}

	out := renderTestNode(t, skillMarkdownViewer(skillMarkdownProps{
		slotID:  "slot-3",
		skill:   sk,
		onClose: func() {},
	}))

	if !strings.Contains(out, "Skill: go-style") {
		t.Fatalf("expected header to contain skill name, got:\n%s", out)
	}
	if !strings.Contains(out, "[Workspace]") {
		t.Fatalf("expected header to contain scope badge, got:\n%s", out)
	}
	if !strings.Contains(out, "When to use: writing or reviewing Go code") {
		t.Fatalf("expected header to contain UseWhen, got:\n%s", out)
	}
	if !strings.Contains(out, "Go Guidelines") || !strings.Contains(out, "Write unit tests") {
		t.Fatalf("expected markdown body to be rendered, got:\n%s", out)
	}
}

func TestRegisterSkillResourceDoesNotOverwriteBuiltinSkills(t *testing.T) {
	reg := NewRegistry()

	// Builtin /skills must exist
	cmd, ok := reg.Lookup("skills")
	if !ok || cmd.Name != "skills" {
		t.Fatal("expected built-in /skills command to exist")
	}

	// Registering a workspace skill named "skills" must not overwrite it
	reg.RegisterSkillResource(api.Skill{
		Name:        "skills",
		Description: "Custom workspace skill that shouldn't clobber builtin",
	})

	cmd, ok = reg.Lookup("skills")
	if !ok || cmd.Description != "List, inspect, and reload workspace skills" {
		t.Fatalf("expected built-in /skills to be preserved, got %+v", cmd)
	}
}

func TestSkillMarkdownViewerKeyboardScrolling(t *testing.T) {
	lines := make([]string, 50)
	for i := range lines {
		lines[i] = fmt.Sprintf("Line %02d: Some important runbook content", i+1)
	}
	sk := api.Skill{
		Name:         "long-runbook",
		Namespace:    "workspace",
		Instructions: strings.Join(lines, "\n\n"),
	}

	b := mock.New(100, 24)
	inst, err := trail.New(skillMarkdownViewer(skillMarkdownProps{
		slotID:  "slot-scroll",
		skill:   sk,
		onClose: func() {},
	}), trail.WithBackend(b))
	if err != nil {
		t.Fatalf("trail.New: %v", err)
	}
	defer inst.Unmount()
	inst.Frame()

	// Initial render
	initial := surfaceText(b)
	if !strings.Contains(initial, "Skill: long-runbook") {
		t.Fatalf("expected header in surface, got:\n%s", initial)
	}

	// 1. Scroll down 1 line with 'j'
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: 'j', Text: "j"}})
	inst.Frame()
	afterJ := surfaceText(b)
	if !strings.Contains(afterJ, "line +1") {
		t.Fatalf("expected 'line +1' after pressing j, got:\n%s", afterJ)
	}

	// 2. Scroll down 8 lines with pgdown
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyPgDown}})
	inst.Frame()
	afterPgDown := surfaceText(b)
	if !strings.Contains(afterPgDown, "line +9") {
		t.Fatalf("expected 'line +9' after pressing pgdown, got:\n%s", afterPgDown)
	}

	// 3. Scroll up 1 line with 'k'
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: 'k', Text: "k"}})
	inst.Frame()
	afterK := surfaceText(b)
	if !strings.Contains(afterK, "line +8") {
		t.Fatalf("expected 'line +8' after pressing k, got:\n%s", afterK)
	}

	// 4. Scroll to top with 'home'
	inst.Engine().ProcessRawEvent(&backend.RawKeyEvent{Key: key.Key{Code: key.KeyHome}})
	inst.Frame()
	afterHome := surfaceText(b)
	if strings.Contains(afterHome, "line +") {
		t.Fatalf("expected scroll indicator to disappear at top, got:\n%s", afterHome)
	}
}

func TestSkillsBrowserDescriptionTruncationToSingleLine(t *testing.T) {
	store.ClearSlot()
	defer store.ClearSlot()

	longDesc := "Expert guidance for developing applications and extending the Loom graph-based AI engine. Use it when you need to build stateful AI agents, implement directed graphs with conditional logic, manage conversation memory, or add support for new multimodal LLM providers."
	multilineDesc := "First line of the skill description.\nSecond line with additional instructions\nThird line that should not wrap."

	skills := []api.Skill{
		{
			Name:         "loom-dev",
			Description:  longDesc,
			Namespace:    "workspace",
			Instructions: "loom instructions",
		},
		{
			Name:         "multi-desc",
			Description:  multilineDesc,
			Namespace:    "local",
			Instructions: "multiline instructions",
		},
	}
	store.SetWorkspaceSkills(skills)

	// 1. In a wide terminal (130 cols), the truncated description and its ellipsis "…" are visible
	bWide := mock.New(130, 24)
	instWide, err := trail.New(SkillsBrowser("slot-trunc-wide"), trail.WithBackend(bWide))
	if err != nil {
		t.Fatalf("trail.New: %v", err)
	}
	defer instWide.Unmount()
	instWide.Frame()
	surfWide := surfaceText(bWide)
	if !strings.Contains(surfWide, "…") {
		t.Fatalf("expected ellipsis '…' in 130-col surface, got:\n%s", surfWide)
	}

	// 2. Multiline description should have newlines collapsed into spaces (no raw line breaks)
	if strings.Contains(surfWide, "First line of the skill description.\n") {
		t.Fatalf("expected multiline description to have newlines replaced by spaces, got:\n%s", surfWide)
	}

	// 3. Render in a standard 80x24 terminal and verify line count per row
	b := mock.New(80, 24)
	inst, err := trail.New(SkillsBrowser("slot-trunc-80"), trail.WithBackend(b))
	if err != nil {
		t.Fatalf("trail.New: %v", err)
	}
	defer inst.Unmount()
	inst.Frame()
	surf := surfaceText(b)

	// Ensure each skill name appears on its own line and neither wraps onto subsequent lines
	lines := strings.Split(surf, "\n")
	loomLineIdx := -1
	multiLineIdx := -1
	for idx, l := range lines {
		if strings.Contains(l, "loom-dev") {
			loomLineIdx = idx
		}
		if strings.Contains(l, "multi-desc") {
			multiLineIdx = idx
		}
	}
	if loomLineIdx == -1 || multiLineIdx == -1 {
		t.Fatalf("expected both skills in 80-col surface, got:\n%s", surf)
	}
	// In the table, adjacent options should be contiguous lines (row height 1)
	if multiLineIdx != loomLineIdx+1 {
		t.Fatalf("expected loom-dev and multi-desc to occupy consecutive single lines (row height 1), got line indices %d and %d:\n%s", loomLineIdx, multiLineIdx, surf)
	}
}
