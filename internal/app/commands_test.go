package app

import (
	"context"
	"testing"

	"github.com/masterkeysrd/tasksmith/internal/app/flags"
	"github.com/masterkeysrd/tasksmith/internal/tui/command"
	"github.com/masterkeysrd/tasksmith/internal/tui/mode"
)

func TestCommands(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	opts := &flags.Flags{
		CWD: ".",
	}
	app := New(opts, cancel)
	app.InitializeCommands()

	t.Run("mode commands", func(t *testing.T) {
		command.Execute(ctx, "startinsert")
		if mode.Get() != mode.Insert {
			t.Errorf("expected mode Insert, got %v", mode.Get())
		}

		command.Execute(ctx, "stopinsert")
		if mode.Get() != mode.Normal {
			t.Errorf("expected mode Normal, got %v", mode.Get())
		}
	})

	t.Run("quit command", func(t *testing.T) {
		command.Execute(ctx, "quit")
		select {
		case <-ctx.Done():
			// OK
		default:
			t.Error("expected context to be cancelled after quit command")
		}
	})
}
