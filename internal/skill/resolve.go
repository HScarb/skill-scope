package skill

import "slices"

type ResolutionState string
type ResolutionReason string

const (
	ReasonSpecialFile   ResolutionReason = "special-file"
	ReasonLimitExceeded ResolutionReason = "limit-exceeded"
)

const (
	StateNative      ResolutionState = "native"
	StateProjected   ResolutionState = "projected"
	StateUnavailable ResolutionState = "unavailable"
	StateMissing     ResolutionState = "missing"
)

type Resolution struct {
	ID       string
	State    ResolutionState
	Names    []string
	Location *Location
	Reason   ResolutionReason
}

type Resolved struct {
	Agent   Agent
	Entries []Resolution
}

func ResolveNative(agent Agent, selected []string, inventory []Skill) Resolved {
	byID := make(map[string]Skill, len(inventory))
	for _, candidate := range inventory {
		byID[candidate.ID] = candidate
	}

	resolved := Resolved{Agent: agent}
	seen := make(map[string]struct{}, len(selected))
	for _, id := range selected {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}

		candidate, ok := byID[id]
		if !ok {
			resolved.Entries = append(resolved.Entries, Resolution{ID: id, State: StateMissing})
			continue
		}

		names := make(map[string]struct{}, len(candidate.Locations))
		for _, location := range candidate.Locations {
			if name := location.Names[agent]; name != "" {
				names[name] = struct{}{}
			}
		}
		resolved.Entries = append(resolved.Entries, Resolution{
			ID:    id,
			State: StateNative,
			Names: sortedNames(names),
		})
	}
	return resolved
}

func (r Resolved) Count(state ResolutionState) int {
	count := 0
	for _, entry := range r.Entries {
		if entry.State == state {
			count++
		}
	}
	return count
}

func (r Resolved) AllowedNames() []string {
	names := make(map[string]struct{})
	for _, entry := range r.Entries {
		if entry.State != StateNative {
			continue
		}
		for _, name := range entry.Names {
			names[name] = struct{}{}
		}
	}
	return sortedNames(names)
}

func sortedNames(names map[string]struct{}) []string {
	sorted := make([]string, 0, len(names))
	for name := range names {
		sorted = append(sorted, name)
	}
	slices.Sort(sorted)
	return sorted
}
