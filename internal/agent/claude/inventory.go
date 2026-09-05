package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"

	"github.com/scarb/skope/internal/agent"
	"github.com/scarb/skope/internal/host"
	"github.com/scarb/skope/internal/skill"
)

func (a Adapter) Inventory(ctx context.Context, env host.Env) (agent.Inventory, error) {
	if err := ctx.Err(); err != nil {
		return agent.Inventory{}, err
	}
	scanned, err := a.Scanner.ScanClaude(env)
	if err != nil {
		return agent.Inventory{}, fmt.Errorf("scan Claude skills: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return agent.Inventory{}, err
	}
	skillNames, err := a.readSkillNames(ctx, env, scanned.ProjectRoot)
	if err != nil {
		return agent.Inventory{}, err
	}
	return agent.Inventory{
		Skills:     cloneSkills(scanned.Skills),
		SkillNames: skillNames,
		PluginIDs:  make([]string, 0),
		Collisions: cloneCollisions(scanned.Collisions),
		Warnings:   make([]string, 0),
	}, nil
}

func (a Adapter) readSkillNames(ctx context.Context, env host.Env, projectRoot string) ([]string, error) {
	configDirectory := strings.TrimSpace(env.Get("CLAUDE_CONFIG_DIR"))
	if configDirectory == "" {
		configDirectory = joinPath(env.Home(), ".claude")
	}
	paths := []string{
		joinPath(configDirectory, "settings.json"),
		joinPath(projectRoot, ".claude", "settings.json"),
		joinPath(projectRoot, ".claude", "settings.local.json"),
	}
	names := make(map[string]struct{})
	for _, name := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		contents, err := a.FS.ReadFile(name)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		settings, err := parseSettings(contents)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", name, err)
		}
		for skillName := range settings.SkillOverrides {
			names[skillName] = struct{}{}
		}
	}
	sorted := make([]string, 0, len(names))
	for name := range names {
		sorted = append(sorted, name)
	}
	slices.Sort(sorted)
	return sorted, nil
}

type settingsFile struct {
	SkillOverrides map[string]string `json:"skillOverrides"`
}

func parseSettings(contents []byte) (settingsFile, error) {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	var root json.RawMessage
	if err := decoder.Decode(&root); err != nil {
		return settingsFile{}, err
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return settingsFile{}, fmt.Errorf("trailing JSON value")
		}
		return settingsFile{}, err
	}

	trimmedRoot := bytes.TrimSpace(root)
	if len(trimmedRoot) == 0 || trimmedRoot[0] != '{' {
		return settingsFile{}, fmt.Errorf("settings root must be an object")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(trimmedRoot, &fields); err != nil {
		return settingsFile{}, err
	}
	overrides, exists := fields["skillOverrides"]
	if exists {
		trimmedOverrides := bytes.TrimSpace(overrides)
		if len(trimmedOverrides) == 0 || trimmedOverrides[0] != '{' {
			return settingsFile{}, fmt.Errorf("skillOverrides must be an object")
		}
	}

	var settings settingsFile
	if err := json.Unmarshal(trimmedRoot, &settings); err != nil {
		return settingsFile{}, err
	}
	return settings, nil
}

func joinPath(elements ...string) string {
	native := make([]string, len(elements))
	for index, element := range elements {
		native[index] = filepath.FromSlash(element)
	}
	return filepath.ToSlash(filepath.Join(native...))
}

func cloneSkills(source []skill.Skill) []skill.Skill {
	cloned := make([]skill.Skill, len(source))
	for index, candidate := range source {
		cloned[index] = skill.Skill{ID: candidate.ID, Locations: cloneLocations(candidate.Locations)}
	}
	return cloned
}

func cloneLocations(source []skill.Location) []skill.Location {
	cloned := make([]skill.Location, len(source))
	for index, location := range source {
		cloned[index] = location
		cloned[index].Names = make(map[skill.Agent]string, len(location.Names))
		for name, value := range location.Names {
			cloned[index].Names[name] = value
		}
	}
	return cloned
}

func cloneCollisions(source []skill.Collision) []skill.Collision {
	cloned := make([]skill.Collision, len(source))
	for index, collision := range source {
		cloned[index] = collision
		cloned[index].IDs = append([]string(nil), collision.IDs...)
		cloned[index].Paths = append([]string(nil), collision.Paths...)
	}
	return cloned
}
