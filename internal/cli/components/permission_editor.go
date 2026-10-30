package components

import (
	"fmt"
	"strings"

	"github.com/masterkeysrd/kite/dom"
	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/ui/components"
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

// PermissionConfigEditor is a 3-screen interactive permission rule configuration manager.
var PermissionConfigEditor = trail.FC("PermissionConfigEditor", func(props PermissionConfigEditorProps) trail.Node {
	scheme := theme.UseTheme()

	step, setStep := kitex.UseState(stepScope)
	scopeIndex, setScopeIndex := kitex.UseState(0) // 0: Project, 1: Global
	activeEffect, setActiveEffect := kitex.UseState(api.EffectAllow)
	ruleCursor, setRuleCursor := kitex.UseState(0)
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

	rules := scopePerms.Allow
	if activeEffect() == api.EffectDeny {
		rules = scopePerms.Deny
	}

	curRule := ruleCursor()
	if curRule >= len(rules) {
		curRule = len(rules) - 1
	}
	if curRule < 0 {
		curRule = 0
	}

	trail.UseInput(func(input string, k event.KeyEvent) {
		if step() == stepInput {
			return
		}

		switch step() {
		case stepScope:
			switch {
			case k.MatchString("up", "k"):
				if scopeIndex() > 0 {
					setScopeIndex(0)
				}
			case k.MatchString("down", "j"):
				if scopeIndex() < 1 {
					setScopeIndex(1)
				}
			case k.MatchString("enter"):
				setStep(stepRules)
				setRuleCursor(0)
				if props.OnClearError != nil {
					props.OnClearError()
				}
			case k.MatchString("esc", "escape"):
				if props.OnClose != nil {
					props.OnClose()
				}
			}

		case stepRules:
			switch {
			case k.MatchString("tab", "right", "l"):
				if activeEffect() == api.EffectAllow {
					setActiveEffect(api.EffectDeny)
				} else {
					setActiveEffect(api.EffectAllow)
				}
				setRuleCursor(0)
				if props.OnClearError != nil {
					props.OnClearError()
				}

			case k.MatchString("left", "h"):
				if activeEffect() == api.EffectDeny {
					setActiveEffect(api.EffectAllow)
				} else {
					setActiveEffect(api.EffectDeny)
				}
				setRuleCursor(0)
				if props.OnClearError != nil {
					props.OnClearError()
				}

			case k.MatchString("up", "k"):
				if curRule > 0 {
					setRuleCursor(curRule - 1)
				}

			case k.MatchString("down", "j"):
				if curRule < len(rules)-1 {
					setRuleCursor(curRule + 1)
				}

			case input == "a" || input == "A" || k.MatchString("a"):
				setIsEdit(false)
				setEditTarget("")
				setInputText("")
				setLocalError("")
				if props.OnClearError != nil {
					props.OnClearError()
				}
				setStep(stepInput)

			case input == "e" || input == "E" || k.MatchString("e"):
				if len(rules) > 0 {
					setIsEdit(true)
					setEditTarget(rules[curRule])
					setInputText(rules[curRule])
					setLocalError("")
					if props.OnClearError != nil {
						props.OnClearError()
					}
					setStep(stepInput)
				}

			case input == "d" || input == "D" || k.MatchString("d", "backspace", "delete"):
				if len(rules) > 0 && props.OnRemoveRule != nil {
					props.OnRemoveRule(activeScope, activeEffect(), rules[curRule])
				}

			case k.MatchString("esc", "escape"):
				setStep(stepScope)
				if props.OnClearError != nil {
					props.OnClearError()
				}
			}
		}
	})

	// Styles
	border := style.SingleBorder().Top(true).Bottom(false).Left(false).Right(false).Color(scheme.Color.Border.Default)
	titleStyle := style.S().Bold(true).Foreground(scheme.Color.Agent.Permission)
	primaryStyle := style.S().Bold(true).WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Icon.Primary)
	secondaryStyle := style.S().WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Text.Secondary)
	hintStyle := style.S().WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Text.Muted)
	errorStyle := style.S().Foreground(scheme.Color.Text.Error)
	activeTabStyle := style.S().Bold(true).WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Icon.Primary)
	inactiveTabStyle := style.S().WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Text.Secondary)

	var children []trail.Node

	switch step() {
	case stepScope:
		children = append(children,
			trail.Span(trail.SpanProps{Style: titleStyle}, trail.Text("Permission Config Editor")),
			trail.Newline(1),
			trail.Span(trail.SpanProps{Style: secondaryStyle}, trail.Text("Select a config scope to edit:")),
			trail.Newline(1),
		)

		scopeItems := []struct {
			name string
			desc string
		}{
			{name: "Project", desc: "Rules that apply only to this project (highest priority)"},
			{name: "Global", desc: "Rules that apply across all projects"},
		}

		for i, item := range scopeItems {
			prefix := "  "
			itemStyle := secondaryStyle
			if i == scopeIndex() {
				prefix = "❯ "
				itemStyle = primaryStyle
			}
			children = append(children, trail.Box(trail.BoxProps{
				Style: style.S().FlexDirection(style.FlexRow),
			},
				trail.Span(trail.SpanProps{Style: itemStyle}, trail.Text(prefix+item.name)),
			))
		}

		children = append(children,
			trail.Newline(1),
			trail.Box(trail.BoxProps{
				Style: style.S().FlexDirection(style.FlexRow),
			},
				trail.Span(trail.SpanProps{Style: hintStyle}, trail.Text("  "+scopeItems[scopeIndex()].desc)),
			),
			components.KeyboardHint(components.KeyboardHintProps{
				Bindings: []components.KeyBinding{
					{Key: "↑/↓", Label: "Navigate"},
					{Key: "enter", Label: "Select"},
					{Key: "esc", Label: "Close"},
				},
			}),
		)

	case stepRules:
		children = append(children,
			trail.Span(trail.SpanProps{Style: titleStyle}, trail.Text(fmt.Sprintf("Permissions — %s", scopeTitle))),
		)

		var allowTab, denyTab style.Style
		if activeEffect() == api.EffectAllow {
			allowTab = activeTabStyle
			denyTab = inactiveTabStyle
		} else {
			allowTab = inactiveTabStyle
			denyTab = activeTabStyle
		}

		children = append(children,
			trail.Box(trail.BoxProps{
				Style: style.S().FlexDirection(style.FlexRow),
			},
				trail.Span(trail.SpanProps{Style: allowTab}, trail.Text(fmt.Sprintf("allowlist (%d)", len(scopePerms.Allow)))),
				trail.Span(trail.SpanProps{Style: inactiveTabStyle}, trail.Text("    ")),
				trail.Span(trail.SpanProps{Style: denyTab}, trail.Text(fmt.Sprintf("denylist (%d)", len(scopePerms.Deny)))),
			),
			trail.Newline(1),
		)

		if len(rules) == 0 {
			emptyText := "No allowlist rules."
			if activeEffect() == api.EffectDeny {
				emptyText = "No denylist rules."
			}
			children = append(children, trail.Box(trail.BoxProps{
				Style: style.S().FlexDirection(style.FlexRow),
			},
				trail.Span(trail.SpanProps{Style: secondaryStyle}, trail.Text(emptyText)),
			))
		} else {
			for i, r := range rules {
				prefix := "  "
				rStyle := secondaryStyle
				if i == curRule {
					prefix = "❯ "
					rStyle = primaryStyle
				}
				children = append(children, trail.Box(trail.BoxProps{
					Style: style.S().FlexDirection(style.FlexRow),
				},
					trail.Span(trail.SpanProps{Style: rStyle}, trail.Text(prefix+r)),
				))
			}
		}

		if props.ErrorMessage != "" {
			children = append(children,
				trail.Newline(1),
				trail.Box(trail.BoxProps{
					Style: style.S().FlexDirection(style.FlexRow),
				},
					trail.Span(trail.SpanProps{Style: errorStyle}, trail.Text(props.ErrorMessage)),
				),
			)
		}

		children = append(children,
			components.KeyboardHint(components.KeyboardHintProps{
				Bindings: []components.KeyBinding{
					{Key: "↑/↓", Label: "Navigate"},
					{Key: "←/→", Label: "Switch View"},
					{Key: "a", Label: "Add rule"},
					{Key: "e", Label: "Edit rule"},
					{Key: "d/⌫", Label: "Delete rule"},
					{Key: "esc", Label: "Back"},
				},
			}),
		)

	case stepInput:
		listName := "allowlist"
		if activeEffect() == api.EffectDeny {
			listName = "denylist"
		}

		inputTitle := fmt.Sprintf("Add Rule — %s — %s", scopeTitle, listName)
		if isEdit() {
			inputTitle = fmt.Sprintf("Edit Rule — %s — %s", scopeTitle, listName)
		}

		children = append(children,
			trail.Span(trail.SpanProps{Style: titleStyle}, trail.Text(inputTitle)),
			trail.Newline(1),
			trail.Span(trail.SpanProps{Style: secondaryStyle}, trail.Text("Enter a permission rule:")),
			trail.Newline(1),
			trail.Box(trail.BoxProps{
				Style: style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexRow).
					AlignItems(style.AlignCenter).
					Width(style.Percent(100)),
			},
				trail.Span(trail.SpanProps{Style: primaryStyle}, trail.Text("❯ ")),
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
										props.OnEditRule(activeScope, activeEffect(), editTarget(), rule)
									}
								} else {
									if props.OnAddRule != nil {
										props.OnAddRule(activeScope, activeEffect(), rule)
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
			),
		)

		errText := localError()
		if errText == "" && props.ErrorMessage != "" {
			errText = props.ErrorMessage
		}
		if errText != "" {
			children = append(children,
				trail.Newline(1),
				trail.Box(trail.BoxProps{
					Style: style.S().FlexDirection(style.FlexRow),
				},
					trail.Span(trail.SpanProps{Style: errorStyle}, trail.Text(errText)),
				),
			)
		}

		children = append(children,
			trail.Newline(1),
			trail.Span(trail.SpanProps{Style: hintStyle}, trail.Text("Format: tool(pattern)")),
			components.KeyboardHint(components.KeyboardHintProps{
				Bindings: []components.KeyBinding{
					{Key: "enter", Label: "Save"},
					{Key: "esc", Label: "Cancel"},
				},
			}),
		)
	}

	return trail.Box(trail.BoxProps{
		Style: style.S().
			Border(border).
			Width(style.Percent(100)).
			FlexDirection(style.FlexColumn),
	}, children...)
})
