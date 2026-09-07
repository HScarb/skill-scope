package codex

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/scarb/skope/internal/agent"
	"github.com/scarb/skope/internal/session"
	"github.com/scarb/skope/internal/skill"
)

var _ agent.Adapter = Adapter{}

func (a Adapter) Plan(resolved skill.Resolved, inv agent.Inventory, _ *session.Session) (agent.LaunchPlan, error) {
	plugins := make(map[string]bool)
	for _, id := range inv.PluginIDs {
		if !validPluginID(id) {
			return agent.LaunchPlan{}, errors.New("codex plan contains an invalid plugin ID")
		}
		plugins[id] = false
	}
	for _, id := range a.options.Plugins {
		if !validPluginID(id) {
			return agent.LaunchPlan{}, errors.New("codex plan contains an invalid plugin ID")
		}
		plugins[id] = true
	}
	selected := make(map[string]bool)
	for _, entry := range resolved.Entries {
		if entry.State == skill.StateNative {
			selected[entry.ID] = true
		}
	}
	paths := make(map[string]bool)
	for _, candidate := range inv.Skills {
		for _, loc := range candidate.Locations {
			if loc.Kind != skill.KindSkill || loc.Names[skill.AgentCodex] == "" {
				continue
			}
			allowed := selected[candidate.ID]
			if loc.Level == skill.LevelPlugin || loc.PluginID != "" || loc.PluginAgent != "" {
				if loc.PluginAgent != skill.AgentCodex {
					continue
				}
				if !validPluginID(loc.PluginID) {
					return agent.LaunchPlan{}, errors.New("codex plan contains an invalid plugin ID")
				}
				allowed = plugins[loc.PluginID]
			}
			path, err := a.canonicalPath(loc)
			if err != nil {
				return agent.LaunchPlan{}, err
			}
			// Different content IDs and plugin sources can control the same canonical file.
			paths[path] = paths[path] || allowed
		}
	}
	var pathNames []string
	for path := range paths {
		pathNames = append(pathNames, path)
	}
	slices.Sort(pathNames)
	rows := make([]string, 0, len(pathNames))
	for _, path := range pathNames {
		encoded, err := tomlString(path)
		if err != nil {
			return agent.LaunchPlan{}, err
		}
		rows = append(rows, fmt.Sprintf("{path=%s,enabled=%t}", encoded, paths[path]))
	}
	var pluginIDs []string
	for id := range plugins {
		pluginIDs = append(pluginIDs, id)
	}
	slices.Sort(pluginIDs)
	pluginRows := make([]string, 0, len(pluginIDs))
	for _, id := range pluginIDs {
		encoded, err := tomlString(id)
		if err != nil {
			return agent.LaunchPlan{}, err
		}
		pluginRows = append(pluginRows, fmt.Sprintf("%s={enabled=%t}", encoded, plugins[id]))
	}
	return agent.LaunchPlan{ControlArgs: []string{
		"-c", "skills.config=[" + strings.Join(rows, ",") + "]",
		"-c", fmt.Sprintf("skills.bundled.enabled=%t", a.options.Bundled),
		"-c", "plugins={" + strings.Join(pluginRows, ",") + "}",
		"-c", "features.remote_plugin=false",
	}}, nil
}
