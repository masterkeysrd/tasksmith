package main

import (
	"context"

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
		if err := application.Close(ctx); err != nil {
			panic(err)
		}
	}()

	if err := application.Run(ctx); err != nil {
		panic(err)
	}
}
