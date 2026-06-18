package setup

import (
	"context"
	"fmt"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/promise"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/api"
	tuiapi "github.com/masterkeysrd/tasksmith/internal/tui/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/command"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/queries"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

var (
	RootStyle = style.S().
			Display(style.DisplayFlex).
			Flex(1).
			Padding(4).
			AlignItems(style.AlignCenter).
			JustifyContent(style.JustifyCenter).
			Width(style.Percent(100)).
			Height(style.Percent(100))

	ContainerStyle = style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexColumn).
			Width(style.Percent(100)).
			MaxWidth(style.Cells(120)).
			Height(style.Percent(100)).
			MaxHeight(style.Cells(32))

	ContentStyle = style.S().
			Flex(1, 1, style.Cells(0)).
			MinHeight(style.Cells(0))

	ActionsContainerStyle = style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow).
				JustifyContent(style.JustifyEnd).
				Gap(1)

	FooterStyle = style.S().
			Display(style.DisplayFlex).
			FlexWrap(style.FlexWrapOn).
			AlignItems(style.AlignEnd).
			JustifyContent(style.JustifyBetween).
			Padding(1).
			Gap(2)

	FooterButtonGroupStyle = style.S().
				Display(style.DisplayFlex).
				Gap(2)

	TabStyle = style.S().
			Flex(1).
			AlignItems(style.AlignCenter).
			TextAlign(style.TextAlignCenter).
			JustifyContent(style.JustifyCenter).
			Padding(1)

	StepContentStyle = style.S().
				Display(style.DisplayFlex).
				Flex(1).
				FlexDirection(style.FlexColumn).
				JustifyContent(style.JustifyBetween).
				Gap(1)

	TabPanelStyle = style.S().
			Flex(1, 1, style.Cells(0)).
			MinHeight(style.Cells(0)).
			Padding(1).
			Overflow(style.OverflowAuto)

	CloseButtonStyle = style.S().
				Padding(0).
				PaddingHorizontal(1)
)

var View = kitex.SimpleFC("SetupView", func() kitex.Node {
	client := tuiapi.UseClient()
	resp := queries.UseListProjects()
	currentStep, setCurrentStep := kitex.UseState(3)
	projectName, setProjectName := kitex.UseState("untrusted-local-repo")
	selectedProvider, setSelectedProvider := kitex.UseState("")
	authorizedTools, setAuthorizedTools := kitex.UseState(make(map[string]bool))
	configs, setConfigs := kitex.UseState(make(map[string]ProviderForm))
	isExited, setIsExited := kitex.UseState(false)
	isDeclined, setIsDeclined := kitex.UseState(false)

	_, quit := command.UseCommand("quit")

	kitex.UseEffect(func() {
		if !resp.IsLoading && len(resp.Data.Projects) > 0 {
			// Prefer the first project name if available
			setProjectName(resp.Data.Projects[0].Name)
		}
	}, []any{resp.IsLoading})

	if isExited() {
		return ExitedView(func() { setIsExited(false) }, func() { quit() })
	}

	if isDeclined() {
		return DeclinedView(func() { setIsDeclined(false) }, func() {})
	}

	return components.Paper(components.PaperProps{
		Color: components.PaperBase,
		Style: RootStyle,
	},
		components.Card(components.CardProps{
			Color:   components.PaperSurface,
			Variant: components.CardOutlined,
			Style:   ContainerStyle,
		},
			Header(HeaderProps{
				Step:    currentStep(),
				OnClose: func() { setIsExited(true) },
			}),
			Content(ContentProps{
				Step:                currentStep(),
				ProjectName:         projectName(),
				SetProjectName:      setProjectName,
				SelectedProvider:    selectedProvider,
				SetSelectedProvider: setSelectedProvider,
				AuthorizedTools:     authorizedTools,
				SetAuthorizedTools:  setAuthorizedTools,
				Configs:             configs,
				SetConfigs:          setConfigs,
			}),
			Footer(FooterProps{
				Step: currentStep(),
				OnNext: func() {
					setCurrentStep(min(currentStep()+1, 4))
				},
				OnBack: func() {
					setCurrentStep(max(currentStep()-1, 1))
				},
				OnConfirm: func() {
					conf := configs()[selectedProvider()]
					req := api.InitializeWorkspaceRequest{
						ProjectName:      projectName(),
						SelectedProvider: selectedProvider(),
						APIKey:           conf.APIKey,
						Endpoint:         conf.Endpoint,
						DefaultModel:     conf.DefaultModel,
						Theme:            theme.GetName(),
						AuthorizedTools:  authorizedTools(),
					}
					promise.New(func(ctx context.Context) (any, error) {
						return client.InitializeWorkspace(ctx, req)
					}).Then(
						func(any) {
							quit()
						},
						func(err error) {
							quit()
						},
					)
				},
				OnSkip: func() {
					// Skip setup
				},
				OnDecline: func() {
					setIsDeclined(true)
				},
				OnExit: func() {
					setIsExited(true)
				},
			}),
		),
	)
})

func ExitedView(onRelaunch func(), onExit func()) kitex.Node {
	t := theme.UseTheme()
	whiteColor := t.Color.Text.Primary
	if c, ok := t.Palette["white"]; ok {
		whiteColor = c
	}
	bodyStyle := style.S().Foreground(t.Color.Surface.Primary).MarginTop(1).TextAlign(style.TextAlignCenter)

	return components.Paper(components.PaperProps{
		Color: components.PaperBase,
		Style: RootStyle,
	},
		components.Card(components.CardProps{
			Color:   components.PaperSurface,
			Variant: components.CardOutlined,
			Style:   ContainerStyle.MaxHeight(style.Cells(16)).Border(style.SingleBorder().Color(t.Color.Text.Tertiary)),
		},
			components.CardContent(components.CardContentProps{
				Style: style.S().Flex(1).Display(style.DisplayFlex).FlexDirection(style.FlexColumn).AlignItems(style.AlignCenter).JustifyContent(style.JustifyCenter).Padding(2),
			},
				kitex.Box(kitex.BoxProps{
					Style: style.S().Foreground(whiteColor).Bold(true).MarginBottom(1),
				}, kitex.Text("[!] SESSION_TERMINATED")),
				kitex.Box(kitex.BoxProps{
					Style: style.S().Foreground(t.Color.Border.Primary),
				}, kitex.Text("PROCESS EXITED WITH STATUS CODE: 0")),
				kitex.Box(kitex.BoxProps{
					Style: bodyStyle,
				}, kitex.Text("The active interactive setup sequence was terminated by user command. Memory buffers have been safely unmapped and workspace boundary processes exited.")),
				kitex.Box(kitex.BoxProps{
					Style: bodyStyle,
				}, kitex.Text("You can now safely navigate away or close this session.")),
				kitex.Box(kitex.BoxProps{
					Style: style.S().Display(style.DisplayFlex).Gap(2).MarginTop(2),
				},
					components.Button(components.ButtonProps{
						Key:        "relaunch",
						Variant:    components.ButtonText,
						Style:      style.S().Foreground(t.Color.Surface.Primary).Bold(true),
						HoverStyle: style.S().Foreground(t.Color.Surface.PrimaryHover),
						OnClick:    onRelaunch,
					}, kitex.Text("[ RE-LAUNCH SYSTEM SETUP ]")),
					components.Button(components.ButtonProps{
						Key:        "exit_app",
						Variant:    components.ButtonText,
						Style:      style.S().Foreground(t.Color.Surface.Error).Bold(true),
						HoverStyle: style.S().Foreground(t.Color.Surface.ErrorHover),
						OnClick:    onExit,
					}, kitex.Text("[ EXIT APPLICATION ]")),
				),
			),
		),
	)
}

func DeclinedView(onReturn func(), onSkip func()) kitex.Node {
	t := theme.UseTheme()
	muted := style.S().Foreground(t.Color.Text.Tertiary)
	danger := style.S().Foreground(t.Color.Surface.Error)
	subtext := style.S().Foreground(t.Color.Text.Secondary)

	return components.Paper(components.PaperProps{
		Color: components.PaperBase,
		Style: RootStyle,
	},
		components.Card(components.CardProps{
			Color:   components.PaperSurface,
			Variant: components.CardOutlined,
			Style:   ContainerStyle.MaxHeight(style.Cells(16)).Border(style.SingleBorder().Color(t.Color.Surface.Error)),
		},
			components.CardContent(components.CardContentProps{
				Style: style.S().Flex(1).Display(style.DisplayFlex).FlexDirection(style.FlexColumn).AlignItems(style.AlignCenter).JustifyContent(style.JustifyCenter).Padding(2),
			},
				kitex.Box(kitex.BoxProps{
					Style: danger.Bold(true).MarginBottom(1),
				}, kitex.Text("[!] BOOT_SEQUENCE_DENIED")),
				kitex.Box(kitex.BoxProps{
					Style: muted,
				}, kitex.Text("STATE: VERIFICATION_HALTED")),
				kitex.Box(kitex.BoxProps{
					Style: subtext.MarginTop(1).TextAlign(style.TextAlignCenter),
				}, kitex.Text("Tasksmith suspended initialization or config deployment. Workspaces must either be authorized or skipped explicitly.")),
				kitex.Box(kitex.BoxProps{
					Style: style.S().Display(style.DisplayFlex).Gap(2).MarginTop(2),
				},
					components.Button(components.ButtonProps{
						Key:        "return",
						Variant:    components.ButtonText,
						Style:      style.S().Foreground(t.Color.Surface.Error).Bold(true),
						HoverStyle: style.S().Foreground(t.Color.Surface.Tertiary),
						OnClick:    onReturn,
					}, kitex.Text("[ < RETURN ]")),
					components.Button(components.ButtonProps{
						Key:        "skip_adhoc",
						Variant:    components.ButtonText,
						Style:      style.S().Foreground(t.Color.Text.Tertiary).Bold(true),
						HoverStyle: style.S().Foreground(t.Color.Text.Secondary),
						OnClick:    onSkip,
					}, kitex.Text("[ IGNORE & RUN AD-HOC ]")),
				),
			),
		),
	)
}

type HeaderProps struct {
	Step    int
	OnClose func()
}

func Header(props HeaderProps) kitex.Node {
	t := theme.UseTheme()

	success := style.S().Foreground(t.Color.Surface.Success)
	danger := style.S().Foreground(t.Color.Surface.Error)

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

	return components.CardHeader(components.CardHeaderProps{
		Style: style.S().Padding(1),
		Title: kitex.Box(kitex.BoxProps{
			Style: success.Bold(true),
		}, kitex.Text("[*] TASKSMITH_INTERACTIVE_PROXIER")),
		Action: kitex.Box(kitex.BoxProps{
			Style: ActionsContainerStyle.AlignItems(style.AlignCenter).Gap(2),
		},
			themeGroup,
			kitex.Box(kitex.BoxProps{
				Style: style.S().Foreground(t.Color.Border.Primary),
			}, kitex.Text("|")),
			kitex.Box(kitex.BoxProps{
				Style: success,
			}, kitex.Text(fmt.Sprintf("STEP: %d/4", props.Step))),
			components.Button(components.ButtonProps{
				Key:     "header_close",
				Variant: components.ButtonText,
				Style:   CloseButtonStyle.Merge(danger).Bold(true),
				OnClick: props.OnClose,
			},
				kitex.Text("[X]"),
			),
		),
	})
}

type ContentProps struct {
	Step                int
	ProjectName         string
	SetProjectName      func(string)
	SelectedProvider    func() string
	SetSelectedProvider func(string)
	AuthorizedTools     func() map[string]bool
	SetAuthorizedTools  func(map[string]bool)
	Configs             func() map[string]ProviderForm
	SetConfigs          func(map[string]ProviderForm)
}

func Content(props ContentProps) kitex.Node {
	t := theme.UseTheme()
	muted := style.S().Foreground(t.Color.Text.Tertiary)

	return components.CardContent(components.CardContentProps{
		Style: ContentStyle,
	},
		components.Tabs(components.TabsProps{
			Value: props.Step,
			Style: style.S().Flex(1),
			Color: components.PaperContent,
			Separator: kitex.Span(kitex.SpanProps{
				Style: muted,
			}, kitex.Text("|")),
		},
			Tab(TabProps{
				Step:        1,
				Label:       "WELCOME",
				CurrentStep: props.Step,
			}),
			Tab(TabProps{
				Step:        2,
				Label:       "PROVIDERS",
				CurrentStep: props.Step,
			}),
			Tab(TabProps{
				Step:        3,
				Label:       "TOOLS",
				CurrentStep: props.Step,
			}),
			Tab(TabProps{
				Step:        4,
				Label:       "CONFIRM",
				CurrentStep: props.Step,
			}),
			components.TabPanel(components.TabPanelProps{
				Value: 1,
				Style: TabPanelStyle,
			},
				WelcomeStep(),
			),
			components.TabPanel(components.TabPanelProps{
				Value: 2,
				Style: TabPanelStyle,
			},
				ProviderStep(ProviderStepProps{
					SelectedProvider:    props.SelectedProvider(),
					SetSelectedProvider: props.SetSelectedProvider,
					Configs:             props.Configs(),
					SetConfigs:          props.SetConfigs,
				}),
			),
			components.TabPanel(components.TabPanelProps{
				Value: 3,
				Style: TabPanelStyle,
			},
				ToolsStep(ToolsStepProps{
					AuthorizedTools:    props.AuthorizedTools(),
					SetAuthorizedTools: props.SetAuthorizedTools,
				}),
			),
			components.TabPanel(components.TabPanelProps{
				Value: 4,
				Style: TabPanelStyle,
			},
				ConfirmStep(ConfirmStepProps{
					ProjectName:      props.ProjectName,
					SelectedProvider: props.SelectedProvider(),
					AuthorizedTools:  props.AuthorizedTools(),
				}),
			),
		),
	)
}

type FooterProps struct {
	Step      int
	OnNext    func()
	OnBack    func()
	OnConfirm func()
	OnSkip    func()
	OnDecline func()
	OnExit    func()
}

func Footer(props FooterProps) kitex.Node {
	t := theme.UseTheme()

	return components.CardActions(components.CardActionsProps{
		Style: FooterStyle.Background(t.Color.Surface.BaseDisabled),
	},
		kitex.Box(kitex.BoxProps{
			Style: FooterButtonGroupStyle,
		},
			kitex.If(props.Step > 1, func() kitex.Node {
				return components.Button(components.ButtonProps{
					Key:        "footer_back",
					Variant:    components.ButtonText,
					Style:      style.S().Foreground(t.Color.Text.Secondary).Bold(true),
					HoverStyle: style.S().Foreground(t.Color.Text.Primary),
					OnClick:    props.OnBack,
				},
					kitex.Text("[ < BACK ]"),
				)
			}),
		),
		kitex.Box(kitex.BoxProps{
			Style: FooterButtonGroupStyle,
		},
			kitex.If(props.Step < 4, func() kitex.Node {
				return components.Button(components.ButtonProps{
					Key:        "footer_continue",
					Variant:    components.ButtonText,
					Style:      style.S().Foreground(t.Color.Surface.Primary).Bold(true),
					HoverStyle: style.S().Foreground(t.Color.Surface.PrimaryHover),
					OnClick:    props.OnNext,
				},
					kitex.Text("[ CONTINUE SETUP > ]"),
				)
			}),
			kitex.If(props.Step == 4, func() kitex.Node {
				return components.Button(components.ButtonProps{
					Key:        "footer_confirm",
					Variant:    components.ButtonText,
					Style:      style.S().Foreground(t.Color.Surface.Success).Bold(true),
					HoverStyle: style.S().Foreground(t.Color.Surface.SuccessHover),
					OnClick:    props.OnConfirm,
				},
					kitex.Text("[ CONFIRM & TRUST WORKSPACE ]"),
				)
			}),
		),
		kitex.Box(kitex.BoxProps{
			Style: FooterButtonGroupStyle,
		},
			components.Button(components.ButtonProps{
				Key:        "footer_skip",
				Variant:    components.ButtonText,
				Style:      style.S().Foreground(t.Color.Text.Tertiary).Bold(true),
				HoverStyle: style.S().Foreground(t.Color.Text.Secondary),
				OnClick:    props.OnSkip,
			},
				kitex.Text("[ SKIP SETUP ]"),
			),
			components.Button(components.ButtonProps{
				Key:        "footer_decline",
				Variant:    components.ButtonText,
				Style:      style.S().Foreground(t.Color.Text.Magenta).Bold(true),
				HoverStyle: style.S().Foreground(t.Color.Surface.Primary),
				OnClick:    props.OnDecline,
			},
				kitex.Text("[ DECLINE ]"),
			),
			components.Button(components.ButtonProps{
				Key:        "footer_exit",
				Variant:    components.ButtonText,
				Style:      style.S().Foreground(t.Color.Surface.Error).Bold(true),
				HoverStyle: style.S().Foreground(t.Color.Surface.ErrorHover),
				OnClick:    props.OnExit,
			},
				kitex.Text("[ EXIT ]"),
			),
		),
	)
}

type TabProps struct {
	Step        int
	Label       string
	CurrentStep int
}

func Tab(props TabProps) kitex.Node {
	t := theme.UseTheme()

	primary := style.S().Foreground(t.Color.Surface.Primary)
	success := style.S().Foreground(t.Color.Surface.Success)
	border := style.S().Foreground(t.Color.Border.Primary)

	status := "[ ]"
	hl := border
	if props.Step == props.CurrentStep {
		status = fmt.Sprintf("[%d]", props.Step)
		hl = primary
	}
	if props.Step < props.CurrentStep {
		status = "[X]"
		hl = success
	}

	return components.Tab(components.TabProps{
		Value: props.Step,
		Style: TabStyle.Merge(hl).Bold(true),
	},
		kitex.Text(fmt.Sprintf("%s %s", status, props.Label)),
	)
}
