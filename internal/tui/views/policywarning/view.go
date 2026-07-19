package policywarning

import (
	"context"
	"fmt"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/promise"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/active"
	tuiapi "github.com/masterkeysrd/tasksmith/internal/tui/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
	"github.com/masterkeysrd/tasksmith/internal/tui/toast"
)

type ViewProps struct{}

// View is the policy authorization warning dialog using the shared ConfirmDialog component.
var View = kitex.FC("PolicyWarningView", func(props ViewProps) kitex.Node {
	isOpen := active.UseModal() == "policywarning"
	if !isOpen {
		return nil
	}

	command, tools := active.GetPendingWorkflow()
	t := theme.UseTheme()
	client := tuiapi.UseClient()

	if t == nil {
		return nil
	}

	handleAuthorize := func() {
		promise.New(func(ctx context.Context) (any, error) {
			_, err := client.AuthorizeWorkspaceTools(ctx, api.AuthorizeWorkspaceToolsRequest{
				Tools: tools,
			})
			return nil, err
		}).Then(func(any) {
			sessionID := active.GetSessionID()
			if sessionID != "" {
				promise.New(func(ctx context.Context) (any, error) {
					_, err := client.SendMessage(ctx, api.SendMessageRequest{
						SessionID: sessionID,
						Text:      command,
					})
					return nil, err
				}).Then(func(any) {
					active.SetModal("")
					active.SetPendingWorkflow("", nil)
					if active.InvalidateSessionMessages != nil {
						active.InvalidateSessionMessages(sessionID)
					}
					if active.InvalidateSessionState != nil {
						active.InvalidateSessionState(sessionID)
					}
				}, func(err error) {
					toast.AddErrorMessage("Workflow Trigger Failed", err.Error())
					active.SetModal("")
					active.SetPendingWorkflow("", nil)
				})
			} else {
				active.SetModal("")
				active.SetPendingWorkflow("", nil)
			}
		}, func(err error) {
			toast.AddErrorMessage("Authorization Failed", err.Error())
		})
	}

	handleCancel := func() {
		active.SetModal("")
		active.SetPendingWorkflow("", nil)
	}

	var toolNodes []kitex.Node
	for _, tool := range tools {
		toolNodes = append(toolNodes, kitex.Box(kitex.BoxProps{
			Style: style.S().Foreground(t.Color.Text.Magenta).Bold(true).PaddingHorizontal(2),
		}, kitex.Text(fmt.Sprintf("• %s", tool))))
	}

	content := kitex.Box(kitex.BoxProps{
		Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Gap(1),
	},
		kitex.Box(kitex.BoxProps{
			Style: style.S().Foreground(t.Color.Text.Primary),
		}, kitex.Text(fmt.Sprintf("The workflow command '%s' requires tools that are not authorized in your WORKSPACE.md configuration.", command))),
		kitex.Box(kitex.BoxProps{
			Style: style.S().Foreground(t.Color.Text.Secondary).MarginTop(1),
		}, kitex.Text("Required Tools:")),
		kitex.Box(kitex.BoxProps{
			Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).MarginBottom(1),
		}, toolNodes...),
		kitex.Box(kitex.BoxProps{
			Style: style.S().Foreground(t.Color.Text.Primary),
		}, kitex.Text("Would you like to authorize these tools and append them to WORKSPACE.md?")),
	)

	return components.ConfirmDialog(components.ConfirmDialogProps{
		Title:        "󰌆 Tool Authorization Required",
		Content:      content,
		ConfirmLabel: "Authorize (Enter)",
		CancelLabel:  "Cancel (Esc)",
		ConfirmColor: components.ButtonPrimary,
		OnConfirm:    handleAuthorize,
		OnCancel:     handleCancel,
	})
})
