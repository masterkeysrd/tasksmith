package statusline

import (
	"fmt"
	"image/color"
	"os/exec"
	"strings"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/tui/components/icon"
	"github.com/masterkeysrd/tasksmith/internal/tui/mode"
	"github.com/masterkeysrd/tasksmith/internal/tui/queries"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

// ModeProps defines properties for the Mode component.
type ModeProps struct {
	Style style.Style
}

// Mode renders the current input mode fragment.
var Mode = kitex.FC("Mode", func(props ModeProps) kitex.Node {
	t := theme.UseTheme()
	m := mode.Use()

	modeStr := m.String()

	var modeBg color.Color = color.Transparent
	colorTextDark := color.Color(color.Transparent)

	if t != nil {
		colorTextDark = t.Color.Text.InversePrimary
		switch m {
		case mode.Command:
			if val, ok := t.Palette["yellow"]; ok {
				modeBg = val
			} else {
				modeBg = t.Color.Surface.Tertiary
			}
		case mode.Insert:
			if val, ok := t.Palette["cyan"]; ok {
				modeBg = val
			} else {
				modeBg = t.Color.Surface.Primary
			}
		default:
			if val, ok := t.Palette["green"]; ok {
				modeBg = val
			} else {
				modeBg = t.Color.Surface.Success
			}
		}
	}

	modeStyle := style.S().
		Background(modeBg).
		Foreground(colorTextDark).
		Bold(true).
		PaddingHorizontal(2).
		Merge(props.Style)

	return kitex.Box(kitex.BoxProps{Style: modeStyle}, kitex.Text(modeStr))
})

// GitBranchProps defines properties for the GitBranch component.
type GitBranchProps struct {
	Style style.Style
}

// GitBranch renders the current Git branch fragment.
var GitBranch = kitex.FC("GitBranch", func(props GitBranchProps) kitex.Node {
	t := theme.UseTheme()

	gitBranch, setGitBranch := kitex.UseState("MAIN")
	kitex.UseEffect(func() {
		go func() {
			cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
			out, err := cmd.Output()
			if err == nil {
				branch := strings.TrimSpace(string(out))
				if branch != "" {
					setGitBranch(strings.ToUpper(branch))
				}
			}
		}()
	}, []any{})

	var borderPrimary, textMain color.Color = color.Transparent, color.Transparent
	if t != nil {
		borderPrimary = t.Color.Border.Primary
		textMain = t.Color.Text.Primary
	}

	gitStyle := style.S().
		Background(borderPrimary).
		Foreground(textMain).
		Bold(true).
		PaddingHorizontal(2).
		Display(style.DisplayFlex).
		FlexDirection(style.FlexRow).
		AlignItems(style.AlignCenter).
		Gap(1).
		Merge(props.Style)

	return kitex.Box(kitex.BoxProps{Style: gitStyle},
		kitex.Box(kitex.BoxProps{Style: style.S().Foreground(textMain)}, icon.GitBranch),
		kitex.Text(gitBranch()),
	)
})

// ProjectProps defines properties for the Project component.
type ProjectProps struct {
	ProjectName string
	Style       style.Style
}

// Project renders the workspace project name fragment.
var Project = kitex.FC("Project", func(props ProjectProps) kitex.Node {
	t := theme.UseTheme()
	wsCfg := queries.UseGetWorkspaceConfig()

	projectName := props.ProjectName
	if projectName == "" && wsCfg.Data != nil {
		projectName = wsCfg.Data.Name
	}

	if projectName == "" {
		return nil
	}

	var textDim color.Color = color.Transparent
	if t != nil {
		textDim = t.Color.Text.Secondary
	}

	projectStyle := style.S().
		Foreground(textDim).
		PaddingHorizontal(2).
		Merge(props.Style)

	return kitex.Box(kitex.BoxProps{Style: projectStyle},
		kitex.Text(strings.ToUpper(projectName)),
	)
})

// Spacer renders a flexible space fragment to separate left and right components.
var Spacer = kitex.SimpleFC("Spacer", func() kitex.Node {
	t := theme.UseTheme()
	bg := color.Color(color.Transparent)
	if t != nil {
		bg = t.Color.Surface.BaseHover
	}

	spacerStyle := style.S().
		Flex(1).
		Background(bg)

	return kitex.Box(kitex.BoxProps{Style: spacerStyle})
})

// StatsProps defines properties for the Stats component.
type StatsProps struct {
	Provider       string
	Model          string
	CurrentAgent   string
	ThinkingEffort string
	InputTokens    int
	OutputTokens   int
	Cost           float64
	Style          style.Style
}

// Stats renders the system and token statistics fragment.
var Stats = kitex.FC("Stats", func(props StatsProps) kitex.Node {
	t := theme.UseTheme()
	wsCfg := queries.UseGetWorkspaceConfig()

	// Resolve properties with fallbacks
	provider := props.Provider
	if provider == "" {
		if wsCfg.Data != nil && wsCfg.Data.DefaultProvider != "" {
			provider = wsCfg.Data.DefaultProvider
		} else {
			provider = "Loom_Engine"
		}
	}

	model := props.Model
	if model == "" {
		model = "Gemini 3.5 Flash"
	}

	currentAgent := props.CurrentAgent
	if currentAgent == "" {
		currentAgent = "Loom_Primary"
	}

	thinkingEffort := props.ThinkingEffort
	if thinkingEffort == "" {
		thinkingEffort = "high"
	}

	inputTokens := props.InputTokens
	if inputTokens == 0 {
		inputTokens = 25100
	}

	outputTokens := props.OutputTokens
	if outputTokens == 0 {
		outputTokens = 10400
	}

	cost := props.Cost
	if cost == 0.0 {
		cost = 0.021
	}

	var bg, textDim, textExtraDim, colorNormal, colorInsert, modelColor, colorInfo color.Color = color.Transparent, color.Transparent, color.Transparent, color.Transparent, color.Transparent, color.Transparent, color.Transparent

	if t != nil {
		bg = t.Color.Surface.BaseHover
		textDim = t.Color.Text.Secondary
		textExtraDim = t.Color.Text.Tertiary

		colorNormal = t.Color.Surface.Success
		colorInsert = t.Color.Surface.Primary
		modelColor = t.Color.Surface.Tertiary
		colorInfo = t.Color.Surface.Info

		if val, ok := t.Palette["green"]; ok {
			colorNormal = val
		}
		if val, ok := t.Palette["cyan"]; ok {
			colorInsert = val
		}
		if val, ok := t.Palette["orange"]; ok {
			modelColor = val
		}
		if val, ok := t.Palette["blue"]; ok {
			colorInfo = val
		}
	}

	var modelDisplayColor color.Color
	if strings.ToLower(thinkingEffort) == "off" {
		modelDisplayColor = colorInsert
	} else {
		modelDisplayColor = modelColor
	}

	statsStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexRow).
		AlignItems(style.AlignCenter).
		Gap(2).
		Background(bg).
		PaddingHorizontal(2).
		Merge(props.Style)

	magentaColor := color.Color(color.Transparent)
	if t != nil {
		magentaColor = t.Color.Text.Magenta
	}

	return kitex.Box(kitex.BoxProps{Style: statsStyle},
		// Provider
		kitex.Box(kitex.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow).
				AlignItems(style.AlignCenter).
				Gap(1).
				Foreground(magentaColor),
		},
			icon.Server,
			kitex.Text(provider),
		),

		// Model & Effort
		kitex.Box(kitex.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow).
				AlignItems(style.AlignCenter).
				Gap(1).
				Foreground(modelDisplayColor).
				Bold(strings.ToLower(thinkingEffort) != "off"),
		},
			icon.Cpu,
			kitex.Text(fmt.Sprintf("%s [%s]", model, strings.ToUpper(thinkingEffort))),
		),

		// Current Agent
		kitex.Box(kitex.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow).
				AlignItems(style.AlignCenter).
				Gap(1).
				Foreground(colorNormal),
		},
			icon.Robot,
			kitex.Text(currentAgent),
		),

		// Separator
		kitex.Box(kitex.BoxProps{Style: style.S().Foreground(textExtraDim)}, kitex.Text("│")),

		// Input tokens
		kitex.Box(kitex.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow).
				AlignItems(style.AlignCenter).
				Gap(1).
				Foreground(textDim),
		},
			kitex.Box(kitex.BoxProps{Style: style.S().Foreground(colorInfo)}, icon.MoveDown),
			kitex.Text(formatTokens(inputTokens)),
		),

		// Output tokens
		kitex.Box(kitex.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow).
				AlignItems(style.AlignCenter).
				Gap(1).
				Foreground(textDim),
		},
			kitex.Box(kitex.BoxProps{Style: style.S().Foreground(colorNormal)}, icon.MoveUp),
			kitex.Text(formatTokens(outputTokens)),
		),

		// Separator
		kitex.Box(kitex.BoxProps{Style: style.S().Foreground(textExtraDim)}, kitex.Text("│")),

		// Cost
		kitex.Box(kitex.BoxProps{Style: style.S().Foreground(colorNormal)},
			kitex.Text(fmt.Sprintf("$%.3f", cost)),
		),
	)
})

// StatusProps defines properties for the Status component.
type StatusProps struct {
	Status string
	Style  style.Style
}

// Status renders the agent run status fragment.
var Status = kitex.FC("Status", func(props StatusProps) kitex.Node {
	t := theme.UseTheme()

	status := props.Status
	if status == "" {
		status = "idle"
	}

	var colorNormal, colorInsert, colorWaiting, colorError, colorTextDark color.Color = color.Transparent, color.Transparent, color.Transparent, color.Transparent, color.Transparent

	if t != nil {
		colorNormal = t.Color.Surface.Success
		colorInsert = t.Color.Surface.Primary
		colorWaiting = t.Color.Surface.Tertiary
		colorError = t.Color.Surface.Error
		colorTextDark = t.Color.Text.InversePrimary

		if val, ok := t.Palette["green"]; ok {
			colorNormal = val
		}
		if val, ok := t.Palette["cyan"]; ok {
			colorInsert = val
		}
		if val, ok := t.Palette["yellow"]; ok {
			colorWaiting = val
		}
		if val, ok := t.Palette["red"]; ok {
			colorError = val
		}
	}

	var statusBg color.Color
	switch strings.ToLower(status) {
	case "running":
		statusBg = colorInsert
	case "waiting_approval", "waiting":
		statusBg = colorWaiting
	case "error", "failed":
		statusBg = colorError
	default:
		statusBg = colorNormal
	}

	statusStyle := style.S().
		Background(statusBg).
		Foreground(colorTextDark).
		Bold(true).
		PaddingHorizontal(2).
		Merge(props.Style)

	return kitex.Box(kitex.BoxProps{Style: statusStyle},
		kitex.Text(strings.ToUpper(strings.ReplaceAll(status, "_", " "))),
	)
})

// Props defines the properties for the main StatusLine container.
type Props struct {
	Style    style.Style
	Children []kitex.Node
}

// View is the primary StatusLine component that arranges layout fragments.
var View = kitex.FCC("StatusLine", func(props Props) kitex.Node {
	t := theme.UseTheme()

	bg := color.Color(color.Transparent)
	if t != nil {
		bg = t.Color.Surface.BaseHover
	}

	lineStyle := style.S().
		Width(style.Percent(100)).
		Height(style.Cells(1)).
		Display(style.DisplayFlex).
		FlexDirection(style.FlexRow).
		AlignItems(style.AlignCenter).
		Background(bg).
		Merge(props.Style)

	var children []kitex.Node
	if len(props.Children) > 0 {
		children = props.Children
	} else {
		// Default fragment configuration if no children are specified
		children = []kitex.Node{
			Mode(ModeProps{}),
			GitBranch(GitBranchProps{}),
			Spacer(),
			Stats(StatsProps{}),
			Status(StatusProps{}),
		}
	}

	return kitex.Box(kitex.BoxProps{Style: lineStyle}, children...)
})

func formatTokens(tokens int) string {
	if tokens >= 1000 {
		return fmt.Sprintf("%.1fK", float64(tokens)/1000.0)
	}
	return fmt.Sprintf("%d", tokens)
}
