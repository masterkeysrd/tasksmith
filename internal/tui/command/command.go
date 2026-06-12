package command

import (
	"context"

	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/kite/promise"
)

// CommandState holds the reactive state of a command execution.
type CommandState struct {
	// IsPending is true while the command is in-flight.
	IsPending bool
	// Error holds any error returned by the command, if applicable.
	Error error
}

// UseCommand returns an execution function and the reactive state of the command.
// Call it inside a Kitex component to trigger commands and react to loading/error states.
//
// The returned execute function runs the underlying Execute in a background worker pool
// via promise.New. State updates from promise .Then() callbacks run on the main UI thread
// automatically — no manual goroutine or QueueMacrotask management is needed.

type UseCommandOpts struct {
	OnComplete func()
}

// UseCommand returns an execution function and the reactive state of the command.
// Call it inside a Kitex component to trigger commands and react to loading/error states.
func UseCommand(cmd string) (CommandState, func(args ...string)) {
	state, executeWithPayload := UseCommandWithPayload(cmd)
	execute := func(args ...string) {
		executeWithPayload(nil, args...)
	}
	return state, execute
}

// UseCommandWithPayload returns an execution function that accepts an optional
// payload and the reactive state of the command.
func UseCommandWithPayload(cmd string) (CommandState, func(payload any, args ...string)) {
	state, setState := kitex.UseState(CommandState{})

	execute := func(payload any, args ...string) {
		setState(CommandState{IsPending: true})

		promise.New(func(ctx context.Context) (any, error) {
			return nil, ExecuteWithPayload(ctx, cmd, payload, args...)
		}).Then(
			func(any) {
				setState(CommandState{IsPending: false, Error: nil})
			},
			func(err error) {
				setState(CommandState{IsPending: false, Error: err})
			},
		)
	}

	return state(), execute
}
