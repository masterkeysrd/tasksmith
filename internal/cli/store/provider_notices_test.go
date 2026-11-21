package store

import (
	"slices"
	"testing"

	"github.com/masterkeysrd/tasksmith/api"
)

func TestSkippedPresetAliasNotices(t *testing.T) {
	ClearTranscript()
	t.Cleanup(ClearTranscript)
	AppendSkippedPresetAliasNotices([]api.SkippedPresetAlias{
		{Alias: "@smart", ModelID: "new/reasoning", Owner: "existing/large"},
		{Alias: "@fast", ModelID: "new/small", Owner: "existing/small", WorkspaceOverride: true},
	})
	want := []string{
		"Skipped preset alias @smart for new/reasoning; it already points to existing/large. To move it, use the F2 alias editor in /model or /providers.",
		"Skipped preset alias @fast for new/small; WORKSPACE.md already points it to existing/small. Edit spec.modelAliases in WORKSPACE.md to change it.",
	}
	if got := transcriptWarnings(t); !slices.Equal(got, want) {
		t.Fatalf("warnings = %q, want %q", got, want)
	}
}
