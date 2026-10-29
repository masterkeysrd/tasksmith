package command

import (
	"context"
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
	if sk.Scope != "" {
		return sk.Scope
	}
	switch strings.ToLower(strings.TrimSpace(sk.Namespace)) {
	case "workspace":
		return "Workspace"
	case "local", "project":
		return "Project"
	case "user":
		return "User"
	case "system", "builtin", "":
		return "Builtin"
	default:
		s := strings.TrimSpace(sk.Namespace)
		if len(s) > 0 {
			return strings.ToUpper(s[:1]) + s[1:]
		}
		return "Builtin"
	}
}

// EstimateSkillTokens calculates approximate tokens for skill instructions using ⌈bytes / 4⌉.
func EstimateSkillTokens(instructions string) int {
	trimmed := strings.TrimSpace(instructions)
	if len(trimmed) == 0 {
		return 0
	}
	return (len(trimmed) + 3) / 4
}

type skillMarkdownProps struct {
	slotID  string
	skill   api.Skill
	onClose func()
}

var skillMarkdownViewer = trail.FC("SkillMarkdownViewer", func(props skillMarkdownProps) trail.Node {
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
			store.Send("/" + props.skill.Name)
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

	scope := SkillScope(props.skill)
	estTokens := EstimateSkillTokens(props.skill.Instructions)
	tokStr := format.FormatTokens(estTokens) + " tok"

	titleStyle := style.S().Bold(true).Foreground(scheme.Color.Text.Primary)
	scopeBadgeStyle := style.S().Bold(true).Foreground(scheme.Color.Icon.Primary)
	metaStyle := style.S().Margin(0, 0, 1, 0).Foreground(scheme.Color.Text.Secondary)
	descStyle := style.S().Margin(0, 0, 1, 0).Foreground(scheme.Color.Text.Secondary)

	var children []trail.Node

	headerItems := []trail.Node{
		trail.Span(trail.SpanProps{Style: titleStyle}, trail.Text(fmt.Sprintf("Skill: %s  ", props.skill.Name))),
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

	if props.skill.Description != "" {
		children = append(children, trail.Span(trail.SpanProps{Style: descStyle}, trail.Text(props.skill.Description)))
	}
	if props.skill.UseWhen != "" {
		children = append(children, trail.Span(trail.SpanProps{Style: metaStyle}, trail.Text("When to use: "+props.skill.UseWhen)))
	}
	if props.skill.ArgumentHint != "" {
		children = append(children, trail.Span(trail.SpanProps{Style: metaStyle}, trail.Text("Usage: "+props.skill.ArgumentHint)))
	}

	var mdContent trail.Node
	if strings.TrimSpace(props.skill.Instructions) != "" {
		mdContent = uicomponents.Markdown(uicomponents.MarkdownProps{
			Source: props.skill.Instructions,
		})
	} else {
		mdContent = trail.Span(trail.SpanProps{
			Style: style.S().Foreground(scheme.Color.Text.Muted),
		}, trail.Text("(no markdown runbook or instructions defined for this skill)"))
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

	children = append(children, components.KeyboardHint(components.KeyboardHintProps{
		Bindings: []components.KeyBinding{
			{Key: "↑/↓ or j/k", Label: "Scroll"},
			{Key: "pgup/pgdn", Label: "Page"},
			{Key: "esc", Label: "Return to skills"},
			{Key: "enter", Label: "Invoke skill"},
		},
	}))

	return trail.Box(trail.BoxProps{
		Style: style.S().
			Border(border).
			Width(style.Percent(100)).
			FlexDirection(style.FlexColumn),
	}, children...)
})

type emptySkillsViewProps struct {
	slotID   string
	notice   string
	onReload func()
}

var emptySkillsView = trail.FC("EmptySkillsView", func(props emptySkillsViewProps) trail.Node {
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
		trail.Span(trail.SpanProps{Style: titleStyle}, trail.Text("Skills (0 available)")),
		trail.Span(trail.SpanProps{Style: emptyStyle}, trail.Text("  No skills configured in workspace.")),
	)
	if props.notice != "" {
		children = append(children, trail.Span(trail.SpanProps{
			Style: style.S().Margin(0, 0, 1, 0).Foreground(scheme.Color.Text.Primary),
		}, trail.Text("  "+props.notice)))
	}
	children = append(children, components.KeyboardHint(components.KeyboardHintProps{
		Bindings: []components.KeyBinding{
			{Key: "r", Label: "Reload workspace"},
			{Key: "esc", Label: "Close"},
		},
	}))

	return trail.Box(trail.BoxProps{Style: boxStyle}, children...)
})

// SkillsBrowser renders an interactive skills list and markdown inspector inside <Slot />.
var SkillsBrowser = trail.FC("SkillsBrowser", func(slotID string) trail.Node {
	skillsPtr := store.UseApp(func(s store.AppState) *[]api.Skill { return s.Skills })
	selectedSkill, setSelectedSkill := kitex.UseState[*api.Skill](nil)
	notice, setNotice := kitex.UseState("")
	isReloading, setIsReloading := kitex.UseState(false)

	var skills []api.Skill
	if skillsPtr != nil {
		skills = *skillsPtr
	}

	// Fetch skills on mount if store has none yet
	kitex.UseEffect(func() {
		if skillsPtr == nil {
			if c := store.Client(); c != nil {
				go func() {
					if sks, err := c.ListSkills(context.Background()); err == nil {
						store.SetWorkspaceSkills(sks)
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
		setNotice("Reloading workspace skills via registry.Reload...")
		go func() {
			defer setIsReloading(false)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err := store.ReloadWorkspace(ctx)
			if err != nil {
				setNotice("Failed to reload workspace: " + err.Error())
			} else {
				count := 0
				if s := store.GetApp().Skills; s != nil {
					count = len(*s)
				}
				setNotice(fmt.Sprintf("Workspace reloaded via registry.Reload (%d skills available).", count))
			}
		}()
	}

	if sel := selectedSkill(); sel != nil {
		return skillMarkdownViewer(skillMarkdownProps{
			slotID:  slotID,
			skill:   *sel,
			onClose: func() { setSelectedSkill(nil) },
		})
	}

	if len(skills) == 0 {
		return emptySkillsView(emptySkillsViewProps{
			slotID:   slotID,
			notice:   notice(),
			onReload: doReload,
		})
	}

	title := fmt.Sprintf("Skills (%d available)", len(skills))
	if n := notice(); n != "" {
		title = fmt.Sprintf("Skills (%d available) — %s", len(skills), n)
	}

	scheme := theme.UseTheme()

	winSize := trail.UseWindowSize()
	availDescWidth := 0
	if winSize.Width > 0 {
		availDescWidth = max(20, winSize.Width-30-10-8-4) // 30 for name, 10 for scope, 8 for tokens, 4 for padding
	}

	opts := make([]uicomponents.Option, len(skills))
	for i, sk := range skills {
		scope := SkillScope(sk)
		estTokens := EstimateSkillTokens(sk.Instructions)
		tokStr := format.FormatTokens(estTokens)

		scopeCol := fmt.Sprintf("[%s]", scope)
		desc := format.CompactSingleLine(sk.Description, availDescWidth)

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
			Value:       sk.Name,
			Label:       sk.Name,
			Description: desc,
			Cells:       []string{sk.Name, scopeCol, tokStr, desc},
			CellNodes:   []trail.Node{nil, nil, nil, descNode},
		}
	}

	pickerProps := components.NewPicker(title, opts, func(opt uicomponents.Option) {
		for i := range skills {
			if skills[i].Name == opt.Value {
				setSelectedSkill(&skills[i])
				return
			}
		}
	})
	pickerProps.Columns = []uicomponents.Column{
		{Width: 30, Align: style.TextAlignLeft},
		{Width: 10, Align: style.TextAlignLeft},
		{Width: 8, Align: style.TextAlignRight},
		{Width: 0, Align: style.TextAlignLeft},
	}
	pickerProps.Filterable = false
	pickerProps.Bindings = []components.KeyBinding{
		{Key: "↑/↓", Label: "Select"},
		{Key: "enter", Label: "Inspect runbook"},
		{Key: "r", Label: "Reload workspace"},
		{Key: "esc", Label: "Close"},
	}
	pickerProps.Keys = map[string]func(uicomponents.Option){
		"r": func(_ uicomponents.Option) {
			doReload()
		},
		"i": func(opt uicomponents.Option) {
			for i := range skills {
				if skills[i].Name == opt.Value {
					setSelectedSkill(&skills[i])
					return
				}
			}
		},
	}
	pickerProps.OnCancel = func() {
		store.RemoveSlot(slotID)
		store.AppendCommandOutput("/skills", "Exited /skills command")
	}

	return components.Picker(pickerProps)
})
