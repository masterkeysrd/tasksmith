package components

import (
	"fmt"
	"slices"
	"strings"

	"github.com/masterkeysrd/kite/dom"
	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/strutils"
	"github.com/masterkeysrd/tasksmith/internal/ui/components"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

// SandboxPolicyKnob identifies one boolean sandbox.json posture toggle.
type SandboxPolicyKnob string

const (
	// KnobFailIfUnavailable toggles hard failure when kernel sandboxing is absent.
	KnobFailIfUnavailable SandboxPolicyKnob = "failIfUnavailable"
	// KnobAllowUnsandboxed toggles per-command unsandboxed execution.
	KnobAllowUnsandboxed SandboxPolicyKnob = "allowUnsandboxed"
	// KnobAutoAllowBash toggles auto-approval of safe sandboxed bash plans.
	KnobAutoAllowBash SandboxPolicyKnob = "autoAllowBash"
)

// SandboxConfigEditorProps defines the properties for the SandboxConfigEditor component.
type SandboxConfigEditorProps struct {
	Response api.SandboxResponse
	// SessionMode is the backend the active session actually runs under; the
	// stored default mode only applies to sessions created after a change.
	SessionMode api.SandboxMode
	// LockedKnobs holds, per scope, the knobs the user cannot change from that
	// scope because the opposite scope (or the workspace trust floor) already
	// pins them via the monotonic merge rules. The scope a knob was set in is
	// never locked to itself, and the Global scope is never locked at all, so
	// a lock can always be undone where it was written.
	LockedKnobs map[api.PermissionScope]map[SandboxPolicyKnob]string
	// Saving marks an in-flight settings write; it renders inline in the tab
	// strip so toggling never shifts rows vertically.
	Saving       bool
	ErrorMessage string
	// OnSetMode requests a new default backend for the scope ("" clears the
	// override so the scope inherits).
	OnSetMode func(scope api.PermissionScope, mode api.SandboxMode)
	// OnToggleKnob requests setting one boolean posture knob; a nil value
	// clears the override so the scope inherits again.
	OnToggleKnob func(scope api.PermissionScope, knob SandboxPolicyKnob, value *bool)
	// OnAddExcluded requests appending one excludedCommands pattern.
	OnAddExcluded func(scope api.PermissionScope, pattern string)
	// OnEditExcluded requests replacing one excludedCommands pattern in place.
	OnEditExcluded func(scope api.PermissionScope, oldPattern, newPattern string)
	// OnRemoveExcluded requests deleting one excludedCommands pattern.
	OnRemoveExcluded func(scope api.PermissionScope, pattern string)
	OnClose          func()
	OnClearError     func()
}

type sandboxEditorTab int

const (
	sandboxTabPolicy sandboxEditorTab = iota
	sandboxTabExcluded
	sandboxTabResolved
)

// sandboxPolicyRow describes one row of the aligned policy table.
type sandboxPolicyRow struct {
	knob  SandboxPolicyKnob // "" for the mode row
	label string
	key   string
	desc  string
}

func sandboxPolicyRows() []sandboxPolicyRow {
	return []sandboxPolicyRow{
		{label: "Default mode", key: "mode", desc: "Default isolation backend for new sessions. The active session keeps its own backend."},
		{knob: KnobFailIfUnavailable, label: "Fail if unavailable", key: "failIfUnavailable", desc: "Fail session creation instead of silently degrading to local isolation when kernel sandboxing is unavailable."},
		{knob: KnobAllowUnsandboxed, label: "Allow unsandboxed", key: "allowUnsandboxed", desc: "Allow unsandboxed command execution (bypass_sandbox, excludedCommands, ! prompt)."},
		{knob: KnobAutoAllowBash, label: "Auto-allow safe bash", key: "autoAllowBash", desc: "Auto-approve safe sandboxed bash plans in default and accept-edits modes."},
	}
}

func knobValue(settings api.SandboxSettings, knob SandboxPolicyKnob) *bool {
	switch knob {
	case KnobFailIfUnavailable:
		return settings.FailIfUnavailable
	case KnobAllowUnsandboxed:
		return settings.AllowUnsandboxed
	case KnobAutoAllowBash:
		return settings.AutoAllowBash
	default:
		return nil
	}
}

// nextSandboxMode cycles the scope's mode override native -> local -> inherit.
func nextSandboxMode(current api.SandboxMode, effective api.SandboxMode) api.SandboxMode {
	switch current {
	case "":
		return api.SandboxNative
	case api.SandboxNative:
		return api.SandboxLocal
	case api.SandboxLocal:
		return ""
	default:
		if effective == api.SandboxNative {
			return api.SandboxLocal
		}
		return api.SandboxNative
	}
}

// sandboxTabStrip renders the bracketed tab strip and optional scope/saving
// suffix, mirroring the /usage tab bar conventions while letting the scope
// indicator wrap onto a second line on narrow terminals instead of clipping.
func sandboxTabStrip(active sandboxEditorTab, scopeTitle string, excludedCount int, saving bool) (tabs string, meta string) {
	labels := []string{
		"1 Policy",
		fmt.Sprintf("2 Exclusions (%d)", excludedCount),
		"3 Effective",
	}
	parts := make([]string, len(labels))
	for i, label := range labels {
		if sandboxEditorTab(i) == active {
			label = "[" + label + "]"
		}
		parts[i] = label
	}
	tabs = strings.Join(parts, "  ")

	var metaParts []string
	if active != sandboxTabResolved {
		projectLabel, globalLabel := "Project", "[Global]"
		if scopeTitle == "Project" {
			projectLabel, globalLabel = "[Project]", "Global"
		}
		metaParts = append(metaParts, "scope: "+projectLabel+" · "+globalLabel)
	}
	if saving {
		metaParts = append(metaParts, "saving…")
	}
	meta = strings.Join(metaParts, "  · ")
	return tabs, meta
}

// windowIndex returns the half-open [start, end) slice of count rows that a
// viewport of the given size should display so the cursor row stays visible.
func windowIndex(cur, count, size int) (start, end int) {
	if size < 1 {
		size = 1
	}
	if count <= size {
		return 0, count
	}
	start = max(min(cur-size/2, count-size), 0)
	return start, start + size
}

// SandboxConfigEditor is a 3-tab interactive sandbox.json configuration manager.
var SandboxConfigEditor = trail.FC("SandboxConfigEditor", func(props SandboxConfigEditorProps) trail.Node {
	scheme := theme.UseTheme()
	winSize := trail.UseWindowSize()

	tab, setTab := kitex.UseState(sandboxTabPolicy)
	scopeIndex, setScopeIndex := kitex.UseState(0) // 0: Project, 1: Global
	offset, setOffset := kitex.UseState(0)         // Tab 3 viewport scroll position
	inInput, setInInput := kitex.UseState(false)
	editing, setEditing := kitex.UseState("") // non-empty while editing a pattern in place
	inputText, setInputText := kitex.UseState("")
	localError, setLocalError := kitex.UseState("")

	inputRef := kitex.UseRef[dom.Element](nil)
	trail.UseFocus(inputRef, trail.FocusOptions{
		AutoFocus: true,
		IsActive:  inInput(),
	})

	activeScope := api.ScopeProject
	scopeTitle := "Project"
	scopeSettings := props.Response.Project
	if scopeIndex() == 1 {
		activeScope = api.ScopeGlobal
		scopeTitle = "Global"
		scopeSettings = props.Response.Global
	}

	avail := props.Response.Availability
	excluded := scopeSettings.ExcludedCommands
	inherited := props.Response.Global.ExcludedCommands
	activeLocked := props.LockedKnobs[activeScope]
	effExclusionsOff := knobValue(props.Response.Effective, KnobAllowUnsandboxed) != nil &&
		!*knobValue(props.Response.Effective, KnobAllowUnsandboxed)

	policyRows := sandboxPolicyRows()

	// UseCursor clamps the cursor to the active tab's row count.
	maxRows := len(policyRows)
	if tab() == sandboxTabExcluded {
		maxRows = len(excluded)
	}
	cursor := components.UseCursor(maxRows, 0)
	cur := cursor.Index

	var resolvedContent []sandboxLine
	if tab() == sandboxTabResolved {
		resolvedContent = resolvedLines(props.Response)
	}
	const reserved = 7
	viewport := 22
	if winSize.Height > 0 {
		viewport = max(winSize.Height-reserved, 4)
	}
	maxOffset := max(0, len(resolvedContent)-viewport)
	pos := min(offset(), maxOffset)

	clearError := func() {
		setLocalError("")
		if props.OnClearError != nil {
			props.OnClearError()
		}
	}

	trail.UseInput(func(input string, k event.KeyEvent) {
		if inInput() {
			return
		}

		switch {
		case k.MatchString("shift+tab"), k.MatchString("left"):
			if tab() > sandboxTabPolicy {
				setTab(tab() - 1)
			} else {
				setTab(sandboxTabResolved)
			}
			cursor.Set(0)
			setOffset(0)
			clearError()
		case k.MatchString("tab", "right"):
			if tab() < sandboxTabResolved {
				setTab(tab() + 1)
			} else {
				setTab(sandboxTabPolicy)
			}
			cursor.Set(0)
			setOffset(0)
			clearError()
		case k.MatchString("1"):
			setTab(sandboxTabPolicy)
			cursor.Set(0)
			setOffset(0)
			clearError()
		case k.MatchString("2"):
			setTab(sandboxTabExcluded)
			cursor.Set(0)
			setOffset(0)
			clearError()
		case k.MatchString("3"):
			setTab(sandboxTabResolved)
			cursor.Set(0)
			setOffset(0)
			clearError()
		case tab() != sandboxTabResolved && k.MatchString("s"):
			if scopeIndex() == 0 {
				setScopeIndex(1)
			} else {
				setScopeIndex(0)
			}
			cursor.Set(0)
			clearError()
		case k.MatchString("up", "k", "ctrl+p"):
			if tab() == sandboxTabResolved {
				setOffset(max(0, pos-1))
			} else {
				cursor.HandleKey(k)
			}
		case k.MatchString("down", "j", "ctrl+n"):
			if tab() == sandboxTabResolved {
				setOffset(min(maxOffset, pos+1))
			} else {
				cursor.HandleKey(k)
			}
		case tab() == sandboxTabResolved && k.MatchString("pgup"):
			setOffset(max(0, pos-viewport))
		case tab() == sandboxTabResolved && k.MatchString("pgdown", "space"):
			setOffset(min(maxOffset, pos+viewport))
		case tab() == sandboxTabResolved && input == "g":
			setOffset(0)
		case tab() == sandboxTabResolved && input == "G":
			setOffset(maxOffset)
		case k.MatchString("esc", "escape"):
			if props.OnClose != nil {
				props.OnClose()
			}
		}

		switch tab() {
		case sandboxTabPolicy:
			switch {
			case k.MatchString("enter", "t", "space"):
				if cur < 0 || cur >= len(policyRows) {
					return
				}
				row := policyRows[cur]
				if row.knob == "" {
					if props.OnSetMode != nil {
						props.OnSetMode(activeScope, nextSandboxMode(scopeSettings.Mode, props.Response.Effective.Mode))
					}
					return
				}
				if activeLocked[row.knob] != "" || props.OnToggleKnob == nil {
					return
				}
				// Flip the scope's own value when set; an unset override
				// flips the effective value, which is what writing an
				// explicit value will actually change.
				base := knobValue(scopeSettings, row.knob)
				if base == nil {
					base = knobValue(props.Response.Effective, row.knob)
				}
				if base != nil {
					next := !*base
					props.OnToggleKnob(activeScope, row.knob, &next)
				}
			case input == "x" || input == "X" || k.MatchString("x", "backspace", "delete"):
				if cur < 0 || cur >= len(policyRows) {
					return
				}
				row := policyRows[cur]
				if row.knob == "" {
					if props.OnSetMode != nil {
						props.OnSetMode(activeScope, "")
					}
					return
				}
				if activeLocked[row.knob] == "" && props.OnToggleKnob != nil {
					props.OnToggleKnob(activeScope, row.knob, nil)
				}
			}

		case sandboxTabExcluded:
			switch {
			case input == "a" || input == "A" || k.MatchString("a"):
				setEditing("")
				setInInput(true)
				setInputText("")
				clearError()
			case input == "e" || input == "E" || k.MatchString("e"):
				if len(excluded) > 0 && cur < len(excluded) {
					setEditing(excluded[cur])
					setInInput(true)
					setInputText(excluded[cur])
					clearError()
				}
			case input == "d" || input == "D" || k.MatchString("d", "backspace", "delete"):
				if len(excluded) > 0 && cur < len(excluded) && props.OnRemoveExcluded != nil {
					props.OnRemoveExcluded(activeScope, excluded[cur])
				}
			}
		}
	})

	border := style.SingleBorder().Top(true).Bottom(false).Left(false).Right(false).Color(scheme.Color.Border.Default)
	titleStyle := style.S().Bold(true).Foreground(scheme.Color.Icon.Primary)
	barStyle := style.S().WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Text.Secondary)
	primaryStyle := style.S().Bold(true).WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Icon.Primary)
	textPrimaryStyle := style.S().WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Text.Primary)
	secondaryStyle := style.S().WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Text.Secondary)
	mutedStyle := style.S().WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Text.Muted)
	accentStyle := style.S().WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Text.Accent)
	textSuccessStyle := style.S().WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Text.Success)
	textWarnStyle := style.S().WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Text.Warning)
	textErrorStyle := style.S().WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Text.Error)
	errorStyle := style.S().Foreground(scheme.Color.Text.Error)
	successStyle := style.S().Bold(true).WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Icon.Success)
	warnStyle := style.S().Bold(true).WhiteSpace(style.WhiteSpacePre).Foreground(scheme.Color.Icon.Warning)
	wrapStyle := style.S().Foreground(scheme.Color.Text.Muted).OverflowWrap(style.OverflowWrapAnywhere)

	var contentWidth int
	if winSize.Width > 0 {
		contentWidth = max(winSize.Width-6, 24)
	}
	truncate := func(s string) string {
		if contentWidth <= 0 {
			return s
		}
		out, _ := strutils.TruncateLine(s, contentWidth, "…")
		return out
	}
	row := func(st style.Style, text string) trail.Node {
		if text == "" {
			text = " "
		}
		return trail.Box(trail.BoxProps{}, trail.Span(trail.SpanProps{Style: st}, trail.Text(truncate(text))))
	}

	var children []trail.Node

	// Fixed header: title + wrapping tab/scope strip (scope chrome and saving
	// status live inline on wide screens and wrap cleanly onto a second line on
	// narrow panes instead of clipping).
	tabsText, metaText := sandboxTabStrip(tab(), scopeTitle, len(excluded), props.Saving)
	barChildren := []trail.Node{
		trail.Span(trail.SpanProps{Style: barStyle.Flex(0, 0)}, trail.Text(truncate(tabsText))),
	}
	if metaText != "" {
		barChildren = append(barChildren,
			trail.Span(trail.SpanProps{Style: barStyle.Flex(0, 0)}, trail.Text(truncate(metaText))),
		)
	}
	children = append(children,
		row(titleStyle, "Sandbox"),
		trail.Box(trail.BoxProps{
			Style: style.S().
				Display(style.DisplayFlex).
				FlexDirection(style.FlexRow).
				FlexWrap(style.FlexWrapOn).
				Gap(0, 4).
				Width(style.Percent(100)),
		}, barChildren...),
		row(mutedStyle, ""),
	)

	switch tab() {
	case sandboxTabPolicy:
		children = append(children, policyTabNodes(policyTabParams{
			scopeTitle:    scopeTitle,
			scopeSettings: scopeSettings,
			effective:     props.Response.Effective,
			sessionMode:   props.SessionMode,
			availability:  avail,
			restricted:    props.Response.Restricted,
			locked:        activeLocked,
			cursor:        cur,
			styles: editorStyles{
				primary:   primaryStyle,
				secondary: secondaryStyle,
				muted:     mutedStyle,
				success:   successStyle,
				warn:      warnStyle,
			},
			row: row,
			wrapColumn: func(text string, indent int) trail.Node {
				return trail.Box(trail.BoxProps{
					Style: style.S().FlexDirection(style.FlexColumn).Width(style.Percent(100)).PaddingLeft(indent),
				}, trail.Span(trail.SpanProps{Style: wrapStyle}, trail.Text(text)))
			},
		})...)

	case sandboxTabExcluded:
		listHeight := max(winSize.Height-12, 5)
		start, end := windowIndex(cur, len(excluded), listHeight)
		children = append(children, excludedTabNodes(excludedTabParams{
			scopeTitle:  scopeTitle,
			excluded:    excluded,
			inherited:   inherited,
			showInherit: activeScope == api.ScopeProject && len(inherited) > 0,
			cursor:      cur,
			start:       start,
			end:         end,
			warnOff:     effExclusionsOff,
			styles: editorStyles{
				primary:   primaryStyle,
				secondary: secondaryStyle,
				muted:     mutedStyle,
				warn:      warnStyle,
			},
			row: row,
		})...)

	case sandboxTabResolved:
		children = append(children, resolvedTabNodes(resolvedTabParams{
			lines:        resolvedContent,
			pos:          pos,
			end:          min(len(resolvedContent), pos+viewport),
			total:        len(resolvedContent),
			contentWidth: contentWidth,
			styles: resolvedStyles{
				heading:     primaryStyle,
				primaryText: textPrimaryStyle,
				secondary:   secondaryStyle,
				muted:       mutedStyle,
				accent:      accentStyle,
				success:     textSuccessStyle,
				warn:        textWarnStyle,
				danger:      textErrorStyle,
			},
			row: row,
		})...)
	}

	errText := localError()
	if errText == "" && props.ErrorMessage != "" {
		errText = props.ErrorMessage
	}
	if errText != "" {
		children = append(children, row(errorStyle, errText))
	}

	if inInput() {
		inputTitle := fmt.Sprintf("Add excluded command pattern (%s):", scopeTitle)
		if editing() != "" {
			inputTitle = fmt.Sprintf("Edit excluded command pattern (%s):", scopeTitle)
		}
		children = append(children,
			trail.Box(trail.BoxProps{}, trail.Span(trail.SpanProps{Style: titleStyle}, trail.Text(inputTitle))),
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
								pattern := strings.TrimSpace(inputText())
								if pattern == "" {
									setLocalError("Pattern must not be empty")
									return
								}
								if strings.ContainsAny(pattern, "\n\r") {
									setLocalError("Pattern must be a single line")
									return
								}
								if editing() != "" {
									if pattern != editing() {
										if containsPattern(excluded, pattern) {
											setLocalError("Another exclusion already uses that pattern")
											return
										}
										if props.OnEditExcluded != nil {
											props.OnEditExcluded(activeScope, editing(), pattern)
										}
									}
								} else {
									if containsPattern(excluded, pattern) {
										setLocalError("That pattern is already excluded")
										return
									}
									if props.OnAddExcluded != nil {
										props.OnAddExcluded(activeScope, pattern)
									}
								}
								setInInput(false)
								setEditing("")
								setInputText("")
							case ke.MatchString("esc", "escape"):
								e.PreventDefault()
								e.StopPropagation()
								clearError()
								setInInput(false)
								setEditing("")
								setInputText("")
							}
						}
					},
				}),
			),
			trail.Box(trail.BoxProps{}, trail.Span(trail.SpanProps{Style: mutedStyle},
				trail.Text("Examples: git commit *, docker build *, make release"))),
			components.KeyboardHint(components.KeyboardHintProps{Bindings: []components.KeyBinding{
				{Key: "enter", Label: "Save"},
				{Key: "esc", Label: "Cancel"},
			}}),
		)
	} else {
		var bindings []components.KeyBinding
		switch tab() {
		case sandboxTabPolicy:
			bindings = []components.KeyBinding{
				{Key: "←/→", Label: "Tab"},
				{Key: "s", Label: "Scope"},
				{Key: "↑/↓", Label: "Move"},
				{Key: "space", Label: "Toggle"},
				{Key: "x", Label: "Reset"},
				{Key: "esc", Label: "Close"},
			}
		case sandboxTabExcluded:
			bindings = []components.KeyBinding{
				{Key: "←/→", Label: "Tab"},
				{Key: "s", Label: "Scope"},
				{Key: "↑/↓", Label: "Move"},
				{Key: "a", Label: "Add"},
				{Key: "e", Label: "Edit"},
				{Key: "d", Label: "Delete"},
				{Key: "esc", Label: "Close"},
			}
		case sandboxTabResolved:
			bindings = []components.KeyBinding{
				{Key: "←/→", Label: "Tab"},
				{Key: "↑/↓", Label: "Scroll"},
				{Key: "pgup/pgdn", Label: "Page"},
				{Key: "esc", Label: "Close"},
			}
		}
		children = append(children, components.KeyboardHint(components.KeyboardHintProps{Bindings: bindings}))
	}

	return trail.Box(trail.BoxProps{
		Style: style.S().
			Border(border).
			Width(style.Percent(100)).
			FlexDirection(style.FlexColumn),
	}, children...)
})

func containsPattern(patterns []string, pattern string) bool {
	return slices.Contains(patterns, pattern)
}

type editorStyles struct {
	primary   style.Style
	secondary style.Style
	muted     style.Style
	success   style.Style
	warn      style.Style
}

type policyTabParams struct {
	scopeTitle    string
	scopeSettings api.SandboxSettings
	effective     api.SandboxSettings
	sessionMode   api.SandboxMode
	availability  api.SandboxAvailability
	restricted    bool
	locked        map[SandboxPolicyKnob]string
	cursor        int
	styles        editorStyles
	row           func(style.Style, string) trail.Node
	wrapColumn    func(string, int) trail.Node
}

const policyLabelCol = 22

// policyRowText joins one policy table row with fixed column offsets.
func policyRowText(label, effective, override string) string {
	return fmt.Sprintf("%-*s%-14s%s", policyLabelCol, label, effective, override)
}

func policyTabNodes(p policyTabParams) []trail.Node {
	var nodes []trail.Node

	// Live runtime status banner.
	sessionLine := "Active session:    none"
	if p.sessionMode != "" {
		sessionLine = fmt.Sprintf("Active session:    %s sandbox (mode edits apply to new sessions)", p.sessionMode)
	}
	nodes = append(nodes, p.row(p.styles.secondary, sessionLine))

	backendLine := "Host backend:      ⚠ unavailable — sessions run with local isolation"
	backendStyle := p.styles.warn
	if p.availability.Available {
		backendLine = fmt.Sprintf("Host backend:      ✔ %s available", p.availability.Backend)
		backendStyle = p.styles.success
	} else if p.availability.Backend != "" {
		backendLine = fmt.Sprintf("Host backend:      ⚠ %s unavailable", p.availability.Backend)
	}
	nodes = append(nodes, p.row(backendStyle, backendLine))
	if !p.availability.Available {
		if p.availability.Error != "" {
			nodes = append(nodes, p.row(p.styles.muted, "  error: "+p.availability.Error))
		}
		if p.availability.Remediation != "" {
			// Remediation carries the exact command to run, so it must never
			// be truncated away; it wraps across lines instead.
			nodes = append(nodes, p.wrapColumn("Fix: "+p.availability.Remediation, 0))
		}
	}

	trustLine := "Workspace trust:   ✔ trusted"
	trustStyle := p.styles.success
	if p.restricted {
		trustLine = "Workspace trust:   ⚠ untrusted (unsandboxed execution disabled)"
		trustStyle = p.styles.warn
	}
	nodes = append(nodes, p.row(trustStyle, trustLine))

	nodes = append(nodes, p.row(p.styles.muted, ""))

	// Aligned 3-column settings table.
	overrideCol := fmt.Sprintf("%s override", p.scopeTitle)
	nodes = append(nodes, p.row(p.styles.muted, policyRowText("Setting", "Effective", overrideCol)))
	rows := sandboxPolicyRows()
	for i, r := range rows {
		selected := p.cursor == i
		rowStyle := p.styles.secondary
		if selected {
			rowStyle = p.styles.primary
		}

		var effectiveCell, overrideCell string
		if r.knob == "" {
			effMode := p.effective.Mode
			if effMode == "" {
				effMode = api.SandboxLocal
			}
			effectiveCell = "● " + string(effMode)
		} else {
			eff := knobValue(p.effective, r.knob)
			effectiveCell = "○ off"
			if eff != nil && *eff {
				effectiveCell = "● on"
			}
		}
		if reason, locked := p.locked[r.knob]; locked {
			overrideCell = "🔒 locked (" + reason + ")"
		} else if r.knob == "" {
			if p.scopeSettings.Mode != "" {
				overrideCell = string(p.scopeSettings.Mode)
			} else {
				overrideCell = "inherited"
			}
		} else if val := knobValue(p.scopeSettings, r.knob); val != nil {
			overrideCell = fmt.Sprintf("%v", *val)
		} else {
			overrideCell = "inherited"
		}

		mark := "  "
		if selected {
			mark = "❯ "
		}
		nodes = append(nodes, p.row(rowStyle, mark+policyRowText(r.label, effectiveCell, overrideCell)))
	}

	// Fixed detail footer: description and JSON key for the highlighted row
	// plus lock guidance when the knob is pinned. Key usage itself comes from
	// the KeyboardHint below. It never inserts or collapses rows inside the
	// table.
	if p.cursor >= 0 && p.cursor < len(rows) {
		r := rows[p.cursor]
		nodes = append(nodes, p.row(p.styles.muted, ""))
		nodes = append(nodes, p.wrapColumn(r.desc, 2))
		if lockReason := p.locked[r.knob]; lockReason != "" {
			nodes = append(nodes, p.wrapColumn(
				fmt.Sprintf("key: %s · locked: edit that scope (s) or its pinning setting to change this", r.key), 2))
		} else {
			nodes = append(nodes, p.row(p.styles.muted, "  key: "+r.key))
		}
	}

	return nodes
}

type excludedTabParams struct {
	scopeTitle  string
	excluded    []string
	inherited   []string
	showInherit bool
	cursor      int
	start       int
	end         int
	warnOff     bool
	styles      editorStyles
	row         func(style.Style, string) trail.Node
}

func excludedTabNodes(p excludedTabParams) []trail.Node {
	var nodes []trail.Node
	if p.warnOff {
		nodes = append(nodes, p.row(p.styles.warn,
			"⚠ Inactive: \"Allow unsandboxed\" is off, so excluded commands still run sandboxed."))
	}
	nodes = append(nodes, p.row(p.styles.secondary, fmt.Sprintf("Excluded commands — %s (%d)", p.scopeTitle, len(p.excluded))))
	if len(p.excluded) == 0 {
		nodes = append(nodes, p.row(p.styles.secondary, "No excluded commands. Press a to add one (e.g. \"git commit *\")."))
	} else {
		if p.start > 0 {
			nodes = append(nodes, p.row(p.styles.muted, fmt.Sprintf("  ↑ %d more", p.start)))
		}
		for i := p.start; i < p.end && i < len(p.excluded); i++ {
			styleRow := p.styles.secondary
			if i == p.cursor {
				styleRow = p.styles.primary
			}
			mark := "  "
			if i == p.cursor {
				mark = "❯ "
			}
			nodes = append(nodes, p.row(styleRow, mark+p.excluded[i]))
		}
		if p.end < len(p.excluded) {
			nodes = append(nodes, p.row(p.styles.muted, fmt.Sprintf("  ↓ %d more", len(p.excluded)-p.end)))
		}
	}
	if p.showInherit {
		nodes = append(nodes, p.row(p.styles.muted, ""))
		nodes = append(nodes, p.row(p.styles.muted,
			fmt.Sprintf("Inherited from Global (%d) — press s to edit Global", len(p.inherited))))
		for _, pattern := range p.inherited {
			nodes = append(nodes, p.row(p.styles.muted, "  "+pattern))
		}
	}
	return nodes
}

type sandboxLineRole int

const (
	lineSecondary sandboxLineRole = iota
	lineHeading
	linePrimaryText
	lineMuted
	lineAccent
	lineSuccess
	lineWarn
	lineDanger
)

type sandboxSpan struct {
	text string
	role sandboxLineRole
}

type sandboxLine struct {
	spans []sandboxSpan
}

func resolvedKVLine(label, value string, valRole sandboxLineRole) sandboxLine {
	return sandboxLine{
		spans: []sandboxSpan{
			{text: fmt.Sprintf("%-*s", policyLabelCol, label), role: lineSecondary},
			{text: value, role: valRole},
		},
	}
}

func boolOnOffRole(v *bool, onRole sandboxLineRole) (string, sandboxLineRole) {
	if v != nil && *v {
		return "on", onRole
	}
	return "off", lineMuted
}

// resolvedLines renders the full effective posture for the read-only
// Effective tab: posture summary plus the canonical filesystem rules. Paths
// are home-shortened and empty lists collapse onto one line.
func resolvedLines(resp api.SandboxResponse) []sandboxLine {
	eff := resp.Effective
	lines := []sandboxLine{
		{spans: []sandboxSpan{{text: "Posture", role: lineHeading}}},
		{},
	}

	mode := eff.Mode
	if mode == "" {
		mode = api.SandboxLocal
	}
	modeRole := lineSuccess
	if mode != api.SandboxNative {
		modeRole = lineWarn
	}

	failVal, failRole := boolOnOffRole(eff.FailIfUnavailable, lineSuccess)
	unsandboxedVal, unsandboxedRole := boolOnOffRole(eff.AllowUnsandboxed, lineWarn)
	autoBashVal, autoBashRole := boolOnOffRole(eff.AutoAllowBash, lineSuccess)

	lines = append(lines,
		resolvedKVLine("Mode:", string(mode), modeRole),
		resolvedKVLine("Fail if unavailable:", failVal, failRole),
		resolvedKVLine("Allow unsandboxed:", unsandboxedVal, unsandboxedRole),
		resolvedKVLine("Auto-allow bash:", autoBashVal, autoBashRole),
	)

	netMode := eff.Network.Mode
	if netMode == "" {
		netMode = api.SandboxNetworkOpen
	}
	netRole := lineWarn
	switch netMode {
	case api.SandboxNetworkDisabled:
		netRole = lineSuccess
	case api.SandboxNetworkAllowlist:
		netRole = lineAccent
	}
	lines = append(lines, resolvedKVLine("Network:", string(netMode), netRole))
	for _, d := range eff.Network.AllowedDomains {
		lines = append(lines, sandboxLine{spans: []sandboxSpan{
			{text: "  allow ", role: lineSuccess},
			{text: d, role: linePrimaryText},
		}})
	}
	for _, d := range eff.Network.DeniedDomains {
		lines = append(lines, sandboxLine{spans: []sandboxSpan{
			{text: "  deny ", role: lineDanger},
			{text: d, role: linePrimaryText},
		}})
	}

	if len(eff.ExcludedCommands) == 0 {
		lines = append(lines, resolvedKVLine("Excluded commands:", "none", lineMuted))
	} else {
		lines = append(lines, sandboxLine{spans: []sandboxSpan{
			{text: fmt.Sprintf("%-*s", policyLabelCol, "Excluded commands:"), role: lineSecondary},
			{text: fmt.Sprintf("%d", len(eff.ExcludedCommands)), role: lineWarn},
			{text: " (union of scopes)", role: lineMuted},
		}})
		for _, pattern := range eff.ExcludedCommands {
			lines = append(lines, sandboxLine{spans: []sandboxSpan{
				{text: "  " + pattern, role: lineWarn},
			}})
		}
	}

	lines = append(lines,
		sandboxLine{},
		sandboxLine{spans: []sandboxSpan{{text: "Filesystem", role: lineHeading}}},
		sandboxLine{},
		resolvedKVLine("Working directory:", ShortenHomePath(resp.ResolvedPaths.WorkDir), lineAccent),
	)

	sections := []struct {
		title string
		paths []string
		role  sandboxLineRole
	}{
		{title: "Allow write", paths: resp.ResolvedPaths.AllowWrite, role: lineSuccess},
		{title: "Deny write", paths: resp.ResolvedPaths.DenyWrite, role: lineDanger},
		{title: "Deny read", paths: resp.ResolvedPaths.DenyRead, role: lineDanger},
		{title: "Allow read", paths: resp.ResolvedPaths.AllowRead, role: lineSuccess},
	}
	for _, sec := range sections {
		if len(sec.paths) == 0 {
			lines = append(lines, resolvedKVLine(sec.title+":", "none", lineMuted))
			continue
		}
		lines = append(lines, resolvedKVLine(sec.title+":", fmt.Sprintf("%d", len(sec.paths)), sec.role))
		for _, path := range sec.paths {
			lines = append(lines, sandboxLine{spans: []sandboxSpan{
				{text: "  " + ShortenHomePath(path), role: sec.role},
			}})
		}
	}

	return lines
}

type resolvedStyles struct {
	heading     style.Style
	primaryText style.Style
	secondary   style.Style
	muted       style.Style
	accent      style.Style
	success     style.Style
	warn        style.Style
	danger      style.Style
}

func (s resolvedStyles) styleFor(role sandboxLineRole) style.Style {
	switch role {
	case lineHeading:
		return s.heading
	case linePrimaryText:
		return s.primaryText
	case lineMuted:
		return s.muted
	case lineAccent:
		return s.accent
	case lineSuccess:
		return s.success
	case lineWarn:
		return s.warn
	case lineDanger:
		return s.danger
	default:
		return s.secondary
	}
}

func truncateSandboxSpans(spans []sandboxSpan, maxBytes int) []sandboxSpan {
	if maxBytes <= 0 {
		return spans
	}
	total := 0
	for _, sp := range spans {
		total += len(sp.text)
	}
	if total <= maxBytes {
		return spans
	}
	out := make([]sandboxSpan, 0, len(spans))
	used := 0
	for _, sp := range spans {
		if used+len(sp.text) <= maxBytes {
			out = append(out, sp)
			used += len(sp.text)
			continue
		}
		rem := maxBytes - used
		if rem <= 0 {
			out = append(out, sandboxSpan{text: "…", role: sp.role})
		} else {
			prefix, _ := strutils.TruncateBytes(sp.text, rem)
			out = append(out, sandboxSpan{text: prefix + "…", role: sp.role})
		}
		break
	}
	return out
}

type resolvedTabParams struct {
	lines        []sandboxLine
	pos          int
	end          int
	total        int
	contentWidth int
	styles       resolvedStyles
	row          func(style.Style, string) trail.Node
}

func resolvedTabNodes(p resolvedTabParams) []trail.Node {
	var nodes []trail.Node
	for _, l := range p.lines[p.pos:p.end] {
		if len(l.spans) == 0 {
			nodes = append(nodes, p.row(p.styles.muted, ""))
			continue
		}
		spans := truncateSandboxSpans(l.spans, p.contentWidth)
		children := make([]trail.Node, 0, len(spans))
		for _, sp := range spans {
			children = append(children, trail.Span(trail.SpanProps{
				Style: p.styles.styleFor(sp.role),
			}, trail.Text(sp.text)))
		}
		nodes = append(nodes, trail.Box(trail.BoxProps{}, children...))
	}
	if p.total > p.end-p.pos {
		nodes = append(nodes, p.row(p.styles.muted,
			fmt.Sprintf("lines %d-%d of %d", p.pos+1, p.end, p.total)))
	}
	return nodes
}
