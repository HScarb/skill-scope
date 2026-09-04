package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/scarb/skope/internal/config"
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

func reportLaunch(writer io.Writer, agent skill.Agent, parsed parsedLaunchArgs, result launch.Result) error {
	displayName, err := selectedDisplayName(parsed.setValue, result.NoIsolation)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "→ Skillset [%s] for %s\n", displayName, agent); err != nil {
		return err
	}

	if result.NoIsolation {
		if _, err := fmt.Fprintln(writer, "  no isolation: existing agent configuration is unchanged"); err != nil {
			return err
		}
	} else if err := reportResolution(writer, result); err != nil {
		return err
	}
	if err := reportWarnings(writer, result); err != nil {
		return err
	}
	if err := reportCollisions(writer, result); err != nil {
		return err
	}

	if parsed.dryRun {
		return reportDryRun(writer, result)
	}
	_, err = fmt.Fprintf(writer, "→ Launching %s\n", agent)
	return err
}

func selectedDisplayName(value string, noIsolation bool) (string, error) {
	if noIsolation {
		return "none", nil
	}
	names, err := config.ParseSelection(value)
	if err != nil {
		return "", fmt.Errorf("format skill set selection: %w", err)
	}
	return strings.Join(names, "+"), nil
}

func reportResolution(writer io.Writer, result launch.Result) error {
	native := result.Resolved.Count(skill.StateNative)
	missing := result.Resolved.Count(skill.StateMissing)
	if _, err := fmt.Fprintf(writer, "  skills: %d native, %d missing\n", native, missing); err != nil {
		return err
	}

	missingIDs := make([]string, 0, missing)
	for _, entry := range result.Resolved.Entries {
		if entry.State == skill.StateMissing {
			missingIDs = append(missingIDs, entry.ID)
		}
	}
	if len(missingIDs) == 0 {
		return nil
	}
	_, err := fmt.Fprintf(writer, "           missing: %s\n", strings.Join(missingIDs, ", "))
	return err
}

func reportWarnings(writer io.Writer, result launch.Result) error {
	for _, warning := range result.Warnings {
		if _, err := fmt.Fprintf(writer, "  warning: %v\n", warning); err != nil {
			return err
		}
	}
	for _, warning := range result.Inventory.Warnings {
		if _, err := fmt.Fprintf(writer, "  warning: %s\n", warning); err != nil {
			return err
		}
	}
	return nil
}

func reportCollisions(writer io.Writer, result launch.Result) error {
	for _, collision := range result.Inventory.Collisions {
		if _, err := fmt.Fprintf(writer, "  collision %s: IDs=%s", collision.Kind, strings.Join(collision.IDs, ",")); err != nil {
			return err
		}
		if collision.Agent != "" {
			if _, err := fmt.Fprintf(writer, " agent=%s", collision.Agent); err != nil {
				return err
			}
		}
		if collision.Name != "" {
			if _, err := fmt.Fprintf(writer, " name=%s", collision.Name); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(writer, " paths=%s\n", strings.Join(collision.Paths, ",")); err != nil {
			return err
		}
	}
	return nil
}

func reportDryRun(writer io.Writer, result launch.Result) error {
	if _, err := fmt.Fprintf(writer, "Dry run:\nExecutable: %s\nArgv:\n", result.Executable); err != nil {
		return err
	}
	argv := make([]string, 0, len(result.Args)+1)
	argv = append(argv, result.Executable)
	argv = append(argv, result.Args...)
	for index, arg := range argv {
		if _, err := fmt.Fprintf(writer, "  [%d] %s\n", index, strconv.Quote(arg)); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprintln(writer, "Session files:"); err != nil {
		return err
	}
	for _, file := range result.Plan.Files {
		relative, err := sessionRelativePath(result, file.Path)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(writer, "  %s\n", relative); err != nil {
			return err
		}
	}
	for _, file := range result.Plan.Files {
		relative, err := sessionRelativePath(result, file.Path)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(writer, "Contents %s:\n%s", relative, file.Data); err != nil {
			return err
		}
		if len(file.Data) == 0 || file.Data[len(file.Data)-1] != '\n' {
			if _, err := fmt.Fprintln(writer); err != nil {
				return err
			}
		}
	}
	return nil
}

func sessionRelativePath(result launch.Result, path string) (string, error) {
	if result.Session == nil {
		return "", errors.New("dry-run result has session files but no session")
	}
	relative, err := filepath.Rel(result.Session.Root, path)
	if err != nil {
		return "", fmt.Errorf("make session file path relative: %w", err)
	}
	if relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", fmt.Errorf("session file path %q is outside session root %q", path, result.Session.Root)
	}
	return filepath.ToSlash(relative), nil
}
