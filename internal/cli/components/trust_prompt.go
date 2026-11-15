package components

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	uicomponents "github.com/masterkeysrd/tasksmith/internal/ui/components"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

// TrustDecision represents the user's selected trust action.
type TrustDecision string

const (
	TrustDecisionFolder     TrustDecision = "folder"
	TrustDecisionParent     TrustDecision = "parent"
	TrustDecisionRestricted TrustDecision = "restricted"
	TrustDecisionExit       TrustDecision = "exit"
)

// TrustPromptProps configures the interactive TrustPrompt component.
type TrustPromptProps struct {
	Status   api.TrustStatus
	OnSelect func(decision TrustDecision)
	OnCancel func()
}

// TrustPrompt renders the interactive repository trust gate matching section 4.1.
var TrustPrompt = trail.FC("TrustPrompt", func(props TrustPromptProps) trail.Node {
	scheme := theme.UseTheme()

	folderPath := props.Status.Path
	parentPath := filepath.Dir(folderPath)

	headerNodes := []trail.Node{
		trail.Box(trail.BoxProps{
			Style: style.S().FlexDirection(style.FlexColumn).MarginBottom(1),
		},
			trail.Box(trail.BoxProps{
				Style: style.S().FlexDirection(style.FlexRow).MarginBottom(0),
			},
				trail.Span(trail.SpanProps{
					Style: style.S().Bold(true).Foreground(scheme.Color.Text.Primary),
				}, trail.Text(folderPath)),
			),
		),
	}

	execs := props.Status.Executables
	if len(execs.MCPServers) > 0 || len(execs.CustomTools) > 0 || execs.HasEnv {
		var execLines []trail.Node
		execLines = append(execLines, trail.Span(trail.SpanProps{
			Style: style.S().Foreground(scheme.Color.Text.Secondary),
		}, trail.Text("This project ships executable configuration:")))

		if len(execs.MCPServers) > 0 {
			label := fmt.Sprintf("  ▸ %d MCP server(s)  %s", len(execs.MCPServers), strings.Join(execs.MCPServers, ", "))
			execLines = append(execLines, trail.Span(trail.SpanProps{
				Style: style.S().Foreground(scheme.Color.Text.Muted),
			}, trail.Text(label)))
		}
		if len(execs.CustomTools) > 0 {
			label := fmt.Sprintf("  ▸ %d custom tool(s) %s", len(execs.CustomTools), strings.Join(execs.CustomTools, ", "))
			execLines = append(execLines, trail.Span(trail.SpanProps{
				Style: style.S().Foreground(scheme.Color.Text.Muted),
			}, trail.Text(label)))
		}
		if execs.HasEnv {
			execLines = append(execLines, trail.Span(trail.SpanProps{
				Style: style.S().Foreground(scheme.Color.Text.Muted),
			}, trail.Text("  ▸ .env               loaded into tool environments")))
		}

		headerNodes = append(headerNodes, trail.Box(trail.BoxProps{
			Style: style.S().FlexDirection(style.FlexColumn).MarginBottom(1),
		}, execLines...))
	} else {
		headerNodes = append(headerNodes, trail.Box(trail.BoxProps{
			Style: style.S().FlexDirection(style.FlexColumn).MarginBottom(1),
		},
			trail.Span(trail.SpanProps{
				Style: style.S().Foreground(scheme.Color.Text.Muted),
			}, trail.Text("No project tools or MCP servers found.")),
		))
	}

	header := trail.Box(trail.BoxProps{
		Style: style.S().FlexDirection(style.FlexColumn),
	}, headerNodes...)

	options := []uicomponents.Option{
		{
			Value:       string(TrustDecisionFolder),
			Label:       "Trust this folder",
			Description: "Load project agents, tools, MCP servers and .env",
		},
		{
			Value:       string(TrustDecisionParent),
			Label:       fmt.Sprintf("Trust all folders in %s", parentPath),
			Description: "Remember trust for every repo under the parent directory",
		},
		{
			Value:       string(TrustDecisionRestricted),
			Label:       "Restricted mode",
			Description: "Read-only session. Project tools, MCP and .env are ignored",
		},
		{
			Value:       string(TrustDecisionExit),
			Label:       "Exit",
			Description: "Quit without running TaskSmith",
		},
	}

	pickerProps := PickerProps{
		Title:  "Do you trust the files in this folder?",
		Header: header,
		Bindings: []uicomponents.KeyBinding{
			{Key: "↑/↓", Label: "navigate"},
			{Key: "enter", Label: "select"},
			{Key: "esc", Label: "exit"},
		},
		Options: options,
		OnSelect: func(opt uicomponents.Option) {
			if props.OnSelect != nil {
				props.OnSelect(TrustDecision(opt.Value))
			}
		},
		OnCancel: func() {
			if props.OnCancel != nil {
				props.OnCancel()
			} else if props.OnSelect != nil {
				props.OnSelect(TrustDecisionExit)
			}
		},
	}

	return Picker(pickerProps)
})
