package components

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/ui/components/icon"
	"github.com/masterkeysrd/tasksmith/internal/ui/hooks"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

type ToolRenderer func(tool hooks.ToolPart) trail.Node

func ViewToolRenderer(tool hooks.ToolPart) trail.Node {
	props := ViewToolWidgetProps{
		IsRunning: tool.Status == hooks.ToolRunning || tool.Status == hooks.ToolReceiving,
	}
	if tool.Call != nil && tool.Call.Args != nil {
		data, err := json.Marshal(tool.Call.Args)
		var input api.ViewInput
		if err == nil {
			err = json.Unmarshal(data, &input)
		}
		if err != nil {
			return viewToolNotice(viewToolNoticeProps{
				Text: "Unable to render read arguments: " + err.Error(), Error: true,
			})
		}
		props.Path = input.Path
		if input.Offset > 0 || input.Limit > 0 {
			from := max(input.Offset, 1)
			props.From = strconv.Itoa(from)
			if input.Limit > 0 {
				props.To = strconv.FormatUint(uint64(from)+uint64(input.Limit)-1, 10)
			}
		}
	}
	if tool.Response == nil {
		if props.IsRunning {
			return ViewToolWidget(props)
		}
		state, failed := "pending", false
		switch tool.Status {
		case hooks.ToolWaitingPermission:
			state = "waiting for permission"
		case hooks.ToolFailed:
			state, failed = "failed without a result", true
		case hooks.ToolInterrupted:
			state, failed = "interrupted", true
		}
		return viewToolNotice(viewToolNoticeProps{
			Text: strings.TrimSpace("○ Read "+viewToolDetails(props)) + " (" + state + ")", Error: failed,
		})
	}

	if tool.Response.IsError {
		return viewToolNotice(viewToolNoticeProps{
			Text: strings.TrimSpace("● Read failed "+viewToolDetails(props)) + "\n" + tool.Response.Content[0].Text, Error: true,
		})
	}
	var output api.ViewOutput
	if err := json.Unmarshal(tool.Response.StructuredContent, &output); err != nil {
		return viewToolNotice(viewToolNoticeProps{
			Text: "Unable to render read result: " + err.Error(), Error: true,
		})
	}
	props = ViewToolWidgetProps{Path: output.Path}
	if output.StartLine > 0 {
		props.From, props.To = strconv.Itoa(output.StartLine), strconv.Itoa(output.EndLine)
	}
	return ViewToolWidget(props)
}

type ViewToolWidgetProps struct {
	Path      string
	From      string
	To        string
	IsRunning bool
}

var ViewToolWidget = trail.FC("ViewToolWidget", func(props ViewToolWidgetProps) trail.Node {
	schema := theme.UseTheme()

	label, circle := "Read", "●"
	if props.IsRunning {
		label, circle = "Reading", "○"
	}
	fixed := style.S().WhiteSpace(style.WhiteSpaceNoWrap).Flex(0, 0)
	dotStyle, labelStyle, detailsStyle := fixed, fixed, style.S().
		WhiteSpace(style.WhiteSpaceNoWrap).
		Flex(0, 1).
		MinWidth(style.Cells(0)).
		Overflow(style.OverflowHidden).
		TextOverflow(style.TextOverflowEllipsis)
	if schema != nil {
		dot := schema.Color.Surface.Success
		if props.IsRunning {
			dot = schema.Color.Surface.Info
		}
		dotStyle = dotStyle.Foreground(dot)
		labelStyle = labelStyle.Foreground(schema.Color.Text.Purple)
		detailsStyle = detailsStyle.Foreground(schema.Color.Text.Secondary)
	}

	children := []trail.Node{
		trail.Span(trail.SpanProps{Style: dotStyle}, trail.Text(circle+" ")),
		trail.Span(trail.SpanProps{Style: labelStyle}, trail.Text(label+" ")),
	}
	if props.Path != "" {
		children = append(children,
			trail.Span(trail.SpanProps{Style: fixed},
				icon.FileIcon(icon.FileIconProps{Path: props.Path}), trail.Text(" ")),
		)
	}
	children = append(children, trail.Span(trail.SpanProps{Style: detailsStyle}, trail.Text(viewToolDetails(props))))

	return trail.Box(trail.BoxProps{
		Style: style.S().
			Display(style.DisplayFlex).
			FlexDirection(style.FlexRow).
			Width(style.Percent(100)).
			MaxWidth(style.Percent(100)).
			MinWidth(style.Cells(0)).
			Overflow(style.OverflowHidden),
	}, children...)
})

func viewToolDetails(props ViewToolWidgetProps) string {
	var details []string
	if props.Path != "" {
		details = append(details, props.Path)
	}
	switch {
	case props.From != "" && props.To != "":
		if props.From == props.To {
			details = append(details, "(line "+props.From+")")
		} else {
			details = append(details, "(lines "+props.From+"-"+props.To+")")
		}
	case props.From != "":
		details = append(details, "(from line "+props.From+")")
	case props.To != "":
		details = append(details, "(through line "+props.To+")")
	}
	return strings.Join(details, " ")
}

type viewToolNoticeProps struct {
	Text  string
	Error bool
}

var viewToolNotice = trail.FC("ViewToolNotice", func(props viewToolNoticeProps) trail.Node {
	textStyle := style.S().
		WhiteSpace(style.WhiteSpacePreWrap).
		OverflowWrap(style.OverflowWrapBreakWord)
	if schema := theme.UseTheme(); schema != nil {
		if props.Error {
			textStyle = textStyle.Foreground(schema.Color.Text.Error)
		} else {
			textStyle = textStyle.Foreground(schema.Color.Text.Secondary)
		}
	}
	return trail.Span(trail.SpanProps{Style: textStyle}, trail.Text(props.Text))
})

func WriteToolRenderer(tool hooks.ToolPart) trail.Node { return genericToolRenderer(tool) }

func LsRenderer(tool hooks.ToolPart) trail.Node { return genericToolRenderer(tool) }

func BashToolRenderer(tool hooks.ToolPart) trail.Node { return genericToolRenderer(tool) }

func DefaultToolRenderer(tool hooks.ToolPart) trail.Node { return genericToolRenderer(tool) }

// toolVerbs maps a tool name to its present-progressive and past-tense labels.
var toolVerbs = map[string][2]string{
	"bash":      {"Running", "Ran"},
	"ls":        {"Listing", "Listed"},
	"glob":      {"Searching", "Searched"},
	"grep":      {"Searching", "Searched"},
	"edit":      {"Editing", "Edited"},
	"write":     {"Writing", "Wrote"},
	"remove":    {"Removing", "Removed"},
	"web_fetch": {"Fetching", "Fetched"},
	"download":  {"Downloading", "Downloaded"},
}

// toolPrimaryArg returns the most descriptive single-line argument of a call.
func toolPrimaryArg(call *api.ToolCall) string {
	if call == nil || call.Args == nil {
		return ""
	}
	keys := []string{"path", "pattern", "command", "url", "query", "name"}
	switch call.Name {
	case "bash":
		keys = []string{"command"}
	case "ls":
		keys = []string{"path", "dir"}
	case "glob", "grep":
		keys = []string{"pattern"}
	case "web_fetch", "download":
		keys = []string{"url"}
	}
	for _, key := range keys {
		if v, ok := call.Args[key].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(strings.ReplaceAll(v, "\n", " "))
		}
	}
	return ""
}

func genericToolRenderer(tool hooks.ToolPart) trail.Node {
	name := tool.ID
	if tool.Call != nil {
		name = tool.Call.Name
	} else if tool.Response != nil && tool.Response.Name != "" {
		name = tool.Response.Name
	}
	verbs, ok := toolVerbs[name]
	if !ok {
		verbs = [2]string{"Calling " + name, "Called " + name}
	}
	running := tool.Status == hooks.ToolRunning || tool.Status == hooks.ToolReceiving
	label, circle := verbs[1], "●"
	if running {
		label, circle = verbs[0], "○"
	}
	failed := tool.Status == hooks.ToolFailed || tool.Status == hooks.ToolInterrupted
	switch tool.Status {
	case hooks.ToolPending, hooks.ToolWaitingPermission, hooks.ToolInterrupted:
		label, circle = verbs[0], "○"
	}
	arg := toolPrimaryArg(tool.Call)
	text := circle + " " + label
	if arg != "" {
		text += " (" + arg + ")"
	}
	switch tool.Status {
	case hooks.ToolWaitingPermission:
		text += " (waiting for permission)"
	case hooks.ToolInterrupted:
		text += " (interrupted)"
	}
	nodes := []trail.Node{genericToolLine(genericToolLineProps{Text: text, Error: failed})}
	if tool.Response != nil && tool.Response.IsError && len(tool.Response.Content) > 0 {
		first, _, _ := strings.Cut(strings.TrimSpace(tool.Response.Content[0].Text), "\n")
		if first = strings.TrimSpace(first); first != "" {
			nodes = append(nodes, genericToolLine(genericToolLineProps{Text: "  ⎿  ✗ " + first, Error: true}))
		}
	}
	return trail.Box(trail.BoxProps{
		Style: style.S().Display(style.DisplayFlex).FlexDirection(style.FlexColumn).Width(style.Percent(100)).MinWidth(style.Cells(0)),
	}, nodes...)
}

type genericToolLineProps struct {
	Text  string
	Error bool
}

var genericToolLine = trail.FC("GenericToolLine", func(props genericToolLineProps) trail.Node {
	textStyle := style.S().
		WhiteSpace(style.WhiteSpacePre).
		MinWidth(style.Cells(0)).
		Overflow(style.OverflowHidden).
		TextOverflow(style.TextOverflowEllipsis)
	if schema := theme.UseTheme(); schema != nil {
		if props.Error {
			textStyle = textStyle.Foreground(schema.Color.Text.Error)
		} else {
			textStyle = textStyle.Foreground(schema.Color.Text.Secondary)
		}
	}
	return trail.Span(trail.SpanProps{Style: textStyle}, trail.Text(props.Text))
})
