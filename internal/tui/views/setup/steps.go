package setup

import (
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/queries"
)

var (
	FormContainerStyle = style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexColumn).
				Gap(2)

	InputGroupStyle = style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexRow).
			Gap(2)

	InputContainerStyle = style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexColumn).
				Flex(1)

	InputLabelStyle = style.S().
			Display(style.DisplayFlex).
			JustifyContent(style.JustifyBetween).
			AlignItems(style.AlignCenter)

	InputStyle = style.S().
			PaddingVertical(1)

	ModelPrestsContainerStyle = style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexRow).
					Gap(1)
)

func WelcomeStep() kitex.Node {
	return kitex.Box(kitex.BoxProps{
		Style: StepContentStyle,
	},
		kitex.Box(kitex.BoxProps{},
			kitex.Box(kitex.BoxProps{
				Style: style.S().MarginBottom(1),
			},
				kitex.Text("WELCOME TO TASKSMITH CONSOLE!"),
			),
			kitex.Box(kitex.BoxProps{},
				kitex.Text("This setup wizard will guide you through the initial configuration of Tasksmith Console. You can skip any step and configure it later in the settings."),
			),
		),
		components.Alert(components.AlertProps{
			Severity: components.AlertInfo,
			Style:    style.S().TextAlign(style.TextAlignCenter),
		},
			kitex.Text("[!] Skipping this wizard allows you to run in ad-hoc mode without writing enviroment configurations on disk."),
		),
	)
}

type ProviderForm struct {
	APIKey       string
	Endpoint     string
	DefaultModel string
}

func ProviderStep() kitex.Node {
	resp := queries.UseListProvidersPresets()

	// Provider configurations: provider name -> config
	configs, setConfigs := kitex.UseState(make(map[string]ProviderForm))
	selectedProvider, setSelectedProvider := kitex.UseState("")

	// Initialize state from presets
	kitex.UseEffect(func() {
		if !resp.IsLoading && len(resp.Data.Providers) > 0 {
			newConfigs := make(map[string]ProviderForm)
			for _, p := range resp.Data.Providers {
				newConfigs[p.Name] = ProviderForm{
					Endpoint:     p.Endpoint,
					DefaultModel: p.DefaultModel,
				}
			}
			setConfigs(newConfigs)
			if selectedProvider() == "" {
				setSelectedProvider(resp.Data.Providers[0].Name)
			}
		}
	}, []any{resp.IsLoading})

	// Styles
	if resp.IsLoading {
		return kitex.Box(kitex.BoxProps{
			Style: StepContentStyle,
		},
			kitex.Text("Loading provider presets..."),
		)
	}

	conf := configs()[selectedProvider()]
	var currProvider api.Provider
	for _, p := range resp.Data.Providers {
		if p.Name == selectedProvider() {
			currProvider = p
			break
		}
	}

	return kitex.Box(kitex.BoxProps{
		Style: StepContentStyle,
	},
		kitex.Box(kitex.BoxProps{},
			kitex.Box(kitex.BoxProps{
				Style: style.S().MarginBottom(1),
			},
				kitex.Text("CONFIGURE MODEL PROVIDERS"),
			),
			kitex.Box(kitex.BoxProps{},
				kitex.Text("Define your reasoning engine. Select a provider, customize endpoints, and quickly click presets to bind default models."),
			),
		),
		kitex.Box(kitex.BoxProps{
			Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1),
		},
			kitex.Text("PROVIDER:"),
			kitex.Map(resp.Data.Providers, func(p api.Provider, _ int) kitex.Node {
				return components.Button(components.ButtonProps{
					Active: selectedProvider() == p.Name,
					OnClick: func() {
						setSelectedProvider(p.Name)
					},
				},
					kitex.Text(p.Name),
				)
			}),
		),

		kitex.If(selectedProvider() != "", func() kitex.Node {
			return kitex.Box(kitex.BoxProps{
				Style: FormContainerStyle,
			},
				// API Key and Endpoint Row
				kitex.Box(kitex.BoxProps{
					Style: InputGroupStyle,
				},
					// API Key
					kitex.Box(kitex.BoxProps{
						Style: InputContainerStyle,
					},
						kitex.Box(kitex.BoxProps{
							Style: InputLabelStyle,
						},
							kitex.Box(kitex.BoxProps{}, kitex.Text("API KEY")),
							kitex.Box(kitex.BoxProps{}, kitex.Text("(add if using cloud/remote)")),
						),
						components.Input(components.InputProps{
							Value:       conf.APIKey,
							Placeholder: "Optional API key...",
							Style:       InputStyle,
							OnChange: func(val string) {
								curr := configs()
								c := curr[selectedProvider()]
								c.APIKey = val
								curr[selectedProvider()] = c
								setConfigs(curr)
							},
						}),
					),
					// Endpoint
					kitex.Box(kitex.BoxProps{
						Style: InputContainerStyle,
					},
						kitex.Box(kitex.BoxProps{}, kitex.Text("BASE ENDPOINT URL:")),
						components.Input(components.InputProps{
							Value:       conf.Endpoint,
							Placeholder: "Base URL...",
							Style:       InputStyle,
							OnChange: func(val string) {
								curr := configs()
								c := curr[selectedProvider()]
								c.Endpoint = val
								curr[selectedProvider()] = c
								setConfigs(curr)
							},
						}),
					),
				),
				// Default Model Identifier Row
				kitex.Box(kitex.BoxProps{
					Style: InputContainerStyle,
				},
					kitex.Box(kitex.BoxProps{
						Style: InputLabelStyle,
					},
						kitex.Box(kitex.BoxProps{}, kitex.Text("DEFAULT MODEL IDENTIFIER:")),
						kitex.Box(kitex.BoxProps{}, kitex.Text("select from presets below to autofill")),
					),
					components.Input(components.InputProps{
						Value:       conf.DefaultModel,
						Placeholder: "e.g. gemini-2.5-flash",
						Style:       InputStyle,
						OnChange: func(val string) {
							curr := configs()
							c := curr[selectedProvider()]
							c.DefaultModel = val
							curr[selectedProvider()] = c
							setConfigs(curr)
						},
					}),
				),

				// Model Presets
				kitex.Box(kitex.BoxProps{
					Style: ModelPrestsContainerStyle,
				},
					kitex.Text("MODEL PRESETS:"),
					kitex.Map(currProvider.Models, func(m api.Model, _ int) kitex.Node {
						return components.Button(components.ButtonProps{
							Active: conf.DefaultModel == m.Name,
							OnClick: func() {
								curr := configs()
								c := curr[selectedProvider()]
								c.DefaultModel = m.Name
								curr[selectedProvider()] = c
								setConfigs(curr)
							},
						},
							kitex.Text(m.Name),
						)
					}),
				),
			)
		}),
	)
}
