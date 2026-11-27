package components

import (
	"maps"
	"regexp"
	"slices"
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

// StandardModelAlias is one built-in alias tier the editor always renders.
type StandardModelAlias struct {
	Name string
	Role string
}

// StandardModelAliases are the built-in tiers listed in display order.
var StandardModelAliases = []StandardModelAlias{
	{"default", "Primary session & main agent model"},
	{"fast", "Fast subagents: explore, title-generator"},
	{"balanced", "General coding & subagent tier"},
	{"smart", "Stronger reasoning, review & planning"},
	{"flagship", "Frontier reasoning ceiling"},
}

var customAliasPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// IsStandardModelAlias reports whether alias names one built-in tier.
func IsStandardModelAlias(alias string) bool {
	return slices.ContainsFunc(StandardModelAliases, func(s StandardModelAlias) bool { return s.Name == alias })
}

// ModelAliasEditorProps describes the target model whose aliases are edited.
// The card is presentational: the parent command loads assignments and
// performs the save, so /model and /providers share one editor and one daemon
// endpoint.
type ModelAliasEditorProps struct {
	Provider string
	// ModelID is the raw manifest model id (not the qualified "provider/model" form).
	ModelID string
	// ModelName is the display name shown in the header.
	ModelName string
	// ManifestPath is the home-shortened owning *.provider.yaml path.
	ManifestPath string
	// Scope is the owning provider's scope badge ("User" or "Workspace").
	Scope string
	// DefaultModel is the manifest's spec.defaultModels.completion value.
	DefaultModel string
	// Assignments is the live workspace-wide alias report. Loading renders the
	// muted placeholder while the parent fetches it.
	Assignments []api.AliasAssignment
	Loading     bool
	Saving      bool
	// ErrorMessage renders inline when the parent's load or save failed.
	ErrorMessage string
	OnSave       func(req api.AssignModelAliasesRequest)
	OnCancel     func()
}

// aliasEditorRow is one staged checklist row.
type aliasEditorRow struct {
	alias   string // bare alias, no "@"
	role    string // standard-alias role text shown beside the name
	state   string // live assignment state text
	warn    bool   // state renders as a warning
	checked bool   // staged: alias will be owned by this model after save
}

// aliasEditorStaged holds the user's toggles before save. A present alias
// overrides the live ownership with the staged checkbox; addedNames are
// freshly typed custom aliases that have no live owner yet.
type aliasEditorStaged struct {
	toggles   map[string]bool
	added     []string
	inputOpen bool
	inputVal  string
	notice    string
}

type modelAliasEditorStyles struct {
	styles.Common
	row, gutter, name, state, warning style.Style
}

func newModelAliasEditorStyles(s *theme.Scheme) modelAliasEditorStyles {
	c := styles.NewCommon(s)
	return modelAliasEditorStyles{
		Common: c,
		row: c.Row.Width(style.Percent(100)).
			Overflow(style.OverflowHidden),
		gutter: c.Muted.WhiteSpace(style.WhiteSpacePre).
			Flex(0, 0),
		name: c.Primary.WhiteSpace(style.WhiteSpacePre).
			Flex(1, 1).
			MinWidth(style.Cells(0)).
			Overflow(style.OverflowHidden),
		warning: c.Warning.Flex(0, 0).
			WhiteSpace(style.WhiteSpacePre).
			Overflow(style.OverflowHidden),
		state: c.Muted.Flex(0, 0).
			WhiteSpace(style.WhiteSpacePre).
			Overflow(style.OverflowHidden),
	}
}

// ModelAliasEditor renders the inline alias checklist card shared by /model
// and /providers. Space toggles a row, n adds a custom alias, Enter saves the
// staged set to the owning manifest, Esc cancels.
var ModelAliasEditor = trail.FC("ModelAliasEditor", func(props ModelAliasEditorProps) trail.Node {
	sheet := styles.Use(newModelAliasEditorStyles)
	staged, setStaged := kitex.UseState(aliasEditorStaged{toggles: map[string]bool{}})
	cursor, setCursor := kitex.UseState(0)
	inputRef := kitex.UseRef[dom.Element](nil)

	trail.UseFocus(inputRef, trail.FocusOptions{AutoFocus: true, IsActive: staged().inputOpen})

	byAlias := make(map[string]api.AliasAssignment, len(props.Assignments))
	for _, a := range props.Assignments {
		byAlias[a.Alias] = a
	}
	defaultHere := strings.TrimSpace(props.DefaultModel) != "" && props.DefaultModel == props.ModelID

	// ownerHere reports whether this model declares the alias, including when
	// a manual YAML collision made it one of several owners.
	ownerHere := func(a api.AliasAssignment) bool {
		if a.Provider == props.Provider && a.Model == props.ModelID {
			return true
		}
		return slices.Contains(a.Duplicates, props.Provider+"/"+props.ModelID)
	}
	// duplicateText names the colliding owners other than this model.
	duplicateText := func(a api.AliasAssignment) string {
		if !a.Duplicate {
			return ""
		}
		target := props.Provider + "/" + props.ModelID
		var others []string
		if primary := a.Provider + "/" + a.Model; a.Provider != "" && primary != target {
			others = append(others, primary)
		}
		for _, d := range a.Duplicates {
			if d != target {
				others = append(others, d)
			}
		}
		if len(others) == 0 {
			return ""
		}
		return "⚠ duplicate on " + strings.Join(others, ", ")
	}

	// buildRows composes the checklist from live assignments plus staged
	// toggles: a row's checkbox reflects the staged decision once touched,
	// otherwise live ownership.
	buildRows := func(st aliasEditorStaged) []aliasEditorRow {
		rows := make([]aliasEditorRow, 0, len(StandardModelAliases)+4)
		for _, std := range StandardModelAliases {
			row := aliasEditorRow{alias: std.Name, role: std.Role}
			a, assigned := byAlias[std.Name]
			if std.Name == "default" {
				owned := defaultHere
				checked := owned
				if touched, ok := st.toggles["default"]; ok {
					checked = touched
				}
				row.checked = checked
				hasDefaultOwner := strings.TrimSpace(a.Provider) != ""
				switch {
				case a.WorkspaceDefault:
					row.state = "⚠ WORKSPACE.md overrides @default"
					row.warn = true
				case owned && hasDefaultOwner && (a.Provider != props.Provider || a.Model != props.ModelID):
					row.state = "⚠ " + a.Provider + "/" + a.Model + " wins @default — saving moves it here"
					row.warn = true
				case owned:
					row.state = "assigned to this model"
				case !hasDefaultOwner:
					row.state = "unassigned"
				case checked:
					row.state = "moves from " + a.Provider + "/" + a.Model
				default:
					row.state = "currently " + a.Provider + "/" + a.Model
				}
				// The WORKSPACE.md override outranks manifest collisions:
				// saving cannot move the alias, so keep the override warning.
				if dup := duplicateText(a); dup != "" && !a.WorkspaceDefault {
					if checked {
						dup += " — saving assigns only here"
					}
					row.state = dup
					row.warn = true
				}
				rows = append(rows, row)
				continue
			}
			owned := assigned && ownerHere(a)
			checked := owned
			if touched, ok := st.toggles[std.Name]; ok {
				checked = touched
			}
			row.checked = checked
			switch {
			case a.WorkspaceDefault:
				row.state = "⚠ WORKSPACE.md overrides @" + std.Name
				row.warn = true
			case owned && !a.Duplicate:
				row.state = "assigned to this model"
			case !assigned:
				row.state = "unassigned"
			case checked:
				row.state = "moves from " + a.Provider + "/" + a.Model
			default:
				row.state = "currently " + a.Provider + "/" + a.Model
			}
			if dup := duplicateText(a); dup != "" && !a.WorkspaceDefault {
				if checked {
					dup += " — saving assigns only here"
				}
				row.state = dup
				row.warn = true
			}
			rows = append(rows, row)
		}

		seen := map[string]bool{}
		for _, a := range props.Assignments {
			if IsStandardModelAlias(a.Alias) || !ownerHere(a) || seen[a.Alias] {
				continue
			}
			seen[a.Alias] = true
			checked := true
			if touched, ok := st.toggles[a.Alias]; ok {
				checked = touched
			}
			row := aliasEditorRow{alias: a.Alias, checked: checked, state: "assigned to this model"}
			if a.WorkspaceDefault {
				row.state = "⚠ WORKSPACE.md overrides @" + a.Alias
				row.warn = true
			} else if dup := duplicateText(a); dup != "" {
				if checked {
					dup += " — saving assigns only here"
				}
				row.state = dup
				row.warn = true
			}
			rows = append(rows, row)
		}
		for _, name := range st.added {
			if seen[name] {
				continue
			}
			seen[name] = true
			checked, touched := st.toggles[name]
			row := aliasEditorRow{alias: name, checked: !touched || checked, state: "new alias"}
			if a, ok := byAlias[name]; ok && a.Provider != "" {
				// A typed alias owned elsewhere stages as a move; unchecking it
				// leaves it with the current owner.
				switch {
				case a.WorkspaceDefault:
					row.state = "⚠ WORKSPACE.md overrides @" + name
					row.warn = true
				case row.checked:
					row.state = "moves from " + a.Provider + "/" + a.Model
				default:
					row.state = "currently " + a.Provider + "/" + a.Model
				}
				if dup := duplicateText(a); dup != "" && !a.WorkspaceDefault {
					if row.checked {
						dup += " — saving assigns only here"
					}
					row.state = dup
					row.warn = true
				}
			}
			rows = append(rows, row)
		}
		return rows
	}

	rows := buildRows(staged())
	cur := min(cursor(), max(0, len(rows)-1))

	toggle := func(idx int) {
		if idx < 0 || idx >= len(rows) || props.Saving {
			return
		}
		row := rows[idx]
		next := aliasEditorStaged{
			toggles:   map[string]bool{},
			added:     slices.Clone(staged().added),
			inputOpen: false,
		}
		maps.Copy(next.toggles, staged().toggles)
		next.toggles[row.alias] = !row.checked
		setStaged(next)
	}

	submit := func() {
		if props.Saving || props.OnSave == nil {
			return
		}
		var aliases []string
		defaultWanted := false
		for _, row := range buildRows(staged()) {
			if !row.checked {
				continue
			}
			if row.alias == "default" {
				defaultWanted = true
				continue
			}
			aliases = append(aliases, row.alias)
		}
		// An explicitly unchecked custom alias that was never assigned cannot
		// appear, so an empty list here either clears the model or leaves only
		// @default; send the exact set so removals persist.
		props.OnSave(api.AssignModelAliasesRequest{
			Provider:    props.Provider,
			Model:       props.ModelID,
			Aliases:     aliases,
			DefaultHere: defaultWanted,
		})
	}

	addAlias := func() {
		next := staged()
		name := strings.TrimPrefix(strings.TrimSpace(next.inputVal), "@")
		next.inputOpen = false
		next.inputVal = ""
		next.notice = ""
		next.toggles = maps.Clone(next.toggles)
		switch {
		case !customAliasPattern.MatchString(name):
			next.notice = "Custom aliases must be lowercase letters, digits, '-' or '_'."
		case slices.Contains(next.added, name) || ownerHere(byAlias[name]):
			next.toggles[name] = true
		case IsStandardModelAlias(name):
			next.notice = "@" + name + " is a standard alias — toggle it above instead."
		default:
			next.added = append(slices.Clone(next.added), name)
			next.toggles[name] = true
		}
		setStaged(next)
	}
	actions := []uicomponents.KeyAction{
		{Keys: []string{"up", "ctrl+p", "k"}, Hint: "↑/↓", Label: "Select", OnPress: func() { setCursor(max(0, cur-1)) }},
		{Keys: []string{"down", "ctrl+n", "j"}, OnPress: func() { setCursor(min(len(rows)-1, cur+1)) }},
		{Keys: []string{"space"}, Label: "Toggle", Disabled: props.Saving, OnPress: func() { toggle(cur) }},
		{Keys: []string{"n"}, Label: "Add custom alias…", Disabled: props.Saving || props.Loading, OnPress: func() {
			next := staged()
			next.inputOpen = true
			next.notice = ""
			setStaged(next)
		}},
		{Keys: []string{"enter"}, Label: "Save", Disabled: props.Saving, OnPress: submit},
		{Keys: []string{"esc", "escape"}, Label: "Cancel", OnPress: props.OnCancel},
	}
	if staged().inputOpen {
		actions = []uicomponents.KeyAction{
			{Keys: []string{"enter"}, Label: "Add", OnPress: addAlias},
			{Keys: []string{"esc", "escape"}, Label: "Cancel", OnPress: func() {
				next := staged()
				next.inputOpen = false
				next.inputVal = ""
				setStaged(next)
			}},
		}
	}
	bindings := uicomponents.UseKeyMap(actions, true)
	tone := uicomponents.ToneSuccess
	if strings.EqualFold(props.Scope, "Workspace") {
		tone = uicomponents.ToneAccent
	}
	return uicomponents.Panel(uicomponents.PanelProps{
		Header: trail.Box(trail.BoxProps{Style: sheet.Row.Gap(0, 1)},
			trail.Span(trail.SpanProps{Style: sheet.Title}, trail.Text("Model aliases · "+props.Provider+"/"+props.ModelName)),
			uicomponents.Badge(uicomponents.BadgeProps{Label: "[" + props.Scope + "]", Tone: tone}),
		),
		Error:    props.ErrorMessage,
		Notice:   staged().notice,
		Bindings: bindings,
	},
		uicomponents.KVList(uicomponents.KVListProps{Items: []uicomponents.KVItem{
			{Label: "Manifest", Value: props.ManifestPath, Tone: uicomponents.ToneMuted, OmitEmpty: true},
		}}),
		kitex.If(props.Loading, func() trail.Node {
			return trail.Span(trail.SpanProps{Style: sheet.Muted}, trail.Text("Loading alias assignments…"))
		}),
		kitex.If(!props.Loading, func() trail.Node {
			return kitex.Fragment(
				aliasChecklistSection(sheet, "Standard aliases", rows[:len(StandardModelAliases)], cur),
				aliasChecklistSection(sheet, "Custom aliases", rows[len(StandardModelAliases):], cur-len(StandardModelAliases)),
				kitex.If(staged().inputOpen, func() trail.Node {
					return trail.Box(trail.BoxProps{Style: sheet.Row},
						trail.Span(trail.SpanProps{Style: sheet.Title.WhiteSpace(style.WhiteSpacePre)}, trail.Text("  @")),
						kitex.Input(kitex.InputProps{
							Ref:   inputRef,
							Value: staged().inputVal,
							Style: style.S().Flex(1),
							OnChange: func(e event.Event) {
								if ie, ok := e.(*event.InputEvent); ok {
									next := staged()
									next.inputVal = ie.Value
									setStaged(next)
								}
							},
						}),
					)
				}),
			)
		}),
		kitex.If(props.Saving, func() trail.Node {
			return trail.Span(trail.SpanProps{Style: sheet.Muted}, trail.Text("Saving…"))
		}),
	)
})

func aliasChecklistSection(s modelAliasEditorStyles, title string, rows []aliasEditorRow, cursor int) trail.Node {
	return trail.Box(trail.BoxProps{Style: s.Column.MarginTop(1)},
		trail.Span(trail.SpanProps{Style: s.Subtitle}, trail.Text(title)),
		kitex.Map(rows, func(row aliasEditorRow, i int) trail.Node {
			mark := "[ ]"
			if row.checked {
				mark = "[x]"
			}
			label := mark + " @" + row.alias
			if row.role != "" {
				label += " · " + row.role
			}
			gutter := "  "
			if i == cursor {
				gutter = "▸ "
			}
			state := s.state
			if row.warn {
				state = s.warning
			}
			return trail.Box(trail.BoxProps{Style: s.row},
				trail.Span(trail.SpanProps{Style: s.gutter}, trail.Text(gutter)),
				trail.Span(trail.SpanProps{Style: s.name.Bold(i == cursor)}, trail.Text(label)),
				trail.Span(trail.SpanProps{Style: s.gutter}, trail.Text("  ")),
				trail.Span(trail.SpanProps{Style: state}, trail.Text(row.state)),
			)
		}),
		kitex.If(len(rows) == 0, func() trail.Node {
			return trail.Span(trail.SpanProps{Style: s.Muted}, trail.Text("  none"))
		}),
	)
}
