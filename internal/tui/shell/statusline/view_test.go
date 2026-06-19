package statusline

import (
	"testing"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

func TestStatusLine(t *testing.T) {
	thm := &theme.Scheme{}

	render := func(node kitex.Node) kitex.Node {
		return wind.Provider(wind.ProviderProps{Client: wind.NewClient()},
			theme.Provider(theme.Props{Theme: thm}, node),
		)
	}

	t.Run("DefaultLayout", func(t *testing.T) {
		node := render(View(Props{}))
		if node == nil {
			t.Fatal("StatusLine default layout returned nil node")
		}
	})

	t.Run("CustomFragments", func(t *testing.T) {
		node := render(View(Props{},
			Mode(ModeProps{}),
			GitBranch(GitBranchProps{}),
			Spacer(),
			Project(ProjectProps{ProjectName: "test-project"}),
			Spacer(),
			Stats(StatsProps{
				Provider:       "Loom_Engine",
				Model:          "Gemini 3.1 Pro",
				CurrentAgent:   "Loom_Primary",
				ThinkingEffort: "medium",
				InputTokens:    25100,
				OutputTokens:   10400,
				Cost:           0.021,
			}),
			Status(StatusProps{
				Status: "running",
			}),
		))
		if node == nil {
			t.Fatal("StatusLine with custom fragments returned nil node")
		}
	})
}
