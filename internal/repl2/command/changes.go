package command

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/masterkeysrd/kite/event"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/kite/promise"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/repl2/components"
	"github.com/masterkeysrd/tasksmith/internal/repl2/store"
	uicomponents "github.com/masterkeysrd/tasksmith/internal/ui/components"
)

func (r *Registry) registerChangesCommands() {
	r.Register(Command{
		Name:        "changes",
		Usage:       "/changes",
		Description: "List and inspect tracked file changes",
		Handler:     handleChanges,
	})
	r.Register(Command{
		Name:        "diff",
		Usage:       "/diff <path> <entry_id>",
		Description: "Show unified diff for a tracked file change",
		Handler:     handleDiff,
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

func handleDiff(args []string) error {
	sess := store.GetSession().Session
	if sess == nil || sess.ID == "" {
		store.AppendNotice("No active session.")
		return nil
	}
	if len(args) < 2 || strings.TrimSpace(args[0]) == "" || strings.TrimSpace(args[1]) == "" {
		return errors.New("usage: /diff <path> <entry_id>")
	}

	c := store.Client()
	if c == nil {
		store.AppendNotice("Client unavailable.")
		return nil
	}

	path := strings.TrimSpace(args[0])
	entryID := strings.TrimSpace(args[1])
	go func() {
		diff, err := c.DiffChange(context.Background(), sess.ID, path, entryID)
		if err != nil {
			store.AppendError("Diff Error", err.Error())
			return
		}
		store.AppendCommandOutput(fmt.Sprintf("/diff %s %s", path, entryID), diff)
	}()
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

type changeDiffProps struct {
	sessionID string
	change    *api.FileChange
	onClose   func()
}

var changeDiffViewer = trail.FC("ChangeDiffViewer", func(props changeDiffProps) trail.Node {
	c := store.Client()

	diffQuery := wind.Use("diff:"+props.sessionID+":"+props.change.ID, promise.WrapWithProps(func(ctx context.Context, _ string) (string, error) {
		if c == nil {
			return "", errors.New("client unavailable")
		}
		return c.DiffChange(ctx, props.sessionID, props.change.Path, props.change.ID)
	}), wind.Options{Enabled: c != nil})

	trail.UseInput(func(_ string, k event.KeyEvent) {
		if k.MatchString("esc", "escape") {
			if props.onClose != nil {
				props.onClose()
			}
		}
	})

	var content trail.Node
	if diffQuery.IsLoading {
		content = trail.Text("Loading diff...")
	} else if diffQuery.IsError {
		content = trail.Text("Error loading diff: " + diffQuery.Error.Error())
	} else {
		content = components.DiffView(components.DiffProps{Text: diffQuery.Data})
	}

	border := style.SingleBorder().Top(true).Bottom(false).Left(false).Right(false)
	return trail.Box(trail.BoxProps{
		Style: style.S().
			Border(border).
			Width(style.Percent(100)).
			FlexDirection(style.FlexColumn),
	},
		trail.Span(trail.SpanProps{
			Style: style.S().Bold(true).Margin(0, 0, 1, 0),
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

	sessID := ""
	if sess != nil {
		sessID = sess.ID
	}

	changesQuery := wind.Use("changes:"+sessID, promise.WrapWithProps(func(ctx context.Context, _ string) ([]api.FileChange, error) {
		if c == nil || sessID == "" {
			return nil, nil
		}
		return c.Changes(ctx, sessID)
	}), wind.Options{Enabled: c != nil && sessID != ""})

	revertMutation := wind.UseMutation(func(ctx context.Context, req api.RevertRequest) *promise.Promise[any] {
		return promise.Wrap(func(ctx context.Context) (any, error) {
			if c == nil || sessID == "" {
				return nil, errors.New("session or client unavailable")
			}
			err := c.Revert(ctx, sessID, req)
			return nil, err
		})(ctx)
	}, wind.MutationOptions[api.RevertRequest, any]{
		OnSuccess: func(_ any, _ api.RevertRequest, _ wind.MutationContext) {
			changesQuery.Refetch()
			setSelectedChange(nil)
		},
		OnError: func(err error, _ api.RevertRequest, _ wind.MutationContext) {
			store.AppendError("Revert Error", err.Error())
		},
	})

	kitex.UseEffect(func() {
		if sess == nil || sess.ID == "" {
			store.RemoveSlot(slotID)
			store.AppendCommandOutput("/changes", "No active session.")
			return
		}
		if c == nil {
			store.RemoveSlot(slotID)
			store.AppendNotice("Client unavailable.")
			return
		}
		if changesQuery.IsLoading {
			return
		}
		if changesQuery.IsError {
			store.RemoveSlot(slotID)
			store.AppendError("Error", changesQuery.Error.Error())
		} else if len(changesQuery.Data) == 0 {
			store.RemoveSlot(slotID)
			store.AppendCommandOutput("/changes", "No tracked file changes.")
		}
	}, []any{sess == nil, c == nil, changesQuery.IsLoading, changesQuery.IsError, changesQuery.Error, len(changesQuery.Data)})

	if sess == nil || sess.ID == "" || c == nil {
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

	if changesQuery.IsLoading {
		return loadingHint("Loading changes...")
	}
	if changesQuery.IsError || len(changesQuery.Data) == 0 {
		return nil
	}

	changes := changesQuery.Data
	opts := make([]uicomponents.Option, len(changes))
	for i, ch := range changes {
		stats := fmt.Sprintf("+%d/-%d", ch.Additions, ch.Deletions)
		opts[i] = uicomponents.Option{
			Value: ch.ID,
			Label: ch.Path,
			Cells: []string{ch.Path, ch.Kind, stats},
		}
	}

	pickerProps := components.NewPicker("Tracked Changes", opts, func(opt uicomponents.Option) {
		for i := range changes {
			if changes[i].ID == opt.Value {
				ch := changes[i]
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
		{Key: "f4/r", Label: "Revert"},
		{Key: "esc", Label: "Cancel"},
	}
	pickerProps.OnCancel = func() {
		store.RemoveSlot(slotID)
		store.AppendCommandOutput("/changes", "Exited /changes command")
	}
	pickerProps.ConfirmActions = map[string]components.ConfirmAction{
		"f4": {
			Prompt: func(opt uicomponents.Option) string {
				return fmt.Sprintf("Revert changes to %s?", opt.Label)
			},
			OnConfirm: func(opt uicomponents.Option) {
				for i := range changes {
					if changes[i].ID == opt.Value {
						revertMutation.Mutate(api.RevertRequest{
							Path:    changes[i].Path,
							EntryID: changes[i].ID,
						})
						break
					}
				}
			},
		},
		"r": {
			Prompt: func(opt uicomponents.Option) string {
				return fmt.Sprintf("Revert changes to %s?", opt.Label)
			},
			OnConfirm: func(opt uicomponents.Option) {
				for i := range changes {
					if changes[i].ID == opt.Value {
						revertMutation.Mutate(api.RevertRequest{
							Path:    changes[i].Path,
							EntryID: changes[i].ID,
						})
						break
					}
				}
			},
		},
	}

	return components.Picker(pickerProps)
})
