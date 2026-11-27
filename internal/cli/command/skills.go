package command

import (
	"cmp"
	"context"
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
	"github.com/masterkeysrd/tasksmith/internal/core/tokenizer"
	uicomponents "github.com/masterkeysrd/tasksmith/internal/ui/components"
	"github.com/masterkeysrd/tasksmith/internal/ui/format"
	"github.com/masterkeysrd/tasksmith/internal/ui/styles"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

func (r *Registry) registerSkillCommands() {
	r.Register(Command{
		Name:        "skills",
		Usage:       "/skills",
		Description: "List, inspect, and reload workspace skills",
		Handler:     handleSkills,
	})
}

func handleSkills(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("/skills does not accept arguments (did you mean to invoke /%s?)", strings.TrimPrefix(args[0], "/"))
	}
	store.EnqueueSlot(store.SlotItem{
		Kind: store.SlotKindPicker,
		Data: SkillsBrowser,
	})
	return nil
}

// SkillScope returns the canonical scope string for a skill:
// [Workspace], [Project], [User], or [Builtin] (System and Builtin are treated as Builtin).
func SkillScope(sk api.Skill) string {
	return resourceScope(sk.Scope, sk.Namespace)
}

func resourceScope(scope, namespace string) string {
	if scope != "" {
		return scope
	}
	namespace = strings.TrimSpace(namespace)
	switch strings.ToLower(namespace) {
	case "workspace":
		return "Workspace"
	case "local", "project":
		return "Project"
	case "user":
		return "User"
	case "system", "builtin", "":
		return "Builtin"
	default:
		return strings.ToUpper(namespace[:1]) + namespace[1:]
	}
}

// EstimateSkillTokens calculates approximate tokens for skill instructions using
// the code-calibrated tokenizer density (⌈bytes / 3⌉).
func EstimateSkillTokens(instructions string) int {
	return tokenizer.Estimate(strings.TrimSpace(instructions))
}

func resourceTokens(tokens int, instructions string) string {
	if tokens == 0 {
		tokens = tokenizer.Estimate(strings.TrimSpace(instructions))
	}
	return format.Tokens(tokens)
}

type resourceInspectorProps struct {
	title, scope, tokens, description string
	instructions, emptyText           string
	backLabel, actionLabel            string
	metadata                          []uicomponents.KVItem
	onClose, onAction                 func()
}

var resourceInspector = trail.FC("ResourceInspector", func(p resourceInspectorProps) trail.Node {
	st := styles.UseCommon()
	return uicomponents.ScrollViewer(uicomponents.ScrollViewerProps{
		OuterStyle: &st.Panel,
		Header: []trail.Node{
			trail.Span(trail.SpanProps{Style: st.Title}, trail.Text(p.title+"  ")),
			uicomponents.Badge(uicomponents.BadgeProps{Label: "[" + p.scope + "]", Tone: uicomponents.ToneAccent}),
			trail.Span(trail.SpanProps{Style: st.Secondary}, trail.Text("  (est. "+p.tokens+" tok)")),
		},
		HeaderMarginBottom: 1,
		Meta: []trail.Node{
			kitex.If(p.description != "", func() trail.Node {
				return trail.Span(trail.SpanProps{Style: st.Secondary}, trail.Text(p.description))
			}),
			uicomponents.KVList(uicomponents.KVListProps{Items: p.metadata}),
		},
		Markdown:  p.instructions,
		EmptyText: p.emptyText,
		OnClose:   p.onClose,
		Actions:   []uicomponents.ScrollViewerAction{{Keys: []string{"enter"}, Handler: p.onAction}},
		Bindings: []uicomponents.KeyBinding{
			{Key: "↑/↓ or j/k", Label: "Scroll"},
			{Key: "pgup/pgdn", Label: "Page"},
			{Key: "esc", Label: p.backLabel},
			{Key: "enter", Label: p.actionLabel},
		},
	})
})

type skillMarkdownProps struct {
	slotID  string
	skill   api.Skill
	onClose func()
}

var skillMarkdownViewer = trail.FC("SkillMarkdownViewer", func(props skillMarkdownProps) trail.Node {
	sk := props.skill
	return resourceInspector(resourceInspectorProps{
		title:       "Skill: " + sk.Name,
		scope:       SkillScope(sk),
		tokens:      resourceTokens(sk.Tokens, sk.Instructions),
		description: sk.Description,
		metadata: []uicomponents.KVItem{
			{Label: "When to use", Value: sk.UseWhen, OmitEmpty: true},
			{Label: "Usage", Value: sk.ArgumentHint, OmitEmpty: true},
		},
		instructions: sk.Instructions,
		emptyText:    "(no markdown runbook or instructions defined for this skill)",
		backLabel:    "Return to skills",
		actionLabel:  "Invoke skill",
		onClose:      props.onClose,
		onAction: func() {
			store.RemoveSlot(props.slotID)
			store.Send("/" + sk.Name)
		},
	})
})

type resourceReload struct{ notice, failure string }

func useWorkspaceReload[T any](kind string, selectRecords func(store.AppState) *[]T) (resourceReload, func()) {
	status, setStatus := kitex.UseState(resourceReload{})
	busy, setBusy := kitex.UseState(false)
	return status(), func() {
		if busy() {
			return
		}
		setBusy(true)
		setStatus(resourceReload{notice: "Reloading workspace " + kind + " via registry.Reload..."})
		go func() {
			defer setBusy(false)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := store.ReloadWorkspace(ctx); err != nil {
				setStatus(resourceReload{failure: "Failed to reload workspace: " + err.Error()})
				return
			}
			count := 0
			if records := selectRecords(store.GetApp()); records != nil {
				count = len(*records)
			}
			setStatus(resourceReload{notice: fmt.Sprintf("Workspace reloaded via registry.Reload (%d %s available).", count, kind)})
		}()
	}
}

type resourceListProps struct {
	slotID, title, inspectLabel string
	options                     []uicomponents.Option
	columns                     []uicomponents.Column
	reload                      resourceReload
	onReload                    func()
	detail                      func(string) flight.Route
}

var resourceList = trail.FC("ResourceList", func(p resourceListProps) trail.Node {
	nav := flight.UseNavigation()
	st := styles.UseCommon()
	kind := strings.ToLower(p.title)
	title := fmt.Sprintf("%s (%d available)", p.title, len(p.options))
	closeSlot := func() { store.RemoveSlot(p.slotID) }
	bindings := uicomponents.UseKeyMap([]uicomponents.KeyAction{
		{Keys: []string{"r"}, Label: "Reload workspace", OnPress: p.onReload},
		{Keys: []string{"esc", "escape"}, Label: "Close", OnPress: closeSlot},
	}, len(p.options) == 0)
	if len(p.options) == 0 {
		return uicomponents.Panel(uicomponents.PanelProps{
			Title: title, Notice: p.reload.notice, Error: p.reload.failure, Bindings: bindings,
		}, trail.Span(trail.SpanProps{Style: st.Secondary}, trail.Text("  No "+kind+" configured in workspace.")))
	}
	if message := cmp.Or(p.reload.failure, p.reload.notice); message != "" {
		title += " — " + message
	}
	inspect := func(opt uicomponents.Option) { nav.Push(p.detail(opt.Value)) }
	picker := components.NewPicker(title, p.options, inspect)
	picker.Filterable = false
	picker.Columns = p.columns
	picker.Bindings = []uicomponents.KeyBinding{
		{Key: "↑/↓", Label: "Select"},
		{Key: "enter/i", Label: p.inspectLabel},
		{Key: "r", Label: "Reload workspace"},
		{Key: "esc", Label: "Close"},
	}
	picker.Keys = map[string]func(uicomponents.Option){
		"r": func(uicomponents.Option) { p.onReload() },
		"i": inspect,
	}
	picker.OnCancel = func() {
		closeSlot()
		store.AppendCommandOutput("/"+kind, "Exited /"+kind+" command")
	}
	return components.Picker(picker)
})

type resourceDescriptionStyles struct{ row, text style.Style }

func newResourceDescriptionStyles(s *theme.Scheme) resourceDescriptionStyles {
	common := styles.NewCommon(s)
	return resourceDescriptionStyles{
		row: common.Row.
			Width(style.Percent(100)).
			MinWidth(style.Cells(0)).
			Overflow(style.OverflowHidden),
		text: common.Muted.
			Flex(0, 1).
			MinWidth(style.Cells(0)).
			Overflow(style.OverflowHidden).
			WhiteSpace(style.WhiteSpacePre),
	}
}

var resourceDescription = trail.FC("ResourceDescription", func(text string) trail.Node {
	st := styles.Use(newResourceDescriptionStyles)
	return trail.Box(trail.BoxProps{Style: st.row}, trail.Span(trail.SpanProps{Style: st.text}, trail.Text(text)))
})

func resourceDescriptionWidth(width, reserved int) int {
	if width == 0 {
		return 0
	}
	return max(20, width-reserved)
}

var missingResource = trail.FC("MissingResource", func(kind string) trail.Node {
	nav := flight.UseNavigation()
	bindings := uicomponents.UseKeyMap([]uicomponents.KeyAction{
		{Keys: []string{"esc", "escape"}, Label: "Return to " + strings.ToLower(kind) + "s", OnPress: nav.Pop},
	}, true)
	return uicomponents.Panel(uicomponents.PanelProps{Notice: kind + " no longer available.", Bindings: bindings})
})

type skillsListRoute struct{}

type skillDetailRoute struct{ Name string }

type resourceDetailProps struct{ slotID, name string }

var skillDetail = trail.FC("SkillDetail", func(p resourceDetailProps) trail.Node {
	nav := flight.UseNavigation()
	if records := store.UseApp(selectSkills); records != nil {
		for _, sk := range *records {
			if sk.Name == p.name {
				return skillMarkdownViewer(skillMarkdownProps{slotID: p.slotID, skill: sk, onClose: nav.Pop})
			}
		}
	}
	return missingResource("Skill")
})

func selectSkills(s store.AppState) *[]api.Skill { return s.Skills }

func skillOptions(records *[]api.Skill, width int) []uicomponents.Option {
	if records == nil {
		return nil
	}
	options := make([]uicomponents.Option, 0, len(*records))
	for _, sk := range *records {
		desc := format.CompactSingleLine(sk.Description, width)
		options = append(options, uicomponents.Option{
			Value: sk.Name, Label: sk.Name, Description: desc,
			Cells:     []string{sk.Name, "[" + SkillScope(sk) + "]", resourceTokens(sk.Tokens, sk.Instructions), desc},
			CellNodes: []trail.Node{nil, nil, nil, resourceDescription(desc)},
		})
	}
	return options
}

// SkillsBrowser renders an interactive skills list and markdown inspector inside <Slot />.
var SkillsBrowser = trail.FC("SkillsBrowser", func(slotID string) trail.Node {
	records := store.UseApp(selectSkills)
	reload, onReload := useWorkspaceReload("skills", selectSkills)
	width := resourceDescriptionWidth(trail.UseWindowSize().Width, 30+10+8+4)
	c := store.Client()
	kitex.UseEffect(func() {
		if records == nil && c != nil {
			go func() {
				if skills, err := c.ListSkills(context.Background()); err == nil {
					store.SetWorkspaceSkills(skills)
				}
			}()
		}
	}, []any{records == nil, c == nil})

	return flight.Stack(flight.StackProps{
		InitialRoute: skillsListRoute{},
		RenderRoute: func(route flight.Route) trail.Node {
			if detail, ok := route.(skillDetailRoute); ok {
				return skillDetail(resourceDetailProps{slotID: slotID, name: detail.Name})
			}
			return resourceList(resourceListProps{
				slotID:       slotID,
				title:        "Skills",
				inspectLabel: "Inspect runbook",
				options:      skillOptions(records, width),
				columns: []uicomponents.Column{
					{Width: 30},
					{Width: 10},
					{Width: 8, Align: style.TextAlignRight},
					{},
				},
				reload:   reload,
				onReload: onReload,
				detail:   func(name string) flight.Route { return skillDetailRoute{Name: name} },
			})
		},
	})
})
