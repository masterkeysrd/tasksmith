package statusline

import (
	"fmt"
	"image/color"
	"strings"
	"time"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/active"
	"github.com/masterkeysrd/tasksmith/internal/tui/components/icon"
	"github.com/masterkeysrd/tasksmith/internal/tui/mode"
	plugin "github.com/masterkeysrd/tasksmith/internal/tui/plugin/statusline"
	"github.com/masterkeysrd/tasksmith/internal/tui/queries"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
	"github.com/masterkeysrd/tasksmith/internal/tui/tokenutils"
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
	Branch string
	Style  style.Style
}

// GitBranch renders the current Git branch fragment.
var GitBranch = kitex.FC("GitBranch", func(props GitBranchProps) kitex.Node {
	t := theme.UseTheme()

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
		kitex.Text(props.Branch),
	)
})

// CommandProps defines properties for a custom shell command fragment.
type CommandProps struct {
	Output string
	Style  style.Style
}

// Command renders the output of a custom shell command.
var Command = kitex.FC("Command", func(props CommandProps) kitex.Node {
	t := theme.UseTheme()

	var borderPrimary, textMain color.Color = color.Transparent, color.Transparent
	if t != nil {
		borderPrimary = t.Color.Border.Primary
		textMain = t.Color.Text.Primary
	}

	cmdStyle := style.S().
		Background(borderPrimary).
		Foreground(textMain).
		Bold(true).
		PaddingHorizontal(2).
		Merge(props.Style)

	return kitex.Box(kitex.BoxProps{Style: cmdStyle},
		kitex.Text(props.Output),
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
		bg = t.Color.Surface.BaseFocus
	}

	spacerStyle := style.S().
		Flex(1).
		Background(bg)

	return kitex.Box(kitex.BoxProps{Style: spacerStyle})
})

// ProviderProps defines properties for the Provider component.
type ProviderProps struct {
	Provider string
	Style    style.Style
}

// Provider renders the current model provider.
var Provider = kitex.FC("Provider", func(props ProviderProps) kitex.Node {
	t := theme.UseTheme()
	magentaColor := color.Color(color.Transparent)
	if t != nil {
		magentaColor = t.Color.Text.Magenta
	}

	providerStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexRow).
		AlignItems(style.AlignCenter).
		Gap(1).
		Foreground(magentaColor).
		PaddingHorizontal(2).
		Merge(props.Style)

	return kitex.Box(kitex.BoxProps{Style: providerStyle},
		icon.Server,
		kitex.Text(fmt.Sprintf(" %s", props.Provider)),
	)
})

// ModelProps defines properties for the Model component.
type ModelProps struct {
	Model          string
	ThinkingEffort string
	Style          style.Style
}

// Model renders the active language model name and thinking effort.
var Model = kitex.FC("Model", func(props ModelProps) kitex.Node {
	t := theme.UseTheme()

	modelColor := color.Color(color.Transparent)
	colorInsert := color.Color(color.Transparent)
	if t != nil {
		colorInsert = t.Color.Surface.Primary
		modelColor = t.Color.Surface.Tertiary
		if val, ok := t.Palette["cyan"]; ok {
			colorInsert = val
		}
		if val, ok := t.Palette["orange"]; ok {
			modelColor = val
		}
	}

	var modelDisplayColor color.Color
	if strings.ToLower(props.ThinkingEffort) == "off" {
		modelDisplayColor = colorInsert
	} else {
		modelDisplayColor = modelColor
	}

	modelStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexRow).
		AlignItems(style.AlignCenter).
		Gap(1).
		Foreground(modelDisplayColor).
		Bold(strings.ToLower(props.ThinkingEffort) != "off").
		PaddingHorizontal(2).
		Merge(props.Style)

	return kitex.Box(kitex.BoxProps{Style: modelStyle},
		icon.CPU,
		kitex.Text(fmt.Sprintf(" %s [%s]", props.Model, strings.ToUpper(props.ThinkingEffort))),
	)
})

// AgentProps defines properties for the Agent component.
type AgentProps struct {
	Agent string
	Style style.Style
}

// Agent renders the active agent name.
var Agent = kitex.FC("Agent", func(props AgentProps) kitex.Node {
	t := theme.UseTheme()

	colorNormal := color.Color(color.Transparent)
	if t != nil {
		colorNormal = t.Color.Surface.Success
		if val, ok := t.Palette["green"]; ok {
			colorNormal = val
		}
	}

	agentStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexRow).
		AlignItems(style.AlignCenter).
		Gap(1).
		Foreground(colorNormal).
		PaddingHorizontal(2).
		Merge(props.Style)

	return kitex.Box(kitex.BoxProps{Style: agentStyle},
		icon.Robot,
		kitex.Text(fmt.Sprintf(" %s", props.Agent)),
	)
})

// StatsProps defines properties for the Stats component.
type StatsProps struct {
	InputTokens  int
	OutputTokens int
	Cost         float64
	Style        style.Style
}

// Stats renders token statistics and run cost.
var Stats = kitex.FC("Stats", func(props StatsProps) kitex.Node {
	t := theme.UseTheme()

	var bg, textDim, textExtraDim, colorNormal, colorInfo color.Color = color.Transparent, color.Transparent, color.Transparent, color.Transparent, color.Transparent

	if t != nil {
		bg = t.Color.Surface.BaseFocus
		textDim = t.Color.Text.Secondary
		textExtraDim = t.Color.Text.Tertiary
		colorNormal = t.Color.Surface.Success
		colorInfo = t.Color.Surface.Info

		if val, ok := t.Palette["green"]; ok {
			colorNormal = val
		}
		if val, ok := t.Palette["blue"]; ok {
			colorInfo = val
		}
	}

	statsStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexRow).
		AlignItems(style.AlignCenter).
		Gap(1).
		Background(bg).
		PaddingHorizontal(1).
		Merge(props.Style)

	return kitex.Box(kitex.BoxProps{Style: statsStyle},
		// Input tokens
		kitex.Box(kitex.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow).
				AlignItems(style.AlignCenter).
				Gap(1).
				Foreground(textDim),
		},
			kitex.Box(kitex.BoxProps{Style: style.S().Foreground(colorInfo)}, icon.MoveUp),
			kitex.Text(tokenutils.FormatTokens(props.InputTokens)),
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
			kitex.Box(kitex.BoxProps{Style: style.S().Foreground(colorNormal)}, icon.MoveDown),
			kitex.Text(tokenutils.FormatTokens(props.OutputTokens)),
		),

		// Separator
		kitex.Box(kitex.BoxProps{Style: style.S().Foreground(textExtraDim)}, kitex.Text("│")),

		// Cost
		kitex.Box(kitex.BoxProps{Style: style.S().Foreground(colorNormal)},
			kitex.Text(fmt.Sprintf("$%.3f", props.Cost)),
		),
	)
})

// StatusProps defines properties for the Status component.
type StatusProps struct {
	Status    string
	SessionID string
	Style     style.Style
}

// Status renders the agent run status fragment.
var Status = kitex.FC("Status", func(props StatusProps) kitex.Node {
	t := theme.UseTheme()

	status := props.Status
	if status == "" && props.SessionID != "" {
		stateQuery := queries.UseGetSessionState(props.SessionID)
		if stateQuery.Data != nil {
			status = stateQuery.Data.Status
		}
	}
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

// DiagnosticsProps defines properties for the Diagnostics component.
type DiagnosticsProps struct {
	Style style.Style
}

// Diagnostics renders the LSP diagnostic counts.
var Diagnostics = kitex.FC("Diagnostics", func(props DiagnosticsProps) kitex.Node {
	t := theme.UseTheme()
	query := queries.UseGetLspDiagnosticCounts()

	errors := 0
	warnings := 0
	infos := 0
	if query.Data != nil {
		errors = query.Data.Errors
		warnings = query.Data.Warnings
		infos = query.Data.Infos
	}

	var bg, textDim, colorError, colorWarn, colorInfo color.Color = color.Transparent, color.Transparent, color.Transparent, color.Transparent, color.Transparent

	if t != nil {
		bg = t.Color.Surface.BaseFocus
		textDim = t.Color.Text.Secondary
		colorError = t.Color.Text.Error
		colorWarn = t.Color.Text.Secondary
		colorInfo = t.Color.Text.Secondary

		if val, ok := t.Palette["red"]; ok {
			colorError = val
		}
		if val, ok := t.Palette["yellow"]; ok {
			colorWarn = val
		}
		if val, ok := t.Palette["cyan"]; ok {
			colorInfo = val
		}
	}

	diagStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexRow).
		AlignItems(style.AlignCenter).
		Gap(2).
		Background(bg).
		PaddingHorizontal(1).
		Merge(props.Style)

	var children []kitex.Node

	if errors > 0 {
		children = append(children, kitex.Box(kitex.BoxProps{
			Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1).AlignItems(style.AlignCenter).Foreground(textDim),
		}, kitex.Box(kitex.BoxProps{Style: style.S().Foreground(colorError)}, icon.Error), kitex.Text(fmt.Sprintf("%d", errors))))
	}

	if warnings > 0 {
		children = append(children, kitex.Box(kitex.BoxProps{
			Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1).AlignItems(style.AlignCenter).Foreground(textDim),
		}, kitex.Box(kitex.BoxProps{Style: style.S().Foreground(colorWarn)}, icon.Warning), kitex.Text(fmt.Sprintf("%d", warnings))))
	}

	if infos > 0 {
		children = append(children, kitex.Box(kitex.BoxProps{
			Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1).AlignItems(style.AlignCenter).Foreground(textDim),
		}, kitex.Box(kitex.BoxProps{Style: style.S().Foreground(colorInfo)}, icon.Info), kitex.Text(fmt.Sprintf("%d", infos))))
	}

	if len(children) == 0 {
		return nil
	}

	return kitex.Box(kitex.BoxProps{Style: diagStyle}, children...)
})

// Props defines the properties for the main StatusLine container.
type Props struct {
	Style    style.Style
	Children []kitex.Node
}

func renderFragment(f plugin.Fragment, state plugin.State, sessionID string, activeAgent, activeProvider, activeModel string, metrics *api.SessionMetrics) kitex.Node {
	inputTokens := 0
	outputTokens := 0
	costValue := 0.0
	thinkingEffort := "off"

	if metrics != nil {
		inputTokens = metrics.CumulativePromptTokens
		if inputTokens == 0 {
			inputTokens = metrics.PromptTokens
		}

		outputTokens = metrics.CumulativeCompletionTokens
		if outputTokens == 0 {
			outputTokens = metrics.CompletionTokens
		}

		if inputTokens == 0 && outputTokens == 0 {
			inputTokens = metrics.CumulativeTotalTokens
			if inputTokens == 0 {
				inputTokens = metrics.TotalTokens
			}
		}

		costValue = metrics.CumulativeCostUSD
		if costValue == 0 {
			costValue = metrics.EstimatedCostUSD
		}
	}

	switch f.Type {
	case "builtin":
		switch f.Name {
		case "mode":
			return Mode(ModeProps{})
		case "git_branch":
			return GitBranch(GitBranchProps{Branch: state.GitBranch})
		case "provider":
			return Provider(ProviderProps{Provider: activeProvider})
		case "model":
			return Model(ModelProps{Model: activeModel, ThinkingEffort: thinkingEffort})
		case "agent":
			return Agent(AgentProps{Agent: activeAgent})
		case "stats":
			return Stats(StatsProps{InputTokens: inputTokens, OutputTokens: outputTokens, Cost: costValue})
		case "status":
			return Status(StatusProps{SessionID: sessionID})
		case "diagnostics":
			return Diagnostics(DiagnosticsProps{})
		default:
			return nil
		}
	case "command":
		if f.Exec != "" {
			output := state.CommandOutputs[f.Exec]
			return Command(CommandProps{Output: output})
		}
		return nil
	default:
		return nil
	}
}

// View is the primary StatusLine component that arranges layout fragments.
var View = kitex.FCC("StatusLine", func(props Props) kitex.Node {
	t := theme.UseTheme()
	state := plugin.Use()
	wsCfg := queries.UseGetWorkspaceConfig()
	sessionsQuery := queries.UseListSessions()
	providersQuery := queries.UseListProviders()
	sessionID := active.UseSessionID()

	windClient := wind.UseClient()
	kitex.UseInterval(func() {
		windClient.InvalidateQueries(api.GetLspDiagnosticCountsRequest{})
	}, 2*time.Second, []any{windClient})

	if wsCfg.Data != nil && wsCfg.Data.CWD != "" {
		plugin.SetCWD(wsCfg.Data.CWD)
	}

	bg := color.Color(color.Transparent)
	if t != nil {
		bg = t.Color.Surface.BaseFocus
	}

	lineStyle := style.S().
		Width(style.Percent(100)).
		MinHeight(style.Cells(1)).
		Display(style.DisplayFlex).
		FlexDirection(style.FlexRow).
		AlignItems(style.AlignCenter).
		Background(bg).
		Merge(props.Style)

	// Resolve active agent, provider, and model from query cache
	agentName := ""
	providerName := ""
	modelName := ""
	var metrics *api.SessionMetrics

	if sessionsQuery.Data != nil && sessionID != "" {
		for _, s := range sessionsQuery.Data.Sessions {
			if s.ID == sessionID {
				agentName = s.AgentName
				providerName = s.ProviderName
				modelName = s.ModelName
				metrics = s.LastTurnMetrics
				break
			}
		}
	}

	// Resolve human-friendly provider and model labels
	providerLabel := providerName
	modelLabel := modelName

	if providersQuery.Data != nil {
		if providerName != "" {
			for _, p := range providersQuery.Data.Providers {
				if p.Name == providerName {
					if p.DisplayName != "" {
						providerLabel = p.DisplayName
					}
					break
				}
			}
		}
		if modelName != "" {
			foundModel := false
			for _, p := range providersQuery.Data.Providers {
				for _, m := range p.Models {
					if m.ID == modelName {
						if m.Label != "" {
							modelLabel = m.Label
						} else if m.Name != "" {
							modelLabel = m.Name
						}
						foundModel = true
						break
					}
				}
				if foundModel {
					break
				}
			}
		}
	}

	var children []kitex.Node
	if len(props.Children) > 0 {
		children = props.Children
	} else {
		var leftNodes []kitex.Node
		for _, f := range state.Config.Left {
			if node := renderFragment(f, state, sessionID, agentName, providerLabel, modelLabel, metrics); node != nil {
				leftNodes = append(leftNodes, node)
			}
		}

		var rightNodes []kitex.Node
		for _, f := range state.Config.Right {
			if node := renderFragment(f, state, sessionID, agentName, providerLabel, modelLabel, metrics); node != nil {
				rightNodes = append(rightNodes, node)
			}
		}

		children = append(children, leftNodes...)
		children = append(children, Spacer())

		// Wrap right-side components with a gap container for spacing between them
		var rightContainer kitex.Node
		if len(rightNodes) > 0 {
			rightContainer = kitex.Box(kitex.BoxProps{
				Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).AlignItems(style.AlignCenter).Gap(1),
			}, rightNodes...)
		}
		children = append(children, rightContainer)
	}

	return kitex.Box(kitex.BoxProps{Style: lineStyle}, children...)
})
