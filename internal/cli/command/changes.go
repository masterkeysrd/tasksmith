package command

import (
	"context"
	"fmt"
	"strings"

	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli/components"
	"github.com/masterkeysrd/tasksmith/internal/cli/store"
	uicomponents "github.com/masterkeysrd/tasksmith/internal/ui/components"
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
	if len(entries) == 0 {
		return nil
	}

	type fileGroup struct {
		path    string
		entries []api.FileChange
	}

	var groups []fileGroup
	groupIndices := make(map[string]int)

	for _, ch := range entries {
		idx, ok := groupIndices[ch.Path]
		if !ok {
			groupIndices[ch.Path] = len(groups)
			groups = append(groups, fileGroup{
				path:    ch.Path,
				entries: []api.FileChange{ch},
			})
		} else {
			groups[idx].entries = append(groups[idx].entries, ch)
		}
	}

	var result []api.FileChange
	for _, g := range groups {
		if len(g.entries) == 0 {
			continue
		}
		first := g.entries[0]
		last := g.entries[len(g.entries)-1]

		baselineHash := first.BeforeHash
		currentHash := last.AfterHash
		if last.Kind == "deleted" {
			currentHash = ""
		}

		if baselineHash == currentHash && (first.Kind != "created" || last.Kind == "deleted") {
			continue
		}

		var netKind string
		if (first.Kind == "created" || first.BeforeHash == "") && last.Kind != "deleted" {
			netKind = "created"
		} else if last.Kind == "deleted" {
			netKind = "deleted"
		} else {
			netKind = "modified"
		}

		lastRevertIdx := -1
		for i, e := range g.entries {
			if e.Tool == "revert" {
				lastRevertIdx = i
			}
		}

		totalAdd := 0
		totalDel := 0
		for i := lastRevertIdx + 1; i < len(g.entries); i++ {
			if g.entries[i].Tool != "revert" {
				totalAdd += g.entries[i].Additions
				totalDel += g.entries[i].Deletions
			}
		}

		result = append(result, api.FileChange{
			ID:        last.ID,
			Path:      g.path,
			Kind:      netKind,
			Additions: totalAdd,
			Deletions: totalDel,
			Timestamp: last.Timestamp,
		})
	}

	return result
}

type emptyChangesViewProps struct {
	slotID string
}

var emptyChangesView = trail.FC("EmptyChangesView", func(props emptyChangesViewProps) trail.Node {
	trail.UseInput(func(_ string, k event.KeyEvent) {
		if k.MatchString("esc", "escape") {
			store.RemoveSlot(props.slotID)
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

	return trail.Box(trail.BoxProps{Style: boxStyle},
		trail.Span(trail.SpanProps{Style: titleStyle}, trail.Text("Tracked Changes")),
		trail.Span(trail.SpanProps{Style: emptyStyle}, trail.Text("  No tracked file changes.")),
		components.KeyboardHint(components.KeyboardHintProps{
			Bindings: []components.KeyBinding{
				{Key: "esc", Label: "Close"},
			},
		}),
	)
})

type changeDiffProps struct {
	sessionID string
	change    *api.FileChange
	onClose   func()
}

var changeDiffViewer = trail.FC("ChangeDiffViewer", func(props changeDiffProps) trail.Node {
	c := store.Client()
	diffText, setDiffText := kitex.UseState("")
	isLoading, setIsLoading := kitex.UseState(true)
	errText, setErrText := kitex.UseState("")

	kitex.UseEffectCleanup(func() func() {
		if c == nil {
			setIsLoading(false)
			setErrText("client unavailable")
			return nil
		}
		ctx, cancel := context.WithCancel(context.Background())
		setIsLoading(true)
		go func() {
			diff, err := c.DiffChange(ctx, props.sessionID, props.change.Path, "")
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				setErrText(err.Error())
			} else {
				setDiffText(diff)
				setErrText("")
			}
			setIsLoading(false)
		}()
		return cancel
	}, []any{props.sessionID, props.change.Path})

	trail.UseInput(func(_ string, k event.KeyEvent) {
		if k.MatchString("esc", "escape") {
			if props.onClose != nil {
				props.onClose()
			}
		}
	})

	scheme := theme.UseTheme()
	border := style.SingleBorder().Top(true).Bottom(false).Left(false).Right(false).Color(scheme.Color.Border.Default)
	headerStyle := style.S().Bold(true).Margin(0, 0, 1, 0).Foreground(scheme.Color.Text.Primary)

	var content trail.Node
	if isLoading() {
		content = trail.Box(trail.BoxProps{
			Style: style.S().FlexDirection(style.FlexRow).Margin(0, 0, 1, 0),
		}, trail.Span(trail.SpanProps{
			Style: style.S().Foreground(scheme.Color.Text.Secondary),
		}, trail.Text("Loading diff...")))
	} else if err := errText(); err != "" {
		content = trail.Box(trail.BoxProps{
			Style: style.S().FlexDirection(style.FlexRow).Margin(0, 0, 1, 0),
		}, trail.Span(trail.SpanProps{
			Style: style.S().Foreground(scheme.Color.Text.Error),
		}, trail.Text("Error loading diff: "+err)))
	} else {
		content = components.DiffView(components.DiffProps{
			Text: diffText(),
			Path: props.change.Path,
		})
	}

	return trail.Box(trail.BoxProps{
		Style: style.S().
			Border(border).
			Width(style.Percent(100)).
			FlexDirection(style.FlexColumn),
	},
		trail.Span(trail.SpanProps{
			Style: headerStyle,
		}, trail.Text(fmt.Sprintf("Diff for %s (%s):", props.change.Path, props.change.Kind))),
		content,
		components.KeyboardHint(components.KeyboardHintProps{
			Bindings: []components.KeyBinding{
				{Key: "esc", Label: "Return to changes"},
			},
		}),
	)
})

// ChangesBrowser renders an interactive changes list and inline diff viewer inside <Slot />.
var ChangesBrowser = trail.FC("ChangesBrowser", func(slotID string) trail.Node {
	sess := store.GetSession().Session
	c := store.Client()

	selectedChange, setSelectedChange := kitex.UseState[*api.FileChange](nil)
	changesState, setChangesState := kitex.UseState[[]api.FileChange](nil)
	isLoading, setIsLoading := kitex.UseState(true)
	errorState, setErrorState := kitex.UseState[error](nil)
	fetchCount, setFetchCount := kitex.UseState(0)

	sessID := ""
	if sess != nil {
		sessID = sess.ID
	}

	refetch := func() {
		setFetchCount(fetchCount() + 1)
	}

	kitex.UseEffectCleanup(func() func() {
		if sess == nil || sess.ID == "" || c == nil {
			setIsLoading(false)
			return nil
		}
		ctx, cancel := context.WithCancel(context.Background())
		setIsLoading(true)
		go func() {
			data, err := c.Changes(ctx, sess.ID)
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				setErrorState(err)
			} else {
				setChangesState(data)
				setErrorState(nil)
			}
			setIsLoading(false)
		}()
		return cancel
	}, []any{sessID, fetchCount()})

	if sess == nil || sess.ID == "" {
		store.RemoveSlot(slotID)
		store.AppendCommandOutput("/changes", "No active session.")
		return nil
	}
	if c == nil {
		store.RemoveSlot(slotID)
		store.AppendNotice("Client unavailable.")
		return nil
	}

	if sel := selectedChange(); sel != nil {
		return changeDiffViewer(changeDiffProps{
			sessionID: sess.ID,
			change:    sel,
			onClose: func() {
				setSelectedChange(nil)
			},
		})
	}

	if isLoading() {
		return loadingHint("Loading changes...")
	}
	if err := errorState(); err != nil {
		store.RemoveSlot(slotID)
		store.AppendError("Error", err.Error())
		return nil
	}

	aggregated := aggregateFileChanges(changesState())
	if len(aggregated) == 0 {
		return emptyChangesView(emptyChangesViewProps{slotID: slotID})
	}

	scheme := theme.UseTheme()
	opts := make([]uicomponents.Option, len(aggregated))
	for i, ch := range aggregated {
		stats := fmt.Sprintf("+%d/-%d", ch.Additions, ch.Deletions)
		statsNode := trail.Span(trail.SpanProps{
			Style: style.S().WhiteSpace(style.WhiteSpacePre),
		},
			trail.Span(trail.SpanProps{
				Style: style.S().Foreground(scheme.Color.Diff.AddedText),
			}, trail.Text(fmt.Sprintf("+%d", ch.Additions))),
			trail.Span(trail.SpanProps{
				Style: style.S().Foreground(scheme.Color.Text.Secondary),
			}, trail.Text("/")),
			trail.Span(trail.SpanProps{
				Style: style.S().Foreground(scheme.Color.Diff.RemovedText),
			}, trail.Text(fmt.Sprintf("-%d", ch.Deletions))),
		)
		opts[i] = uicomponents.Option{
			Value:     ch.Path,
			Label:     ch.Path,
			Cells:     []string{ch.Path, ch.Kind, stats},
			CellNodes: []trail.Node{nil, nil, statsNode},
		}
	}

	pickerProps := components.NewPicker("Tracked Changes", opts, func(opt uicomponents.Option) {
		for i := range aggregated {
			if aggregated[i].Path == opt.Value {
				ch := aggregated[i]
				setSelectedChange(&ch)
				break
			}
		}
	})
	pickerProps.Columns = []uicomponents.Column{
		{Width: 0},
		{Width: 16, Align: style.TextAlignLeft},
		{Width: 16, Align: style.TextAlignRight},
	}
	pickerProps.Filterable = true
	pickerProps.Bindings = []components.KeyBinding{
		{Key: "↑/↓", Label: "Select"},
		{Key: "enter", Label: "View diff"},
		{Key: "r", Label: "Revert"},
		{Key: "esc", Label: "Close"},
	}
	pickerProps.OnCancel = func() {
		store.RemoveSlot(slotID)
	}
	pickerProps.ConfirmActions = map[string]components.ConfirmAction{
		"r": {
			Prompt: func(opt uicomponents.Option) string {
				return fmt.Sprintf("Revert changes to %s?", opt.Label)
			},
			OnConfirm: func(opt uicomponents.Option) {
				go func() {
					err := c.Revert(context.Background(), sess.ID, api.RevertRequest{
						Path: opt.Value,
					})
					if err != nil {
						store.AppendError("Revert Error", err.Error())
						return
					}
					refetch()
				}()
			},
		},
	}

	return components.Picker(pickerProps)
})
