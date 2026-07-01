package chat

import (
	"fmt"
	"image/color"
	"path/filepath"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/agent/tools"
	"github.com/masterkeysrd/tasksmith/internal/api"
	"github.com/masterkeysrd/tasksmith/internal/core/preview"
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

	if t != nil {
		if tm == nil {
			statusLabel = fmt.Sprintf("Searching for %s", query)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, toolPulse())
			themeColor = t.Color.Surface.Info
		} else if tm.IsError {
			statusLabel = fmt.Sprintf("Error searching for %s", query)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Error)}, icon.Error)
			themeColor = t.Color.Text.Error
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
							Gap(1).
							MarginRight(1),
					},
						kitex.Span(kitex.SpanProps{Style: style.S().Foreground(searchIconColor)}, icon.Search),
						kitex.Span(kitex.SpanProps{
							Style: style.S().
								Foreground(color.RGBA{255, 255, 255, 255}).
								Bold(true),
						}, kitex.Text(query)),
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
	if tm != nil && !tm.IsError && len(results) > 0 && props.OnViewPreview != nil {
		onClick = func() {
			props.OnViewPreview(
				fmt.Sprintf("Found %d symbols for %s", len(results), query),
				LspSymbolsPreview{Query: query, Results: results},
			)
		}
	}

	return components.ToolBadge(components.ToolBadgeProps{
		Icon:      iconNode,
		Label:     statusLabel,
		LabelNode: labelNode,
		Color:     themeColor,
		OnClick:   onClick,
	})
})

// LspRestartToolWidget renders the result of an lsp_restart tool call inline.
var LspRestartToolWidget = kitex.FC("LspRestartToolWidget", func(props ToolExecutionProps) kitex.Node {
	t := theme.UseTheme()

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
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, toolPulse())
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
	if tm != nil && (tm.IsError || details != "") && props.OnViewPreview != nil {
		onClick = func() {
			title := fmt.Sprintf("Successfully restarted %s", serverName)
			if isError {
				title = fmt.Sprintf("Failed to restart %s", serverName)
			}
			props.OnViewPreview(
				title,
				preview.DefaultTextPreview{Text: details},
			)
		}
	}

	return components.ToolBadge(components.ToolBadgeProps{
		Icon:      iconNode,
		Label:     statusLabel,
		LabelNode: labelNode,
		Color:     themeColor,
		OnClick:   onClick,
	})
})

// LspDiagnosticsToolWidget renders the result of an lsp_diagnostics tool call inline.
var LspDiagnosticsToolWidget = kitex.FC("LspDiagnosticsToolWidget", func(props ToolExecutionProps) kitex.Node {
	t := theme.UseTheme()

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

	if t != nil {
		if tm == nil {
			statusLabel = fmt.Sprintf("Fetching diagnostics for %s", folderName)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, toolPulse())
			themeColor = t.Color.Surface.Info
		} else if tm.IsError {
			statusLabel = fmt.Sprintf("Error fetching diagnostics for %s", folderName)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Error)}, icon.Error)
			themeColor = t.Color.Text.Error
		} else {
			diags, totalCount, _ = parseLspDiagnosticsOutput(tm.StructuredContent)
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
	if tm != nil && !tm.IsError && totalCount > 0 && props.OnViewPreview != nil {
		onClick = func() {
			props.OnViewPreview(
				fmt.Sprintf("Found %d diagnostics for %s", totalCount, folderName),
				LspDiagnosticsPreview{Path: targetPath, Diagnostics: diags},
			)
		}
	}

	return components.ToolBadge(components.ToolBadgeProps{
		Icon:      iconNode,
		Label:     statusLabel,
		LabelNode: labelNode,
		Color:     themeColor,
		OnClick:   onClick,
	})
})

// LspInspectToolWidget renders the result of an lsp_inspect tool call inline.
var LspInspectToolWidget = kitex.FC("LspInspectToolWidget", func(props ToolExecutionProps) kitex.Node {
	t := theme.UseTheme()

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

	var details string
	isError := false

	if t != nil {
		if tm == nil {
			statusLabel = fmt.Sprintf("Inspecting symbol %s", query)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, toolPulse())
			themeColor = t.Color.Surface.Info
		} else if tm.IsError {
			statusLabel = fmt.Sprintf("Error inspecting symbol %s", query)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Error)}, icon.Error)
			themeColor = t.Color.Text.Error
			details = getToolOutput(tm.Content)
			isError = true
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
	if tm != nil && props.OnViewPreview != nil {
		if isError && details != "" {
			onClick = func() {
				props.OnViewPreview(
					fmt.Sprintf("Inspection Error for %s", query),
					preview.DefaultTextPreview{Text: details},
				)
			}
		} else if hasResult {
			onClick = func() {
				props.OnViewPreview(
					fmt.Sprintf("Inspection Report: %s", query),
					LspInspectPreview{Query: query, Output: out},
				)
			}
		}
	}

	return components.ToolBadge(components.ToolBadgeProps{
		Icon:      iconNode,
		Label:     statusLabel,
		LabelNode: labelNode,
		Color:     themeColor,
		OnClick:   onClick,
	})
})
