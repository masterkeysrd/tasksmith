package command

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/masterkeysrd/kite/extras/flight"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/components"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	uicomponents "github.com/masterkeysrd/tasksmith/internal/ui/components"
	"github.com/masterkeysrd/tasksmith/internal/ui/format"
	"github.com/masterkeysrd/tasksmith/internal/ui/styles"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

func (r *Registry) registerToolCommands() {
	r.Register(Command{
		Name:        "tools",
		Usage:       "/tools",
		Description: "List, inspect, and reload workspace tools",
		Handler:     handleTools,
	})
}

func handleTools(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("/tools does not accept arguments (run /tools to browse the workspace toolset)")
	}
	store.EnqueueSlot(store.SlotItem{
		Kind: store.SlotKindPicker,
		Data: ToolsBrowser,
	})
	return nil
}

// ToolScope returns the canonical scope string for a tool:
// [Builtin], [Workspace], [Project], or [User].
func ToolScope(t api.Tool) string {
	if t.Scope != "" {
		return t.Scope
	}
	switch strings.ToLower(strings.TrimSpace(t.Namespace)) {
	case "workspace":
		return "Workspace"
	case "local", "project":
		return "Project"
	case "user":
		return "User"
	case "system", "builtin", "":
		return "Builtin"
	default:
		s := strings.TrimSpace(t.Namespace)
		if len(s) > 0 {
			return strings.ToUpper(s[:1]) + s[1:]
		}
		return "Builtin"
	}
}

// toolStatusBadge returns the availability badge for a tool in the active
// agent's toolset: [Eager] when loaded on the first request, [Deferred] when
// policy-allowed but discoverable only via tool_search, [Denied] when blocked
// by the workspace or agent tool policy ceiling, [Disabled] when the workspace
// switched the tool off (manifest spec.enabled or a WORKSPACE.md exclusion),
// and [Inactive] when the tool is not applicable to the agent at all. An empty
// badge is returned when the listing was not classified for an agent.
func toolStatusBadge(t api.Tool) string {
	switch t.Status {
	case api.ToolStatusEager:
		return "[Eager]"
	case api.ToolStatusDeferred:
		return "[Deferred]"
	case api.ToolStatusDenied:
		return "[Denied]"
	case api.ToolStatusDisabled:
		return "[Disabled]"
	case api.ToolStatusInactive:
		return "[Inactive]"
	default:
		return "[—]"
	}
}

// toolAnnotationBadges renders the safety annotations that shape a tool's
// permission posture as compact badges.
func toolAnnotationBadges(t api.Tool) string {
	var badges []string
	if t.Annotations.IsReadOnly {
		badges = append(badges, "[ReadOnly]")
	}
	if t.Annotations.IsDangerous {
		badges = append(badges, "[Dangerous]")
	}
	if t.Annotations.IsOpenWorld {
		badges = append(badges, "[OpenWorld]")
	}
	return strings.Join(badges, " ")
}

// toolCommandLine renders a tool's declared command line, or an em dash for
// host-native tools that run inside the daemon.
func toolCommandLine(t api.Tool) string {
	if len(t.Command) == 0 {
		return "—"
	}
	return strings.Join(t.Command, " ")
}

// toolSchemaJSON pretty-prints a JSON schema for the inspector, returning an
// empty string when the tool declares no schema for that direction.
func toolSchemaJSON(schema map[string]any) string {
	if len(schema) == 0 {
		return ""
	}
	out, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return ""
	}
	return string(out)
}

// toolStatusColor maps a tool's status to a theme color for the badge column.
func toolStatusColor(scheme *theme.Scheme, status api.ToolStatus) style.Style {
	base := style.S().Bold(true)
	switch status {
	case api.ToolStatusEager:
		return base.Foreground(scheme.Color.Icon.Primary)
	case api.ToolStatusDeferred:
		return base.Foreground(scheme.Color.Text.Warning)
	case api.ToolStatusDenied:
		return base.Foreground(scheme.Color.Text.Error)
	case api.ToolStatusDisabled:
		return base.Foreground(scheme.Color.Text.Muted)
	default:
		return style.S().Foreground(scheme.Color.Text.Muted)
	}
}

// toolColumnWidths sizes the browser's table columns so long tool names, status
// badges, scopes, and annotation sets stay readable. OptionList renders a
// fixed-width column as width minus its two-cell right padding (and the name
// column also carries the two-cell cursor prefix), so each width includes that
// overhead. Status and scope size to the widest badge present but never below
// the common vocabulary, so the columns stay stable for a typical toolset. The
// description column takes whatever width remains; a termWidth of 0 leaves it
// unmeasured.
func toolColumnWidths(tools []api.Tool, termWidth int) (name, status, scope, annotations, description int) {
	longestName, longestStatus, longestScope, widestAnnotations := len("bash"), 0, 0, 0
	for _, t := range tools {
		if n := len(t.Name); n > longestName {
			longestName = n
		}
		if n := len(toolStatusBadge(t)); n > longestStatus {
			longestStatus = n
		}
		if n := len(ToolScope(t)) + 2; n > longestScope {
			longestScope = n
		}
		if n := len(toolAnnotationBadges(t)); n > widestAnnotations {
			widestAnnotations = n
		}
	}
	const columnPadding = 2
	name = min(max(longestName+2+columnPadding, 16), 30)
	status = max(longestStatus, len("[Deferred]")) + columnPadding + 1
	scope = max(longestScope, len("[Workspace]")) + columnPadding + 1
	annotations = min(max(widestAnnotations+columnPadding+1, 12), 36)
	if termWidth > 0 {
		description = max(20, termWidth-name-status-scope-annotations)
	}
	return name, status, scope, annotations, description
}

type toolInspectorProps struct {
	tool    api.Tool
	onClose func()
}

var toolInspector = trail.FC("ToolInspector", func(props toolInspectorProps) trail.Node {
	scheme := theme.UseTheme()
	outerStyle := style.S().Border(
		style.SingleBorder().Top(true).Bottom(false).Left(false).Right(false).Color(scheme.Color.Border.Default),
	)

	titleStyle := style.S().Bold(true).Foreground(scheme.Color.Text.Primary)
	scopeBadgeStyle := style.S().Bold(true).Foreground(scheme.Color.Icon.Primary)
	statusBadgeStyle := toolStatusColor(scheme, props.tool.Status)
	metaStyle := style.S().Foreground(scheme.Color.Text.Secondary)
	labelStyle := style.S().Bold(true).Foreground(scheme.Color.Text.Primary)
	descStyle := style.S().Foreground(scheme.Color.Text.Secondary)
	codeStyle := style.S().Width(style.Percent(100)).MaxWidth(style.Percent(100)).MinWidth(style.Percent(0))

	// Each logical line needs its own flex-row box; sibling spans inside a
	// column box collapse onto a single row.
	line := func(items ...trail.Node) trail.Node {
		return trail.Box(trail.BoxProps{
			Style: style.S().FlexDirection(style.FlexRow),
		}, items...)
	}
	sectionLabel := func(text string) trail.Node {
		return trail.Box(trail.BoxProps{
			Style: style.S().FlexDirection(style.FlexRow).Margin(1, 0, 0, 0),
		}, trail.Span(trail.SpanProps{Style: labelStyle}, trail.Text(text)))
	}

	headerItems := []trail.Node{
		trail.Span(trail.SpanProps{Style: titleStyle}, trail.Text(fmt.Sprintf("Tool: %s  ", props.tool.Name))),
		trail.Span(trail.SpanProps{Style: scopeBadgeStyle}, trail.Text(fmt.Sprintf("[%s]  ", ToolScope(props.tool)))),
		trail.Span(trail.SpanProps{Style: statusBadgeStyle}, trail.Text(toolStatusBadge(props.tool)+"  ")),
		trail.Span(trail.SpanProps{Style: metaStyle}, trail.Text(fmt.Sprintf("(est. %s tok)", format.Tokens(props.tool.Tokens)))),
	}

	var meta []trail.Node
	if props.tool.DisplayName != "" && props.tool.DisplayName != props.tool.Name {
		meta = append(meta, line(trail.Span(trail.SpanProps{Style: metaStyle}, trail.Text("Display name: "+props.tool.DisplayName))))
	}
	if props.tool.Description != "" {
		meta = append(meta, line(trail.Span(trail.SpanProps{Style: descStyle}, trail.Text(props.tool.Description))))
	}

	annotationText := toolAnnotationBadges(props.tool)
	if props.tool.Annotations.IsIdempotent {
		if annotationText != "" {
			annotationText += " "
		}
		annotationText += "[Idempotent]"
	}
	if props.tool.Annotations.UserHint != "" {
		if annotationText != "" {
			annotationText += "  "
		}
		annotationText += "hint: " + props.tool.Annotations.UserHint
	}
	if annotationText == "" {
		annotationText = "— (no safety annotations declared)"
	}

	meta = append(meta,
		sectionLabel("Command"),
		line(trail.Span(trail.SpanProps{
			Style: style.S().Foreground(scheme.Color.Text.Secondary).WhiteSpace(style.WhiteSpacePreWrap),
		}, trail.Text(toolCommandLine(props.tool)))),
		sectionLabel("Annotations"),
		line(trail.Span(trail.SpanProps{
			Style: style.S().Foreground(scheme.Color.Text.Secondary).WhiteSpace(style.WhiteSpacePreWrap),
		}, trail.Text(annotationText))),
	)

	var schemaBlocks []trail.Node
	for _, section := range []struct {
		label  string
		schema map[string]any
	}{
		{label: "inputSchema", schema: props.tool.InputSchema},
		{label: "outputSchema", schema: props.tool.OutputSchema},
	} {
		schemaBlocks = append(schemaBlocks, sectionLabel(section.label))
		if rendered := toolSchemaJSON(section.schema); rendered != "" {
			schemaBlocks = append(schemaBlocks, uicomponents.CodeBlock(uicomponents.CodeBlockProps{
				Code:       rendered,
				Lang:       "json",
				HideHeader: true,
				Compact:    true,
				Wrap:       true,
				Style:      codeStyle,
			}))
		} else {
			schemaBlocks = append(schemaBlocks, line(trail.Span(trail.SpanProps{
				Style: style.S().Foreground(scheme.Color.Text.Muted),
			}, trail.Text("— (no schema declared)"))))
		}
	}

	return uicomponents.ScrollViewer(uicomponents.ScrollViewerProps{
		Header:             headerItems,
		HeaderMarginBottom: 1,
		Meta:               meta,
		OuterStyle:         &outerStyle,
		OnClose:            props.onClose,
		Bindings: []uicomponents.KeyBinding{
			{Key: "↑/↓ or j/k", Label: "Scroll"},
			{Key: "pgup/pgdn", Label: "Page"},
			{Key: "esc", Label: "Return to tools"},
		},
	}, schemaBlocks...)
})

type emptyToolsViewProps struct {
	slotID   string
	notice   string
	onReload func()
}

var emptyToolsView = trail.FC("EmptyToolsView", func(props emptyToolsViewProps) trail.Node {
	common := styles.UseCommon()
	bindings := uicomponents.UseKeyMap([]uicomponents.KeyAction{
		{Keys: []string{"r"}, Label: "Reload workspace", OnPress: props.onReload},
		{Keys: []string{"esc", "escape"}, Label: "Close", OnPress: func() { store.RemoveSlot(props.slotID) }},
	}, true)
	emptyStyle := common.Secondary.
		WhiteSpace(style.WhiteSpacePreWrap).
		MarginBottom(1)
	return uicomponents.Panel(uicomponents.PanelProps{
		Title: "Tools (0 available)", Notice: props.notice, Bindings: bindings,
	}, trail.Span(trail.SpanProps{Style: emptyStyle}, trail.Text("  No tools available in workspace.")))
})

type toolsListRoute struct{}

type toolDetailRoute struct{ Name string }

type toolsScreenProps struct {
	slotID string
	route  flight.Route
}

// ToolsBrowser renders an interactive tool table and schema inspector inside <Slot />.
var ToolsBrowser = trail.FC("ToolsBrowser", func(slotID string) trail.Node {
	return flight.Stack(flight.StackProps{
		InitialRoute: toolsListRoute{},
		RenderRoute: func(route flight.Route) trail.Node {
			return toolsScreen(toolsScreenProps{slotID: slotID, route: route})
		},
	})
})

var toolsScreen = trail.FC("ToolsScreen", func(props toolsScreenProps) trail.Node {
	nav := flight.UseNavigation()
	scheme := theme.UseTheme()
	winSize := trail.UseWindowSize()
	slotID := props.slotID
	toolsPtr := store.UseApp(func(s store.AppState) *[]api.Tool { return s.Tools })
	cachedAgent := store.UseApp(func(s store.AppState) string { return s.ToolsAgent })
	toolsFetched := store.UseApp(func(s store.AppState) bool { return s.ToolsFetched })
	defaultAgent := store.UseApp(func(s store.AppState) string { return s.DefaultAgent })
	sessionAgent := store.UseSession(func(s store.SessionState) string { return s.Config.Agent })
	notice, setNotice := kitex.UseState("")
	isReloading, setIsReloading := kitex.UseState(false)
	toggling, setToggling := kitex.UseState("")

	var tools []api.Tool
	if toolsPtr != nil {
		tools = *toolsPtr
	}

	// Statuses are agent-specific, so the effective agent decides whether the
	// cached listing is still the right one to show.
	agent := strings.TrimSpace(sessionAgent)
	if agent == "" {
		agent = strings.TrimSpace(defaultAgent)
	}

	// Fetch tools on mount, and whenever the active agent changes, because the
	// eager/deferred/denied classification differs per agent. The fetched flag
	// plus the recorded agent form the freshness key, so an empty toolset does
	// not re-trigger the fetch.
	kitex.UseEffect(func() {
		if toolsFetched && cachedAgent == agent {
			return
		}
		if store.Client() == nil {
			return
		}
		go func() {
			if _, err := store.RefreshWorkspaceTools(context.Background(), agent); err != nil {
				store.AppendCommandOutput("/tools", "Failed to load workspace tools: "+err.Error())
			}
		}()
	}, []any{agent, cachedAgent, toolsFetched})

	doReload := func() {
		if isReloading() {
			return
		}
		setIsReloading(true)
		setNotice("Reloading workspace tools via registry.Reload...")
		go func() {
			defer setIsReloading(false)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := store.ReloadWorkspace(ctx); err != nil {
				setNotice("Failed to reload workspace: " + err.Error())
				return
			}
			count := 0
			if s := store.GetApp().Tools; s != nil {
				for _, t := range *s {
					if t.Enabled {
						count++
					}
				}
			}
			setNotice(fmt.Sprintf("Workspace reloaded via registry.Reload (%d tools available).", count))
		}()
	}

	// toggleEnabled flips one tool's availability through the daemon, which
	// decides where the change lands: spec.enabled on a writable workspace
	// manifest, or a WORKSPACE.md tools policy exclusion for inherited and
	// built-in tools. The PATCH response already reflects the new state, but
	// statuses are agent-classified, so the store cache is refetched.
	toggleEnabled := func(opt uicomponents.Option) {
		c := store.Client()
		if c == nil || toggling() != "" {
			return
		}
		target := false
		for i := range tools {
			if tools[i].Name == opt.Value {
				target = tools[i].Enabled
				break
			}
		}
		setToggling(opt.Value)
		goMCPBackground(func() {
			defer setToggling("")
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if _, err := c.SetToolEnabled(ctx, opt.Value, !target); err != nil {
				setNotice("Failed to update " + opt.Value + ": " + err.Error())
				return
			}
			if _, err := store.ForceRefreshWorkspaceTools(ctx); err != nil {
				setNotice("Updated " + opt.Value + ", but refreshing the tool listing failed: " + err.Error())
				return
			}
			if target {
				setNotice("Disabled " + opt.Value + ".")
			} else {
				setNotice("Enabled " + opt.Value + ".")
			}
		})
	}

	if route, ok := props.route.(toolDetailRoute); ok {
		return toolDetailView(toolDetailViewProps{Name: route.Name, Tools: tools, OnClose: nav.Pop})
	}

	if len(tools) == 0 {
		return emptyToolsView(emptyToolsViewProps{
			slotID:   slotID,
			notice:   notice(),
			onReload: doReload,
		})
	}

	enabledCount := 0
	for i := range tools {
		if tools[i].Enabled {
			enabledCount++
		}
	}
	title := fmt.Sprintf("Tools (%d available)", enabledCount)
	if enabledCount < len(tools) {
		title = fmt.Sprintf("Tools (%d of %d available)", enabledCount, len(tools))
	}
	if n := notice(); n != "" {
		title = fmt.Sprintf("%s — %s", title, n)
	}

	nameCol, statusCol, scopeCol, annotationCol, availDescWidth := toolColumnWidths(tools, winSize.Width)

	opts := make([]uicomponents.Option, len(tools))
	for i, tl := range tools {
		statusBadge := toolStatusBadge(tl)
		scopeBadge := fmt.Sprintf("[%s]", ToolScope(tl))
		annotations := toolAnnotationBadges(tl)
		desc := format.CompactSingleLine(tl.Description, availDescWidth)

		statusNode := trail.Box(trail.BoxProps{
			Style: style.S().
				FlexDirection(style.FlexRow).
				Width(style.Percent(100)).
				MinWidth(style.Cells(0)).
				Overflow(style.OverflowHidden),
		}, trail.Span(trail.SpanProps{
			Style: toolStatusColor(scheme, tl.Status).
				WhiteSpace(style.WhiteSpacePre).
				MinWidth(style.Cells(0)).
				Overflow(style.OverflowHidden),
		}, trail.Text(statusBadge)))

		descNode := trail.Box(trail.BoxProps{
			Style: style.S().
				FlexDirection(style.FlexRow).
				Width(style.Percent(100)).
				MinWidth(style.Cells(0)).
				Overflow(style.OverflowHidden),
		}, trail.Span(trail.SpanProps{
			Style: style.S().
				Flex(0, 1).
				MinWidth(style.Cells(0)).
				Overflow(style.OverflowHidden).
				WhiteSpace(style.WhiteSpacePre).
				Foreground(scheme.Color.Text.Muted),
		}, trail.Text(desc)))

		opts[i] = uicomponents.Option{
			Value:       tl.Name,
			Label:       tl.Name,
			Description: desc,
			Cells:       []string{tl.Name, statusBadge, scopeBadge, annotations, desc},
			CellNodes:   []trail.Node{nil, statusNode, nil, nil, descNode},
		}
	}

	inspect := func(opt uicomponents.Option) {
		nav.Push(toolDetailRoute{Name: opt.Value})
	}

	pickerProps := components.NewPicker(title, opts, inspect)
	pickerProps.Columns = []uicomponents.Column{
		{Width: nameCol, Align: style.TextAlignLeft},
		{Width: statusCol, Align: style.TextAlignLeft},
		{Width: scopeCol, Align: style.TextAlignLeft},
		{Width: annotationCol, Align: style.TextAlignLeft},
		{Width: 0, Align: style.TextAlignLeft},
	}
	pickerProps.Filterable = false
	pickerProps.Bindings = []uicomponents.KeyBinding{
		{Key: "↑/↓", Label: "Select"},
		{Key: "enter", Label: "Inspect tool"},
		{Key: "space", Label: "Enable/disable"},
		{Key: "r", Label: "Reload workspace"},
		{Key: "esc", Label: "Close"},
	}
	pickerProps.Keys = map[string]func(uicomponents.Option){
		"space": toggleEnabled,
		"r": func(_ uicomponents.Option) {
			doReload()
		},
		"i": inspect,
	}
	pickerProps.OnCancel = func() {
		store.RemoveSlot(slotID)
		store.AppendCommandOutput("/tools", "Exited /tools command")
	}

	return components.Picker(pickerProps)
})

type toolDetailViewProps struct {
	Name    string
	Tools   []api.Tool
	OnClose func()
}

var toolDetailView = trail.FC("ToolDetailView", func(props toolDetailViewProps) trail.Node {
	for _, tool := range props.Tools {
		if tool.Name == props.Name {
			return toolInspector(toolInspectorProps{tool: tool, onClose: props.OnClose})
		}
	}
	return missingToolView(props.OnClose)
})

var missingToolView = trail.FC("MissingToolView", func(onClose func()) trail.Node {
	bindings := uicomponents.UseKeyMap([]uicomponents.KeyAction{
		{Keys: []string{"esc", "escape"}, Label: "Return to tools", OnPress: onClose},
	}, true)
	return uicomponents.Panel(uicomponents.PanelProps{Notice: "Tool no longer available.", Bindings: bindings})
})
