package claude

import (
	"encoding/json"
	"fmt"

	"github.com/scarb/skope/internal/agent"
	"github.com/scarb/skope/internal/session"
	"github.com/scarb/skope/internal/skill"
)

type generatedSettings struct {
	SkillOverrides       map[string]string `json:"skillOverrides"`
	EnabledPlugins       map[string]bool   `json:"enabledPlugins"`
	DisableBundledSkills bool              `json:"disableBundledSkills"`
}

var _ agent.Adapter = Adapter{}

func (a Adapter) Plan(resolved skill.Resolved, inv agent.Inventory, sess *session.Session) (agent.LaunchPlan, error) {
	overrides := make(map[string]string)
	pluginNames := make(map[string]bool)
	ordinaryNames := make(map[string]bool)
	for _, candidate := range inv.Skills {
		for _, location := range candidate.Locations {
			if name := location.Names[skill.AgentClaude]; name != "" {
				if location.Level == skill.LevelPlugin || location.PluginID != "" || location.PluginAgent != "" {
					pluginNames[name] = true
				} else {
					ordinaryNames[name] = true
					overrides[name] = "off"
				}
			}
		}
	}
	for _, entry := range resolved.Entries {
		if entry.State == skill.StateProjected {
			for _, name := range entry.Names {
				if name != "" {
					ordinaryNames[name] = true
					overrides[name] = "off"
				}
			}
		}
	}
	for _, name := range inv.SkillNames {
		if name != "" {
			overrides[name] = "off"
		}
	}
	for _, name := range resolved.AllowedNames() {
		if name != "" && (!pluginNames[name] || ordinaryNames[name]) {
			overrides[name] = "on"
		}
	}
	plugins := make(map[string]bool)
	for _, id := range inv.PluginIDs {
		plugins[id] = false
	}
	for _, id := range a.options.Plugins {
		plugins[id] = true
	}

	data, err := json.MarshalIndent(generatedSettings{
		SkillOverrides:       overrides,
		EnabledPlugins:       plugins,
		DisableBundledSkills: !a.options.Bundled,
	}, "", "  ")
	if err != nil {
		return agent.LaunchPlan{}, fmt.Errorf("marshal Claude settings: %w", err)
	}
	data = append(data, '\n')
	settingsPath := sess.AgentPath("settings.json")
	controlArgs := []string{"--settings", settingsPath}
	if resolved.Count(skill.StateProjected) > 0 {
		controlArgs = append(controlArgs, "--add-dir", sess.AgentPath("addDir"))
	}
	return agent.LaunchPlan{
		ControlArgs: controlArgs,
		Env:         make(map[string]string),
		Files: []agent.PlannedFile{{
			Path: settingsPath,
			Data: data,
			Mode: 0o600,
		}},
	}, nil
}
