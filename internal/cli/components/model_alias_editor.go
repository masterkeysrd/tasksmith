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
	kind    string // "standard" or "custom"
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

// ModelAliasEditor renders the inline alias checklist card shared by /model
// and /providers. Space toggles a row, n adds a custom alias, Enter saves the
// staged set to the owning manifest, Esc cancels.
var ModelAliasEditor = trail.FC("ModelAliasEditor", func(props ModelAliasEditorProps) trail.Node {
	scheme := theme.UseTheme()
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
			row := aliasEditorRow{alias: std.Name, kind: "standard", role: std.Role}
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
			row := aliasEditorRow{alias: a.Alias, kind: "custom", checked: checked, state: "assigned to this model"}
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
			row := aliasEditorRow{alias: name, kind: "custom", checked: true, state: "new alias"}
			if a, ok := byAlias[name]; ok && a.Provider != "" {
				// A typed alias owned elsewhere stages as a move; unchecking it
				// leaves it with the current owner.
				checked := true
				if touched, okT := st.toggles[name]; okT {
					checked = touched
				}
				row.checked = checked
				switch {
				case a.WorkspaceDefault:
					row.state = "⚠ WORKSPACE.md overrides @" + name
					row.warn = true
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

	trail.UseInput(func(inputChar string, k event.KeyEvent) {
		st := staged()
		if st.inputOpen {
			switch {
			case k.MatchString("enter"):
				name := strings.TrimPrefix(strings.TrimSpace(st.inputVal), "@")
				next := aliasEditorStaged{toggles: map[string]bool{}, notice: ""}
				maps.Copy(next.toggles, st.toggles)
				current := byAlias[name]
				onThisModel := ownerHere(current)
				switch {
				case !customAliasPattern.MatchString(name):
					next.notice = "Custom aliases must be lowercase letters, digits, '-' or '_'."
				case slices.Contains(st.added, name) || onThisModel:
					next.toggles[name] = true
				case IsStandardModelAlias(name):
					next.notice = "@" + name + " is a standard alias — toggle it above instead."
				case current.Provider != "":
					// Aliasing another model's entry stages the one-step move:
					// the row appears checked as "moves from …".
					if !slices.Contains(st.added, name) {
						next.added = append(slices.Clone(st.added), name)
					}
					next.toggles[name] = true
				default:
					next.added = append(slices.Clone(st.added), name)
					next.toggles[name] = true
				}
				setStaged(next)
			case k.MatchString("esc", "escape"):
				next := st
				next.inputOpen = false
				next.inputVal = ""
				setStaged(next)
			}
			return
		}
		switch {
		case k.MatchString("esc", "escape"):
			if props.OnCancel != nil {
				props.OnCancel()
			}
		case k.MatchString("space") || inputChar == " ":
			toggle(cur)
		case k.MatchString("n") && !props.Saving:
			setStaged(aliasEditorStaged{
				toggles:   staged().toggles,
				added:     staged().added,
				inputOpen: true,
				inputVal:  "",
			})
		case k.MatchString("up", "ctrl+p", "k"):
			setCursor(max(0, cur-1))
		case k.MatchString("down", "ctrl+n", "j"):
			setCursor(min(len(rows)-1, cur+1))
		case k.MatchString("enter"):
			submit()
		}
	})

	border := style.SingleBorder().Top(true).Bottom(true).Left(false).Right(false).Color(scheme.Color.Border.Default)
	titleStyle := style.S().Bold(true).Foreground(scheme.Color.Text.Primary)
	muted := style.S().Foreground(scheme.Color.Text.Muted)

	var body []trail.Node
	badge := style.S().Bold(true).Foreground(scheme.Color.Text.Success)
	if strings.EqualFold(props.Scope, "Workspace") {
		badge = style.S().Bold(true).Foreground(scheme.Color.Text.Info)
	}
	body = append(body, trail.Box(trail.BoxProps{Style: style.S().FlexDirection(style.FlexRow)},
		trail.Span(trail.SpanProps{Style: titleStyle}, trail.Text("Model aliases · "+props.Provider+"/"+props.ModelName)),
		trail.Span(trail.SpanProps{Style: badge}, trail.Text(" ["+props.Scope+"]")),
	))
	if props.ManifestPath != "" {
		body = append(body, trail.Box(trail.BoxProps{Style: style.S().FlexDirection(style.FlexRow)},
			trail.Span(trail.SpanProps{Style: muted}, trail.Text(props.ManifestPath))))
	}

	if props.Loading {
		body = append(body, trail.Box(trail.BoxProps{Style: style.S().FlexDirection(style.FlexRow)},
			trail.Span(trail.SpanProps{Style: muted}, trail.Text("Loading alias assignments…"))))
	} else {
		body = append(body, aliasChecklistSection(scheme, "Standard aliases", rows, "standard", cur))
		body = append(body, aliasChecklistSection(scheme, "Custom aliases", rows, "custom", cur))
		if !staged().inputOpen {
			body = append(body, trail.Box(trail.BoxProps{Style: style.S().FlexDirection(style.FlexRow).Margin(0, 0, 0, 2)},
				trail.Span(trail.SpanProps{Style: muted}, trail.Text("n  Add custom alias…"))))
		} else {
			body = append(body, trail.Box(trail.BoxProps{Style: style.S().FlexDirection(style.FlexRow)},
				trail.Span(trail.SpanProps{Style: titleStyle.WhiteSpace(style.WhiteSpacePre)}, trail.Text("  @")),
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
			))
		}
	}

	if notice := staged().notice; notice != "" {
		body = append(body, trail.Box(trail.BoxProps{Style: style.S().FlexDirection(style.FlexRow)},
			trail.Span(trail.SpanProps{Style: style.S().Foreground(scheme.Color.Text.Warning)}, trail.Text(notice))))
	}
	if props.ErrorMessage != "" {
		body = append(body, trail.Box(trail.BoxProps{Style: style.S().FlexDirection(style.FlexRow)},
			trail.Span(trail.SpanProps{Style: style.S().Foreground(scheme.Color.Text.Error)}, trail.Text(props.ErrorMessage))))
	}
	if props.Saving {
		body = append(body, trail.Box(trail.BoxProps{Style: style.S().FlexDirection(style.FlexRow)},
			trail.Span(trail.SpanProps{Style: muted}, trail.Text("Saving…"))))
	}

	body = append(body, uicomponents.KeyboardHint(uicomponents.KeyboardHintProps{
		Bindings: []uicomponents.KeyBinding{
			{Key: "↑/↓", Label: "Select"},
			{Key: "space", Label: "Toggle"},
			{Key: "n", Label: "Add custom"},
			{Key: "enter", Label: "Save"},
			{Key: "esc", Label: "Cancel"},
		},
	}))

	return trail.Box(trail.BoxProps{
		Style: style.S().Border(border).Width(style.Percent(100)).FlexDirection(style.FlexColumn).Padding(0, 0, 0, 1),
	}, body...)
})

// aliasChecklistSection renders one titled group of the checklist. The cursor
// marker is shared across groups, so only the row at the global index is
// highlighted even though each group is rendered separately.
func aliasChecklistSection(scheme *theme.Scheme, title string, rows []aliasEditorRow, kind string, cursor int) trail.Node {
	var nodes []trail.Node
	nodes = append(nodes, trail.Box(trail.BoxProps{Style: style.S().FlexDirection(style.FlexRow).Margin(1, 0, 0, 0)},
		trail.Span(trail.SpanProps{Style: style.S().Bold(true).Foreground(scheme.Color.Text.Secondary)}, trail.Text(title))))
	shown := 0
	for i, row := range rows {
		if row.kind != kind {
			continue
		}
		shown++
		box := "[x]"
		if !row.checked {
			box = "[ ]"
		}
		stateStyle := style.S().Foreground(scheme.Color.Text.Muted)
		if row.warn {
			stateStyle = style.S().Foreground(scheme.Color.Text.Warning)
		}
		gutter := "  "
		nameStyle := style.S().Foreground(scheme.Color.Text.Primary)
		if i == cursor {
			gutter = "▸ "
			nameStyle = nameStyle.Bold(true)
		}
		label := box + " @" + row.alias
		if row.role != "" {
			label += " · " + row.role
		}
		nodes = append(nodes, trail.Box(trail.BoxProps{Style: style.S().FlexDirection(style.FlexRow).Width(style.Percent(100)).Overflow(style.OverflowHidden)},
			trail.Span(trail.SpanProps{Style: style.S().Foreground(scheme.Color.Text.Muted).WhiteSpace(style.WhiteSpacePre).Flex(0, 0)}, trail.Text(gutter)),
			trail.Span(trail.SpanProps{Style: nameStyle.WhiteSpace(style.WhiteSpacePre).Flex(1, 1).MinWidth(style.Cells(0)).Overflow(style.OverflowHidden)}, trail.Text(label)),
			trail.Span(trail.SpanProps{Style: style.S().Foreground(scheme.Color.Text.Muted).WhiteSpace(style.WhiteSpacePre).Flex(0, 0)}, trail.Text("  ")),
			trail.Span(trail.SpanProps{Style: stateStyle.Flex(0, 0).WhiteSpace(style.WhiteSpacePre).Overflow(style.OverflowHidden)}, trail.Text(row.state)),
		))
	}
	if shown == 0 {
		nodes = append(nodes, trail.Box(trail.BoxProps{Style: style.S().FlexDirection(style.FlexRow)},
			trail.Span(trail.SpanProps{Style: style.S().Foreground(scheme.Color.Text.Muted)}, trail.Text("  none"))))
	}
	return trail.Box(trail.BoxProps{Style: style.S().FlexDirection(style.FlexColumn)}, nodes...)
}
