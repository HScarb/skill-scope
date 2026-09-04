package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"
)

type SkillSet struct {
	Name        string
	Description string
	Skills      []string
	Plugins     map[string][]string
	Bundled     bool
}

type SkillSets struct {
	Exists bool
	Items  []SkillSet
}

type Selection struct {
	DisplayName string
	Skills      []string
	Plugins     map[string][]string
	Bundled     bool
}

type UnknownSetError struct {
	Name      string
	Available []string
}

func (e *UnknownSetError) Error() string {
	if len(e.Available) == 0 {
		return fmt.Sprintf("unknown skill set %q; no skill sets are configured", e.Name)
	}
	return fmt.Sprintf("unknown skill set %q; available: %s", e.Name, strings.Join(e.Available, ", "))
}

type skillSetsFile struct {
	Version   *int                    `toml:"version"`
	SkillSets map[string]skillSetFile `toml:"skillsets"`
}

type skillSetFile struct {
	Description string      `toml:"description"`
	Skills      *[]string   `toml:"skills"`
	Plugins     pluginsFile `toml:"plugins"`
	Bundled     *bool       `toml:"bundled"`
}

type pluginsFile struct {
	Claude []string `toml:"claude"`
	Codex  []string `toml:"codex"`
}

func LoadSkillSets(fsys ReadFileFS, path string) (SkillSets, error) {
	data, err := fsys.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return SkillSets{Items: []SkillSet{}}, nil
	}
	if err != nil {
		return SkillSets{}, &PathError{Path: path, Err: err}
	}

	var raw skillSetsFile
	err = toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields().Decode(&raw)
	if err != nil {
		return SkillSets{}, decodePathError(path, err)
	}
	if raw.Version == nil {
		return SkillSets{}, fieldError(path, "version", errors.New("is required"))
	}
	if *raw.Version != 1 {
		return SkillSets{}, fieldError(path, "version", fmt.Errorf("unsupported version %d", *raw.Version))
	}

	order, err := skillSetOrder(data)
	if err != nil {
		return SkillSets{}, &PathError{Path: path, Err: err}
	}
	if len(order) != len(raw.SkillSets) {
		return SkillSets{}, &PathError{Path: path, Err: fmt.Errorf("internal invariant: found %d ordered skill sets for %d decoded skill sets", len(order), len(raw.SkillSets))}
	}

	items := make([]SkillSet, 0, len(order))
	for _, name := range order {
		rawSet, exists := raw.SkillSets[name]
		if !exists {
			return SkillSets{}, &PathError{Path: path, Err: fmt.Errorf("internal invariant: ordered skill set %q was not decoded", name)}
		}
		item, err := normalizeSkillSet(path, name, rawSet)
		if err != nil {
			return SkillSets{}, err
		}
		items = append(items, item)
	}

	return SkillSets{Exists: true, Items: items}, nil
}

func ParseSelection(value string) ([]string, error) {
	parts := strings.Split(value, ",")
	names := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		name := strings.TrimSpace(part)
		if name == "" {
			return nil, errors.New("skill set selection contains an empty name")
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	if _, hasNone := seen["none"]; hasNone && len(names) != 1 {
		return nil, errors.New("skill set none cannot be combined with another set")
	}
	return names, nil
}

func (s SkillSets) Merge(names []string) (Selection, error) {
	byName := make(map[string]SkillSet, len(s.Items))
	for _, item := range s.Items {
		byName[item.Name] = item
	}

	selection := Selection{
		DisplayName: strings.Join(names, "+"),
		Skills:      []string{},
		Plugins:     make(map[string][]string),
	}
	seenSkills := make(map[string]struct{})
	seenPlugins := make(map[string]map[string]struct{})
	for _, name := range names {
		item, exists := byName[name]
		if !exists {
			return Selection{}, &UnknownSetError{Name: name, Available: s.Names()}
		}
		for _, skill := range item.Skills {
			if _, exists := seenSkills[skill]; exists {
				continue
			}
			seenSkills[skill] = struct{}{}
			selection.Skills = append(selection.Skills, skill)
		}
		for agent, plugins := range item.Plugins {
			if seenPlugins[agent] == nil {
				seenPlugins[agent] = make(map[string]struct{})
			}
			for _, plugin := range plugins {
				if _, exists := seenPlugins[agent][plugin]; exists {
					continue
				}
				seenPlugins[agent][plugin] = struct{}{}
				selection.Plugins[agent] = append(selection.Plugins[agent], plugin)
			}
		}
		selection.Bundled = selection.Bundled || item.Bundled
	}
	return selection, nil
}

func (s SkillSets) Names() []string {
	names := make([]string, len(s.Items))
	for i, item := range s.Items {
		names[i] = item.Name
	}
	return names
}

func normalizeSkillSet(path, name string, raw skillSetFile) (SkillSet, error) {
	baseField := "skillsets." + name
	if err := validateSetName(name); err != nil {
		return SkillSet{}, fieldError(path, baseField, err)
	}
	if raw.Skills == nil {
		return SkillSet{}, fieldError(path, baseField+".skills", errors.New("is required"))
	}

	skills, err := normalizeUnique("skills", *raw.Skills)
	if err != nil {
		return SkillSet{}, fieldError(path, baseField+".skills", err)
	}
	plugins := make(map[string][]string)
	pluginGroups := []struct {
		agent  string
		values []string
	}{
		{agent: "claude", values: raw.Plugins.Claude},
		{agent: "codex", values: raw.Plugins.Codex},
	}
	for _, group := range pluginGroups {
		agent, values := group.agent, group.values
		normalized, err := normalizeUnique("plugins."+agent, values)
		if err != nil {
			return SkillSet{}, fieldError(path, baseField+".plugins."+agent, err)
		}
		for _, id := range normalized {
			if err := validatePluginID(id); err != nil {
				return SkillSet{}, fieldError(path, baseField+".plugins."+agent, err)
			}
		}
		if len(normalized) != 0 {
			plugins[agent] = normalized
		}
	}

	bundled := true
	if raw.Bundled != nil {
		bundled = *raw.Bundled
	}
	return SkillSet{
		Name:        name,
		Description: raw.Description,
		Skills:      skills,
		Plugins:     plugins,
		Bundled:     bundled,
	}, nil
}

func decodePathError(path string, err error) *PathError {
	pathErr := &PathError{Path: path, Err: err}
	var decodeErr *toml.DecodeError
	if errors.As(err, &decodeErr) {
		pathErr.Field = strings.Join(decodeErr.Key(), ".")
	}
	return pathErr
}

func fieldError(path, field string, err error) *PathError {
	return &PathError{Path: path, Field: field, Err: err}
}

func skillSetOrder(data []byte) ([]string, error) {
	var parser unstable.Parser
	parser.Reset(data)
	seen := make(map[string]struct{})
	order := []string{}
	add := func(name string) {
		if _, exists := seen[name]; exists {
			return
		}
		seen[name] = struct{}{}
		order = append(order, name)
	}

	for parser.NextExpression() {
		expr := parser.Expression()
		if expr.Kind != unstable.Table && expr.Kind != unstable.KeyValue {
			continue
		}
		key := nodeKey(expr)
		if len(key) >= 2 && key[0] == "skillsets" {
			add(key[1])
			continue
		}
		if expr.Kind != unstable.KeyValue || len(key) != 1 || key[0] != "skillsets" || expr.Value().Kind != unstable.InlineTable {
			continue
		}
		children := expr.Value().Children()
		for children.Next() {
			child := children.Node()
			if child.Kind != unstable.KeyValue {
				continue
			}
			childKey := child.Key()
			if childKey.Next() {
				add(string(childKey.Node().Data))
			}
		}
	}
	if err := parser.Error(); err != nil {
		return nil, err
	}
	return order, nil
}

func nodeKey(node *unstable.Node) []string {
	key := []string{}
	iterator := node.Key()
	for iterator.Next() {
		key = append(key, string(iterator.Node().Data))
	}
	return key
}
