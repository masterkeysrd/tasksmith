package statusline

import (
	"testing"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
	"github.com/masterkeysrd/tasksmith/internal/tui/tokenutils"
)

func TestFormatTokens(t *testing.T) {
	tests := []struct {
		input    int
		expected string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1.0K"},
		{1500, "1.5K"},
		{999499, "999.5K"},
		{1000000, "1.0M"},
		{1500000, "1.5M"},
		{999499999, "999.5M"},
		{1000000000, "1.0B"},
		{1500000000, "1.5B"},
		{1232200, "1.2M"},
	}

	for _, tt := range tests {
		result := tokenutils.FormatTokens(tt.input)
		if result != tt.expected {
			t.Errorf("formatTokens(%d) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

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
			GitBranch(GitBranchProps{Branch: "MAIN"}),
			Spacer(),
			Project(ProjectProps{ProjectName: "test-project"}),
			Spacer(),
			Provider(ProviderProps{Provider: "Loom_Engine"}),
			Model(ModelProps{Model: "Gemini 3.1 Pro", ThinkingEffort: "medium"}),
			Agent(AgentProps{Agent: "Loom_Primary"}),
			Stats(StatsProps{
				InputTokens:  25100,
				OutputTokens: 10400,
				Cost:         0.021,
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
