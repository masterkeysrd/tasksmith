package chat

import (
	"fmt"
	"image/color"
	"path/filepath"
	"strings"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/agent/tools"
	"github.com/masterkeysrd/tasksmith/internal/core/preview"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/components/icon"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

// WebSearchToolWidget renders the result of a web_search tool call inline.
var WebSearchToolWidget = kitex.FC("WebSearchToolWidget", func(props ToolExecutionProps) kitex.Node {
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
	var details string

	var results []tools.WebSearchOutputResultsItem

	if t != nil {
		var actionText string
		if tm == nil {
			actionText = "Web Search: Searching for "
			statusLabel = fmt.Sprintf("Web Search: Searching for %q", query)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, toolPulse())
			themeColor = t.Color.Surface.Info
		} else if tm.IsError {
			actionText = "Web Search: Error searching for "
			statusLabel = fmt.Sprintf("Web Search: Error searching for %q", query)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Error)}, icon.Error)
			themeColor = t.Color.Text.Error
			details = getToolOutput(tm.Content)
		} else {
			results = parseWebSearchOutput(tm.StructuredContent)
			resWord := "results"
			if len(results) == 1 {
				resWord = "result"
			}
			if len(results) > 0 {
				actionText = fmt.Sprintf("Web Search: Found %d %s for ", len(results), resWord)
				statusLabel = fmt.Sprintf("Web Search: Found %d %s for %q", len(results), resWord, query)
				iconNode = nil // remove checkmark completely on success
				themeColor = color.RGBA{R: 255, G: 255, B: 255, A: 255}
			} else {
				actionText = "Web Search: No results for "
				statusLabel = fmt.Sprintf("Web Search: No results found for %q", query)
				iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Secondary)}, icon.Info)
				themeColor = t.Color.Text.Secondary
			}
		}

		baseFocusBg := t.Color.Surface.BaseFocus
		searchIconColor := t.Color.Surface.Info

		labelNode = kitex.Box(kitex.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow).
				AlignItems(style.AlignCenter),
		},
			kitex.Span(kitex.SpanProps{
				Style: style.S().
					Bold(true).
					Foreground(color.RGBA{255, 255, 255, 255}),
			}, kitex.Text(actionText)),
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
				kitex.Span(kitex.SpanProps{
					Style: style.S().
						Foreground(color.RGBA{255, 255, 255, 255}).
						Bold(true),
				}, kitex.Text(query)),
			),
		)
	}

	var onClick func()
	if tm != nil && props.OnViewPreview != nil && (tm.IsError || len(results) > 0) {
		onClick = func() {
			if tm.IsError {
				props.OnViewPreview(
					fmt.Sprintf("Web Search Error for %q", query),
					preview.DefaultTextPreview{Text: details},
				)
			} else {
				var sb strings.Builder
				fmt.Fprintf(&sb, "## Search Results for %q\n\n", query)
				for i, res := range results {
					fmt.Fprintf(&sb, "### %d. %s\n", i+1, res.Title)
					fmt.Fprintf(&sb, "*URL: [%s](%s)*\n\n", res.Url, res.Url)
					fmt.Fprintf(&sb, "> %s\n\n", res.Snippet)
				}
				props.OnViewPreview(
					fmt.Sprintf("Web Search Results for %q", query),
					preview.MarkdownPreview{Markdown: sb.String()},
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

// WebFetchToolWidget renders the result of a web_fetch tool call inline.
var WebFetchToolWidget = kitex.FC("WebFetchToolWidget", func(props ToolExecutionProps) kitex.Node {
	t := theme.UseTheme()

	tc := props.ToolCall
	tm := props.ToolMessage

	var url string
	if tc.Args != nil {
		url, _ = tc.Args["url"].(string)
	}

	var statusLabel string
	var labelNode kitex.Node
	var iconNode kitex.Node
	var themeColor color.Color
	var details string

	var cleanCode string
	var truncated bool
	var cachedPath string
	var mimeType string
	var isBinary bool
	var title string
	var displayTarget = url

	if tm != nil {
		vOut, ok := parseWebFetchStructuredOutput(tm.StructuredContent)
		if ok {
			cleanCode = vOut.Content
			truncated = vOut.Truncated
			cachedPath = vOut.CachedPath
			mimeType = vOut.MimeType
			isBinary = vOut.IsBinary
			title = vOut.Title
		} else {
			cleanCode = getToolOutput(tm.Content)
		}
		if title != "" {
			displayTarget = title
		}
	}

	if t != nil {
		var actionText string
		if tm == nil {
			actionText = "Fetching "
			statusLabel = fmt.Sprintf("Fetching [%s]", url)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, toolPulse())
			themeColor = t.Color.Surface.Info
		} else if tm.IsError {
			actionText = "Error Fetching "
			statusLabel = fmt.Sprintf("Error Fetching [%s]", url)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Error)}, icon.Error)
			themeColor = t.Color.Text.Error
			details = getToolOutput(tm.Content)
		} else {
			if isBinary {
				actionText = "Fetched Binary "
				statusLabel = fmt.Sprintf("Fetched Binary [%s] (%s)", filepath.Base(url), mimeType)
			} else if title != "" {
				actionText = "Fetched "
				statusLabel = fmt.Sprintf("Fetched [%s]", title)
			} else {
				actionText = "Fetched "
				statusLabel = fmt.Sprintf("Fetched [%s]", url)
			}
			iconNode = nil // remove checkmark completely on success
			themeColor = t.Color.Surface.Success
		}

		baseFocusBg := t.Color.Surface.BaseFocus
		globeIconColor := t.Color.Surface.Info

		labelNode = kitex.Box(kitex.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow).
				AlignItems(style.AlignCenter),
		},
			kitex.Span(kitex.SpanProps{
				Style: style.S().
					Bold(true).
					Foreground(color.RGBA{255, 255, 255, 255}),
			}, kitex.Text(actionText)),
			kitex.Box(kitex.BoxProps{
				Style: style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexRow).
					AlignItems(style.AlignCenter).
					Background(baseFocusBg).
					PaddingHorizontal(1).
					Gap(1),
			},
				kitex.Span(kitex.SpanProps{Style: style.S().Foreground(globeIconColor)}, icon.Globe),
				kitex.Span(kitex.SpanProps{
					Style: style.S().
						Foreground(color.RGBA{255, 255, 255, 255}).
						Bold(true),
				}, kitex.Text(displayTarget)),
			),
		)
	}

	var onClick func()
	if tm != nil && props.OnViewPreview != nil {
		onClick = func() {
			if tm.IsError {
				props.OnViewPreview(
					fmt.Sprintf("Web Fetch Error for %s", url),
					preview.DefaultTextPreview{Text: details},
				)
			} else {
				var contentToView string
				if truncated {
					contentToView = fmt.Sprintf("[TRUNCATED] Content exceeded 16,000 chars. Full saved to: %s\n\n%s", cachedPath, cleanCode)
				} else {
					contentToView = cleanCode
				}
				props.OnViewPreview(
					fmt.Sprintf("Fetched %s", displayTarget),
					preview.FileViewPreview{
						Path:     url,
						Content:  contentToView,
						IsBinary: isBinary,
						MimeType: mimeType,
					},
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

// DownloadToolWidget renders the execution state and results of a download tool call.
var DownloadToolWidget = kitex.FC("DownloadToolWidget", func(props ToolExecutionProps) kitex.Node {
	t := theme.UseTheme()

	tc := props.ToolCall
	tm := props.ToolMessage

	var urlVal string
	var destVal string
	if tc.Args != nil {
		urlVal, _ = tc.Args["url"].(string)
		destVal, _ = tc.Args["destination"].(string)
	}

	var statusLabel string
	var labelNode kitex.Node
	var iconNode kitex.Node
	var themeColor color.Color
	var details string

	var finalDest string
	var sizeStr string
	var bgTaskStr string
	var successVal bool
	var detailsMsg string

	if tm != nil {
		dOut, ok := parseDownloadOutput(tm.StructuredContent)
		if ok {
			finalDest = dOut.Path
			sizeStr = fmt.Sprintf("%.2f MB (%d bytes)", float64(dOut.SizeBytes)/(1024*1024), dOut.SizeBytes)
			bgTaskStr = dOut.TaskId
			successVal = dOut.Success
			detailsMsg = dOut.Message
		}
	}
	if finalDest == "" {
		finalDest = destVal
	}

	if t != nil {
		var actionText string
		if tm == nil {
			actionText = "Downloading "
			statusLabel = fmt.Sprintf("Downloading [%s]", filepath.Base(urlVal))
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, toolPulse())
			themeColor = t.Color.Surface.Info
		} else if tm.IsError {
			actionText = "Download Error "
			statusLabel = fmt.Sprintf("Download Error [%s]", filepath.Base(urlVal))
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Error)}, icon.Error)
			themeColor = t.Color.Text.Error
			details = getToolOutput(tm.Content)
		} else {
			if bgTaskStr != "" {
				actionText = "Download BG Started "
				statusLabel = fmt.Sprintf("Download BG Started [%s]", filepath.Base(urlVal))
				iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, icon.Info)
				themeColor = t.Color.Surface.Info
			} else if successVal {
				actionText = "Downloaded "
				statusLabel = fmt.Sprintf("Downloaded [%s]", filepath.Base(urlVal))
				iconNode = nil // remove checkmark completely on success
				themeColor = t.Color.Surface.Success
			} else {
				actionText = "Download Failed "
				statusLabel = fmt.Sprintf("Download Failed [%s]", filepath.Base(urlVal))
				iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Error)}, icon.Error)
				themeColor = t.Color.Text.Error
			}
		}

		baseFocusBg := t.Color.Surface.BaseFocus
		globeIconColor := t.Color.Surface.Info

		labelNode = kitex.Box(kitex.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow).
				AlignItems(style.AlignCenter),
		},
			kitex.Span(kitex.SpanProps{
				Style: style.S().
					Bold(true).
					Foreground(color.RGBA{255, 255, 255, 255}),
			}, kitex.Text(actionText)),
			kitex.Box(kitex.BoxProps{
				Style: style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexRow).
					AlignItems(style.AlignCenter).
					Background(baseFocusBg).
					PaddingHorizontal(1).
					Gap(1),
			},
				kitex.Span(kitex.SpanProps{Style: style.S().Foreground(globeIconColor)}, icon.Globe),
				kitex.Span(kitex.SpanProps{
					Style: style.S().
						Foreground(color.RGBA{255, 255, 255, 255}).
						Bold(true),
				}, kitex.Text(filepath.Base(urlVal))),
			),
		)
	}

	var onClick func()
	if tm != nil && props.OnViewPreview != nil {
		onClick = func() {
			if tm.IsError {
				props.OnViewPreview(
					fmt.Sprintf("Download Error for %s", urlVal),
					preview.DefaultTextPreview{Text: details},
				)
			} else {
				var sb strings.Builder
				fmt.Fprintf(&sb, "## Download Details for %s\n\n", filepath.Base(urlVal))
				fmt.Fprintf(&sb, "- **URL**: %s\n", urlVal)
				fmt.Fprintf(&sb, "- **Destination**: %s\n", finalDest)
				if bgTaskStr != "" {
					fmt.Fprintf(&sb, "- **Background Task ID**: %s\n", bgTaskStr)
				}
				if sizeStr != "" && bgTaskStr == "" {
					fmt.Fprintf(&sb, "- **Size**: %s\n", sizeStr)
				}
				statusStr := "Failed"
				if successVal {
					statusStr = "Success"
				}
				fmt.Fprintf(&sb, "- **Status**: %s\n", statusStr)
				if detailsMsg != "" {
					fmt.Fprintf(&sb, "- **Message**: %s\n", detailsMsg)
				}
				props.OnViewPreview(
					fmt.Sprintf("Downloaded %s", filepath.Base(urlVal)),
					preview.MarkdownPreview{Markdown: sb.String()},
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

// FetchToolWidget renders the result of a raw fetch tool call.
var FetchToolWidget = kitex.FC("FetchToolWidget", func(props ToolExecutionProps) kitex.Node {
	t := theme.UseTheme()

	tc := props.ToolCall
	tm := props.ToolMessage

	var urlVal string
	if tc.Args != nil {
		urlVal, _ = tc.Args["url"].(string)
	}

	var statusLabel string
	var labelNode kitex.Node
	var iconNode kitex.Node
	var themeColor color.Color
	var details string

	var cleanCode string
	var truncated bool
	var cachedPath string
	var status int

	if tm != nil {
		fOut, ok := parseFetchOutput(tm.StructuredContent)
		if ok {
			cleanCode = fOut.Content
			truncated = fOut.Truncated
			cachedPath = fOut.CachedPath
			status = fOut.Status
		} else {
			cleanCode = getToolOutput(tm.Content)
		}
	}

	if t != nil {
		var actionText string
		if tm == nil {
			actionText = "Fetching "
			statusLabel = fmt.Sprintf("Fetching [%s]", urlVal)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, toolPulse())
			themeColor = t.Color.Surface.Info
		} else if tm.IsError {
			actionText = "Error Fetching "
			statusLabel = fmt.Sprintf("Error Fetching [%s]", urlVal)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Error)}, icon.Error)
			themeColor = t.Color.Text.Error
			details = getToolOutput(tm.Content)
		} else {
			if status >= 400 {
				actionText = fmt.Sprintf("Fetched (%d) ", status)
				statusLabel = fmt.Sprintf("Fetched [%s] (Status: %d)", urlVal, status)
				iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Error)}, icon.Error)
				themeColor = t.Color.Text.Error
			} else {
				if status > 0 {
					actionText = fmt.Sprintf("Fetched (%d) ", status)
					statusLabel = fmt.Sprintf("Fetched [%s] (Status: %d)", urlVal, status)
				} else {
					actionText = "Fetched "
					statusLabel = fmt.Sprintf("Fetched [%s]", urlVal)
				}
				iconNode = nil // remove checkmark completely on success
				themeColor = t.Color.Surface.Success
			}
		}

		baseFocusBg := t.Color.Surface.BaseFocus
		globeIconColor := t.Color.Surface.Info

		labelNode = kitex.Box(kitex.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow).
				AlignItems(style.AlignCenter),
		},
			kitex.Span(kitex.SpanProps{
				Style: style.S().
					Bold(true).
					Foreground(color.RGBA{255, 255, 255, 255}),
			}, kitex.Text(actionText)),
			kitex.Box(kitex.BoxProps{
				Style: style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexRow).
					AlignItems(style.AlignCenter).
					Background(baseFocusBg).
					PaddingHorizontal(1).
					Gap(1),
			},
				kitex.Span(kitex.SpanProps{Style: style.S().Foreground(globeIconColor)}, icon.Globe),
				kitex.Span(kitex.SpanProps{
					Style: style.S().
						Foreground(color.RGBA{255, 255, 255, 255}).
						Bold(true),
				}, kitex.Text(urlVal)),
			),
		)
	}

	var onClick func()
	if tm != nil && props.OnViewPreview != nil {
		onClick = func() {
			if tm.IsError {
				props.OnViewPreview(
					fmt.Sprintf("Fetch Error for %s", urlVal),
					preview.DefaultTextPreview{Text: details},
				)
			} else {
				var contentToView string
				if truncated {
					contentToView = fmt.Sprintf("[TRUNCATED] Content exceeded 16,000 chars. Full saved to: %s\n\n%s", cachedPath, cleanCode)
				} else {
					contentToView = cleanCode
				}
				props.OnViewPreview(
					fmt.Sprintf("Fetched %s", urlVal),
					preview.FileViewPreview{
						Path:     urlVal,
						Content:  contentToView,
						IsBinary: false,
						MimeType: "text/plain",
					},
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
