package command

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/masterkeysrd/kite/dom"
	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/components"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	uicomponents "github.com/masterkeysrd/tasksmith/internal/ui/components"
	"github.com/masterkeysrd/tasksmith/internal/ui/format"
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
// by the workspace or agent tool policy ceiling, and [Inactive] when the tool
// is not applicable to the agent at all. An empty badge is returned when the
// listing was not classified for an agent.
func toolStatusBadge(t api.Tool) string {
	switch t.Status {
	case api.ToolStatusEager:
		return "[Eager]"
	case api.ToolStatusDeferred:
		return "[Deferred]"
	case api.ToolStatusDenied:
		return "[Denied]"
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
	slotID  string
	tool    api.Tool
	onClose func()
}

var toolInspector = trail.FC("ToolInspector", func(props toolInspectorProps) trail.Node {
	scrollPos, setScrollPos := kitex.UseState(0)
	scrollBoxRef := kitex.UseRef[dom.Element](nil)

	kitex.UseLayoutEffect(func() {
		if el := scrollBoxRef.Current; el != nil {
			el.ScrollTo(0, scrollPos())
		}
	}, []any{scrollPos()})

	trail.UseInput(func(_ string, k event.KeyEvent) {
		switch {
		case k.MatchString("esc", "escape"):
			if props.onClose != nil {
				props.onClose()
			}
		case k.MatchString("up", "k", "ctrl+p"):
			cur := scrollPos()
			if cur > 0 {
				setScrollPos(cur - 1)
			}
		case k.MatchString("down", "j", "ctrl+n"):
			setScrollPos(scrollPos() + 1)
		case k.MatchString("pgup", "b", "ctrl+u", "ctrl+b"):
			cur := scrollPos()
			if cur > 8 {
				setScrollPos(cur - 8)
			} else {
				setScrollPos(0)
			}
		case k.MatchString("pgdown", "space", "f", "ctrl+d", "ctrl+f"):
			setScrollPos(scrollPos() + 8)
		case k.MatchString("home", "g"):
			setScrollPos(0)
		case k.MatchString("end", "G"):
			setScrollPos(9999)
		}
	})

	scheme := theme.UseTheme()
	border := style.SingleBorder().Top(true).Bottom(false).Left(false).Right(false).Color(scheme.Color.Border.Default)

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

	var children []trail.Node

	headerItems := []trail.Node{
		trail.Span(trail.SpanProps{Style: titleStyle}, trail.Text(fmt.Sprintf("Tool: %s  ", props.tool.Name))),
		trail.Span(trail.SpanProps{Style: scopeBadgeStyle}, trail.Text(fmt.Sprintf("[%s]  ", ToolScope(props.tool)))),
		trail.Span(trail.SpanProps{Style: statusBadgeStyle}, trail.Text(toolStatusBadge(props.tool)+"  ")),
		trail.Span(trail.SpanProps{Style: metaStyle}, trail.Text(fmt.Sprintf("(est. %s tok)", format.Tokens(props.tool.Tokens)))),
	}
	if cur := scrollPos(); cur > 0 {
		headerItems = append(headerItems, trail.Span(trail.SpanProps{Style: metaStyle}, trail.Text(fmt.Sprintf("  •  line +%d", cur))))
	}
	children = append(children, trail.Box(trail.BoxProps{
		Style: style.S().FlexDirection(style.FlexRow).Margin(0, 0, 1, 0),
	}, headerItems...))

	if props.tool.DisplayName != "" && props.tool.DisplayName != props.tool.Name {
		children = append(children, line(trail.Span(trail.SpanProps{Style: metaStyle}, trail.Text("Display name: "+props.tool.DisplayName))))
	}
	if props.tool.Description != "" {
		children = append(children, line(trail.Span(trail.SpanProps{Style: descStyle}, trail.Text(props.tool.Description))))
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

	children = append(children,
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

	scrollBoxStyle := style.S().
		Width(style.Percent(100)).
		MaxHeight(style.Cells(18)).
		OverflowY(style.OverflowAuto).
		ScrollbarY(true)

	children = append(children, trail.Box(trail.BoxProps{
		Ref:   scrollBoxRef,
		Style: scrollBoxStyle,
	}, trail.Box(trail.BoxProps{
		Style: style.S().FlexDirection(style.FlexColumn),
	}, schemaBlocks...)))

	children = append(children, uicomponents.KeyboardHint(uicomponents.KeyboardHintProps{
		Bindings: []uicomponents.KeyBinding{
			{Key: "↑/↓ or j/k", Label: "Scroll"},
			{Key: "pgup/pgdn", Label: "Page"},
			{Key: "esc", Label: "Return to tools"},
		},
	}))

	return trail.Box(trail.BoxProps{
		Style: style.S().
			Border(border).
			Width(style.Percent(100)).
			FlexDirection(style.FlexColumn),
	}, children...)
})

type emptyToolsViewProps struct {
	slotID   string
	notice   string
	onReload func()
}

var emptyToolsView = trail.FC("EmptyToolsView", func(props emptyToolsViewProps) trail.Node {
	trail.UseInput(func(_ string, k event.KeyEvent) {
		if k.MatchString("esc", "escape") {
			store.RemoveSlot(props.slotID)
		} else if k.MatchString("r") {
			if props.onReload != nil {
				props.onReload()
			}
		}
	})

	scheme := theme.UseTheme()
	border := style.SingleBorder().Top(true).Bottom(false).Left(false).Right(false).Color(scheme.Color.Border.Default)
	boxStyle := style.S().
		Border(border).
		Width(style.Percent(100)).
		FlexDirection(style.FlexColumn)
	titleStyle := style.S().Bold(true).Margin(0, 0, 1, 0).Foreground(scheme.Color.Text.Primary)
	emptyStyle := style.S().Margin(0, 0, 1, 0).Foreground(scheme.Color.Text.Secondary)

	var children []trail.Node
	children = append(children,
		trail.Span(trail.SpanProps{Style: titleStyle}, trail.Text("Tools (0 available)")),
		trail.Span(trail.SpanProps{Style: emptyStyle}, trail.Text("  No tools available in workspace.")),
	)
	if props.notice != "" {
		children = append(children, trail.Span(trail.SpanProps{
			Style: style.S().Margin(0, 0, 1, 0).Foreground(scheme.Color.Text.Primary),
		}, trail.Text("  "+props.notice)))
	}
	children = append(children, uicomponents.KeyboardHint(uicomponents.KeyboardHintProps{
		Bindings: []uicomponents.KeyBinding{
			{Key: "r", Label: "Reload workspace"},
			{Key: "esc", Label: "Close"},
		},
	}))

	return trail.Box(trail.BoxProps{Style: boxStyle}, children...)
})

// ToolsBrowser renders an interactive tool table and schema inspector inside <Slot />.
var ToolsBrowser = trail.FC("ToolsBrowser", func(slotID string) trail.Node {
	toolsPtr := store.UseApp(func(s store.AppState) *[]api.Tool { return s.Tools })
	cachedAgent := store.UseApp(func(s store.AppState) string { return s.ToolsAgent })
	toolsFetched := store.UseApp(func(s store.AppState) bool { return s.ToolsFetched })
	defaultAgent := store.UseApp(func(s store.AppState) string { return s.DefaultAgent })
	sessionAgent := store.UseSession(func(s store.SessionState) string { return s.Config.Agent })
	selectedTool, setSelectedTool := kitex.UseState[*api.Tool](nil)
	notice, setNotice := kitex.UseState("")
	isReloading, setIsReloading := kitex.UseState(false)

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
				count = len(*s)
			}
			setNotice(fmt.Sprintf("Workspace reloaded via registry.Reload (%d tools available).", count))
		}()
	}

	if sel := selectedTool(); sel != nil {
		return toolInspector(toolInspectorProps{
			slotID:  slotID,
			tool:    *sel,
			onClose: func() { setSelectedTool(nil) },
		})
	}

	if len(tools) == 0 {
		return emptyToolsView(emptyToolsViewProps{
			slotID:   slotID,
			notice:   notice(),
			onReload: doReload,
		})
	}

	title := fmt.Sprintf("Tools (%d available)", len(tools))
	if n := notice(); n != "" {
		title = fmt.Sprintf("Tools (%d available) — %s", len(tools), n)
	}

	scheme := theme.UseTheme()

	winSize := trail.UseWindowSize()
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
		for i := range tools {
			if tools[i].Name == opt.Value {
				setSelectedTool(&tools[i])
				return
			}
		}
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
		{Key: "r", Label: "Reload workspace"},
		{Key: "esc", Label: "Close"},
	}
	pickerProps.Keys = map[string]func(uicomponents.Option){
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
