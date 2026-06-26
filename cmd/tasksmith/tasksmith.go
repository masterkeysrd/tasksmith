package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime/debug"

	"github.com/masterkeysrd/tasksmith/internal/app"
	"github.com/masterkeysrd/tasksmith/internal/app/flags"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
)

func main() {
	defer func() {
		if r := recover(); r != nil {
			// Restore the terminal state if it was left in raw/altered mode.
			cmd := exec.Command("stty", "sane")
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			_ = cmd.Run()

			// Print a carriage return and newline to start cleanly at the left edge
			fmt.Fprint(os.Stderr, "\r\n")

			stack := debug.Stack()
			log.Error("Application panicked", log.Any("error", r), log.String("stack", string(stack)))
			fmt.Fprintf(os.Stderr, "Tasksmith encountered a fatal error: %v\n\nStack trace:\n%s\n", r, stack)
			os.Exit(1)
		}
	}()

	opts, err := flags.Load()
	if err != nil {
		panic(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	application := app.New(opts, cancel)

	defer func() {
		// Use a background context for closing to ensure cleanup tasks
		// can complete even if the main context was canceled.
		if err := application.Close(context.Background()); err != nil {
			panic(err)
		}
	}()

	if err := application.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		panic(err)
	}
}
