package components

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	uicomponents "github.com/masterkeysrd/tasksmith/internal/ui/components"
	"github.com/masterkeysrd/tasksmith/internal/ui/styles"
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

var inlineTrustPrompt = trail.FC("InlineTrustPrompt", func(status api.WorkspaceStatus) trail.Node {
	sheet := styles.UseCommon()
	failure, setFailure := kitex.UseState("")
	return kitex.Fragment(
		kitex.If(failure() != "", func() trail.Node {
			return trail.Span(trail.SpanProps{Style: sheet.Error}, trail.Text(failure()))
		}),
		TrustPrompt(TrustPromptProps{
			Status: toTrustStatus(status),
			OnSelect: func(decision TrustDecision) {
				switch decision {
				case TrustDecisionFolder, TrustDecisionParent:
					scope := api.TrustScope(decision)
					if err := store.TrustWorkspace(context.Background(), store.Client(), scope, status); err != nil {
						setFailure("Could not trust workspace: " + err.Error())
					}
				case TrustDecisionRestricted:
					store.DismissWorkspaceTrust()
				case TrustDecisionExit:
					store.RequestExit()
				}
			},
		}),
	)
})

type trustPromptStyles struct {
	styles.Common
	section style.Style
}

func newTrustPromptStyles(s *theme.Scheme) trustPromptStyles {
	common := styles.NewCommon(s)
	return trustPromptStyles{
		Common:  common,
		section: common.Column.MarginBottom(1),
	}
}

// TrustPrompt renders the interactive repository trust gate matching section 4.1.
var TrustPrompt = trail.FC("TrustPrompt", func(props TrustPromptProps) trail.Node {
	sheet := styles.Use(newTrustPromptStyles)

	folderPath := props.Status.Path
	parentPath := filepath.Dir(folderPath)

	headerNodes := []trail.Node{
		trail.Box(trail.BoxProps{
			Style: sheet.section,
		},
			trail.Box(trail.BoxProps{
				Style: sheet.Row,
			},
				trail.Span(trail.SpanProps{
					Style: sheet.Title,
				}, trail.Text(folderPath)),
			),
		),
	}

	if props.Status.FingerprintChanged {
		headerNodes = append(headerNodes, trail.Span(trail.SpanProps{
			Style: sheet.Warning,
		}, trail.Text("Executable configuration changed since approval. Review it before trusting again.")))
	}

	execs := props.Status.Executables
	if len(execs.MCPServers) > 0 || len(execs.CustomTools) > 0 || len(execs.Decisions) > 0 || execs.HasEnv {
		var execLines []trail.Node
		execLines = append(execLines, trail.Span(trail.SpanProps{
			Style: sheet.Secondary,
		}, trail.Text("This project ships executable configuration:")))

		if len(execs.MCPServers) > 0 {
			label := fmt.Sprintf("  ▸ %d MCP server(s)  %s", len(execs.MCPServers), strings.Join(execs.MCPServers, ", "))
			execLines = append(execLines, trail.Span(trail.SpanProps{
				Style: sheet.Muted,
			}, trail.Text(label)))
		}
		if len(execs.CustomTools) > 0 {
			label := fmt.Sprintf("  ▸ %d custom tool(s) %s", len(execs.CustomTools), strings.Join(execs.CustomTools, ", "))
			execLines = append(execLines, trail.Span(trail.SpanProps{
				Style: sheet.Muted,
			}, trail.Text(label)))
		}
		if len(execs.Decisions) > 0 {
			label := fmt.Sprintf("  ▸ %d decision(s)   %s", len(execs.Decisions), strings.Join(execs.Decisions, ", "))
			execLines = append(execLines, trail.Span(trail.SpanProps{
				Style: sheet.Muted,
			}, trail.Text(label)))
		}
		if execs.HasEnv {
			execLines = append(execLines, trail.Span(trail.SpanProps{
				Style: sheet.Muted,
			}, trail.Text("  ▸ .env               loaded into tool environments")))
		}

		headerNodes = append(headerNodes, trail.Box(trail.BoxProps{
			Style: sheet.section,
		}, execLines...))
	} else {
		headerNodes = append(headerNodes, trail.Box(trail.BoxProps{
			Style: sheet.section,
		},
			trail.Span(trail.SpanProps{
				Style: sheet.Muted,
			}, trail.Text("No project tools or MCP servers found.")),
		))
	}

	header := trail.Box(trail.BoxProps{
		Style: sheet.Column,
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
