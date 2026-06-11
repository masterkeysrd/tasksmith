package main

import (
	"context"

	"github.com/masterkeysrd/tasksmith/internal/app"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := app.Run(ctx); err != nil {
		panic(err)
	}
}
