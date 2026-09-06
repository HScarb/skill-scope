package codex

import (
	"context"
	"fmt"
	"slices"

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
	warnings := slices.Clone(snapshot.Warnings)
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
