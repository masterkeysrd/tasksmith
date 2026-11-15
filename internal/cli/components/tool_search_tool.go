package components

import (
	"fmt"
	"strings"

	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

type ToolSearchToolWidgetProps struct {
	Tool store.ToolPart
}

// ToolSearchToolWidget reports how many deferred tools a tool_search call
// loaded, so a productive search is distinguishable from an empty one. The
// matched names ride along because a timeline row is immutable once it reaches
// scrollback, so there is no later view in which to expand them.
var ToolSearchToolWidget = trail.FC("ToolSearchToolWidget", func(props ToolSearchToolWidgetProps) trail.Node {
	scheme := theme.UseTheme()
	tool := props.Tool
	input := parseToolInput[api.ToolSearchInput](tool.Call)
	output := parseToolOutput[api.ToolSearchOutput](tool.Response)

	query := strings.TrimSpace(input.Query)
	completed := tool.Response != nil
	failed := tool.Status == store.ToolFailed || (tool.Response != nil && tool.Response.IsError)

	status := tool.Status
	if failed {
		status = store.ToolFailed
	}

	matched := len(output.Tools)

	verb := "Searching tools"
	switch {
	case failed:
		verb = "Failed to search tools"
	case completed:
		verb = "Searched tools"
	}

	// The query stays on the row so a re-read of the transcript says what was
	// looked for; the outcome is the suffix.
	target := ""
	if query != "" {
		target = `"` + query + `"`
	}

	headerNodes := toolHeaderLeading(status, verb, target != "", scheme)
	if target != "" {
		headerNodes = append(headerNodes, trail.Span(trail.SpanProps{
			Style: style.S().
				Flex(0, 1).
				MinWidth(style.Cells(0)).
				WhiteSpace(style.WhiteSpaceNoWrap).
				Overflow(style.OverflowHidden).
				TextOverflow(style.TextOverflowEllipsis).
				Foreground(scheme.Color.Text.Primary),
		}, trail.Text(target)))
	}

	var suffix string
	switch {
	case failed:
	case completed && matched == 0:
		suffix = " (no matches)"
	case completed:
		toolWord := fmt.Sprintf("%d tools", matched)
		if matched == 1 {
			toolWord = "1 tool"
		}
		suffix = " (" + toolWord + ": " + strings.Join(output.Tools, ", ") + ")"
	}
	if suffixNode := renderSuffixSpan(suffix, scheme); suffixNode != nil {
		headerNodes = append(headerNodes, suffixNode)
	}

	if stateSuffix := toolStateSuffix(tool.Status, scheme); stateSuffix != nil && !failed {
		headerNodes = append(headerNodes, stateSuffix)
	}

	return wrapToolWidget(headerNodes, toolErrorSubline(tool, scheme))
})

func ToolSearchToolRenderer(tool store.ToolPart) trail.Node {
	return ToolSearchToolWidget(ToolSearchToolWidgetProps{Tool: tool})
}
