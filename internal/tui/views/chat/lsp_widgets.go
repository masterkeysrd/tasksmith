package chat

import (
	"fmt"
	"image/color"
	"path/filepath"
	"strings"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/agent/tools"
	"github.com/masterkeysrd/tasksmith/internal/api"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/components/icon"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

type LspSuggestionWidgetProps struct {
	Suggestions []api.LspSuggestion
	OnConfigure func(lang string)
	OnDismiss   func(lang string)
}

var LspSuggestionWidget = kitex.FC("LspSuggestionWidget", func(props LspSuggestionWidgetProps) kitex.Node {
	if len(props.Suggestions) == 0 {
		return nil
	}

	var boxes []kitex.Node
	for _, sug := range props.Suggestions {
		sugLang := sug.Language // capture loop variable
		boxes = append(boxes, kitex.Box(kitex.BoxProps{
			Style: style.S().
				MarginBottom(1).
				Width(style.Percent(100)).
				MaxWidth(style.Percent(90)),
		},
			components.Alert(components.AlertProps{
				Severity: components.AlertInfo,
				Variant:  components.AlertOutlined,
				ShowIcon: true,
				Action: kitex.Box(kitex.BoxProps{
					Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1),
				},
					components.Button(components.ButtonProps{
						Variant: components.ButtonSolid,
						Color:   components.ButtonInfo,
						OnClick: func() { props.OnConfigure(sugLang) },
					}, kitex.Text("Configure")),
					components.Button(components.ButtonProps{
						Variant: components.ButtonText,
						Color:   components.ButtonBase,
						OnClick: func() { props.OnDismiss(sugLang) },
					}, kitex.Text("Dismiss")),
				),
			}, kitex.Text(fmt.Sprintf("Enable %s language server for %s?", sug.ServerName, sug.Language))),
		))
	}

	return kitex.Box(kitex.BoxProps{
		Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).MarginTop(1).MarginBottom(1).AlignSelf(style.AlignStart).Width(style.Percent(100)),
	}, boxes...)
})

// LspSymbolsToolWidget renders the result of an lsp_symbols tool call inline.
var LspSymbolsToolWidget = kitex.FC("LspSymbolsToolWidget", func(props ToolExecutionProps) kitex.Node {
	t := theme.UseTheme()
	showModal, setShowModal := kitex.UseState(false)

	tc := props.ToolCall
	tm := props.ToolMessage

	var query string
	if tc.Args != nil {
		query, _ = tc.Args["query"].(string)
	}

	var statusLabel string
	var labelNode kitex.Node
	var iconNode kitex.Node
	var themeColor color.Color

	var results []tools.LspSymbolsOutputResultsItem
	var details string

	if t != nil {
		if tm == nil {
			statusLabel = fmt.Sprintf("Searching for %s", query)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, kitex.Text(props.CurrentDots))
			themeColor = t.Color.Surface.Info
		} else if tm.IsError {
			statusLabel = fmt.Sprintf("Error searching for %s", query)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Error)}, icon.Error)
			themeColor = t.Color.Text.Error
			details = getToolOutput(tm.Content)
		} else {
			results = parseLspSymbolsOutput(tm.StructuredContent)
			if len(results) > 0 {
				baseFocusBg := t.Color.Surface.BaseFocus
				searchIconColor := t.Color.Surface.Info
				labelNode = kitex.Box(kitex.BoxProps{
					Style: style.S().
						Display(style.DisplayFlex).
						FlexDirection(style.FlexRow).
						AlignItems(style.AlignCenter).
						Bold(true),
				},
					kitex.Span(kitex.SpanProps{}, kitex.Text(fmt.Sprintf("Found %d symbols for ", len(results)))),
					kitex.Box(kitex.BoxProps{
						Style: style.S().
							Display(style.DisplayFlex).
							FlexDirection(style.FlexRow).
							AlignItems(style.AlignCenter).
							Background(baseFocusBg).
							PaddingHorizontal(1).
							Gap(1),
					},
						kitex.Span(kitex.SpanProps{Style: style.S().Foreground(searchIconColor)}, icon.Search),
						kitex.Span(kitex.SpanProps{}, kitex.Text(query)),
					),
				)
				iconNode = nil // remove checkmark completely on success
				themeColor = color.RGBA{R: 255, G: 255, B: 255, A: 255}
			} else {
				statusLabel = fmt.Sprintf("No symbols found for %s", query)
				iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Secondary)}, icon.Info)
				themeColor = t.Color.Text.Secondary
			}
		}
	}

	var onClick func()
	if tm != nil && (tm.IsError || len(results) > 0) {
		onClick = func() { setShowModal(true) }
	}

	badgeNode := components.ToolBadge(components.ToolBadgeProps{
		Icon:      iconNode,
		Label:     statusLabel,
		LabelNode: labelNode,
		Color:     themeColor,
		OnClick:   onClick,
	})

	return kitex.Fragment(
		badgeNode,
		components.Modal(components.ModalProps{
			IsOpen: showModal(),
			Title: kitex.Box(kitex.BoxProps{
				Style: style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexRow).
					AlignItems(style.AlignCenter).
					Gap(1),
			},
				kitex.If(t != nil && tm != nil && tm.IsError, func() kitex.Node {
					return kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Error)}, icon.Error)
				}),
				kitex.If(tm != nil && tm.IsError, func() kitex.Node {
					return kitex.Span(kitex.SpanProps{}, kitex.Text(fmt.Sprintf("LSP Search Error for %s", query)))
				}),
				kitex.If(tm != nil && !tm.IsError, func() kitex.Node {
					return kitex.Span(kitex.SpanProps{}, kitex.Text(fmt.Sprintf("Found %d symbols for %s", len(results), query)))
				}),
			),
			OnClose: func() { setShowModal(false) },
		},
			kitex.If(showModal() && tm != nil && tm.IsError && details != "", func() kitex.Node {
				return kitex.Box(kitex.BoxProps{
					Style: style.S().
						Padding(1).
						Width(style.Percent(100)).
						MinWidth(style.Percent(0)).
						Foreground(t.Color.Text.Secondary).
						WhiteSpace(style.WhiteSpacePreWrap),
				}, kitex.Text(details))
			}),
			kitex.If(showModal() && tm != nil && !tm.IsError && len(results) > 0, func() kitex.Node {
				var rows []kitex.Node
				for _, item := range results {
					dirPart := filepath.Dir(item.Path)
					filePart := filepath.Base(item.Path)
					if dirPart == "." {
						dirPart = ""
					} else if !strings.HasSuffix(dirPart, "/") {
						dirPart += "/"
					}

					var kindIconNode kitex.Node
					var kindColor color.Color
					if t != nil {
						kindIconNode, kindColor = icon.LspKindIcon(item.Kind, t)
					}

					rows = append(rows, kitex.Box(kitex.BoxProps{
						Style: style.S().
							Display(style.DisplayFlex).
							FlexDirection(style.FlexColumn).
							Padding(0, 1).
							Background(t.Color.Surface.BaseHover).
							BorderLeft(true, style.SingleBorder(), t.Color.Border.Primary),
					},
						kitex.Box(kitex.BoxProps{
							Style: style.S().
								Display(style.DisplayFlex).
								FlexDirection(style.FlexRow).
								AlignItems(style.AlignCenter).
								Gap(1),
						},
							kitex.If(kindIconNode != nil, func() kitex.Node {
								return kindIconNode
							}),
							kitex.Box(kitex.BoxProps{Style: style.S().Foreground(kindColor).Bold(true)}, kitex.Text(strings.ToUpper(item.Kind))),
							kitex.Box(kitex.BoxProps{Style: style.S().Bold(true).Foreground(t.Color.Text.Primary)}, kitex.Text(item.Name)),
							kitex.If(item.ContainerName != "", func() kitex.Node {
								return kitex.Box(kitex.BoxProps{Style: style.S().Foreground(t.Color.Text.Tertiary)}, kitex.Text("in "+item.ContainerName))
							}),
						),
						kitex.Box(kitex.BoxProps{
							Style: style.S().
								Display(style.DisplayFlex).
								FlexDirection(style.FlexRow).
								AlignItems(style.AlignCenter),
						},
							kitex.Span(kitex.SpanProps{Style: style.S().MarginRight(1)}, icon.FileIcon(icon.FileIconProps{Path: item.Path})),
							kitex.If(dirPart != "", func() kitex.Node {
								return kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Secondary)}, kitex.Text(dirPart))
							}),
							kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Primary)}, kitex.Text(filePart)),
							kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Tertiary)}, kitex.Text(":")),
							kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Purple)}, kitex.Text(fmt.Sprintf("%d", item.Range.Start.Line+1))),
							kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Tertiary)}, kitex.Text(":")),
							kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Secondary)}, kitex.Text(fmt.Sprintf("%d", item.Range.Start.Character+1))),
						),
					))
				}
				return kitex.Box(kitex.BoxProps{
					Style: style.S().
						Display(style.DisplayFlex).
						FlexDirection(style.FlexColumn).
						Gap(1).
						Padding(1).
						Width(style.Percent(100)).
						MinWidth(style.Percent(0)),
				}, rows...)
			}),
		),
	)
})

// LspRestartToolWidget renders the result of an lsp_restart tool call inline.
var LspRestartToolWidget = kitex.FC("LspRestartToolWidget", func(props ToolExecutionProps) kitex.Node {
	t := theme.UseTheme()
	showModal, setShowModal := kitex.UseState(false)

	tc := props.ToolCall
	tm := props.ToolMessage

	var serverName string
	if tc.Args != nil {
		serverName, _ = tc.Args["server"].(string)
	}

	var statusLabel string
	var labelNode kitex.Node
	var iconNode kitex.Node
	var themeColor color.Color
	var details string
	isError := false

	if t != nil {
		if tm == nil {
			statusLabel = "Restarting"
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, kitex.Text(props.CurrentDots))
			themeColor = t.Color.Surface.Info
		} else if tm.IsError {
			statusLabel = "Failed to restart"
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Error)}, icon.Error)
			themeColor = t.Color.Text.Error
			details = getToolOutput(tm.Content)
			isError = true
		} else {
			out, _ := parseLspRestartOutput(tm.StructuredContent)
			details = out.Message
			if out.Success {
				baseFocusBg := t.Color.Surface.BaseFocus
				serverIconColor := t.Color.Surface.Info
				labelNode = kitex.Box(kitex.BoxProps{
					Style: style.S().
						Display(style.DisplayFlex).
						FlexDirection(style.FlexRow).
						AlignItems(style.AlignCenter).
						Bold(true),
				},
					kitex.Span(kitex.SpanProps{}, kitex.Text("Successfully restarted ")),
					kitex.Box(kitex.BoxProps{
						Style: style.S().
							Display(style.DisplayFlex).
							FlexDirection(style.FlexRow).
							AlignItems(style.AlignCenter).
							Background(baseFocusBg).
							PaddingHorizontal(1).
							Gap(1),
					},
						kitex.Span(kitex.SpanProps{Style: style.S().Foreground(serverIconColor)}, icon.Server),
						kitex.Span(kitex.SpanProps{}, kitex.Text(serverName)),
					),
				)
				iconNode = nil // Remove the checkmark completely on success
				themeColor = color.RGBA{R: 255, G: 255, B: 255, A: 255}
			} else {
				statusLabel = "Failed to restart"
				iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Error)}, icon.Error)
				themeColor = t.Color.Text.Error
				isError = true
			}
		}
	}

	var onClick func()
	if tm != nil && (tm.IsError || details != "") {
		onClick = func() { setShowModal(true) }
	}

	badgeNode := components.ToolBadge(components.ToolBadgeProps{
		Icon:      iconNode,
		Label:     statusLabel,
		LabelNode: labelNode,
		Color:     themeColor,
		OnClick:   onClick,
	})

	return kitex.Fragment(
		badgeNode,
		components.Modal(components.ModalProps{
			IsOpen: showModal(),
			Title: kitex.Box(kitex.BoxProps{
				Style: style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexRow).
					AlignItems(style.AlignCenter).
					Gap(1),
			},
				kitex.If(t != nil && isError, func() kitex.Node {
					return kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Error)}, icon.Error)
				}),
				kitex.If(isError, func() kitex.Node {
					return kitex.Span(kitex.SpanProps{}, kitex.Text(fmt.Sprintf("Failed to restart %s", serverName)))
				}),
				kitex.If(!isError, func() kitex.Node {
					return kitex.Span(kitex.SpanProps{}, kitex.Text(fmt.Sprintf("Successfully restarted %s", serverName)))
				}),
			),
			OnClose: func() { setShowModal(false) },
		},
			kitex.If(showModal() && details != "", func() kitex.Node {
				return kitex.Box(kitex.BoxProps{
					Style: style.S().
						Padding(1).
						Width(style.Percent(100)).
						MinWidth(style.Percent(0)).
						Foreground(t.Color.Text.Secondary).
						WhiteSpace(style.WhiteSpacePreWrap),
				}, kitex.Text(details))
			}),
		),
	)
})

// LspDiagnosticsToolWidget renders the result of an lsp_diagnostics tool call inline.
var LspDiagnosticsToolWidget = kitex.FC("LspDiagnosticsToolWidget", func(props ToolExecutionProps) kitex.Node {
	t := theme.UseTheme()
	showModal, setShowModal := kitex.UseState(false)

	tc := props.ToolCall
	tm := props.ToolMessage

	var targetPath string
	if tc.Args != nil {
		targetPath, _ = tc.Args["path"].(string)
	}

	folderName := filepath.Base(targetPath)
	if folderName == "." || folderName == "/" {
		folderName = targetPath
	}

	var statusLabel string
	var labelNode kitex.Node
	var iconNode kitex.Node
	var themeColor color.Color

	var diags []tools.LspDiagnosticsOutputDiagnosticsItem
	var totalCount int
	var truncated bool
	var details string

	if t != nil {
		if tm == nil {
			statusLabel = fmt.Sprintf("Fetching diagnostics for %s", folderName)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, kitex.Text(props.CurrentDots))
			themeColor = t.Color.Surface.Info
		} else if tm.IsError {
			statusLabel = fmt.Sprintf("Error fetching diagnostics for %s", folderName)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Error)}, icon.Error)
			themeColor = t.Color.Text.Error
			details = getToolOutput(tm.Content)
		} else {
			diags, totalCount, truncated = parseLspDiagnosticsOutput(tm.StructuredContent)
			if totalCount > 0 {
				baseFocusBg := t.Color.Surface.BaseFocus
				folderIconColor := t.Color.Surface.Info
				labelNode = kitex.Box(kitex.BoxProps{
					Style: style.S().
						Display(style.DisplayFlex).
						FlexDirection(style.FlexRow).
						AlignItems(style.AlignCenter).
						Bold(true),
				},
					kitex.Span(kitex.SpanProps{}, kitex.Text(fmt.Sprintf("Found %d diagnostics for ", totalCount))),
					kitex.Box(kitex.BoxProps{
						Style: style.S().
							Display(style.DisplayFlex).
							FlexDirection(style.FlexRow).
							AlignItems(style.AlignCenter).
							Background(baseFocusBg).
							PaddingHorizontal(1).
							Gap(1),
					},
						kitex.Span(kitex.SpanProps{Style: style.S().Foreground(folderIconColor)}, icon.Folder),
						kitex.Span(kitex.SpanProps{}, kitex.Text(folderName)),
					),
				)
				iconNode = nil // remove checkmark completely on success
				themeColor = color.RGBA{R: 255, G: 255, B: 255, A: 255}
			} else {
				statusLabel = fmt.Sprintf("No diagnostics found for %s", folderName)
				iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Success)}, icon.Checkmark)
				themeColor = t.Color.Surface.Success
			}
		}
	}

	var onClick func()
	if tm != nil && (tm.IsError || totalCount > 0) {
		onClick = func() { setShowModal(true) }
	}

	badgeNode := components.ToolBadge(components.ToolBadgeProps{
		Icon:      iconNode,
		Label:     statusLabel,
		LabelNode: labelNode,
		Color:     themeColor,
		OnClick:   onClick,
	})

	return kitex.Fragment(
		badgeNode,
		components.Modal(components.ModalProps{
			IsOpen: showModal(),
			Title: kitex.Box(kitex.BoxProps{
				Style: style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexRow).
					AlignItems(style.AlignCenter).
					Gap(1),
			},
				kitex.If(t != nil && tm != nil && tm.IsError, func() kitex.Node {
					return kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Error)}, icon.Error)
				}),
				kitex.If(tm != nil && tm.IsError, func() kitex.Node {
					return kitex.Span(kitex.SpanProps{}, kitex.Text(fmt.Sprintf("LSP Diagnostics Error for %s", folderName)))
				}),
				kitex.If(tm != nil && !tm.IsError, func() kitex.Node {
					return kitex.Span(kitex.SpanProps{}, kitex.Text(fmt.Sprintf("Found %d diagnostics for %s", totalCount, folderName)))
				}),
			),
			OnClose: func() { setShowModal(false) },
		},
			kitex.If(showModal() && tm != nil && tm.IsError && details != "", func() kitex.Node {
				return kitex.Box(kitex.BoxProps{
					Style: style.S().
						Padding(1).
						Width(style.Percent(100)).
						MinWidth(style.Percent(0)).
						Foreground(t.Color.Text.Secondary).
						WhiteSpace(style.WhiteSpacePreWrap),
				}, kitex.Text(details))
			}),
			kitex.If(showModal() && tm != nil && !tm.IsError && len(diags) > 0, func() kitex.Node {
				var rows []kitex.Node
				for _, d := range diags {
					var severityColor color.Color
					var severityIcon kitex.Node
					var severityName string

					switch d.Severity {
					case "error":
						severityColor = t.Color.Text.Error
						severityIcon = icon.Error
						severityName = "ERROR"
					case "warning":
						severityColor = t.Color.Text.Purple
						severityIcon = icon.Warning
						severityName = "WARNING"
					case "info":
						severityColor = t.Color.Surface.Info
						if severityColor == nil {
							severityColor = t.Color.Text.Secondary
						}
						severityIcon = icon.Info
						severityName = "INFO"
					default:
						severityColor = t.Color.Text.Tertiary
						severityIcon = icon.Info
						severityName = "HINT"
					}

					dirPart := filepath.Dir(d.Path)
					filePart := filepath.Base(d.Path)
					if dirPart == "." {
						dirPart = ""
					} else if !strings.HasSuffix(dirPart, "/") {
						dirPart += "/"
					}

					rows = append(rows, kitex.Box(kitex.BoxProps{
						Style: style.S().
							Display(style.DisplayFlex).
							FlexDirection(style.FlexColumn).
							Padding(0, 1).
							BorderLeft(true, style.SingleBorder(), severityColor).
							Background(t.Color.Surface.BaseHover),
					},
						kitex.Box(kitex.BoxProps{
							Style: style.S().
								Display(style.DisplayFlex).
								FlexDirection(style.FlexRow).
								AlignItems(style.AlignCenter).
								Gap(1),
						},
							kitex.Box(kitex.BoxProps{Style: style.S().Foreground(severityColor).Bold(true)}, severityIcon),
							kitex.Box(kitex.BoxProps{Style: style.S().Foreground(severityColor).Bold(true)}, kitex.Text(severityName)),
							kitex.Box(kitex.BoxProps{
								Style: style.S().
									Display(style.DisplayFlex).
									FlexDirection(style.FlexRow).
									AlignItems(style.AlignCenter),
							},
								kitex.Span(kitex.SpanProps{Style: style.S().MarginRight(1)}, icon.FileIcon(icon.FileIconProps{Path: d.Path})),
								kitex.If(dirPart != "", func() kitex.Node {
									return kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Secondary)}, kitex.Text(dirPart))
								}),
								kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Primary)}, kitex.Text(filePart)),
								kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Tertiary)}, kitex.Text(":")),
								kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Purple)}, kitex.Text(fmt.Sprintf("%d", d.Range.Start.Line+1))),
								kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Tertiary)}, kitex.Text(":")),
								kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Secondary)}, kitex.Text(fmt.Sprintf("%d", d.Range.Start.Character+1))),
							),
						),
						kitex.Box(kitex.BoxProps{
							Style: style.S().Foreground(t.Color.Text.Primary),
						}, kitex.Text(d.Message)),
					))
				}

				if truncated {
					rows = append(rows, kitex.Box(kitex.BoxProps{
						Style: style.S().Padding(1).Foreground(t.Color.Text.Tertiary),
					}, kitex.Text("... diagnostics truncated ...")))
				}

				return kitex.Box(kitex.BoxProps{
					Style: style.S().
						Display(style.DisplayFlex).
						FlexDirection(style.FlexColumn).
						Gap(1).
						Padding(1).
						Width(style.Percent(100)).
						MinWidth(style.Percent(0)),
				}, rows...)
			}),
		),
	)
})

// LspInspectToolWidget renders the result of an lsp_inspect tool call inline.
var LspInspectToolWidget = kitex.FC("LspInspectToolWidget", func(props ToolExecutionProps) kitex.Node {
	t := theme.UseTheme()
	showModal, setShowModal := kitex.UseState(false)

	tc := props.ToolCall
	tm := props.ToolMessage

	var query string
	if tc.Args != nil {
		query, _ = tc.Args["query"].(string)
	}

	var statusLabel string
	var labelNode kitex.Node
	var iconNode kitex.Node
	var themeColor color.Color

	var out tools.LspInspectOutput
	var hasStructured bool
	if tm != nil {
		out, hasStructured = parseLspInspectOutput(tm.StructuredContent)
	}

	hasResult := hasStructured && out.Result.Name != ""
	totalInspected := 0
	if hasResult {
		totalInspected = 1 + len(out.SimilarSymbols)
	}

	if t != nil {
		if tm == nil {
			statusLabel = fmt.Sprintf("Inspecting symbol %s", query)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, kitex.Text(props.CurrentDots))
			themeColor = t.Color.Surface.Info
		} else if tm.IsError {
			statusLabel = fmt.Sprintf("Error inspecting symbol %s", query)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Error)}, icon.Error)
			themeColor = t.Color.Text.Error
		} else {
			if hasResult {
				baseFocusBg := t.Color.Surface.BaseFocus
				searchIconColor := t.Color.Surface.Info
				labelNode = kitex.Box(kitex.BoxProps{
					Style: style.S().
						Display(style.DisplayFlex).
						FlexDirection(style.FlexRow).
						AlignItems(style.AlignCenter).
						Bold(true),
				},
					kitex.Span(kitex.SpanProps{}, kitex.Text(fmt.Sprintf("Inspected %d symbol(s) for ", totalInspected))),
					kitex.Box(kitex.BoxProps{
						Style: style.S().
							Display(style.DisplayFlex).
							FlexDirection(style.FlexRow).
							AlignItems(style.AlignCenter).
							Background(baseFocusBg).
							PaddingHorizontal(1).
							Gap(1),
					},
						kitex.Span(kitex.SpanProps{Style: style.S().Foreground(searchIconColor)}, icon.Search),
						kitex.Span(kitex.SpanProps{}, kitex.Text(query)),
					),
				)
				iconNode = nil
				themeColor = color.RGBA{R: 255, G: 255, B: 255, A: 255}
				statusLabel = fmt.Sprintf("Inspected symbol %s (%d matches)", query, totalInspected)
			} else {
				statusLabel = fmt.Sprintf("No matches found for symbol %s", query)
				iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Secondary)}, icon.Info)
				themeColor = t.Color.Text.Secondary
			}
		}
	}

	var onClick func()
	if tm != nil && (tm.IsError || hasResult) {
		onClick = func() { setShowModal(true) }
	}

	badgeNode := components.ToolBadge(components.ToolBadgeProps{
		Icon:      iconNode,
		Label:     statusLabel,
		LabelNode: labelNode,
		Color:     themeColor,
		OnClick:   onClick,
	})

	var fullReportPath string
	if hasResult {
		fullReportPath = out.Result.FullReportPath
	}

	var markdownSource string
	if showModal() && tm != nil && !tm.IsError && hasResult {
		markdownSource = out.TextContent()
	}

	var details string
	if tm != nil && tm.IsError {
		details = getToolOutput(tm.Content)
	}

	return kitex.Fragment(
		badgeNode,
		components.Modal(components.ModalProps{
			IsOpen: showModal(),
			Title: kitex.Box(kitex.BoxProps{
				Style: style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexRow).
					AlignItems(style.AlignCenter).
					Gap(1),
			},
				kitex.If(t != nil && tm != nil && tm.IsError, func() kitex.Node {
					return kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Error)}, icon.Error)
				}),
				kitex.If(tm != nil && tm.IsError, func() kitex.Node {
					return kitex.Span(kitex.SpanProps{}, kitex.Text(fmt.Sprintf("Inspection Error for %s", query)))
				}),
				kitex.If(tm != nil && !tm.IsError, func() kitex.Node {
					return kitex.Span(kitex.SpanProps{}, kitex.Text(fmt.Sprintf("Inspection Report: %s", query)))
				}),
			),
			OnClose: func() { setShowModal(false) },
		},
			kitex.If(showModal() && tm != nil && tm.IsError && details != "", func() kitex.Node {
				return kitex.Box(kitex.BoxProps{
					Style: style.S().
						Padding(1).
						Width(style.Percent(100)).
						MinWidth(style.Percent(0)).
						Foreground(t.Color.Text.Secondary).
						WhiteSpace(style.WhiteSpacePreWrap),
				}, kitex.Text(details))
			}),
			kitex.If(showModal() && tm != nil && !tm.IsError && markdownSource != "", func() kitex.Node {
				return kitex.Box(kitex.BoxProps{
					Style: style.S().
						Display(style.DisplayFlex).
						FlexDirection(style.FlexColumn).
						Padding(1).
						Width(style.Percent(100)).
						MinWidth(style.Percent(0)),
				},
					kitex.If(fullReportPath != "", func() kitex.Node {
						return components.Button(components.ButtonProps{
							Variant: components.ButtonSolid,
							Color:   components.ButtonPrimary,
							Style: style.S().
								MarginBottom(1).
								Width(style.MaxContent),
							OnClick: func() {
								openWithSystemViewer(fullReportPath)
							},
						}, kitex.Fragment(
							kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Primary)}, icon.Search),
							kitex.Text(" VIEW FULL REPORT"),
						))
					}),
					components.Markdown(components.MarkdownProps{Source: markdownSource}),
				)
			}),
		),
	)
})
