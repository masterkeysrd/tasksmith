package main

import (
	"context"
	"fmt"
	"os"

	"github.com/masterkeysrd/tasksmith/internal/app"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	app := app.New()
	defer func() {
		if err := app.Close(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "Error during application shutdown:", err)
		}
	}()

	if err := app.Run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "Error running application:", err)
		os.Exit(1)
	}
}
