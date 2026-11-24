package components

import (
	"strings"
	"testing"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/testutils"
)

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
