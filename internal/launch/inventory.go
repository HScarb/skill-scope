package launch

import (
	"github.com/scarb/skope/internal/agent"
	"github.com/scarb/skope/internal/skill"
)

func mergeForeignInventory(native agent.Inventory, foreign skill.ScanResult) (agent.Inventory, []skill.ScanRejection) {
	var locations []skill.Location
	for _, candidates := range [][]skill.Skill{native.Skills, foreign.Skills} {
		for _, candidate := range candidates {
			locations = append(locations, candidate.Locations...)
		}
	}
	merged := agent.Inventory{
		SkillNames: append([]string(nil), native.SkillNames...),
		PluginIDs:  append([]string(nil), native.PluginIDs...),
		Warnings:   append([]string(nil), native.Warnings...),
	}
	merged.Skills, merged.Collisions = skill.Build(locations)
	return merged, append([]skill.ScanRejection(nil), foreign.Rejections...)
}
