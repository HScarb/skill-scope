package launch

import "github.com/scarb/skope/internal/skill"

type Summary struct {
	Native  int
	Missing []string
}

func Summarize(resolved skill.Resolved) Summary {
	summary := Summary{Missing: make([]string, 0)}
	for _, entry := range resolved.Entries {
		switch entry.State {
		case skill.StateNative:
			summary.Native++
		case skill.StateMissing:
			summary.Missing = append(summary.Missing, entry.ID)
		}
	}
	return summary
}
