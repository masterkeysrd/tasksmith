package command

// This file holds the tool drill-down reached from a server's overview card: the
// filterable tool list and the read-only detail view for one tool.

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/masterkeysrd/kite/dom"
	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/components"
	uicomponents "github.com/masterkeysrd/tasksmith/internal/ui/components"
	"github.com/masterkeysrd/tasksmith/internal/ui/format"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

// mcpToolName renders the name a discovered tool is called by, preferring the
// server's own name over the materialized "<server>__<tool>" form.
func mcpToolName(tool api.MCPToolSummary) string {
	if raw := strings.TrimSpace(tool.RawName); raw != "" {
		return raw
	}
	return tool.Name
}

// mcpToolIcon renders the policy glyph for a tool: permitted tools are green, a
// prohibited one is red, because that verdict is what decides whether the user
// can act on the tool at all.
func mcpToolIcon(scheme *theme.Scheme, allowed bool) trail.Node {
	color := scheme.Color.Text.Success
	if !allowed {
		color = scheme.Color.Text.Error
	}
	return trail.Span(trail.SpanProps{
		Style: style.S().WhiteSpace(style.WhiteSpacePre).Foreground(color),
	}, trail.Text(map[bool]string{true: "✔", false: "✘"}[allowed]))
}

type mcpToolListProps struct {
	livePaneProps
	server api.MCPServer
	onBack func()
	// onSelect opens the detail view for the chosen tool.
	onSelect func(tool api.MCPToolSummary)
}

// mcpToolList lists one server's discovered tools. It stays filterable because a
// server can expose dozens of tools and their names are the only handle on them.
var mcpToolList = trail.FC("MCPToolList", func(props mcpToolListProps) trail.Node {
	scheme := theme.UseTheme()
	tools := props.server.Tools

	pickerProps := components.NewPicker(
		fmt.Sprintf("%s — tools (%d)", props.server.Name, len(tools)),
		nil,
		func(opt uicomponents.Option) {
			for _, tool := range tools {
				if tool.Name == opt.Value && props.onSelect != nil {
					props.onSelect(tool)
					return
				}
			}
		},
	)
	options := make([]uicomponents.Option, len(tools))
	for i, tool := range tools {
		options[i] = uicomponents.Option{
			Value:       tool.Name,
			Label:       mcpToolName(tool),
			Description: format.CompactSingleLine(tool.Description, 60),
			Cells:       []string{mcpToolName(tool), mcpToolBadge(tool)},
			Icon:        mcpToolIcon(scheme, tool.Allowed),
			CellNodes: []trail.Node{
				nil,
				mcpBadgeNode(scheme, mcpToolBadge(tool), mcpAllowedColor(scheme, tool.Allowed)),
			},
		}
	}
	pickerProps.Options = options
	pickerProps.Columns = []uicomponents.Column{
		{Width: 0, Align: style.TextAlignLeft},
		{Width: len("[Allowed] [ReadOnly] [Dangerous] [OpenWorld]") + 3, Align: style.TextAlignLeft},
	}
	pickerProps.Filterable = true
	pickerProps.Bindings = []uicomponents.KeyBinding{
		{Key: "↑/↓", Label: "Select"},
		{Key: "type", Label: "Filter"},
		{Key: "enter", Label: "Details"},
		{Key: "esc", Label: "Back"},
	}
	pickerProps.OnCancel = func() {
		if props.onBack != nil {
			props.onBack()
		}
	}
	return components.Picker(pickerProps)
})

// mcpToolPolicyLine renders a tool's policy verdict followed by its safety
// annotations, e.g. `✔ Allowed · [ReadOnly]`. The verdict leads because it decides
// whether the tool can run at all; the annotations only qualify what running it
// does, so they never stand alone.
func mcpToolPolicyLine(tool api.MCPToolSummary) string {
	line := "✔ Allowed"
	if !tool.Allowed {
		line = "✘ Prohibited"
	}
	var badges []string
	if tool.Annotations.IsReadOnly {
		badges = append(badges, "[ReadOnly]")
	}
	if tool.Annotations.IsDangerous {
		badges = append(badges, "[Dangerous]")
	}
	if tool.Annotations.IsOpenWorld {
		badges = append(badges, "[OpenWorld]")
	}
	if len(badges) > 0 {
		line += " · " + strings.Join(badges, " ")
	}
	return line
}

type mcpToolDetailProps struct {
	livePaneProps
	server api.MCPServer
	tool   api.MCPToolSummary
	// notice carries the outcome of the last action taken from this view, such as
	// a manifest edit, so the result is visible where the action was taken.
	notice string
	// onEnable permits this tool in the server manifest. It is nil when the tool is
	// already allowed or the manifest cannot be rewritten, which is also when the
	// view withholds the `e` shortcut.
	onEnable func()
	onBack   func()
}

// mcpToolDetailView shows everything known about one discovered tool, including
// the input schema it expects and, for a prohibited tool, the way to permit it.
var mcpToolDetailView = trail.FC("MCPToolDetail", func(props mcpToolDetailProps) trail.Node {
	scheme := theme.UseTheme()
	scrollPos, setScrollPos := kitex.UseState(0)
	scrollBoxRef := kitex.UseRef[dom.Element](nil)
	winSize := trail.UseWindowSize()

	kitex.UseLayoutEffect(func() {
		if el := scrollBoxRef.Current; el != nil {
			el.ScrollTo(0, scrollPos())
		}
	}, []any{scrollPos()})

	trail.UseInput(func(_ string, k event.KeyEvent) {
		switch {
		case k.MatchString("esc", "escape"):
			if props.onBack != nil {
				props.onBack()
			}
		case k.MatchString("up"):
			if cur := scrollPos(); cur > 0 {
				setScrollPos(cur - 1)
			}
		case k.MatchString("down"):
			setScrollPos(scrollPos() + 1)
		case k.MatchString("pgup", "ctrl+u"):
			if cur := scrollPos(); cur > 8 {
				setScrollPos(cur - 8)
			} else {
				setScrollPos(0)
			}
		case k.MatchString("pgdown", "ctrl+d"):
			setScrollPos(scrollPos() + 8)
		case k.MatchString("g"):
			setScrollPos(0)
		case k.MatchString("G"):
			setScrollPos(9999)
		case k.MatchString("e") && props.onEnable != nil:
			props.onEnable()
		}
	})

	titleStyle := style.S().Bold(true).Foreground(scheme.Color.Text.Primary)
	headingStyle := style.S().Bold(true).Foreground(scheme.Color.Text.Primary)
	mutedStyle := style.S().Foreground(scheme.Color.Text.Muted)
	metaStyle := style.S().Foreground(scheme.Color.Text.Secondary)
	noticeStyle := style.S().Foreground(scheme.Color.Text.Warning)

	policyColor := mcpAllowedColor(scheme, props.tool.Allowed)

	// The label column hugs this view's own short labels, so the card does not
	// reserve the wider column the server overview needs for "Config location:".
	fields := []mcpCardField{
		mcpCardTextField("Tool name", mcpToolName(props.tool), metaStyle),
		mcpCardTextField("Full name", props.tool.Name, metaStyle),
		mcpCardTextField("Policy", mcpToolPolicyLine(props.tool), policyColor),
	}
	labelWidth := mcpCardLabelWidthFor(fields, 0)
	valueWidth := winSize.Width - mcpCardIndent - labelWidth - 1

	rows := []trail.Node{
		mcpDetailLine(trail.Span(trail.SpanProps{Style: titleStyle},
			trail.Text(mcpToolName(props.tool)))),
		mcpDetailLine(trail.Span(trail.SpanProps{Style: mutedStyle},
			trail.Text(props.server.Name))),
	}
	if notice := strings.TrimSpace(props.notice); notice != "" {
		// The reason a manifest edit failed is the one thing the user needs to read in
		// full, so it wraps across lines instead of running off the terminal edge.
		for _, line := range mcpWrapText(notice, winSize.Width-mcpCardIndent, 0) {
			rows = append(rows, mcpDetailLine(trail.Span(trail.SpanProps{
				Style: noticeStyle.WhiteSpace(style.WhiteSpacePre),
			}, trail.Text(line))))
		}
	}
	rows = append(rows, mcpDetailLine(trail.Span(trail.SpanProps{}, trail.Text(""))))

	for _, field := range fields {
		rows = append(rows, mcpCardFieldRow(field, labelWidth, valueWidth, metaStyle))
	}

	// The description is a section rather than an aligned row because server tool
	// descriptions run to sentences that would wrap awkwardly inside a value column.
	if description := strings.TrimSpace(props.tool.Description); description != "" {
		rows = append(rows, blankRow(), sectionHeading(headingStyle, "Description:"))
		for _, line := range mcpWrapText(description, winSize.Width-mcpCardIndent, 0) {
			rows = append(rows, mcpDetailLine(trail.Span(trail.SpanProps{
				Style: metaStyle.WhiteSpace(style.WhiteSpacePre),
			}, trail.Text(line))))
		}
	}

	if params := mcpToolParameters(props.tool); len(params) > 0 {
		rows = append(rows, blankRow(), sectionHeading(headingStyle, "Parameters:"))
		for _, param := range params {
			rows = append(rows, mcpDetailLine(trail.Span(trail.SpanProps{
				Style: metaStyle.WhiteSpace(style.WhiteSpacePre),
			}, trail.Text(param))))
		}
	}

	bindings := []uicomponents.KeyBinding{{Key: "↑/↓", Label: "Scroll"}}
	if props.onEnable != nil {
		bindings = append(bindings, uicomponents.KeyBinding{Key: "e", Label: "Enable in manifest"})
	}
	bindings = append(bindings, uicomponents.KeyBinding{Key: "esc", Label: "Back to tools"})
	rows = append(rows, uicomponents.KeyboardHint(uicomponents.KeyboardHintProps{
		Bindings: bindings,
	}))

	return trail.Box(trail.BoxProps{
		Style: style.S().
			Border(style.SingleBorder().Top(true).Bottom(false).Left(false).Right(false).Color(scheme.Color.Border.Default)).
			Width(style.Percent(100)).
			FlexDirection(style.FlexColumn),
	},
		trail.Box(trail.BoxProps{
			Ref: scrollBoxRef,
			Style: style.S().
				Width(style.Percent(100)).
				MaxHeight(style.Cells(mcpInspectorHeight(winSize.Height))).
				OverflowY(style.OverflowAuto).
				ScrollbarY(true),
		}, trail.Box(trail.BoxProps{
			Style: style.S().FlexDirection(style.FlexColumn),
		}, rows...)),
	)
})

// mcpToolParameters renders a tool's input schema as parameter rows, required
// parameters first and then optional ones alphabetically, because the required
// ones are what the caller has to work out before a call can succeed.
func mcpToolParameters(tool api.MCPToolSummary) []string {
	properties, _ := tool.InputSchema["properties"].(map[string]any)
	if len(properties) == 0 {
		return nil
	}
	required := map[string]bool{}
	switch list := tool.InputSchema["required"].(type) {
	case []any:
		for _, entry := range list {
			if name, ok := entry.(string); ok {
				required[name] = true
			}
		}
	case []string:
		for _, name := range list {
			required[name] = true
		}
	}

	names := slices.Sorted(maps.Keys(properties))
	slices.SortStableFunc(names, func(a, b string) int {
		if ra, rb := required[a], required[b]; ra != rb {
			if ra {
				return -1
			}
			return 1
		}
		return strings.Compare(a, b)
	})

	rows := make([]string, 0, len(names))
	for _, name := range names {
		entry, _ := properties[name].(map[string]any)
		paramType, _ := entry["type"].(string)
		if paramType == "" {
			paramType = "unknown"
		}
		label := "  ● " + name
		if required[name] {
			label += " (required)"
		}
		label += ": " + paramType
		if description, _ := entry["description"].(string); strings.TrimSpace(description) != "" {
			label += " - " + strings.TrimSpace(description)
		}
		rows = append(rows, label)
	}
	return rows
}
