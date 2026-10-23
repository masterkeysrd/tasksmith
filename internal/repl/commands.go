package repl

import (
	"context"
	"strings"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/kite/extras/wind"
	"github.com/masterkeysrd/kite/promise"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/repl/command"
	"github.com/masterkeysrd/tasksmith/internal/repl/components"
	"github.com/masterkeysrd/tasksmith/internal/repl/ui"
	"github.com/masterkeysrd/tasksmith/internal/ui/autocomplete"
	uiclient "github.com/masterkeysrd/tasksmith/internal/ui/client"
	uihooks "github.com/masterkeysrd/tasksmith/internal/ui/hooks"
)

// CommandsControl holds the slash commands and their autocomplete engine.
type CommandsControl struct {
	Registry *command.Registry
	// Engine completes slash commands and their arguments. It is nil when a
	// source failed to register; Err explains why.
	Engine *autocomplete.Engine
	Err    error
	// Agent and Model are the defaults for new sessions.
	Agent string
	Model string
}

var commandsContext = kitex.CreateContext[*CommandsControl](nil)

// UseCommands returns the slash commands.
func UseCommands() CommandsControl {
	control := kitex.UseContext(commandsContext)
	if control == nil {
		panic("repl: UseCommands must be used within a CommandsProvider")
	}
	return *control
}

// CommandsProviderProps configures [CommandsProvider].
type CommandsProviderProps struct {
	// Registry defaults to the built-in commands.
	Registry *command.Registry
	Agent    string
	Model    string

	Children []trail.Node // injected by trail.FCC
}

// CommandsProvider builds the command registry and its autocomplete engine.
var CommandsProvider = trail.FCC("CommandsProvider", func(props CommandsProviderProps) trail.Node {
	registry := kitex.UseMemo(func() *command.Registry {
		if props.Registry != nil {
			return props.Registry
		}
		return command.NewRegistry()
	}, []any{props.Registry})
	completions := kitex.UseMemo(func() *CommandsControl {
		control := &CommandsControl{Registry: registry, Agent: props.Agent, Model: props.Model}
		engine := autocomplete.New()
		for _, source := range registry.Sources() {
			if err := engine.Register(source); err != nil {
				control.Err = err
				return control
			}
		}
		control.Engine = engine
		return control
	}, []any{registry, props.Agent, props.Model})
	return commandsContext.Provider(completions, props.Children...)
})

type changeInput struct {
	SessionID string
	Path      string
	EntryID   string
}

// changeCommands runs the commands that inspect or revert the session's file
// changes. They need a persisted session; a draft has no changes.
type changeCommands struct {
	List   func()
	Diff   func(path, entryID string)
	Revert func(path, entryID string)
}

func useChangeCommands() changeCommands {
	c := uiclient.UseClient()
	session := uihooks.UseSession()
	transcript := UseTranscript()

	onError := func(err error, _ changeInput, _ wind.MutationContext) { appendError(transcript, err) }

	list := wind.UseMutation(promise.WrapWithProps(func(ctx context.Context, in changeInput) ([]api.FileChange, error) {
		return c.Changes(ctx, in.SessionID)
	}), wind.MutationOptions[changeInput, []api.FileChange]{
		OnSuccess: func(changes []api.FileChange, _ changeInput, _ wind.MutationContext) {
			transcript.Append(components.ChangesEntry(ui.ChangesListProps{Changes: changes}))
		},
		OnError: onError,
	})
	diff := wind.UseMutation(promise.WrapWithProps(func(ctx context.Context, in changeInput) (string, error) {
		return c.DiffChange(ctx, in.SessionID, in.Path, in.EntryID)
	}), wind.MutationOptions[changeInput, string]{
		OnSuccess: func(text string, _ changeInput, _ wind.MutationContext) {
			transcript.Append(components.DiffEntry(ui.DiffProps{Text: text}))
		},
		OnError: onError,
	})
	revert := wind.UseMutation(promise.WrapWithProps(func(ctx context.Context, in changeInput) (struct{}, error) {
		return struct{}{}, c.Revert(ctx, in.SessionID, api.RevertRequest{Path: in.Path, EntryID: in.EntryID})
	}), wind.MutationOptions[changeInput, struct{}]{
		OnSuccess: func(_ struct{}, in changeInput, _ wind.MutationContext) {
			text := "/revert"
			if in.Path != "" {
				text += " " + in.Path
				if in.EntryID != "" {
					text += " " + in.EntryID
				}
			}
			transcript.Append(components.CommandEntry(ui.CommandOutputProps{Command: text, Output: "Reverted changes."}))
		},
		OnError: onError,
	})

	noSession := func() {
		transcript.Append(components.NoticeEntry(ui.NoticeProps{Text: "No active session."}))
	}
	return changeCommands{
		List: func() {
			if session.Draft {
				noSession()
				return
			}
			list.Mutate(changeInput{SessionID: session.ID})
		},
		Diff: func(path, entryID string) {
			if session.Draft {
				noSession()
				return
			}
			diff.Mutate(changeInput{SessionID: session.ID, Path: path, EntryID: entryID})
		},
		Revert: func(path, entryID string) {
			if session.Draft {
				transcript.Append(components.CommandEntry(ui.CommandOutputProps{
					Command: "/revert",
					Output:  "No active session.",
				}))
				return
			}
			revert.Mutate(changeInput{SessionID: session.ID, Path: path, EntryID: entryID})
		},
	}
}

// useCommandEnv adapts the REPL's hooks to the environment slash commands run
// against. Commands are plain functions, so they cannot call hooks themselves.
func useCommandEnv() command.Env {
	commands := UseCommands()
	session := uihooks.UseSession()
	mode := uihooks.UsePermissionMode()
	selection := uihooks.UseSessionSelection()
	transcript := UseTranscript()
	overlay := UseOverlay()
	actions := UseSessionActions()
	changes := useChangeCommands()
	app := trail.UseApp()

	var persisted *api.Session
	if !session.Draft {
		persisted = &session.Session
	}
	return command.Env{
		Mode:         mode.Mode,
		Session:      persisted,
		DefaultAgent: commands.Agent,
		DefaultModel: commands.Model,

		AppendOutput: func(output ui.CommandOutputProps) {
			transcript.Append(components.CommandEntry(output))
		},
		OpenPicker:     overlay.OpenPicker,
		SetMode:        mode.Set,
		NewSession:     selection.StartDraft,
		SwitchSession:  actions.Switch,
		RenameSession:  actions.Rename,
		DeleteSession:  actions.Delete,
		BrowseSessions: overlay.OpenSessions,
		ListChanges:    changes.List,
		DiffChange:     changes.Diff,
		RevertChanges:  changes.Revert,
		ClearScreen:    transcript.Clear,
		Exit: func() {
			if app != nil {
				app.Exit(nil)
			}
		},
	}
}

// UseSubmit returns the handler for submitted prompt text: slash commands run
// against the REPL, anything else is sent to the active session.
func UseSubmit() func(text string) {
	commands := UseCommands()
	env := useCommandEnv()
	send := UseSend()
	transcript := UseTranscript()
	conversation := uihooks.UseCurrentConversation()
	streaming := uihooks.UseIsStreaming()

	return func(text string) {
		if strings.HasPrefix(text, "/") {
			if err := commands.Registry.Execute(env, text); err != nil {
				appendError(transcript, err)
			}
			return
		}
		if streaming {
			transcript.Append(components.NoticeEntry(ui.NoticeProps{Text: "A response is already running."}))
			return
		}
		if conversation.Error != nil {
			conversation.Refetch()
		}
		send.Send(text)
	}
}
