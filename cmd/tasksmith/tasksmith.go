package main

import (
	"context"
	"errors"

	"github.com/masterkeysrd/tasksmith/internal/app"
	"github.com/masterkeysrd/tasksmith/internal/app/flags"
)

func main() {
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
