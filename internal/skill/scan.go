package skill

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"

	"github.com/scarb/skope/internal/host"
	"gopkg.in/yaml.v3"
)

type FileSystem interface {
	ReadFile(name string) ([]byte, error)
	ReadDir(name string) ([]fs.DirEntry, error)
	Stat(name string) (fs.FileInfo, error)
	Lstat(name string) (fs.FileInfo, error)
	EvalSymlinks(name string) (string, error)
}

type ScanResult struct {
	Skills      []Skill
	Collisions  []Collision
	ProjectRoot string
}

type Scanner struct {
	FS FileSystem
}

func (s Scanner) ScanClaude(env host.Env) (ScanResult, error) {
	roots, projectRoot, err := claudeScanRoots(s.FS, env)
	if err != nil {
		return ScanResult{}, err
	}

	result, err := s.ScanRoots(roots)
	result.ProjectRoot = projectRoot
	return result, err
}

func (s Scanner) ScanRoots(roots []Root) (ScanResult, error) {
	var err error
	var locations []Location
	for _, root := range roots {
		var found []Location
		switch root.Kind {
		case KindSkill:
			found, err = s.scanSkillRoot(root)
		case KindCommand:
			found, err = s.scanCommandRoot(root)
		}
		if err != nil {
			return ScanResult{}, err
		}
		locations = append(locations, found...)
	}

	skills, collisions := Build(locations)
	return ScanResult{Skills: skills, Collisions: collisions}, nil
}

func (s Scanner) scanSkillRoot(root Root) ([]Location, error) {
	entries, err := s.FS.ReadDir(root.Path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read directory %s: %w", root.Path, err)
	}
	slices.SortFunc(entries, func(left, right fs.DirEntry) int {
		return strings.Compare(left.Name(), right.Name())
	})

	locations := make([]Location, 0, len(entries))
	for _, entry := range entries {
		isSymlink := entry.Type()&fs.ModeSymlink != 0
		if !entry.IsDir() && !isSymlink {
			continue
		}

		if root.PluginID == "" && root.Source == SourceClaude {
			manifest := joinPath(root.Path, entry.Name(), ".claude-plugin", "plugin.json")
			if _, err := s.FS.Stat(manifest); err == nil {
				continue
			} else if !errors.Is(err, fs.ErrNotExist) {
				return nil, fmt.Errorf("stat %s: %w", manifest, err)
			}
		}
		discoveryPath := joinPath(root.Path, entry.Name(), "SKILL.md")
		contents, err := s.FS.ReadFile(discoveryPath)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) && !isSymlink {
				_, lstatErr := s.FS.Lstat(discoveryPath)
				if errors.Is(lstatErr, fs.ErrNotExist) {
					continue
				}
				if lstatErr != nil {
					return nil, fmt.Errorf("lstat %s: %w", discoveryPath, lstatErr)
				}
			}
			return nil, fmt.Errorf("read %s: %w", discoveryPath, err)
		}
		realPath, err := s.FS.EvalSymlinks(discoveryPath)
		if err != nil {
			return nil, fmt.Errorf("resolve %s: %w", discoveryPath, err)
		}
		frontmatterName, err := parseFrontmatterName(contents)
		if err != nil {
			return nil, fmt.Errorf("parse frontmatter %s: %w", discoveryPath, err)
		}

		id := scopedName(root.Scope, entry.Name())
		locations = append(locations, Location{
			Kind:            root.Kind,
			DiscoveryPath:   discoveryPath,
			RealPath:        cleanPath(realPath),
			Level:           root.Level,
			Source:          root.Source,
			Scope:           root.Scope,
			PluginID:        root.PluginID,
			PluginAgent:     root.PluginAgent,
			FrontmatterName: frontmatterName,
			Names:           rootNames(root, id),
		})
	}
	return locations, nil
}

func (s Scanner) scanCommandRoot(root Root) ([]Location, error) {
	var locations []Location
	if err := s.walkCommands(root, root.Path, true, &locations); err != nil {
		return nil, err
	}
	return locations, nil
}

func (s Scanner) walkCommands(root Root, directory string, isRoot bool, locations *[]Location) error {
	entries, err := s.FS.ReadDir(directory)
	if err != nil {
		if isRoot && errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read directory %s: %w", directory, err)
	}
	slices.SortFunc(entries, func(left, right fs.DirEntry) int {
		return strings.Compare(left.Name(), right.Name())
	})

	for _, entry := range entries {
		entryPath := joinPath(directory, entry.Name())
		isSymlink := entry.Type()&fs.ModeSymlink != 0
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == "node_modules" {
				continue
			}
			if err := s.walkCommands(root, entryPath, false, locations); err != nil {
				return err
			}
			continue
		}
		if path.Ext(entry.Name()) != ".md" || (!entry.Type().IsRegular() && !isSymlink) {
			continue
		}
		if isSymlink {
			info, err := s.FS.Stat(entryPath)
			if err != nil {
				return fmt.Errorf("stat %s: %w", entryPath, err)
			}
			if info.IsDir() {
				continue
			}
		}
		if _, err := s.FS.ReadFile(entryPath); err != nil {
			return fmt.Errorf("read %s: %w", entryPath, err)
		}
		realPath, err := s.FS.EvalSymlinks(entryPath)
		if err != nil {
			return fmt.Errorf("resolve %s: %w", entryPath, err)
		}

		relative, err := relativePath(root.Path, entryPath)
		if err != nil {
			return fmt.Errorf("resolve command path %s from %s: %w", entryPath, root.Path, err)
		}
		commandName := strings.TrimSuffix(relative, path.Ext(relative))
		id := scopedName(root.Scope, strings.ReplaceAll(commandName, "/", ":"))
		*locations = append(*locations, Location{
			Kind:          root.Kind,
			DiscoveryPath: entryPath,
			RealPath:      cleanPath(realPath),
			Level:         root.Level,
			Source:        root.Source,
			Scope:         root.Scope,
			PluginID:      root.PluginID,
			PluginAgent:   root.PluginAgent,
			Names:         rootNames(root, id),
		})
	}
	return nil
}

func parseFrontmatterName(contents []byte) (string, error) {
	normalized := strings.ReplaceAll(string(contents), "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	if len(lines) == 0 || lines[0] != "---" {
		return "", nil
	}

	closing := -1
	for index := 1; index < len(lines); index++ {
		if lines[index] == "---" {
			closing = index
			break
		}
	}
	if closing < 0 {
		return "", fmt.Errorf("missing closing delimiter")
	}

	frontmatter := []byte(strings.Join(lines[1:closing], "\n"))
	var document yaml.Node
	if err := yaml.Unmarshal(frontmatter, &document); err != nil {
		return "", err
	}
	if len(document.Content) == 0 || document.Content[0].Kind != yaml.MappingNode {
		return "", nil
	}

	var validated map[string]any
	if err := yaml.Unmarshal(frontmatter, &validated); err != nil {
		return "", err
	}

	var metadata struct {
		Name yaml.Node `yaml:"name"`
	}
	if err := document.Content[0].Decode(&metadata); err != nil {
		return "", err
	}
	if metadata.Name.Kind == 0 {
		return "", nil
	}
	if metadata.Name.Kind != yaml.ScalarNode || metadata.Name.Tag != "!!str" {
		return "", fmt.Errorf("name must be a string")
	}
	return metadata.Name.Value, nil
}

func scopedName(scope, name string) string {
	if scope == "" {
		return name
	}
	return scope + ":" + name
}

func rootNames(root Root, name string) map[Agent]string {
	names := make(map[Agent]string, len(root.VisibleTo))
	for _, agent := range root.VisibleTo {
		if root.Kind != KindCommand || agent == AgentClaude {
			names[agent] = scopedName(root.NamePrefix, name)
		}
	}
	return names
}
