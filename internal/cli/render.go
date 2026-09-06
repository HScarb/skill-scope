package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/scarb/skope/internal/config"
	"github.com/scarb/skope/internal/launch"
	"github.com/scarb/skope/internal/skill"
	"github.com/scarb/skope/internal/termsafe"
	"github.com/spf13/cobra"
)

// Render the standard help layout without Help/UsageString's process exit path.
func renderHelp(cmd *cobra.Command) error {
	description := cmd.Long
	if description == "" {
		description = cmd.Short
	}
	if description = strings.TrimRightFunc(description, unicode.IsSpace); description != "" {
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\n\n", description); err != nil {
			return err
		}
	}
	if !cmd.Runnable() && !cmd.HasSubCommands() {
		return nil
	}
	// Usage returns its template error but also prints it; retain the error and
	// leave all diagnostic output to Application.Execute's safe error boundary.
	previous := cmd.ErrOrStderr()
	var diagnostics bytes.Buffer
	cmd.SetErr(&diagnostics)
	err := cmd.Usage()
	cmd.SetErr(previous)
	if err != nil {
		return err
	}
	if diagnostics.Len() != 0 {
		return errors.New(strings.TrimSuffix(diagnostics.String(), "\n"))
	}
	return nil
}

func reportLaunch(writer io.Writer, agent skill.Agent, parsed parsedLaunchArgs, result launch.Result) error {
	displayName, err := selectedDisplayName(parsed.setValue, result.NoIsolation)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "→ Skillset [%s] for %s\n", termsafe.Escape(displayName), termsafe.Escape(string(agent))); err != nil {
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
	_, err = fmt.Fprintf(writer, "→ Launching %s\n", termsafe.Escape(string(agent)))
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
	summary := launch.Summarize(result.Resolved)
	if _, err := fmt.Fprintf(writer, "  skills: %d native, %d projected, %d unavailable, %d missing\n", summary.Native, summary.Projected, len(summary.Unavailable), len(summary.Missing)); err != nil {
		return err
	}

	for _, unavailable := range summary.Unavailable {
		if _, err := fmt.Fprintf(writer, "           unavailable: %s (%s)\n", termsafe.Escape(unavailable.ID), unavailableReason(unavailable.Reason)); err != nil {
			return err
		}
	}
	if len(summary.Missing) > 0 {
		if _, err := fmt.Fprintf(writer, "           missing: %s\n", termsafe.Escape(strings.Join(summary.Missing, ", "))); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(writer, "  plugins: %d allowed, %d disabled (Claude may enable dependencies of allowed plugins)\n", result.Plugins.Allowed, result.Plugins.Disabled); err != nil {
		return err
	}
	bundled := "off"
	if result.Bundled {
		bundled = "on"
	}
	_, err := fmt.Fprintf(writer, "  bundled: %s\n", bundled)
	return err
}

func unavailableReason(reason skill.ResolutionReason) string {
	switch reason {
	case skill.ReasonSpecialFile:
		return "包含非普通文件"
	case skill.ReasonLimitExceeded:
		return "超过文件数或字节数限额"
	case skill.ReasonProjectionUnsupported:
		return "目标 agent 不支持投影"
	case skill.ReasonPluginDisabled:
		return "所属 Claude plugin 未在 plugins.claude 中允许"
	case skill.ReasonPluginOnly:
		return "仅存在于其他 agent 的 plugin 中"
	case skill.ReasonCommandOnly:
		return "command 不能投影为 skill"
	case skill.ReasonOutsideRoot:
		return "引用目录外内容"
	case skill.ReasonTargetConflict:
		return "投影目标路径或名称冲突"
	case skill.ReasonSymlinkLoop:
		return "符号链接循环或层级过深"
	case skill.ReasonPluginManifest:
		return "包含 plugin 清单，不能作为普通 skill 投影"
	case skill.ReasonInvalidPath:
		return "投影路径无效"
	default:
		return termsafe.Escape(string(reason))
	}
}

func reportWarnings(writer io.Writer, result launch.Result) error {
	for _, warning := range result.Warnings {
		if _, err := fmt.Fprintf(writer, "  warning: %s\n", termsafe.Escape(fmt.Sprint(warning))); err != nil {
			return err
		}
	}
	for _, warning := range result.Inventory.Warnings {
		if _, err := fmt.Fprintf(writer, "  warning: %s\n", termsafe.Escape(warning)); err != nil {
			return err
		}
	}
	return nil
}

func reportCollisions(writer io.Writer, result launch.Result) error {
	for _, collision := range result.Inventory.Collisions {
		if _, err := fmt.Fprintf(writer, "  collision %s: IDs=%s", termsafe.Escape(string(collision.Kind)), termsafe.Escape(strings.Join(collision.IDs, ","))); err != nil {
			return err
		}
		if collision.Agent != "" {
			if _, err := fmt.Fprintf(writer, " agent=%s", termsafe.Escape(string(collision.Agent))); err != nil {
				return err
			}
		}
		if collision.Name != "" {
			if _, err := fmt.Fprintf(writer, " name=%s", termsafe.Escape(collision.Name)); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(writer, " paths=%s\n", termsafe.Escape(strings.Join(collision.Paths, ","))); err != nil {
			return err
		}
	}
	return nil
}

func reportDryRun(writer io.Writer, result launch.Result) error {
	if _, err := fmt.Fprintf(writer, "Dry run:\nExecutable: %s\nArgv:\n", termsafe.Escape(result.Executable)); err != nil {
		return err
	}
	argv := make([]string, 0, len(result.Args)+1)
	argv = append(argv, result.Executable)
	argv = append(argv, result.Args...)
	for index, arg := range argv {
		if _, err := fmt.Fprintf(writer, "  [%d] %s\n", index, strconv.Quote(termsafe.Escape(arg))); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprintln(writer, "Environment changes:"); err != nil {
		return err
	}
	keys := make([]string, 0, len(result.Plan.Env))
	for key := range result.Plan.Env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if _, err := fmt.Fprintf(writer, "  %s=%s\n", termsafe.Escape(key), termsafe.EnvValue(key, result.Plan.Env[key])); err != nil {
			return err
		}
	}
	paths := make(map[string]struct{})
	if result.Session != nil {
		paths[filepath.Join(result.Session.Root, "owner.json")] = struct{}{}
	}
	for _, file := range result.Plan.Files {
		paths[file.Path] = struct{}{}
	}
	for _, file := range result.ProjectionFiles {
		paths[file.Path] = struct{}{}
	}
	relatives := make(map[string]struct{}, len(paths))
	for path := range paths {
		relative, err := sessionRelativePath(result, path)
		if err != nil {
			return err
		}
		relatives[relative] = struct{}{}
	}
	files := make([]string, 0, len(relatives))
	for relative := range relatives {
		files = append(files, relative)
	}
	sort.Strings(files)
	if len(files) > 0 {
		if _, err := fmt.Fprintln(writer, "Session files:"); err != nil {
			return err
		}
	}
	for _, relative := range files {
		if _, err := fmt.Fprintf(writer, "  %s\n", termsafe.Escape(relative)); err != nil {
			return err
		}
	}
	for _, file := range result.Plan.Files {
		relative, err := sessionRelativePath(result, file.Path)
		if err != nil {
			return err
		}
		if relative != "claude/settings.json" {
			continue
		}
		if _, err := fmt.Fprintf(writer, "Contents %s:\n", relative); err != nil {
			return err
		}
		// Only the generated settings are printable; preserve its layout newlines.
		for _, line := range strings.Split(strings.TrimSuffix(string(file.Data), "\n"), "\n") {
			if _, err := fmt.Fprintln(writer, termsafe.Escape(line)); err != nil {
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
	if !filepath.IsAbs(result.Session.Root) || !filepath.IsAbs(path) {
		return "", fmt.Errorf("session file path %q and session root %q must be absolute", path, result.Session.Root)
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
