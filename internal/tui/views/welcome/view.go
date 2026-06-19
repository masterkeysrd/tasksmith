package welcome

import (
	"fmt"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/command"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/components/icon"
	"github.com/masterkeysrd/tasksmith/internal/tui/queries"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

// ViewProps defines the properties for the Welcome view.
type ViewProps struct {
	OnOpenSetupWizard func()
	OnNewSession      func()
	OnOpenSession     func(sessionID string)
}

// Mock structures to match mockup.tsx expectations since they aren't fully represented in backend API yet.
type mockSession struct {
	ID   string
	Name string
}

var (
	RootStyle = style.S().
			Display(style.DisplayFlex).
			Flex(1).
			Padding(2).
			AlignItems(style.AlignCenter).
			JustifyContent(style.JustifyCenter).
			Width(style.Percent(100)).
			Height(style.Percent(100))

	ContainerStyle = style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexColumn).
			Width(style.Percent(100)).
			MaxWidth(style.Cells(110)).
			Height(style.Percent(100)).
			MaxHeight(style.Cells(38))

	HeaderSectionStyle = style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexColumn).
				AlignItems(style.AlignCenter).
				MarginBottom(3)

	TitleStyle = style.S().
			Bold(true).
			MarginBottom(1)

	SubtitleStyle = style.S().
			MarginBottom(2)

	BadgesRowStyle = style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexRow).
			Gap(3)

	BadgeStyle = style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexRow).
			Gap(1)

	GridContainerStyle = style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow).
				Gap(6).
				PaddingHorizontal(4)

	LeftColumnStyle = style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexColumn).
			Flex(1, 1, style.Cells(0))

	RightColumnStyle = style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexColumn).
				Flex(1, 1, style.Cells(0))

	ActionBoxContainerStyle = style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexColumn).
				MarginBottom(1)

	ActionBoxTitleStyle = style.S().
				Bold(true).
				MarginBottom(0)

	ActionBoxContentStyle = style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexColumn).
				Gap(0)

	ActionItemStyle = style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexRow).
			AlignItems(style.AlignCenter).
			JustifyContent(style.JustifyStart).
			Gap(1).
			PaddingHorizontal(1).
			PaddingVertical(0).
			Border(false)

	InfoSectionContainerStyle = style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexColumn).
					MarginBottom(1)

	InfoSectionTitleStyle = style.S().
				Bold(true).
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow).
				AlignItems(style.AlignCenter).
				Gap(2).
				MarginBottom(0)

	InfoSectionContentStyle = style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexColumn).
				PaddingLeft(0).
				Gap(0)

	InfoRowStyle = style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexRow).
			JustifyContent(style.JustifyStart).
			AlignItems(style.AlignCenter).
			Gap(2).
			PaddingHorizontal(1).
			Border(false)
)

// ActionItem represents a clickable menu item in the left actions column.
type ActionItemProps struct {
	Icon    kitex.Node
	Label   string
	OnClick func()
}

var ActionItem = kitex.FC("ActionItem", func(props ActionItemProps) kitex.Node {
	iconContainer := kitex.Box(kitex.BoxProps{
		Style: style.S().Width(style.Cells(2)).TextAlign(style.TextAlignCenter),
	}, props.Icon)

	return components.Button(components.ButtonProps{
		Variant:   components.ButtonText,
		Color:     components.ButtonBase,
		Style:     style.S().Width(style.Percent(100)).JustifyContent(style.JustifyStart).PaddingHorizontal(1),
		StartIcon: iconContainer,
		OnClick:   props.OnClick,
	},
		kitex.Text(props.Label),
	)
})

// ActionBox groups related ActionItems.
type ActionBoxProps struct {
	Title    string
	Children []kitex.Node
}

var ActionBox = kitex.FCC("ActionBox", func(props ActionBoxProps) kitex.Node {
	t := theme.UseTheme()

	headerStyle := ActionBoxTitleStyle.Foreground(t.Color.Text.Tertiary)

	return kitex.Box(kitex.BoxProps{Style: ActionBoxContainerStyle},
		kitex.Box(kitex.BoxProps{Style: headerStyle}, kitex.Text(props.Title)),
		kitex.Box(kitex.BoxProps{Style: ActionBoxContentStyle}, props.Children...),
	)
})

// InfoSection displays list data on the right column.
type InfoSectionProps struct {
	Icon     kitex.Node
	Title    string
	Count    *int
	Children []kitex.Node
}

var InfoSection = kitex.FCC("InfoSection", func(props InfoSectionProps) kitex.Node {
	t := theme.UseTheme()

	titleText := props.Title
	if props.Count != nil {
		titleText = fmt.Sprintf("%s (%d)", props.Title, *props.Count)
	}

	titleStyle := InfoSectionTitleStyle.Foreground(t.Color.Text.Primary)

	return kitex.Box(kitex.BoxProps{Style: InfoSectionContainerStyle},
		kitex.Box(kitex.BoxProps{Style: titleStyle},
			props.Icon,
			kitex.Text("  "+titleText),
		),
		kitex.Box(kitex.BoxProps{Style: InfoSectionContentStyle}, props.Children...),
	)
})

// SessionItem represents a clickable row in the recent sessions info block.
type SessionItemProps struct {
	Name    string
	Hours   string
	OnClick func()
}

var SessionItem = kitex.FC("SessionItem", func(props SessionItemProps) kitex.Node {
	t := theme.UseTheme()

	return components.Button(components.ButtonProps{
		Variant: components.ButtonText,
		Color:   components.ButtonBase,
		Style:   InfoRowStyle.Width(style.Percent(100)),
		OnClick: props.OnClick,
	},
		kitex.Text(props.Name),
		kitex.Box(kitex.BoxProps{
			Style: style.S().Foreground(t.Color.Text.Tertiary),
		}, kitex.Text(props.Hours)),
	)
})

// View is the main Welcome view component.
var View = kitex.FC("WelcomeView", func(props ViewProps) kitex.Node {
	t := theme.UseTheme()

	// Reactive data hooks
	projects := queries.UseListProjects()
	agents := queries.UseListAgents()
	providers := queries.UseListProviders()
	wsCfg := queries.UseGetWorkspaceConfig()

	// Action notification feedback state
	actionMessage, setActionMessage := kitex.UseState("")

	_, quit := command.UseCommand("quit")

	// Helper for toast actions
	triggerAction := func(name string) {
		setActionMessage(fmt.Sprintf("Action '%s' clicked (not yet implemented)", name))
	}

	// Dynamic text elements based on initialization state
	isInitialized := false
	if !wsCfg.IsLoading && wsCfg.Data != nil {
		isInitialized = wsCfg.Data.IsConfigured
	}

	initStatus := "initialized"
	if !isInitialized {
		initStatus = "unconfigured"
	}

	// Prepare data lengths
	var projectCount *int
	if !projects.IsLoading && projects.Data != nil {
		cnt := len(projects.Data.Projects)
		projectCount = &cnt
	}

	var agentCount *int
	if !agents.IsLoading && agents.Data != nil {
		cnt := len(agents.Data.Agents)
		agentCount = &cnt
	}

	// Mocking sessions
	mockSessions := []mockSession{
		{ID: "session-1", Name: "agentic-refactoring"},
		{ID: "session-2", Name: "tui-theme-debugging"},
	}

	return components.Paper(components.PaperProps{
		Color: components.PaperSurface,
		Style: RootStyle,
	},
		kitex.Box(kitex.BoxProps{
			Style: ContainerStyle,
		},
			kitex.Box(kitex.BoxProps{
				Style: style.S().Flex(1).PaddingVertical(2).Overflow(style.OverflowAuto),
			},
				// Header Section
				kitex.Box(kitex.BoxProps{Style: HeaderSectionStyle},
					kitex.Box(kitex.BoxProps{
						Style: TitleStyle.Foreground(t.Palette["white"]),
					}, kitex.Text("TaskSmith")),
					kitex.Box(kitex.BoxProps{
						Style: SubtitleStyle.Foreground(t.Color.Surface.Info),
					}, kitex.Text("Your AI-powered task automation assistant")),
					kitex.Box(kitex.BoxProps{Style: BadgesRowStyle},
						kitex.Box(kitex.BoxProps{
							Style: BadgeStyle.Foreground(t.Color.Surface.Info),
						},
							icon.Folder,
							kitex.Text("  tasksmith"),
						),
						kitex.Box(kitex.BoxProps{
							Style: BadgeStyle.Foreground(t.Color.Surface.Info),
						},
							kitex.If(isInitialized, func() kitex.Node { return icon.Checkmark }),
							kitex.If(!isInitialized, func() kitex.Node { return icon.Alert }),
							kitex.Text("  "+initStatus),
						),
					),
				),

				// Action message notification banner
				kitex.If(actionMessage() != "", func() kitex.Node {
					return components.Alert(components.AlertProps{
						Severity: components.AlertInfo,
						ShowIcon: true,
						Style:    style.S().MarginBottom(1).MarginHorizontal(4),
						Action: components.Button(components.ButtonProps{
							Variant: components.ButtonText,
							OnClick: func() { setActionMessage("") },
						}, kitex.Text("[X]")),
					}, kitex.Text(actionMessage()))
				}),

				// Grid Layout
				kitex.Box(kitex.BoxProps{Style: GridContainerStyle},
					// Left Column: Actions
					kitex.Box(kitex.BoxProps{Style: LeftColumnStyle},
						ActionBox(ActionBoxProps{Title: "Sessions"},
							ActionItem(ActionItemProps{Icon: icon.Calendar, Label: "New Session", OnClick: props.OnNewSession}),
						),
						ActionBox(ActionBoxProps{Title: "Agents & Skills"},
							ActionItem(ActionItemProps{Icon: icon.Robot, Label: "New Agent", OnClick: func() { triggerAction("New Agent") }}),
							ActionItem(ActionItemProps{Icon: icon.Pencil, Label: "New Skill", OnClick: func() { triggerAction("New Skill") }}),
						),
						ActionBox(ActionBoxProps{Title: "Tools & MCPs"},
							ActionItem(ActionItemProps{Icon: icon.Wrench, Label: "New Tool", OnClick: func() { triggerAction("New Tool") }}),
							ActionItem(ActionItemProps{Icon: icon.Package, Label: "New Toolkit", OnClick: func() { triggerAction("New Toolkit") }}),
							ActionItem(ActionItemProps{Icon: icon.Network, Label: "Register MCP", OnClick: func() { triggerAction("Register MCP") }}),
						),
						ActionBox(ActionBoxProps{Title: "Workspace"},
							ActionItem(ActionItemProps{Icon: icon.Folder, Label: "New Project", OnClick: func() { triggerAction("New Project") }}),
							ActionItem(ActionItemProps{Icon: icon.Terminal, Label: "New Command", OnClick: func() { triggerAction("New Command") }}),
							ActionItem(ActionItemProps{Icon: icon.Plugin, Label: "Install Plugin", OnClick: func() { triggerAction("Install Plugin") }}),
							kitex.If(props.OnOpenSetupWizard != nil, func() kitex.Node {
								return ActionItem(ActionItemProps{
									Icon:    icon.Cog,
									Label:   "Setup Wizard",
									OnClick: props.OnOpenSetupWizard,
								})
							}),
						),
						ActionBox(ActionBoxProps{Title: "Telemetry & Performance"},
							ActionItem(ActionItemProps{Icon: icon.Fire, Label: "CodeBurn Analytics", OnClick: func() { triggerAction("CodeBurn Analytics") }}),
						),
						ActionBox(ActionBoxProps{Title: "App"},
							ActionItem(ActionItemProps{Icon: icon.Exit, Label: "Quit", OnClick: func() { quit() }}),
						),
					),

					// Right Column: Info
					kitex.Box(kitex.BoxProps{Style: RightColumnStyle},
						// Projects
						InfoSection(InfoSectionProps{Icon: icon.Folder, Title: "Projects", Count: projectCount},
							kitex.If(!projects.IsLoading && projects.Data != nil && len(projects.Data.Projects) > 0, func() kitex.Node {
								limit := min(3, len(projects.Data.Projects))
								return kitex.Fragment(
									kitex.Map(projects.Data.Projects[:limit], func(p api.Project, _ int) kitex.Node {
										name := p.DisplayName
										if name == "" {
											name = p.Name
										}
										return kitex.Box(kitex.BoxProps{
											Style: style.S().Foreground(t.Color.Text.Secondary).PaddingLeft(1),
										}, kitex.Text(name))
									}),
								)
							}),
							kitex.If(projects.IsLoading || projects.Data == nil || len(projects.Data.Projects) == 0, func() kitex.Node {
								return kitex.Box(kitex.BoxProps{
									Style: style.S().Foreground(t.Color.Text.Secondary).PaddingLeft(1),
								}, kitex.Text("kite"))
							}),
						),

						// Providers
						InfoSection(InfoSectionProps{Icon: icon.Database, Title: "Providers"},
							kitex.If(!providers.IsLoading && providers.Data != nil && len(providers.Data.Providers) > 0, func() kitex.Node {
								limit := min(3, len(providers.Data.Providers))
								return kitex.Fragment(
									kitex.Map(providers.Data.Providers[:limit], func(p api.Provider, idx int) kitex.Node {
										name := p.DisplayName
										if name == "" {
											name = p.Name
										}
										return kitex.Box(kitex.BoxProps{Style: InfoRowStyle},
											kitex.Text(name),
											kitex.If(idx == 0, func() kitex.Node {
												return kitex.Box(kitex.BoxProps{
													Style: style.S().Foreground(t.Color.Text.Tertiary),
												}, kitex.Text("(default)"))
											}),
										)
									}),
								)
							}),
							kitex.If(providers.IsLoading || providers.Data == nil || len(providers.Data.Providers) == 0, func() kitex.Node {
								return kitex.Fragment(
									kitex.Box(kitex.BoxProps{Style: InfoRowStyle.Foreground(t.Color.Text.Secondary)}, kitex.Text("anthropic")),
									kitex.Box(kitex.BoxProps{Style: InfoRowStyle},
										kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Secondary)}, kitex.Text("genai")),
										kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary)}, kitex.Text("(default)")),
									),
									kitex.Box(kitex.BoxProps{Style: InfoRowStyle.Foreground(t.Color.Text.Secondary)}, kitex.Text("ollama")),
								)
							}),
						),

						// Agents
						InfoSection(InfoSectionProps{Icon: icon.Robot, Title: "Agents", Count: agentCount},
							kitex.If(!agents.IsLoading && agents.Data != nil && len(agents.Data.Agents) > 0, func() kitex.Node {
								limit := min(3, len(agents.Data.Agents))
								return kitex.Fragment(
									kitex.Map(agents.Data.Agents[:limit], func(a api.Agent, idx int) kitex.Node {
										return kitex.Box(kitex.BoxProps{Style: InfoRowStyle},
											kitex.Text(a.Name),
											kitex.If(idx == 0, func() kitex.Node {
												return kitex.Box(kitex.BoxProps{
													Style: style.S().Foreground(t.Color.Surface.Primary),
												}, icon.Checkmark)
											}),
										)
									}),
								)
							}),
							kitex.If(agents.IsLoading || agents.Data == nil || len(agents.Data.Agents) == 0, func() kitex.Node {
								return kitex.Box(kitex.BoxProps{Style: InfoRowStyle},
									kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Secondary)}, kitex.Text("main")),
									kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Surface.Primary)}, icon.Checkmark),
								)
							}),
						),

						// Recent Sessions
						InfoSection(InfoSectionProps{Icon: icon.History, Title: "Recent Sessions"},
							kitex.Map(mockSessions, func(s mockSession, idx int) kitex.Node {
								hours := fmt.Sprintf("%dh", idx+1)
								sessionID := s.ID
								return SessionItem(SessionItemProps{
									Name:  s.Name,
									Hours: hours,
									OnClick: func() {
										if props.OnOpenSession != nil {
											props.OnOpenSession(sessionID)
										}
									},
								})
							}),
						),
					),
				),
			),
		),
	)
})
