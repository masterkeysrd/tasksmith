package components

import (
	"encoding/json"
	"fmt"
	"image/color"
	"path/filepath"
	"strings"

	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/ui/components/icon"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

type ToolRenderer func(tool store.ToolPart) trail.Node

func parseToolInput[T any](call *api.ToolCall) T {
	var input T
	if call != nil && len(call.Args) > 0 {
		data, err := json.Marshal(call.Args)
		if err == nil {
			_ = json.Unmarshal(data, &input)
		}
	}
	return input
}

func parseToolOutput[T any](resp *api.Message) T {
	var output T
	if resp != nil && len(resp.StructuredContent) > 0 {
		_ = json.Unmarshal(resp.StructuredContent, &output)
	}
	return output
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	units := []string{"KB", "MB", "GB", "TB"}
	val := float64(b) / float64(div)
	if val == float64(int64(val)) {
		return fmt.Sprintf("%.0f %s", val, units[exp])
	}
	return fmt.Sprintf("%.1f %s", val, units[exp])
}

func formatDuration(ms int64) string {
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	sec := float64(ms) / 1000.0
	if sec < 60 {
		if sec == float64(int64(sec)) {
			return fmt.Sprintf("%.0fs", sec)
		}
		return fmt.Sprintf("%.1fs", sec)
	}
	min := int(sec / 60)
	remainingSec := int(sec) % 60
	return fmt.Sprintf("%dm%02ds", min, remainingSec)
}

func toolHeaderLeading(status store.ToolStatus, verb string, hasTarget bool, scheme *theme.Scheme) []trail.Node {
	dotChar := "○"
	var dotColor color.Color
	switch status {
	case store.ToolSucceeded:
		dotChar = "●"
		dotColor = scheme.Color.Surface.Success
	case store.ToolRunning, store.ToolReceiving:
		dotChar = "○"
		dotColor = scheme.Color.Surface.Info
	case store.ToolFailed:
		dotChar = "●"
		dotColor = scheme.Color.Text.Error
	case store.ToolPending, store.ToolWaitingPermission, store.ToolInterrupted:
		fallthrough
	default:
		dotChar = "○"
		dotColor = scheme.Color.Text.Secondary
	}

	verbColor := scheme.Color.Text.Primary
	if status == store.ToolFailed {
		verbColor = scheme.Color.Text.Error
	}

	verbText := verb
	if hasTarget {
		verbText += " "
	}

	dotNode := trail.Span(trail.SpanProps{
		Style: style.S().Flex(0, 0).WhiteSpace(style.WhiteSpacePre).Foreground(dotColor),
	}, trail.Text(dotChar+" "))

	verbNode := trail.Span(trail.SpanProps{
		Style: style.S().Flex(0, 0).WhiteSpace(style.WhiteSpacePre).Bold(true).Foreground(verbColor),
	}, trail.Text(verbText))

	return []trail.Node{dotNode, verbNode}
}

func toolStateSuffix(status store.ToolStatus, scheme *theme.Scheme) trail.Node {
	var suffixText string
	switch status {
	case store.ToolWaitingPermission:
		suffixText = " (waiting for permission)"
	case store.ToolInterrupted:
		suffixText = " (interrupted)"
	default:
		return nil
	}
	return trail.Span(trail.SpanProps{
		Style: style.S().
			Flex(0, 0).
			WhiteSpace(style.WhiteSpacePre).
			Foreground(scheme.Color.Text.Secondary),
	}, trail.Text(suffixText))
}

func toolErrorSubline(tool store.ToolPart, scheme *theme.Scheme) trail.Node {
	if tool.Response == nil || !tool.Response.IsError {
		return nil
	}
	firstLine := ""
	for _, b := range tool.Response.Content {
		for line := range strings.SplitSeq(b.Text, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				firstLine = line
				break
			}
		}
		if firstLine != "" {
			break
		}
	}
	if firstLine == "" {
		return nil
	}
	return trail.Span(trail.SpanProps{
		Style: style.S().
			Flex(0, 0).
			WhiteSpace(style.WhiteSpacePre).
			Overflow(style.OverflowHidden).
			TextOverflow(style.TextOverflowEllipsis).
			Foreground(scheme.Color.Text.Error),
	}, trail.Text("  ⎿  ✗ "+firstLine))
}

func renderPathNodes(rawPath string, isDir bool, scheme *theme.Scheme) []trail.Node {
	trimmed := strings.TrimSpace(rawPath)
	if trimmed == "" {
		return nil
	}
	clean := filepath.Clean(trimmed)

	iconBox := trail.Span(trail.SpanProps{
		Style: style.S().Flex(0, 0).WhiteSpace(style.WhiteSpacePre),
	}, icon.FileIcon(icon.FileIconProps{Path: clean, IsDir: isDir}), trail.Text(" "))

	nodes := []trail.Node{iconBox}

	dir := filepath.Dir(clean)
	if dir != "." && dir != "/" {
		dirSpan := trail.Span(trail.SpanProps{
			Style: style.S().
				Flex(0, 1).
				MinWidth(style.Cells(0)).
				Overflow(style.OverflowHidden).
				TextOverflow(style.TextOverflowEllipsis).
				WhiteSpace(style.WhiteSpacePre).
				Foreground(scheme.Color.Text.Primary),
		}, trail.Text(dir))

		baseSpan := trail.Span(trail.SpanProps{
			Style: style.S().
				Flex(0, 0).
				WhiteSpace(style.WhiteSpacePre).
				Foreground(scheme.Color.Text.Primary),
		}, trail.Text("/"+filepath.Base(clean)))

		nodes = append(nodes, dirSpan, baseSpan)
	} else {
		fileSpan := trail.Span(trail.SpanProps{
			Style: style.S().
				Flex(0, 0).
				WhiteSpace(style.WhiteSpacePre).
				Foreground(scheme.Color.Text.Primary),
		}, trail.Text(clean))

		nodes = append(nodes, fileSpan)
	}

	return nodes
}

func renderSuffixSpan(text string, scheme *theme.Scheme) trail.Node {
	if text == "" {
		return nil
	}
	return trail.Span(trail.SpanProps{
		Style: style.S().
			Flex(0, 0).
			WhiteSpace(style.WhiteSpacePre).
			Foreground(scheme.Color.Text.Secondary),
	}, trail.Text(text))
}

func renderDiffStatNodes(additions int, deletions int, replacements int, scheme *theme.Scheme) []trail.Node {
	hasLines := additions > 0 || deletions > 0
	hasRepl := replacements > 1

	if !hasLines && !hasRepl {
		return nil
	}

	secStyle := style.S().Flex(0, 0).WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Text.Secondary)
	addStyle := style.S().Flex(0, 0).WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Surface.Success)
	delStyle := style.S().Flex(0, 0).WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Text.Error)

	var nodes []trail.Node
	nodes = append(nodes, trail.Span(trail.SpanProps{Style: secStyle}, trail.Text(" (")))

	if hasLines {
		if additions > 0 && deletions > 0 {
			nodes = append(nodes,
				trail.Span(trail.SpanProps{Style: addStyle}, trail.Text(fmt.Sprintf("+%d", additions))),
				trail.Span(trail.SpanProps{Style: secStyle}, trail.Text(" ")),
				trail.Span(trail.SpanProps{Style: delStyle}, trail.Text(fmt.Sprintf("-%d", deletions))),
			)
		} else if additions > 0 {
			nodes = append(nodes,
				trail.Span(trail.SpanProps{Style: addStyle}, trail.Text(fmt.Sprintf("+%d", additions))),
			)
			if replacements > 0 {
				nodes = append(nodes,
					trail.Span(trail.SpanProps{Style: secStyle}, trail.Text(" ")),
					trail.Span(trail.SpanProps{Style: delStyle}, trail.Text("-0")),
				)
			}
		} else if deletions > 0 {
			if replacements > 0 {
				nodes = append(nodes,
					trail.Span(trail.SpanProps{Style: addStyle}, trail.Text("+0")),
					trail.Span(trail.SpanProps{Style: secStyle}, trail.Text(" ")),
				)
			}
			nodes = append(nodes,
				trail.Span(trail.SpanProps{Style: delStyle}, trail.Text(fmt.Sprintf("-%d", deletions))),
			)
		}
		if hasRepl {
			nodes = append(nodes, trail.Span(trail.SpanProps{Style: secStyle}, trail.Text(fmt.Sprintf(", %d replacements", replacements))))
		}
	} else if hasRepl {
		nodes = append(nodes, trail.Span(trail.SpanProps{Style: secStyle}, trail.Text(fmt.Sprintf("%d replacements", replacements))))
	}

	nodes = append(nodes, trail.Span(trail.SpanProps{Style: secStyle}, trail.Text(")")))
	return nodes
}

func wrapToolWidget(headerNodes []trail.Node, errorSubline trail.Node) trail.Node {
	headerRow := trail.Box(trail.BoxProps{
		Style: style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexRow).
			Width(style.Percent(100)).
			MaxWidth(style.Percent(100)).
			MinWidth(style.Cells(0)).
			Overflow(style.OverflowHidden),
	}, headerNodes...)

	if errorSubline != nil {
		return trail.Box(trail.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexColumn).
				Width(style.Percent(100)).
				MinWidth(style.Cells(0)),
		}, headerRow, errorSubline)
	}

	return headerRow
}

type ViewToolWidgetProps struct {
	Tool store.ToolPart
}

var ViewToolWidget = trail.FC("ViewToolWidget", func(props ViewToolWidgetProps) trail.Node {
	scheme := theme.UseTheme()
	tool := props.Tool
	input := parseToolInput[api.ViewInput](tool.Call)
	output := parseToolOutput[api.ViewOutput](tool.Response)

	path := input.Path
	if output.Path != "" {
		path = output.Path
	}

	failed := tool.Status == store.ToolFailed || (tool.Response != nil && tool.Response.IsError)
	verb := "Reading"
	if tool.Status == store.ToolSucceeded || failed {
		verb = "Read"
	}

	status := tool.Status
	if failed {
		status = store.ToolFailed
	}

	hasTarget := path != ""
	leading := toolHeaderLeading(status, verb, hasTarget, scheme)
	headerNodes := append([]trail.Node{}, leading...)

	if hasTarget {
		headerNodes = append(headerNodes, renderPathNodes(path, false, scheme)...)
	}

	var suffix string
	if output.StartLine > 0 && output.EndLine > 0 && output.TotalLines > 0 && (output.StartLine > 1 || output.EndLine < output.TotalLines) {
		suffix = fmt.Sprintf(" (lines %d-%d of %d)", output.StartLine, output.EndLine, output.TotalLines)
	} else if input.Offset > 0 && input.Limit > 0 {
		suffix = fmt.Sprintf(" (lines %d-%d)", input.Offset, input.Offset+input.Limit-1)
	} else if input.Offset > 0 {
		suffix = fmt.Sprintf(" (from line %d)", input.Offset)
	} else if input.Limit > 0 {
		suffix = fmt.Sprintf(" (first %d lines)", input.Limit)
	} else if output.TotalLines > 0 {
		suffix = fmt.Sprintf(" (%d lines)", output.TotalLines)
	}

	if suffixNode := renderSuffixSpan(suffix, scheme); suffixNode != nil {
		headerNodes = append(headerNodes, suffixNode)
	}

	if stateSuffix := toolStateSuffix(tool.Status, scheme); stateSuffix != nil {
		headerNodes = append(headerNodes, stateSuffix)
	}

	return wrapToolWidget(headerNodes, toolErrorSubline(tool, scheme))
})

func ViewToolRenderer(tool store.ToolPart) trail.Node {
	return ViewToolWidget(ViewToolWidgetProps{Tool: tool})
}

type LsToolWidgetProps struct {
	Tool store.ToolPart
}

var LsToolWidget = trail.FC("LsToolWidget", func(props LsToolWidgetProps) trail.Node {
	scheme := theme.UseTheme()
	tool := props.Tool
	input := parseToolInput[api.LsInput](tool.Call)
	output := parseToolOutput[api.LsOutput](tool.Response)

	path := input.Path
	if output.Path != "" {
		path = output.Path
	}
	if strings.TrimSpace(path) == "" {
		path = "."
	}

	failed := tool.Status == store.ToolFailed || (tool.Response != nil && tool.Response.IsError)
	verb := "Listing"
	if tool.Status == store.ToolSucceeded || failed {
		verb = "Listed"
	}

	status := tool.Status
	if failed {
		status = store.ToolFailed
	}

	leading := toolHeaderLeading(status, verb, true, scheme)
	headerNodes := append([]trail.Node{}, leading...)
	headerNodes = append(headerNodes, renderPathNodes(path, true, scheme)...)

	var suffix string
	if tool.Response != nil && !tool.Response.IsError {
		n := len(output.Entries)
		entryStr := fmt.Sprintf("%d entries", n)
		if n == 1 {
			entryStr = "1 entry"
		}
		parts := []string{entryStr}
		if input.Recursive {
			parts = append(parts, "recursive")
		}
		if output.Truncated {
			parts = append(parts, "truncated")
		}
		suffix = " (" + strings.Join(parts, ", ") + ")"
	} else if input.Recursive {
		suffix = " (recursive)"
	}

	if suffixNode := renderSuffixSpan(suffix, scheme); suffixNode != nil {
		headerNodes = append(headerNodes, suffixNode)
	}

	if stateSuffix := toolStateSuffix(tool.Status, scheme); stateSuffix != nil {
		headerNodes = append(headerNodes, stateSuffix)
	}

	return wrapToolWidget(headerNodes, toolErrorSubline(tool, scheme))
})

func LsToolRenderer(tool store.ToolPart) trail.Node {
	return LsToolWidget(LsToolWidgetProps{Tool: tool})
}

func LsRenderer(tool store.ToolPart) trail.Node {
	return LsToolRenderer(tool)
}

type GlobToolWidgetProps struct {
	Tool store.ToolPart
}

var GlobToolWidget = trail.FC("GlobToolWidget", func(props GlobToolWidgetProps) trail.Node {
	scheme := theme.UseTheme()
	tool := props.Tool
	input := parseToolInput[api.GlobInput](tool.Call)
	output := parseToolOutput[api.GlobOutput](tool.Response)

	failed := tool.Status == store.ToolFailed || (tool.Response != nil && tool.Response.IsError)
	verb := "Matching"
	if tool.Status == store.ToolSucceeded || failed {
		verb = "Matched"
	}

	status := tool.Status
	if failed {
		status = store.ToolFailed
	}

	hasTarget := input.Pattern != "" || (input.Path != "" && input.Path != ".")
	leading := toolHeaderLeading(status, verb, hasTarget, scheme)
	headerNodes := append([]trail.Node{}, leading...)

	if input.Pattern != "" {
		patternSpan := trail.Span(trail.SpanProps{
			Style: style.S().
				Flex(0, 0).
				WhiteSpace(style.WhiteSpacePre).
				Foreground(scheme.Color.Text.Primary),
		}, trail.Text(input.Pattern))
		headerNodes = append(headerNodes, patternSpan)
	}

	if input.Path != "" && input.Path != "." {
		inSpan := trail.Span(trail.SpanProps{
			Style: style.S().
				Flex(0, 0).
				WhiteSpace(style.WhiteSpacePre).
				Foreground(scheme.Color.Text.Secondary),
		}, trail.Text(" in "))
		headerNodes = append(headerNodes, inSpan)
		headerNodes = append(headerNodes, renderPathNodes(input.Path, true, scheme)...)
	}

	var suffix string
	if tool.Response != nil && !tool.Response.IsError {
		n := len(output.Matches)
		fileStr := fmt.Sprintf("%d files", n)
		if n == 1 {
			fileStr = "1 file"
		}
		if output.Truncated {
			fileStr += ", truncated"
		}
		suffix = " (" + fileStr + ")"
	}

	if suffixNode := renderSuffixSpan(suffix, scheme); suffixNode != nil {
		headerNodes = append(headerNodes, suffixNode)
	}

	if stateSuffix := toolStateSuffix(tool.Status, scheme); stateSuffix != nil {
		headerNodes = append(headerNodes, stateSuffix)
	}

	return wrapToolWidget(headerNodes, toolErrorSubline(tool, scheme))
})

func GlobToolRenderer(tool store.ToolPart) trail.Node {
	return GlobToolWidget(GlobToolWidgetProps{Tool: tool})
}

type GrepToolWidgetProps struct {
	Tool store.ToolPart
}

var GrepToolWidget = trail.FC("GrepToolWidget", func(props GrepToolWidgetProps) trail.Node {
	scheme := theme.UseTheme()
	tool := props.Tool
	input := parseToolInput[api.GrepInput](tool.Call)
	output := parseToolOutput[api.GrepOutput](tool.Response)

	failed := tool.Status == store.ToolFailed || (tool.Response != nil && tool.Response.IsError)
	verb := "Searching"
	if tool.Status == store.ToolSucceeded || failed {
		verb = "Searched"
	}

	status := tool.Status
	if failed {
		status = store.ToolFailed
	}

	hasTarget := input.Pattern != "" || input.Include != "" || (input.Path != "" && input.Path != ".")
	leading := toolHeaderLeading(status, verb, hasTarget, scheme)
	headerNodes := append([]trail.Node{}, leading...)

	if input.Pattern != "" {
		quotedPattern := fmt.Sprintf("%q", input.Pattern)
		patternSpan := trail.Span(trail.SpanProps{
			Style: style.S().
				Flex(0, 0).
				WhiteSpace(style.WhiteSpacePre).
				Foreground(scheme.Color.Text.Primary),
		}, trail.Text(quotedPattern))
		headerNodes = append(headerNodes, patternSpan)
	}

	if input.Include != "" {
		includeSpan := trail.Span(trail.SpanProps{
			Style: style.S().
				Flex(0, 0).
				WhiteSpace(style.WhiteSpacePre).
				Foreground(scheme.Color.Text.Secondary),
		}, trail.Text(" ("+input.Include+")"))
		headerNodes = append(headerNodes, includeSpan)
	}

	if input.Path != "" && input.Path != "." {
		inSpan := trail.Span(trail.SpanProps{
			Style: style.S().
				Flex(0, 0).
				WhiteSpace(style.WhiteSpacePre).
				Foreground(scheme.Color.Text.Secondary),
		}, trail.Text(" in "))
		headerNodes = append(headerNodes, inSpan)
		headerNodes = append(headerNodes, renderPathNodes(input.Path, true, scheme)...)
	}

	var suffix string
	if tool.Response != nil && !tool.Response.IsError {
		n := len(output.Matches)
		matchStr := fmt.Sprintf("%d matches", n)
		if n == 1 {
			matchStr = "1 match"
		}
		if output.Truncated {
			matchStr += ", truncated"
		}
		suffix = " (" + matchStr + ")"
	}

	if suffixNode := renderSuffixSpan(suffix, scheme); suffixNode != nil {
		headerNodes = append(headerNodes, suffixNode)
	}

	if stateSuffix := toolStateSuffix(tool.Status, scheme); stateSuffix != nil {
		headerNodes = append(headerNodes, stateSuffix)
	}

	return wrapToolWidget(headerNodes, toolErrorSubline(tool, scheme))
})

func GrepToolRenderer(tool store.ToolPart) trail.Node {
	return GrepToolWidget(GrepToolWidgetProps{Tool: tool})
}

func toolPrimaryArg(call *api.ToolCall) string {
	if call == nil || call.Args == nil {
		return ""
	}
	keys := []string{"path", "pattern", "command", "url", "query", "name"}
	for _, key := range keys {
		if v, ok := call.Args[key].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(strings.ReplaceAll(v, "\n", " "))
		}
	}
	return ""
}

type ToolDefaultWidgetProps struct {
	Tool store.ToolPart
}

var ToolDefaultWidget = trail.FC("ToolDefaultWidget", func(props ToolDefaultWidgetProps) trail.Node {
	scheme := theme.UseTheme()
	tool := props.Tool
	name := tool.ID
	if tool.Call != nil {
		name = tool.Call.Name
	} else if tool.Response != nil && tool.Response.Name != "" {
		name = tool.Response.Name
	}
	verbs := [2]string{"Calling " + name, "Called " + name}

	failed := tool.Status == store.ToolFailed || (tool.Response != nil && tool.Response.IsError)
	verb := verbs[0]
	if tool.Status == store.ToolSucceeded || failed {
		verb = verbs[1]
	}

	status := tool.Status
	if failed {
		status = store.ToolFailed
	}

	arg := toolPrimaryArg(tool.Call)
	hasArg := arg != ""

	leading := toolHeaderLeading(status, verb, hasArg, scheme)
	headerNodes := append([]trail.Node{}, leading...)

	if hasArg {
		argSpan := trail.Span(trail.SpanProps{
			Style: style.S().
				Flex(0, 0).
				WhiteSpace(style.WhiteSpacePre).
				Foreground(scheme.Color.Text.Secondary),
		}, trail.Text("("+arg+")"))
		headerNodes = append(headerNodes, argSpan)
	}

	if stateSuffix := toolStateSuffix(tool.Status, scheme); stateSuffix != nil {
		headerNodes = append(headerNodes, stateSuffix)
	}

	return wrapToolWidget(headerNodes, toolErrorSubline(tool, scheme))
})

func DefaultToolRenderer(tool store.ToolPart) trail.Node {
	return ToolDefaultWidget(ToolDefaultWidgetProps{Tool: tool})
}

type WriteToolWidgetProps struct {
	Tool store.ToolPart
}

var WriteToolWidget = trail.FC("WriteToolWidget", func(props WriteToolWidgetProps) trail.Node {
	scheme := theme.UseTheme()
	tool := props.Tool
	input := parseToolInput[api.WriteInput](tool.Call)
	output := parseToolOutput[api.WriteOutput](tool.Response)

	path := input.Path
	if output.Path != "" {
		path = output.Path
	}

	failed := tool.Status == store.ToolFailed || (tool.Response != nil && tool.Response.IsError)
	verb := "Writing"
	if tool.Status == store.ToolSucceeded || failed {
		verb = "Wrote"
	}

	status := tool.Status
	if failed {
		status = store.ToolFailed
	}

	hasTarget := path != ""
	leading := toolHeaderLeading(status, verb, hasTarget, scheme)
	headerNodes := append([]trail.Node{}, leading...)

	if hasTarget {
		headerNodes = append(headerNodes, renderPathNodes(path, false, scheme)...)
	}

	if tool.Response != nil && !tool.Response.IsError {
		headerNodes = append(headerNodes, renderDiffStatNodes(output.Additions, output.Deletions, 0, scheme)...)
	}

	if stateSuffix := toolStateSuffix(tool.Status, scheme); stateSuffix != nil {
		headerNodes = append(headerNodes, stateSuffix)
	}

	return wrapToolWidget(headerNodes, toolErrorSubline(tool, scheme))
})

func WriteToolRenderer(tool store.ToolPart) trail.Node {
	return WriteToolWidget(WriteToolWidgetProps{Tool: tool})
}

type EditToolWidgetProps struct {
	Tool store.ToolPart
}

var EditToolWidget = trail.FC("EditToolWidget", func(props EditToolWidgetProps) trail.Node {
	scheme := theme.UseTheme()
	tool := props.Tool
	input := parseToolInput[api.EditInput](tool.Call)
	output := parseToolOutput[api.EditOutput](tool.Response)

	path := input.Path
	if output.Path != "" {
		path = output.Path
	}

	failed := tool.Status == store.ToolFailed || (tool.Response != nil && tool.Response.IsError)
	verb := "Editing"
	if tool.Status == store.ToolSucceeded || failed {
		verb = "Edited"
	}

	status := tool.Status
	if failed {
		status = store.ToolFailed
	}

	hasTarget := path != ""
	leading := toolHeaderLeading(status, verb, hasTarget, scheme)
	headerNodes := append([]trail.Node{}, leading...)

	if hasTarget {
		headerNodes = append(headerNodes, renderPathNodes(path, false, scheme)...)
	}

	if tool.Response != nil && !tool.Response.IsError {
		headerNodes = append(headerNodes, renderDiffStatNodes(output.Additions, output.Deletions, output.Replacements, scheme)...)
	}

	if stateSuffix := toolStateSuffix(tool.Status, scheme); stateSuffix != nil {
		headerNodes = append(headerNodes, stateSuffix)
	}

	return wrapToolWidget(headerNodes, toolErrorSubline(tool, scheme))
})

func EditToolRenderer(tool store.ToolPart) trail.Node {
	return EditToolWidget(EditToolWidgetProps{Tool: tool})
}

type RemoveToolWidgetProps struct {
	Tool store.ToolPart
}

var RemoveToolWidget = trail.FC("RemoveToolWidget", func(props RemoveToolWidgetProps) trail.Node {
	scheme := theme.UseTheme()
	tool := props.Tool
	input := parseToolInput[api.RemoveInput](tool.Call)
	output := parseToolOutput[api.RemoveOutput](tool.Response)

	path := input.Path
	if output.Path != "" {
		path = output.Path
	}

	failed := tool.Status == store.ToolFailed || (tool.Response != nil && tool.Response.IsError)
	verb := "Removing"
	if tool.Status == store.ToolSucceeded || failed {
		verb = "Removed"
	}

	status := tool.Status
	if failed {
		status = store.ToolFailed
	}

	hasTarget := path != ""
	leading := toolHeaderLeading(status, verb, hasTarget, scheme)
	headerNodes := append([]trail.Node{}, leading...)

	if hasTarget {
		headerNodes = append(headerNodes, renderPathNodes(path, false, scheme)...)
	}

	if tool.Response != nil && !tool.Response.IsError {
		headerNodes = append(headerNodes, renderDiffStatNodes(0, output.Deletions, 0, scheme)...)
	}

	if stateSuffix := toolStateSuffix(tool.Status, scheme); stateSuffix != nil {
		headerNodes = append(headerNodes, stateSuffix)
	}

	return wrapToolWidget(headerNodes, toolErrorSubline(tool, scheme))
})

func RemoveToolRenderer(tool store.ToolPart) trail.Node {
	return RemoveToolWidget(RemoveToolWidgetProps{Tool: tool})
}

type BashToolWidgetProps struct {
	Tool store.ToolPart
}

var BashToolWidget = trail.FC("BashToolWidget", func(props BashToolWidgetProps) trail.Node {
	scheme := theme.UseTheme()
	tool := props.Tool
	input := parseToolInput[api.BashInput](tool.Call)
	output := parseToolOutput[api.BashOutput](tool.Response)

	failed := tool.Status == store.ToolFailed || (tool.Response != nil && tool.Response.IsError) || output.ExitCode != 0 || output.Status == "failed"
	status := tool.Status
	if output.TaskID != "" && (output.Status == "running" || tool.Status == store.ToolRunning) {
		status = store.ToolRunning
	} else if failed {
		status = store.ToolFailed
	}

	verb := "Running"
	if status == store.ToolSucceeded || status == store.ToolFailed {
		verb = "Ran"
	}

	desc := strings.TrimSpace(strings.ReplaceAll(input.Description, "\n", " "))
	hasTarget := desc != ""
	leading := toolHeaderLeading(status, verb, hasTarget, scheme)
	headerNodes := append([]trail.Node{}, leading...)

	if hasTarget {
		descSpan := trail.Span(trail.SpanProps{
			Style: style.S().
				Flex(0, 1).
				MinWidth(style.Cells(0)).
				Overflow(style.OverflowHidden).
				TextOverflow(style.TextOverflowEllipsis).
				WhiteSpace(style.WhiteSpacePre).
				Foreground(scheme.Color.Text.Primary),
		}, trail.Text(desc))
		headerNodes = append(headerNodes, descSpan)
	}

	if output.TaskID != "" && (output.Status == "running" || tool.Status == store.ToolRunning) {
		if suffixNode := renderSuffixSpan(fmt.Sprintf(" (background: %s)", output.TaskID), scheme); suffixNode != nil {
			headerNodes = append(headerNodes, suffixNode)
		}
	} else if tool.Response != nil {
		durStr := ""
		if output.DurationMs > 0 {
			durStr = formatDuration(output.DurationMs)
		}
		if output.ExitCode != 0 {
			secStyle := style.S().Flex(0, 0).WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Text.Secondary)
			errStyle := style.S().Flex(0, 0).WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Text.Error)

			headerNodes = append(headerNodes,
				trail.Span(trail.SpanProps{Style: secStyle}, trail.Text(" (")),
				trail.Span(trail.SpanProps{Style: errStyle}, trail.Text(fmt.Sprintf("exit %d", output.ExitCode))),
			)
			if durStr != "" {
				headerNodes = append(headerNodes,
					trail.Span(trail.SpanProps{Style: secStyle}, trail.Text(", "+durStr)),
				)
			}
			headerNodes = append(headerNodes,
				trail.Span(trail.SpanProps{Style: secStyle}, trail.Text(")")),
			)
		} else if durStr != "" {
			if suffixNode := renderSuffixSpan(" ("+durStr+")", scheme); suffixNode != nil {
				headerNodes = append(headerNodes, suffixNode)
			}
		}
	}

	if stateSuffix := toolStateSuffix(tool.Status, scheme); stateSuffix != nil {
		headerNodes = append(headerNodes, stateSuffix)
	}

	headerRow := trail.Box(trail.BoxProps{
		Style: style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexRow).
			Width(style.Percent(100)).
			MaxWidth(style.Percent(100)).
			MinWidth(style.Cells(0)).
			Overflow(style.OverflowHidden),
	}, headerNodes...)

	cmd := strings.TrimSpace(strings.ReplaceAll(input.Command, "\n", " "))
	var cmdRow trail.Node
	if cmd != "" {
		line2Nodes := []trail.Node{
			trail.Span(trail.SpanProps{
				Style: style.S().
					Flex(0, 0).
					WhiteSpace(style.WhiteSpacePre).
					Foreground(scheme.Color.Text.Secondary),
			}, trail.Text("  ⎿  "), icon.FontAwesomeTerminal, trail.Text(" ")),
			trail.Span(trail.SpanProps{
				Style: style.S().
					Flex(0, 1).
					MinWidth(style.Cells(0)).
					Overflow(style.OverflowHidden).
					TextOverflow(style.TextOverflowEllipsis).
					WhiteSpace(style.WhiteSpacePre).
					Foreground(scheme.Color.Text.Secondary),
			}, trail.Text(cmd)),
		}
		if input.Dir != "" && input.Dir != "." {
			line2Nodes = append(line2Nodes, trail.Span(trail.SpanProps{
				Style: style.S().
					Flex(0, 0).
					WhiteSpace(style.WhiteSpacePre).
					Foreground(scheme.Color.Text.Secondary),
			}, trail.Text(" (in "+input.Dir+")")))
		}
		cmdRow = trail.Box(trail.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow).
				Width(style.Percent(100)).
				MaxWidth(style.Percent(100)).
				MinWidth(style.Cells(0)).
				Overflow(style.OverflowHidden),
		}, line2Nodes...)
	}

	rows := []trail.Node{headerRow}
	if cmdRow != nil {
		rows = append(rows, cmdRow)
	}
	if errSubline := toolErrorSubline(tool, scheme); errSubline != nil {
		rows = append(rows, errSubline)
	}
	if len(rows) == 1 {
		return headerRow
	}
	return trail.Box(trail.BoxProps{
		Style: style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexColumn).
			Width(style.Percent(100)).
			MinWidth(style.Cells(0)),
	}, rows...)
})

func BashToolRenderer(tool store.ToolPart) trail.Node {
	return BashToolWidget(BashToolWidgetProps{Tool: tool})
}

type WebFetchToolWidgetProps struct {
	Tool store.ToolPart
}

var WebFetchToolWidget = trail.FC("WebFetchToolWidget", func(props WebFetchToolWidgetProps) trail.Node {
	scheme := theme.UseTheme()
	tool := props.Tool
	input := parseToolInput[api.WebFetchInput](tool.Call)
	output := parseToolOutput[api.WebFetchOutput](tool.Response)

	rawURL := input.URL
	if output.URL != "" {
		rawURL = output.URL
	}
	rawURL = strings.TrimSpace(rawURL)

	failed := tool.Status == store.ToolFailed || (tool.Response != nil && tool.Response.IsError) || output.StatusCode >= 400
	verb := "Fetching"
	if tool.Status == store.ToolSucceeded || failed {
		verb = "Fetched"
	}

	status := tool.Status
	if failed {
		status = store.ToolFailed
	}

	hasTarget := rawURL != ""
	leading := toolHeaderLeading(status, verb, hasTarget, scheme)
	headerNodes := append([]trail.Node{}, leading...)

	if hasTarget {
		iconBox := trail.Span(trail.SpanProps{
			Style: style.S().Flex(0, 0).WhiteSpace(style.WhiteSpacePre),
		}, icon.Globe, trail.Text(" "))

		urlSpan := trail.Span(trail.SpanProps{
			Style: style.S().
				Flex(0, 1).
				MinWidth(style.Cells(0)).
				Overflow(style.OverflowHidden).
				TextOverflow(style.TextOverflowEllipsis).
				WhiteSpace(style.WhiteSpacePre).
				Foreground(scheme.Color.Text.Primary),
		}, trail.Text(rawURL))

		headerNodes = append(headerNodes, iconBox, urlSpan)
	}

	var suffix string
	if output.StatusCode > 0 {
		ct, _, _ := strings.Cut(output.ContentType, ";")
		ct = strings.TrimSpace(ct)
		if ct != "" {
			suffix = fmt.Sprintf(" (%d, %s)", output.StatusCode, ct)
		} else {
			suffix = fmt.Sprintf(" (%d)", output.StatusCode)
		}
	}

	if suffixNode := renderSuffixSpan(suffix, scheme); suffixNode != nil {
		headerNodes = append(headerNodes, suffixNode)
	}

	if stateSuffix := toolStateSuffix(tool.Status, scheme); stateSuffix != nil {
		headerNodes = append(headerNodes, stateSuffix)
	}

	return wrapToolWidget(headerNodes, toolErrorSubline(tool, scheme))
})

func WebFetchToolRenderer(tool store.ToolPart) trail.Node {
	return WebFetchToolWidget(WebFetchToolWidgetProps{Tool: tool})
}

type DownloadToolWidgetProps struct {
	Tool store.ToolPart
}

var DownloadToolWidget = trail.FC("DownloadToolWidget", func(props DownloadToolWidgetProps) trail.Node {
	scheme := theme.UseTheme()
	tool := props.Tool
	input := parseToolInput[api.DownloadInput](tool.Call)
	output := parseToolOutput[api.DownloadOutput](tool.Response)

	path := input.Path
	if output.Path != "" {
		path = output.Path
	}

	failed := tool.Status == store.ToolFailed || (tool.Response != nil && tool.Response.IsError) || output.StatusCode >= 400
	verb := "Downloading"
	if tool.Status == store.ToolSucceeded || failed {
		verb = "Downloaded"
	}

	status := tool.Status
	if failed {
		status = store.ToolFailed
	}

	hasTarget := path != ""
	leading := toolHeaderLeading(status, verb, hasTarget, scheme)
	headerNodes := append([]trail.Node{}, leading...)

	if hasTarget {
		headerNodes = append(headerNodes, renderPathNodes(path, false, scheme)...)
	}

	var suffix string
	if tool.Response != nil && !tool.Response.IsError && output.BytesWritten > 0 {
		suffix = fmt.Sprintf(" (%s)", formatBytes(int64(output.BytesWritten)))
	}

	if suffixNode := renderSuffixSpan(suffix, scheme); suffixNode != nil {
		headerNodes = append(headerNodes, suffixNode)
	}

	if stateSuffix := toolStateSuffix(tool.Status, scheme); stateSuffix != nil {
		headerNodes = append(headerNodes, stateSuffix)
	}

	return wrapToolWidget(headerNodes, toolErrorSubline(tool, scheme))
})

func DownloadToolRenderer(tool store.ToolPart) trail.Node {
	return DownloadToolWidget(DownloadToolWidgetProps{Tool: tool})
}

type TasksToolWidgetProps struct {
	Tool store.ToolPart
}

var TasksToolWidget = trail.FC("TasksToolWidget", func(props TasksToolWidgetProps) trail.Node {
	scheme := theme.UseTheme()
	tool := props.Tool
	input := parseToolInput[api.TasksInput](tool.Call)
	output := parseToolOutput[api.TasksOutput](tool.Response)

	failed := tool.Status == store.ToolFailed || (tool.Response != nil && tool.Response.IsError)
	isDone := tool.Status == store.ToolSucceeded || failed

	status := tool.Status
	if failed {
		status = store.ToolFailed
	}

	var verb string
	var target string
	var suffix string

	switch input.Action {
	case "list":
		verb = "Listing tasks"
		if isDone {
			verb = "Listed tasks"
		}
		if tool.Response != nil && !tool.Response.IsError {
			n := len(output.Tasks)
			if n == 1 {
				suffix = " (1 task)"
			} else {
				suffix = fmt.Sprintf(" (%d tasks)", n)
			}
		}
	case "status":
		verb = "Checking task"
		if isDone {
			verb = "Checked task"
		}
		target = input.TaskID
		if tool.Response != nil && !tool.Response.IsError && output.Status != "" {
			suffix = fmt.Sprintf(" (%s)", output.Status)
		}
	case "kill":
		verb = "Killing task"
		if isDone {
			verb = "Killed task"
		}
		target = input.TaskID
		if tool.Response != nil && !tool.Response.IsError && output.Status != "" {
			suffix = fmt.Sprintf(" (%s)", output.Status)
		}
	case "send_input":
		verb = "Sending input"
		if isDone {
			verb = "Sent input"
		}
		if input.TaskID != "" {
			target = "to " + input.TaskID
		}
	default:
		verb = "Managing tasks"
		if isDone {
			verb = "Managed tasks"
		}
		target = input.TaskID
	}

	hasTarget := target != ""
	leading := toolHeaderLeading(status, verb, hasTarget, scheme)
	headerNodes := append([]trail.Node{}, leading...)

	if hasTarget {
		targetSpan := trail.Span(trail.SpanProps{
			Style: style.S().
				Flex(0, 0).
				WhiteSpace(style.WhiteSpacePre).
				Foreground(scheme.Color.Text.Primary),
		}, trail.Text(target))
		headerNodes = append(headerNodes, targetSpan)
	}

	if suffixNode := renderSuffixSpan(suffix, scheme); suffixNode != nil {
		headerNodes = append(headerNodes, suffixNode)
	}

	if stateSuffix := toolStateSuffix(tool.Status, scheme); stateSuffix != nil {
		headerNodes = append(headerNodes, stateSuffix)
	}

	return wrapToolWidget(headerNodes, toolErrorSubline(tool, scheme))
})

func TasksToolRenderer(tool store.ToolPart) trail.Node {
	return TasksToolWidget(TasksToolWidgetProps{Tool: tool})
}
