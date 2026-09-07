package skill

import (
	"errors"
	"fmt"
	"slices"
)

type ResolutionState string
type ResolutionReason string

const (
	ReasonSpecialFile           ResolutionReason = "special-file"
	ReasonLimitExceeded         ResolutionReason = "limit-exceeded"
	ReasonProjectionUnsupported ResolutionReason = "projection-unsupported"
	ReasonPluginDisabled        ResolutionReason = "plugin-disabled"
	ReasonPluginOnly            ResolutionReason = "plugin-only"
	ReasonCommandOnly           ResolutionReason = "command-only"
	ReasonOutsideRoot           ResolutionReason = "outside-root"
	ReasonTargetConflict        ResolutionReason = "target-conflict"
	ReasonSymlinkLoop           ResolutionReason = "symlink-loop"
	ReasonPluginManifest        ResolutionReason = "plugin-manifest"
	ReasonInvalidPath           ResolutionReason = "invalid-path"
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

type ResolveOptions struct {
	Projection     bool
	AllowedPlugins []string
}

type ProjectionCheck func(Location) (ResolutionReason, error)

func Resolve(target Agent, selected []string, inventory []Skill, opts ResolveOptions, check ProjectionCheck) (Resolved, error) {
	byID := make(map[string]Skill, len(inventory))
	for _, candidate := range inventory {
		byID[candidate.ID] = candidate
	}
	resolved := Resolved{Agent: target}
	seen := make(map[string]bool, len(selected))
	for _, id := range selected {
		if seen[id] {
			continue
		}
		seen[id] = true
		candidate, ok := byID[id]
		if !ok {
			resolved.Entries = append(resolved.Entries, Resolution{ID: id, State: StateMissing})
			continue
		}
		entry, err := resolveCandidate(target, candidate, opts, check)
		if err != nil {
			return Resolved{}, fmt.Errorf("resolve skill %q: %w", id, err)
		}
		resolved.Entries = append(resolved.Entries, entry)
	}
	return resolved, nil
}

func resolveCandidate(target Agent, candidate Skill, opts ResolveOptions, check ProjectionCheck) (Resolution, error) {
	names := make(map[string]struct{})
	for _, loc := range candidate.Locations {
		if isPluginLocation(loc) && (loc.PluginAgent != target || loc.PluginID == "" || !slices.Contains(opts.AllowedPlugins, loc.PluginID)) {
			continue
		}
		if name := loc.Names[target]; name != "" {
			names[name] = struct{}{}
		}
	}
	if len(names) > 0 {
		return Resolution{ID: candidate.ID, State: StateNative, Names: sortedNames(names)}, nil
	}
	entry := Resolution{ID: candidate.ID, State: StateUnavailable, Reason: ReasonProjectionUnsupported}
	if !opts.Projection {
		excluded := make(map[ResolutionReason]bool)
		for _, loc := range candidate.Locations {
			reason := projectionIneligible(target, loc)
			if reason == "" {
				return entry, nil
			}
			excluded[reason] = true
		}
		for _, reason := range []ResolutionReason{ReasonPluginDisabled, ReasonPluginOnly, ReasonCommandOnly} {
			if excluded[reason] {
				entry.Reason = reason
				break
			}
		}
		return entry, nil
	}
	var rejected, excluded ResolutionReason
	for _, loc := range candidate.Locations {
		if reason := projectionIneligible(target, loc); reason != "" {
			if excluded == "" {
				excluded = reason
			}
			continue
		}
		if check == nil {
			return Resolution{}, errors.New("projection check is required")
		}
		reason, err := check(cloneLocation(loc))
		if err != nil {
			return Resolution{}, err
		}
		if reason != "" {
			if rejected == "" {
				rejected = reason
			}
			continue
		}
		location := cloneLocation(loc)
		name := skillDirectoryName(loc)
		if (target == AgentCodex || target == AgentOpenCode) && loc.FrontmatterName != "" {
			name = loc.FrontmatterName
		}
		return Resolution{ID: candidate.ID, State: StateProjected, Names: []string{name}, Location: &location}, nil
	}
	if rejected != "" {
		entry.Reason = rejected
	} else if excluded != "" {
		entry.Reason = excluded
	}
	return entry, nil
}

func isPluginLocation(loc Location) bool {
	return loc.Level == LevelPlugin || loc.PluginID != "" || loc.PluginAgent != ""
}

func projectionIneligible(target Agent, loc Location) ResolutionReason {
	if isPluginLocation(loc) {
		if loc.PluginAgent == target {
			return ReasonPluginDisabled
		}
		return ReasonPluginOnly
	}
	if loc.Kind != KindSkill {
		return ReasonCommandOnly
	}
	return ""
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
		if entry.State != StateNative && entry.State != StateProjected {
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
