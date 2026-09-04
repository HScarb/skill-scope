// Package cli defines the skope command tree. It contains no business
// logic; every command delegates to an internal package.
package cli

import (
	"io"

	"github.com/scarb/skope/internal/config"
	"github.com/spf13/cobra"
)

type Application struct {
	LoadSkillSets func() (path string, sets config.SkillSets, err error)
}

// Execute runs skope with the given arguments and returns the process
// exit code. It never calls os.Exit so tests can drive it directly.
func Execute(args []string, stdout, stderr io.Writer, version string) int {
	return (Application{LoadSkillSets: loadSkillSets}).Execute(args, stdout, stderr, version)
}

// Execute runs this application with the given arguments and returns the
// process exit code.
func (a Application) Execute(args []string, stdout, stderr io.Writer, version string) int {
	root := newRootCmd(version, a.LoadSkillSets)
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)

	if err := root.Execute(); err != nil {
		return 1
	}
	return 0
}

func newRootCmd(version string, loadSkillSets listLoader) *cobra.Command {
	root := &cobra.Command{
		Use:           "skope",
		Short:         "Launch coding agents with a session-scoped skill whitelist",
		SilenceUsage:  true,
		SilenceErrors: false,
	}
	root.AddCommand(newListCmd(loadSkillSets), newVersionCmd(version))
	return root
}
