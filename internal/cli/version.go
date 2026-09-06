package cli

import (
	"fmt"

	"github.com/scarb/skope/internal/termsafe"
	"github.com/spf13/cobra"
)

func newVersionCmd(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the skope version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "skope %s\n", termsafe.Escape(version))
			return err
		},
	}
}
