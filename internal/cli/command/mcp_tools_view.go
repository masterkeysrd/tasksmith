package command

// This file holds the tool drill-down reached from a server's overview card: the
// filterable tool list and the read-only detail view for one tool.

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/components"
	uicomponents "github.com/masterkeysrd/tasksmith/internal/ui/components"
	"github.com/masterkeysrd/tasksmith/internal/ui/format"
	"github.com/masterkeysrd/tasksmith/internal/ui/styles"
)

// mcpToolName renders the name a discovered tool is called by, preferring the
// server's own name over the materialized "<server>__<tool>" form.
func mcpToolName(tool api.MCPToolSummary) string {
	if raw := strings.TrimSpace(tool.RawName); raw != "" {
		return raw
	}
	return tool.Name
}

func mcpPolicyBadge(label string, allowed bool) trail.Node {
	tone := uicomponents.ToneError
	if allowed {
		tone = uicomponents.ToneSuccess
	}
	return uicomponents.Badge(uicomponents.BadgeProps{Label: label, Tone: tone})
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
			Icon:        mcpPolicyBadge(map[bool]string{true: "✔", false: "✘"}[tool.Allowed], tool.Allowed),
			CellNodes: []trail.Node{
				nil,
				mcpPolicyBadge(mcpToolBadge(tool), tool.Allowed),
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
	common := styles.UseCommon()
	size := trail.UseWindowSize()
	width := size.Width - mcpCardIndent
	bindings := uicomponents.UseKeyMap([]uicomponents.KeyAction{
		{Hint: "↑/↓", Label: "Scroll"},
		{Keys: []string{"e"}, Label: "Enable in manifest", OnPress: props.onEnable, Disabled: props.onEnable == nil},
		{Keys: []string{"esc", "escape"}, Label: "Back to tools", OnPress: props.onBack},
	}, true)
	params := mcpToolParameters(props.tool)
	description := strings.TrimSpace(props.tool.Description)
	return uicomponents.ScrollViewer(uicomponents.ScrollViewerProps{
		MaxHeight:  mcpInspectorHeight(size.Height),
		OuterStyle: &common.Panel,
		Bindings:   bindings,
	},
		sectionHeading(common.Title, mcpToolName(props.tool)),
		sectionHeading(common.Muted, props.server.Name),
		kitex.If(props.notice != "", func() trail.Node {
			return mcpWrappedLines(common.Warning, props.notice, width)
		}),
		blankRow(),
		uicomponents.KVList(uicomponents.KVListProps{
			Indent:        mcpCardIndent,
			MinLabelWidth: len("Tool name:"),
			Items: []uicomponents.KVItem{
				mcpCardTextField("Tool name", mcpToolName(props.tool), common.Secondary),
				mcpCardTextField("Full name", props.tool.Name, common.Secondary),
				{Label: "Policy", ValueNode: mcpPolicyBadge(mcpToolPolicyLine(props.tool), props.tool.Allowed)},
			},
		}),
		kitex.If(description != "", func() trail.Node {
			return kitex.Fragment(
				blankRow(), sectionHeading(common.Title, "Description:"),
				mcpWrappedLines(common.Secondary, description, width),
			)
		}),
		kitex.If(len(params) > 0, func() trail.Node {
			return kitex.Fragment(blankRow(), sectionHeading(common.Title, "Parameters:"),
				kitex.Map(params, func(param string, _ int) trail.Node {
					return sectionHeading(common.Secondary.WhiteSpace(style.WhiteSpacePre), param)
				}))
		}),
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
