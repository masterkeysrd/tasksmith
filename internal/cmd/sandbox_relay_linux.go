//go:build linux

package cmd

import (
	"github.com/masterkeysrd/tasksmith/internal/daemon/sandbox"
	"github.com/spf13/cobra"
)

// newSandboxRelayCmd wires the hidden sandbox-relay subcommand used by the
// Linux network-namespace relay. See sandbox.RunSandboxRelay and
// sandbox.RelayServe for the two modes.
func newSandboxRelayCmd() *cobra.Command {
	var (
		serve  bool
		socket string
		port   int
	)
	cmd := &cobra.Command{
		Use:    sandbox.SandboxRelayArg + " [--socket PATH --port N] [--] [COMMAND ...]",
		Short:  "Internal: bridge sandbox loopback traffic to the daemon network proxy",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if serve {
				return sandbox.RelayServe(cmd.Context(), socket, port)
			}
			if len(args) == 0 {
				return cmd.Usage()
			}
			return sandbox.RunSandboxRelay(socket, port, args[0], args[1:])
		},
	}
	cmd.Flags().BoolVar(&serve, "serve", false, "Run the relay server itself instead of supervising a command")
	cmd.Flags().StringVar(&socket, "socket", "", "Proxy Unix socket path")
	cmd.Flags().IntVar(&port, "port", 0, "Loopback port to bridge")
	return cmd
}
