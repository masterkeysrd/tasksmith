package mcpinfo

import (
	"context"
	"fmt"
	"strings"

	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/active"
	tuiapi "github.com/masterkeysrd/tasksmith/internal/tui/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/queries"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

type ViewProps struct{}

var View = kitex.FC("McpInfoView", func(props ViewProps) kitex.Node {
	modal := active.UseModal()
	isOpen := modal == "mcp" || modal == "mcpinfo"
	t := theme.UseTheme()
	client := tuiapi.UseClient()
	statusQuery := queries.UseGetMcpStatus()
	selectedServer, setSelectedServer := kitex.UseState("")
	isRestarting, setRestarting := kitex.UseState(false)
	activeTab, setActiveTab := kitex.UseState("tools")

	kitex.UseEffect(func() {
		if statusQuery.Data != nil && len(statusQuery.Data.Servers) > 0 && selectedServer() == "" {
			setSelectedServer(statusQuery.Data.Servers[0].Name)
		}
	}, []any{statusQuery.Data})

	if !isOpen {
		return nil
	}

	var content kitex.Node

	if statusQuery.IsLoading {
		content = kitex.Box(kitex.BoxProps{
			Style: style.S().Padding(2).Foreground(t.Color.Text.Tertiary),
		}, kitex.Text("Loading MCP status..."))
	} else if statusQuery.Error != nil {
		content = components.Alert(components.AlertProps{
			Severity: components.AlertError,
			Children: []kitex.Node{
				kitex.Text(statusQuery.Error.Error()),
			},
		})
	} else if statusQuery.Data == nil || len(statusQuery.Data.Servers) == 0 {
		content = kitex.Box(kitex.BoxProps{
			Style: style.S().Padding(2).Foreground(t.Color.Text.Tertiary),
		}, kitex.Text("No MCP servers configured in the workspace."))
	} else {
		// Render Split Panels
		var leftItems []kitex.Node
		for _, srv := range statusQuery.Data.Servers {
			srv := srv
			isSelected := srv.Name == selectedServer()

			bg := t.Color.Surface.Base
			if isSelected {
				bg = t.Color.Surface.BaseHover
			}

			statusStr := "Disconnected"
			statusColor := t.Color.Text.Error
			if srv.IsRunning {
				statusStr = "Connected"
				statusColor = t.Color.Surface.Success
			}

			displayName := srv.Name
			if srv.Title != "" {
				displayName = srv.Title
			}

			leftItems = append(leftItems, kitex.Box(kitex.BoxProps{
				Style: style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexColumn).
					Padding(0, 1).
					Background(bg).
					BorderLeft(isSelected, style.SingleBorder(), t.Color.Surface.Primary),
				OnClick: func(e event.Event) {
					setSelectedServer(srv.Name)
				},
			},
				// First Row: Name & Status
				kitex.Box(kitex.BoxProps{
					Style: style.S().
						Display(style.DisplayFlex).
						FlexDirection(style.FlexRow).
						AlignItems(style.AlignCenter).
						JustifyContent(style.JustifyBetween).
						Width(style.Percent(100)),
				},
					kitex.Box(kitex.BoxProps{Style: style.S().Bold(isSelected).Foreground(t.Color.Text.Primary)}, kitex.Text(displayName)),
					kitex.Box(kitex.BoxProps{Style: style.S().Foreground(statusColor)}, kitex.Text(statusStr)),
				),
				// Second Row: Transport & Version
				kitex.Box(kitex.BoxProps{
					Style: style.S().
						Display(style.DisplayFlex).
						FlexDirection(style.FlexRow).
						AlignItems(style.AlignCenter).
						JustifyContent(style.JustifyBetween).
						Width(style.Percent(100)),
				},
					kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary)}, kitex.Text(srv.Type)),
					kitex.If(srv.Version != "", func() kitex.Node {
						return kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary)}, kitex.Text("v"+srv.Version))
					}),
				),
			))
		}

		leftPanel := kitex.Box(kitex.BoxProps{
			Style: style.S().
				Width(style.Cells(30)).
				MinWidth(style.Cells(30)).
				Height(style.Percent(100)).
				Display(style.DisplayFlex).
				FlexDirection(style.FlexColumn).
				Gap(0).
				OverflowY(style.OverflowAuto),
		}, leftItems...)

		// Find details for selected server
		var selectedInfo *api.McpServerInfo
		for i := range statusQuery.Data.Servers {
			if statusQuery.Data.Servers[i].Name == selectedServer() {
				selectedInfo = &statusQuery.Data.Servers[i]
				break
			}
		}

		var rightPanel kitex.Node
		if selectedInfo == nil {
			rightPanel = kitex.Box(kitex.BoxProps{
				Style: style.S().Flex(1, 1, style.Cells(0)).Padding(1).Foreground(t.Color.Text.Tertiary),
			}, kitex.Text("Select an MCP server to view details."))
		} else {
			statusStr := "Disconnected"
			statusColor := t.Color.Text.Error
			if selectedInfo.IsRunning {
				statusStr = "Connected"
				statusColor = t.Color.Surface.Success
			}

			// Helper to strip the "mcp__<serverName>__" prefix
			cleanToolName := func(fullName string) string {
				parts := strings.Split(fullName, "__")
				if len(parts) >= 3 && parts[0] == "mcp" {
					return strings.Join(parts[2:], "__")
				}
				return fullName
			}

			// Helper for section headers
			sectionHeader := func(title string) kitex.Node {
				return kitex.Box(kitex.BoxProps{
					Style: style.S().
						Bold(true).
						Foreground(t.Color.Text.Primary).
						BorderBottom(true, style.SingleBorder(), t.Color.Border.Primary).
						MarginTop(1).
						MarginBottom(0).
						PaddingBottom(0).
						Width(style.Percent(100)),
				}, kitex.Text(title))
			}

			var detailSections []kitex.Node

			// ================= STATIC HEADER CARD SECTION =================
			displayNameText := selectedInfo.Name
			if selectedInfo.Title != "" {
				displayNameText = selectedInfo.Title
			}

			// Status indicator badge
			badgeNode := kitex.Box(kitex.BoxProps{
				Style: style.S().
					PaddingHorizontal(1).
					Background(t.Color.Surface.Base).
					Foreground(statusColor).
					Bold(true),
			}, kitex.Text("● "+statusStr))

			titleAndBadgeRow := kitex.Box(kitex.BoxProps{
				Style: style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexRow).
					AlignItems(style.AlignCenter).
					JustifyContent(style.JustifyBetween).
					Width(style.Percent(100)),
			},
				kitex.Box(kitex.BoxProps{
					Style: style.S().Bold(true).Foreground(t.Color.Text.Primary),
				}, kitex.Text(displayNameText)),
				badgeNode,
			)

			var metaItems []string
			if selectedInfo.Version != "" {
				metaItems = append(metaItems, "VERSION: "+selectedInfo.Version)
			}
			metaItems = append(metaItems, "TRANSPORT: "+strings.ToUpper(selectedInfo.Type))

			metaRow := kitex.Box(kitex.BoxProps{
				Style: style.S().Foreground(t.Color.Text.Tertiary),
			}, kitex.Text(strings.Join(metaItems, "  ")))

			infoCol := kitex.Box(kitex.BoxProps{
				Style: style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexColumn).
					Gap(0).
					Width(style.Percent(100)).
					Flex(1, 1, style.Cells(0)),
			},
				titleAndBadgeRow,
				metaRow,
			)

			headerCard := kitex.Box(kitex.BoxProps{
				Style: style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexRow).
					AlignItems(style.AlignCenter).
					Padding(1).
					Background(t.Color.Surface.BaseHover).
					Border(true, style.SingleBorder(), t.Color.Border.Primary).
					Width(style.Percent(100)).
					MarginBottom(0),
			},
				infoCol,
			)

			detailSections = append(detailSections, headerCard)

			// Server Description (from YAML metadata)
			if selectedInfo.Description != "" {
				detailSections = append(detailSections, kitex.Box(kitex.BoxProps{
					Style: style.S().
						Foreground(t.Color.Text.Secondary).
						MarginTop(1).
						MarginBottom(0),
				}, kitex.Text(selectedInfo.Description)))
			}

			// Server Instructions (collapsible accordion)
			if selectedInfo.Instructions != "" {
				detailSections = append(detailSections, components.Accordion(components.AccordionProps{
					Color:           components.PaperHover,
					Variant:         components.PaperDefault,
					DefaultExpanded: false,
					Style:           style.S().MarginTop(1).MarginBottom(0),
				},
					components.AccordionSummary(components.AccordionSummaryProps{},
						kitex.Box(kitex.BoxProps{
							Style: style.S().Bold(true).Foreground(t.Color.Text.Primary),
						}, kitex.Text("Instructions")),
					),
					components.AccordionDetails(components.AccordionDetailsProps{},
						kitex.Box(kitex.BoxProps{
							Style: style.S().Foreground(t.Color.Text.Secondary),
						}, kitex.Text(selectedInfo.Instructions)),
					),
				))
			}

			// ================= TABS BAR =================
			isToolsTab := activeTab() == "tools"
			isResourcesTab := activeTab() == "resources"
			isPromptsTab := activeTab() == "prompts"
			isConfigTab := activeTab() == "config"

			tabStyle := func(isActive bool) style.Style {
				s := style.S().
					Padding(0, 1).
					Bold(isActive)
				if isActive {
					return s.
						Foreground(t.Color.Text.Primary).
						BorderBottom(true, style.SingleBorder(), t.Color.Surface.Primary)
				}
				return s.
					Foreground(t.Color.Text.Tertiary)
			}

			tabsBar := kitex.Box(kitex.BoxProps{
				Style: style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexRow).
					Gap(1).
					BorderBottom(true, style.SingleBorder(), t.Color.Border.Primary).
					Width(style.Percent(100)).
					MarginTop(1).
					MarginBottom(0),
			},
				kitex.Box(kitex.BoxProps{
					Style: tabStyle(isToolsTab),
					OnClick: func(e event.Event) {
						setActiveTab("tools")
					},
				}, kitex.Text(fmt.Sprintf("Tools (%d)", len(selectedInfo.Tools)))),
				kitex.Box(kitex.BoxProps{
					Style: tabStyle(isResourcesTab),
					OnClick: func(e event.Event) {
						setActiveTab("resources")
					},
				}, kitex.Text(fmt.Sprintf("Resources (%d)", len(selectedInfo.Resources)+len(selectedInfo.ResourceTemplates)))),
				kitex.Box(kitex.BoxProps{
					Style: tabStyle(isPromptsTab),
					OnClick: func(e event.Event) {
						setActiveTab("prompts")
					},
				}, kitex.Text(fmt.Sprintf("Prompts (%d)", len(selectedInfo.Prompts)))),
				kitex.Box(kitex.BoxProps{
					Style: tabStyle(isConfigTab),
					OnClick: func(e event.Event) {
						setActiveTab("config")
					},
				}, kitex.Text("Configuration")),
			)

			detailSections = append(detailSections, tabsBar)

			// ================= SELECTED TAB CONTENT =================
			if isToolsTab {
				// Website URL
				if selectedInfo.WebsiteURL != "" {
					detailSections = append(detailSections, kitex.Box(kitex.BoxProps{
						Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1),
					},
						kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary)}, kitex.Text("Website:")),
						kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Secondary)}, kitex.Text(selectedInfo.WebsiteURL)),
					))
				}

				// Capabilities
				var caps []string
				if selectedInfo.Capabilities.Tools {
					caps = append(caps, "Tools")
				}
				if selectedInfo.Capabilities.Resources {
					caps = append(caps, "Resources")
				}
				if selectedInfo.Capabilities.Prompts {
					caps = append(caps, "Prompts")
				}
				if selectedInfo.Capabilities.Logging {
					caps = append(caps, "Logging")
				}
				if selectedInfo.Capabilities.Completions {
					caps = append(caps, "Completions")
				}
				if len(caps) > 0 {
					detailSections = append(detailSections, kitex.Box(kitex.BoxProps{
						Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1),
					},
						kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary)}, kitex.Text("Capabilities:")),
						kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Secondary)}, kitex.Text(strings.Join(caps, ", "))),
					))
				}

				// Safety & Authorization Section
				var safetyBadges []kitex.Node
				if selectedInfo.IsDangerous {
					safetyBadges = append(safetyBadges, kitex.Box(kitex.BoxProps{
						Style: style.S().PaddingHorizontal(1).Background(t.Color.Surface.BaseHover).Foreground(t.Color.Text.Error),
					}, kitex.Text("Dangerous")))
				}
				if selectedInfo.IsReadOnly {
					safetyBadges = append(safetyBadges, kitex.Box(kitex.BoxProps{
						Style: style.S().PaddingHorizontal(1).Background(t.Color.Surface.BaseHover).Foreground(t.Color.Surface.Success),
					}, kitex.Text("Read-Only")))
				}
				if selectedInfo.IsOpenWorld {
					safetyBadges = append(safetyBadges, kitex.Box(kitex.BoxProps{
						Style: style.S().PaddingHorizontal(1).Background(t.Color.Surface.BaseHover).Foreground(t.Color.Surface.Info),
					}, kitex.Text("Open-World")))
				}
				if selectedInfo.IsIdempotent {
					safetyBadges = append(safetyBadges, kitex.Box(kitex.BoxProps{
						Style: style.S().PaddingHorizontal(1).Background(t.Color.Surface.BaseHover).Foreground(t.Color.Surface.Primary),
					}, kitex.Text("Idempotent")))
				}

				var hasSafetyInfo = len(safetyBadges) > 0 || selectedInfo.UserHint != ""
				if hasSafetyInfo {
					detailSections = append(detailSections, sectionHeader("Safety & Authorization"))
					if len(safetyBadges) > 0 {
						detailSections = append(detailSections, kitex.Box(kitex.BoxProps{
							Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1),
						},
							append([]kitex.Node{
								kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary)}, kitex.Text("Default Profile:")),
							}, safetyBadges...)...,
						))
					}
					if selectedInfo.UserHint != "" {
						detailSections = append(detailSections, kitex.Box(kitex.BoxProps{
							Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1),
						},
							kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary)}, kitex.Text("User Hint:")),
							kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Secondary).Italic(true)}, kitex.Text(selectedInfo.UserHint)),
						))
					}
				}

				// Error Alert
				if selectedInfo.Error != "" {
					detailSections = append(detailSections, kitex.Box(kitex.BoxProps{
						Style: style.S().MarginTop(1).MarginBottom(0),
					}, components.Alert(components.AlertProps{
						Severity: components.AlertError,
						Children: []kitex.Node{
							kitex.Text(selectedInfo.Error),
						},
					})))
				}

				// Exposed Tools list
				detailSections = append(detailSections, sectionHeader(fmt.Sprintf("Exposed Tools (%d)", len(selectedInfo.Tools))))

				if len(selectedInfo.Tools) == 0 {
					detailSections = append(detailSections, kitex.Box(kitex.BoxProps{
						Style: style.S().Padding(1).Foreground(t.Color.Text.Tertiary),
					}, kitex.Text("No tools exposed by this server.")))
				} else {
					var toolItems []kitex.Node
					for _, tl := range selectedInfo.Tools {
						tl := tl
						toolItems = append(toolItems, components.Accordion(components.AccordionProps{
							Color:           components.PaperHover,
							Variant:         components.PaperDefault,
							DefaultExpanded: false,
							Style:           style.S().MarginBottom(0),
						},
							components.AccordionSummary(components.AccordionSummaryProps{},
								kitex.Box(kitex.BoxProps{
									Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).AlignItems(style.AlignCenter).Gap(1),
								},
									kitex.Box(kitex.BoxProps{
										Style: style.S().Bold(true).Foreground(t.Color.Text.Primary),
									}, kitex.Text(cleanToolName(tl.Name))),
									kitex.If(tl.IsDangerous, func() kitex.Node {
										return kitex.Box(kitex.BoxProps{
											Style: style.S().PaddingHorizontal(1).Background(t.Color.Surface.BaseHover).Foreground(t.Color.Text.Error),
										}, kitex.Text("Dangerous"))
									}),
									kitex.If(tl.IsReadOnly, func() kitex.Node {
										return kitex.Box(kitex.BoxProps{
											Style: style.S().PaddingHorizontal(1).Background(t.Color.Surface.BaseHover).Foreground(t.Color.Surface.Success),
										}, kitex.Text("Read-Only"))
									}),
									kitex.If(tl.IsOpenWorld, func() kitex.Node {
										return kitex.Box(kitex.BoxProps{
											Style: style.S().PaddingHorizontal(1).Background(t.Color.Surface.BaseHover).Foreground(t.Color.Surface.Info),
										}, kitex.Text("Open-World"))
									}),
									kitex.If(tl.IsIdempotent, func() kitex.Node {
										return kitex.Box(kitex.BoxProps{
											Style: style.S().PaddingHorizontal(1).Background(t.Color.Surface.BaseHover).Foreground(t.Color.Surface.Primary),
										}, kitex.Text("Idempotent"))
									}),
								),
							),
							components.AccordionDetails(components.AccordionDetailsProps{},
								kitex.Box(kitex.BoxProps{
									Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Gap(1),
								},
									kitex.Box(kitex.BoxProps{
										Style: style.S().Foreground(t.Color.Text.Secondary),
									}, kitex.Text(tl.Description)),
									kitex.Box(kitex.BoxProps{
										Style: style.S().Foreground(t.Color.Text.Tertiary),
									}, kitex.Text("Full Name: "+tl.Name)),
									kitex.If(tl.UserHint != "", func() kitex.Node {
										return kitex.Box(kitex.BoxProps{
											Style: style.S().Foreground(t.Color.Text.Secondary).Italic(true),
										}, kitex.Text("Hint: "+tl.UserHint))
									}),
								),
							),
						))
					}

					detailSections = append(detailSections, kitex.Box(kitex.BoxProps{
						Style: style.S().
							Display(style.DisplayFlex).
							FlexDirection(style.FlexColumn).
							OverflowY(style.OverflowAuto).
							Flex(1, 1, style.Cells(0)).
							MinHeight(style.Cells(0)),
					}, toolItems...))
				}

			} else if isResourcesTab {
				var resourceItems []kitex.Node
				// Regular Resources
				for _, r := range selectedInfo.Resources {
					r := r
					displayName := r.Name
					if r.Title != "" {
						displayName = r.Title
					}
					resourceItems = append(resourceItems, components.Accordion(components.AccordionProps{
						Color:           components.PaperHover,
						Variant:         components.PaperDefault,
						DefaultExpanded: false,
						Style:           style.S().MarginBottom(0),
					},
						components.AccordionSummary(components.AccordionSummaryProps{},
							kitex.Box(kitex.BoxProps{
								Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).AlignItems(style.AlignCenter).Gap(1),
							},
								kitex.Box(kitex.BoxProps{
									Style: style.S().Bold(true).Foreground(t.Color.Text.Primary),
								}, kitex.Text(displayName)),
								kitex.If(r.MIMEType != "", func() kitex.Node {
									return kitex.Box(kitex.BoxProps{
										Style: style.S().PaddingHorizontal(1).Background(t.Color.Surface.BaseHover).Foreground(t.Color.Text.Secondary),
									}, kitex.Text(r.MIMEType))
								}),
							),
						),
						components.AccordionDetails(components.AccordionDetailsProps{},
							kitex.Box(kitex.BoxProps{
								Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Gap(0),
							},
								kitex.If(r.Description != "", func() kitex.Node {
									return kitex.Box(kitex.BoxProps{
										Style: style.S().Foreground(t.Color.Text.Secondary),
									}, kitex.Text(r.Description))
								}),
								kitex.Box(kitex.BoxProps{
									Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1),
								},
									kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary)}, kitex.Text("URI:")),
									kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Secondary)}, kitex.Text(r.URI)),
								),
							),
						),
					))
				}

				// Resource Templates
				for _, rt := range selectedInfo.ResourceTemplates {
					rt := rt
					displayName := rt.Name
					if rt.Title != "" {
						displayName = rt.Title
					}
					resourceItems = append(resourceItems, components.Accordion(components.AccordionProps{
						Color:           components.PaperHover,
						Variant:         components.PaperDefault,
						DefaultExpanded: false,
						Style:           style.S().MarginBottom(0),
					},
						components.AccordionSummary(components.AccordionSummaryProps{},
							kitex.Box(kitex.BoxProps{
								Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).AlignItems(style.AlignCenter).Gap(1),
							},
								kitex.Box(kitex.BoxProps{
									Style: style.S().Bold(true).Foreground(t.Color.Text.Primary),
								}, kitex.Text(displayName)),
								kitex.Box(kitex.BoxProps{
									Style: style.S().PaddingHorizontal(1).Background(t.Color.Surface.BaseHover).Foreground(t.Color.Surface.Info),
								}, kitex.Text("Template")),
								kitex.If(rt.MIMEType != "", func() kitex.Node {
									return kitex.Box(kitex.BoxProps{
										Style: style.S().PaddingHorizontal(1).Background(t.Color.Surface.BaseHover).Foreground(t.Color.Text.Secondary),
									}, kitex.Text(rt.MIMEType))
								}),
							),
						),
						components.AccordionDetails(components.AccordionDetailsProps{},
							kitex.Box(kitex.BoxProps{
								Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Gap(0),
							},
								kitex.If(rt.Description != "", func() kitex.Node {
									return kitex.Box(kitex.BoxProps{
										Style: style.S().Foreground(t.Color.Text.Secondary),
									}, kitex.Text(rt.Description))
								}),
								kitex.Box(kitex.BoxProps{
									Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1),
								},
									kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary)}, kitex.Text("URI Template:")),
									kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Secondary)}, kitex.Text(rt.URITemplate)),
								),
							),
						),
					))
				}

				if len(resourceItems) == 0 {
					detailSections = append(detailSections, kitex.Box(kitex.BoxProps{
						Style: style.S().Padding(1).Foreground(t.Color.Text.Tertiary),
					}, kitex.Text("No resources exposed by this server.")))
				} else {
					detailSections = append(detailSections, kitex.Box(kitex.BoxProps{
						Style: style.S().
							Display(style.DisplayFlex).
							FlexDirection(style.FlexColumn).
							OverflowY(style.OverflowAuto).
							Flex(1, 1, style.Cells(0)).
							MinHeight(style.Cells(0)),
					}, resourceItems...))
				}

			} else if isPromptsTab {
				var promptItems []kitex.Node
				for _, p := range selectedInfo.Prompts {
					p := p
					displayName := p.Name
					if p.Title != "" {
						displayName = p.Title
					}

					var argNodes []kitex.Node
					if len(p.Arguments) > 0 {
						argNodes = append(argNodes, kitex.Box(kitex.BoxProps{
							Style: style.S().Bold(true).Foreground(t.Color.Text.Secondary).MarginTop(1),
						}, kitex.Text("Arguments:")))

						for _, arg := range p.Arguments {
							reqStr := ""
							if arg.Required {
								reqStr = " (Required)"
							}
							descStr := ""
							if arg.Description != "" {
								descStr = " - " + arg.Description
							}
							argName := arg.Name
							if arg.Title != "" {
								argName = arg.Title
							}
							argNodes = append(argNodes, kitex.Box(kitex.BoxProps{
								Style: style.S().PaddingLeft(2),
							},
								kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Secondary)},
									kitex.Text(fmt.Sprintf("• %s%s%s", argName, reqStr, descStr)),
								),
							))
						}
					}

					promptItems = append(promptItems, components.Accordion(components.AccordionProps{
						Color:           components.PaperHover,
						Variant:         components.PaperDefault,
						DefaultExpanded: false,
						Style:           style.S().MarginBottom(0),
					},
						components.AccordionSummary(components.AccordionSummaryProps{},
							kitex.Box(kitex.BoxProps{
								Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).AlignItems(style.AlignCenter).Gap(1),
							},
								kitex.Box(kitex.BoxProps{
									Style: style.S().Bold(true).Foreground(t.Color.Text.Primary),
								}, kitex.Text(displayName)),
							),
						),
						components.AccordionDetails(components.AccordionDetailsProps{},
							kitex.Box(kitex.BoxProps{
								Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Gap(0),
							},
								kitex.If(p.Description != "", func() kitex.Node {
									return kitex.Box(kitex.BoxProps{
										Style: style.S().Foreground(t.Color.Text.Secondary),
									}, kitex.Text(p.Description))
								}),
								kitex.Box(kitex.BoxProps{
									Style: style.S().Foreground(t.Color.Text.Tertiary),
								}, kitex.Text("Full Name: "+p.Name)),
								kitex.Box(kitex.BoxProps{
									Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Gap(0),
								}, argNodes...),
							),
						),
					))
				}

				if len(promptItems) == 0 {
					detailSections = append(detailSections, kitex.Box(kitex.BoxProps{
						Style: style.S().Padding(1).Foreground(t.Color.Text.Tertiary),
					}, kitex.Text("No prompts exposed by this server.")))
				} else {
					detailSections = append(detailSections, kitex.Box(kitex.BoxProps{
						Style: style.S().
							Display(style.DisplayFlex).
							FlexDirection(style.FlexColumn).
							OverflowY(style.OverflowAuto).
							Flex(1, 1, style.Cells(0)).
							MinHeight(style.Cells(0)),
					}, promptItems...))
				}

			} else if isConfigTab {
				// Connection Context Fields
				var configFields []kitex.Node
				configFields = append(configFields, kitex.Box(kitex.BoxProps{
					Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1),
				},
					kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary)}, kitex.Text("Transport:")),
					kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Secondary)}, kitex.Text(selectedInfo.Type)),
				))

				if len(selectedInfo.Command) > 0 {
					configFields = append(configFields, kitex.Box(kitex.BoxProps{
						Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1),
					},
						kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary)}, kitex.Text("Command:")),
						kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Secondary)}, kitex.Text(strings.Join(selectedInfo.Command, " "))),
					))
				} else if selectedInfo.URL != "" {
					configFields = append(configFields, kitex.Box(kitex.BoxProps{
						Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1),
					},
						kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary)}, kitex.Text("URL:")),
						kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Secondary)}, kitex.Text(selectedInfo.URL)),
					))
				}
				if len(selectedInfo.EnvKeys) > 0 {
					configFields = append(configFields, kitex.Box(kitex.BoxProps{
						Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1),
					},
						kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary)}, kitex.Text("Env Keys:")),
						kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Secondary)}, kitex.Text(strings.Join(selectedInfo.EnvKeys, ", "))),
					))
				}

				detailSections = append(detailSections, sectionHeader("Connection Context"))
				detailSections = append(detailSections, kitex.Box(kitex.BoxProps{
					Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Gap(0),
				}, configFields...))

				// Render YAML workspace config spec using chroma CodeBlock
				detailSections = append(detailSections, sectionHeader("Workspace YAML Config"))
				detailSections = append(detailSections, components.CodeBlock(components.CodeBlockProps{
					Code:            selectedInfo.Config,
					Lang:            "yaml",
					HideHeader:      true,
					Compact:         true,
					ShowLineNumbers: false,
				}))
			}

			rightPanel = kitex.Box(kitex.BoxProps{
				Style: style.S().
					Flex(1, 1, style.Cells(0)).
					Width(style.Percent(100)).
					MinWidth(style.Cells(0)).
					Display(style.DisplayFlex).
					FlexDirection(style.FlexColumn).
					PaddingLeft(1).
					Height(style.Percent(100)),
			}, detailSections...)
		}

		content = kitex.Box(kitex.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow).
				Height(style.Percent(100)).
				Width(style.Percent(100)),
		},
			leftPanel,
			// Vertical border separator
			kitex.Box(kitex.BoxProps{
				Style: style.S().
					Width(style.Cells(1)).
					MarginHorizontal(1).
					BorderLeft(true, style.SingleBorder(), t.Color.Border.Primary),
			}),
			rightPanel,
		)
	}

	var restartBtnLabel = "Restart Server"
	if selectedServer() != "" {
		restartBtnLabel = fmt.Sprintf("Restart %s", selectedServer())
	}

	footer := kitex.Box(kitex.BoxProps{
		Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1).JustifyContent(style.JustifyEnd).MarginTop(1),
	},
		components.Button(components.ButtonProps{
			Variant: components.ButtonSolid,
			Color:   components.ButtonPrimary,
			OnClick: func() {
				srv := selectedServer()
				if srv == "" {
					return
				}
				setRestarting(true)
				go func() {
					_, _ = client.RestartMcp(context.Background(), api.RestartMcpRequest{
						ServerName: srv,
					})
					kitex.PostMacro(func() {
						setRestarting(false)
						statusQuery.Refetch()
					})
				}()
			},
			Disabled: isRestarting() || selectedServer() == "" || statusQuery.Data == nil,
		}, kitex.Text(restartBtnLabel)),
		components.Button(components.ButtonProps{
			Variant: components.ButtonText,
			OnClick: func() {
				active.SetModal("")
			},
		}, kitex.Text("Close")),
	)

	return components.Modal(components.ModalProps{
		IsOpen: true,
		Title:  kitex.Text("Model Context Protocol (MCP) Info"),
		OnClose: func() {
			active.SetModal("")
		},
	},
		kitex.Box(kitex.BoxProps{
			Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Height(style.Percent(100)),
		},
			kitex.Box(kitex.BoxProps{
				Style: style.S().Flex(1, 1, style.Cells(0)).Height(style.Percent(100)).Width(style.Percent(100)),
			}, content),
			footer,
		),
	)
})
