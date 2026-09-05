package claude

import (
	"encoding/json"
	"fmt"

	"github.com/scarb/skope/internal/agent"
	"github.com/scarb/skope/internal/session"
	"github.com/scarb/skope/internal/skill"
)

type settingsFile struct {
	SkillOverrides map[string]string `json:"skillOverrides"`
}

var _ agent.Adapter = Adapter{}

func (Adapter) Plan(resolved skill.Resolved, inv agent.Inventory, sess *session.Session) (agent.LaunchPlan, error) {
	overrides := make(map[string]string)
	for _, candidate := range inv.Skills {
		for _, location := range candidate.Locations {
			if name := location.Names[skill.AgentClaude]; name != "" {
				overrides[name] = "off"
			}
		}
	}
	for _, name := range inv.SkillNames {
		if name != "" {
			overrides[name] = "off"
		}
	}
	for _, name := range resolved.AllowedNames() {
		if name != "" {
			overrides[name] = "on"
		}
	}

	data, err := json.MarshalIndent(settingsFile{SkillOverrides: overrides}, "", "  ")
	if err != nil {
		return agent.LaunchPlan{}, fmt.Errorf("marshal Claude settings: %w", err)
	}
	data = append(data, '\n')
	settingsPath := sess.AgentPath("settings.json")
	return agent.LaunchPlan{
		ControlArgs: []string{"--settings", settingsPath},
		Env:         make(map[string]string),
		Files: []agent.PlannedFile{{
			Path: settingsPath,
			Data: data,
			Mode: 0o600,
		}},
	}, nil
}
