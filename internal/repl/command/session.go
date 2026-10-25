package command

import (
	"context"
	"fmt"
	"strings"

	"github.com/dustin/go-humanize"
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/kite/promise"
	"github.com/masterkeysrd/kite/style"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/repl/components"
	"github.com/masterkeysrd/tasksmith/internal/repl/store"
	uicomponents "github.com/masterkeysrd/tasksmith/internal/ui/components"
)

func (r *Registry) registerSessionCommands() {
	r.Register(Command{
		Name:        "new",
		Usage:       "/new [model]",
		Description: "Create and switch to a new session",
		Handler: func(args []string) error {
			store.NewDraftSession(strings.TrimSpace(strings.Join(args, " ")))
			return nil
		},
	})
	r.Register(Command{
		Name:        "sessions",
		Aliases:     []string{"session", "ls"},
		Usage:       "/sessions [session_id]",
		Description: "Browse, switch, or manage workspace sessions",
		Handler:     handleSessions,
	})
}

func handleSessions(args []string) error {
	if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
		store.SwitchSession(strings.TrimSpace(args[0]))
		return nil
	}

	store.EnqueueSlot(store.SlotItem{
		Kind: store.SlotKindPicker,
		Data: SessionBrowser,
	})
	return nil
}

// SessionBrowser renders an interactive session browser with rename (f2) and delete (f4).
var SessionBrowser = trail.FC("SessionBrowser", func(slotID string) trail.Node {
	c := store.Client()

	result := wind.Use("sessions", promise.WrapWithProps(func(ctx context.Context, _ string) ([]api.Session, error) {
		if c == nil {
			return nil, nil
		}
		return c.ListSessions(ctx)
	}), wind.Options{Enabled: c != nil})

	type renamePayload struct {
		id    string
		title string
	}
	renameMutation := wind.UseMutation(func(ctx context.Context, p renamePayload) *promise.Promise[*api.Session] {
		return promise.Wrap(func(ctx context.Context) (*api.Session, error) {
			if c == nil {
				return nil, fmt.Errorf("client unavailable")
			}
			return c.UpdateSession(ctx, p.id, api.UpdateSessionRequest{Title: p.title})
		})(ctx)
	}, wind.MutationOptions[renamePayload, *api.Session]{
		OnSuccess: func(s *api.Session, v renamePayload, mc wind.MutationContext) {
			result.Refetch()
			if store.GetSession().Session != nil && store.GetSession().Session.ID == v.id {
				store.SetSession(s)
			}
		},
		OnError: func(err error, _ renamePayload, _ wind.MutationContext) {
			store.AppendError("Error renaming session", err.Error())
		},
	})

	deleteMutation := wind.UseMutation(func(ctx context.Context, id string) *promise.Promise[any] {
		return promise.Wrap(func(ctx context.Context) (any, error) {
			if c == nil {
				return nil, fmt.Errorf("client unavailable")
			}
			err := c.DeleteSession(ctx, id)
			return nil, err
		})(ctx)
	}, wind.MutationOptions[string, any]{
		OnSuccess: func(_ any, id string, mc wind.MutationContext) {
			result.Refetch()
			if store.GetSession().Session != nil && store.GetSession().Session.ID == id {
				store.NewDraftSession("")
			}
		},
		OnError: func(err error, _ string, _ wind.MutationContext) {
			store.AppendError("Error deleting session", err.Error())
		},
	})

	kitex.UseEffect(func() {
		if c == nil {
			store.RemoveSlot(slotID)
			store.AppendNotice("Client unavailable.")
			return
		}
		if result.IsLoading {
			return
		}
		if result.IsError {
			store.RemoveSlot(slotID)
			store.AppendError("Error", result.Error.Error())
		} else if len(result.Data) == 0 {
			store.RemoveSlot(slotID)
			store.AppendCommandOutput("/sessions", "No sessions found.")
		}
	}, []any{c == nil, result.IsLoading, result.IsError, result.Error, len(result.Data)})

	if c == nil {
		return nil
	}
	if result.IsLoading {
		return loadingHint("Loading sessions...")
	}
	if result.IsError || len(result.Data) == 0 {
		return nil
	}

	activeID := ""
	if store.GetSession().Session != nil {
		activeID = store.GetSession().Session.ID
	}

	opts := make([]uicomponents.Option, len(result.Data))
	for i, s := range result.Data {
		title := strings.TrimSpace(s.Title)
		if title == "" {
			title = "Untitled"
		}
		model := strings.TrimSpace(s.Config.Model)
		if model == "" {
			model = "-"
		}
		relTime := humanize.Time(s.UpdatedAt)
		opts[i] = uicomponents.Option{
			Value:   s.ID,
			Label:   title,
			Cells:   []string{title, model, relTime},
			Current: activeID != "" && s.ID == activeID,
		}
	}

	pickerProps := components.NewPicker("Sessions", opts, func(opt uicomponents.Option) {
		store.RemoveSlot(slotID)
		store.SwitchSession(opt.Value)
	})
	pickerProps.Columns = []uicomponents.Column{
		{Width: 0},
		{Width: 24, Align: style.TextAlignLeft},
		{Width: 26, Align: style.TextAlignRight},
	}
	pickerProps.Filterable = true
	pickerProps.Bindings = []components.KeyBinding{
		{Key: "↑/↓", Label: "Select"},
		{Key: "enter", Label: "Switch"},
		{Key: "f2", Label: "Rename"},
		{Key: "f4", Label: "Delete"},
		{Key: "esc", Label: "Cancel"},
	}
	pickerProps.OnCancel = func() {
		store.RemoveSlot(slotID)
		store.AppendCommandOutput("/sessions", "Exited /sessions command")
	}
	pickerProps.InputActions = map[string]components.InputAction{
		"f2": {
			Prompt: "Rename session:",
			InitialValue: func(opt uicomponents.Option) string {
				return opt.Label
			},
			OnSubmit: func(opt uicomponents.Option, val string) {
				renameMutation.Mutate(renamePayload{id: opt.Value, title: val})
			},
		},
	}
	pickerProps.ConfirmActions = map[string]components.ConfirmAction{
		"f4": {
			Prompt: func(opt uicomponents.Option) string {
				return fmt.Sprintf("Delete session %q?", opt.Label)
			},
			OnConfirm: func(opt uicomponents.Option) {
				deleteMutation.Mutate(opt.Value)
			},
		},
	}

	return components.Picker(pickerProps)
})
