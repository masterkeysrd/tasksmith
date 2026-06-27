package chat

import (
	"fmt"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/api"
	"github.com/masterkeysrd/tasksmith/internal/mcp"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
)

type McpRequestWidgetProps struct {
	Requests  []api.PendingMcpRequest
	OnResolve func(reqID string, action string, code string, content map[string]any)
}

var McpRequestWidget = kitex.FC("McpRequestWidget", func(props McpRequestWidgetProps) kitex.Node {
	if len(props.Requests) == 0 {
		return nil
	}

	var boxes []kitex.Node
	for _, req := range props.Requests {
		reqID := req.ID
		reqServer := req.ServerName
		reqType := req.Type
		reqURL := req.URL

		var msgText string
		if reqType == "oauth" {
			msgText = fmt.Sprintf("MCP Server %q needs browser authentication.", reqServer)
		} else {
			msgText = fmt.Sprintf("MCP %q: %s", reqServer, req.Message)
		}

		boxes = append(boxes, kitex.Box(kitex.BoxProps{
			Style: style.S().
				MarginBottom(1).
				Width(style.Percent(100)).
				MaxWidth(style.Percent(90)),
		},
			components.Alert(components.AlertProps{
				Severity: components.AlertWarning,
				Variant:  components.AlertOutlined,
				ShowIcon: true,
				Action: kitex.Box(kitex.BoxProps{
					Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1),
				},
					kitex.If(reqType == "oauth", func() kitex.Node {
						return components.Button(components.ButtonProps{
							Variant: components.ButtonSolid,
							Color:   components.ButtonPrimary,
							OnClick: func() {
								_ = mcp.OpenBrowser(reqURL)
							},
						}, kitex.Text("Open Link"))
					}),
					kitex.If(reqType == "elicitation", func() kitex.Node {
						return components.Button(components.ButtonProps{
							Variant: components.ButtonSolid,
							Color:   components.ButtonSuccess,
							OnClick: func() {
								props.OnResolve(reqID, "accept", "", nil)
							},
						}, kitex.Text("Accept"))
					}),
					components.Button(components.ButtonProps{
						Variant: components.ButtonText,
						Color:   components.ButtonBase,
						OnClick: func() {
							props.OnResolve(reqID, "cancel", "", nil)
						},
					}, kitex.Text("Cancel")),
				),
			}, kitex.Text(msgText)),
		))
	}

	return kitex.Box(kitex.BoxProps{
		Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).MarginTop(1).MarginBottom(1).AlignSelf(style.AlignStart).Width(style.Percent(100)),
	}, boxes...)
})
