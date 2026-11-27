package components

import (
	"cmp"
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
	uicomponents "github.com/masterkeysrd/tasksmith/internal/ui/components"
	"github.com/masterkeysrd/tasksmith/internal/ui/styles"
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

func sandboxTabMeta(active sandboxEditorTab, scopeTitle string, saving bool) string {
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
	return strings.Join(metaParts, "  · ")
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
	sheet := styles.Use(newSandboxEditorStyles)
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
	cursor := uicomponents.UseCursor(maxRows, 0)
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

	tabs := []uicomponents.TabItem{
		{Label: "Policy"},
		{Label: fmt.Sprintf("Exclusions (%d)", len(excluded))},
		{Label: "Effective"},
	}
	switchTab := func(i int) {
		setTab(sandboxEditorTab(i))
		cursor.Set(0)
		setOffset(0)
		clearError()
	}
	actions := uicomponents.TabKeyActions(len(tabs), int(tab()), switchTab, inInput())
	actions[0].Keys = append(actions[0].Keys, "right")
	actions[0].Hint = "←/→"
	actions[0].Label = "Tab"
	actions[1].Keys = append(actions[1].Keys, "left")
	actions = append(actions, uicomponents.KeyAction{
		Keys:     []string{"s"},
		Label:    "Scope",
		Disabled: tab() == sandboxTabResolved,
		OnPress: func() {
			setScopeIndex(1 - scopeIndex())
			cursor.Set(0)
			clearError()
		},
	})
	move := cursor.Move
	moveLabel := "Move"
	if tab() == sandboxTabResolved {
		moveLabel = "Scroll"
		move = func(delta int) { setOffset(max(0, min(maxOffset, pos+delta))) }
	}
	actions = append(actions, []uicomponents.KeyAction{
		{Keys: []string{"up", "k", "ctrl+p"}, Hint: "↑/↓", Label: moveLabel, OnPress: func() { move(-1) }},
		{Keys: []string{"down", "j", "ctrl+n"}, OnPress: func() { move(1) }},
	}...)
	switch tab() {
	case sandboxTabPolicy:
		row := policyRows[cur]
		update := func(reset bool) {
			if row.knob == "" {
				if props.OnSetMode != nil {
					var mode api.SandboxMode
					if !reset {
						mode = nextSandboxMode(scopeSettings.Mode, props.Response.Effective.Mode)
					}
					props.OnSetMode(activeScope, mode)
				}
				return
			}
			if activeLocked[row.knob] != "" || props.OnToggleKnob == nil {
				return
			}
			var value *bool
			if !reset {
				base := cmp.Or(knobValue(scopeSettings, row.knob), knobValue(props.Response.Effective, row.knob))
				if base == nil {
					return
				}
				next := !*base
				value = &next
			}
			props.OnToggleKnob(activeScope, row.knob, value)
		}
		actions = append(actions, []uicomponents.KeyAction{
			{Keys: []string{"space", "enter", "t"}, Label: "Toggle", OnPress: func() { update(false) }},
			{Keys: []string{"x", "X", "backspace", "delete"}, Label: "Reset", OnPress: func() { update(true) }},
		}...)
	case sandboxTabExcluded:
		open := func(pattern string) {
			setEditing(pattern)
			setInputText(pattern)
			setInInput(true)
			clearError()
		}
		actions = append(actions, []uicomponents.KeyAction{
			{Keys: []string{"a", "A"}, Label: "Add", OnPress: func() { open("") }},
			{Keys: []string{"e", "E"}, Label: "Edit", Disabled: len(excluded) == 0, OnPress: func() { open(excluded[cur]) }},
			{Keys: []string{"d", "D", "backspace", "delete"}, Label: "Delete", Disabled: len(excluded) == 0, OnPress: func() {
				if props.OnRemoveExcluded != nil {
					props.OnRemoveExcluded(activeScope, excluded[cur])
				}
			}},
		}...)
	case sandboxTabResolved:
		actions = append(actions, []uicomponents.KeyAction{
			{Keys: []string{"pgup"}, Hint: "pgup/pgdn", Label: "Page", OnPress: func() { move(-viewport) }},
			{Keys: []string{"pgdown", "space"}, OnPress: func() { move(viewport) }},
			{Keys: []string{"g"}, OnPress: func() { setOffset(0) }},
			{Keys: []string{"G", "shift+g"}, OnPress: func() { setOffset(maxOffset) }},
		}...)
	}
	actions = append(actions, uicomponents.KeyAction{Keys: []string{"esc", "escape"}, Label: "Close", OnPress: props.OnClose})
	bindings := uicomponents.UseKeyMap(actions, !inInput())

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

	var body trail.Node
	switch tab() {
	case sandboxTabPolicy:
		body = policyTabNodes(policyTabParams{
			scopeTitle:    scopeTitle,
			scopeSettings: scopeSettings,
			effective:     props.Response.Effective,
			sessionMode:   props.SessionMode,
			availability:  avail,
			restricted:    props.Response.Restricted,
			locked:        activeLocked,
			cursor:        cur,
			styles:        sheet.editorStyles,
			row:           row,
			wrapColumn: func(text string, indent int) trail.Node {
				return trail.Box(trail.BoxProps{
					Style: sheet.column.PaddingLeft(indent),
				}, trail.Span(trail.SpanProps{Style: sheet.wrap}, trail.Text(text)))
			},
		})

	case sandboxTabExcluded:
		listHeight := max(winSize.Height-12, 5)
		start, end := windowIndex(cur, len(excluded), listHeight)
		body = excludedTabNodes(excludedTabParams{
			scopeTitle:  scopeTitle,
			excluded:    excluded,
			inherited:   inherited,
			showInherit: activeScope == api.ScopeProject && len(inherited) > 0,
			cursor:      cur,
			start:       start,
			end:         end,
			warnOff:     effExclusionsOff,
			styles:      sheet.editorStyles,
			row:         row,
		})

	case sandboxTabResolved:
		body = resolvedTabNodes(resolvedTabParams{
			lines:        resolvedContent,
			pos:          pos,
			end:          min(len(resolvedContent), pos+viewport),
			total:        len(resolvedContent),
			contentWidth: contentWidth,
			styles:       sheet.resolvedStyles,
			row:          row,
		})
	}

	var prompt trail.Node
	if inInput() {
		bindings = []uicomponents.KeyBinding{{Key: "enter", Label: "Save"}, {Key: "esc", Label: "Cancel"}}
		inputTitle := fmt.Sprintf("Add excluded command pattern (%s):", scopeTitle)
		if editing() != "" {
			inputTitle = fmt.Sprintf("Edit excluded command pattern (%s):", scopeTitle)
		}
		prompt = kitex.Fragment(
			trail.Box(trail.BoxProps{}, trail.Span(trail.SpanProps{Style: sheet.primary}, trail.Text(inputTitle))),
			trail.Box(trail.BoxProps{
				Style: style.S().
					Display(style.DisplayFlex).
					FlexDirection(style.FlexRow).
					AlignItems(style.AlignCenter).
					Width(style.Percent(100)),
			},
				trail.Span(trail.SpanProps{Style: sheet.primary}, trail.Text("❯ ")),
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
			trail.Box(trail.BoxProps{}, trail.Span(trail.SpanProps{Style: sheet.editorStyles.muted},
				trail.Text("Examples: git commit *, docker build *, make release"))),
		)
	}
	return uicomponents.Panel(uicomponents.PanelProps{
		Header:   row(sheet.primary, "Sandbox"),
		Error:    cmp.Or(localError(), props.ErrorMessage),
		Bindings: bindings,
	}, uicomponents.TabBar(uicomponents.TabBarProps{
		Tabs: tabs, Active: int(tab()), Numbered: true,
		Meta: sandboxTabMeta(tab(), scopeTitle, props.Saving), MarginBottom: 1,
	}), body, prompt)
})

func containsPattern(patterns []string, pattern string) bool {
	return slices.Contains(patterns, pattern)
}

type editorStyles struct {
	primary   style.Style
	secondary style.Style
	muted     style.Style
	warn      style.Style
}

type sandboxEditorStyles struct {
	editorStyles
	resolvedStyles
	column, wrap style.Style
}

func newSandboxEditorStyles(s *theme.Scheme) sandboxEditorStyles {
	c := styles.NewCommon(s)
	pre := style.WhiteSpacePre
	heading := style.S().Foreground(s.Color.Icon.Primary).
		Bold(true).
		WhiteSpace(pre)
	muted, secondary := c.Muted.WhiteSpace(pre), c.Secondary.WhiteSpace(pre)
	return sandboxEditorStyles{
		editorStyles: editorStyles{primary: heading, secondary: secondary, muted: muted, warn: c.Warning.Bold(true)},
		resolvedStyles: resolvedStyles{
			heading:     heading,
			primaryText: c.Primary.WhiteSpace(pre),
			secondary:   secondary,
			muted:       muted,
			accent:      c.Accent.WhiteSpace(pre),
			success:     c.Success.WhiteSpace(pre),
			warn:        c.Warning.WhiteSpace(pre),
			danger:      c.Error.WhiteSpace(pre),
		},
		column: c.Column.Width(style.Percent(100)), wrap: c.Muted.OverflowWrap(style.OverflowWrapAnywhere),
	}
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

func policyTabNodes(p policyTabParams) trail.Node {
	session := "none"
	if p.sessionMode != "" {
		session = fmt.Sprintf("%s sandbox (mode edits apply to new sessions)", p.sessionMode)
	}
	backend := uicomponents.KVItem{Label: "Host backend", Value: "⚠ unavailable — sessions run with local isolation", Tone: uicomponents.ToneWarning}
	if p.availability.Available {
		backend.Value = "✔ " + p.availability.Backend + " available"
		backend.Tone = uicomponents.ToneSuccess
	} else if p.availability.Backend != "" {
		backend.Value = "⚠ " + p.availability.Backend + " unavailable"
	}
	trust := uicomponents.KVItem{Label: "Workspace trust", Value: "✔ trusted", Tone: uicomponents.ToneSuccess}
	if p.restricted {
		trust.Value = "⚠ untrusted (unsandboxed execution disabled)"
		trust.Tone = uicomponents.ToneWarning
	}
	rows := sandboxPolicyRows()
	selectedRow := rows[p.cursor]
	detail := p.row(p.styles.muted, "  key: "+selectedRow.key)
	if p.locked[selectedRow.knob] != "" {
		detail = p.wrapColumn("key: "+selectedRow.key+" · locked: edit that scope (s) or its pinning setting to change this", 2)
	}
	return kitex.Fragment(
		uicomponents.KVList(uicomponents.KVListProps{MinLabelWidth: 17, Items: []uicomponents.KVItem{{Label: "Active session", Value: session}, backend}}),
		kitex.If(!p.availability.Available && p.availability.Error != "", func() trail.Node { return p.row(p.styles.muted, "  error: "+p.availability.Error) }),
		kitex.If(!p.availability.Available && p.availability.Remediation != "", func() trail.Node { return p.wrapColumn("Fix: "+p.availability.Remediation, 0) }),
		uicomponents.KVList(uicomponents.KVListProps{MinLabelWidth: 17, Items: []uicomponents.KVItem{trust}}),
		p.row(p.styles.muted, ""),
		p.row(p.styles.muted, policyRowText("Setting", "Effective", p.scopeTitle+" override")),
		kitex.Map(rows, func(r sandboxPolicyRow, i int) trail.Node {
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
			return p.row(rowStyle, mark+policyRowText(r.label, effectiveCell, overrideCell))
		}),
		p.row(p.styles.muted, ""),
		p.wrapColumn(selectedRow.desc, 2),
		detail,
	)
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

func excludedTabNodes(p excludedTabParams) trail.Node {
	return kitex.Fragment(
		kitex.If(p.warnOff, func() trail.Node {
			return p.row(p.styles.warn, `⚠ Inactive: "Allow unsandboxed" is off, so excluded commands still run sandboxed.`)
		}),
		p.row(p.styles.secondary, fmt.Sprintf("Excluded commands — %s (%d)", p.scopeTitle, len(p.excluded))),
		kitex.If(len(p.excluded) == 0, func() trail.Node {
			return p.row(p.styles.secondary, `No excluded commands. Press a to add one (e.g. "git commit *").`)
		}),
		kitex.If(p.start > 0, func() trail.Node {
			return p.row(p.styles.muted, fmt.Sprintf("  ↑ %d more", p.start))
		}),
		kitex.Map(p.excluded[p.start:p.end], func(pattern string, i int) trail.Node {
			if p.start+i == p.cursor {
				return p.row(p.styles.primary, "❯ "+pattern)
			}
			return p.row(p.styles.secondary, "  "+pattern)
		}),
		kitex.If(p.end < len(p.excluded), func() trail.Node {
			return p.row(p.styles.muted, fmt.Sprintf("  ↓ %d more", len(p.excluded)-p.end))
		}),
		kitex.If(p.showInherit, func() trail.Node {
			return kitex.Fragment(
				p.row(p.styles.muted, ""),
				p.row(p.styles.muted, fmt.Sprintf("Inherited from Global (%d) — press s to edit Global", len(p.inherited))),
				kitex.Map(p.inherited, func(pattern string, _ int) trail.Node { return p.row(p.styles.muted, "  "+pattern) }),
			)
		}),
	)
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

func resolvedTabNodes(p resolvedTabParams) trail.Node {
	return kitex.Fragment(
		kitex.Map(p.lines[p.pos:p.end], func(line sandboxLine, _ int) trail.Node {
			if len(line.spans) == 0 {
				return p.row(p.styles.muted, "")
			}
			return trail.Box(trail.BoxProps{}, kitex.Map(truncateSandboxSpans(line.spans, p.contentWidth), func(span sandboxSpan, _ int) trail.Node {
				return trail.Span(trail.SpanProps{Style: p.styles.styleFor(span.role)}, trail.Text(span.text))
			}))
		}),
		kitex.If(p.total > p.end-p.pos, func() trail.Node {
			return p.row(p.styles.muted, fmt.Sprintf("lines %d-%d of %d", p.pos+1, p.end, p.total))
		}),
	)
}
