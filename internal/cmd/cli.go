package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/masterkeysrd/kite/backend"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/cli"
	"github.com/masterkeysrd/tasksmith/internal/version"
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

	c, notices, err := connectOrStart(ctx, sockPath)
	if err != nil {
		return fmt.Errorf("connect to daemon: %w", err)
	}

	if err := cli.Run(ctx, cli.Options{
		Client:         c,
		Workspace:      wsDir,
		Version:        version.Get(),
		SessionID:      strings.TrimSpace(opts.sessionID),
		Agent:          strings.TrimSpace(opts.agent),
		Model:          strings.TrimSpace(opts.model),
		Title:          strings.TrimSpace(opts.title),
		Backend:        opts.backend,
		StartupNotices: notices,
	}); err != nil {
		return err
	}

	if sess := cli.ActiveSession(); sess != nil && sess.ID != "" {
		usage := cli.ActiveUsage()
		var quota *api.ModelQuota
		if sess.Model != nil {
			qctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
			quota, _ = c.GetModelQuota(qctx, sess.Model.ID)
			cancel()
		}
		summary := cli.FormatExitSummary(cli.ExitSummaryInput{
			SessionID: sess.ID,
			Usage:     usage,
			Quota:     quota,
			Color:     exitSummaryColors(cmd),
		})
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "\n\n\n%s\n\n", summary)
	}

	return nil
}

// exitSummaryColors enables ANSI colors only for an interactive color-capable
// TTY, honoring NO_COLOR.
func exitSummaryColors(cmd *cobra.Command) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	f, ok := cmd.OutOrStdout().(*os.File)
	return ok && isatty.IsTerminal(f.Fd())
}
