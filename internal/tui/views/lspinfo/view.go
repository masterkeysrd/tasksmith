package lspinfo

import (
	"context"
	"strings"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/active"
	tuiapi "github.com/masterkeysrd/tasksmith/internal/tui/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/components/icon"
	"github.com/masterkeysrd/tasksmith/internal/tui/queries"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

type ViewProps struct{}

var View = kitex.FC("LspInfoView", func(props ViewProps) kitex.Node {
	isOpen := active.UseModal() == "lspinfo"
	if !isOpen {
		return nil
	}

	t := theme.UseTheme()
	client := tuiapi.UseClient()
	statusQuery := queries.UseGetLspStatus()
	isRestarting, setRestarting := kitex.UseState(false)

	var content kitex.Node

	if statusQuery.IsLoading {
		content = kitex.Box(kitex.BoxProps{
			Style: style.S().Padding(2).Foreground(t.Color.Text.Tertiary),
		}, kitex.Text("Loading LSP info..."))
	} else if statusQuery.Error != nil {
		content = components.Alert(components.AlertProps{
			Severity: components.AlertError,
			Children: []kitex.Node{
				kitex.Text(statusQuery.Error.Error()),
			},
		})
	} else if statusQuery.Data != nil {
		var listItems []kitex.Node

		for _, server := range statusQuery.Data.Servers {
			statusStr := "Not Running"
			statusColor := t.Color.Text.Error
			statusIcon := icon.Alert
			if server.IsRunning {
				statusStr = "Running"
				statusColor = t.Color.Surface.Success
				statusIcon = icon.Checkmark
			}

			listItems = append(listItems, kitex.Box(kitex.BoxProps{
				Style: style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexColumn).
					Padding(0, 1).
					BorderLeft(true, style.SingleBorder(), statusColor).
					Background(t.Color.Surface.BaseHover),
			},
				kitex.Box(kitex.BoxProps{
					Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).AlignItems(style.AlignCenter).JustifyContent(style.JustifyBetween).Width(style.Percent(100)),
				},
					kitex.Box(kitex.BoxProps{
						Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1).AlignItems(style.AlignCenter),
					},
						kitex.Box(kitex.BoxProps{Style: style.S().Foreground(statusColor)}, statusIcon),
						kitex.Box(kitex.BoxProps{Style: style.S().Bold(true).Foreground(t.Color.Text.Primary)}, kitex.Text(server.Name)),
					),
					kitex.Box(kitex.BoxProps{
						Style: style.S().PaddingHorizontal(1).Background(statusColor).Foreground(t.Color.Surface.Base),
					}, kitex.Text(statusStr)),
				),
				kitex.Box(kitex.BoxProps{
					Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1),
				},
					kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary)}, kitex.Text("Command:")),
					kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Secondary)}, kitex.Text(strings.Join(server.Command, " "))),
				),
				kitex.Box(kitex.BoxProps{
					Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1),
				},
					kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary)}, kitex.Text("Filetypes:")),
					kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Secondary)}, kitex.Text(strings.Join(server.FileTypes, ", "))),
				),
			))
		}

		if len(listItems) == 0 {
			content = kitex.Box(kitex.BoxProps{
				Style: style.S().Padding(2).Foreground(t.Color.Text.Tertiary),
			}, kitex.Text("No LSP servers configured."))
		} else {
			content = kitex.Box(kitex.BoxProps{
				Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Gap(1),
			}, listItems...)
		}
	}

	footer := kitex.Box(kitex.BoxProps{
		Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1).JustifyContent(style.JustifyEnd).MarginTop(1),
	},
		components.Button(components.ButtonProps{
			Variant: components.ButtonSolid,
			Color:   components.ButtonPrimary,
			OnClick: func() {
				setRestarting(true)
				go func() {
					_, _ = client.RestartLsp(context.Background(), api.RestartLspRequest{})
					kitex.PostMacro(func() {
						setRestarting(false)
					})
				}()
			},
			Disabled: isRestarting(),
		}, kitex.Text("Restart All")),
		components.Button(components.ButtonProps{
			Variant: components.ButtonText,
			OnClick: func() {
				active.SetModal("")
			},
		}, kitex.Text("Close")),
	)

	return components.Modal(components.ModalProps{
		IsOpen: true,
		Title:  kitex.Text("LSP Info"),
		OnClose: func() {
			active.SetModal("")
		},
	},
		kitex.Box(kitex.BoxProps{
			Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Height(style.Percent(100)),
		},
			kitex.Box(kitex.BoxProps{
				Style: style.S().Flex(1, 1, style.Cells(0)).OverflowY(style.OverflowAuto),
			}, content),
			footer,
		),
	)
})

