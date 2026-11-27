package command

import (
	"context"
	"fmt"
	"strings"

	"github.com/masterkeysrd/kite/extras/flight"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/components"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	uicomponents "github.com/masterkeysrd/tasksmith/internal/ui/components"
	"github.com/masterkeysrd/tasksmith/internal/ui/format"
	"github.com/masterkeysrd/tasksmith/internal/ui/styles"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

func (r *Registry) registerChangesCommands() {
	r.Register(Command{
		Name:        "changes",
		Usage:       "/changes",
		Description: "List and inspect tracked file changes",
		Handler:     handleChanges,
	})
	r.Register(Command{
		Name:        "revert",
		Usage:       "/revert [path] [entry_id]",
		Description: "Revert tracked changes for a file or the entire session",
		Handler:     handleRevert,
	})
}

func handleChanges(_ []string) error {
	sess := store.GetSession().Session
	if sess == nil || sess.ID == "" {
		store.AppendCommandOutput("/changes", "No active session.")
		return nil
	}

	store.EnqueueSlot(store.SlotItem{
		Kind: store.SlotKindPicker,
		Data: ChangesBrowser,
	})
	return nil
}

func handleRevert(args []string) error {
	sess := store.GetSession().Session
	if sess == nil || sess.ID == "" {
		store.AppendCommandOutput("/revert", "No active session.")
		return nil
	}

	c := store.Client()
	if c == nil {
		store.AppendNotice("Client unavailable.")
		return nil
	}

	var path, entryID string
	if len(args) > 0 {
		path = strings.TrimSpace(args[0])
	}
	if len(args) > 1 {
		entryID = strings.TrimSpace(args[1])
	}

	go func() {
		err := c.Revert(context.Background(), sess.ID, api.RevertRequest{
			Path:    path,
			EntryID: entryID,
		})
		if err != nil {
			store.AppendError("Revert Error", err.Error())
			return
		}
		cmdName := "/revert"
		if path != "" {
			cmdName += " " + path
		}
		if entryID != "" {
			cmdName += " " + entryID
		}
		store.AppendCommandOutput(cmdName, "Changes reverted.")
	}()
	return nil
}

func aggregateFileChanges(entries []api.FileChange) []api.FileChange {
	type group struct {
		first, last    api.FileChange
		added, removed int
	}
	var paths []string
	groups := make(map[string]*group)
	for _, ch := range entries {
		g := groups[ch.Path]
		if g == nil {
			paths = append(paths, ch.Path)
			g = &group{first: ch}
			groups[ch.Path] = g
		}
		g.last = ch
		if ch.Tool == "revert" {
			g.added, g.removed = 0, 0
		} else {
			g.added += ch.Additions
			g.removed += ch.Deletions
		}
	}

	var result []api.FileChange
	for _, path := range paths {
		g := groups[path]
		first, last := g.first, g.last
		currentHash := last.AfterHash
		if last.Kind == api.KindDeleted {
			currentHash = ""
		}
		if first.BeforeHash == currentHash && (first.Kind != api.KindCreated || last.Kind == api.KindDeleted) {
			continue
		}
		kind := api.KindModified
		switch {
		case last.Kind == api.KindDeleted:
			kind = api.KindDeleted
		case first.Kind == api.KindCreated || first.BeforeHash == "":
			kind = api.KindCreated
		}
		result = append(result, api.FileChange{
			ID: last.ID, Path: path, Kind: kind,
			Additions: g.added, Deletions: g.removed, Timestamp: last.Timestamp,
		})
	}
	return result
}

type changeDiffProps struct {
	sessionID string
	path      string
	kind      api.ChangeKind
}

var changeDiffViewer = trail.FC("ChangeDiffViewer", func(props changeDiffProps) trail.Node {
	nav := flight.UseNavigation()
	bindings := uicomponents.UseKeyMap([]uicomponents.KeyAction{
		{Keys: []string{"esc", "escape"}, Label: "Return to changes", OnPress: nav.Pop},
	}, true)
	type result struct{ text, err string }
	diff, setDiff := kitex.UseState[*result](nil)
	kitex.UseEffectCleanup(func() func() {
		c := store.Client()
		if c == nil {
			setDiff(&result{err: "Error loading diff: client unavailable"})
			return nil
		}
		ctx, cancel := context.WithCancel(context.Background())
		setDiff(nil)
		go func() {
			text, err := c.DiffChange(ctx, props.sessionID, props.path, "")
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				setDiff(&result{err: "Error loading diff: " + err.Error()})
			} else {
				setDiff(&result{text: text})
			}
		}()
		return cancel
	}, []any{props.sessionID, props.path})

	panel := uicomponents.PanelProps{Title: fmt.Sprintf("Diff for %s (%s):", props.path, props.kind), Bindings: bindings}
	data := diff()
	if data == nil {
		panel.Notice = "Loading diff..."
	} else {
		panel.Error = data.err
	}
	return uicomponents.Panel(panel, kitex.If(data != nil && data.err == "", func() trail.Node {
		return components.DiffView(components.DiffProps{Text: data.text, Path: props.path})
	}))
})

type changesListRoute struct{}

type changeDiffRoute struct{ Path string }

// ChangesBrowser renders an interactive changes list and inline diff viewer inside <Slot />.
var ChangesBrowser = trail.FC("ChangesBrowser", func(slotID string) trail.Node {
	sessID := store.UseSession(func(s store.SessionState) string {
		if s.Session != nil {
			return s.Session.ID
		}
		return ""
	})
	c := store.Client()
	changes, setChanges := kitex.UseState[[]api.FileChange](nil)
	loading, setLoading := kitex.UseState(true)
	generation, refresh := kitex.UseState(0)
	kitex.UseEffectCleanup(func() func() {
		switch {
		case sessID == "":
			store.RemoveSlot(slotID)
			store.AppendCommandOutput("/changes", "No active session.")
			return nil
		case c == nil:
			store.RemoveSlot(slotID)
			store.AppendNotice("Client unavailable.")
			return nil
		}
		ctx, cancel := context.WithCancel(context.Background())
		setLoading(true)
		go func() {
			entries, err := c.Changes(ctx, sessID)
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				store.RemoveSlot(slotID)
				store.AppendError("Error", err.Error())
				return
			}
			setChanges(aggregateFileChanges(entries))
			setLoading(false)
		}()
		return cancel
	}, []any{sessID, c, generation()})

	return changesStack(changesListProps{
		slotID:    slotID,
		sessionID: sessID,
		changes:   changes(),
		loading:   loading(),
		revert: func(path string) {
			go func() {
				if err := c.Revert(context.Background(), sessID, api.RevertRequest{Path: path}); err != nil {
					store.AppendError("Revert Error", err.Error())
					return
				}
				refresh(generation() + 1)
			}()
		},
	})
})

type changesListProps struct {
	slotID, sessionID string
	changes           []api.FileChange
	loading           bool
	revert            func(string)
}

func changesStack(props changesListProps) trail.Node {
	return flight.Stack(flight.StackProps{
		InitialRoute: changesListRoute{},
		RenderRoute: func(route flight.Route) trail.Node {
			if detail, ok := route.(changeDiffRoute); ok {
				for _, ch := range props.changes {
					if ch.Path == detail.Path {
						return changeDiffViewer(changeDiffProps{sessionID: props.sessionID, path: ch.Path, kind: ch.Kind})
					}
				}
				return missingChange()
			}
			var content trail.Node
			if props.loading {
				content = loadingHint("Loading changes...")
			} else {
				content = changesList(props)
			}
			return trail.Box(trail.BoxProps{}, content)
		},
	})
}

var missingChange = trail.SimpleFC("MissingChange", func() trail.Node {
	nav := flight.UseNavigation()
	kitex.UseEffect(nav.Pop, []any{})
	return trail.Box(trail.BoxProps{})
})

type changesListStyles struct {
	stats, added, removed, separator style.Style
}

func newChangesListStyles(s *theme.Scheme) changesListStyles {
	common := styles.NewCommon(s)
	return changesListStyles{
		stats:     style.S().WhiteSpace(style.WhiteSpacePre),
		added:     style.S().Foreground(s.Color.Diff.AddedText),
		removed:   style.S().Foreground(s.Color.Diff.RemovedText),
		separator: common.Secondary,
	}
}

var changesList = trail.FC("ChangesList", func(props changesListProps) trail.Node {
	nav := flight.UseNavigation()
	sheet := styles.Use(newChangesListStyles)
	closeList := func() { store.RemoveSlot(props.slotID) }
	bindings := uicomponents.UseKeyMap([]uicomponents.KeyAction{
		{Keys: []string{"esc", "escape"}, Label: "Close", OnPress: closeList},
	}, len(props.changes) == 0)
	if len(props.changes) == 0 {
		return uicomponents.Panel(uicomponents.PanelProps{
			Title: "Tracked Changes", Notice: "  No tracked file changes.", Bindings: bindings,
		})
	}

	opts := make([]uicomponents.Option, len(props.changes))
	for i, ch := range props.changes {
		added, removed := "+"+format.Tokens(ch.Additions), "-"+format.Tokens(ch.Deletions)
		stats := trail.Span(trail.SpanProps{Style: sheet.stats},
			trail.Span(trail.SpanProps{Style: sheet.added}, trail.Text(added)),
			trail.Span(trail.SpanProps{Style: sheet.separator}, trail.Text("/")),
			trail.Span(trail.SpanProps{Style: sheet.removed}, trail.Text(removed)),
		)
		opts[i] = uicomponents.Option{
			Value: ch.Path, Label: ch.Path,
			Cells:     []string{ch.Path, string(ch.Kind), added + "/" + removed},
			CellNodes: []trail.Node{nil, nil, stats},
		}
	}
	picker := components.NewPicker("Tracked Changes", opts, func(opt uicomponents.Option) {
		nav.Push(changeDiffRoute{Path: opt.Value})
	})
	picker.Columns = []uicomponents.Column{{}, {Width: 16}, {Width: 16, Align: style.TextAlignRight}}
	picker.Filterable = true
	picker.Bindings = []uicomponents.KeyBinding{
		{Key: "↑/↓", Label: "Select"},
		{Key: "enter", Label: "View diff"},
		{Key: "r", Label: "Revert"},
		{Key: "esc", Label: "Close"},
	}
	picker.OnCancel = closeList
	picker.ConfirmActions = map[string]components.ConfirmAction{
		"r": {
			Prompt:    func(opt uicomponents.Option) string { return "Revert changes to " + opt.Label + "?" },
			OnConfirm: func(opt uicomponents.Option) { props.revert(opt.Value) },
		},
	}
	return components.Picker(picker)
})
