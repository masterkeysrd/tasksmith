package cli

import (
	"fmt"
	"strings"

	"github.com/masterkeysrd/kite/backend"
	"github.com/masterkeysrd/tasksmith/internal/repl"
	"github.com/spf13/cobra"
)

type replOptions struct {
	sessionID string
	agent     string
	model     string
	title     string
	backend   backend.Backend
}

func bindREPLFlags(cmd *cobra.Command, opts *replOptions) {
	cmd.Flags().StringVar(&opts.sessionID, "session", "", "Existing session ID to resume")
	cmd.Flags().StringVar(&opts.agent, "agent", "", "Agent name when creating a new session")
	cmd.Flags().StringVar(&opts.model, "model", "", "Model identifier when creating a new session")
	cmd.Flags().StringVar(&opts.title, "title", "", "Session title when creating a new session")
}

func newREPLCmd(rootOpts *rootOptions) *cobra.Command {
	opts := &replOptions{}
	cmd := &cobra.Command{
		Use:   "repl",
		Short: "Start an interactive REPL session attached to the Brain Daemon",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runREPL(cmd, rootOpts, opts)
		},
	}
	bindREPLFlags(cmd, opts)
	return cmd
}

func runREPL(cmd *cobra.Command, rootOpts *rootOptions, opts *replOptions) error {
	ctx := cmd.Context()

	wsDir, err := rootOpts.resolveWorkspace()
	if err != nil {
		return err
	}

	sockPath, err := rootOpts.resolveSocket()
	if err != nil {
		return err
	}

	c, err := connectOrStart(ctx, sockPath)
	if err != nil {
		return fmt.Errorf("connect to daemon: %w", err)
	}

	if err := repl.Run(ctx, repl.Options{
		Client:    c,
		Workspace: wsDir,
		SessionID: strings.TrimSpace(opts.sessionID),
		Agent:     strings.TrimSpace(opts.agent),
		Model:     strings.TrimSpace(opts.model),
		Title:     strings.TrimSpace(opts.title),
		Backend:   opts.backend,
	}); err != nil {
		return err
	}

	if sess := repl.ActiveSession(); sess != nil && sess.ID != "" {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "\n\n\nResume this session with:\n    tasksmith --session %s\n", sess.ID)
	}

	return nil
}
