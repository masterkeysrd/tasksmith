package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/masterkeysrd/tasksmith/internal/daemon"
	"github.com/spf13/cobra"
)

type rootOptions struct {
	workspace string
	socket    string
}

func (o *rootOptions) resolveWorkspace() (string, error) {
	if ws := strings.TrimSpace(o.workspace); ws != "" {
		return ws, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve working directory: %w", err)
	}
	return cwd, nil
}

func (o *rootOptions) resolveSocket() (string, error) {
	if sock := strings.TrimSpace(o.socket); sock != "" {
		return sock, nil
	}
	sock, err := daemon.DefaultSocketPath()
	if err != nil {
		return "", fmt.Errorf("resolve socket path: %w", err)
	}
	return sock, nil
}

// NewRootCmd constructs the root `tasksmith` Cobra command and attaches all
// subcommands (`repl`, `daemon`).
func NewRootCmd() *cobra.Command {
	rootOpts := &rootOptions{}
	replOpts := &replOptions{}

	cmd := &cobra.Command{
		Use:           "tasksmith",
		Short:         "TaskSmith AI coding assistant and background Brain Daemon",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runREPL(cmd, rootOpts, replOpts)
		},
	}

	cmd.PersistentFlags().StringVarP(&rootOpts.workspace, "workspace", "w", "", "Target workspace directory (defaults to current working directory)")
	cmd.PersistentFlags().StringVar(&rootOpts.socket, "socket", "", "Daemon Unix domain socket path")

	bindREPLFlags(cmd, replOpts)

	cmd.AddCommand(
		newREPLCmd(rootOpts),
		newDaemonCmd(rootOpts),
	)

	return cmd
}

// Execute builds and executes the root TaskSmith CLI command with ctx.
func Execute(ctx context.Context) error {
	return NewRootCmd().ExecuteContext(ctx)
}
