package setup

import (
	"fmt"
	"maps"

	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/queries"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

var (
	FormContainerStyle = style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexColumn).
				Gap(1)

	FormRowStyle = style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexRow).
			AlignItems(style.AlignCenter).
			Gap(1).
			Width(style.Percent(100)).
			MinWidth(style.Cells(0))

	InputGroupStyle = style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexRow).
			Gap(1)

	InputContainerStyle = style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexColumn).
				Flex(1, 1, style.Cells(0)).
				MinWidth(style.Cells(0))

	InputLabelStyle = style.S().
			Display(style.DisplayFlex).
			JustifyContent(style.JustifyBetween).
			AlignItems(style.AlignCenter)

	InputStyle = style.S().
			PaddingHorizontal(1)

	ModelPrestsContainerStyle = style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexRow).
					Gap(1)

	ToolsHeaderStyle = style.S().
				Display(style.DisplayFlex).
				JustifyContent(style.JustifyBetween).
				AlignItems(style.AlignCenter).
				Padding(1)

	ToolsListStyle = style.S().
			Display(style.DisplayFlex).
			Flex(1).
			FlexDirection(style.FlexColumn).
			Gap(1).
			Overflow(style.OverflowAuto)

	ToolsCategoryHeaderStyle = style.S().
					Display(style.DisplayFlex).
					JustifyContent(style.JustifyBetween).
					AlignItems(style.AlignCenter).
					Width(style.Percent(100))

	ToolsGridStyle = style.S().
			Display(style.DisplayFlex).
			FlexWrap(style.FlexWrapOn).
			PaddingHorizontal(1).
			PaddingBottom(1)

	ToolOptionStyle = style.S().
			Width(style.Percent(32))
)

var ToolCategoryOrders = []string{
	"filesystem",
	"network",
	"system",
	"lsp",
	"mcp",
	"workflow",
}

var ToolCategoryLabels = map[string]string{
	"filesystem": "FILE SYSTEM",
	"network":    "NETWORK & RESEARCH",
	"system":     "SYSTEM",
	"lsp":        "LANGUAGE SERVER PROTOCOL (LSP)",
	"mcp":        "MODEL CONTEXT PROTOCOLS (MCP)",
	"workflow":   "WORKFLOW & AGENTS",
}

var WelcomeStep = kitex.SimpleFC("WelcomeStep", func() kitex.Node {
	t := theme.UseTheme()

	subtext := style.S().Foreground(t.Color.Text.Secondary)
	alertText := style.S().Foreground(t.Color.Text.Purple)

	titleColor := t.Color.Text.Primary
	if c, ok := t.Palette["white"]; ok {
		titleColor = c
	}

	return kitex.Box(kitex.BoxProps{
		Style: StepContentStyle.Gap(3),
	},
		kitex.Box(kitex.BoxProps{},
			kitex.Box(kitex.BoxProps{
				Style: style.S().Bold(true).MarginBottom(1).Foreground(titleColor),
			},
				kitex.Text("WELCOME TO TASKSMITH CONSOLE SETUP!"),
			),
			kitex.Box(kitex.BoxProps{
				Style: subtext,
			},
				kitex.Text("We've detected you are running this app inside an unconfigured workspace folder. Let's customize step-by-step cognitive parameters, plugins, and skills for this project."),
			),
		),
		components.Paper(components.PaperProps{
			Color: components.PaperContentAlt,
			Style: style.S().PaddingVertical(1).PaddingHorizontal(2).TextAlign(style.TextAlignCenter),
		},
			kitex.Box(kitex.BoxProps{
				Style: alertText,
			}, kitex.Text("[!] Skipping this wizard allows you to run in ad-hoc mode without writing environment configurations on disk.")),
		),
	)
})

type ProviderForm struct {
	APIKey       string
	Endpoint     string
	DefaultModel string
	AuthMethod   string // "direct", "env", "file"
	AuthType     string // "bearer", "api-key", "basic"
	AuthHeader   string // custom header name if "api-key"
	AuthEnv      string // custom environment variable name
	AuthFile     string // custom file path
}

type ProviderStepProps struct {
	SelectedProvider    string
	SetSelectedProvider func(string)
	Configs             map[string]ProviderForm
	SetConfigs          func(map[string]ProviderForm)
}

var ProviderStep = kitex.FC("ProviderStep", func(props ProviderStepProps) kitex.Node {
	resp := queries.UseListProvidersPresets()
	t := theme.UseTheme()

	primary := style.S().Foreground(t.Color.Surface.Primary)
	muted := style.S().Foreground(t.Color.Text.Tertiary)

	configs := func() map[string]ProviderForm {
		return props.Configs
	}
	setConfigs := props.SetConfigs

	// Initialize state from presets
	kitex.UseEffect(func() {
		if !resp.IsLoading && len(resp.Data.Providers) > 0 {
			newConfigs := make(map[string]ProviderForm)
			for _, p := range resp.Data.Providers {
				authMethod := "direct"
				authType := "bearer"
				if p.Description == "google" {
					authType = "api-key"
				}
				authEnv := p.AuthEnv
				if authEnv == "" {
					if p.Description == "openai" {
						authEnv = "OPENAI_API_KEY"
					} else if p.Description == "anthropic" {
						authEnv = "ANTHROPIC_API_KEY"
					} else if p.Description == "google" {
						authEnv = "GOOGLE_API_KEY"
					}
				}

				newConfigs[p.Name] = ProviderForm{
					Endpoint:     p.Endpoint,
					DefaultModel: p.DefaultModel,
					APIKey:       p.APIKey,
					AuthMethod:   authMethod,
					AuthType:     authType,
					AuthEnv:      authEnv,
					AuthFile:     "",
					AuthHeader:   "",
				}
			}
			setConfigs(newConfigs)
			if props.SelectedProvider == "" {
				props.SetSelectedProvider(resp.Data.Providers[0].Name)
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

	return kitex.Box(kitex.BoxProps{
		Style: StepContentStyle,
	},
		kitex.Box(kitex.BoxProps{},
			kitex.Box(kitex.BoxProps{
				Style: style.S().Bold(true).MarginBottom(1),
			},
				kitex.Text("CONFIGURE MODEL PROVIDERS"),
			),
			kitex.Box(kitex.BoxProps{
				Style: muted,
			},
				kitex.Text("Define your reasoning engine. Select a provider, customize endpoints, and quickly click presets to bind default models."),
			),
		),
		kitex.Box(kitex.BoxProps{
			Style: FormRowStyle,
		},
			kitex.Box(kitex.BoxProps{
				Style: muted.Bold(true),
			}, kitex.Text("PROVIDER NODE:")),
			kitex.Map(resp.Data.Providers, func(p api.Provider, _ int) kitex.Node {
				isSelected := props.SelectedProvider == p.Name

				colorVal := components.ButtonBase
				if isSelected {
					colorVal = components.ButtonInfo
				}

				return components.Button(components.ButtonProps{
					Key:     p.Name,
					Variant: components.ButtonTonal,
					Color:   colorVal,
					Style:   style.S().PaddingHorizontal(1).Bold(true),
					OnClick: func() {
						props.SetSelectedProvider(p.Name)
					},
				},
					kitex.Text(func() string {
						if isSelected {
							return "■ " + p.Name
						}
						return "□ " + p.Name
					}()),
				)
			}),
		),

		kitex.If(props.SelectedProvider != "", func() kitex.Node {
			selected := props.SelectedProvider
			conf := configs()[selected]

			var currProvider api.Provider
			for _, p := range resp.Data.Providers {
				if p.Name == selected {
					currProvider = p
					break
				}
			}

			return components.Paper(components.PaperProps{
				Color: components.PaperContentAlt,
				Style: FormContainerStyle.PaddingVertical(1).PaddingHorizontal(2),
			},
				// Endpoint Row
				kitex.Box(kitex.BoxProps{
					Style: FormRowStyle,
				},
					kitex.Box(kitex.BoxProps{
						Style: style.S().Width(style.Cells(13)).Bold(true).Foreground(t.Color.Text.Secondary),
					}, kitex.Text("ENDPOINT:")),
					components.Input(components.InputProps{
						Name:        "endpoint_" + selected,
						Value:       conf.Endpoint,
						Placeholder: "Base URL...",
						Variant:     components.InputSolid,
						Style:       InputStyle.Flex(1, 1, style.Cells(0)),
						OnChange: func(val string) {
							curr := configs()
							next := make(map[string]ProviderForm)
							maps.Copy(next, curr)
							c := next[selected]
							c.Endpoint = val
							next[selected] = c
							setConfigs(next)
						},
					}),
				),

				// Auth Secret Row
				kitex.If(currProvider.AuthEnv != "", func() kitex.Node {
					return kitex.Box(kitex.BoxProps{
						Style: FormRowStyle,
					},
						kitex.Box(kitex.BoxProps{
							Style: style.S().Width(style.Cells(13)).Bold(true).Foreground(t.Color.Text.Secondary),
						}, kitex.Text("AUTH SECRET:")),
						components.Select(components.SelectProps{
							Value: func() string {
								if conf.AuthMethod == "" {
									return "direct"
								}
								return conf.AuthMethod
							}(),
							OnValueChange: func(val string) {
								curr := configs()
								next := make(map[string]ProviderForm)
								maps.Copy(next, curr)
								c := next[selected]
								c.AuthMethod = val
								next[selected] = c
								setConfigs(next)
							},
						},
							components.Option(components.OptionProps{Text: "Value", Value: "direct"}),
							components.Option(components.OptionProps{Text: "Env", Value: "env"}),
							components.Option(components.OptionProps{Text: "File", Value: "file"}),
						),
						components.Input(components.InputProps{
							Name: "auth_val_" + selected,
							Value: func() string {
								if conf.AuthMethod == "file" {
									return conf.AuthFile
								}
								if conf.AuthMethod == "env" {
									return conf.AuthEnv
								}
								return conf.APIKey
							}(),
							Placeholder: func() string {
								if conf.AuthMethod == "file" {
									return "e.g. /path/to/key"
								}
								if conf.AuthMethod == "env" {
									return "e.g. OPENAI_API_KEY"
								}
								return "e.g. sk-proj-..."
							}(),
							Variant: components.InputSolid,
							Style:   InputStyle.Flex(1, 1, style.Cells(0)),
							OnChange: func(val string) {
								curr := configs()
								next := make(map[string]ProviderForm)
								maps.Copy(next, curr)
								c := next[selected]
								if conf.AuthMethod == "file" {
									c.AuthFile = val
								} else if conf.AuthMethod == "env" {
									c.AuthEnv = val
								} else {
									c.APIKey = val
								}
								next[selected] = c
								setConfigs(next)
							},
						}),
					)
				}),

				// Auth Scheme Row
				kitex.If(currProvider.AuthEnv != "", func() kitex.Node {
					return kitex.Box(kitex.BoxProps{
						Style: FormRowStyle,
					},
						kitex.Box(kitex.BoxProps{
							Style: style.S().Width(style.Cells(13)).Bold(true).Foreground(t.Color.Text.Secondary),
						}, kitex.Text("AUTH SCHEME:")),
						components.Select(components.SelectProps{
							Value: func() string {
								if conf.AuthType == "" {
									return "bearer"
								}
								return conf.AuthType
							}(),
							OnValueChange: func(val string) {
								curr := configs()
								next := make(map[string]ProviderForm)
								maps.Copy(next, curr)
								c := next[selected]
								c.AuthType = val
								next[selected] = c
								setConfigs(next)
							},
						},
							components.Option(components.OptionProps{Text: "Bearer", Value: "bearer"}),
							components.Option(components.OptionProps{Text: "API Key", Value: "api-key"}),
							components.Option(components.OptionProps{Text: "Basic", Value: "basic"}),
						),
						kitex.If(conf.AuthType == "api-key", func() kitex.Node {
							return kitex.Fragment(
								kitex.Box(kitex.BoxProps{
									Style: style.S().Width(style.Cells(13)).Bold(true).Foreground(t.Color.Text.Secondary).MarginLeft(1),
								}, kitex.Text("HTTP HEADER:")),
								components.Input(components.InputProps{
									Name:        "auth_header_" + selected,
									Value:       conf.AuthHeader,
									Placeholder: "e.g. x-api-key",
									Variant:     components.InputSolid,
									Style:       InputStyle.Flex(1, 1, style.Cells(0)),
									OnChange: func(val string) {
										curr := configs()
										next := make(map[string]ProviderForm)
										maps.Copy(next, curr)
										c := next[selected]
										c.AuthHeader = val
										next[selected] = c
										setConfigs(next)
									},
								}),
							)
						}),
					)
				}),
				// Default Model Identifier Row
				kitex.Box(kitex.BoxProps{
					Style: InputContainerStyle,
				},
					kitex.Box(kitex.BoxProps{
						Style: InputLabelStyle,
					},
						kitex.Box(kitex.BoxProps{
							Style: muted.Bold(true),
						}, kitex.Text("DEFAULT MODEL IDENTIFIER:")),
						kitex.Box(kitex.BoxProps{
							Style: primary.Italic(true),
						}, kitex.Text("select from presets below to autofill")),
					),
					components.Input(components.InputProps{
						Name:        "model_" + selected,
						Value:       conf.DefaultModel,
						Placeholder: "e.g. gemini-2.5-flash",
						Variant:     components.InputSolid,
						Color:       components.InputPrimary,
						Style:       InputStyle,
						OnChange: func(val string) {
							curr := configs()
							next := make(map[string]ProviderForm)
							maps.Copy(next, curr)
							c := next[selected]
							c.DefaultModel = val
							next[selected] = c
							setConfigs(next)
						},
					}),
				),

				// Model Presets
				kitex.Box(kitex.BoxProps{
					Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Gap(1),
				},
					kitex.Box(kitex.BoxProps{
						Style: muted.Bold(true),
					}, kitex.Text("MODEL PRESETS:")),
					kitex.Box(kitex.BoxProps{
						Style: ModelPrestsContainerStyle,
					},
						kitex.Map(currProvider.Models, func(m api.Model, _ int) kitex.Node {
							modelID := m.ID
							if modelID == "" {
								modelID = m.Name
							}

							isActive := conf.DefaultModel == modelID
							colorVal := components.ButtonBase
							if isActive {
								colorVal = components.ButtonInfo
							}

							buttonLabel := m.Label
							if buttonLabel == "" {
								buttonLabel = m.Name
							}
							if buttonLabel == "" {
								buttonLabel = modelID
							}

							return components.Button(components.ButtonProps{
								Key:     modelID,
								Variant: components.ButtonTonal,
								Color:   colorVal,
								Style:   style.S().PaddingHorizontal(1).Bold(true),
								OnClick: func() {
									curr := configs()
									next := make(map[string]ProviderForm)
									maps.Copy(next, curr)
									c := next[selected]
									c.DefaultModel = modelID
									next[selected] = c
									setConfigs(next)
								},
							},
								kitex.Text(buttonLabel),
							)
						}),
					),
				),
			)
		}),
	)
})

type ToolCategoryHeaderSummaryProps struct {
	Label    string
	Expanded bool
	Hovered  bool
}

var ToolCategoryHeaderSummary = kitex.FC("ToolCategoryHeaderSummary", func(props ToolCategoryHeaderSummaryProps) kitex.Node {
	t := theme.UseTheme()

	titleColor := t.Color.Surface.Tertiary // yellow
	if props.Hovered {
		titleColor = t.Color.Surface.TertiaryHover
	}

	actionColor := t.Color.Text.Tertiary // comment
	if props.Hovered {
		actionColor = t.Color.Surface.Primary // cyan
	}

	labelText := "> " + props.Label
	actionText := "[+] EXPAND"
	if props.Expanded {
		actionText = "[-] COLLAPSE"
	}

	return kitex.Box(kitex.BoxProps{
		Style: ToolsCategoryHeaderStyle,
	},
		kitex.Box(kitex.BoxProps{
			Style: style.S().Foreground(titleColor).Bold(true),
		}, kitex.Text(labelText)),
		kitex.Box(kitex.BoxProps{
			Style: style.S().Foreground(actionColor),
		}, kitex.Text(actionText)),
	)
})

type ToolCategoryAccordionProps struct {
	Cat                    string
	Tools                  []api.Tool
	CollapsedCategories    func() map[string]bool
	SetCollapsedCategories func(map[string]bool)
	AuthorizedTools        map[string]bool
	SetAuthorizedTools     func(map[string]bool)
}

var ToolCategoryAccordion = kitex.FC("ToolCategoryAccordion", func(props ToolCategoryAccordionProps) kitex.Node {
	isHovered, setIsHovered := kitex.UseState(false)
	isCollapsed := props.CollapsedCategories()[props.Cat]
	expanded := !isCollapsed

	// Use a fresh pointer to ensure kitex detects the prop change
	expPtr := new(bool)
	*expPtr = expanded

	return components.Accordion(components.AccordionProps{
		Color:    components.PaperContentAlt,
		Expanded: expPtr,
		OnChange: func(e bool) {
			curr := make(map[string]bool)
			maps.Copy(curr, props.CollapsedCategories())
			curr[props.Cat] = !e
			props.SetCollapsedCategories(curr)
		},
	},
		components.AccordionSummary(components.AccordionSummaryProps{
			HideExpandIcon: true,
			Style:          style.S().Padding(1),
		},
			kitex.Box(kitex.BoxProps{
				OnMouseEnter: func(e event.Event) {
					setIsHovered(true)
				},
				OnMouseLeave: func(e event.Event) {
					setIsHovered(false)
				},
				Style: style.S().Width(style.Percent(100)),
			},
				ToolCategoryHeaderSummary(ToolCategoryHeaderSummaryProps{
					Label:    ToolCategoryLabels[props.Cat],
					Expanded: expanded,
					Hovered:  isHovered(),
				}),
			),
		),
		components.AccordionDetails(components.AccordionDetailsProps{
			Style: ToolsGridStyle,
		},
			kitex.Map(props.Tools, func(tool api.Tool, _ int) kitex.Node {
				enabled := props.AuthorizedTools[tool.Name]
				return components.Checkbox(components.CheckboxProps{
					Checked: enabled,
					Label:   kitex.Text(tool.Name),
					Style:   ToolOptionStyle,
					OnChange: func(val bool) {
						curr := make(map[string]bool)
						maps.Copy(curr, props.AuthorizedTools)
						curr[tool.Name] = val
						props.SetAuthorizedTools(curr)
					},
				})
			}),
		),
	)
})

type ToolsStepProps struct {
	AuthorizedTools    map[string]bool
	SetAuthorizedTools func(map[string]bool)
}

var ToolsStep = kitex.FC("ToolsStep", func(props ToolsStepProps) kitex.Node {
	resp := queries.UseListToolsPresets()
	t := theme.UseTheme()

	muted := style.S().Foreground(t.Color.Text.Tertiary)

	collapsedCategories, setCollapsedCategories := kitex.UseState(make(map[string]bool))

	if resp.IsLoading {
		return kitex.Box(kitex.BoxProps{
			Style: StepContentStyle,
		},
			kitex.Text("Loading tool presets..."),
		)
	}

	// Group tools by category
	categories := make(map[string][]api.Tool)
	for _, tool := range resp.Data.Tools {
		categories[tool.Category] = append(categories[tool.Category], tool)
	}

	return kitex.Box(kitex.BoxProps{
		Style: StepContentStyle,
	},
		kitex.Box(kitex.BoxProps{},
			components.Paper(components.PaperProps{
				Color: components.PaperContentAlt,
				Style: ToolsHeaderStyle,
			},
				kitex.Box(kitex.BoxProps{},
					kitex.Box(kitex.BoxProps{
						Style: style.S().Bold(true),
					}, kitex.Text("CONFIGURE SANDBOX TOOLS")),
					kitex.Box(kitex.BoxProps{
						Style: muted,
					}, kitex.Text("Authorize or restrict terminal-level scripts, search, and system execution vectors.")),
				),
				components.Button(components.ButtonProps{
					Key:     "tools_enable_all",
					Variant: components.ButtonTonal,
					Color:   components.ButtonInfo,
					Style:   style.S().PaddingHorizontal(1).Bold(true),
					OnClick: func() {
						newAuth := make(map[string]bool)
						for _, tool := range resp.Data.Tools {
							newAuth[tool.Name] = true
						}
						props.SetAuthorizedTools(newAuth)
					},
				}, kitex.Text(" ENABLE ALL")),
			),
		),

		kitex.Box(kitex.BoxProps{
			Style: ToolsListStyle,
		},
			kitex.Map(ToolCategoryOrders, func(cat string, _ int) kitex.Node {
				tools := categories[cat]
				if len(tools) == 0 {
					return kitex.Empty()
				}
				return ToolCategoryAccordion(ToolCategoryAccordionProps{
					Cat:                    cat,
					Tools:                  tools,
					CollapsedCategories:    collapsedCategories,
					SetCollapsedCategories: setCollapsedCategories,
					AuthorizedTools:        props.AuthorizedTools,
					SetAuthorizedTools:     props.SetAuthorizedTools,
				})
			}),
		),
	)
})

type ConfirmStepProps struct {
	ProjectName      string
	SelectedProvider string
	AuthorizedTools  map[string]bool
}

var ConfirmStep = kitex.FC("ConfirmStep", func(props ConfirmStepProps) kitex.Node {
	t := theme.UseTheme()

	primary := style.S().Foreground(t.Color.Surface.Primary)
	success := style.S().Foreground(t.Color.Surface.Success)
	accent := style.S().Foreground(t.Color.Surface.Tertiary)
	muted := style.S().Foreground(t.Color.Text.Tertiary)

	var activeTools []string
	for tool, enabled := range props.AuthorizedTools {
		if enabled {
			activeTools = append(activeTools, tool)
		}
	}

	return kitex.Box(kitex.BoxProps{
		Style: StepContentStyle,
	},
		kitex.Box(kitex.BoxProps{
			Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Gap(2),
		},
			kitex.Box(kitex.BoxProps{},
				kitex.Box(kitex.BoxProps{
					Style: style.S().Bold(true).MarginBottom(1),
				},
					kitex.Text("CONFIRM BOUNDARY CONFIGURATIONS"),
				),
				kitex.Box(kitex.BoxProps{
					Style: muted,
				},
					kitex.Text("Review your customized environment parameters."),
				),
			),

			components.Paper(components.PaperProps{
				Color: components.PaperFooter,
				Style: style.S().
					PaddingVertical(1).
					PaddingHorizontal(2).
					Overflow(style.OverflowAuto).
					MaxHeight(style.Cells(14)).
					Display(style.DisplayFlex).
					FlexDirection(style.FlexColumn).
					Gap(1),
			},
				kitex.Box(kitex.BoxProps{
					Style: style.S().
						Display(style.DisplayFlex).
						JustifyContent(style.JustifyBetween),
				},
					kitex.Box(kitex.BoxProps{
						Style: primary.Bold(true),
					}, kitex.Text("  WORKSPACE:")),
					kitex.Text(props.ProjectName),
				),
				kitex.Box(kitex.BoxProps{
					Style: style.S().
						Display(style.DisplayFlex).
						JustifyContent(style.JustifyBetween),
				},
					kitex.Box(kitex.BoxProps{
						Style: success.Bold(true),
					}, kitex.Text("  ROUTER:")),
					kitex.Text(props.SelectedProvider),
				),
				kitex.Box(kitex.BoxProps{},
					kitex.Box(kitex.BoxProps{
						Style: accent.Bold(true),
					}, kitex.Text(fmt.Sprintf("  AUTHORIZED TOOLS (%d ACTIVE):", len(activeTools)))),
					kitex.Box(kitex.BoxProps{
						Style: style.S().Display(style.DisplayFlex).FlexWrap(style.FlexWrapOn).Gap(1),
					},
						kitex.Map(activeTools, func(toolID string, _ int) kitex.Node {
							return kitex.Box(kitex.BoxProps{
								Style: style.S().
									PaddingHorizontal(1).
									PaddingVertical(0).
									Margin(0).
									Background(t.Color.Surface.InfoFocus).
									Foreground(t.Color.Surface.Primary).
									Bold(true),
							},
								kitex.Text(toolID),
							)
						}),
					),
				),
			),

			kitex.Box(kitex.BoxProps{
				Style: success.Background(t.Color.Surface.SuccessFocus).Foreground(t.Color.Surface.Success).PaddingVertical(1).PaddingHorizontal(2).Bold(true),
			},
				kitex.Text("[OK] Establishing this signature authorizes this directory for local processing."),
			),
		),
	)
})
