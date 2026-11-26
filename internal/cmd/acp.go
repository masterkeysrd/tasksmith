package cmd

import (
	"fmt"

	"github.com/masterkeysrd/tasksmith/internal/acp"
	"github.com/spf13/cobra"
)

func newACPCmd(opts *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "acp",
		Short: "Serve the Agent Client Protocol over stdio",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			socket, err := opts.resolveSocket()
			if err != nil {
				return err
			}

			cwd, err := opts.resolveWorkspace()
			if err != nil {
				return err
			}

			c, notices, err := ConnectOrStart(cmd.Context(), socket)
			for _, s := range notices {
				if _, writeErr := fmt.Fprintln(cmd.ErrOrStderr(), s); writeErr != nil {
					return writeErr
				}
			}
			if err != nil {
				return err
			}

			return acp.Serve(cmd.Context(), c.WithCWD(cwd), cmd.InOrStdin(), cmd.OutOrStdout())
		},
	}
}
