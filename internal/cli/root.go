// Package cli defines the skope command tree. It contains no business
// logic; every command delegates to an internal package.
package cli

import (
	"io"

	"github.com/spf13/cobra"
)

// Execute runs skope with the given arguments and returns the process
// exit code. It never calls os.Exit so tests can drive it directly.
func Execute(args []string, stdout, stderr io.Writer, version string) int {
	root := newRootCmd(version)
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)

	if err := root.Execute(); err != nil {
		return 1
	}
	return 0
}

func newRootCmd(version string) *cobra.Command {
	root := &cobra.Command{
		Use:           "skope",
		Short:         "Launch coding agents with a session-scoped skill whitelist",
		SilenceUsage:  true,
		SilenceErrors: false,
	}
	root.AddCommand(newVersionCmd(version))
	return root
}
