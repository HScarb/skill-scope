package launch

import (
	"github.com/scarb/skope/internal/agent"
	"github.com/scarb/skope/internal/skill"
)

func mergeForeignInventory(native agent.Inventory, foreign skill.ScanResult) (agent.Inventory, []skill.ScanRejection) {
	candidates := make([]skill.Skill, 0, len(native.Skills)+len(foreign.Skills))
	candidates = append(candidates, native.Skills...)
	candidates = append(candidates, foreign.Skills...)
	merged := agent.Inventory{
		SkillNames: append([]string(nil), native.SkillNames...),
		PluginIDs:  append([]string(nil), native.PluginIDs...),
		Warnings:   append([]string(nil), native.Warnings...),
	}
	merged.Warnings = append(merged.Warnings, foreign.Warnings...)
	merged.Skills, merged.Collisions = skill.Merge(candidates)
	return merged, append([]skill.ScanRejection(nil), foreign.Rejections...)
}
