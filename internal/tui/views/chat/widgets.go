package chat

import (
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/agent/permissions"
	"github.com/masterkeysrd/tasksmith/internal/agent/tools"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/components/icon"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

var CollapsibleThinking = kitex.FC("CollapsibleThinking", func(props CollapsibleThinkingProps) kitex.Node {
	t := theme.UseTheme()

	lines := strings.Split(strings.TrimSpace(props.Content), "\n")
	const previewLines = 10
	hasMore := len(lines) > previewLines

	lineNodes := func(ls []string) []kitex.Node {
		nodes := make([]kitex.Node, len(ls))
		for i, line := range ls {
			var fg color.Color
			if t != nil {
				fg = t.Color.Text.Tertiary
			}
			nodes[i] = kitex.Box(kitex.BoxProps{
				Style: style.S().Foreground(fg).WhiteSpace(style.WhiteSpacePreWrap),
			}, kitex.Text(line))
		}
		return nodes
	}

	bodyStyle := style.S().
		Padding(1).
		Display(style.DisplayFlex).
		FlexDirection(style.FlexColumn)

	return components.Accordion(components.AccordionProps{
		Color:   components.PaperHover,
		Variant: components.PaperOutlined,
	},
		components.AccordionSummary(components.AccordionSummaryProps{
			HideExpandIcon: !hasMore,
			EndContent: kitex.If(hasMore, func() kitex.Node {
				var fg color.Color
				if t != nil {
					fg = t.Color.Text.Secondary
				}
				label := fmt.Sprintf("%d more lines", len(lines)-previewLines)
				return kitex.Span(kitex.SpanProps{Style: style.S().Foreground(fg)}, kitex.Text(label))
			}),
		},
			kitex.Box(kitex.BoxProps{
				Style: style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexRow).
					AlignItems(style.AlignCenter).
					Gap(1),
			},
				kitex.If(t != nil, func() kitex.Node {
					return kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Tertiary)}, kitex.Text("≈"))
				}),
				kitex.If(t != nil, func() kitex.Node {
					titleStr := "Thought"
					durStr := formatThinkingDuration(props.Duration)
					if durStr != "" {
						titleStr += " for " + durStr
					}
					if props.Tokens > 0 {
						if durStr != "" {
							titleStr += ", "
						} else {
							titleStr += " for "
						}
						titleStr += fmt.Sprintf("%d tokens", props.Tokens)
					}
					return kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Tertiary).Bold(true)}, kitex.Text(titleStr))
				}),
			),
		),
		// Preview: first N lines, always visible.
		components.AccordionPreview(components.AccordionPreviewProps{Style: bodyStyle},
			lineNodes(func() []string {
				if hasMore {
					return lines[:previewLines]
				}
				return lines
			}())...,
		),
		// Details: overflow lines, only visible when expanded.
		kitex.If(hasMore, func() kitex.Node {
			return components.AccordionDetails(components.AccordionDetailsProps{Style: bodyStyle},
				lineNodes(lines[previewLines:])...,
			)
		}),
	)
})

func formatThinkingDuration(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	secs := int(d.Round(time.Second).Seconds())
	if secs < 60 {
		return fmt.Sprintf("%ds", secs)
	}
	mins := secs / 60
	remSecs := secs % 60
	if remSecs == 0 {
		return fmt.Sprintf("%dm", mins)
	}
	return fmt.Sprintf("%dm %ds", mins, remSecs)
}

var ViewToolWidget = kitex.FC("ViewToolWidget", func(props ToolExecutionProps) kitex.Node {
	t := theme.UseTheme()
	showModal, setShowModal := kitex.UseState(false)

	tc := props.ToolCall
	tm := props.ToolMessage

	var path string
	if tc.Args != nil {
		path, _ = tc.Args["path"].(string)
	}
	filename := filepath.Base(path)

	var statusLabel string
	var iconNode kitex.Node
	var themeColor color.Color

	if t != nil {
		if tm == nil {
			statusLabel = fmt.Sprintf("Pending View [%s]", filename)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, kitex.Text(props.CurrentDots))
			themeColor = t.Color.Surface.Info
		} else if tm.IsError {
			statusLabel = fmt.Sprintf("Error Viewing [%s]", filename)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Error)}, icon.Error)
			themeColor = t.Color.Text.Error
		} else {
			vOut, ok := parseViewStructuredOutput(tm.StructuredContent)
			if ok {
				var rangeStr string
				if vOut.StartLine > 0 && vOut.EndLine > 0 {
					rangeStr = fmt.Sprintf(" (Lines %d-%d)", vOut.StartLine, vOut.EndLine)
				}
				if vOut.IsBinary {
					statusLabel = fmt.Sprintf("Viewed Binary [%s] (%s)", filename, vOut.MimeType)
				} else {
					statusLabel = fmt.Sprintf("Viewed [%s]%s", filename, rangeStr)
				}
			} else {
				outText := getToolOutput(tm.Content)
				actualStart, actualEnd := parseRangeFromHeader(outText)
				var rangeStr string
				if actualStart > 0 && actualEnd > 0 {
					rangeStr = fmt.Sprintf(" (Lines %d-%d)", actualStart, actualEnd)
				}
				statusLabel = fmt.Sprintf("Viewed [%s]%s", filename, rangeStr)
			}
			iconNode = nil
			themeColor = t.Color.Surface.Success
		}
	}

	var labelNode kitex.Node
	if t != nil {
		actionText := "Pending View "
		var detailsText string
		if tm != nil {
			if tm.IsError {
				actionText = "Error Viewing "
			} else {
				actionText = "Viewed "
				vOut, ok := parseViewStructuredOutput(tm.StructuredContent)
				if ok {
					if vOut.IsBinary {
						detailsText = "binary"
					} else if vOut.StartLine > 0 && vOut.EndLine > 0 {
						detailsText = fmt.Sprintf("%d-%d", vOut.StartLine, vOut.EndLine)
					}
				} else {
					outText := getToolOutput(tm.Content)
					actualStart, actualEnd := parseRangeFromHeader(outText)
					if actualStart > 0 && actualEnd > 0 {
						detailsText = fmt.Sprintf("%d-%d", actualStart, actualEnd)
					}
				}
			}
		}

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
					Background(t.Color.Surface.BaseFocus).
					PaddingHorizontal(1).
					Gap(1),
			},
				icon.FileIcon(icon.FileIconProps{Path: path}),
				kitex.Span(kitex.SpanProps{
					Style: style.S().
						Foreground(color.RGBA{255, 255, 255, 255}).
						Bold(true),
				}, kitex.Text(filename)),
				kitex.If(detailsText != "", func() kitex.Node {
					return kitex.Span(kitex.SpanProps{
						Style: style.S().
							Foreground(t.Color.Text.Secondary).
							Bold(true).
							MarginLeft(1),
					}, kitex.Text(detailsText))
				}),
			),
		)
	}

	var onClick func()
	if tm != nil && !tm.IsError {
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
				icon.FileIcon(icon.FileIconProps{Path: path}),
				kitex.Text(fmt.Sprintf("Viewing %s", filename)),
			),
			OnClose: func() { setShowModal(false) },
		},
			kitex.If(showModal(), func() kitex.Node {
				var cleanCode string
				var startLine int
				var showLines bool

				vOut, ok := parseViewStructuredOutput(tm.StructuredContent)
				if ok {
					cleanCode = stripLinePrefixes(vOut.Content)
					startLine = vOut.StartLine
					showLines = true
				} else {
					outText := getToolOutput(tm.Content)
					actualStart, _ := parseRangeFromHeader(outText)
					if actualStart > 0 {
						_, after, ok := strings.Cut(outText, "\n")
						if ok {
							cleanCode = stripLinePrefixes(after)
						} else {
							cleanCode = outText
						}
						startLine = actualStart
						showLines = true
					} else {
						cleanCode = outText
						showLines = false
					}
				}

				return kitex.Fragment(
					kitex.If(ok && vOut.IsBinary, func() kitex.Node {
						var textPrimary color.Color
						var textSecondary color.Color
						if t != nil {
							textPrimary = t.Color.Text.Primary
							textSecondary = t.Color.Text.Secondary
						}
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
								kitex.Span(kitex.SpanProps{Style: style.S().Bold(true).Foreground(textPrimary)}, kitex.Text("Binary File Details:")),
								kitex.Span(kitex.SpanProps{Style: style.S().Foreground(textSecondary)}, kitex.Text(fmt.Sprintf("  • Name:      %s", filename))),
								kitex.Span(kitex.SpanProps{Style: style.S().Foreground(textSecondary)}, kitex.Text(fmt.Sprintf("  • MIME Type: %s", vOut.MimeType))),
								kitex.Span(kitex.SpanProps{Style: style.S().Foreground(textSecondary)}, kitex.Text(fmt.Sprintf("  • Path:      %s", vOut.Source))),
							),
							components.Button(components.ButtonProps{
								Variant: components.ButtonSolid,
								Color:   components.ButtonPrimary,
								Style: style.S().
									AlignSelf(style.AlignStart).
									MarginTop(1).
									Padding(0, 2),
								OnClick: func() {
									openWithSystemViewer(vOut.Source)
								},
							}, kitex.Text("Open with System Viewer")),
						)
					}),
					kitex.If(!ok || !vOut.IsBinary, func() kitex.Node {
						return components.CodeBlock(components.CodeBlockProps{
							Code:            cleanCode,
							Lang:            detectLang(filename),
							HideHeader:      true,
							ShowLineNumbers: showLines,
							StartLine:       startLine,
						})
					}),
				)
			}),
		),
	)
})

// LsToolWidget renders the result of an ls tool call inline — no modal.
// Results beyond lsPreviewLines are hidden behind an expand toggle.
var LsToolWidget = kitex.FC("LsToolWidget", func(props ToolExecutionProps) kitex.Node {
	t := theme.UseTheme()

	tc := props.ToolCall
	tm := props.ToolMessage

	var dirPath string
	if tc.Args != nil {
		dirPath, _ = tc.Args["path"].(string)
	}
	dirName := filepath.Base(dirPath)
	if dirName == "" {
		dirName = dirPath
	}

	var statusLabel string
	var iconNode kitex.Node
	var borderCol color.Color

	var lsFiles []tools.FileEntry
	var totalCount int
	var truncated bool

	if t != nil {
		if tm == nil {
			statusLabel = fmt.Sprintf("Listing [%s]", dirName)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, kitex.Text(props.CurrentDots))
			borderCol = t.Color.Surface.Info
		} else if tm.IsError {
			statusLabel = fmt.Sprintf("Error Listing [%s]", dirName)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Error)}, icon.Error)
			borderCol = t.Color.Text.Error
		} else {
			lsFiles, totalCount, truncated = parseLsOutput(tm.StructuredContent)
			entryWord := "entries"
			if totalCount == 1 {
				entryWord = "entry"
			}
			statusLabel = fmt.Sprintf("Listed [%s] — %d %s", dirName, totalCount, entryWord)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Success)}, icon.Checkmark)
			borderCol = t.Color.Surface.Success
		}
	}

	// The Accordion Outlined variant handles border + BaseFocus header + BaseHover body.
	// We override the border color via style to reflect the current status.
	accordionStyle := style.S()
	if t != nil {
		accordionStyle = accordionStyle.Border(borderCol)
	}

	return components.Accordion(components.AccordionProps{
		Color:   components.PaperHover,
		Variant: components.PaperOutlined,
		Style:   accordionStyle,
	},
		components.AccordionSummary(components.AccordionSummaryProps{
			HideExpandIcon: tm == nil || tm.IsError,
			EndContent: kitex.If(tm != nil && !tm.IsError, func() kitex.Node {
				var fg color.Color
				if t != nil {
					fg = t.Color.Text.Secondary
				}
				return kitex.Span(kitex.SpanProps{Style: style.S().Foreground(fg)},
					kitex.Text("Click to expand/collapse"),
				)
			}),
		},
			kitex.Box(kitex.BoxProps{
				Style: style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexRow).
					AlignItems(style.AlignCenter).
					Gap(1),
			},
				iconNode,
				kitex.Span(kitex.SpanProps{Style: style.S().Bold(true)}, kitex.Text(statusLabel)),
			),
		),
		components.AccordionDetails(components.AccordionDetailsProps{},
			// Entry list as a borderless table for natural column alignment
			kitex.If(tm != nil && !tm.IsError && len(lsFiles) > 0, func() kitex.Node {

				rows := make([]kitex.Node, 0, lsPreviewLines)
				limit := len(lsFiles)
				for i := range limit {
					rows = append(rows, lsEntryRow(t, lsFiles[i]))
				}

				var textCol color.Color
				if t != nil {
					textCol = t.Color.Text.Tertiary
				}
				return kitex.Box(kitex.BoxProps{
					Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn),
				},
					kitex.Table(kitex.TableProps{},
						kitex.TBody(kitex.TBodyProps{}, rows...),
					),
					kitex.If(truncated, func() kitex.Node {
						return kitex.Box(kitex.BoxProps{
							Style: style.S().Foreground(textCol).Italic(true).MarginTop(1),
						}, kitex.Text(fmt.Sprintf("[Showing %d of %d — use limit parameter to paginate]", len(lsFiles), totalCount)))
					}),
				)
			}),

			// Empty directory notice
			kitex.If(tm != nil && !tm.IsError && len(lsFiles) == 0, func() kitex.Node {
				var textCol color.Color
				if t != nil {
					textCol = t.Color.Text.Tertiary
				}
				return kitex.Box(kitex.BoxProps{
					Style: style.S().Foreground(textCol).Italic(true),
				}, kitex.Text("(empty directory)"))
			}),
		),
	)
})

// GlobToolWidget renders the result of a glob tool call inline — no modal.
var GlobToolWidget = kitex.FC("GlobToolWidget", func(props ToolExecutionProps) kitex.Node {
	t := theme.UseTheme()
	showModal, setShowModal := kitex.UseState(false)

	tc := props.ToolCall
	tm := props.ToolMessage

	var pattern string
	var path string
	if tc.Args != nil {
		pattern, _ = tc.Args["pattern"].(string)
		path, _ = tc.Args["path"].(string)
	}

	var scope string
	if path != "" {
		scope = fmt.Sprintf(" in %s", path)
	}

	var statusLabel string
	var labelNode kitex.Node
	var iconNode kitex.Node
	var themeColor color.Color

	var matches []string
	var totalCount int
	var truncated bool
	var details string

	var isDir bool
	if path != "" {
		if fi, err := os.Stat(path); err == nil {
			isDir = fi.IsDir()
		} else {
			isDir = filepath.Ext(path) == ""
		}
	}

	if t != nil {
		var actionText string
		if tm == nil {
			if path != "" {
				actionText = "Glob: Searching in "
			} else {
				actionText = "Glob: Searching for "
			}
			statusLabel = fmt.Sprintf("Glob: Searching%s for [%s]", scope, pattern)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, kitex.Text(props.CurrentDots))
			themeColor = t.Color.Surface.Info
		} else if tm.IsError {
			if path != "" {
				actionText = "Glob: Error searching in "
			} else {
				actionText = "Glob: Error searching for "
			}
			statusLabel = fmt.Sprintf("Glob: Error searching%s for [%s]", scope, pattern)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Error)}, icon.Error)
			themeColor = t.Color.Text.Error
			details = getToolOutput(tm.Content)
		} else {
			matches, totalCount, truncated = parseGlobOutput(tm.StructuredContent)
			matchWord := "matches"
			if totalCount == 1 {
				matchWord = "match"
			}
			if totalCount > 0 {
				if path != "" {
					actionText = fmt.Sprintf("Glob: Found %d %s in ", totalCount, matchWord)
				} else {
					actionText = fmt.Sprintf("Glob: Found %d %s for ", totalCount, matchWord)
				}
				statusLabel = fmt.Sprintf("Glob: Found %d %s%s for [%s]", totalCount, matchWord, scope, pattern)
				iconNode = nil // remove checkmark completely on success
				themeColor = color.RGBA{R: 255, G: 255, B: 255, A: 255}
			} else {
				if path != "" {
					actionText = "Glob: No matches in "
				} else {
					actionText = "Glob: No matches for "
				}
				statusLabel = fmt.Sprintf("Glob: No matches found%s for [%s]", scope, pattern)
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
			kitex.If(path != "", func() kitex.Node {
				return kitex.Box(kitex.BoxProps{
					Style: style.S().
						Display(style.DisplayFlex).
						FlexDirection(style.FlexRow).
						AlignItems(style.AlignCenter).
						Background(baseFocusBg).
						PaddingHorizontal(1).
						Gap(1).
						MarginRight(1),
				},
					kitex.If(isDir, func() kitex.Node {
						return kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, icon.Folder)
					}),
					kitex.If(!isDir, func() kitex.Node {
						return icon.FileIcon(icon.FileIconProps{Path: path})
					}),
					kitex.Span(kitex.SpanProps{
						Style: style.S().
							Foreground(color.RGBA{255, 255, 255, 255}).
							Bold(true),
					}, kitex.Text(filepath.Base(path))),
				)
			}),
			kitex.If(path != "", func() kitex.Node {
				return kitex.Span(kitex.SpanProps{
					Style: style.S().
						Bold(true).
						Foreground(color.RGBA{255, 255, 255, 255}).
						MarginRight(1),
				}, kitex.Text("for"))
			}),
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
				}, kitex.Text(pattern)),
			),
		)
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
					return kitex.Span(kitex.SpanProps{}, kitex.Text(fmt.Sprintf("Glob Error for %s", pattern)))
				}),
				kitex.If(tm != nil && !tm.IsError, func() kitex.Node {
					return kitex.Fragment(
						kitex.If(path != "", func() kitex.Node {
							return kitex.Fragment(
								kitex.If(isDir, func() kitex.Node {
									return kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, icon.Folder)
								}),
								kitex.If(!isDir, func() kitex.Node {
									return icon.FileIcon(icon.FileIconProps{Path: path})
								}),
							)
						}),
						kitex.If(path == "", func() kitex.Node {
							return kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, icon.Search)
						}),
						kitex.Span(kitex.SpanProps{}, kitex.Text(fmt.Sprintf("Found %d matches for %s", totalCount, pattern))),
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
			kitex.If(showModal() && tm != nil && !tm.IsError && len(matches) > 0, func() kitex.Node {
				rows := make([]kitex.Node, 0, len(matches))
				for _, match := range matches {
					rows = append(rows, globEntryRow(t, match))
				}

				var textCol color.Color
				if t != nil {
					textCol = t.Color.Text.Tertiary
				}
				return kitex.Box(kitex.BoxProps{
					Style: style.S().
						Display(style.DisplayFlex).
						FlexDirection(style.FlexColumn).
						Padding(1).
						Width(style.Percent(100)).
						MinWidth(style.Percent(0)),
				},
					kitex.Box(kitex.BoxProps{
						Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Gap(0),
					}, rows...),
					kitex.If(truncated, func() kitex.Node {
						return kitex.Box(kitex.BoxProps{
							Style: style.S().Foreground(textCol).Italic(true).MarginTop(1).PaddingHorizontal(1),
						}, kitex.Text(fmt.Sprintf("[Showing %d of %d matches]", len(matches), totalCount)))
					}),
				)
			}),
		),
	)
})

// globEntryRow renders a single glob match path, highlighting the directory path and the base filename.
func globEntryRow(t *theme.Scheme, match string) kitex.Node {
	var nameColor color.Color
	var dirColor color.Color
	if t != nil {
		nameColor = t.Color.Text.Primary
		dirColor = t.Color.Text.Secondary
	}

	dirPart, filePart := filepath.Split(match)
	if len(filePart) > tools.MaxFilenameChars {
		filePart = filePart[:tools.MaxFilenameChars] + "…"
	}

	return kitex.Box(kitex.BoxProps{
		Style: style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexRow).
			AlignItems(style.AlignCenter).
			PaddingVertical(0).
			PaddingHorizontal(1),
	},
		kitex.Span(kitex.SpanProps{Style: style.S().MarginRight(1)}, icon.FileIcon(icon.FileIconProps{Path: match})),
		kitex.If(dirPart != "", func() kitex.Node {
			return kitex.Span(kitex.SpanProps{Style: style.S().Foreground(dirColor)}, kitex.Text(dirPart))
		}),
		kitex.Span(kitex.SpanProps{Style: style.S().Foreground(nameColor).Bold(true)}, kitex.Text(filePart)),
	)
}

// GrepToolWidget renders the result of a grep tool call inline.
// GrepToolWidget renders the result of a grep tool call inline.
var GrepToolWidget = kitex.FC("GrepToolWidget", func(props ToolExecutionProps) kitex.Node {
	t := theme.UseTheme()
	showModal, setShowModal := kitex.UseState(false)

	tc := props.ToolCall
	tm := props.ToolMessage

	var pattern string
	var path string
	if tc.Args != nil {
		pattern, _ = tc.Args["pattern"].(string)
		path, _ = tc.Args["path"].(string)
	}

	var scope string
	if path != "" {
		scope = fmt.Sprintf(" in %s", path)
	}

	var statusLabel string
	var labelNode kitex.Node
	var iconNode kitex.Node
	var themeColor color.Color

	var matches []tools.GrepOutputMatchesItem
	var totalCount int
	var truncated bool
	var details string

	var isDir bool
	if path != "" {
		if fi, err := os.Stat(path); err == nil {
			isDir = fi.IsDir()
		} else {
			isDir = filepath.Ext(path) == ""
		}
	}

	if t != nil {
		var actionText string
		if tm == nil {
			if path != "" {
				actionText = "Searching in "
			} else {
				actionText = "Searching for "
			}
			statusLabel = fmt.Sprintf("Searching%s for %s", scope, pattern)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, kitex.Text(props.CurrentDots))
			themeColor = t.Color.Surface.Info
		} else if tm.IsError {
			if path != "" {
				actionText = "Error searching in "
			} else {
				actionText = "Error searching for "
			}
			statusLabel = fmt.Sprintf("Error searching%s for %s", scope, pattern)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Error)}, icon.Error)
			themeColor = t.Color.Text.Error
			details = getToolOutput(tm.Content)
		} else {
			matches, totalCount, truncated = parseGrepOutput(tm.StructuredContent)
			if totalCount > 0 {
				matchWord := "matches"
				if totalCount == 1 {
					matchWord = "match"
				}
				if path != "" {
					actionText = fmt.Sprintf("Found %d %s in ", totalCount, matchWord)
				} else {
					actionText = fmt.Sprintf("Found %d %s for ", totalCount, matchWord)
				}
				statusLabel = fmt.Sprintf("Found %d %s%s for %s", totalCount, matchWord, scope, pattern)
				iconNode = nil // remove checkmark completely on success
				themeColor = color.RGBA{R: 255, G: 255, B: 255, A: 255}
			} else {
				if path != "" {
					actionText = "No matches in "
				} else {
					actionText = "No matches for "
				}
				statusLabel = fmt.Sprintf("No matches found%s for %s", scope, pattern)
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
			kitex.If(path != "", func() kitex.Node {
				return kitex.Box(kitex.BoxProps{
					Style: style.S().
						Display(style.DisplayFlex).
						FlexDirection(style.FlexRow).
						AlignItems(style.AlignCenter).
						Background(baseFocusBg).
						PaddingHorizontal(1).
						Gap(1).
						MarginRight(1),
				},
					kitex.If(isDir, func() kitex.Node {
						return kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, icon.Folder)
					}),
					kitex.If(!isDir, func() kitex.Node {
						return icon.FileIcon(icon.FileIconProps{Path: path})
					}),
					kitex.Span(kitex.SpanProps{
						Style: style.S().
							Foreground(color.RGBA{255, 255, 255, 255}).
							Bold(true),
					}, kitex.Text(filepath.Base(path))),
				)
			}),
			kitex.If(path != "", func() kitex.Node {
				return kitex.Span(kitex.SpanProps{
					Style: style.S().
						Bold(true).
						Foreground(color.RGBA{255, 255, 255, 255}).
						MarginRight(1),
				}, kitex.Text("for"))
			}),
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
				}, kitex.Text(pattern)),
			),
		)
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
					return kitex.Span(kitex.SpanProps{}, kitex.Text(fmt.Sprintf("Grep Error for %s", pattern)))
				}),
				kitex.If(tm != nil && !tm.IsError, func() kitex.Node {
					return kitex.Fragment(
						kitex.If(path != "", func() kitex.Node {
							return kitex.Fragment(
								kitex.If(isDir, func() kitex.Node {
									return kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, icon.Folder)
								}),
								kitex.If(!isDir, func() kitex.Node {
									return icon.FileIcon(icon.FileIconProps{Path: path})
								}),
							)
						}),
						kitex.If(path == "", func() kitex.Node {
							return kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, icon.Search)
						}),
						kitex.Span(kitex.SpanProps{}, kitex.Text(fmt.Sprintf("Found %d matches for %s", totalCount, pattern))),
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
			kitex.If(showModal() && tm != nil && !tm.IsError && len(matches) > 0, func() kitex.Node {
				rows := make([]kitex.Node, 0, len(matches))
				var currentFile string
				firstFile := true
				for _, match := range matches {
					if match.Path != currentFile {
						currentFile = match.Path
						dirPart := filepath.Dir(match.Path)
						filePart := filepath.Base(match.Path)
						if dirPart == "." {
							dirPart = ""
						} else if !strings.HasSuffix(dirPart, "/") {
							dirPart += "/"
						}

						headerStyle := style.S().
							Display(style.DisplayFlex).
							FlexDirection(style.FlexRow).
							AlignItems(style.AlignCenter)

						if firstFile {
							headerStyle = headerStyle.PaddingHorizontal(0)
							firstFile = false
						} else {
							headerStyle = headerStyle.PaddingTop(1).PaddingHorizontal(0)
						}

						rows = append(rows, kitex.Box(kitex.BoxProps{
							Style: headerStyle,
						},
							kitex.Span(kitex.SpanProps{Style: style.S().MarginRight(1)}, icon.FileIcon(icon.FileIconProps{Path: match.Path})),
							kitex.If(dirPart != "", func() kitex.Node {
								return kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Secondary)}, kitex.Text(dirPart))
							}),
							kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Primary)}, kitex.Text(filePart)),
							kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Tertiary)}, kitex.Text(":")),
						))
					}

					rows = append(rows, grepEntryRow(t, match))
				}

				var textCol color.Color
				if t != nil {
					textCol = t.Color.Text.Tertiary
				}
				return kitex.Box(kitex.BoxProps{
					Style: style.S().
						Display(style.DisplayFlex).
						FlexDirection(style.FlexColumn).
						Padding(1).
						Width(style.Percent(100)).
						MinWidth(style.Percent(0)),
				},
					kitex.Box(kitex.BoxProps{
						Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Gap(0),
					}, rows...),
					kitex.If(truncated, func() kitex.Node {
						return kitex.Box(kitex.BoxProps{
							Style: style.S().Foreground(textCol).Italic(true).MarginTop(1).PaddingHorizontal(1),
						}, kitex.Text(fmt.Sprintf("[Showing %d of %d matches]", len(matches), totalCount)))
					}),
				)
			}),
		),
	)
})

// grepEntryRow renders a single grep match line using components.CodeBlock with Compact styling.
func grepEntryRow(t *theme.Scheme, match tools.GrepOutputMatchesItem) kitex.Node {
	ext := filepath.Ext(match.Path)
	lang := strings.TrimPrefix(ext, ".")

	return kitex.Box(kitex.BoxProps{
		Style: style.S().
			PaddingVertical(0).
			PaddingHorizontal(1),
	},
		components.CodeBlock(components.CodeBlockProps{
			Code:            match.Content,
			Lang:            lang,
			HideHeader:      true,
			ShowLineNumbers: true,
			StartLine:       match.Line,
			Compact:         true,
			Style:           style.S().Margin(0).Padding(0),
		}),
	)
}

// WriteToolWidget renders the result of a write tool call inline.
var WriteToolWidget = kitex.FC("WriteToolWidget", func(props ToolExecutionProps) kitex.Node {
	t := theme.UseTheme()
	showModal, setShowModal := kitex.UseState(false)

	tc := props.ToolCall
	tm := props.ToolMessage

	var path string
	var content string
	if tc.Args != nil {
		path, _ = tc.Args["path"].(string)
		content, _ = tc.Args["content"].(string)
	}
	filename := filepath.Base(path)

	var statusLabel string
	var iconNode kitex.Node
	var themeColor color.Color

	if t != nil {
		if tm == nil {
			statusLabel = fmt.Sprintf("Pending Write [%s]", filename)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, kitex.Text(props.CurrentDots))
			themeColor = t.Color.Surface.Info
		} else if tm.IsError {
			statusLabel = fmt.Sprintf("Error Writing [%s]", filename)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Error)}, icon.Error)
			themeColor = t.Color.Text.Error
		} else {
			wOut, ok := parseWriteStructuredOutput(tm.StructuredContent)
			if ok && wOut.Success {
				statusLabel = fmt.Sprintf("Wrote [%s] (%d bytes)", filename, wOut.BytesWritten)
			} else {
				statusLabel = fmt.Sprintf("Wrote [%s]", filename)
			}
			iconNode = nil
			themeColor = t.Color.Surface.Success
		}
	}

	var labelNode kitex.Node
	if t != nil {
		actionText := "Pending Write "
		var detailsText string
		if tm != nil {
			if tm.IsError {
				actionText = "Error Writing "
			} else {
				actionText = "Wrote "
				wOut, ok := parseWriteStructuredOutput(tm.StructuredContent)
				if ok && wOut.Success {
					detailsText = fmt.Sprintf("(%d bytes)", wOut.BytesWritten)
				}
			}
		}

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
					Background(t.Color.Surface.BaseFocus).
					PaddingHorizontal(1).
					Gap(1),
			},
				icon.FileIcon(icon.FileIconProps{Path: path}),
				kitex.Span(kitex.SpanProps{
					Style: style.S().
						Foreground(color.RGBA{255, 255, 255, 255}).
						Bold(true),
				}, kitex.Text(filename)),
				kitex.If(detailsText != "", func() kitex.Node {
					return kitex.Span(kitex.SpanProps{
						Style: style.S().
							Foreground(t.Color.Text.Secondary).
							Bold(true).
							MarginLeft(1),
					}, kitex.Text(detailsText))
				}),
			),
		)
	}

	var onClick func()
	if tm != nil && !tm.IsError && content != "" {
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
				icon.FileIcon(icon.FileIconProps{Path: path}),
				kitex.Text(fmt.Sprintf("Writing %s", filename)),
			),
			OnClose: func() { setShowModal(false) },
		},
			kitex.If(showModal(), func() kitex.Node {
				return components.CodeBlock(components.CodeBlockProps{
					Code:            content,
					Lang:            detectLang(filename),
					HideHeader:      true,
					ShowLineNumbers: true,
					StartLine:       1,
				})
			}),
		),
	)
})

// EditToolWidget renders the result of an edit tool call inline.
var EditToolWidget = kitex.FC("EditToolWidget", func(props ToolExecutionProps) kitex.Node {
	t := theme.UseTheme()
	showModal, setShowModal := kitex.UseState(false)
	split, setSplit := kitex.UseState(false)

	tc := props.ToolCall
	tm := props.ToolMessage

	var path string
	if tc.Args != nil {
		path, _ = tc.Args["path"].(string)
	}
	filename := filepath.Base(path)

	var statusLabel string
	var iconNode kitex.Node
	var themeColor color.Color
	var diffContent string

	if t != nil {
		if tm == nil {
			statusLabel = fmt.Sprintf("Pending Edit [%s]", filename)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, kitex.Text(props.CurrentDots))
			themeColor = t.Color.Surface.Info
		} else if tm.IsError {
			statusLabel = fmt.Sprintf("Error Editing [%s]", filename)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Error)}, icon.Error)
			themeColor = t.Color.Text.Error
		} else {
			eOut, ok := parseEditStructuredOutput(tm.StructuredContent)
			if ok && eOut.Success {
				statusLabel = fmt.Sprintf("Edited [%s +%d -%d]", filename, eOut.Additions, eOut.Deletions)
				diffContent = eOut.Diff
			} else {
				statusLabel = fmt.Sprintf("Edited [%s]", filename)
				diffContent = getToolOutput(tm.Content)
			}
			iconNode = nil
			themeColor = t.Color.Surface.Success
		}
	}

	var labelNode kitex.Node
	if t != nil {
		actionText := "Pending Edit "
		var additions, deletions int
		var hasDiffStats bool
		if tm != nil {
			if tm.IsError {
				actionText = "Error Editing "
			} else {
				actionText = "Wrote "
				eOut, ok := parseEditStructuredOutput(tm.StructuredContent)
				if ok && eOut.Success {
					additions = eOut.Additions
					deletions = eOut.Deletions
					hasDiffStats = true
				}
			}
		}

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
					Background(t.Color.Surface.BaseFocus).
					PaddingHorizontal(1).
					Gap(1),
			},
				icon.FileIcon(icon.FileIconProps{Path: path}),
				kitex.Span(kitex.SpanProps{
					Style: style.S().
						Foreground(color.RGBA{255, 255, 255, 255}).
						Bold(true),
				}, kitex.Text(filename)),
				kitex.If(hasDiffStats, func() kitex.Node {
					return kitex.Box(kitex.BoxProps{
						Style: style.S().
							Display(style.DisplayFlex).
							FlexDirection(style.FlexRow).
							AlignItems(style.AlignCenter).
							Gap(1).
							MarginLeft(1),
					},
						kitex.Box(kitex.BoxProps{
							Style: style.S().
								Display(style.DisplayFlex).
								FlexDirection(style.FlexRow).
								AlignItems(style.AlignCenter).
								Foreground(t.Color.Surface.Success).
								Bold(true),
						},
							kitex.Text(fmt.Sprintf("+%d", additions)),
						),
						kitex.Box(kitex.BoxProps{
							Style: style.S().
								Display(style.DisplayFlex).
								FlexDirection(style.FlexRow).
								AlignItems(style.AlignCenter).
								Foreground(t.Color.Text.Error).
								Bold(true),
						},
							kitex.Text(fmt.Sprintf("-%d", deletions)),
						),
					)
				}),
			),
		)
	}

	var onClick func()
	if tm != nil && !tm.IsError && diffContent != "" {
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
				icon.FileIcon(icon.FileIconProps{Path: path}),
				kitex.Text(fmt.Sprintf("Changes in %s", filename)),
			),
			OnClose: func() { setShowModal(false) },
			HeaderActions: components.Button(components.ButtonProps{
				Variant: components.ButtonText,
				Color:   components.ButtonBase,
				OnClick: func() {
					setSplit(!split())
				},
			}, func() kitex.Node {
				if split() {
					return kitex.Text("Show Unified")
				}
				return kitex.Text("Show Split")
			}()),
		},
			kitex.If(showModal(), func() kitex.Node {
				return components.DiffBlock(components.DiffBlockProps{
					Diff:  diffContent,
					Lang:  detectLang(filename),
					Split: split(),
				})
			}),
		),
	)
})

// MultiEditToolWidget renders the result of a multi_edit tool call inline.
var MultiEditToolWidget = kitex.FC("MultiEditToolWidget", func(props ToolExecutionProps) kitex.Node {
	t := theme.UseTheme()
	showModal, setShowModal := kitex.UseState(false)
	split, setSplit := kitex.UseState(false)

	tc := props.ToolCall
	tm := props.ToolMessage

	var path string
	if tc.Args != nil {
		path, _ = tc.Args["path"].(string)
	}
	filename := filepath.Base(path)

	var statusLabel string
	var iconNode kitex.Node
	var themeColor color.Color
	var diffContent string

	if t != nil {
		if tm == nil {
			statusLabel = fmt.Sprintf("Pending Multi-Edit [%s]", filename)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, kitex.Text(props.CurrentDots))
			themeColor = t.Color.Surface.Info
		} else if tm.IsError {
			statusLabel = fmt.Sprintf("Error Multi-Editing [%s]", filename)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Error)}, icon.Error)
			themeColor = t.Color.Text.Error
		} else {
			meOut, ok := parseMultiEditStructuredOutput(tm.StructuredContent)
			if ok && meOut.Success {
				statusLabel = fmt.Sprintf("Multi-Edited [%s +%d -%d]", filename, meOut.Additions, meOut.Deletions)
				diffContent = meOut.Diff
			} else {
				statusLabel = fmt.Sprintf("Multi-Edited (No Changes) [%s]", filename)
				diffContent = getToolOutput(tm.Content)
			}
			iconNode = nil
			themeColor = t.Color.Surface.Success
		}
	}

	var labelNode kitex.Node
	if t != nil {
		actionText := "Pending Multi-Edit "
		var additions, deletions int
		var hasDiffStats bool
		if tm != nil {
			if tm.IsError {
				actionText = "Error Multi-Editing "
			} else {
				actionText = "Wrote "
				meOut, ok := parseMultiEditStructuredOutput(tm.StructuredContent)
				if ok && meOut.Success {
					additions = meOut.Additions
					deletions = meOut.Deletions
					hasDiffStats = true
				}
			}
		}

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
					Background(t.Color.Surface.BaseFocus).
					PaddingHorizontal(1).
					Gap(1),
			},
				icon.FileIcon(icon.FileIconProps{Path: path}),
				kitex.Span(kitex.SpanProps{
					Style: style.S().
						Foreground(color.RGBA{255, 255, 255, 255}).
						Bold(true),
				}, kitex.Text(filename)),
				kitex.If(hasDiffStats, func() kitex.Node {
					return kitex.Box(kitex.BoxProps{
						Style: style.S().
							Display(style.DisplayFlex).
							FlexDirection(style.FlexRow).
							AlignItems(style.AlignCenter).
							Gap(1).
							MarginLeft(1),
					},
						kitex.Box(kitex.BoxProps{
							Style: style.S().
								Display(style.DisplayFlex).
								FlexDirection(style.FlexRow).
								AlignItems(style.AlignCenter).
								Foreground(t.Color.Surface.Success).
								Bold(true),
						},
							kitex.Text(fmt.Sprintf("+%d", additions)),
						),
						kitex.Box(kitex.BoxProps{
							Style: style.S().
								Display(style.DisplayFlex).
								FlexDirection(style.FlexRow).
								AlignItems(style.AlignCenter).
								Foreground(t.Color.Text.Error).
								Bold(true),
						},
							kitex.Text(fmt.Sprintf("-%d", deletions)),
						),
					)
				}),
			),
		)
	}

	var onClick func()
	if tm != nil && !tm.IsError && diffContent != "" {
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
				icon.FileIcon(icon.FileIconProps{Path: path}),
				kitex.Text(fmt.Sprintf("Multi-Edit Changes in %s", filename)),
			),
			OnClose: func() { setShowModal(false) },
			HeaderActions: components.Button(components.ButtonProps{
				Variant: components.ButtonText,
				Color:   components.ButtonBase,
				OnClick: func() {
					setSplit(!split())
				},
			}, func() kitex.Node {
				if split() {
					return kitex.Text("Show Unified")
				}
				return kitex.Text("Show Split")
			}()),
		},
			kitex.If(showModal(), func() kitex.Node {
				return components.DiffBlock(components.DiffBlockProps{
					Diff:  diffContent,
					Lang:  detectLang(filename),
					Split: split(),
				})
			}),
		),
	)
})

// RemoveToolWidget renders the result of a remove tool call inline.
var RemoveToolWidget = kitex.FC("RemoveToolWidget", func(props ToolExecutionProps) kitex.Node {
	t := theme.UseTheme()
	showModal, setShowModal := kitex.UseState(false)

	tc := props.ToolCall
	tm := props.ToolMessage

	var path string
	if tc.Args != nil {
		path, _ = tc.Args["path"].(string)
	}
	filename := filepath.Base(path)

	var statusLabel string
	var labelNode kitex.Node
	var iconNode kitex.Node
	var themeColor color.Color
	var details string

	if t != nil {
		var actionText string
		if tm == nil {
			actionText = "Pending Remove "
			statusLabel = fmt.Sprintf("Pending Remove [%s]", filename)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, kitex.Text(props.CurrentDots))
			themeColor = t.Color.Surface.Info
		} else if tm.IsError {
			actionText = "Error Removing "
			statusLabel = fmt.Sprintf("Error Removing [%s]", filename)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Error)}, icon.Error)
			themeColor = t.Color.Text.Error
			details = getToolOutput(tm.Content)
		} else {
			rOut, ok := parseRemoveStructuredOutput(tm.StructuredContent)
			if ok && rOut.Success {
				actionText = "Removed "
				statusLabel = fmt.Sprintf("Removed [%s]", filename)
				themeColor = t.Color.Surface.Success
			} else {
				actionText = "Failed to Remove "
				statusLabel = fmt.Sprintf("Failed to Remove [%s]", filename)
				themeColor = t.Color.Text.Error
			}
			iconNode = nil // remove checkmark completely on success
		}

		baseFocusBg := t.Color.Surface.BaseFocus

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
				icon.FileIcon(icon.FileIconProps{Path: path}),
				kitex.Span(kitex.SpanProps{
					Style: style.S().
						Foreground(color.RGBA{255, 255, 255, 255}).
						Bold(true),
				}, kitex.Text(filename)),
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
					return kitex.Span(kitex.SpanProps{}, kitex.Text(fmt.Sprintf("Error Removing %s", filename)))
				}),
				kitex.If(tm != nil && !tm.IsError, func() kitex.Node {
					return kitex.Fragment(
						icon.FileIcon(icon.FileIconProps{Path: path}),
						kitex.Span(kitex.SpanProps{}, kitex.Text(fmt.Sprintf("Removed %s", filename))),
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
				rOut, ok := parseRemoveStructuredOutput(tm.StructuredContent)
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
							FlexDirection(style.FlexRow).
							Gap(1),
					},
						kitex.Span(kitex.SpanProps{Style: style.S().Bold(true)}, kitex.Text("Path:")),
						kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Tertiary)}, kitex.Text(path)),
					),
					kitex.If(ok && rOut.IsBinary, func() kitex.Node {
						var textPrimary color.Color
						var textSecondary color.Color
						if t != nil {
							textPrimary = t.Color.Text.Primary
							textSecondary = t.Color.Text.Secondary
						}
						return kitex.Box(kitex.BoxProps{
							Style: style.S().
								Display(style.DisplayFlex).
								FlexDirection(style.FlexColumn).
								AlignItems(style.AlignCenter).
								JustifyContent(style.JustifyCenter).
								Gap(1).
								MarginTop(2).
								Padding(1),
						},
							kitex.Span(kitex.SpanProps{Style: style.S().Bold(true).Foreground(textPrimary)}, kitex.Text("Binary File Removed")),
							kitex.Span(kitex.SpanProps{Style: style.S().Foreground(textSecondary)}, kitex.Text(fmt.Sprintf("MimeType: %s (Text preview is not available)", rOut.MimeType))),
						)
					}),
					kitex.If(ok && !rOut.IsBinary && rOut.Content != "", func() kitex.Node {
						return kitex.Fragment(
							kitex.Box(kitex.BoxProps{
								Style: style.S().
									MarginTop(1).
									PaddingBottom(1),
							},
								kitex.Span(kitex.SpanProps{Style: style.S().Bold(true)}, kitex.Text("Deleted File Content:")),
							),
							components.CodeBlock(components.CodeBlockProps{
								Code:            rOut.Content,
								Lang:            detectLang(filename),
								HideHeader:      true,
								ShowLineNumbers: true,
								StartLine:       1,
							}),
						)
					}),
					kitex.If(!ok || (!rOut.IsBinary && rOut.Content == ""), func() kitex.Node {
						var statusMsg string
						if tm.IsError {
							statusMsg = "Failed to remove target (see error)."
						} else if ok && rOut.Success {
							statusMsg = "Successfully removed directory/target."
						} else {
							statusMsg = "Failed to remove target."
						}
						return kitex.Box(kitex.BoxProps{
							Style: style.S().
								MarginTop(1),
						},
							kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Secondary)}, kitex.Text(statusMsg)),
						)
					}),
				)
			}),
		),
	)
})

// BashToolWidget renders bash execution status, command input, and formatted stdout/stderr output.
var BashToolWidget = kitex.FC("BashToolWidget", func(props ToolExecutionProps) kitex.Node {
	t := theme.UseTheme()
	isOpen, setIsOpen := kitex.UseState(true)
	showModal, setShowModal := kitex.UseState(false)

	tc := props.ToolCall
	tm := props.ToolMessage

	// Extract command and description from args
	command := ""
	description := ""
	if tc != nil && len(tc.Args) > 0 {
		if cmdVal, ok := tc.Args["command"]; ok {
			command, _ = cmdVal.(string)
		}
		if descVal, ok := tc.Args["description"]; ok {
			description, _ = descVal.(string)
		}
	}

	containerStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexColumn).
		Width(style.Percent(100)).
		MaxWidth(style.Percent(100)).
		MinWidth(style.Percent(0)).
		Overflow(style.OverflowHidden)

	headerStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexRow).
		AlignItems(style.AlignCenter).
		JustifyContent(style.JustifyBetween).
		Padding(0, 1).
		Width(style.Percent(100)).
		MaxWidth(style.Percent(100)).
		Overflow(style.OverflowHidden)

	bodyStyle := style.S().
		Padding(1).
		Display(style.DisplayFlex).
		FlexDirection(style.FlexColumn).
		Gap(1).
		Width(style.Percent(100)).
		MaxWidth(style.Percent(100)).
		MinWidth(style.Percent(0)).
		Overflow(style.OverflowHidden)

	var iconNode kitex.Node
	var statusLabel string
	var headerBg color.Color
	var headerFg color.Color
	var borderCol color.Color

	// Determine status and widget color
	isFinished := tm != nil && (tm.GetMetadata() == nil || tm.GetMetadata()["status"] != "running")
	hasErr := false
	statusMsg := ""

	if isFinished {
		if tm.IsError {
			hasErr = true
		}
		// Try parsing exit code from structured content
		var exitCode int
		if tm.StructuredContent != nil {
			if m, ok := tm.StructuredContent.(map[string]any); ok {
				if ecVal, ok := m["exitCode"]; ok {
					switch ec := ecVal.(type) {
					case float64:
						exitCode = int(ec)
					case int:
						exitCode = ec
					case int64:
						exitCode = int(ec)
					}
					if exitCode != 0 {
						hasErr = true
					}
				}
				if statusVal, ok := m["status"]; ok {
					statusMsg, _ = statusVal.(string)
				}
			}
		}

		if statusMsg == "running" {
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, icon.Info)
			statusLabel = "BASH RUNNING IN BACKGROUND"
			headerBg = t.Color.Surface.BaseFocus
			headerFg = t.Color.Surface.Info
			borderCol = t.Color.Surface.Info
		} else if hasErr {
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Error)}, icon.Error)
			statusLabel = "BASH ERROR"
			if statusMsg != "" {
				statusLabel += fmt.Sprintf(" (%s)", strings.ToUpper(statusMsg))
			}
			headerBg = t.Color.Surface.BaseFocus
			headerFg = t.Color.Text.Error
			borderCol = t.Color.Text.Error
		} else {
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Success)}, icon.FontAwesomeTerminal)
			statusLabel = "BASH SUCCESS"
			headerBg = t.Color.Surface.BaseFocus
			headerFg = t.Color.Surface.Success
			borderCol = t.Color.Surface.Success
		}
	} else {
		iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, kitex.Text(props.CurrentDots))
		statusLabel = "RUNNING BASH COMMAND"
		headerBg = t.Color.Surface.BaseFocus
		headerFg = t.Color.Surface.Info
		borderCol = t.Color.Surface.Info
	}

	if t != nil {
		containerStyle = containerStyle.
			Border(true, style.SingleBorder(), borderCol).
			Background(t.Color.Surface.BaseHover)

		headerStyle = headerStyle.
			Background(headerBg).
			Foreground(headerFg)

		bodyStyle = bodyStyle.
			Background(t.Color.Surface.BaseHover)
	}

	return kitex.Fragment(
		kitex.Box(kitex.BoxProps{Style: containerStyle},
			components.Button(components.ButtonProps{
				Variant: components.ButtonText,
				Color:   components.ButtonBase,
				Style:   headerStyle,
				OnClick: func() {
					setIsOpen(!isOpen())
				},
			},
				kitex.Box(kitex.BoxProps{
					Style: style.S().
						Display(style.DisplayFlex).
						FlexDirection(style.FlexRow).
						AlignItems(style.AlignCenter).
						Gap(1),
				},
					iconNode,
					kitex.Span(kitex.SpanProps{Style: style.S().Bold(true)}, kitex.Text(" "+statusLabel)),
				),
				kitex.If(isFinished, func() kitex.Node {
					var label string
					if isOpen() {
						label = "▲ COLLAPSE"
					} else {
						label = "▼ EXPAND"
					}
					var textCol color.Color
					if t != nil {
						textCol = t.Color.Text.Secondary
					}
					return kitex.Span(kitex.SpanProps{
						Style: style.S().Foreground(textCol),
					}, kitex.Text(label))
				}),
			),
			kitex.If(isOpen(), func() kitex.Node {
				return kitex.Box(kitex.BoxProps{Style: bodyStyle},
					// Description
					kitex.If(description != "", func() kitex.Node {
						var textCol color.Color
						if t != nil {
							textCol = t.Color.Text.Primary
						}
						return kitex.Box(kitex.BoxProps{
							Style: style.S().
								Foreground(textCol).
								Italic(true),
						}, kitex.Text(description))
					}),
					// Input: codeblock without header or borders
					kitex.If(command != "", func() kitex.Node {
						var cbStyle style.Style
						if t != nil {
							cbStyle = style.S().Background(t.Color.Surface.Base)
						}
						return kitex.Box(kitex.BoxProps{
							Style: style.S().
								Width(style.Percent(100)).
								MinWidth(style.Percent(0)),
						},
							components.CodeBlock(components.CodeBlockProps{
								Code:       "$ " + command,
								Lang:       "bash",
								HideHeader: true,
								Wrap:       true,
								Compact:    true,
								Style:      cbStyle.Padding(1),
							}),
						)
					}),
					// Output (stdout/stderr) without outer header or borders
					kitex.If(tm != nil, func() kitex.Node {
						outText := getToolOutput(tm.Content)
						return kitex.Fragment(
							kitex.If(strings.TrimSpace(outText) != "", func() kitex.Node {
								lines := strings.Split(outText, "\n")
								isTruncated := len(lines) > 10

								var inlineText string
								if isTruncated {
									inlineText = strings.Join(lines[len(lines)-10:], "\n")
								} else {
									inlineText = outText
								}

								parts := strings.Split(inlineText, "[stderr]\n")
								var elements []kitex.Node

								var textCol color.Color
								if t != nil {
									textCol = t.Color.Text.Primary
								}

								outputContainerStyle := style.S().
									Display(style.DisplayFlex).
									FlexDirection(style.FlexColumn).
									Width(style.Percent(100)).
									MaxWidth(style.Percent(100)).
									MinWidth(style.Percent(0)).
									Overflow(style.OverflowHidden)

								// First part is stdout
								stdoutText := strings.TrimSpace(parts[0])
								if stdoutText != "" {
									stdoutText = strings.ReplaceAll(stdoutText, "\t", "    ")
									elements = append(elements, kitex.Box(kitex.BoxProps{
										Style: style.S().
											Foreground(textCol).
											Width(style.Percent(100)).
											MinWidth(style.Percent(0)).
											WhiteSpace(style.WhiteSpacePreWrap),
									}, kitex.Text(stdoutText)))
								}

								// Subsequent parts are stderr
								if len(parts) > 1 {
									stderrText := strings.TrimSpace(strings.Join(parts[1:], ""))
									if stderrText != "" {
										stderrText = strings.ReplaceAll(stderrText, "\t", "    ")
										elements = append(elements, kitex.Box(kitex.BoxProps{
											Style: style.S().
												Foreground(t.Color.Text.Error).
												Width(style.Percent(100)).
												MinWidth(style.Percent(0)).
												WhiteSpace(style.WhiteSpacePreWrap).
												MarginTop(1),
										}, kitex.Text("[stderr]\n"+stderrText)))
									}
								}

								var buttonNode kitex.Node
								if isTruncated {
									buttonNode = components.Button(components.ButtonProps{
										Variant: components.ButtonText,
										Color:   components.ButtonBase,
										Style: style.S().
											Foreground(t.Color.Surface.Info).
											MarginTop(1).
											Bold(true),
										OnClick: func() {
											setShowModal(true)
										},
									}, kitex.Fragment(
										kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, icon.FontAwesomeTerminal),
										kitex.Text(" VIEW FULL OUTPUT"),
									),
									)
								}

								return kitex.Box(kitex.BoxProps{
									Style: style.S().
										Display(style.DisplayFlex).
										FlexDirection(style.FlexColumn).
										Width(style.Percent(100)).
										MaxWidth(style.Percent(100)).
										Overflow(style.OverflowHidden),
								},
									kitex.Box(kitex.BoxProps{Style: outputContainerStyle}, elements...),
									buttonNode,
								)
							}),
						)
					}),
				)
			}),
		),
		components.Modal(components.ModalProps{
			IsOpen: showModal(),
			Title:  kitex.Text("Command Output"),
			OnClose: func() {
				setShowModal(false)
			},
		},
			kitex.If(showModal(), func() kitex.Node {
				outText := getToolOutput(tm.Content)
				parts := strings.Split(outText, "[stderr]\n")
				var elements []kitex.Node

				var textCol color.Color
				if t != nil {
					textCol = t.Color.Text.Primary
				}

				outputContainerStyle := style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexColumn).
					Background(t.Color.Surface.BaseHover).
					Padding(1).
					Width(style.Percent(100)).
					MaxWidth(style.Percent(100)).
					MinWidth(style.Percent(0)).
					Overflow(style.OverflowHidden)

				// First part is stdout
				stdoutText := strings.TrimSpace(parts[0])
				if stdoutText != "" {
					stdoutText = strings.ReplaceAll(stdoutText, "\t", "    ")
					elements = append(elements, kitex.Box(kitex.BoxProps{
						Style: style.S().
							Foreground(textCol).
							Width(style.Percent(100)).
							MinWidth(style.Percent(0)).
							WhiteSpace(style.WhiteSpacePreWrap),
					}, kitex.Text(stdoutText)))
				}

				// Subsequent parts are stderr
				if len(parts) > 1 {
					stderrText := strings.TrimSpace(strings.Join(parts[1:], ""))
					if stderrText != "" {
						stderrText = strings.ReplaceAll(stderrText, "\t", "    ")
						elements = append(elements, kitex.Box(kitex.BoxProps{
							Style: style.S().
								Foreground(t.Color.Text.Error).
								Width(style.Percent(100)).
								MinWidth(style.Percent(0)).
								WhiteSpace(style.WhiteSpacePreWrap).
								MarginTop(1),
						}, kitex.Text("[stderr]\n"+stderrText)))
					}
				}

				return kitex.Box(kitex.BoxProps{Style: outputContainerStyle}, elements...)
			}),
		),
	)
})

// TasksToolWidget renders background task queries action and result output.
// TasksToolWidget renders background task queries action and result output.
var TasksToolWidget = kitex.FC("TasksToolWidget", func(props ToolExecutionProps) kitex.Node {
	t := theme.UseTheme()
	isOpen, setIsOpen := kitex.UseState(true)
	showModal, setShowModal := kitex.UseState(false)

	tc := props.ToolCall
	tm := props.ToolMessage

	action := ""
	targetTaskId := ""
	if tc != nil && len(tc.Args) > 0 {
		if actVal, ok := tc.Args["action"]; ok {
			action, _ = actVal.(string)
		}
		if tidVal, ok := tc.Args["taskId"]; ok {
			targetTaskId, _ = tidVal.(string)
		}
	}

	containerStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexColumn).
		Width(style.Percent(100)).
		MaxWidth(style.Percent(100)).
		Overflow(style.OverflowHidden)

	headerStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexRow).
		AlignItems(style.AlignCenter).
		JustifyContent(style.JustifyBetween).
		Padding(0, 1).
		Width(style.Percent(100)).
		MaxWidth(style.Percent(100)).
		Overflow(style.OverflowHidden)

	bodyStyle := style.S().
		Padding(1).
		Display(style.DisplayFlex).
		FlexDirection(style.FlexColumn).
		Width(style.Percent(100)).
		MaxWidth(style.Percent(100)).
		Overflow(style.OverflowHidden)

	var iconNode kitex.Node
	var statusLabel string
	var headerBg color.Color
	var headerFg color.Color
	var borderCol color.Color

	isFinished := tm != nil
	hasErr := tm != nil && tm.IsError

	// Parse structured result
	var out tools.TasksOutput
	var hasStructured bool
	if tm != nil {
		out, hasStructured = parseTasksOutput(tm.StructuredContent)
	}

	if isFinished {
		if hasErr {
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Error)}, icon.Error)
			statusLabel = fmt.Sprintf("TASKS %s ERROR", strings.ToUpper(action))
			headerBg = t.Color.Surface.BaseFocus
			headerFg = t.Color.Text.Error
			borderCol = t.Color.Text.Error
		} else {
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Success)}, icon.Checkmark)
			statusLabel = fmt.Sprintf("TASKS %s SUCCESS", strings.ToUpper(action))
			headerBg = t.Color.Surface.BaseFocus
			headerFg = t.Color.Surface.Success
			borderCol = t.Color.Surface.Success
		}
	} else {
		iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, kitex.Text(props.CurrentDots))
		statusLabel = fmt.Sprintf("RUNNING TASKS %s", strings.ToUpper(action))
		headerBg = t.Color.Surface.BaseFocus
		headerFg = t.Color.Surface.Info
		borderCol = t.Color.Surface.Info
	}

	if t != nil {
		containerStyle = containerStyle.
			Border(true, style.SingleBorder(), borderCol).
			Background(t.Color.Surface.BaseHover)

		headerStyle = headerStyle.
			Background(headerBg).
			Foreground(headerFg)

		bodyStyle = bodyStyle.
			Background(t.Color.Surface.BaseHover)
	}

	return kitex.Fragment(
		kitex.Box(kitex.BoxProps{Style: containerStyle},
			components.Button(components.ButtonProps{
				Variant: components.ButtonText,
				Color:   components.ButtonBase,
				Style:   headerStyle,
				OnClick: func() {
					setIsOpen(!isOpen())
				},
			},
				kitex.Box(kitex.BoxProps{
					Style: style.S().
						Display(style.DisplayFlex).
						FlexDirection(style.FlexRow).
						AlignItems(style.AlignCenter).
						Gap(1),
				},
					iconNode,
					kitex.Span(kitex.SpanProps{Style: style.S().Bold(true)}, kitex.Text(" "+statusLabel)),
				),
				kitex.If(isFinished && action != "kill", func() kitex.Node {
					var label string
					if isOpen() {
						label = "▲ COLLAPSE"
					} else {
						label = "▼ EXPAND"
					}
					var textCol color.Color
					if t != nil {
						textCol = t.Color.Text.Secondary
					}
					return kitex.Span(kitex.SpanProps{
						Style: style.S().Foreground(textCol),
					}, kitex.Text(label))
				}),
			),
			kitex.If(isOpen(), func() kitex.Node {
				return kitex.Box(kitex.BoxProps{Style: bodyStyle},
					// Result Output depending on action
					kitex.If(tm != nil, func() kitex.Node {
						if hasStructured {
							if action == "list" {
								if len(out.Tasks) == 0 {
									return kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Secondary).Italic(true)}, kitex.Text("No active or background tasks found in this session."))
								}

								// Header row for table
								headerRow := kitex.TR(kitex.TRProps{},
									kitex.TD(kitex.TDProps{Style: style.S().Bold(true).Foreground(t.Color.Text.Secondary).PaddingRight(1).Width(style.MaxContent)}, kitex.Text("TASK ID")),
									kitex.TD(kitex.TDProps{Style: style.S().Bold(true).Foreground(t.Color.Text.Secondary).PaddingRight(1).Width(style.MaxContent)}, kitex.Text("STATUS")),
									kitex.TD(kitex.TDProps{Style: style.S().Bold(true).Foreground(t.Color.Text.Secondary).PaddingRight(1).Width(style.MaxContent)}, kitex.Text("STARTED")),
									kitex.TD(kitex.TDProps{Style: style.S().Bold(true).Foreground(t.Color.Text.Secondary).Width(style.Percent(100))}, kitex.Text("COMMAND / NAME")),
								)

								var taskRows []kitex.Node
								taskRows = append(taskRows, headerRow)

								for _, task := range out.Tasks {
									var statText string
									var statCol color.Color
									switch task.Status {
									case "running":
										statText = "● RUNNING"
										statCol = t.Color.Surface.Info
									case "finished", "completed":
										if task.ExitCode == 0 {
											statText = "✔ COMPLETED"
											statCol = t.Color.Surface.Success
										} else {
											statText = fmt.Sprintf("✘ FAILED (%d)", task.ExitCode)
											statCol = t.Color.Text.Error
										}
									case "killed":
										statText = "⏹ KILLED"
										statCol = t.Color.Text.Secondary
									default:
										statText = strings.ToUpper(task.Status)
										statCol = t.Color.Text.Primary
									}

									startedTime := task.StartedAt
									if pt, err := time.Parse(time.RFC3339, task.StartedAt); err == nil {
										startedTime = pt.Format("15:04:05")
									}

									shortId := task.TaskId
									if len(shortId) > 12 {
										shortId = shortId[:12] + "…"
									}

									taskRows = append(taskRows, kitex.TR(kitex.TRProps{},
										kitex.TD(kitex.TDProps{Style: style.S().Foreground(t.Color.Text.Secondary).PaddingRight(1).Width(style.MaxContent)}, kitex.Text(shortId)),
										kitex.TD(kitex.TDProps{Style: style.S().Foreground(statCol).PaddingRight(1).Width(style.MaxContent)}, kitex.Text(statText)),
										kitex.TD(kitex.TDProps{Style: style.S().Foreground(t.Color.Text.Secondary).PaddingRight(1).Width(style.MaxContent)}, kitex.Text(startedTime)),
										kitex.TD(kitex.TDProps{Style: style.S().Foreground(t.Color.Text.Primary).Width(style.Percent(100))}, kitex.Text(task.Name)),
									))
								}

								return kitex.Box(kitex.BoxProps{
									Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Width(style.Percent(100)),
								},
									kitex.Table(kitex.TableProps{},
										kitex.TBody(kitex.TBodyProps{}, taskRows...),
									),
								)
							}

							if action == "status" {
								var statusText string
								var statusCol color.Color
								switch out.Status {
								case "running":
									statusText = "● RUNNING"
									statusCol = t.Color.Surface.Info
								case "finished", "completed":
									if out.ExitCode == 0 {
										statusText = "✔ COMPLETED"
										statusCol = t.Color.Surface.Success
									} else {
										statusText = fmt.Sprintf("✘ FAILED (%d)", out.ExitCode)
										statusCol = t.Color.Text.Error
									}
								case "killed":
									statusText = "⏹ KILLED"
									statusCol = t.Color.Text.Secondary
								default:
									statusText = strings.ToUpper(out.Status)
									statusCol = t.Color.Text.Primary
								}

								stdoutLines := strings.Split(out.StdoutTail, "\n")
								stderrLines := strings.Split(out.StderrTail, "\n")

								isStdoutTruncated := len(stdoutLines) > 10
								isStderrTruncated := len(stderrLines) > 10
								hasAnyTruncation := isStdoutTruncated || isStderrTruncated

								var displayStdout string
								if isStdoutTruncated {
									displayStdout = strings.Join(stdoutLines[len(stdoutLines)-10:], "\n")
								} else {
									displayStdout = out.StdoutTail
								}

								var displayStderr string
								if isStderrTruncated {
									displayStderr = strings.Join(stderrLines[len(stderrLines)-10:], "\n")
								} else {
									displayStderr = out.StderrTail
								}

								var logElements []kitex.Node

								if strings.TrimSpace(displayStdout) != "" {
									logElements = append(logElements,
										kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Secondary).Bold(true).MarginBottom(1)}, kitex.Text("STDOUT:")),
										kitex.Box(kitex.BoxProps{
											Style: style.S().
												Foreground(t.Color.Text.Primary).
												Background(t.Color.Surface.BaseHover).
												Border(true, style.SingleBorder(), t.Color.Border.Primary).
												Padding(1).
												MarginBottom(1).
												WhiteSpace(style.WhiteSpacePreWrap),
										}, kitex.Text(strings.ReplaceAll(displayStdout, "\t", "    "))),
									)
								}

								if strings.TrimSpace(displayStderr) != "" {
									logElements = append(logElements,
										kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Error).Bold(true).MarginBottom(1)}, kitex.Text("STDERR:")),
										kitex.Box(kitex.BoxProps{
											Style: style.S().
												Foreground(t.Color.Text.Error).
												Background(t.Color.Surface.BaseHover).
												Border(true, style.SingleBorder(), t.Color.Text.Error).
												Padding(1).
												MarginBottom(1).
												WhiteSpace(style.WhiteSpacePreWrap),
										}, kitex.Text(strings.ReplaceAll(displayStderr, "\t", "    "))),
									)
								}

								if len(logElements) == 0 {
									logElements = append(logElements, kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Secondary).Italic(true)}, kitex.Text("No command output logged yet.")))
								}

								return kitex.Box(kitex.BoxProps{
									Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Width(style.Percent(100)),
								},
									kitex.Box(kitex.BoxProps{
										Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).AlignItems(style.AlignCenter).Gap(1).MarginBottom(1),
									},
										kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Secondary).Bold(true)}, kitex.Text("Status:")),
										kitex.Span(kitex.SpanProps{Style: style.S().Foreground(statusCol).Bold(true)}, kitex.Text(statusText)),
										kitex.If(out.Message != "", func() kitex.Node {
											return kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Secondary)}, kitex.Text(" — "+out.Message))
										}),
									),
									kitex.Box(kitex.BoxProps{
										Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn),
									}, logElements...),
									kitex.If(hasAnyTruncation, func() kitex.Node {
										return components.Button(components.ButtonProps{
											Variant: components.ButtonText,
											Color:   components.ButtonBase,
											Style: style.S().
												Foreground(t.Color.Surface.Info).
												MarginTop(1).
												Bold(true),
											OnClick: func() {
												setShowModal(true)
											},
										}, kitex.Fragment(
											kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, icon.FontAwesomeTerminal),
											kitex.Text(" VIEW FULL OUTPUT"),
										),
										)
									}),
								)
							}

							if action == "kill" {
								return kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Primary)}, kitex.Text(out.Message))
							}
						}

						// Fallback to text blocks
						outText := getToolOutput(tm.Content)
						if strings.TrimSpace(outText) != "" {
							return kitex.Box(kitex.BoxProps{
								Style: style.S().
									Foreground(t.Color.Text.Primary).
									WhiteSpace(style.WhiteSpacePreWrap),
							}, kitex.Text(outText))
						}
						return nil
					}),
				)
			}),
		),
		components.Modal(components.ModalProps{
			IsOpen: showModal(),
			Title:  kitex.Text(fmt.Sprintf("Task Logs: %s", targetTaskId)),
			OnClose: func() {
				setShowModal(false)
			},
		},
			kitex.If(showModal(), func() kitex.Node {
				var modalLogElements []kitex.Node

				if strings.TrimSpace(out.StdoutTail) != "" {
					modalLogElements = append(modalLogElements,
						kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Secondary).Bold(true).MarginBottom(1)}, kitex.Text("FULL STDOUT:")),
						kitex.Box(kitex.BoxProps{
							Style: style.S().
								Foreground(t.Color.Text.Primary).
								Padding(1).
								MarginBottom(1).
								WhiteSpace(style.WhiteSpacePreWrap),
						}, kitex.Text(strings.ReplaceAll(out.StdoutTail, "\t", "    "))),
					)
				}

				if strings.TrimSpace(out.StderrTail) != "" {
					modalLogElements = append(modalLogElements,
						kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Error).Bold(true).MarginBottom(1)}, kitex.Text("FULL STDERR:")),
						kitex.Box(kitex.BoxProps{
							Style: style.S().
								Foreground(t.Color.Text.Error).
								Padding(1).
								MarginBottom(1).
								WhiteSpace(style.WhiteSpacePreWrap),
						}, kitex.Text(strings.ReplaceAll(out.StderrTail, "\t", "    "))),
					)
				}

				return kitex.Fragment(modalLogElements...)
			}),
		),
	)
})

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

// ActivateSkillToolWidget renders the result of an activate_skill tool call inline, opening a modal on click.
var ActivateSkillToolWidget = kitex.FC("ActivateSkillToolWidget", func(props ToolExecutionProps) kitex.Node {
	t := theme.UseTheme()
	showModal, setShowModal := kitex.UseState(false)

	tc := props.ToolCall
	tm := props.ToolMessage

	var skillName string
	if tc != nil && tc.Args != nil {
		skillName, _ = tc.Args["skill"].(string)
	}

	var statusLabel string
	var labelNode kitex.Node
	var iconNode kitex.Node
	var themeColor color.Color
	var instructions string
	var hasInstructions bool
	var details string

	if tm != nil {
		out, ok := parseStructuredOutput[tools.ActivateSkillOutput](tm.StructuredContent)
		if ok && out.Success {
			instructions = out.Instructions
			hasInstructions = instructions != ""
		}
	}

	if t != nil {
		var actionText string
		if tm == nil {
			actionText = "Activating Skill "
			statusLabel = fmt.Sprintf("Activating Skill [%s]", skillName)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, kitex.Text(props.CurrentDots))
			themeColor = t.Color.Surface.Info
		} else if tm.IsError {
			actionText = "Error Activating Skill "
			statusLabel = fmt.Sprintf("Error Activating Skill [%s]", skillName)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Error)}, icon.Error)
			themeColor = t.Color.Text.Error
			details = getToolOutput(tm.Content)
		} else {
			out, ok := parseStructuredOutput[tools.ActivateSkillOutput](tm.StructuredContent)
			if ok && out.Success {
				actionText = "Activated Skill "
				statusLabel = fmt.Sprintf("Activated Skill [%s]", skillName)
				iconNode = nil // remove checkmark completely on success
				themeColor = t.Color.Surface.Success
			} else {
				actionText = "Failed to Activate Skill "
				statusLabel = fmt.Sprintf("Failed to Activate Skill [%s]", skillName)
				iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Error)}, icon.Error)
				themeColor = t.Color.Text.Error
			}
		}

		baseFocusBg := t.Color.Surface.BaseFocus
		pencilIconColor := t.Color.Surface.Info

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
				kitex.Span(kitex.SpanProps{Style: style.S().Foreground(pencilIconColor)}, icon.Pencil),
				kitex.Span(kitex.SpanProps{
					Style: style.S().
						Foreground(color.RGBA{255, 255, 255, 255}).
						Bold(true),
				}, kitex.Text(skillName)),
			),
		)
	}

	var onClick func()
	if tm != nil && (tm.IsError || hasInstructions) {
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
					return kitex.Span(kitex.SpanProps{}, kitex.Text(fmt.Sprintf("Error Activating Skill %s", skillName)))
				}),
				kitex.If(tm != nil && !tm.IsError, func() kitex.Node {
					return kitex.Fragment(
						kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, icon.Pencil),
						kitex.Span(kitex.SpanProps{}, kitex.Text(fmt.Sprintf("Instructions for %s Skill", skillName))),
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
			kitex.If(showModal() && tm != nil && !tm.IsError && hasInstructions, func() kitex.Node {
				return kitex.Box(kitex.BoxProps{
					Style: style.S().
						Padding(1).
						Width(style.Percent(100)).
						MinWidth(style.Percent(0)),
				},
					components.Markdown(components.MarkdownProps{
						Source: instructions,
					}),
				)
			}),
		),
	)
})

type localTodo struct {
	Description string
	Status      string
	ActiveText  string
}

func todoRow(t *theme.Scheme, description string, status string, activeText string) kitex.Node {
	checkIcon := "󰄱"
	var iconColor color.Color
	var textColor color.Color
	var activeTextNode kitex.Node

	if t != nil {
		iconColor = t.Color.Text.Tertiary
		textColor = t.Color.Text.Secondary

		if status == "completed" {
			checkIcon = "󰄲"
			iconColor = t.Color.Surface.Success
			textColor = t.Color.Text.Secondary
		} else if status == "in_progress" {
			checkIcon = "󰄰"
			iconColor = t.Color.Surface.Info
			textColor = t.Color.Text.Primary
			if activeText != "" {
				activeTextNode = kitex.Box(kitex.BoxProps{
					Style: style.S().
						Foreground(t.Color.Surface.Info).
						PaddingLeft(3).
						Italic(true),
				}, kitex.Text(activeText))
			}
		}
	}

	rowStyle := style.S().
		Display(style.DisplayFlex).
		AlignItems(style.AlignCenter).
		Gap(1)

	var iconSpan kitex.Node
	if t != nil {
		iconSpan = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(iconColor)}, kitex.Text(checkIcon))
	} else {
		iconSpan = kitex.Text(checkIcon)
	}

	var textSpan kitex.Node
	if t != nil {
		textSpan = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(textColor)}, kitex.Text(description))
	} else {
		textSpan = kitex.Text(description)
	}

	return kitex.Box(kitex.BoxProps{
		Style: style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexColumn),
	},
		kitex.Box(kitex.BoxProps{Style: rowStyle},
			iconSpan,
			textSpan,
		),
		kitex.If(activeTextNode != nil, func() kitex.Node {
			return activeTextNode
		}),
	)
}

// TodosToolWidget renders the list of subtasks as a collapsible accordion showing the in-progress status.
var TodosToolWidget = kitex.FC("TodosToolWidget", func(props ToolExecutionProps) kitex.Node {
	t := theme.UseTheme()
	showModal, setShowModal := kitex.UseState(false)

	tc := props.ToolCall
	tm := props.ToolMessage

	var todos []localTodo
	if tm != nil && !tm.IsError {
		out, ok := parseStructuredOutput[tools.TodosOutput](tm.StructuredContent)
		if ok {
			for _, item := range out.Todos {
				todos = append(todos, localTodo{
					Description: item.Description,
					Status:      item.Status,
					ActiveText:  item.ActiveText,
				})
			}
		}
	}
	if len(todos) == 0 && tc != nil && tc.Args != nil {
		inputArgs, ok := parseStructuredOutput[tools.TodosArgs](tc.Args)
		if ok {
			for _, item := range inputArgs.Todos {
				todos = append(todos, localTodo{
					Description: item.Description,
					Status:      item.Status,
					ActiveText:  item.ActiveText,
				})
			}
		}
	}

	// Calculate counts and identify active/in-progress task
	pendings := 0
	inProgress := 0
	completed := 0
	var activeTaskDesc string

	for _, item := range todos {
		switch item.Status {
		case "pending":
			pendings++
		case "in_progress":
			inProgress++
			if activeTaskDesc == "" {
				activeTaskDesc = item.Description
			}
		case "completed":
			completed++
		}
	}

	// Build status counts suffix
	var countParts []string
	if completed > 0 {
		countParts = append(countParts, fmt.Sprintf("%d completed", completed))
	}
	if inProgress > 0 {
		countParts = append(countParts, fmt.Sprintf("%d in progress", inProgress))
	}
	if pendings > 0 {
		countParts = append(countParts, fmt.Sprintf("%d pending", pendings))
	}

	var statusLabel string
	var labelNode kitex.Node
	var iconNode kitex.Node
	var themeColor color.Color
	var details string

	if tm != nil && tm.IsError {
		details = getToolOutput(tm.Content)
	}

	if t != nil {
		var actionText string
		if tm == nil {
			actionText = "Updating Checklist "
			statusLabel = "Updating Checklist"
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, kitex.Text(props.CurrentDots))
			themeColor = t.Color.Surface.Info
		} else if tm.IsError {
			actionText = "Checklist Error "
			statusLabel = "Checklist Error"
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Error)}, icon.Error)
			themeColor = t.Color.Text.Error
		} else {
			actionText = "Updated Checklist "
			statusLabel = "Checklist"
			iconNode = nil // remove checkmark completely on success
			themeColor = t.Color.Surface.Success
		}

		baseFocusBg := t.Color.Surface.BaseFocus
		calendarIconColor := t.Color.Surface.Info

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
				kitex.Span(kitex.SpanProps{Style: style.S().Foreground(calendarIconColor)}, icon.Calendar),
				kitex.Span(kitex.SpanProps{
					Style: style.S().
						Foreground(color.RGBA{255, 255, 255, 255}).
						Bold(true),
				}, kitex.Text("Checklist")),
			),
			kitex.If(len(countParts) > 0, func() kitex.Node {
				return kitex.Span(kitex.SpanProps{
					Style: style.S().
						Foreground(t.Color.Text.Secondary).
						PaddingLeft(1),
				}, kitex.Text(fmt.Sprintf("(%s)", strings.Join(countParts, ", "))))
			}),
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
					return kitex.Span(kitex.SpanProps{}, kitex.Text("Checklist Error"))
				}),
				kitex.If(tm != nil && !tm.IsError, func() kitex.Node {
					return kitex.Fragment(
						kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, icon.Calendar),
						kitex.Span(kitex.SpanProps{}, kitex.Text("Checklist")),
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
			kitex.If(showModal() && tm != nil && !tm.IsError && len(todos) > 0, func() kitex.Node {
				rows := make([]kitex.Node, len(todos))
				for i, item := range todos {
					rows[i] = todoRow(t, item.Description, item.Status, item.ActiveText)
				}
				return kitex.Box(kitex.BoxProps{
					Style: style.S().
						Display(style.DisplayFlex).
						FlexDirection(style.FlexColumn).
						Padding(1).
						Gap(0),
				}, rows...)
			}),
		),
	)
})

type AuthorizationWidgetProps struct {
	Request            permissions.AuthorizationRequest
	SelectedIndex      int
	SelectedScopeIndex int
	OnPreview          func()
	IsActive           bool
	IsFocused          bool
	OnSelectVertical   func(int)
	OnSelectHorizontal func(int)
	OnApprove          func()
	OnDeny             func()
}

func formatTargetLabel(opt permissions.PermissionOption) string {
	if opt.Target == "*" {
		return "All"
	}
	if strings.HasPrefix(opt.Target, "http://") || strings.HasPrefix(opt.Target, "https://") {
		return opt.Target
	}
	home, err := os.UserHomeDir()
	if err == nil && strings.HasPrefix(opt.Target, home) {
		return "~" + strings.TrimPrefix(opt.Target, home)
	}
	return opt.Target
}

type AuthorizationHybridSelectorProps struct {
	Options            []permissions.PermissionOption
	VerticalIndex      int // 0: Once, 1: Session, 2: Workspace, 3: Global, 4: Deny
	HorizontalIndex    int // index into Options
	IsActive           bool
	OnSelectVertical   func(int)
	OnSelectHorizontal func(int)
}

var AuthorizationHybridSelector = kitex.FC("AuthorizationHybridSelector", func(props AuthorizationHybridSelectorProps) kitex.Node {
	t := theme.UseTheme()
	if t == nil {
		return nil
	}

	scopesList := []struct {
		Name        string
		Description string
		Scope       permissions.PermissionScope
	}{
		{Name: "Once", Description: "Allow this action only once"},
		{Name: "Session", Description: "Allow for the duration of this session"},
		{Name: "Workspace", Description: "Allow for this workspace (local configuration)"},
		{Name: "Global", Description: "Allow globally across all projects"},
		{Name: "Deny", Description: "Deny execution of this tool call"},
	}

	var rows []kitex.Node
	for idx, s := range scopesList {
		// Use a local copy of idx for safe closure capture
		rowIdx := idx
		isVSelected := props.VerticalIndex == rowIdx && props.IsActive

		rowStyle := style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexColumn).
			Padding(0, 1).
			MarginVertical(0)

		lblStyle := style.S()
		if isVSelected {
			lblStyle = lblStyle.
				Foreground(t.Color.Surface.Info).
				Bold(true)
			rowStyle = rowStyle.Background(t.Color.Surface.BaseHover)
		} else {
			lblStyle = lblStyle.Foreground(t.Color.Text.Secondary)
		}

		checkbox := "○"
		if isVSelected {
			checkbox = "●"
		}

		hasHorizontal := rowIdx == 1 || rowIdx == 2 || rowIdx == 3
		var horizNode kitex.Node
		if isVSelected && hasHorizontal && len(props.Options) > 1 {
			var pills []kitex.Node
			pills = append(pills, kitex.Span(kitex.SpanProps{
				Style: style.S().Foreground(t.Color.Text.Secondary).PaddingRight(1),
			}, kitex.Text("Limit to:")))

			for hIdx, opt := range props.Options {
				pillIdx := hIdx
				isHSelected := props.HorizontalIndex == pillIdx
				label := formatTargetLabel(opt)

				pillStyle := style.S().
					MarginRight(1)

				var text string
				if isHSelected {
					pillStyle = pillStyle.
						Foreground(t.Color.Surface.Success).
						Bold(true)
					text = fmt.Sprintf("[%s]", label)
				} else {
					pillStyle = pillStyle.
						Foreground(t.Color.Text.Secondary)
					text = fmt.Sprintf(" %s ", label)
				}

				pills = append(pills, kitex.Box(kitex.BoxProps{
					Style: pillStyle,
					OnClick: func(e event.Event) {
						if props.OnSelectHorizontal != nil {
							props.OnSelectHorizontal(pillIdx)
						}
					},
				}, kitex.Text(text)))
			}

			horizNode = kitex.Box(kitex.BoxProps{
				Style: style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexRow).
					AlignItems(style.AlignCenter).
					PaddingLeft(5).
					PaddingTop(0).
					PaddingBottom(0),
			}, pills...)
		}

		rows = append(rows, kitex.Box(kitex.BoxProps{
			Style: rowStyle,
			OnClick: func(e event.Event) {
				if props.OnSelectVertical != nil {
					props.OnSelectVertical(rowIdx)
				}
			},
		},
			kitex.Box(kitex.BoxProps{
				Style: style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexRow).
					Gap(1).
					PaddingVertical(0),
			},
				kitex.Span(kitex.SpanProps{Style: lblStyle}, kitex.Text(fmt.Sprintf("%s [%s]", checkbox, s.Name))),
				kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Secondary)}, kitex.Text(s.Description)),
			),
			kitex.If(horizNode != nil, func() kitex.Node { return horizNode }),
		))
	}

	return kitex.Box(kitex.BoxProps{
		Style: style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexColumn).
			Gap(0),
	}, rows...)
})

var AuthorizationWidget = kitex.FC("AuthorizationWidget", func(props AuthorizationWidgetProps) kitex.Node {
	t := theme.UseTheme()
	if t == nil {
		return nil
	}

	req := props.Request

	warningColor := color.Color(color.RGBA{R: 224, G: 153, B: 36, A: 255})
	warningFocusColor := color.Color(color.RGBA{R: 224, G: 153, B: 36, A: 40})

	borderColor := t.Color.Border.Primary
	if props.IsActive {
		if props.IsFocused {
			borderColor = t.Color.Surface.Info
		} else {
			borderColor = t.Color.Border.Primary
		}
	}

	containerStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexColumn).
		Width(style.Percent(100)).
		MaxWidth(style.Percent(100)).
		Overflow(style.OverflowHidden).
		Border(true, style.SingleBorder(), borderColor).
		Background(t.Color.Surface.BaseHover)

	headerStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexRow).
		AlignItems(style.AlignCenter).
		Gap(1).
		PaddingBottom(1)

	titleColor := t.Color.Text.Secondary
	if props.IsActive {
		if props.IsFocused {
			titleColor = warningColor
		} else {
			titleColor = t.Color.Text.Secondary
		}
	}

	titleStyle := style.S().
		Bold(true).
		Foreground(titleColor)

	// Render hints (only if active)
	var hintNodes []kitex.Node
	if props.IsActive {
		for _, hint := range req.SystemHints {
			hintNodes = append(hintNodes, kitex.Box(kitex.BoxProps{
				Style: style.S().
					Background(warningFocusColor).
					Foreground(warningColor).
					Padding(0, 1).
					MarginBottom(1),
			}, kitex.Text(hint)))
		}
	}

	return kitex.Box(kitex.BoxProps{Style: containerStyle},
		kitex.Box(kitex.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexColumn).
				Padding(1, 1, 0, 1).
				Width(style.Percent(100)).
				MaxWidth(style.Percent(100)).
				Overflow(style.OverflowHidden),
		},
			// Header
			kitex.Box(kitex.BoxProps{Style: headerStyle},
				kitex.Span(kitex.SpanProps{Style: style.S().Foreground(titleColor)}, icon.Alert),
				kitex.Span(kitex.SpanProps{Style: titleStyle}, kitex.Text("AUTHORIZATION REQUIRED")),
				kitex.If(!props.IsActive, func() kitex.Node {
					return kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Tertiary).Italic(true)}, kitex.Text(" (Queued)"))
				}),
				kitex.If(props.IsActive && !props.IsFocused, func() kitex.Node {
					return kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Tertiary).Italic(true)}, kitex.Text(" (Unfocused)"))
				}),
			),

			// Hints
			kitex.If(len(hintNodes) > 0, func() kitex.Node {
				return kitex.Box(kitex.BoxProps{
					Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn),
				}, hintNodes...)
			}),

			// Tool details
			kitex.Box(kitex.BoxProps{
				Style: style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexRow).
					Gap(1).
					PaddingBottom(1),
			},
				kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Secondary)}, kitex.Text("Tool:")),
				kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Magenta).Bold(true)}, kitex.Text(req.ToolName)),
			),
			kitex.If(len(req.Options) > 0, func() kitex.Node {
				return kitex.Box(kitex.BoxProps{
					Style: style.S().
						Display(style.DisplayFlex).
						FlexDirection(style.FlexRow).
						Gap(1).
						PaddingBottom(1),
				},
					kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Secondary)}, kitex.Text("Target:")),
					kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Purple).Bold(true)}, kitex.Text(req.Options[0].Target)),
				)
			}),

			// Hybrid Scope & Target Selector
			kitex.Box(kitex.BoxProps{
				Style: style.S().PaddingBottom(0),
			},
				AuthorizationHybridSelector(AuthorizationHybridSelectorProps{
					Options:            req.Options,
					VerticalIndex:      props.SelectedIndex,
					HorizontalIndex:    props.SelectedScopeIndex,
					IsActive:           props.IsActive,
					OnSelectVertical:   props.OnSelectVertical,
					OnSelectHorizontal: props.OnSelectHorizontal,
				}),
			),

			// Action Buttons (only if active)
			kitex.If(props.IsActive, func() kitex.Node {
				var btnNodes []kitex.Node
				btnNodes = append(btnNodes, components.Button(components.ButtonProps{
					Variant: components.ButtonText,
					Color:   components.ButtonSuccess,
					Style:   style.S().MarginRight(1),
					OnClick: func() {
						if props.OnApprove != nil {
							props.OnApprove()
						}
					},
				}, kitex.Text("Approve [Enter]")))

				btnNodes = append(btnNodes, components.Button(components.ButtonProps{
					Variant: components.ButtonText,
					Color:   components.ButtonError,
					Style:   style.S().MarginRight(1),
					OnClick: func() {
						if props.OnDeny != nil {
							props.OnDeny()
						}
					},
				}, kitex.Text("Deny [d]")))

				if req.Preview != "" {
					btnNodes = append(btnNodes, components.Button(components.ButtonProps{
						Variant: components.ButtonText,
						Color:   components.ButtonPrimary,
						Style:   style.S().MarginRight(1),
						OnClick: func() {
							if props.OnPreview != nil {
								props.OnPreview()
							}
						},
					}, kitex.Text("Preview [p]")))
				}

				return kitex.Box(kitex.BoxProps{
					Style: style.S().
						Display(style.DisplayFlex).
						FlexDirection(style.FlexRow).
						AlignItems(style.AlignCenter).
						MarginTop(1).
						MarginBottom(0),
				}, btnNodes...)
			}),

			// Instructions (only if active)
			kitex.If(props.IsActive, func() kitex.Node {
				return kitex.Box(kitex.BoxProps{
					Style: style.S().
						Border(style.SingleBorder().Color(t.Color.Border.Primary)).
						Padding(0, 1).
						MarginTop(1).
						Foreground(t.Color.Text.Secondary).
						Width(style.Percent(100)),
				},
					func() kitex.Node {
						if props.IsFocused {
							text := "[j/k] Navigate Scope"
							if len(req.Options) > 1 && (props.SelectedIndex == 1 || props.SelectedIndex == 2 || props.SelectedIndex == 3) {
								text += "    [h/l] Limit Target"
							}
							text += "    [Enter] Approve    [d / Esc] Deny"
							if req.Preview != "" {
								text += "    [p] Preview"
							}
							return kitex.Text(text)
						} else {
							return kitex.Text("Composer focused    [Esc] Focus widget")
						}
					}(),
				)
			}),
		),
	)
})

// parseLspDiagnosticsOutput extracts diagnostics from the tool's StructuredContent.
func parseLspDiagnosticsOutput(structured any) (diags []tools.LspDiagnosticsOutputDiagnosticsItem, totalCount int, truncated bool) {
	out, ok := parseStructuredOutput[tools.LspDiagnosticsOutput](structured)
	if ok {
		return out.Diagnostics, out.TotalCount, out.Truncated
	}
	return
}

// parseLspRestartOutput extracts restart output from the tool's StructuredContent.
func parseLspRestartOutput(structured any) (out tools.LspRestartOutput, ok bool) {
	return parseStructuredOutput[tools.LspRestartOutput](structured)
}

// parseLspSearchOutput extracts search results from the tool's StructuredContent.
func parseLspSearchOutput(structured any) (results []tools.LspSearchOutputResultsItem) {
	out, ok := parseStructuredOutput[tools.LspSearchOutput](structured)
	if ok {
		return out.Results
	}
	return nil
}

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

// LspSearchToolWidget renders the result of an lsp_search tool call inline.
var LspSearchToolWidget = kitex.FC("LspSearchToolWidget", func(props ToolExecutionProps) kitex.Node {
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

	var results []tools.LspSearchOutputResultsItem
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
			results = parseLspSearchOutput(tm.StructuredContent)
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
