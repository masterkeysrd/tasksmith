package components

import (
	"cmp"
	"fmt"
	"strings"

	"github.com/masterkeysrd/kite/dom"
	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	uicomponents "github.com/masterkeysrd/tasksmith/internal/ui/components"
	"github.com/masterkeysrd/tasksmith/internal/ui/styles"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

// PermissionConfigEditorProps defines the properties for the PermissionConfigEditor component.
type PermissionConfigEditorProps struct {
	Permissions  api.PermissionsResponse
	ErrorMessage string
	OnAddRule    func(scope api.PermissionScope, effect api.PermissionEffect, pattern string)
	OnRemoveRule func(scope api.PermissionScope, effect api.PermissionEffect, pattern string)
	OnEditRule   func(scope api.PermissionScope, effect api.PermissionEffect, oldPattern, newPattern string)
	OnClose      func()
	OnClearError func()
}

type editorStep int

const (
	stepScope editorStep = iota
	stepRules
	stepInput
)

type permissionEditorStyles struct {
	styles.Common
	selected, normal style.Style
}

func newPermissionEditorStyles(s *theme.Scheme) permissionEditorStyles {
	c := styles.NewCommon(s)
	return permissionEditorStyles{
		Common:   c,
		selected: c.Title.WhiteSpace(style.WhiteSpacePre),
		normal:   c.Secondary.WhiteSpace(style.WhiteSpacePre),
	}
}

func (s permissionEditorStyles) list(items []string, cursor int) trail.Node {
	return kitex.Map(items, func(item string, i int) trail.Node {
		face, prefix := s.normal, "  "
		if i == cursor {
			face, prefix = s.selected, "❯ "
		}
		return trail.Box(trail.BoxProps{Style: s.Row}, trail.Span(trail.SpanProps{Style: face}, trail.Text(prefix+item)))
	})
}

// PermissionConfigEditor is a 3-screen interactive permission rule configuration manager.
var PermissionConfigEditor = trail.FC("PermissionConfigEditor", func(props PermissionConfigEditorProps) trail.Node {
	sheet := styles.Use(newPermissionEditorStyles)

	step, setStep := kitex.UseState(stepScope)
	scopeIndex, setScopeIndex := kitex.UseState(0) // 0: Project, 1: Global
	tab, setTab := kitex.UseState(0)

	isEdit, setIsEdit := kitex.UseState(false)
	editTarget, setEditTarget := kitex.UseState("")
	inputText, setInputText := kitex.UseState("")
	localError, setLocalError := kitex.UseState("")

	inputRef := kitex.UseRef[dom.Element](nil)
	trail.UseFocus(inputRef, trail.FocusOptions{
		AutoFocus: true,
		IsActive:  step() == stepInput,
	})

	// Resolve active scope
	activeScope := api.ScopeProject
	scopeTitle := "Project"
	if scopeIndex() == 1 {
		activeScope = api.ScopeGlobal
		scopeTitle = "Global"
	}

	scopePerms := props.Permissions.Project
	if activeScope == api.ScopeGlobal {
		scopePerms = props.Permissions.Global
	}

	rules, effect, listName := scopePerms.Allow, api.EffectAllow, "allowlist"
	if tab() == 1 {
		rules, effect, listName = scopePerms.Deny, api.EffectDeny, "denylist"
	}
	cursor := uicomponents.UseCursor(len(rules), 0)
	clearError := func() {
		setLocalError("")
		if props.OnClearError != nil {
			props.OnClearError()
		}
	}
	switchTab := func(i int) {
		setTab(i)
		cursor.Set(0)
		clearError()
	}
	openInput := func(edit bool) {
		pattern := ""
		if edit {
			pattern = rules[cursor.Index]
		}
		setIsEdit(edit)
		setEditTarget(pattern)
		setInputText(pattern)
		clearError()
		setStep(stepInput)
	}
	tabs := []uicomponents.TabItem{
		{Label: fmt.Sprintf("allowlist (%d)", len(scopePerms.Allow))},
		{Label: fmt.Sprintf("denylist (%d)", len(scopePerms.Deny))},
	}
	var actions []uicomponents.KeyAction
	switch step() {
	case stepScope:
		actions = []uicomponents.KeyAction{
			{Keys: []string{"up", "k", "ctrl+p"}, Hint: "↑/↓", Label: "Navigate", OnPress: func() { setScopeIndex(0) }},
			{Keys: []string{"down", "j", "ctrl+n"}, OnPress: func() { setScopeIndex(1) }},
			{Keys: []string{"enter"}, Label: "Select", OnPress: func() {
				setStep(stepRules)
				cursor.Set(0)
				clearError()
			}},
			{Keys: []string{"esc", "escape"}, Label: "Close", OnPress: props.OnClose},
		}
	case stepRules:
		actions = uicomponents.TabKeyActions(len(tabs), tab(), switchTab, false)
		actions[0].Keys = append(actions[0].Keys, "right", "l")
		actions[0].Hint = "←/→"
		actions[0].Label = "Switch View"
		actions[1].Keys = append(actions[1].Keys, "left", "h")
		actions = append(actions, []uicomponents.KeyAction{
			{Keys: []string{"up", "k", "ctrl+p"}, Hint: "↑/↓", Label: "Navigate", OnPress: func() { cursor.Move(-1) }},
			{Keys: []string{"down", "j", "ctrl+n"}, OnPress: func() { cursor.Move(1) }},
			{Keys: []string{"a", "A"}, Label: "Add rule", OnPress: func() { openInput(false) }},
			{Keys: []string{"e", "E"}, Label: "Edit rule", Disabled: len(rules) == 0, OnPress: func() { openInput(true) }},
			{Keys: []string{"d", "D", "backspace", "delete"}, Hint: "d/⌫", Label: "Delete rule", Disabled: len(rules) == 0, OnPress: func() {
				if props.OnRemoveRule != nil {
					props.OnRemoveRule(activeScope, effect, rules[cursor.Index])
				}
			}},
			{Keys: []string{"esc", "escape"}, Label: "Back", OnPress: func() {
				setStep(stepScope)
				clearError()
			}},
		}...)
	}
	bindings := uicomponents.UseKeyMap(actions, step() != stepInput)
	switch step() {
	case stepScope:
		notice := "Rules that apply only to this project (highest priority)"
		if scopeIndex() == 1 {
			notice = "Rules that apply across all projects"
		}
		return uicomponents.Panel(
			uicomponents.PanelProps{
				Title:    "Permission Config Editor",
				Notice:   notice,
				Bindings: bindings,
			},
			trail.Span(trail.SpanProps{Style: sheet.Secondary}, trail.Text("Select a config scope to edit:")),
			sheet.list([]string{"Project", "Global"}, scopeIndex()),
		)
	case stepRules:
		return uicomponents.Panel(
			uicomponents.PanelProps{
				Title:    "Permissions — " + scopeTitle,
				Error:    props.ErrorMessage,
				Bindings: bindings,
			},
			uicomponents.TabBar(uicomponents.TabBarProps{
				Tabs:         tabs,
				Active:       tab(),
				Numbered:     true,
				MarginBottom: 1,
			}),
			kitex.If(len(rules) == 0, func() trail.Node {
				return trail.Span(trail.SpanProps{Style: sheet.Secondary}, trail.Text("No "+listName+" rules."))
			}),
			sheet.list(rules, cursor.Index),
		)
	default:
		verb := "Add"
		if isEdit() {
			verb = "Edit"
		}
		return uicomponents.Panel(
			uicomponents.PanelProps{
				Title:  fmt.Sprintf("%s Rule — %s — %s", verb, scopeTitle, listName),
				Notice: "Format: tool(pattern)",
				Error:  cmp.Or(localError(), props.ErrorMessage),
				Bindings: []uicomponents.KeyBinding{
					{Key: "enter", Label: "Save"},
					{Key: "esc", Label: "Cancel"},
				},
			},
			trail.Span(trail.SpanProps{Style: sheet.Secondary}, trail.Text("Enter a permission rule:")),
			trail.Box(trail.BoxProps{Style: sheet.Row},
				trail.Span(trail.SpanProps{Style: sheet.selected}, trail.Text("❯ ")),
				kitex.Input(kitex.InputProps{
					Ref:   inputRef,
					Value: inputText(),
					Style: style.S().Flex(1),
					OnChange: func(e event.Event) {
						if ie, ok := e.(*event.InputEvent); ok {
							setInputText(ie.Value)
							if localError() != "" {
								setLocalError("")
							}
						}
					},
					OnKeyDown: func(e event.Event) {
						if ke, ok := e.(*event.KeyEvent); ok {
							switch {
							case ke.MatchString("enter"):
								e.PreventDefault()
								e.StopPropagation()
								rule := strings.TrimSpace(inputText())
								if rule == "" {
									setLocalError("Rule must not be empty")
									return
								}
								if !strings.Contains(rule, "(") || !strings.HasSuffix(rule, ")") {
									setLocalError("Invalid rule format: expected tool(pattern)")
									return
								}
								setLocalError("")
								if isEdit() {
									if props.OnEditRule != nil && rule != editTarget() {
										props.OnEditRule(activeScope, effect, editTarget(), rule)
									}
								} else {
									if props.OnAddRule != nil {
										props.OnAddRule(activeScope, effect, rule)
									}
								}
								setStep(stepRules)

							case ke.MatchString("esc", "escape"):
								e.PreventDefault()
								e.StopPropagation()
								setLocalError("")
								if props.OnClearError != nil {
									props.OnClearError()
								}
								setStep(stepRules)
							}
						}
					},
				}),
			))
	}
})
