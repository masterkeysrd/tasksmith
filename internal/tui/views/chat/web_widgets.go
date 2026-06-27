package chat

import (
	"fmt"
	"image/color"
	"path/filepath"
	"strings"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/agent/tools"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/components/icon"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

// WebSearchToolWidget renders the result of a web_search tool call inline.
var WebSearchToolWidget = kitex.FC("WebSearchToolWidget", func(props ToolExecutionProps) kitex.Node {
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
	var details string

	var results []tools.WebSearchOutputResultsItem

	if t != nil {
		var actionText string
		if tm == nil {
			actionText = "Web Search: Searching for "
			statusLabel = fmt.Sprintf("Web Search: Searching for %q", query)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, kitex.Text(props.CurrentDots))
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
					return kitex.Span(kitex.SpanProps{}, kitex.Text(fmt.Sprintf("Web Search Error for %q", query)))
				}),
				kitex.If(tm != nil && !tm.IsError, func() kitex.Node {
					return kitex.Fragment(
						kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, icon.Search),
						kitex.Span(kitex.SpanProps{}, kitex.Text(fmt.Sprintf("Web Search: Found %d results for %q", len(results), query))),
					)
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
				var listNodes []kitex.Node
				for i, res := range results {
					var titleStyle, urlStyle, snippetStyle style.Style
					if t != nil {
						titleStyle = style.S().Bold(true).Foreground(t.Color.Surface.Primary)
						urlStyle = style.S().Italic(true).Foreground(t.Color.Text.Secondary)
						snippetStyle = style.S().Foreground(t.Color.Text.Primary)
					}

					listNodes = append(listNodes, kitex.Box(kitex.BoxProps{
						Style: style.S().MarginBottom(1),
					},
						kitex.Box(kitex.BoxProps{Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).Gap(1)},
							kitex.Span(kitex.SpanProps{Style: titleStyle}, kitex.Text(fmt.Sprintf("%d. %s", i+1, res.Title))),
						),
						kitex.Box(kitex.BoxProps{Style: style.S().MarginLeft(3)},
							kitex.Span(kitex.SpanProps{Style: urlStyle}, kitex.Text(res.Url)),
						),
						kitex.Box(kitex.BoxProps{Style: style.S().MarginLeft(3)},
							kitex.Span(kitex.SpanProps{Style: snippetStyle}, kitex.Text(res.Snippet)),
						),
					))
				}
				return kitex.Box(kitex.BoxProps{
					Style: style.S().
						Display(style.DisplayFlex).
						FlexDirection(style.FlexColumn).
						Padding(1).
						Width(style.Percent(100)).
						MinWidth(style.Percent(0)),
				}, listNodes...)
			}),
		),
	)
})

// WebFetchToolWidget renders the result of a web_fetch tool call.
var WebFetchToolWidget = kitex.FC("WebFetchToolWidget", func(props ToolExecutionProps) kitex.Node {
	t := theme.UseTheme()
	showModal, setShowModal := kitex.UseState(false)

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
	var displayTarget string = url

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
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, kitex.Text(props.CurrentDots))
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
	if tm != nil {
		onClick = func() { setShowModal(true) }
	}

	badgeNode := components.ToolBadge(components.ToolBadgeProps{
		Icon:      iconNode,
		Label:     statusLabel,
		LabelNode: labelNode,
		Color:     themeColor,
		OnClick:   onClick,
	})

	filename := filepath.Base(url)
	if idx := strings.Index(filename, "?"); idx != -1 {
		filename = filename[:idx]
	}
	if filename == "" || filename == "." || filename == "/" {
		filename = "download"
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
					return kitex.Span(kitex.SpanProps{}, kitex.Text(fmt.Sprintf("Web Fetch Error for %s", url)))
				}),
				kitex.If(tm != nil && !tm.IsError, func() kitex.Node {
					return kitex.Fragment(
						kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, icon.Globe),
						kitex.Span(kitex.SpanProps{}, kitex.Text(fmt.Sprintf("Fetched %s", displayTarget))),
					)
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
			kitex.If(showModal() && tm != nil && !tm.IsError, func() kitex.Node {
				return kitex.Fragment(
					// Fetch metadata
					kitex.Box(kitex.BoxProps{
						Style: style.S().
							Display(style.DisplayFlex).
							FlexDirection(style.FlexColumn).
							Gap(0).
							MarginBottom(1).
							Padding(1).
							Background(t.Color.Surface.BaseHover),
					},
						kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Secondary)}, kitex.Text(fmt.Sprintf("  • URL:       %s", url))),
						kitex.If(title != "", func() kitex.Node {
							return kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Secondary)}, kitex.Text(fmt.Sprintf("  • Title:     %s", title)))
						}),
						kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Secondary)}, kitex.Text(fmt.Sprintf("  • MIME Type: %s", mimeType))),
						kitex.If(truncated, func() kitex.Node {
							return kitex.Box(kitex.BoxProps{
								Style: style.S().
									Foreground(t.Color.Text.Error).
									Bold(true).
									MarginTop(1),
							},
								kitex.Text(fmt.Sprintf("[TRUNCATED] Content exceeded 16,000 chars. Full saved to: %s", cachedPath)),
							)
						}),
					),

					// Binary vs Text Content
					kitex.If(isBinary, func() kitex.Node {
						return kitex.Box(kitex.BoxProps{
							Style: style.S().
								Display(style.DisplayFlex).
								FlexDirection(style.FlexColumn).
								Gap(1).
								Padding(1),
						},
							kitex.Box(kitex.BoxProps{
								Style: style.S().
									Display(style.DisplayFlex).
									FlexDirection(style.FlexColumn).
									Gap(0),
							},
								kitex.Span(kitex.SpanProps{Style: style.S().Bold(true)}, kitex.Text("Binary Document:")),
								kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Secondary)}, kitex.Text(fmt.Sprintf("  • Cached Path: %s", cachedPath))),
							),
							components.Button(components.ButtonProps{
								Variant: components.ButtonSolid,
								Color:   components.ButtonPrimary,
								Style: style.S().
									AlignSelf(style.AlignStart).
									MarginTop(1).
									Padding(0, 2),
								OnClick: func() {
									openWithSystemViewer(cachedPath)
								},
							}, kitex.Text("Open with System Viewer")),
						)
					}),
					kitex.If(!isBinary, func() kitex.Node {
						var lang string
						if strings.Contains(mimeType, "json") {
							lang = "json"
						} else if strings.Contains(mimeType, "xml") {
							lang = "xml"
						} else if strings.Contains(mimeType, "html") || strings.HasSuffix(filename, ".md") {
							lang = "markdown"
						}
						return components.CodeBlock(components.CodeBlockProps{
							Code:            cleanCode,
							Lang:            lang,
							HideHeader:      true,
							ShowLineNumbers: false,
						})
					}),
				)
			}),
		),
	)
})

// DownloadToolWidget renders the execution state and results of a download tool call.
var DownloadToolWidget = kitex.FC("DownloadToolWidget", func(props ToolExecutionProps) kitex.Node {
	t := theme.UseTheme()
	showModal, setShowModal := kitex.UseState(false)

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
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, kitex.Text(props.CurrentDots))
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
	if tm != nil {
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
					return kitex.Span(kitex.SpanProps{}, kitex.Text(fmt.Sprintf("Download Error for %s", urlVal)))
				}),
				kitex.If(tm != nil && !tm.IsError, func() kitex.Node {
					return kitex.Fragment(
						kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, icon.Globe),
						kitex.Span(kitex.SpanProps{}, kitex.Text(fmt.Sprintf("Downloaded %s", filepath.Base(urlVal)))),
					)
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
			kitex.If(showModal() && tm != nil && !tm.IsError, func() kitex.Node {
				return kitex.Box(kitex.BoxProps{
					Style: style.S().
						Display(style.DisplayFlex).
						FlexDirection(style.FlexColumn).
						Gap(1).
						Padding(1),
				},
					kitex.Box(kitex.BoxProps{Style: style.S().WhiteSpace(style.WhiteSpacePreWrap)},
						kitex.Span(kitex.SpanProps{Style: style.S().Bold(true)}, kitex.Text("URL: ")),
						kitex.Text(urlVal),
					),
					kitex.Box(kitex.BoxProps{},
						kitex.Span(kitex.SpanProps{Style: style.S().Bold(true)}, kitex.Text("Destination: ")),
						kitex.Text(finalDest),
					),
					kitex.If(bgTaskStr != "", func() kitex.Node {
						return kitex.Box(kitex.BoxProps{},
							kitex.Span(kitex.SpanProps{Style: style.S().Bold(true)}, kitex.Text("Background Task ID: ")),
							kitex.Text(bgTaskStr),
						)
					}),
					kitex.If(sizeStr != "" && bgTaskStr == "", func() kitex.Node {
						return kitex.Box(kitex.BoxProps{},
							kitex.Span(kitex.SpanProps{Style: style.S().Bold(true)}, kitex.Text("Size: ")),
							kitex.Text(sizeStr),
						)
					}),
					kitex.Box(kitex.BoxProps{},
						kitex.Span(kitex.SpanProps{Style: style.S().Bold(true)}, kitex.Text("Status: ")),
						kitex.If(successVal, func() kitex.Node {
							return kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Success)}, kitex.Text("Success"))
						}),
						kitex.If(!successVal, func() kitex.Node {
							return kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Error)}, kitex.Text("Failed"))
						}),
					),
					kitex.If(detailsMsg != "", func() kitex.Node {
						return kitex.Box(kitex.BoxProps{Style: style.S().WhiteSpace(style.WhiteSpacePreWrap)},
							kitex.Span(kitex.SpanProps{Style: style.S().Bold(true)}, kitex.Text("Message: ")),
							kitex.Text(detailsMsg),
						)
					}),
				)
			}),
		),
	)
})

// FetchToolWidget renders the result of a raw fetch tool call.
var FetchToolWidget = kitex.FC("FetchToolWidget", func(props ToolExecutionProps) kitex.Node {
	t := theme.UseTheme()
	showModal, setShowModal := kitex.UseState(false)

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
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, kitex.Text(props.CurrentDots))
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
	if tm != nil {
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
					return kitex.Span(kitex.SpanProps{}, kitex.Text(fmt.Sprintf("Fetch Error for %s", urlVal)))
				}),
				kitex.If(tm != nil && !tm.IsError, func() kitex.Node {
					return kitex.Fragment(
						kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, icon.Globe),
						kitex.Span(kitex.SpanProps{}, kitex.Text(fmt.Sprintf("Fetched %s", urlVal))),
					)
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
			kitex.If(showModal() && tm != nil && !tm.IsError, func() kitex.Node {
				var formatVal string
				if tc.Args != nil {
					formatVal, _ = tc.Args["format"].(string)
				}

				var lang string
				if formatVal == "markdown" {
					lang = "markdown"
				} else if formatVal == "html" {
					lang = "html"
				} else {
					trimmed := strings.TrimSpace(cleanCode)
					if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
						lang = "json"
					} else if strings.HasPrefix(trimmed, "<") {
						if strings.Contains(strings.ToLower(trimmed), "html") {
							lang = "html"
						} else {
							lang = "xml"
						}
					}
				}

				return kitex.Box(kitex.BoxProps{
					Style: style.S().
						Display(style.DisplayFlex).
						FlexDirection(style.FlexColumn).
						Padding(1).
						Width(style.Percent(100)).
						MinWidth(style.Percent(0)),
				},
					// Fetch metadata
					kitex.Box(kitex.BoxProps{
						Style: style.S().
							Display(style.DisplayFlex).
							FlexDirection(style.FlexColumn).
							Gap(0).
							MarginBottom(1).
							Padding(1).
							Background(t.Color.Surface.BaseHover),
					},
						kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Secondary)}, kitex.Text(fmt.Sprintf("  • URL:    %s", urlVal))),
						kitex.If(status > 0, func() kitex.Node {
							return kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Secondary)}, kitex.Text(fmt.Sprintf("  • Status: %d", status)))
						}),
						kitex.If(truncated, func() kitex.Node {
							return kitex.Box(kitex.BoxProps{
								Style: style.S().
									Foreground(t.Color.Text.Error).
									Bold(true).
									MarginTop(1),
							},
								kitex.Text(fmt.Sprintf("[TRUNCATED] Content exceeded 16,000 chars. Full saved to: %s", cachedPath)),
							)
						}),
					),

					// Content pretty printed in code block
					components.CodeBlock(components.CodeBlockProps{
						Code:            cleanCode,
						Lang:            lang,
						HideHeader:      true,
						ShowLineNumbers: false,
					}),
				)
			}),
		),
	)
})
