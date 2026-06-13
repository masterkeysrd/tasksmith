package setup

import (
	"fmt"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
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
			MaxHeight(style.Cells(28))

	ContentStyle = style.S().
			Flex(1).
			Overflow(style.OverflowAuto)

	ActionsContainerStyle = style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow).
				JustifyContent(style.JustifyEnd).
				Gap(1)

	FooterStyle = style.S().
			Display(style.DisplayFlex).
			FlexWrap(style.FlexWrapOn).
			AlignItems(style.AlignCenter).
			JustifyContent(style.JustifyBetween).
			Gap(2)

	FooterButtonGroupStyle = style.S().
				Display(style.DisplayFlex).
				Gap(2)

	TabStyle = style.S().
			Flex(1).
			JustifyContent(style.JustifyCenter)

	CloseButtonStyle = style.S().
				Padding(0)
)

var View = kitex.SimpleFC("SetupView", func() kitex.Node {
	currentStep, setCurrentStep := kitex.UseState(1)

	return kitex.Box(kitex.BoxProps{
		Style: RootStyle,
	},
		components.Card(components.CardProps{
			Variant: components.CardOutlined,
			Style:   ContainerStyle,
		},
			Header(HeaderProps{
				Step:    currentStep(),
				OnClose: func() {},
			}),
			Content(ContentProps{
				Step: currentStep(),
			}),
			Footer(FooterProps{
				Step: currentStep(),
				OnNext: func() {
					setCurrentStep(min(currentStep()+1, 4))
				},
				OnBack: func() {
					setCurrentStep(max(currentStep()-1, 1))
				},
			}),
		),
	)
})

type HeaderProps struct {
	Step    int
	OnClose func()
}

func Header(props HeaderProps) kitex.Node {
	return components.CardHeader(components.CardHeaderProps{
		Title: kitex.Text("[*] TASKSMITH SETUP"),
		Action: kitex.Box(kitex.BoxProps{
			Style: ActionsContainerStyle,
		},
			kitex.Text(fmt.Sprintf("STEP: %d/4", props.Step)),
			components.Button(components.ButtonProps{
				Variant: components.ButtonText,
				Style:   CloseButtonStyle,
				OnClick: props.OnClose,
			},
				kitex.Text("[X]"),
			),
		),
	})
}

type ContentProps struct {
	Step int
}

func Content(props ContentProps) kitex.Node {
	return components.CardContent(components.CardContentProps{
		Style: ContentStyle,
	},
		components.Tabs(components.TabsProps{
			Value: props.Step,
			Style: style.S().Flex(1),
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
				Style: style.S().Flex(1),
			},
				WelcomeStep(),
			),
			components.TabPanel(components.TabPanelProps{Value: 2},
				kitex.Text("Content for Step 2"),
			),
			components.TabPanel(components.TabPanelProps{Value: 3},
				kitex.Text("Content for Step 3"),
			),
			components.TabPanel(components.TabPanelProps{Value: 4},
				kitex.Text("Content for Step 4"),
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
	return components.CardActions(components.CardActionsProps{},
		kitex.Box(kitex.BoxProps{
			Style: FooterStyle,
		},
			kitex.Box(kitex.BoxProps{
				Style: FooterButtonGroupStyle,
			},
				kitex.If(props.Step > 1, func() kitex.Node {
					return components.Button(components.ButtonProps{
						OnClick: props.OnBack,
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
						OnClick: props.OnNext,
					},
						kitex.Text("[ CONTINUE SETUP > ]"),
					)
				}),
				kitex.If(props.Step == 4, func() kitex.Node {
					return components.Button(components.ButtonProps{
						OnClick: props.OnConfirm,
					},
						kitex.Text("[ CONFIRM & TRUST WORKSPACE ]"),
					)
				}),
			),
			kitex.Box(kitex.BoxProps{
				Style: FooterButtonGroupStyle,
			},
				components.Button(components.ButtonProps{
					Variant: components.ButtonText,
					OnClick: props.OnSkip,
				},
					kitex.Text("[ SKIP SETUP ]"),
				),
				components.Button(components.ButtonProps{
					Variant: components.ButtonText,
					OnClick: props.OnDecline,
				},
					kitex.Text("[ DECLINE ]"),
				),
				components.Button(components.ButtonProps{
					Variant: components.ButtonText,
					OnClick: props.OnExit,
				},
					kitex.Text("[ EXIT ]"),
				),
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
	status := "[ ]"
	if props.Step == props.CurrentStep {
		status = fmt.Sprintf("[%d]", props.Step)
	}
	if props.Step < props.CurrentStep {
		status = "[x]"
	}
	return components.Tab(components.TabProps{
		Value: props.Step,
		Style: TabStyle,
	},
		kitex.Text(fmt.Sprintf("%s %s", status, props.Label)),
	)
}

func WelcomeStep() kitex.Node {
	return kitex.Box(kitex.BoxProps{
		Style: style.S().
			Display(style.DisplayFlex).
			Flex(1).
			FlexDirection(style.FlexColumn).
			JustifyContent(style.JustifyBetween).
			AlignItems(style.AlignCenter).
			Gap(1),
	},
		kitex.Box(kitex.BoxProps{},
			kitex.Text("WELCOME TO TASKSMITH CONSOLE!"),
			kitex.Text("This setup wizard will guide you through the initial configuration of Tasksmith Console. You can skip any step and configure it later in the settings."),
		),
		components.Alert(components.AlertProps{
			Severity: components.AlertInfo,
			Style:    style.S().TextAlign(style.TextAlignCenter),
		},
			kitex.Text("[!] Skipping this wizard allows you to run in ad-hoc mode without writing enviroment configurations on disk."),
		),
	)
}
