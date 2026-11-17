package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/masterkeysrd/tasksmith/internal/cmd"
	"github.com/masterkeysrd/tasksmith/internal/exec"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := cmd.Execute(ctx); err != nil {
		// A headless run reports its own diagnostics and exit code; anything
		// else falls back to the generic failure contract.
		if exitErr, ok := errors.AsType[*exec.ExitCodeError](err); ok {
			os.Exit(exitErr.Code)
		}
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
