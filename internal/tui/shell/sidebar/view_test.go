package sidebar

import (
	"strings"
	"testing"

	"github.com/masterkeysrd/kite/element"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/testenv"
	"github.com/masterkeysrd/tasksmith/internal/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

func TestContentExplorerPanel(t *testing.T) {
	env := renderSidebar(t, Content(ContentProps{
		CurrentTab: TabExplorer,
		Data: Data{
			WorkspaceName:       "tasksmith",
			WorkspacePath:       "/Users/test/tasksmith",
			DefaultProvider:     "anthropic",
			IsConfigured:        true,
			Projects:            []api.Project{{Name: "core", DisplayName: "Core", Path: "/Users/test/tasksmith/core"}},
			AuthorizedTools:     map[string]bool{"bash": true, "view": true},
			ActiveSessionID:     "s1",
			ActiveSessionStatus: "idle",
		},
		ExpandedPaths: map[string]bool{},
	}))
	defer env.Close()

	screen := env.Document().TextContent()
	for _, want := range []string{"[EXP]", "tasksmith", "Core", "CHANGED FILES [SESSION]", "src", "CONTEXT:", "SYS: READY"} {
		if !strings.Contains(screen, want) {
			t.Fatalf("expected sidebar explorer output to contain %q, got:\n%s", want, screen)
		}
	}
	for _, wantMissing := range []string{"Workspace Root", "ANTHROPIC", "WORKSPACE PATHS"} {
		if strings.Contains(screen, wantMissing) {
			t.Fatalf("expected sidebar explorer output not to contain %q, got:\n%s", wantMissing, screen)
		}
	}
}

func TestContentSessionsPanel(t *testing.T) {
	env := renderSidebar(t, Content(ContentProps{
		CurrentTab: TabSessions,
		Data: Data{
			IsConfigured:        true,
			ActiveSessionID:     "session-1",
			ActiveSessionStatus: "running",
			Sessions: []api.Session{
				{ID: "session-1", Title: "New Chat", UpdatedAt: "2026-06-19T19:58:00Z"},
				{ID: "session-2", Title: "Fix Sidebar", UpdatedAt: "2026-06-19T20:00:00Z"},
			},
		},
		ExpandedPaths: map[string]bool{},
	}))
	defer env.Close()

	screen := env.Document().TextContent()
	for _, want := range []string{"Add New Session", "New Chat", "SESSION POWER OPERATIONS"} {
		if !strings.Contains(screen, want) {
			t.Fatalf("expected sidebar sessions output to contain %q, got:\n%s", want, screen)
		}
	}
}

func renderSidebar(t *testing.T, node kitex.Node) *testenv.Environment {
	t.Helper()

	env := testenv.Default(120, 40)
	container := element.NewBox(env.Document())
	env.Mount(container)
	kitex.Render(theme.Provider(theme.Props{Theme: &theme.Scheme{}}, node), container)
	env.Flush()
	return env
}
