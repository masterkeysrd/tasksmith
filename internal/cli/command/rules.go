package command

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/masterkeysrd/kite/extras/flight"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	"github.com/masterkeysrd/tasksmith/internal/core/tokenizer"
	uicomponents "github.com/masterkeysrd/tasksmith/internal/ui/components"
	"github.com/masterkeysrd/tasksmith/internal/ui/format"
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
	return resourceScope(r.Scope, r.Namespace)
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
	rule := props.rule
	priority := ""
	if rule.Priority != 0 {
		priority = strconv.Itoa(rule.Priority)
	}
	return resourceInspector(resourceInspectorProps{
		title:       "Rule: " + rule.Name,
		scope:       RuleScope(rule),
		tokens:      resourceTokens(rule.Tokens, rule.Instructions),
		description: rule.Description,
		metadata: []uicomponents.KVItem{
			{Label: "Priority", Value: priority, OmitEmpty: true},
			{Label: "Triggers", Value: ruleTriggersSummary(rule)},
			{Label: "When to use", Value: rule.Triggers.UseWhen, OmitEmpty: true},
		},
		instructions: rule.Instructions,
		emptyText:    "(no instructions defined for this rule)",
		backLabel:    "Return to rules",
		actionLabel:  "Attach rule",
		onClose:      props.onClose,
		onAction: func() {
			store.RemoveSlot(props.slotID)
			store.Send("@rule:" + rule.Name + " ")
		},
	})
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

type rulesListRoute struct{}

type ruleDetailRoute struct{ Name string }

var ruleDetail = trail.FC("RuleDetail", func(p resourceDetailProps) trail.Node {
	nav := flight.UseNavigation()
	if records := store.UseApp(selectRules); records != nil {
		for _, rule := range *records {
			if rule.Name == p.name {
				return ruleMarkdownViewer(ruleMarkdownProps{slotID: p.slotID, rule: rule, onClose: nav.Pop})
			}
		}
	}
	return missingResource("Rule")
})

func selectRules(s store.AppState) *[]api.Rule { return s.Rules }

func ruleOptions(records *[]api.Rule, width int, loaded map[string]bool) []uicomponents.Option {
	if records == nil {
		return nil
	}
	options := make([]uicomponents.Option, 0, len(*records))
	for _, rule := range *records {
		desc := format.CompactSingleLine(rule.Description, width)
		tokens := resourceTokens(rule.Tokens, rule.Instructions)
		status := ruleStatusBadge(rule, loaded)
		tone := uicomponents.ToneSuccess
		if status == "[Standby]" {
			tone = uicomponents.ToneMuted
		}
		badge := uicomponents.Badge(uicomponents.BadgeProps{Label: status, Tone: tone})
		options = append(options, uicomponents.Option{
			Value: rule.Name, Label: rule.Name, Description: desc,
			Cells:     []string{rule.Name, status, "[" + RuleScope(rule) + "]", tokens, desc},
			CellNodes: []trail.Node{nil, badge, nil, nil, resourceDescription(desc)},
		})
	}
	return options
}

// RulesBrowser renders an interactive rules list and markdown inspector inside <Slot />.
var RulesBrowser = trail.FC("RulesBrowser", func(slotID string) trail.Node {
	records := store.UseApp(selectRules)
	transcriptItems := store.UseTranscript(func(s store.TranscriptState) *[]store.TranscriptItem { return s.Items })
	streamMessages := store.UseStream(func(s store.StreamState) *[]store.Message { return s.Messages })
	reload, onReload := useWorkspaceReload("rules", selectRules)
	width := resourceDescriptionWidth(trail.UseWindowSize().Width, 30+12+10+8+4)
	c := store.Client()
	kitex.UseEffect(func() {
		if records == nil && c != nil {
			go func() {
				if rules, err := c.ListRules(context.Background()); err == nil {
					store.SetWorkspaceRules(rules)
				}
			}()
		}
	}, []any{records == nil, c == nil})

	return flight.Stack(flight.StackProps{
		InitialRoute: rulesListRoute{},
		RenderRoute: func(route flight.Route) trail.Node {
			if detail, ok := route.(ruleDetailRoute); ok {
				return ruleDetail(resourceDetailProps{slotID: slotID, name: detail.Name})
			}
			loaded := loadedRuleNamesFromSession(transcriptItems, streamMessages)
			return resourceList(resourceListProps{
				slotID:       slotID,
				title:        "Rules",
				inspectLabel: "Inspect rule",
				options:      ruleOptions(records, width, loaded),
				columns: []uicomponents.Column{
					{Width: 30},
					{Width: 12},
					{Width: 10},
					{Width: 8, Align: style.TextAlignRight},
					{},
				},
				reload:   reload,
				onReload: onReload,
				detail:   func(name string) flight.Route { return ruleDetailRoute{Name: name} },
			})
		},
	})
})
