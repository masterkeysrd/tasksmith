package components

import (
	"strings"
	"testing"

	"github.com/masterkeysrd/kite/element"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/testenv"
	"github.com/masterkeysrd/tasksmith/api"
)

func TestLabelFromValueRule(t *testing.T) {
	label, symKind := labelFromValue("rule", "go-error-handling")
	if label != "go-error-handling" {
		t.Errorf("label = %q, want %q", label, "go-error-handling")
	}
	if symKind != "" {
		t.Errorf("symKind = %q, want empty for rule refs", symKind)
	}
}

func TestKnownPrefixesIncludesRule(t *testing.T) {
	if got := knownPrefixes["@rule:"]; got != "rule" {
		t.Errorf("knownPrefixes[@rule:] = %q, want %q", got, "rule")
	}
}

func TestMarkdownRender_RulePill(t *testing.T) {
	const ruleName = "go-error-handling"

	env := renderMarkdownEnv(t, 80, 6, "Refactor the handler following @rule:"+ruleName)
	defer env.Close()

	lines := nonEmptyLines(env.DumpText())
	if len(lines) == 0 {
		t.Fatal("expected rendered markdown output")
	}

	idx := firstCellIndex(lines[0], ruleName)
	if idx < 0 {
		t.Fatalf("expected rule pill label %q in rendered line %q", ruleName, lines[0])
	}
	if strings.Contains(lines[0], "@rule:") {
		t.Errorf("raw @rule: prefix should be replaced by a pill, got %q", lines[0])
	}

	frame := env.Backend.LastFrame()
	if frame.Surface == nil {
		t.Fatal("expected a rendered framebuffer")
	}
	for offset := range len(ruleName) {
		cell := frame.Surface.CellAt(idx+offset, 0)
		if cell.Bg == nil {
			t.Fatalf("rule pill cell %d missing background styling", offset)
		}
	}
}

func TestMarkdownRender_StructuredAttachments(t *testing.T) {
	env := testenv.New(t, 80, 6)
	defer env.Close()
	container := element.NewBox(env.Document())
	env.Mount(container)

	kitex.Render(
		Markdown(MarkdownProps{
			Source: "Please check @composer.go, @rule:go-gate, @skill:release-runbook, and @mcp:notes:notes://spec-1.",
			Attachments: []api.Attachment{
				{
					Kind:   api.AttachmentFile,
					Ref:    "@composer.go",
					Target: "internal/cli/components/composer.go",
				},
				{
					Kind:   api.AttachmentRule,
					Ref:    "@rule:go-gate",
					Target: "go-gate",
				},
				{
					Kind:   api.AttachmentSkill,
					Ref:    "@skill:release-runbook",
					Target: "release-runbook",
				},
				{
					Kind:   api.AttachmentMCP,
					Ref:    "@mcp:notes:notes://spec-1",
					Target: "notes:notes://spec-1",
				},
			},
		}),
		container,
	)
	env.Flush()

	lines := nonEmptyLines(env.DumpText())
	if len(lines) == 0 {
		t.Fatal("expected rendered markdown output")
	}
	joined := strings.Join(lines, " ")

	// Should contain composer.go, go-gate, release-runbook, and notes:notes://spec-1 as pill labels
	for _, want := range []string{"composer.go", "go-gate", "release-runbook", "notes:notes://spec-1"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected %q in rendered output %q", want, joined)
		}
	}

	// Should not contain raw '@composer.go', '@skill:', or '@mcp:' because they are rendered as pills
	if strings.Contains(joined, "@composer.go") || strings.Contains(joined, "@skill:") || strings.Contains(joined, "@mcp:") {
		t.Errorf("raw @ tokens should be rendered as pills, got %q", joined)
	}
}
