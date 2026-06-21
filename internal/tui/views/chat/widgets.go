package chat

import (
	"fmt"
	"image/color"
	"path/filepath"
	"strings"

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
