package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/scarb/skope/internal/launch"
	"github.com/scarb/skope/internal/skill"
	"github.com/spf13/cobra"
)

type launchRunner func(context.Context, launch.Request, launch.Reporter) error

func newLaunchCmd(agent skill.Agent, runner launchRunner) *cobra.Command {
	cmd := &cobra.Command{
		Use:                   fmt.Sprintf("%s [skope options] [agent args...]", agent),
		Short:                 fmt.Sprintf("Launch %s with a skill set", agent),
		DisableFlagParsing:    true,
		DisableFlagsInUseLine: true,
		Args:                  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			parsed, err := parseLaunchArgs(args)
			if err != nil {
				return fmt.Errorf("parse %s launch arguments: %w", agent, err)
			}
			if runner == nil {
				return errors.New("internal configuration error: launch runner is not configured")
			}

			request := launch.Request{
				Agent:      agent,
				SetValue:   parsed.setValue,
				SetPresent: parsed.setPresent,
				DryRun:     parsed.dryRun,
				AgentArgs:  append([]string(nil), parsed.agentArgs...),
			}
			report := func(result launch.Result) error {
				return reportLaunch(cmd.OutOrStdout(), agent, parsed, result)
			}
			if err := runner(cmd.Context(), request, report); err != nil {
				if errors.Is(err, launch.ErrSetRequired) {
					return fmt.Errorf("launch %s: pass -s <name> or -s none: %w", agent, err)
				}
				return fmt.Errorf("launch %s: %w", agent, err)
			}
			return nil
		},
	}
	cmd.InitDefaultHelpFlag()
	if err := cmd.Flags().MarkHidden("help"); err != nil {
		panic(fmt.Sprintf("hide %s help flag: %v", agent, err))
	}
	return cmd
}
