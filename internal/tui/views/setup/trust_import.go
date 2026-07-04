package setup

import (
	"context"
	"fmt"
	"sort"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/promise"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/api"
	tuiapi "github.com/masterkeysrd/tasksmith/internal/tui/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

// TrustImportView displays a trust verification gate when WORKSPACE.md exists
// but the workspace is not yet locally trusted.
type TrustImportProps struct {
	OnComplete func()
	OnWizard   func()
	OnSkip     func()
	OnExit     func()
	OnDecline  func()
	Config     *api.GetWorkspaceConfigResponse
}

var TrustImportView = kitex.FC("TrustImportView", func(props TrustImportProps) kitex.Node {
	t := theme.UseTheme()
	client := tuiapi.UseClient()

	primary := style.S().Foreground(t.Color.Surface.Primary)
	success := style.S().Foreground(t.Color.Surface.Success)
	accent := style.S().Foreground(t.Color.Surface.Tertiary)
	danger := style.S().Foreground(t.Color.Surface.Error)

	// Collect requested tool listing
	var tools []string
	if props.Config.AuthorizedTools != nil {
		for tool, authorized := range props.Config.AuthorizedTools {
			if authorized {
				tools = append(tools, tool)
			}
		}
	}
	sort.Strings(tools)

	// Theme toggle setup for consistency
	currentTheme := theme.UseName()
	themeLabel := "TOKIO NIGHT"
	switch currentTheme {
	case "solarized":
		themeLabel = "SOLARIZED"
	case "github-dark":
		themeLabel = "GITHUB DARK"
	}

	nextTheme := "default"
	switch currentTheme {
	case "default", "tokyo-night":
		nextTheme = "solarized"
	case "solarized":
		nextTheme = "github-dark"
	case "github-dark":
		nextTheme = "default"
	}

	themeButton := components.Button(components.ButtonProps{
		Key:     "header_theme_toggle",
		Variant: components.ButtonTonal,
		Color:   components.ButtonInfo,
		Style:   style.S().PaddingHorizontal(1).Bold(true),
		OnClick: func() {
			_ = theme.Set(nextTheme)
		},
	}, kitex.Text(themeLabel))

	themeGroup := kitex.Box(kitex.BoxProps{
		Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1).AlignItems(style.AlignCenter),
	},
		kitex.Box(kitex.BoxProps{
			Style: style.S().Foreground(t.Color.Text.Tertiary),
		}, kitex.Text("THEME:")),
		themeButton,
	)

	return components.Paper(components.PaperProps{
		Color: components.PaperBase,
		Style: RootStyle,
	},
		components.Card(components.CardProps{
			Color:   components.PaperSurface,
			Variant: components.CardOutlined,
			Style:   ContainerStyle,
		},
			// Card Header
			components.CardHeader(components.CardHeaderProps{
				Style: style.S().Padding(1),
				Title: kitex.Box(kitex.BoxProps{Style: danger.Bold(true)},
					kitex.Text("[!] WORKSPACE_TRUST_VERIFICATION"),
				),
				Action: kitex.Box(kitex.BoxProps{
					Style: ActionsContainerStyle.AlignItems(style.AlignCenter).Gap(2),
				},
					themeGroup,
					kitex.Box(kitex.BoxProps{
						Style: style.S().Foreground(t.Color.Border.Primary),
					}, kitex.Text("|")),
					components.Button(components.ButtonProps{
						Key:     "header_close",
						Variant: components.ButtonText,
						Style:   CloseButtonStyle.Merge(danger).Bold(true),
						OnClick: props.OnSkip,
					}, kitex.Text("[X]")),
				),
			}),

			// Main Content Block
			components.CardContent(components.CardContentProps{Style: ContentStyle},
				kitex.Box(kitex.BoxProps{
					Style: TabPanelStyle.Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Gap(1),
				},
					// Intro message
					kitex.Box(kitex.BoxProps{
						Style: style.S().Foreground(t.Color.Text.Primary),
					},
						kitex.Text("TaskSmith detected an existing configuration file (WORKSPACE.md)."),
					),

					// Repository specs details panel
					components.Paper(components.PaperProps{
						Color: components.PaperFooter,
						Style: style.S().
							PaddingVertical(1).
							PaddingHorizontal(2).
							Display(style.DisplayFlex).
							FlexDirection(style.FlexColumn).
							Gap(1),
					},
						// Workspace Row
						kitex.Box(kitex.BoxProps{
							Style: style.S().
								Display(style.DisplayFlex).
								JustifyContent(style.JustifyBetween),
						},
							kitex.Box(kitex.BoxProps{
								Style: primary.Bold(true),
							}, kitex.Text("  WORKSPACE:")),
							kitex.Text(props.Config.Name),
						),
						// Router Row
						kitex.Box(kitex.BoxProps{
							Style: style.S().
								Display(style.DisplayFlex).
								JustifyContent(style.JustifyBetween),
						},
							kitex.Box(kitex.BoxProps{
								Style: success.Bold(true),
							}, kitex.Text("  ROUTER:")),
							kitex.Text(props.Config.DefaultProvider),
						),
						// Tools Row
						kitex.Box(kitex.BoxProps{
							Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Gap(1),
						},
							kitex.Box(kitex.BoxProps{
								Style: accent.Bold(true),
							}, kitex.Text(fmt.Sprintf("  REQUESTED TOOLS (%d):", len(tools)))),
							kitex.If(len(tools) > 0, func() kitex.Node {
								return kitex.Box(kitex.BoxProps{
									Style: style.S().Display(style.DisplayFlex).FlexWrap(style.FlexWrapOn).Gap(1),
								},
									kitex.Map(tools, func(tool string, _ int) kitex.Node {
										return kitex.Box(kitex.BoxProps{
											Style: style.S().
												PaddingHorizontal(1).
												Background(t.Color.Surface.InfoFocus).
												Foreground(t.Color.Surface.Primary).
												Bold(true),
										},
											kitex.Text(tool),
										)
									}),
								)
							}),
							kitex.If(len(tools) == 0, func() kitex.Node {
								return kitex.Box(kitex.BoxProps{
									Style: style.S().Foreground(t.Color.Text.Tertiary).Italic(true),
								},
									kitex.Text("(No special tools requested)"),
								)
							}),
						),
					),

					// Security Alert Banner
					kitex.Box(kitex.BoxProps{
						Style: style.S().
							Background(t.Color.Surface.ErrorFocus).
							Foreground(t.Color.Surface.Error).
							PaddingVertical(1).
							PaddingHorizontal(2).
							Bold(true),
					},
						kitex.Text("[!] SECURITY ALERT: Only trust workspaces you verify. AI agents will run local commands."),
					),
				),
			),

			// Action Commands Footer
			components.CardActions(components.CardActionsProps{Style: FooterStyle.Background(t.Color.Surface.BaseDisabled)},
				// Left/Middle Actions
				kitex.Box(kitex.BoxProps{Style: FooterButtonGroupStyle},
					components.Button(components.ButtonProps{
						Key:        "trust_confirm",
						Variant:    components.ButtonText,
						Style:      style.S().Foreground(t.Color.Surface.Success).Bold(true),
						HoverStyle: style.S().Foreground(t.Color.Surface.SuccessHover),
						OnClick: func() {
							req := api.InitializeWorkspaceRequest{
								ProjectName:      props.Config.Name,
								SelectedProvider: props.Config.DefaultProvider,
								APIKey:           "",
								Theme:            theme.GetName(),
								AuthorizedTools:  props.Config.AuthorizedTools,
								TrustOnly:        true,
							}
							promise.New(func(ctx context.Context) (any, error) {
								return client.InitializeWorkspace(ctx, req)
							}).Then(func(any) {
								props.OnComplete()
							}, func(err error) {
								props.OnComplete()
							})
						},
					}, kitex.Text("[ TRUST & IMPORT ]")),

					components.Button(components.ButtonProps{
						Key:        "trust_override",
						Variant:    components.ButtonText,
						Style:      style.S().Foreground(t.Color.Surface.Primary).Bold(true),
						HoverStyle: style.S().Foreground(t.Color.Surface.PrimaryHover),
						OnClick:    props.OnWizard,
					}, kitex.Text("[ RUN SETUP WIZARD ]")),
				),

				// Right Actions
				kitex.Box(kitex.BoxProps{Style: FooterButtonGroupStyle},
					components.Button(components.ButtonProps{
						Key:        "trust_skip",
						Variant:    components.ButtonText,
						Style:      style.S().Foreground(t.Color.Text.Tertiary).Bold(true),
						HoverStyle: style.S().Foreground(t.Color.Text.Secondary),
						OnClick:    props.OnSkip,
					}, kitex.Text("[ SKIP SETUP ]")),

					components.Button(components.ButtonProps{
						Key:        "trust_decline",
						Variant:    components.ButtonText,
						Style:      style.S().Foreground(t.Color.Text.Magenta).Bold(true),
						HoverStyle: style.S().Foreground(t.Color.Surface.Primary),
						OnClick:    props.OnDecline,
					}, kitex.Text("[ DECLINE ]")),

					components.Button(components.ButtonProps{
						Key:        "trust_exit",
						Variant:    components.ButtonText,
						Style:      style.S().Foreground(t.Color.Surface.Error).Bold(true),
						HoverStyle: style.S().Foreground(t.Color.Surface.ErrorHover),
						OnClick:    props.OnExit,
					}, kitex.Text("[ EXIT ]")),
				),
			),
		),
	)
})
