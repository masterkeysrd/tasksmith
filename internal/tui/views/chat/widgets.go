package chat

import (
	"fmt"
	"image/color"
	"path/filepath"
	"strings"
	"time"

	"github.com/masterkeysrd/kite/dom"
	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/key"
	"github.com/masterkeysrd/kite/style"
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
		Style:   style.S().MarginVertical(1),
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
					return kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Tertiary).Bold(true)}, kitex.Text("THINKING PROCESS"))
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

var ViewToolWidget = kitex.FC("ViewToolWidget", func(props ToolExecutionProps) kitex.Node {
	t := theme.UseTheme()
	showModal, setShowModal := kitex.UseState(false)
	modalRef := kitex.CreateRef[dom.Element]()

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
			var rangeStr string
			startLine := getIntField(tc.Args, "start_line")
			endLine := getIntField(tc.Args, "end_line")
			if startLine > 0 {
				if endLine > 0 {
					rangeStr = fmt.Sprintf(" (%d-%d)", startLine, endLine)
				} else {
					rangeStr = fmt.Sprintf(" (%d+)", startLine)
				}
			}
			statusLabel = fmt.Sprintf("Pending [%s%s]", filename, rangeStr)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, kitex.Text(props.CurrentDots))
			themeColor = t.Color.Surface.Info
		} else if tm.IsError {
			statusLabel = fmt.Sprintf("Error Reading [%s]", filename)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Error)}, icon.Error)
			themeColor = t.Color.Text.Error
		} else {
			vOut, ok := parseViewStructuredOutput(tm.StructuredContent)
			if ok && vOut.IsBinary {
				statusLabel = fmt.Sprintf("Binary [%s] (%s)", filename, vOut.MimeType)
			} else {
				actualStart, actualEnd, _, _ := parseViewOutput(tm.StructuredContent)
				if actualStart == 0 {
					outText := getToolOutput(tm.Content)
					actualStart, actualEnd = parseRangeFromHeader(outText)
				}
				if actualStart == 0 {
					actualStart = getIntField(tc.Args, "start_line")
					if actualStart == 0 {
						actualStart = 1
					}
					actualEnd = getIntField(tc.Args, "end_line")
				}
				var rangeStr string
				if actualStart > 0 && actualEnd > 0 {
					rangeStr = fmt.Sprintf(" %d-%d", actualStart, actualEnd)
				}
				statusLabel = fmt.Sprintf("Read [%s%s]", filename, rangeStr)
			}
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Success)}, icon.Checkmark)
			themeColor = t.Color.Surface.Success
		}
	}

	boxStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexRow).
		AlignItems(style.AlignCenter).
		AlignSelf(style.AlignStart).
		Padding(0, 1).
		Gap(1).
		Height(style.Cells(1)).
		MarginVertical(1)

	if t != nil {
		boxStyle = boxStyle.
			Background(t.Color.Surface.BaseHover).
			Foreground(themeColor)
	}

	kitex.UseEffect(func() {
		if showModal() {
			kitex.PostMacro(func() {
				if modalRef.Current != nil {
					if doc := modalRef.Current.OwnerDocument(); doc != nil {
						doc.Focus(modalRef.Current)
					}
				}
			})
		}
	}, []any{showModal()})

	var badgeNode kitex.Node
	if tm != nil && !tm.IsError {
		badgeNode = components.Button(components.ButtonProps{
			Variant: components.ButtonText,
			Color:   components.ButtonBase,
			Style:   boxStyle,
			OnClick: func() {
				setShowModal(true)
			},
		},
			iconNode,
			kitex.Span(kitex.SpanProps{Style: style.S().Bold(true)}, kitex.Text(statusLabel)),
		)
	} else {
		badgeNode = kitex.Box(kitex.BoxProps{Style: boxStyle},
			iconNode,
			kitex.Span(kitex.SpanProps{Style: style.S().Bold(true)}, kitex.Text(statusLabel)),
		)
	}

	return kitex.Fragment(
		badgeNode,
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

			modalStyle := style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexColumn).
				Width(style.Percent(80)).
				Height(style.Percent(80)).
				Padding(1).
				Overflow(style.OverflowHidden)

			return kitex.Dialog(kitex.DialogProps{
				ZIndex: 100,
				Ref:    modalRef,
				OnKeyDown: func(e event.Event) {
					ke, ok := e.(*event.KeyEvent)
					if !ok {
						return
					}
					if ke.Code == key.KeyEscape || ke.Text == "q" {
						e.PreventDefault()
						e.StopPropagation()
						setShowModal(false)
					}
				},
			},
				components.Paper(components.PaperProps{
					Color:   components.PaperBase,
					Variant: components.PaperOutlined,
					Style:   modalStyle,
				},
					kitex.Box(kitex.BoxProps{
						Style: style.S().
							Display(style.DisplayFlex).
							FlexDirection(style.FlexRow).
							JustifyContent(style.JustifyBetween).
							AlignItems(style.AlignCenter).
							PaddingBottom(1).
							BorderBottom(true, style.SingleBorder()),
					},
						kitex.Span(kitex.SpanProps{Style: style.S().Bold(true)}, kitex.Text(fmt.Sprintf("Viewing %s", filename))),
						components.Button(components.ButtonProps{
							Variant: components.ButtonText,
							Color:   components.ButtonBase,
							OnClick: func() {
								setShowModal(false)
							},
						}, kitex.Text("Close [Esc/q]")),
					),
					kitex.Box(kitex.BoxProps{
						Style: style.S().
							Flex(1, 1, style.Cells(0)).
							MinHeight(style.Cells(0)).
							OverflowY(style.OverflowAuto).
							MarginTop(1),
					},
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
									kitex.Span(kitex.SpanProps{Style: style.S().Foreground(textSecondary)}, kitex.Text(fmt.Sprintf("  • Path:      %s", vOut.Path))),
								),
								components.Button(components.ButtonProps{
									Variant: components.ButtonSolid,
									Color:   components.ButtonPrimary,
									Style: style.S().
										AlignSelf(style.AlignStart).
										MarginTop(1).
										Padding(0, 2),
									OnClick: func() {
										openWithSystemViewer(vOut.Path)
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
					),
				),
			)
		}),
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
	accordionStyle := style.S().MarginVertical(1)
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
	var iconNode kitex.Node
	var borderCol color.Color

	var matches []string
	var totalCount int
	var truncated bool

	if t != nil {
		if tm == nil {
			statusLabel = fmt.Sprintf("Glob: Searching%s for [%s]", scope, pattern)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, kitex.Text(props.CurrentDots))
			borderCol = t.Color.Surface.Info
		} else if tm.IsError {
			statusLabel = fmt.Sprintf("Glob: Error searching%s for [%s]", scope, pattern)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Error)}, icon.Error)
			borderCol = t.Color.Text.Error
		} else {
			matches, totalCount, truncated = parseGlobOutput(tm.StructuredContent)
			matchWord := "matches"
			if totalCount == 1 {
				matchWord = "match"
			}
			statusLabel = fmt.Sprintf("Glob: Found %d %s%s for [%s]", totalCount, matchWord, scope, pattern)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Success)}, icon.Checkmark)
			borderCol = t.Color.Surface.Success
		}
	}

	accordionStyle := style.S().MarginVertical(1)
	if t != nil {
		accordionStyle = accordionStyle.Border(borderCol)
	}

	return components.Accordion(components.AccordionProps{
		Color:   components.PaperHover,
		Variant: components.PaperOutlined,
		Style:   accordionStyle,
	},
		components.AccordionSummary(components.AccordionSummaryProps{
			HideExpandIcon: tm == nil || tm.IsError || len(matches) == 0,
			EndContent: kitex.If(tm != nil && !tm.IsError && len(matches) > 0, func() kitex.Node {
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
			kitex.If(tm != nil && !tm.IsError && len(matches) > 0, func() kitex.Node {
				rows := make([]kitex.Node, 0, len(matches))
				for _, match := range matches {
					rows = append(rows, globEntryRow(t, match))
				}

				var textCol color.Color
				if t != nil {
					textCol = t.Color.Text.Tertiary
				}
				return kitex.Box(kitex.BoxProps{
					Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn),
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

			kitex.If(tm != nil && !tm.IsError && len(matches) == 0, func() kitex.Node {
				var textCol color.Color
				if t != nil {
					textCol = t.Color.Text.Tertiary
				}
				return kitex.Box(kitex.BoxProps{
					Style: style.S().Foreground(textCol).Italic(true).PaddingHorizontal(1),
				}, kitex.Text("(no matches found)"))
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
		dirColor = t.Color.Text.Tertiary
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
		kitex.Span(kitex.SpanProps{Style: style.S().Foreground(dirColor)}, kitex.Text(dirPart)),
		kitex.Span(kitex.SpanProps{Style: style.S().Foreground(nameColor).Bold(true)}, kitex.Text(filePart)),
	)
}

// GrepToolWidget renders the result of a grep tool call inline.
var GrepToolWidget = kitex.FC("GrepToolWidget", func(props ToolExecutionProps) kitex.Node {
	t := theme.UseTheme()

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
	var iconNode kitex.Node
	var borderCol color.Color

	var matches []tools.GrepOutputMatchesItem
	var totalCount int
	var truncated bool

	if t != nil {
		if tm == nil {
			statusLabel = fmt.Sprintf("Grep: Searching%s for [%s]", scope, pattern)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, kitex.Text(props.CurrentDots))
			borderCol = t.Color.Surface.Info
		} else if tm.IsError {
			statusLabel = fmt.Sprintf("Grep: Error searching%s for [%s]", scope, pattern)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Error)}, icon.Error)
			borderCol = t.Color.Text.Error
		} else {
			matches, totalCount, truncated = parseGrepOutput(tm.StructuredContent)
			matchWord := "matches"
			if totalCount == 1 {
				matchWord = "match"
			}
			statusLabel = fmt.Sprintf("Grep: Found %d %s%s for [%s]", totalCount, matchWord, scope, pattern)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Success)}, icon.Checkmark)
			borderCol = t.Color.Surface.Success
		}
	}

	accordionStyle := style.S().MarginVertical(1)
	if t != nil {
		accordionStyle = accordionStyle.Border(borderCol)
	}

	return components.Accordion(components.AccordionProps{
		Color:   components.PaperHover,
		Variant: components.PaperOutlined,
		Style:   accordionStyle,
	},
		components.AccordionSummary(components.AccordionSummaryProps{
			HideExpandIcon: tm == nil || tm.IsError || len(matches) == 0,
			EndContent: kitex.If(tm != nil && !tm.IsError && len(matches) > 0, func() kitex.Node {
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
			kitex.If(tm != nil && !tm.IsError && len(matches) > 0, func() kitex.Node {
				rows := make([]kitex.Node, 0, len(matches))
				var currentFile string
				firstFile := true
				for _, match := range matches {
					if match.Path != currentFile {
						currentFile = match.Path
						var fg color.Color
						if t != nil {
							fg = t.Color.Surface.Info
						}
						var headerStyle style.Style
						if firstFile {
							headerStyle = style.S().
								Foreground(fg).
								Bold(true).
								PaddingHorizontal(0)
							firstFile = false
						} else {
							headerStyle = style.S().
								Foreground(fg).
								Bold(true).
								PaddingTop(1).
								PaddingHorizontal(0)
						}
						rows = append(rows, kitex.Box(kitex.BoxProps{
							Style: headerStyle,
						}, kitex.Text(filepath.ToSlash(match.Path)+":")))
					}

					rows = append(rows, grepEntryRow(t, match))
				}

				var textCol color.Color
				if t != nil {
					textCol = t.Color.Text.Tertiary
				}
				return kitex.Box(kitex.BoxProps{
					Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn),
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

			kitex.If(tm != nil && !tm.IsError && len(matches) == 0, func() kitex.Node {
				var textCol color.Color
				if t != nil {
					textCol = t.Color.Text.Tertiary
				}
				return kitex.Box(kitex.BoxProps{
					Style: style.S().Foreground(textCol).Italic(true).PaddingHorizontal(1),
				}, kitex.Text("(no matches found)"))
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
	modalRef := kitex.CreateRef[dom.Element]()

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
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Success)}, icon.Checkmark)
			themeColor = t.Color.Surface.Success
		}
	}

	boxStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexRow).
		AlignItems(style.AlignCenter).
		AlignSelf(style.AlignStart).
		Padding(0, 1).
		Gap(1).
		Height(style.Cells(1)).
		MarginVertical(1)

	if t != nil {
		boxStyle = boxStyle.
			Background(t.Color.Surface.BaseHover).
			Foreground(themeColor)
	}

	kitex.UseEffect(func() {
		if showModal() {
			kitex.PostMacro(func() {
				if modalRef.Current != nil {
					if doc := modalRef.Current.OwnerDocument(); doc != nil {
						doc.Focus(modalRef.Current)
					}
				}
			})
		}
	}, []any{showModal()})

	var badgeNode kitex.Node
	if content != "" {
		badgeNode = components.Button(components.ButtonProps{
			Variant: components.ButtonText,
			Color:   components.ButtonBase,
			Style:   boxStyle,
			OnClick: func() {
				setShowModal(true)
			},
		},
			iconNode,
			kitex.Span(kitex.SpanProps{Style: style.S().Bold(true)}, kitex.Text(statusLabel)),
		)
	} else {
		badgeNode = kitex.Box(kitex.BoxProps{Style: boxStyle},
			iconNode,
			kitex.Span(kitex.SpanProps{Style: style.S().Bold(true)}, kitex.Text(statusLabel)),
		)
	}

	modalStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexColumn).
		Width(style.Percent(80)).
		Height(style.Percent(80)).
		Padding(1).
		Overflow(style.OverflowHidden)

	return kitex.Fragment(
		badgeNode,
		kitex.If(showModal(), func() kitex.Node {
			return kitex.Dialog(kitex.DialogProps{
				ZIndex: 100,
				Ref:    modalRef,
				OnKeyDown: func(e event.Event) {
					ke, ok := e.(*event.KeyEvent)
					if !ok {
						return
					}
					if ke.Code == key.KeyEscape || ke.Text == "q" {
						e.PreventDefault()
						e.StopPropagation()
						setShowModal(false)
					}
				},
			},
				components.Paper(components.PaperProps{
					Color:   components.PaperBase,
					Variant: components.PaperOutlined,
					Style:   modalStyle,
				},
					kitex.Box(kitex.BoxProps{
						Style: style.S().
							Display(style.DisplayFlex).
							FlexDirection(style.FlexRow).
							JustifyContent(style.JustifyBetween).
							AlignItems(style.AlignCenter).
							PaddingBottom(1).
							BorderBottom(true, style.SingleBorder()),
					},
						kitex.Span(kitex.SpanProps{Style: style.S().Bold(true)}, kitex.Text(fmt.Sprintf("Wrote Content for %s", filename))),
						components.Button(components.ButtonProps{
							Variant: components.ButtonText,
							Color:   components.ButtonBase,
							OnClick: func() {
								setShowModal(false)
							},
						}, kitex.Text("Close [Esc/q]")),
					),
					kitex.Box(kitex.BoxProps{
						Style: style.S().
							Flex(1, 1, style.Cells(0)).
							MinHeight(style.Cells(0)).
							OverflowY(style.OverflowAuto).
							MarginTop(1),
					},
						components.CodeBlock(components.CodeBlockProps{
							Code:            content,
							Lang:            detectLang(filename),
							HideHeader:      true,
							ShowLineNumbers: true,
							StartLine:       1,
						}),
					),
				),
			)
		}),
	)
})

// EditToolWidget renders the result of an edit tool call inline.
var EditToolWidget = kitex.FC("EditToolWidget", func(props ToolExecutionProps) kitex.Node {
	t := theme.UseTheme()
	showModal, setShowModal := kitex.UseState(false)
	split, setSplit := kitex.UseState(false)
	modalRef := kitex.CreateRef[dom.Element]()

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
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Success)}, icon.Checkmark)
			themeColor = t.Color.Surface.Success
		}
	}

	boxStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexRow).
		AlignItems(style.AlignCenter).
		AlignSelf(style.AlignStart).
		Padding(0, 1).
		Gap(1).
		Height(style.Cells(1)).
		MarginVertical(1)

	if t != nil {
		boxStyle = boxStyle.
			Background(t.Color.Surface.BaseHover).
			Foreground(themeColor)
	}

	kitex.UseEffect(func() {
		if showModal() {
			kitex.PostMacro(func() {
				if modalRef.Current != nil {
					if doc := modalRef.Current.OwnerDocument(); doc != nil {
						doc.Focus(modalRef.Current)
					}
				}
			})
		}
	}, []any{showModal()})

	var badgeNode kitex.Node
	if tm != nil && !tm.IsError && diffContent != "" {
		badgeNode = components.Button(components.ButtonProps{
			Variant: components.ButtonText,
			Color:   components.ButtonBase,
			Style:   boxStyle,
			OnClick: func() {
				setShowModal(true)
			},
		},
			iconNode,
			kitex.Span(kitex.SpanProps{Style: style.S().Bold(true)}, kitex.Text(statusLabel)),
		)
	} else {
		badgeNode = kitex.Box(kitex.BoxProps{Style: boxStyle},
			iconNode,
			kitex.Span(kitex.SpanProps{Style: style.S().Bold(true)}, kitex.Text(statusLabel)),
		)
	}

	modalStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexColumn).
		Width(style.Percent(80)).
		Height(style.Percent(80)).
		Padding(1).
		Overflow(style.OverflowHidden)

	return kitex.Fragment(
		badgeNode,
		kitex.If(showModal(), func() kitex.Node {
			return kitex.Dialog(kitex.DialogProps{
				ZIndex: 100,
				Ref:    modalRef,
				OnKeyDown: func(e event.Event) {
					ke, ok := e.(*event.KeyEvent)
					if !ok {
						return
					}
					if ke.Code == key.KeyEscape || ke.Text == "q" {
						e.PreventDefault()
						e.StopPropagation()
						setShowModal(false)
					}
				},
			},
				components.Paper(components.PaperProps{
					Color:   components.PaperBase,
					Variant: components.PaperOutlined,
					Style:   modalStyle,
				},
					kitex.Box(kitex.BoxProps{
						Style: style.S().
							Display(style.DisplayFlex).
							FlexDirection(style.FlexRow).
							JustifyContent(style.JustifyBetween).
							AlignItems(style.AlignCenter).
							PaddingBottom(1).
							BorderBottom(true, style.SingleBorder()),
					},
						kitex.Span(kitex.SpanProps{Style: style.S().Bold(true)}, kitex.Text(fmt.Sprintf("Changes in %s", filename))),
						kitex.Box(kitex.BoxProps{
							Style: style.S().
								Display(style.DisplayFlex).
								FlexDirection(style.FlexRow).
								Gap(1),
						},
							components.Button(components.ButtonProps{
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
							components.Button(components.ButtonProps{
								Variant: components.ButtonText,
								Color:   components.ButtonBase,
								OnClick: func() {
									setShowModal(false)
								},
							}, kitex.Text("Close [Esc/q]")),
						),
					),
					kitex.Box(kitex.BoxProps{
						Style: style.S().
							Flex(1, 1, style.Cells(0)).
							MinHeight(style.Cells(0)).
							OverflowY(style.OverflowAuto).
							MarginTop(1),
					},
						components.DiffBlock(components.DiffBlockProps{
							Diff:  diffContent,
							Lang:  detectLang(filename),
							Split: split(),
						}),
					),
				),
			)
		}),
	)
})

// MultiEditToolWidget renders the result of a multi_edit tool call inline.
var MultiEditToolWidget = kitex.FC("MultiEditToolWidget", func(props ToolExecutionProps) kitex.Node {
	t := theme.UseTheme()
	showModal, setShowModal := kitex.UseState(false)
	split, setSplit := kitex.UseState(false)
	modalRef := kitex.CreateRef[dom.Element]()

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
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Success)}, icon.Checkmark)
			themeColor = t.Color.Surface.Success
		}
	}

	boxStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexRow).
		AlignItems(style.AlignCenter).
		AlignSelf(style.AlignStart).
		Padding(0, 1).
		Gap(1).
		Height(style.Cells(1)).
		MarginVertical(1)

	if t != nil {
		boxStyle = boxStyle.
			Background(t.Color.Surface.BaseHover).
			Foreground(themeColor)
	}

	kitex.UseEffect(func() {
		if showModal() {
			kitex.PostMacro(func() {
				if modalRef.Current != nil {
					if doc := modalRef.Current.OwnerDocument(); doc != nil {
						doc.Focus(modalRef.Current)
					}
				}
			})
		}
	}, []any{showModal()})

	var badgeNode kitex.Node
	if tm != nil && !tm.IsError && diffContent != "" {
		badgeNode = components.Button(components.ButtonProps{
			Variant: components.ButtonText,
			Color:   components.ButtonBase,
			Style:   boxStyle,
			OnClick: func() {
				setShowModal(true)
			},
		},
			iconNode,
			kitex.Span(kitex.SpanProps{Style: style.S().Bold(true)}, kitex.Text(statusLabel)),
		)
	} else {
		badgeNode = kitex.Box(kitex.BoxProps{Style: boxStyle},
			iconNode,
			kitex.Span(kitex.SpanProps{Style: style.S().Bold(true)}, kitex.Text(statusLabel)),
		)
	}

	modalStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexColumn).
		Width(style.Percent(80)).
		Height(style.Percent(80)).
		Padding(1).
		Overflow(style.OverflowHidden)

	return kitex.Fragment(
		badgeNode,
		kitex.If(showModal(), func() kitex.Node {
			return kitex.Dialog(kitex.DialogProps{
				ZIndex: 100,
				Ref:    modalRef,
				OnKeyDown: func(e event.Event) {
					ke, ok := e.(*event.KeyEvent)
					if !ok {
						return
					}
					if ke.Code == key.KeyEscape || ke.Text == "q" {
						e.PreventDefault()
						e.StopPropagation()
						setShowModal(false)
					}
				},
			},
				components.Paper(components.PaperProps{
					Color:   components.PaperBase,
					Variant: components.PaperOutlined,
					Style:   modalStyle,
				},
					kitex.Box(kitex.BoxProps{
						Style: style.S().
							Display(style.DisplayFlex).
							FlexDirection(style.FlexRow).
							JustifyContent(style.JustifyBetween).
							AlignItems(style.AlignCenter).
							PaddingBottom(1).
							BorderBottom(true, style.SingleBorder()),
					},
						kitex.Span(kitex.SpanProps{Style: style.S().Bold(true)}, kitex.Text(fmt.Sprintf("Multi-Edit Changes in %s", filename))),
						kitex.Box(kitex.BoxProps{
							Style: style.S().
								Display(style.DisplayFlex).
								FlexDirection(style.FlexRow).
								Gap(1),
						},
							components.Button(components.ButtonProps{
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
							components.Button(components.ButtonProps{
								Variant: components.ButtonText,
								Color:   components.ButtonBase,
								OnClick: func() {
									setShowModal(false)
								},
							}, kitex.Text("Close [Esc/q]")),
						),
					),
					kitex.Box(kitex.BoxProps{
						Style: style.S().
							Flex(1, 1, style.Cells(0)).
							MinHeight(style.Cells(0)).
							OverflowY(style.OverflowAuto).
							MarginTop(1),
					},
						components.DiffBlock(components.DiffBlockProps{
							Diff:  diffContent,
							Lang:  detectLang(filename),
							Split: split(),
						}),
					),
				),
			)
		}),
	)
})

// RemoveToolWidget renders the result of a remove tool call inline.
var RemoveToolWidget = kitex.FC("RemoveToolWidget", func(props ToolExecutionProps) kitex.Node {
	t := theme.UseTheme()
	showModal, setShowModal := kitex.UseState(false)
	modalRef := kitex.CreateRef[dom.Element]()

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
			statusLabel = fmt.Sprintf("Pending Remove [%s]", filename)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, kitex.Text(props.CurrentDots))
			themeColor = t.Color.Surface.Info
		} else if tm.IsError {
			statusLabel = fmt.Sprintf("Error Removing [%s]", filename)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Error)}, icon.Error)
			themeColor = t.Color.Text.Error
		} else {
			rOut, ok := parseRemoveStructuredOutput(tm.StructuredContent)
			if ok && rOut.Success {
				statusLabel = fmt.Sprintf("Removed [%s]", filename)
			} else {
				statusLabel = fmt.Sprintf("Failed to Remove [%s]", filename)
			}
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Success)}, icon.Checkmark)
			themeColor = t.Color.Surface.Success
		}
	}

	boxStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexRow).
		AlignItems(style.AlignCenter).
		AlignSelf(style.AlignStart).
		Padding(0, 1).
		Gap(1).
		Height(style.Cells(1)).
		MarginVertical(1)

	if t != nil {
		boxStyle = boxStyle.
			Background(t.Color.Surface.BaseHover).
			Foreground(themeColor)
	}

	kitex.UseEffect(func() {
		if showModal() {
			kitex.PostMacro(func() {
				if modalRef.Current != nil {
					if doc := modalRef.Current.OwnerDocument(); doc != nil {
						doc.Focus(modalRef.Current)
					}
				}
			})
		}
	}, []any{showModal()})

	var badgeNode kitex.Node
	if tm != nil && !tm.IsError {
		badgeNode = components.Button(components.ButtonProps{
			Variant: components.ButtonText,
			Color:   components.ButtonBase,
			Style:   boxStyle,
			OnClick: func() {
				setShowModal(true)
			},
		},
			iconNode,
			kitex.Span(kitex.SpanProps{Style: style.S().Bold(true)}, kitex.Text(statusLabel)),
		)
	} else {
		badgeNode = kitex.Box(kitex.BoxProps{Style: boxStyle},
			iconNode,
			kitex.Span(kitex.SpanProps{Style: style.S().Bold(true)}, kitex.Text(statusLabel)),
		)
	}

	modalStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexColumn).
		Width(style.Percent(80)).
		Height(style.Percent(80)).
		Padding(1).
		Overflow(style.OverflowHidden)

	return kitex.Fragment(
		badgeNode,
		kitex.If(showModal(), func() kitex.Node {
			rOut, ok := parseRemoveStructuredOutput(tm.StructuredContent)
			return kitex.Dialog(kitex.DialogProps{
				ZIndex: 100,
				Ref:    modalRef,
				OnKeyDown: func(e event.Event) {
					ke, ok := e.(*event.KeyEvent)
					if !ok {
						return
					}
					if ke.Code == key.KeyEscape || ke.Text == "q" {
						e.PreventDefault()
						e.StopPropagation()
						setShowModal(false)
					}
				},
			},
				components.Paper(components.PaperProps{
					Color:   components.PaperBase,
					Variant: components.PaperOutlined,
					Style:   modalStyle,
				},
					kitex.Box(kitex.BoxProps{
						Style: style.S().
							Display(style.DisplayFlex).
							FlexDirection(style.FlexRow).
							JustifyContent(style.JustifyBetween).
							AlignItems(style.AlignCenter).
							PaddingBottom(1).
							BorderBottom(true, style.SingleBorder()),
					},
						kitex.Span(kitex.SpanProps{Style: style.S().Bold(true)}, kitex.Text(fmt.Sprintf("Remove Result for %s", filename))),
						components.Button(components.ButtonProps{
							Variant: components.ButtonText,
							Color:   components.ButtonBase,
							OnClick: func() {
								setShowModal(false)
							},
						}, kitex.Text("Close [Esc/q]")),
					),
					kitex.Box(kitex.BoxProps{
						Style: style.S().
							Flex(1, 1, style.Cells(0)).
							MinHeight(style.Cells(0)).
							OverflowY(style.OverflowAuto).
							MarginTop(1),
					},
						kitex.Box(kitex.BoxProps{
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
						),
					),
				),
			)
		}),
	)
})

// BashToolWidget renders bash execution status, command input, and formatted stdout/stderr output.
var BashToolWidget = kitex.FC("BashToolWidget", func(props ToolExecutionProps) kitex.Node {
	t := theme.UseTheme()
	isOpen, setIsOpen := kitex.UseState(true)
	showModal, setShowModal := kitex.UseState(false)
	modalRef := kitex.CreateRef[dom.Element]()

	kitex.UseEffect(func() {
		if showModal() {
			kitex.PostMacro(func() {
				if modalRef.Current != nil {
					if doc := modalRef.Current.OwnerDocument(); doc != nil {
						doc.Focus(modalRef.Current)
					}
				}
			})
		}
	}, []any{showModal()})

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
		MarginVertical(1).
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
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Success)}, icon.Checkmark)
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
								MarginBottom(1).
								Foreground(textCol).
								Italic(true),
						}, kitex.Text(description))
					}),
					// Input: codeblock without header or borders
					kitex.If(command != "", func() kitex.Node {
						return kitex.Box(kitex.BoxProps{
							Style: style.S().MarginBottom(1),
						},
							components.CodeBlock(components.CodeBlockProps{
								Code:       command,
								Lang:       "bash",
								HideHeader: true,
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
									Background(t.Color.Surface.BaseHover).
									Padding(1).
									Width(style.Percent(100)).
									MaxWidth(style.Percent(100)).
									Overflow(style.OverflowHidden)

								// First part is stdout
								stdoutText := strings.TrimSpace(parts[0])
								if stdoutText != "" {
									stdoutText = strings.ReplaceAll(stdoutText, "\t", "    ")
									elements = append(elements, kitex.Box(kitex.BoxProps{
										Style: style.S().
											Foreground(textCol).
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
									}, kitex.Text("🔍 VIEW FULL OUTPUT"))
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
				Overflow(style.OverflowHidden)

			// First part is stdout
			stdoutText := strings.TrimSpace(parts[0])
			if stdoutText != "" {
				stdoutText = strings.ReplaceAll(stdoutText, "\t", "    ")
				elements = append(elements, kitex.Box(kitex.BoxProps{
					Style: style.S().
						Foreground(textCol).
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
							WhiteSpace(style.WhiteSpacePreWrap).
							MarginTop(1),
					}, kitex.Text("[stderr]\n"+stderrText)))
				}
			}

			modalStyle := style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexColumn).
				Width(style.Percent(90)).
				Height(style.Percent(80)).
				Padding(1).
				Overflow(style.OverflowHidden)

			return kitex.Dialog(kitex.DialogProps{
				ZIndex: 100,
				Ref:    modalRef,
				OnKeyDown: func(e event.Event) {
					ke, ok := e.(*event.KeyEvent)
					if !ok {
						return
					}
					if ke.Code == key.KeyEscape || ke.Text == "q" {
						e.PreventDefault()
						e.StopPropagation()
						setShowModal(false)
					}
				},
			},
				components.Paper(components.PaperProps{
					Color:   components.PaperBase,
					Variant: components.PaperOutlined,
					Style:   modalStyle,
				},
					kitex.Box(kitex.BoxProps{
						Style: style.S().
							Display(style.DisplayFlex).
							FlexDirection(style.FlexRow).
							JustifyContent(style.JustifyBetween).
							AlignItems(style.AlignCenter).
							PaddingBottom(1).
							BorderBottom(true, style.SingleBorder()),
					},
						kitex.Span(kitex.SpanProps{Style: style.S().Bold(true)}, kitex.Text("Command Output")),
						components.Button(components.ButtonProps{
							Variant: components.ButtonText,
							Color:   components.ButtonBase,
							OnClick: func() {
								setShowModal(false)
							},
						}, kitex.Text("Close [Esc/q]")),
					),
					kitex.Box(kitex.BoxProps{
						Style: style.S().
							Flex(1, 1, style.Cells(0)).
							MinHeight(style.Cells(0)).
							OverflowY(style.OverflowAuto).
							MarginTop(1),
					},
						kitex.Box(kitex.BoxProps{Style: outputContainerStyle}, elements...),
					),
				),
			)
		}),
	)
})

// TasksToolWidget renders background task queries action and result output.
// TasksToolWidget renders background task queries action and result output.
var TasksToolWidget = kitex.FC("TasksToolWidget", func(props ToolExecutionProps) kitex.Node {
	t := theme.UseTheme()
	isOpen, setIsOpen := kitex.UseState(true)
	showModal, setShowModal := kitex.UseState(false)
	modalRef := kitex.CreateRef[dom.Element]()

	kitex.UseEffect(func() {
		if showModal() {
			kitex.PostMacro(func() {
				if modalRef.Current != nil {
					if doc := modalRef.Current.OwnerDocument(); doc != nil {
						doc.Focus(modalRef.Current)
					}
				}
			})
		}
	}, []any{showModal()})

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
		MarginVertical(1).
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
										}, kitex.Text("🔍 VIEW FULL OUTPUT"))
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
		kitex.If(showModal(), func() kitex.Node {
			modalStyle := style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexColumn).
				Width(style.Percent(90)).
				Height(style.Percent(80)).
				Padding(1).
				Overflow(style.OverflowHidden)

			var modalLogElements []kitex.Node

			if strings.TrimSpace(out.StdoutTail) != "" {
				modalLogElements = append(modalLogElements,
					kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Secondary).Bold(true).MarginBottom(1)}, kitex.Text("FULL STDOUT:")),
					kitex.Box(kitex.BoxProps{
						Style: style.S().
							Foreground(t.Color.Text.Primary).
							Background(t.Color.Surface.BaseHover).
							Border(true, style.SingleBorder(), t.Color.Border.Primary).
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
							Background(t.Color.Surface.BaseHover).
							Border(true, style.SingleBorder(), t.Color.Text.Error).
							Padding(1).
							MarginBottom(1).
							WhiteSpace(style.WhiteSpacePreWrap),
					}, kitex.Text(strings.ReplaceAll(out.StderrTail, "\t", "    "))),
				)
			}

			return kitex.Dialog(kitex.DialogProps{
				ZIndex: 100,
				Ref:    modalRef,
				OnKeyDown: func(e event.Event) {
					ke, ok := e.(*event.KeyEvent)
					if !ok {
						return
					}
					if ke.Code == key.KeyEscape || ke.Text == "q" {
						e.PreventDefault()
						e.StopPropagation()
						setShowModal(false)
					}
				},
			},
				components.Paper(components.PaperProps{
					Color:   components.PaperBase,
					Variant: components.PaperOutlined,
					Style:   modalStyle,
				},
					kitex.Box(kitex.BoxProps{
						Style: style.S().
							Display(style.DisplayFlex).
							FlexDirection(style.FlexRow).
							JustifyContent(style.JustifyBetween).
							AlignItems(style.AlignCenter).
							PaddingBottom(1).
							BorderBottom(true, style.SingleBorder()),
					},
						kitex.Span(kitex.SpanProps{Style: style.S().Bold(true)}, kitex.Text(fmt.Sprintf("Task Logs: %s", targetTaskId))),
						components.Button(components.ButtonProps{
							Variant: components.ButtonText,
							Color:   components.ButtonBase,
							OnClick: func() {
								setShowModal(false)
							},
						}, kitex.Text("Close [Esc/q]")),
					),
					kitex.Box(kitex.BoxProps{
						Style: style.S().
							Flex(1, 1, style.Cells(0)).
							MinHeight(style.Cells(0)).
							OverflowY(style.OverflowAuto).
							MarginTop(1),
					},
						modalLogElements...,
					),
				),
			)
		}),
	)
})

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
	var iconNode kitex.Node
	var borderCol color.Color

	var results []tools.WebSearchOutputResultsItem

	if t != nil {
		if tm == nil {
			statusLabel = fmt.Sprintf("Web Search: Searching for %q", query)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, kitex.Text(props.CurrentDots))
			borderCol = t.Color.Surface.Info
		} else if tm.IsError {
			statusLabel = fmt.Sprintf("Web Search: Error searching for %q", query)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Error)}, icon.Error)
			borderCol = t.Color.Text.Error
		} else {
			results = parseWebSearchOutput(tm.StructuredContent)
			resWord := "results"
			if len(results) == 1 {
				resWord = "result"
			}
			statusLabel = fmt.Sprintf("Web Search: Found %d %s for %q", len(results), resWord, query)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Success)}, icon.Checkmark)
			borderCol = t.Color.Surface.Success
		}
	}

	accordionStyle := style.S().MarginVertical(1)
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
			kitex.If(tm != nil && !tm.IsError && len(results) > 0, func() kitex.Node {
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
					Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn),
				}, listNodes...)
			}),

			kitex.If(tm != nil && !tm.IsError && len(results) == 0, func() kitex.Node {
				var textCol color.Color
				if t != nil {
					textCol = t.Color.Text.Tertiary
				}
				return kitex.Box(kitex.BoxProps{
					Style: style.S().Foreground(textCol).Italic(true),
				}, kitex.Text("(no results found)"))
			}),
		),
	)
})
