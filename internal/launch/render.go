package launch

import "github.com/scarb/skope/internal/skill"

type Summary struct {
	Native      int
	Projected   int
	Unavailable []UnavailableSkill
	Missing     []string
}

type UnavailableSkill struct {
	ID     string
	Reason skill.ResolutionReason
}

func Summarize(resolved skill.Resolved) Summary {
	summary := Summary{Missing: make([]string, 0)}
	for _, entry := range resolved.Entries {
		switch entry.State {
		case skill.StateNative:
			summary.Native++
		case skill.StateProjected:
			summary.Projected++
		case skill.StateUnavailable:
			summary.Unavailable = append(summary.Unavailable, UnavailableSkill{ID: entry.ID, Reason: entry.Reason})
		case skill.StateMissing:
			summary.Missing = append(summary.Missing, entry.ID)
		}
	}
	return summary
}

func summarizePlugins(inventory, allowed []string) PluginSummary {
	plugins := make(map[string]bool, len(inventory)+len(allowed))
	for _, id := range inventory {
		plugins[id] = false
	}
	for _, id := range allowed {
		plugins[id] = true
	}
	var summary PluginSummary
	for _, enabled := range plugins {
		if enabled {
			summary.Allowed++
		} else {
			summary.Disabled++
		}
	}
	return summary
}
