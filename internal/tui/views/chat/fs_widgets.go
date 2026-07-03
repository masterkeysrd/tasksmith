package chat

import (
	"fmt"
	"image/color"
	"os"
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

// RemoveToolWidget renders the result of a remove tool call inline.
var RemoveToolWidget = kitex.FC("RemoveToolWidget", func(props ToolExecutionProps) kitex.Node {
	t := theme.UseTheme()

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
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, toolPulse())
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
	if tm != nil && props.OnViewPreview != nil {
		rOut, ok := parseRemoveStructuredOutput(tm.StructuredContent)
		var previewText string
		if tm.IsError {
			previewText = details
		} else if ok {
			if rOut.IsBinary {
				previewText = fmt.Sprintf("Removed Binary File: %s\nMimeType: %s (Text preview is not available)", path, rOut.MimeType)
			} else {
				previewText = fmt.Sprintf("Removed File: %s\n\nDeleted File Content:\n%s", path, rOut.Content)
			}
		} else {
			previewText = fmt.Sprintf("Removed File: %s", path)
		}
		onClick = func() {
			title := fmt.Sprintf("Removed %s", filename)
			if tm.IsError {
				title = fmt.Sprintf("Error Removing %s", filename)
			}
			props.OnViewPreview(title, preview.DefaultTextPreview{Text: previewText})
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

var ViewToolWidget = kitex.FC("ViewToolWidget", func(props ToolExecutionProps) kitex.Node {
	t := theme.UseTheme()

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
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, toolPulse())
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
	if tm != nil && props.OnViewPreview != nil {
		onClick = func() {
			if tm.IsError {
				details := getToolOutput(tm.Content)
				props.OnViewPreview(
					fmt.Sprintf("Error Viewing %s", filename),
					preview.DefaultTextPreview{Text: details},
				)
			} else {
				var cleanCode string
				var startLine int
				var isBinary bool
				var mimeType string

				vOut, ok := parseViewStructuredOutput(tm.StructuredContent)
				if ok {
					cleanCode = stripLinePrefixes(vOut.Content)
					startLine = vOut.StartLine
					isBinary = vOut.IsBinary
					mimeType = vOut.MimeType
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
					} else {
						cleanCode = outText
					}
				}

				props.OnViewPreview(
					fmt.Sprintf("Viewing %s", filename),
					preview.FileViewPreview{
						Path:      path,
						Content:   cleanCode,
						IsBinary:  isBinary,
						MimeType:  mimeType,
						StartLine: startLine,
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

// LsToolWidget renders the result of an ls tool call inline — no modal.
// Results beyond lsPreviewLines are hidden behind an expand toggle.
var LsToolWidget = kitex.FC("LsToolWidget", func(props ToolExecutionProps) kitex.Node {
	t := theme.UseTheme()

	tc := props.ToolCall
	tm := props.ToolMessage

	var dirPath, lsDepth string
	if tc.Args != nil {
		dirPath, _ = tc.Args["path"].(string)
		if d, ok := tc.Args["depth"].(float64); ok {
			lsDepth = fmt.Sprintf("%d", int(d))
		}
	}
	dirName := filepath.Base(dirPath)
	if dirName == "" {
		dirName = dirPath
	}

	var statusLabel string
	var labelNode kitex.Node
	var iconNode kitex.Node
	var borderCol color.Color

	var lsFiles []tools.FileEntry
	var totalCount int
	var isDetailed bool

	if t != nil {
		var actionText, suffixText string
		baseFocusBg := t.Color.Surface.BaseFocus
		folderIconColor := t.Color.Surface.Info

		if tm == nil {
			actionText = "Listing "
			statusLabel = fmt.Sprintf("Listing [%s]", dirName)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, toolPulse())
			borderCol = t.Color.Surface.Info
		} else if tm.IsError {
			actionText = "Error listing "
			statusLabel = fmt.Sprintf("Error Listing [%s]", dirName)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Error)}, icon.Error)
			borderCol = t.Color.Text.Error
		} else {
			lsFiles, totalCount, _, isDetailed = parseLsOutput(tm.StructuredContent)
			entryWord := "entries"
			if totalCount == 1 {
				entryWord = "entry"
			}
			actionText = "Listed "
			suffixText = fmt.Sprintf(" — %d %s", totalCount, entryWord)
			if lsDepth != "" {
				suffixText = fmt.Sprintf("%s (depth: %s)", suffixText, lsDepth)
			}
			if isDetailed {
				suffixText = fmt.Sprintf("%s [detailed]", suffixText)
			}
			statusLabel = fmt.Sprintf("Listed [%s]%s", dirName, suffixText)
			if totalCount > 0 {
				iconNode = nil
				borderCol = color.RGBA{R: 255, G: 255, B: 255, A: 255}
			} else {
				iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Secondary)}, icon.Info)
				borderCol = t.Color.Text.Secondary
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
					Background(baseFocusBg).
					PaddingHorizontal(1).
					Gap(1).
					MarginRight(1),
			},
				kitex.Span(kitex.SpanProps{Style: style.S().Foreground(folderIconColor)}, icon.Folder),
				kitex.Span(kitex.SpanProps{
					Style: style.S().
						Foreground(color.RGBA{255, 255, 255, 255}).
						Bold(true),
				}, kitex.Text(dirName)),
			),
			kitex.If(suffixText != "", func() kitex.Node {
				return kitex.Span(kitex.SpanProps{
					Style: style.S().
						Bold(true).
						Foreground(color.RGBA{255, 255, 255, 255}),
				}, kitex.Text(suffixText))
			}),
		)
	}

	var onClick func()
	if tm != nil && !tm.IsError && props.OnViewPreview != nil {
		var previewEntries []preview.LsEntry
		for _, entry := range lsFiles {
			previewEntries = append(previewEntries, preview.LsEntry{
				Name:        entry.Name,
				IsDir:       entry.IsDir,
				SizeBytes:   entry.Size,
				IsSymlink:   entry.IsSymlink,
				Depth:       entry.Depth,
				LinkTarget:  entry.LinkTarget,
				Permissions: entry.Permissions,
				Links:       entry.Links,
				Owner:       entry.Owner,
				Group:       entry.Group,
				Modified:    entry.Modified,
			})
		}
		onClick = func() {
			props.OnViewPreview(
				fmt.Sprintf("Listed %d entries in %s", totalCount, dirName),
				preview.LsPreview{
					Detailed: isDetailed,
					Entries:  previewEntries,
				},
			)
		}
	}

	return components.ToolBadge(components.ToolBadgeProps{
		Icon:      iconNode,
		Label:     statusLabel,
		LabelNode: labelNode,
		Color:     borderCol,
		OnClick:   onClick,
	})
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
	var labelNode kitex.Node
	var iconNode kitex.Node
	var themeColor color.Color

	var matches []string
	var totalCount int

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
				actionText = "Glob searching in "
			} else {
				actionText = "Glob searching for "
			}
			statusLabel = fmt.Sprintf("Glob: Searching%s for [%s]", scope, pattern)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, toolPulse())
			themeColor = t.Color.Surface.Info
		} else if tm.IsError {
			if path != "" {
				actionText = "Glob error searching in "
			} else {
				actionText = "Glob error searching for "
			}
			statusLabel = fmt.Sprintf("Glob: Error searching%s for [%s]", scope, pattern)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Error)}, icon.Error)
			themeColor = t.Color.Text.Error
		} else {
			matches, totalCount, _ = parseGlobOutput(tm.StructuredContent)
			matchWord := "matches"
			if totalCount == 1 {
				matchWord = "match"
			}
			if totalCount > 0 {
				if path != "" {
					actionText = fmt.Sprintf("Glob found %d %s in ", totalCount, matchWord)
				} else {
					actionText = fmt.Sprintf("Glob found %d %s for ", totalCount, matchWord)
				}
				statusLabel = fmt.Sprintf("Glob: Found %d %s%s for [%s]", totalCount, matchWord, scope, pattern)
				iconNode = nil // remove checkmark completely on success
				themeColor = color.RGBA{R: 255, G: 255, B: 255, A: 255}
			} else {
				if path != "" {
					actionText = "Glob no matches in "
				} else {
					actionText = "Glob no matches for "
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
	if tm != nil && !tm.IsError && totalCount > 0 && props.OnViewPreview != nil {
		onClick = func() {
			props.OnViewPreview(
				fmt.Sprintf("Found %d matches for %s", totalCount, pattern),
				preview.FileListPreview{Files: matches},
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
	var labelNode kitex.Node
	var iconNode kitex.Node
	var themeColor color.Color

	var matches []tools.GrepOutputMatchesItem
	var totalCount int

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
				actionText = "Grep searching in "
			} else {
				actionText = "Grep searching for "
			}
			statusLabel = fmt.Sprintf("Grep: Searching%s for [%s]", scope, pattern)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, toolPulse())
			themeColor = t.Color.Surface.Info
		} else if tm.IsError {
			if path != "" {
				actionText = "Grep error searching in "
			} else {
				actionText = "Grep error searching for "
			}
			statusLabel = fmt.Sprintf("Grep: Error searching%s for [%s]", scope, pattern)
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Error)}, icon.Error)
			themeColor = t.Color.Text.Error
		} else {
			matches, totalCount, _ = parseGrepOutput(tm.StructuredContent)
			matchWord := "matches"
			if totalCount == 1 {
				matchWord = "match"
			}
			if totalCount > 0 {
				if path != "" {
					actionText = fmt.Sprintf("Grep found %d %s in ", totalCount, matchWord)
				} else {
					actionText = fmt.Sprintf("Grep found %d %s for ", totalCount, matchWord)
				}
				statusLabel = fmt.Sprintf("Grep: Found %d %s%s for [%s]", totalCount, matchWord, scope, pattern)
				iconNode = nil // remove checkmark completely on success
				themeColor = color.RGBA{R: 255, G: 255, B: 255, A: 255}
			} else {
				if path != "" {
					actionText = "Grep no matches in "
				} else {
					actionText = "Grep no matches for "
				}
				statusLabel = fmt.Sprintf("Grep: No matches found%s for [%s]", scope, pattern)
				iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Text.Secondary)}, icon.Info)
				themeColor = t.Color.Text.Secondary
			}
		}

		baseFocusBg := t.Color.Surface.BaseFocus
		searchIconColor := t.Color.Surface.Info

		labelNode = kitex.Box(kitex.BoxProps{
			Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).AlignItems(style.AlignCenter),
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
	if tm != nil && !tm.IsError && totalCount > 0 && props.OnViewPreview != nil {
		var previewMatches []preview.GrepMatch
		for _, m := range matches {
			previewMatches = append(previewMatches, preview.GrepMatch{
				Path:       m.Path,
				LineNumber: m.Line,
				Content:    m.Content,
			})
		}
		onClick = func() {
			props.OnViewPreview(
				fmt.Sprintf("Found %d matches for %s", totalCount, pattern),
				preview.GrepMatchesPreview{Matches: previewMatches},
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

// grepEntryRow renders a single grep match line with its file path header and matching line content using components.CodeBlock.
func grepEntryRow(t *theme.Scheme, match tools.GrepOutputMatchesItem) kitex.Node {
	var nameColor color.Color
	var dirColor color.Color
	var lineNumColor color.Color
	if t != nil {
		nameColor = t.Color.Text.Primary
		dirColor = t.Color.Text.Secondary
		lineNumColor = t.Color.Text.Tertiary
	}

	dirPart, filePart := filepath.Split(match.Path)
	if len(filePart) > tools.MaxFilenameChars {
		filePart = filePart[:tools.MaxFilenameChars] + "…"
	}

	ext := filepath.Ext(match.Path)
	lang := strings.TrimPrefix(ext, ".")

	return kitex.Box(kitex.BoxProps{
		Style: style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexColumn).
			PaddingVertical(0).
			PaddingHorizontal(1),
	},
		kitex.Box(kitex.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow).
				AlignItems(style.AlignCenter).
				MarginBottom(0),
		},
			kitex.Span(kitex.SpanProps{Style: style.S().MarginRight(1)}, icon.FileIcon(icon.FileIconProps{Path: match.Path})),
			kitex.If(dirPart != "", func() kitex.Node {
				return kitex.Span(kitex.SpanProps{Style: style.S().Foreground(dirColor)}, kitex.Text(dirPart))
			}),
			kitex.Span(kitex.SpanProps{Style: style.S().Foreground(nameColor).Bold(true)}, kitex.Text(filePart)),
			kitex.Span(kitex.SpanProps{Style: style.S().Foreground(lineNumColor).MarginLeft(1)}, kitex.Text(fmt.Sprintf(":%d", match.Line))),
		),
		components.CodeBlock(components.CodeBlockProps{
			Code:            match.Content,
			Lang:            lang,
			HideHeader:      true,
			ShowLineNumbers: true,
			StartLine:       match.Line,
			Compact:         true,
			GutterMinWidth:  6,
			Style:           style.S().Margin(0).Padding(0),
		}),
	)
}

// WriteToolWidget renders the result of a write tool call inline.
var WriteToolWidget = kitex.FC("WriteToolWidget", func(props ToolExecutionProps) kitex.Node {
	t := theme.UseTheme()

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
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, toolPulse())
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
	if tm != nil && props.OnViewPreview != nil {
		onClick = func() {
			if tm.IsError {
				details := getToolOutput(tm.Content)
				props.OnViewPreview(
					fmt.Sprintf("Error Writing %s", filename),
					preview.DefaultTextPreview{Text: details},
				)
			} else {
				props.OnViewPreview(
					fmt.Sprintf("Viewing %s", filename),
					preview.FileViewPreview{
						Path:      path,
						Content:   content,
						StartLine: 1,
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

// EditToolWidget renders the result of an edit tool call inline.
var EditToolWidget = kitex.FC("EditToolWidget", func(props ToolExecutionProps) kitex.Node {
	t := theme.UseTheme()

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
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, toolPulse())
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
				actionText = "Edited "
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
	if tm != nil && props.OnViewPreview != nil {
		onClick = func() {
			if tm.IsError {
				details := getToolOutput(tm.Content)
				props.OnViewPreview(
					fmt.Sprintf("Error Editing %s", filename),
					preview.DefaultTextPreview{Text: details},
				)
			} else {
				props.OnViewPreview(
					fmt.Sprintf("Changes in %s", filename),
					preview.FileEditPreview{
						Path: path,
						Diff: diffContent,
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

// MultiEditToolWidget renders the result of a multi_edit tool call inline.
var MultiEditToolWidget = kitex.FC("MultiEditToolWidget", func(props ToolExecutionProps) kitex.Node {
	t := theme.UseTheme()

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
			iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(t.Color.Surface.Info)}, toolPulse())
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
				actionText = "Multi-Edited "
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
	if tm != nil && props.OnViewPreview != nil {
		onClick = func() {
			if tm.IsError {
				details := getToolOutput(tm.Content)
				props.OnViewPreview(
					fmt.Sprintf("Error Multi-Editing %s", filename),
					preview.DefaultTextPreview{Text: details},
				)
			} else {
				props.OnViewPreview(
					fmt.Sprintf("Changes in %s", filename),
					preview.FileEditPreview{
						Path: path,
						Diff: diffContent,
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

func detectLang(filename string) string {
	ext := filepath.Ext(filename)
	if ext == "" {
		return "txt"
	}
	return strings.ToLower(ext[1:])
}

func parseRangeFromHeader(text string) (startLine, endLine int) {
	before, _, ok := strings.Cut(text, "\n")
	if !ok {
		return
	}
	firstLine := before
	openParen := strings.Index(firstLine, " (")
	if openParen == -1 {
		return
	}
	dash := strings.Index(firstLine[openParen:], "-")
	if dash == -1 {
		return
	}
	dash = openParen + dash
	ofWord := strings.Index(firstLine[dash:], " of ")
	if ofWord == -1 {
		return
	}
	ofWord = dash + ofWord

	startStr := strings.TrimSpace(firstLine[openParen+2 : dash])
	endStr := strings.TrimSpace(firstLine[dash+1 : ofWord])

	_, _ = fmt.Sscan(startStr, &startLine)
	_, _ = fmt.Sscan(endStr, &endLine)
	return
}

// lsEntryRow renders a single FileEntry as a table row (kitex.TR).
// Each metadata field occupies its own TD so the table layout engine
// distributes column widths automatically — no manual Sprintf padding needed.
func lsEntryRow(t *theme.Scheme, fe tools.FileEntry, detailed bool) kitex.Node {
	var metaColor color.Color
	var nameColor color.Color

	if t != nil {
		metaColor = t.Color.Text.Tertiary
		switch {
		case fe.IsDir:
			nameColor = t.Color.Surface.Info
		case fe.IsSymlink:
			nameColor = t.Color.Surface.Tertiary
		default:
			nameColor = t.Color.Text.Primary
		}
	}

	displayName := fe.Name
	if fe.NameTruncated && len(fe.Name) > tools.MaxFilenameChars {
		displayName = fe.Name[:tools.MaxFilenameChars] + "…"
	}

	// metaCell shrinks to its content width and adds a right padding gap.
	metaCell := func(text string, s style.Style) kitex.Node {
		tdStyle := s.Width(style.MaxContent).PaddingRight(2).WhiteSpace(style.WhiteSpaceNoWrap)
		return kitex.TD(kitex.TDProps{Style: tdStyle},
			kitex.Span(kitex.SpanProps{Style: s}, kitex.Text(text)),
		)
	}

	metaStyle := style.S().Foreground(metaColor)

	nameStyle := style.S().Foreground(nameColor)
	if fe.IsDir {
		nameStyle = nameStyle.Bold(true)
	}

	nameText := displayName
	if fe.IsSymlink && fe.LinkTarget != "" {
		nameText += " → " + fe.LinkTarget
	}

	// Name cell takes all remaining width.
	nameTDStyle := nameStyle.Width(style.Percent(100))

	var iconNode kitex.Node
	if fe.IsDir {
		iconNode = kitex.Span(kitex.SpanProps{Style: style.S().Foreground(nameColor)}, icon.Folder)
	} else {
		iconNode = icon.FileIcon(icon.FileIconProps{Path: fe.Name})
	}

	nameBox := kitex.Box(kitex.BoxProps{
		Style: style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexRow).
			AlignItems(style.AlignCenter).
			Gap(1).
			PaddingLeft(fe.Depth * 2),
	},
		iconNode,
		kitex.Span(kitex.SpanProps{Style: nameStyle}, kitex.Text(nameText)),
	)

	// When detailed is false, render as a compact tree-style list (icon + name only).
	if !detailed {
		return kitex.Box(kitex.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexColumn).
				Width(style.Percent(100)).
				MinWidth(style.Percent(0)),
		},
			kitex.Box(kitex.BoxProps{
				Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexRow).AlignItems(style.AlignCenter),
			},
				nameBox,
			),
		)
	}

	// Default: flat table with all columns.
	return kitex.TR(kitex.TRProps{Style: style.S().Gap(1)},
		metaCell(fe.Permissions, metaStyle),
		metaCell(fmt.Sprintf("%d", fe.Links), metaStyle),
		metaCell(fe.Owner, metaStyle),
		metaCell(fe.Group, metaStyle),
		metaCell(tools.FormatSize(fe.Size), metaStyle),
		metaCell(fe.Modified.Format("Jan _2 15:04"), metaStyle),
		kitex.TD(kitex.TDProps{Style: nameTDStyle}, nameBox),
	)
}
