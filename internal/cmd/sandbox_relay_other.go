//go:build !linux

package cmd

import (
	"github.com/spf13/cobra"
)

// newSandboxRelayCmd registers the hidden sandbox-relay subcommand on
// platforms without a network-namespace relay. It exists only so the command
// tree stays identical across builds; invoking it always fails.
func newSandboxRelayCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "sandbox-relay",
		Short:  "Internal: bridge sandbox loopback traffic to the daemon network proxy",
		Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Usage()
		},
	}
}
