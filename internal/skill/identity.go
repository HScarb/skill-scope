// Package skill models discovered coding-agent skills and resolves their identities.
package skill

import (
	"cmp"
	"path"
	"slices"
	"strings"
)

func Build(locations []Location) ([]Skill, []Collision) {
	candidates := make([]Skill, 0, len(locations))
	for _, location := range locations {
		candidates = append(candidates, Skill{ID: locationID(location), Locations: []Location{location}})
	}
	return Merge(candidates)
}

// Merge preserves scanner-assigned IDs while combining locations and collisions.
func Merge(candidates []Skill) ([]Skill, []Collision) {
	grouped := make(map[string][]Location)
	seen := make(map[string]struct{})
	for _, candidate := range candidates {
		for _, location := range candidate.Locations {
			key := string(location.Source) + "\x00" + location.DiscoveryPath
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}

			location = cloneLocation(location)
			grouped[candidate.ID] = append(grouped[candidate.ID], location)
		}
	}

	ids := make([]string, 0, len(grouped))
	for id := range grouped {
		ids = append(ids, id)
	}
	slices.Sort(ids)

	skills := make([]Skill, 0, len(ids))
	for _, id := range ids {
		group := grouped[id]
		slices.SortFunc(group, compareLocations)
		skills = append(skills, Skill{ID: id, Locations: group})
	}

	return skills, buildCollisions(skills)
}

func cloneLocation(location Location) Location {
	if location.Names == nil {
		return location
	}

	names := make(map[Agent]string, len(location.Names))
	for agent, name := range location.Names {
		names[agent] = name
	}
	location.Names = names
	return location
}

func locationID(location Location) string {
	discoveryPath := strings.ReplaceAll(location.DiscoveryPath, "\\", "/")
	var name string
	if location.Kind == KindCommand {
		// The scanner resolves the command namespace relative to its discovery root.
		if effectiveName := location.Names[AgentClaude]; effectiveName != "" {
			return effectiveName
		}
		name = commandName(discoveryPath)
	} else {
		name = path.Base(path.Dir(discoveryPath))
	}

	scope := strings.Trim(strings.ReplaceAll(location.Scope, "\\", "/"), "/")
	if scope == "" {
		return name
	}
	return scope + ":" + name
}

func commandName(discoveryPath string) string {
	const marker = "/commands/"
	var name string
	if index := strings.LastIndex(discoveryPath, marker); index >= 0 {
		name = discoveryPath[index+len(marker):]
	} else {
		name = path.Base(discoveryPath)
	}
	name = strings.TrimSuffix(name, path.Ext(name))
	return strings.ReplaceAll(strings.Trim(name, "/"), "/", ":")
}

func compareLocations(left, right Location) int {
	if order := cmp.Compare(levelPriority(left.Level), levelPriority(right.Level)); order != 0 {
		return order
	}
	if order := cmp.Compare(sourcePriority(left.Source), sourcePriority(right.Source)); order != 0 {
		return order
	}
	return cmp.Compare(left.DiscoveryPath, right.DiscoveryPath)
}

func levelPriority(level Level) int {
	switch level {
	case LevelProject:
		return 0
	case LevelGlobal:
		return 1
	case LevelAdmin:
		return 2
	case LevelPlugin:
		return 3
	default:
		return 4
	}
}

func sourcePriority(source Source) int {
	switch source {
	case SourceClaude:
		return 0
	case SourceAgents:
		return 1
	case SourceCodex:
		return 2
	case SourceOpenCode:
		return 3
	case SourceOpenCodePath:
		return 4
	default:
		return 5
	}
}

func buildCollisions(skills []Skill) []Collision {
	var collisions []Collision
	effectiveNames := make(map[effectiveNameKey]*effectiveNameGroup)

	for _, candidate := range skills {
		if collision, ok := differentContentCollision(candidate); ok {
			collisions = append(collisions, collision)
		}
		for _, location := range candidate.Locations {
			if location.Kind == KindSkill && location.FrontmatterName != "" && location.FrontmatterName != skillDirectoryName(location) {
				collisions = append(collisions, Collision{
					Kind:  CollisionFrontmatterName,
					IDs:   []string{candidate.ID},
					Name:  location.FrontmatterName,
					Paths: []string{location.DiscoveryPath},
				})
			}
			collectEffectiveNames(effectiveNames, candidate.ID, location)
		}
	}

	for key, group := range effectiveNames {
		if len(group.ids) < 2 {
			continue
		}
		collisions = append(collisions, Collision{
			Kind:  CollisionEffectiveName,
			IDs:   sortedKeys(group.ids),
			Agent: key.agent,
			Name:  key.name,
			Paths: sortedKeys(group.paths),
		})
	}

	slices.SortFunc(collisions, compareCollisions)
	return collisions
}

func differentContentCollision(candidate Skill) (Collision, bool) {
	realPaths := make(map[string]struct{})
	paths := make(map[string]struct{})
	for _, location := range candidate.Locations {
		paths[location.DiscoveryPath] = struct{}{}
		if location.RealPath != "" {
			realPaths[location.RealPath] = struct{}{}
		}
	}
	if len(realPaths) < 2 {
		return Collision{}, false
	}
	return Collision{
		Kind:  CollisionDifferentContent,
		IDs:   []string{candidate.ID},
		Paths: sortedKeys(paths),
	}, true
}

func skillDirectoryName(location Location) string {
	discoveryPath := strings.ReplaceAll(location.DiscoveryPath, "\\", "/")
	return path.Base(path.Dir(discoveryPath))
}

type effectiveNameKey struct {
	agent Agent
	name  string
}

type effectiveNameGroup struct {
	ids   map[string]struct{}
	paths map[string]struct{}
}

func collectEffectiveNames(groups map[effectiveNameKey]*effectiveNameGroup, id string, location Location) {
	for agent, name := range location.Names {
		if name == "" {
			continue
		}
		key := effectiveNameKey{agent: agent, name: name}
		group := groups[key]
		if group == nil {
			group = &effectiveNameGroup{
				ids:   make(map[string]struct{}),
				paths: make(map[string]struct{}),
			}
			groups[key] = group
		}
		group.ids[id] = struct{}{}
		group.paths[location.DiscoveryPath] = struct{}{}
	}
}

func sortedKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for value := range values {
		keys = append(keys, value)
	}
	slices.Sort(keys)
	return keys
}

func compareCollisions(left, right Collision) int {
	if order := cmp.Compare(left.Kind, right.Kind); order != 0 {
		return order
	}
	if order := cmp.Compare(left.Agent, right.Agent); order != 0 {
		return order
	}
	if order := cmp.Compare(left.Name, right.Name); order != 0 {
		return order
	}
	if order := slices.Compare(left.IDs, right.IDs); order != 0 {
		return order
	}
	return slices.Compare(left.Paths, right.Paths)
}
