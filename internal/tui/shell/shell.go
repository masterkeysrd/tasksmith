package shell

import (
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/tui/queries"
	"github.com/masterkeysrd/tasksmith/internal/tui/shell/commandbar"
	"github.com/masterkeysrd/tasksmith/internal/tui/shell/sidebar"
	"github.com/masterkeysrd/tasksmith/internal/tui/shell/statusline"
	"github.com/masterkeysrd/tasksmith/internal/tui/shell/titlebar"
)

// Props defines the properties for the Shell component.
type Props struct {
	// Children is the single content view rendered inside the shell.
	Children []kitex.Node
}

var (
	shellStyle = style.S().
			Width(style.Percent(100)).
			Height(style.Percent(100)).
			Display(style.DisplayFlex).
			FlexDirection(style.FlexColumn)

	contentStyle = style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexRow).
			Flex(1, 1, style.Cells(0)).
			MinHeight(style.Cells(0))

	mainPaneStyle = style.S().
			Width(style.Percent(100)).
			Display(style.DisplayFlex).
			FlexDirection(style.FlexColumn).
			Flex(1, 1, style.Cells(0)).
			MinWidth(style.Cells(0)).
			MinHeight(style.Cells(0))
)

// View is the Shell component. It renders the persistent chrome (title bar,
// future sidebar, status bar) around the active workspace view.
var View = kitex.FCC("Shell", func(props Props) kitex.Node {
	wsCfg := queries.UseGetWorkspaceConfig()
	isSidebarOpen, setIsSidebarOpen := kitex.UseState(true)

	workspaceName := ""
	if wsCfg.Data != nil {
		workspaceName = wsCfg.Data.Name
	}

	return kitex.Box(kitex.BoxProps{Style: shellStyle},
		titlebar.View(titlebar.Props{
			WorkspaceName:   workspaceName,
			IsSidebarOpen:   isSidebarOpen(),
			OnToggleSidebar: func() { setIsSidebarOpen(!isSidebarOpen()) },
		}),
		kitex.Box(kitex.BoxProps{Style: contentStyle},
			kitex.If(isSidebarOpen(), func() kitex.Node {
				return sidebar.View(sidebar.Props{})
			}),
			kitex.Box(kitex.BoxProps{Style: mainPaneStyle}, props.Children...),
		),
		statusline.View(statusline.Props{}),
		commandbar.View(commandbar.Props{}),
	)
})
