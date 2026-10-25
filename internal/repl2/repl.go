package repl2

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/masterkeysrd/kite/backend"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/tasksmith/client"
	"github.com/masterkeysrd/tasksmith/internal/repl2/store"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

// ErrInvalidOptions is returned when required [Options] fields are missing.
var ErrInvalidOptions = errors.New("repl2: invalid options")

// Options holds configuration for starting the REPL.
type Options struct {
	Client    *client.Client
	Workspace string
	Version   string
	SessionID string
	Agent     string
	Model     string
	Title     string
	Backend   backend.Backend
}

// Validate checks that all required fields in [Options] are provided.
func (o Options) Validate() error {
	if o.Client == nil {
		return fmt.Errorf("%w: client is required", ErrInvalidOptions)
	}
	if strings.TrimSpace(o.Workspace) == "" {
		return fmt.Errorf("%w: workspace is required", ErrInvalidOptions)
	}
	return nil
}

// Run starts the REPL application using the Kite trail inline engine.
func Run(ctx context.Context, opts Options) error {
	if err := opts.Validate(); err != nil {
		return err
	}

	theme.SetTransparent(true)

	if opts.Client.CWD() == "" && opts.Workspace != "" {
		opts.Client = opts.Client.WithCWD(opts.Workspace)
	}

	store.ResetApp()
	store.ClearSlot()
	store.ResetSession()
	store.ClearTranscript()
	store.ResetStream()

	store.SetHeaderMeta(opts.Version, opts.Workspace)

	store.InitSession(ctx, opts.Client, store.InitSessionOptions{
		SessionID: opts.SessionID,
		Agent:     opts.Agent,
		Model:     opts.Model,
		Title:     opts.Title,
	})

	trailOpts := []trail.Option{trail.WithCtrlCExit(false)}
	if opts.Backend != nil {
		trailOpts = append(trailOpts, trail.WithBackend(opts.Backend))
	}

	if err := trail.Run(ctx, App(), trailOpts...); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}
