package chat

import (
	"fmt"
	"image/color"
	"path/filepath"
	"strings"
	"time"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/internal/agent/tools"
	"github.com/masterkeysrd/tasksmith/internal/core/preview"
	"github.com/masterkeysrd/tasksmith/internal/tui/components"
	"github.com/masterkeysrd/tasksmith/internal/tui/components/icon"
	"github.com/masterkeysrd/tasksmith/internal/tui/theme"
)

// ToolPreviewRenderer defines the function signature for specific preview rendering.
type ToolPreviewRenderer func(t *theme.Scheme, payload map[string]any, previewData preview.ToolPreview) kitex.Node

// PreviewRegistry maps visual preview types to their respective renderer implementations.
var PreviewRegistry = map[string]ToolPreviewRenderer{
	"file_edit":       renderFileEditPreview,
	"shell_command":   renderBashPreview,
	"file_list":       renderFileListPreview,
	"grep_matches":    renderGrepMatchesPreview,
	"ls_entries":      renderLsPreview,
	"lsp_symbols":     renderLspSymbolsPreview,
	"lsp_diagnostics": renderLspDiagnosticsPreview,
	"lsp_inspect":     renderLspInspectPreview,
	"file_view":       renderFileViewPreview,
	"markdown":        renderMarkdownPreview,
	"symbol_view":     renderSymbolViewPreview,
}

// PreviewPanelProps defines properties for the generic PreviewPanel component.
type PreviewPanelProps struct {
	Preview   any // Can be preview.ToolPreview, string, or map[string]any from JSON deserialization
	Payload   map[string]any
	MaxHeight int
	Border    bool
}

// PreviewPanel renders a unified content viewer based on the registered ToolPreview type.
var PreviewPanel = kitex.FCC("PreviewPanel", func(props PreviewPanelProps) kitex.Node {
	if props.Preview == nil {
		return nil
	}

	t := theme.UseTheme()

	var previewType string
	var previewData preview.ToolPreview

	if tp, ok := props.Preview.(preview.ToolPreview); ok {
		// Defensive pointer dereferencing to ensure renderers always get value types
		switch val := tp.(type) {
		case *preview.FileEditPreview:
			previewType = val.Type()
			previewData = *val
		case *preview.BashPreview:
			previewType = val.Type()
			previewData = *val
		case *preview.FileListPreview:
			previewType = val.Type()
			previewData = *val
		case *preview.GrepMatchesPreview:
			previewType = val.Type()
			previewData = *val
		case *preview.LsPreview:
			previewType = val.Type()
			previewData = *val
		case *preview.DefaultTextPreview:
			previewType = val.Type()
			previewData = *val
		case *preview.FileViewPreview:
			previewType = val.Type()
			previewData = *val
		case *preview.MarkdownPreview:
			previewType = val.Type()
			previewData = *val
		case *preview.SymbolViewPreview:
			previewType = val.Type()
			previewData = *val
		default:
			previewType = val.Type()
			previewData = val
		}
	} else if m, ok := props.Preview.(map[string]any); ok {
		if tStr, ok := m["type"].(string); ok {
			previewType = tStr
			previewData = reconstructPreviewFromMap(previewType, m)
		}
	} else if s, ok := props.Preview.(string); ok {
		previewType = "default_text"
		previewData = preview.DefaultTextPreview{Text: s}
	}

	if previewType == "" || previewData == nil {
		return nil
	}

	renderer, exists := PreviewRegistry[previewType]
	if !exists {
		renderer = renderDefaultPreview
	}

	contentNode := renderer(t, props.Payload, previewData)

	containerStyle := style.S().
		Display(style.DisplayFlex).
		FlexDirection(style.FlexColumn).
		Width(style.Percent(100)).
		Overflow(style.OverflowAuto)

	if props.MaxHeight > 0 {
		containerStyle = containerStyle.MaxHeight(style.Cells(props.MaxHeight))
	}
	if props.Border {
		containerStyle = containerStyle.
			Border(true, style.SingleBorder(), t.Color.Border.Primary).
			Padding(1)
	}

	return kitex.Box(kitex.BoxProps{Style: containerStyle}, contentNode)
})

func reconstructPreviewFromMap(previewType string, m map[string]any) preview.ToolPreview {
	switch previewType {
	case "file_edit":
		path, _ := m["path"].(string)
		diffText, _ := m["diff"].(string)
		return preview.FileEditPreview{Path: path, Diff: diffText}
	case "shell_command":
		cmd, _ := m["command"].(string)
		return preview.BashPreview{Command: cmd}
	case "file_list":
		var files []string
		if rawFiles, ok := m["files"].([]any); ok {
			for _, rf := range rawFiles {
				if fStr, ok := rf.(string); ok {
					files = append(files, fStr)
				}
			}
		}
		return preview.FileListPreview{Files: files}
	case "grep_matches":
		var grepMatches []preview.GrepMatch
		if rawMatches, ok := m["matches"].([]any); ok {
			for _, rm := range rawMatches {
				if rMap, ok := rm.(map[string]any); ok {
					path, _ := rMap["path"].(string)
					lineVal, _ := rMap["line_number"].(float64)
					content, _ := rMap["content"].(string)
					grepMatches = append(grepMatches, preview.GrepMatch{
						Path:       path,
						LineNumber: int(lineVal),
						Content:    content,
					})
				}
			}
		}
		return preview.GrepMatchesPreview{Matches: grepMatches}
	case "ls_entries":
		var lsEntries []preview.LsEntry
		detailed, _ := m["detailed"].(bool)
		if rawEntries, ok := m["entries"].([]any); ok {
			for _, re := range rawEntries {
				if rMap, ok := re.(map[string]any); ok {
					name, _ := rMap["name"].(string)
					isDir, _ := rMap["is_dir"].(bool)
					sizeVal, _ := rMap["size_bytes"].(float64)
					isSymlink, _ := rMap["is_symlink"].(bool)
					depthVal, _ := rMap["depth"].(float64)
					linkTarget, _ := rMap["link_target"].(string)
					permissions, _ := rMap["permissions"].(string)
					linksVal, _ := rMap["links"].(float64)
					owner, _ := rMap["owner"].(string)
					group, _ := rMap["group"].(string)
					var modified time.Time
					if modStr, ok := rMap["modified"].(string); ok {
						if parsed, err := time.Parse(time.RFC3339, modStr); err == nil {
							modified = parsed
						}
					}
					lsEntries = append(lsEntries, preview.LsEntry{
						Name:        name,
						IsDir:       isDir,
						SizeBytes:   int64(sizeVal),
						IsSymlink:   isSymlink,
						Depth:       int(depthVal),
						LinkTarget:  linkTarget,
						Permissions: permissions,
						Links:       uint64(linksVal),
						Owner:       owner,
						Group:       group,
						Modified:    modified,
					})
				}
			}
		}
		return preview.LsPreview{Detailed: detailed, Entries: lsEntries}
	case "default_text":
		text, _ := m["text"].(string)
		return preview.DefaultTextPreview{Text: text}
	case "file_view":
		path, _ := m["path"].(string)
		content, _ := m["content"].(string)
		isBinary, _ := m["is_binary"].(bool)
		mimeType, _ := m["mime_type"].(string)
		startLineVal, _ := m["start_line"].(float64)
		return preview.FileViewPreview{
			Path:      path,
			Content:   content,
			IsBinary:  isBinary,
			MimeType:  mimeType,
			StartLine: int(startLineVal),
		}
	case "markdown":
		md, _ := m["markdown"].(string)
		return preview.MarkdownPreview{Markdown: md}
	case "symbol_view":
		name, _ := m["name"].(string)
		kind, _ := m["kind"].(string)
		file, _ := m["file"].(string)
		snippet, _ := m["snippet"].(string)
		docs, _ := m["docs"].(string)
		diags, _ := m["diagnostics"].(string)

		var refs []string
		if rVal, ok := m["references"].([]any); ok {
			for _, r := range rVal {
				if s, ok := r.(string); ok {
					refs = append(refs, s)
				}
			}
		}
		var impls []string
		if iVal, ok := m["implementations"].([]any); ok {
			for _, i := range iVal {
				if s, ok := i.(string); ok {
					impls = append(impls, s)
				}
			}
		}
		return preview.SymbolViewPreview{
			Name:            name,
			Kind:            kind,
			File:            file,
			Snippet:         snippet,
			Docs:            docs,
			Diagnostics:     diags,
			References:      refs,
			Implementations: impls,
		}
	default:
		return nil
	}
}

func renderFileEditPreview(t *theme.Scheme, payload map[string]any, previewData preview.ToolPreview) kitex.Node {
	editPrev, ok := previewData.(preview.FileEditPreview)
	if !ok {
		return renderDefaultPreview(t, payload, previewData)
	}
	return components.DiffBlock(components.DiffBlockProps{
		Diff:  editPrev.Diff,
		Split: false,
	})
}

func renderBashPreview(t *theme.Scheme, payload map[string]any, previewData preview.ToolPreview) kitex.Node {
	bashPrev, ok := previewData.(preview.BashPreview)
	if !ok {
		return renderDefaultPreview(t, payload, previewData)
	}
	return components.CodeBlock(components.CodeBlockProps{
		Code:            bashPrev.Command,
		Lang:            "bash",
		HideHeader:      true,
		ShowLineNumbers: false,
	})
}

func renderFileListPreview(t *theme.Scheme, payload map[string]any, previewData preview.ToolPreview) kitex.Node {
	listPrev, ok := previewData.(preview.FileListPreview)
	if !ok {
		return renderDefaultPreview(t, payload, previewData)
	}
	var rows []kitex.Node
	for _, f := range listPrev.Files {
		rows = append(rows, globEntryRow(t, f))
	}
	return kitex.Box(kitex.BoxProps{Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Gap(0)}, rows...)
}

func renderGrepMatchesPreview(t *theme.Scheme, payload map[string]any, previewData preview.ToolPreview) kitex.Node {
	grepPrev, ok := previewData.(preview.GrepMatchesPreview)
	if !ok {
		return renderDefaultPreview(t, payload, previewData)
	}
	var rows []kitex.Node
	for _, m := range grepPrev.Matches {
		rows = append(rows, grepEntryRow(t, tools.GrepOutputMatchesItem{
			Path:    m.Path,
			Line:    m.LineNumber,
			Content: m.Content,
		}))
	}
	return kitex.Box(kitex.BoxProps{Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Gap(0)}, rows...)
}

func renderLsPreview(t *theme.Scheme, payload map[string]any, previewData preview.ToolPreview) kitex.Node {
	lsPrev, ok := previewData.(preview.LsPreview)
	if !ok {
		return renderDefaultPreview(t, payload, previewData)
	}
	var rows []kitex.Node
	for _, entry := range lsPrev.Entries {
		rows = append(rows, lsEntryRow(t, tools.FileEntry{
			Name:        entry.Name,
			IsDir:       entry.IsDir,
			Size:        entry.SizeBytes,
			IsSymlink:   entry.IsSymlink,
			Depth:       entry.Depth,
			LinkTarget:  entry.LinkTarget,
			Permissions: entry.Permissions,
			Links:       entry.Links,
			Owner:       entry.Owner,
			Group:       entry.Group,
			Modified:    entry.Modified,
		}, lsPrev.Detailed))
	}
	if lsPrev.Detailed {
		return kitex.Table(kitex.TableProps{Style: style.S().MinWidth(style.Percent(0))},
			kitex.TBody(kitex.TBodyProps{},
				rows...,
			),
		)
	}
	return kitex.Box(kitex.BoxProps{Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Gap(0)}, rows...)
}

func renderDefaultPreview(t *theme.Scheme, payload map[string]any, previewData preview.ToolPreview) kitex.Node {
	var code string
	if textPrev, ok := previewData.(preview.DefaultTextPreview); ok {
		code = textPrev.Text
	} else if stringer, ok := previewData.(interface{ String() string }); ok {
		code = stringer.String()
	}

	return components.CodeBlock(components.CodeBlockProps{
		Code:            code,
		HideHeader:      true,
		ShowLineNumbers: false,
	})
}

// GenericPreviewModalProps defines properties for GenericPreviewModal.
type GenericPreviewModalProps struct {
	IsOpen  bool
	Title   string
	Preview preview.ToolPreview
	OnClose func()
}

// GenericPreviewModal renders a generic full-screen overlay for viewing any ToolPreview.
var GenericPreviewModal = kitex.FCC("GenericPreviewModal", func(props GenericPreviewModalProps) kitex.Node {
	if !props.IsOpen || props.Preview == nil {
		return nil
	}

	return components.Modal(components.ModalProps{
		IsOpen:  props.IsOpen,
		Title:   kitex.Text(props.Title),
		OnClose: props.OnClose,
	},
		PreviewPanel(PreviewPanelProps{
			Preview: props.Preview,
			Border:  false,
		}),
	)
})

// LspSymbolsPreview represents LSP symbols search results.
type LspSymbolsPreview struct {
	Query   string
	Results []tools.LspSymbolsOutputResultsItem
}

func (LspSymbolsPreview) Type() string { return "lsp_symbols" }

// LspDiagnosticsPreview represents LSP diagnostics results.
type LspDiagnosticsPreview struct {
	Path        string
	Diagnostics []tools.LspDiagnosticsOutputDiagnosticsItem
}

func (LspDiagnosticsPreview) Type() string { return "lsp_diagnostics" }

// LspInspectPreview represents LSP inspect output.
type LspInspectPreview struct {
	Query  string
	Output tools.LspInspectOutput
}

func (LspInspectPreview) Type() string { return "lsp_inspect" }

func renderLspSymbolsPreview(t *theme.Scheme, payload map[string]any, previewData preview.ToolPreview) kitex.Node {
	lspPrev, ok := previewData.(LspSymbolsPreview)
	if !ok {
		return renderDefaultPreview(t, payload, previewData)
	}
	var rows []kitex.Node
	for _, item := range lspPrev.Results {
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
			Padding(1).
			Width(style.Percent(100)).
			MinWidth(style.Percent(0)),
	}, rows...)
}

func renderLspDiagnosticsPreview(t *theme.Scheme, payload map[string]any, previewData preview.ToolPreview) kitex.Node {
	lspPrev, ok := previewData.(LspDiagnosticsPreview)
	if !ok {
		return renderDefaultPreview(t, payload, previewData)
	}
	var rows []kitex.Node
	for _, d := range lspPrev.Diagnostics {
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
	return kitex.Box(kitex.BoxProps{
		Style: style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexColumn).
			Padding(1).
			Width(style.Percent(100)).
			MinWidth(style.Percent(0)),
	}, rows...)
}

func renderLspInspectPreview(t *theme.Scheme, payload map[string]any, previewData preview.ToolPreview) kitex.Node {
	lspPrev, ok := previewData.(LspInspectPreview)
	if !ok {
		return renderDefaultPreview(t, payload, previewData)
	}
	fullReportPath := lspPrev.Output.Result.FullReportPath
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
		components.Markdown(components.MarkdownProps{
			Source: lspPrev.Output.TextContent(),
		}),
	)
}

func renderFileViewPreview(t *theme.Scheme, payload map[string]any, previewData preview.ToolPreview) kitex.Node {
	viewPrev, ok := previewData.(preview.FileViewPreview)
	if !ok {
		return renderDefaultPreview(t, payload, previewData)
	}
	filename := filepath.Base(viewPrev.Path)
	if viewPrev.IsBinary {
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
				kitex.Span(kitex.SpanProps{Style: style.S().Foreground(textSecondary)}, kitex.Text(fmt.Sprintf("  • MIME Type: %s", viewPrev.MimeType))),
				kitex.Span(kitex.SpanProps{Style: style.S().Foreground(textSecondary)}, kitex.Text(fmt.Sprintf("  • Path:      %s", viewPrev.Path))),
			),
			components.Button(components.ButtonProps{
				Variant: components.ButtonSolid,
				Color:   components.ButtonPrimary,
				Style: style.S().
					AlignSelf(style.AlignStart).
					MarginTop(1).
					Padding(0, 2),
				OnClick: func() {
					openWithSystemViewer(viewPrev.Path)
				},
			}, kitex.Text("Open with System Viewer")),
		)
	}
	showLines := viewPrev.StartLine > 0
	return components.CodeBlock(components.CodeBlockProps{
		Code:            viewPrev.Content,
		Lang:            detectLang(filename),
		HideHeader:      true,
		ShowLineNumbers: showLines,
		StartLine:       viewPrev.StartLine,
	})
}

func renderMarkdownPreview(t *theme.Scheme, payload map[string]any, previewData preview.ToolPreview) kitex.Node {
	mdPrev, ok := previewData.(preview.MarkdownPreview)
	if !ok {
		return renderDefaultPreview(t, payload, previewData)
	}
	return components.Markdown(components.MarkdownProps{
		Source: mdPrev.Markdown,
	})
}

func renderSymbolViewPreview(t *theme.Scheme, payload map[string]any, previewData preview.ToolPreview) kitex.Node {
	symPrev, ok := previewData.(preview.SymbolViewPreview)
	if !ok {
		return renderDefaultPreview(t, payload, previewData)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "## %s (%s)\n\n", symPrev.Name, symPrev.Kind)
	fmt.Fprintf(&sb, "**Declared at:** `%s`\n\n", symPrev.File)

	if symPrev.Snippet != "" {
		sb.WriteString("**Definition:**\n```go\n")
		sb.WriteString(symPrev.Snippet)
		sb.WriteString("\n```\n\n")
	}

	if symPrev.Docs != "" {
		sb.WriteString(symPrev.Docs)
		sb.WriteString("\n\n")
	}

	if len(symPrev.References) > 0 {
		fmt.Fprintf(&sb, "**References** (%d total):\n", len(symPrev.References))
		for _, ref := range symPrev.References {
			fmt.Fprintf(&sb, "- `%s`\n", ref)
		}
		sb.WriteString("\n")
	}

	if len(symPrev.Implementations) > 0 {
		fmt.Fprintf(&sb, "**Implementations** (%d total):\n", len(symPrev.Implementations))
		for _, impl := range symPrev.Implementations {
			fmt.Fprintf(&sb, "- `%s`\n", impl)
		}
		sb.WriteString("\n")
	}

	return kitex.Box(kitex.BoxProps{
		Style: style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexColumn).
			Padding(1).
			Width(style.Percent(100)).
			MinWidth(style.Percent(0)),
	},
		components.Markdown(components.MarkdownProps{
			Source: sb.String(),
		}),
	)
}
