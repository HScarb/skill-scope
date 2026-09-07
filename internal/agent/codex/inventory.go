package codex

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/scarb/skope/internal/agent"
	"github.com/scarb/skope/internal/host"
	"github.com/scarb/skope/internal/skill"
)

func (a Adapter) Inventory(ctx context.Context, env host.Env) (agent.Inventory, error) {
	if err := ctx.Err(); err != nil {
		return agent.Inventory{}, err
	}
	if a.scanner == nil || a.sources == nil || a.paths == nil || a.options.ResolvePaths == nil {
		return agent.Inventory{}, fmt.Errorf("codex inventory configuration requires scanner, sources, canonicalizer and path resolver")
	}
	for _, id := range a.options.Plugins {
		if !validPluginID(id) {
			return agent.Inventory{}, fmt.Errorf("codex inventory configuration contains an invalid plugin ID")
		}
	}
	paths, err := a.options.ResolvePaths(env)
	if err != nil {
		return agent.Inventory{}, err
	}
	if err := ctx.Err(); err != nil {
		return agent.Inventory{}, err
	}
	snapshot, err := a.sources.Read(ctx, env, cloneCodexPaths(paths))
	if err != nil {
		return agent.Inventory{}, err
	}
	if err := ctx.Err(); err != nil {
		return agent.Inventory{}, err
	}
	ordinary, err := a.scanner.ScanCodex(env, cloneCodexPaths(paths))
	if err != nil {
		return agent.Inventory{}, err
	}
	if err := ctx.Err(); err != nil {
		return agent.Inventory{}, err
	}
	roots := slices.Clone(snapshot.SkillRoots)
	for i := range roots {
		roots[i].VisibleTo = slices.Clone(roots[i].VisibleTo)
	}
	plugins, err := a.scanner.ScanRoots(roots)
	if err != nil {
		return agent.Inventory{}, err
	}
	if err := ctx.Err(); err != nil {
		return agent.Inventory{}, err
	}
	candidates := make([]skill.Skill, 0, len(ordinary.Skills)+len(plugins.Skills))
	candidates = append(candidates, ordinary.Skills...)
	candidates = append(candidates, plugins.Skills...)
	skills, collisions := skill.Merge(candidates)
	var names []string
	for _, candidate := range skills {
		for _, location := range candidate.Locations {
			if name := location.Names[skill.AgentCodex]; name != "" {
				names = append(names, name)
			}
		}
	}
	slices.Sort(names)
	pluginIDs := slices.Clone(snapshot.PluginIDs)
	slices.Sort(pluginIDs)
	warnings := append(slices.Clone(snapshot.Warnings), canonicalAliasWarnings(skills)...)
	seen := make(map[string]bool)
	for _, id := range a.options.Plugins {
		if !seen[id] && !slices.Contains(snapshot.InstalledIDs, id) {
			warnings = append(warnings, fmt.Sprintf("plugin %s is not installed", id))
		}
		seen[id] = true
	}
	return agent.Inventory{Skills: skills, SkillNames: slices.Compact(names), PluginIDs: slices.Compact(pluginIDs), Collisions: collisions, Warnings: warnings}, nil
}

func cloneCodexPaths(paths host.CodexPaths) host.CodexPaths {
	paths.AdminSkillRoots = slices.Clone(paths.AdminSkillRoots)
	paths.SystemConfigPaths = slices.Clone(paths.SystemConfigPaths)
	return paths
}

func canonicalAliasWarnings(skills []skill.Skill) []string {
	groups := make(map[string]map[string]struct{})
	for _, candidate := range skills {
		for _, loc := range candidate.Locations {
			if loc.Kind != skill.KindSkill || loc.RealPath == "" || loc.Names[skill.AgentCodex] == "" {
				continue
			}
			control := "ordinary " + candidate.ID
			if loc.Level == skill.LevelPlugin || loc.PluginID != "" || loc.PluginAgent != "" {
				if loc.PluginAgent != skill.AgentCodex || loc.PluginID == "" {
					continue
				}
				control = "plugin " + loc.PluginID
			}
			if groups[loc.RealPath] == nil {
				groups[loc.RealPath] = make(map[string]struct{})
			}
			groups[loc.RealPath][control] = struct{}{}
		}
	}
	var paths []string
	for path, controls := range groups {
		if len(controls) > 1 {
			paths = append(paths, path)
		}
	}
	slices.Sort(paths)
	var warnings []string
	for _, path := range paths {
		var controls []string
		for control := range groups[path] {
			controls = append(controls, control)
		}
		slices.Sort(controls)
		warnings = append(warnings, fmt.Sprintf("Codex skills share canonical path %q (%s); allowing any control source allows this path", path, strings.Join(controls, ", ")))
	}
	return warnings
}
