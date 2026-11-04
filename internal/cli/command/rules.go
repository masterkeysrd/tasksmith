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
	"github.com/masterkeysrd/tasksmith/internal/core/tokenizer"
	uicomponents "github.com/masterkeysrd/tasksmith/internal/ui/components"
	"github.com/masterkeysrd/tasksmith/internal/ui/format"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

func (r *Registry) registerRuleCommands() {
	r.Register(Command{
		Name:        "rules",
		Usage:       "/rules",
		Description: "List, inspect, and reload workspace rules",
		Handler:     handleRules,
	})
}

func handleRules(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("/rules does not accept arguments (did you mean to invoke /%s?)", strings.TrimPrefix(args[0], "/"))
	}
	store.EnqueueSlot(store.SlotItem{
		Kind: store.SlotKindPicker,
		Data: RulesBrowser,
	})
	return nil
}

// RuleScope returns the canonical scope string for a rule:
// [Workspace], [Project], [User], or [Builtin] (System and Builtin are treated as Builtin).
func RuleScope(r api.Rule) string {
	if r.Scope != "" {
		return r.Scope
	}
	switch strings.ToLower(strings.TrimSpace(r.Namespace)) {
	case "workspace":
		return "Workspace"
	case "local", "project":
		return "Project"
	case "user":
		return "User"
	case "system", "builtin", "":
		return "Builtin"
	default:
		s := strings.TrimSpace(r.Namespace)
		if len(s) > 0 {
			return strings.ToUpper(s[:1]) + s[1:]
		}
		return "Builtin"
	}
}

// EstimateRuleTokens calculates approximate tokens for rule instructions using
// the code-calibrated tokenizer density (⌈bytes / 3⌉).
func EstimateRuleTokens(instructions string) int {
	return tokenizer.Estimate(strings.TrimSpace(instructions))
}

// ruleStatusBadge returns the status badge for a rule: [Always] for always-active
// rules, [Loaded] when the rule is active in the session, otherwise [Standby].
func ruleStatusBadge(rule api.Rule, loaded map[string]bool) string {
	if rule.Triggers.Always {
		return "[Always]"
	}
	if loaded[strings.ToLower(rule.Name)] {
		return "[Loaded]"
	}
	return "[Standby]"
}

// ruleTriggersSummary renders a compact summary of a rule's trigger conditions.
func ruleTriggersSummary(rule api.Rule) string {
	var parts []string
	if rule.Triggers.Always {
		parts = append(parts, "always")
	}
	if len(rule.Triggers.Globs) > 0 {
		parts = append(parts, "globs")
	}
	if len(rule.Triggers.Tools) > 0 {
		parts = append(parts, "tools")
	}
	if len(rule.Triggers.Keywords) > 0 {
		parts = append(parts, "keywords")
	}
	if len(parts) == 0 {
		return "—"
	}
	return strings.Join(parts, ", ")
}

type ruleMarkdownProps struct {
	slotID  string
	rule    api.Rule
	onClose func()
}

var ruleMarkdownViewer = trail.FC("RuleMarkdownViewer", func(props ruleMarkdownProps) trail.Node {
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
		case k.MatchString("enter"):
			store.RemoveSlot(props.slotID)
			store.Send("@rule:" + props.rule.Name + " ")
		case k.MatchString("up", "k"):
			cur := scrollPos()
			if cur > 0 {
				setScrollPos(cur - 1)
			}
		case k.MatchString("down", "j"):
			setScrollPos(scrollPos() + 1)
		case k.MatchString("pgup", "b", "ctrl+u"):
			cur := scrollPos()
			if cur > 8 {
				setScrollPos(cur - 8)
			} else {
				setScrollPos(0)
			}
		case k.MatchString("pgdown", "space", "f", "ctrl+d"):
			setScrollPos(scrollPos() + 8)
		case k.MatchString("home", "g"):
			setScrollPos(0)
		case k.MatchString("end", "G"):
			setScrollPos(9999)
		}
	})

	scheme := theme.UseTheme()
	border := style.SingleBorder().Top(true).Bottom(false).Left(false).Right(false).Color(scheme.Color.Border.Default)

	scope := RuleScope(props.rule)
	estTokens := props.rule.Tokens
	if estTokens == 0 && props.rule.Instructions != "" {
		estTokens = EstimateRuleTokens(props.rule.Instructions)
	}
	tokStr := format.Tokens(estTokens) + " tok"

	titleStyle := style.S().Bold(true).Foreground(scheme.Color.Text.Primary)
	scopeBadgeStyle := style.S().Bold(true).Foreground(scheme.Color.Icon.Primary)
	metaStyle := style.S().Margin(0, 0, 1, 0).Foreground(scheme.Color.Text.Secondary)
	descStyle := style.S().Margin(0, 0, 1, 0).Foreground(scheme.Color.Text.Secondary)

	var children []trail.Node

	headerItems := []trail.Node{
		trail.Span(trail.SpanProps{Style: titleStyle}, trail.Text(fmt.Sprintf("Rule: %s  ", props.rule.Name))),
		trail.Span(trail.SpanProps{Style: scopeBadgeStyle}, trail.Text(fmt.Sprintf("[%s]  ", scope))),
		trail.Span(trail.SpanProps{Style: descStyle}, trail.Text(fmt.Sprintf("(est. %s)", tokStr))),
	}
	if cur := scrollPos(); cur > 0 {
		headerItems = append(headerItems, trail.Span(trail.SpanProps{Style: descStyle}, trail.Text(fmt.Sprintf("  •  line +%d", cur))))
	}
	headerLine := trail.Box(trail.BoxProps{
		Style: style.S().FlexDirection(style.FlexRow).Margin(0, 0, 1, 0),
	}, headerItems...)
	children = append(children, headerLine)

	if props.rule.Description != "" {
		children = append(children, trail.Span(trail.SpanProps{Style: descStyle}, trail.Text(props.rule.Description)))
	}
	if props.rule.Priority != 0 {
		children = append(children, trail.Span(trail.SpanProps{Style: metaStyle}, trail.Text(fmt.Sprintf("Priority: %d", props.rule.Priority))))
	}
	children = append(children, trail.Span(trail.SpanProps{Style: metaStyle}, trail.Text("Triggers: "+ruleTriggersSummary(props.rule))))
	if props.rule.Triggers.UseWhen != "" {
		children = append(children, trail.Span(trail.SpanProps{Style: metaStyle}, trail.Text("When to use: "+props.rule.Triggers.UseWhen)))
	}

	var mdContent trail.Node
	if strings.TrimSpace(props.rule.Instructions) != "" {
		mdContent = uicomponents.Markdown(uicomponents.MarkdownProps{
			Source: props.rule.Instructions,
		})
	} else {
		mdContent = trail.Span(trail.SpanProps{
			Style: style.S().Foreground(scheme.Color.Text.Muted),
		}, trail.Text("(no instructions defined for this rule)"))
	}

	scrollBoxStyle := style.S().
		Width(style.Percent(100)).
		MaxHeight(style.Cells(18)).
		OverflowY(style.OverflowAuto).
		ScrollbarY(true)

	children = append(children, trail.Box(trail.BoxProps{
		Ref:   scrollBoxRef,
		Style: scrollBoxStyle,
	}, mdContent))

	children = append(children, uicomponents.KeyboardHint(uicomponents.KeyboardHintProps{
		Bindings: []uicomponents.KeyBinding{
			{Key: "↑/↓ or j/k", Label: "Scroll"},
			{Key: "pgup/pgdn", Label: "Page"},
			{Key: "esc", Label: "Return to rules"},
			{Key: "enter", Label: "Attach rule"},
		},
	}))

	return trail.Box(trail.BoxProps{
		Style: style.S().
			Border(border).
			Width(style.Percent(100)).
			FlexDirection(style.FlexColumn),
	}, children...)
})

type emptyRulesViewProps struct {
	slotID   string
	notice   string
	onReload func()
}

var emptyRulesView = trail.FC("EmptyRulesView", func(props emptyRulesViewProps) trail.Node {
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
		trail.Span(trail.SpanProps{Style: titleStyle}, trail.Text("Rules (0 available)")),
		trail.Span(trail.SpanProps{Style: emptyStyle}, trail.Text("  No rules configured in workspace.")),
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

// loadedRuleNamesFromSession collects the rules active in the current session:
// rules surfaced by TimelineKindRule transcript entries and rules loaded on
// demand via successful load_rule tool calls. Keys are lower-cased rule names.
func loadedRuleNamesFromSession(items *[]store.TranscriptItem, messages *[]store.Message) map[string]bool {
	loaded := make(map[string]bool)
	if items != nil {
		for _, item := range *items {
			switch data := item.Data.(type) {
			case api.TimelineEntry:
				if data.Kind == api.TimelineKindRule && data.Rule != nil {
					loaded[strings.ToLower(data.Rule.Rule)] = true
				}
			case store.Message:
				addLoadRuleParts(loaded, data.Parts)
			}
		}
	}
	if messages != nil {
		for _, msg := range *messages {
			addLoadRuleParts(loaded, msg.Parts)
		}
	}
	return loaded
}

// addLoadRuleParts records rules loaded via successful load_rule tool calls.
func addLoadRuleParts(loaded map[string]bool, parts []store.MessagePart) {
	for _, part := range parts {
		if part.Type != store.PartTool || part.Tool == nil {
			continue
		}
		tool := *part.Tool
		if tool.Call == nil || tool.Call.Name != "load_rule" {
			continue
		}
		if tool.Response != nil && !tool.Response.IsError && len(tool.Response.StructuredContent) > 0 {
			var out api.LoadRuleOutput
			if err := json.Unmarshal(tool.Response.StructuredContent, &out); err == nil && strings.TrimSpace(out.Rule) != "" {
				loaded[strings.ToLower(strings.TrimSpace(out.Rule))] = true
				continue
			}
		}
		if tool.Status == store.ToolSucceeded && tool.Call.Args != nil {
			if rule, ok := tool.Call.Args["rule"].(string); ok && strings.TrimSpace(rule) != "" {
				loaded[strings.ToLower(strings.TrimSpace(rule))] = true
			}
		}
	}
}

// RulesBrowser renders an interactive rules list and markdown inspector inside <Slot />.
var RulesBrowser = trail.FC("RulesBrowser", func(slotID string) trail.Node {
	rulesPtr := store.UseApp(func(s store.AppState) *[]api.Rule { return s.Rules })
	transcriptItems := store.UseTranscript(func(s store.TranscriptState) *[]store.TranscriptItem { return s.Items })
	streamMessages := store.UseStream(func(s store.StreamState) *[]store.Message { return s.Messages })
	selectedRule, setSelectedRule := kitex.UseState[*api.Rule](nil)
	notice, setNotice := kitex.UseState("")
	isReloading, setIsReloading := kitex.UseState(false)

	var rules []api.Rule
	if rulesPtr != nil {
		rules = *rulesPtr
	}

	// Fetch rules on mount if store has none yet
	kitex.UseEffect(func() {
		if rulesPtr == nil {
			if c := store.Client(); c != nil {
				go func() {
					if rls, err := c.ListRules(context.Background()); err == nil {
						store.SetWorkspaceRules(rls)
					}
				}()
			}
		}
	}, nil)

	doReload := func() {
		if isReloading() {
			return
		}
		setIsReloading(true)
		setNotice("Reloading workspace rules via registry.Reload...")
		go func() {
			defer setIsReloading(false)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err := store.ReloadWorkspace(ctx)
			if err != nil {
				setNotice("Failed to reload workspace: " + err.Error())
			} else {
				count := 0
				if s := store.GetApp().Rules; s != nil {
					count = len(*s)
				}
				setNotice(fmt.Sprintf("Workspace reloaded via registry.Reload (%d rules available).", count))
			}
		}()
	}

	if sel := selectedRule(); sel != nil {
		return ruleMarkdownViewer(ruleMarkdownProps{
			slotID:  slotID,
			rule:    *sel,
			onClose: func() { setSelectedRule(nil) },
		})
	}

	if len(rules) == 0 {
		return emptyRulesView(emptyRulesViewProps{
			slotID:   slotID,
			notice:   notice(),
			onReload: doReload,
		})
	}

	title := fmt.Sprintf("Rules (%d available)", len(rules))
	if n := notice(); n != "" {
		title = fmt.Sprintf("Rules (%d available) — %s", len(rules), n)
	}

	scheme := theme.UseTheme()

	winSize := trail.UseWindowSize()
	availDescWidth := 0
	if winSize.Width > 0 {
		availDescWidth = max(20, winSize.Width-30-12-10-8-4) // 30 for name, 12 for status, 10 for scope, 8 for tokens, 4 for padding
	}

	loaded := loadedRuleNamesFromSession(transcriptItems, streamMessages)

	opts := make([]uicomponents.Option, len(rules))
	for i, rl := range rules {
		scope := RuleScope(rl)
		estTokens := rl.Tokens
		if estTokens == 0 && rl.Instructions != "" {
			estTokens = EstimateRuleTokens(rl.Instructions)
		}
		tokStr := format.Tokens(estTokens)

		statusCol := ruleStatusBadge(rl, loaded)
		scopeCol := fmt.Sprintf("[%s]", scope)
		desc := format.CompactSingleLine(rl.Description, availDescWidth)

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
			Value:       rl.Name,
			Label:       rl.Name,
			Description: desc,
			Cells:       []string{rl.Name, statusCol, scopeCol, tokStr, desc},
			CellNodes:   []trail.Node{nil, nil, nil, nil, descNode},
		}
	}

	pickerProps := components.NewPicker(title, opts, func(opt uicomponents.Option) {
		for i := range rules {
			if rules[i].Name == opt.Value {
				setSelectedRule(&rules[i])
				return
			}
		}
	})
	pickerProps.Columns = []uicomponents.Column{
		{Width: 30, Align: style.TextAlignLeft},
		{Width: 12, Align: style.TextAlignLeft},
		{Width: 10, Align: style.TextAlignLeft},
		{Width: 8, Align: style.TextAlignRight},
		{Width: 0, Align: style.TextAlignLeft},
	}
	pickerProps.Filterable = false
	pickerProps.Bindings = []uicomponents.KeyBinding{
		{Key: "↑/↓", Label: "Select"},
		{Key: "enter", Label: "Inspect rule"},
		{Key: "r", Label: "Reload workspace"},
		{Key: "esc", Label: "Close"},
	}
	pickerProps.Keys = map[string]func(uicomponents.Option){
		"r": func(_ uicomponents.Option) {
			doReload()
		},
		"i": func(opt uicomponents.Option) {
			for i := range rules {
				if rules[i].Name == opt.Value {
					setSelectedRule(&rules[i])
					return
				}
			}
		},
	}
	pickerProps.OnCancel = func() {
		store.RemoveSlot(slotID)
		store.AppendCommandOutput("/rules", "Exited /rules command")
	}

	return components.Picker(pickerProps)
})
