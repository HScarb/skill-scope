package cli

import "github.com/spf13/cobra"

func newVersionCmd(version string) *cobra.Command {
	return &cobra.Command{
		Use:    "version",
		Hidden: true,
	}
}
