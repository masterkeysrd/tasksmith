package repl

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/masterkeysrd/kite/backend"
	"github.com/masterkeysrd/kite/extras/trail"
	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/client"
	"github.com/masterkeysrd/tasksmith/internal/repl/command"
	"github.com/masterkeysrd/tasksmith/internal/repl/components"
	uihooks "github.com/masterkeysrd/tasksmith/internal/ui/hooks"
)

// ErrInvalidConfig is returned when required [Config] fields are missing.
var ErrInvalidConfig = errors.New("repl: invalid configuration")

// Config holds the parameters for starting a REPL session.
type Config struct {
	Client    *client.Client
	Workspace string
	Version   string
	SessionID string
	Agent     string
	Model     string
	Title     string
	Backend   backend.Backend
}

// Validate checks that all required fields in [Config] are provided.
func (c Config) Validate() error {
	if c.Client == nil {
		return fmt.Errorf("%w: client is required", ErrInvalidConfig)
	}
	if strings.TrimSpace(c.Workspace) == "" {
		return fmt.Errorf("%w: workspace is required", ErrInvalidConfig)
	}
	return nil
}

// Run starts the REPL using the Kite trail inline application engine.
func Run(ctx context.Context, cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}

	cfg.Version = components.NormalizeVersion(cfg.Version)

	if cfg.Client.CWD() == "" && cfg.Workspace != "" {
		cfg.Client = cfg.Client.WithCWD(cfg.Workspace)
	}

	var sess *api.Session
	mode := api.ModeDefault

	if sid := strings.TrimSpace(cfg.SessionID); sid != "" {
		loaded, err := cfg.Client.GetSession(ctx, sid)
		if err != nil {
			return fmt.Errorf("get session %q: %w", sid, err)
		}
		sess = loaded

		title := strings.TrimSpace(cfg.Title)
		agent := strings.TrimSpace(cfg.Agent)
		model := strings.TrimSpace(cfg.Model)
		if title != "" || agent != "" || model != "" {
			updated, err := cfg.Client.UpdateSession(ctx, sid, api.UpdateSessionRequest{
				Title: title,
				Agent: agent,
				Model: model,
			})
			if err != nil {
				return fmt.Errorf("update session %q: %w", sid, err)
			}
			sess = updated
		}

		if m, mErr := cfg.Client.PermissionMode(ctx, sess.ID); mErr == nil && m != "" {
			mode = m
		}
	}

	return runTUI(ctx, cfg, sess, mode)
}

func runTUI(ctx context.Context, cfg Config, sess *api.Session, mode api.PermissionMode) error {
	modelLabel, thinking := uihooks.ResolveModelInfo(ctx, cfg.Client, cfg.Agent, cfg.Model, sess)

	props := AppProps{
		Client:            cfg.Client,
		Workspace:         cfg.Workspace,
		Version:           cfg.Version,
		SessionID:         cfg.SessionID,
		Agent:             cfg.Agent,
		Model:             cfg.Model,
		Title:             cfg.Title,
		InitialSess:       sess,
		InitialMode:       mode,
		InitialModelLabel: modelLabel,
		InitialThinking:   thinking,
		Registry:          command.NewRegistry(),
	}

	opts := []trail.Option{trail.WithCtrlCExit(true)}
	if cfg.Backend != nil {
		opts = append(opts, trail.WithBackend(cfg.Backend))
	}

	runErr := trail.Run(ctx, App(props), opts...)
	if errors.Is(runErr, context.Canceled) {
		return nil
	}
	return runErr
}
