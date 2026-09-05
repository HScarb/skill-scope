package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"

	"github.com/scarb/skope/internal/agent"
	"github.com/scarb/skope/internal/host"
	"github.com/scarb/skope/internal/proc"
	"github.com/scarb/skope/internal/skill"
)

func (a Adapter) Inventory(ctx context.Context, env host.Env) (agent.Inventory, error) {
	if err := ctx.Err(); err != nil {
		return agent.Inventory{}, err
	}
	if a.scanner == nil || a.fs == nil || a.runner == nil || strings.TrimSpace(a.options.Executable) == "" {
		return agent.Inventory{}, fmt.Errorf("claude inventory configuration requires scanner, filesystem, probe runner and executable")
	}
	scanned, err := a.scanner.ScanClaude(env)
	if err != nil {
		return agent.Inventory{}, fmt.Errorf("scan Claude skills: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return agent.Inventory{}, err
	}
	skillNames, pluginIDs, err := a.readSettings(ctx, env, scanned.ProjectRoot)
	if err != nil {
		return agent.Inventory{}, err
	}
	result, err := a.runner.Run(ctx, proc.Request{Executable: a.options.Executable, Args: []string{"plugin", "list", "--json"}, Dir: env.Cwd(), Env: env.Environ()})
	if err != nil {
		return agent.Inventory{}, fmt.Errorf("claude plugin list: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return agent.Inventory{}, err
	}
	installations, err := parsePlugins(result.Stdout)
	if err != nil {
		return agent.Inventory{}, err
	}
	installed := make(map[string]struct{})
	for _, p := range installations {
		installed[p.ID] = struct{}{}
		pluginIDs[p.ID] = struct{}{}
	}
	warnings := make([]string, 0)
	for _, id := range a.options.Plugins {
		if _, ok := installed[id]; !ok {
			warnings = append(warnings, fmt.Sprintf("plugin %s is not installed", id))
		}
	}
	roots, err := a.pluginRoots(ctx, env, installations)
	if err != nil {
		return agent.Inventory{}, err
	}
	var locations []skill.Location
	for _, candidate := range scanned.Skills {
		locations = append(locations, candidate.Locations...)
	}
	if len(roots) > 0 {
		plugins, err := a.scanner.ScanRoots(roots)
		if err != nil {
			return agent.Inventory{}, fmt.Errorf("scan Claude plugins: %w", err)
		}
		for _, candidate := range plugins.Skills {
			locations = append(locations, candidate.Locations...)
		}
	}
	skills, collisions := skill.Build(locations)
	return agent.Inventory{Skills: skills, SkillNames: skillNames, PluginIDs: sortedSet(pluginIDs), Collisions: collisions, Warnings: warnings}, nil
}

func (a Adapter) readSettings(ctx context.Context, env host.Env, projectRoot string) ([]string, map[string]struct{}, error) {
	configDirectory := strings.TrimSpace(env.Get("CLAUDE_CONFIG_DIR"))
	if configDirectory == "" {
		configDirectory = joinPath(env.Home(), ".claude")
	}
	paths := []string{
		joinPath(configDirectory, "settings.json"),
		joinPath(projectRoot, ".claude", "settings.json"),
		joinPath(projectRoot, ".claude", "settings.local.json"),
	}
	names := make(map[string]struct{})
	plugins := make(map[string]struct{})
	for _, name := range paths {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		contents, err := a.fs.ReadFile(name)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, nil, fmt.Errorf("read %s: %w", name, err)
		}
		settings, err := parseSettings(contents)
		if err != nil {
			return nil, nil, fmt.Errorf("parse %s: %w", name, err)
		}
		for plugin := range settings.EnabledPlugins {
			plugins[plugin] = struct{}{}
		}
		for skillName := range settings.SkillOverrides {
			names[skillName] = struct{}{}
		}
	}
	return sortedSet(names), plugins, nil
}

func sortedSet(values map[string]struct{}) []string {
	sorted := make([]string, 0, len(values))
	for value := range values {
		sorted = append(sorted, value)
	}
	slices.Sort(sorted)
	return sorted
}

type parsedSettings struct {
	EnabledPlugins map[string]bool   `json:"enabledPlugins"`
	SkillOverrides map[string]string `json:"skillOverrides"`
}

func parseSettings(contents []byte) (parsedSettings, error) {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	var root json.RawMessage
	if err := decoder.Decode(&root); err != nil {
		return parsedSettings{}, err
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return parsedSettings{}, fmt.Errorf("trailing JSON value")
		}
		return parsedSettings{}, err
	}

	trimmedRoot := bytes.TrimSpace(root)
	if len(trimmedRoot) == 0 || trimmedRoot[0] != '{' {
		return parsedSettings{}, fmt.Errorf("settings root must be an object")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(trimmedRoot, &fields); err != nil {
		return parsedSettings{}, err
	}
	overrides, exists := fields["skillOverrides"]
	if exists {
		trimmedOverrides := bytes.TrimSpace(overrides)
		if len(trimmedOverrides) == 0 || trimmedOverrides[0] != '{' {
			return parsedSettings{}, fmt.Errorf("skillOverrides must be an object")
		}
	}

	if raw, exists := fields["enabledPlugins"]; exists {
		trimmed := bytes.TrimSpace(raw)
		if len(trimmed) == 0 || trimmed[0] != '{' {
			return parsedSettings{}, fmt.Errorf("enabledPlugins must be an object")
		}
		var plugins map[string]json.RawMessage
		if err := json.Unmarshal(raw, &plugins); err != nil {
			return parsedSettings{}, fmt.Errorf("enabledPlugins is invalid")
		}
		for _, value := range plugins {
			if string(value) != "true" && string(value) != "false" {
				return parsedSettings{}, fmt.Errorf("enabledPlugins values must be booleans")
			}
		}
	}
	var settings parsedSettings
	if err := json.Unmarshal(trimmedRoot, &settings); err != nil {
		return parsedSettings{}, err
	}
	return settings, nil
}

func joinPath(elements ...string) string {
	native := make([]string, len(elements))
	for index, element := range elements {
		native[index] = filepath.FromSlash(element)
	}
	return filepath.ToSlash(filepath.Join(native...))
}
