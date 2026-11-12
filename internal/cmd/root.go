package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/masterkeysrd/tasksmith/client"
	"github.com/masterkeysrd/tasksmith/internal/daemon"
	"github.com/masterkeysrd/tasksmith/internal/version"
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
		Version:       version.Get(),
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
		newVersionCmd(rootOpts),
	)

	return cmd
}

// newVersionCmd prints the client binary version and, when reachable, the
// running daemon's version without spawning one.
func newVersionCmd(rootOpts *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the client version and the running daemon's version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "TaskSmith %s\n", version.Get())

			sockPath, err := rootOpts.resolveSocket()
			if err != nil {
				return err
			}
			c, err := client.New(sockPath)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 500*time.Millisecond)
			defer cancel()
			h, err := c.Health(ctx)
			if err != nil {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Daemon not running\n")
				return nil
			}
			daemonVersion := h.Version
			if daemonVersion == "" {
				daemonVersion = "unknown (predates version reporting)"
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Daemon  %s (pid=%d)\n", daemonVersion, h.PID)
			return nil
		},
	}
}

// Execute builds and executes the root TaskSmith CLI command with ctx.
func Execute(ctx context.Context) error {
	return NewRootCmd().ExecuteContext(ctx)
}
