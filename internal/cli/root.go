// Package cli defines the skope command tree. It contains no business
// logic; every command delegates to an internal package.
package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/scarb/skope/internal/agent"
	"github.com/scarb/skope/internal/agent/claude"
	"github.com/scarb/skope/internal/config"
	"github.com/scarb/skope/internal/handoff"
	"github.com/scarb/skope/internal/host"
	"github.com/scarb/skope/internal/launch"
	"github.com/scarb/skope/internal/proc"
	"github.com/scarb/skope/internal/projection"
	"github.com/scarb/skope/internal/session"
	"github.com/scarb/skope/internal/skill"
	"github.com/scarb/skope/internal/termsafe"
	"github.com/spf13/cobra"
)

type Application struct {
	LoadSkillSets func() (path string, sets config.SkillSets, err error)
	RunLaunch     func(context.Context, launch.Request, launch.Reporter) error
}

type dependencies struct {
	snapshot func() (host.Env, error)
}

// Execute runs skope with the given arguments and returns the process
// exit code. It never calls os.Exit so tests can drive it directly.
func Execute(args []string, stdout, stderr io.Writer, version string) int {
	return productionApplication().Execute(args, stdout, stderr, version)
}

// Execute runs this application with the given arguments and returns the
// process exit code.
func (a Application) Execute(args []string, stdout, stderr io.Writer, version string) int {
	output := &errorTrackingWriter{writer: stdout}
	root := newRootCmd(version, a.LoadSkillSets, a.RunLaunch)
	root.SetArgs(args)
	root.SetOut(output)
	root.SetErr(stderr)

	err := root.Execute()
	if err == nil {
		err = output.err
	}
	if err != nil {
		fmt.Fprintf(stderr, "Error: %s\n", termsafe.Escape(err.Error()))
		return 1
	}
	return 0
}

// Cobra's help handler discards writer errors; retain them for the exit status.
type errorTrackingWriter struct {
	writer io.Writer
	err    error
}

func (w *errorTrackingWriter) Write(p []byte) (int, error) {
	n, err := w.writer.Write(p)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	if w.err == nil {
		w.err = err
	}
	return n, err
}

func newRootCmd(version string, loadSkillSets listLoader, runLaunch launchRunner) *cobra.Command {
	root := &cobra.Command{
		Use:           "skope",
		Short:         "Launch coding agents with a session-scoped skill whitelist",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetUsageTemplate(rootUsageTemplate)
	root.AddCommand(newLaunchCmd(skill.AgentClaude, runLaunch), newListCmd(loadSkillSets), newVersionCmd(version))
	return root
}

const rootUsageTemplate = `Usage:{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}{{if gt (len .Aliases) 0}}

Aliases:
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

Examples:
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

Available Commands:{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{.Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if not .AllChildCommandsHaveGroup}}

Additional Commands:{{range $cmds}}{{if (and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

Global Flags:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasHelpSubCommands}}

Additional help topics:{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{rpad .CommandPath .CommandPathPadding}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

Use "{{.CommandPath}} help [command]" for more information about a command.{{end}}
`

func productionApplication() Application {
	deps := dependencies{snapshot: host.Snapshot}
	return Application{
		LoadSkillSets: loadSkillSets,
		RunLaunch:     deps.runLaunch,
	}
}

func (d dependencies) runLaunch(ctx context.Context, request launch.Request, report launch.Reporter) error {
	env, err := d.snapshot()
	if err != nil {
		return fmt.Errorf("snapshot host environment: %w", err)
	}
	skopeHome, err := config.ResolveHome(env)
	if err != nil {
		return fmt.Errorf("resolve skope home: %w", err)
	}

	fsys := host.OSFileSystem{}
	scanner := skill.Scanner{FS: fsys, RegularFiles: fsys}
	openRoot := func(dir string) (projection.Root, error) { return host.OpenProjectionRoot(dir) }
	service := launch.Service{
		Env:       env,
		FS:        fsys,
		SkopeHome: skopeHome,
		NewRegistry: func(executable string, selection config.Selection) (launch.AdapterRegistry, error) {
			adapter := claude.New(scanner, fsys, proc.Runner{}, claude.Options{Executable: executable, Plugins: append([]string(nil), selection.Plugins["claude"]...), Bundled: selection.Bundled})
			return agent.NewRegistry(adapter)
		},
		CheckConflicts: CheckConflicts,
		Foreign:        scanner,
		Inspector:      projection.Inspector{OpenRoot: openRoot},
		Copier:         projection.Copier{OpenRoot: openRoot},
		Resolver:       host.ExecutableResolver{},
		Sessions:       session.NewManager(skopeHome),
		Handoff:        handoff.Handoff{},
	}
	return service.Run(ctx, request, report)
}
