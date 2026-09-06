package skill

import (
	"errors"
	"fmt"
	"io"
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
	Rejections  []ScanRejection
}

type RegularFileOpener interface {
	OpenRegular(name string) (fs.File, error)
}

type Scanner struct {
	FS           FileSystem
	RegularFiles RegularFileOpener
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

// visitRoot shares discovery rules between native and bounded foreign scans.
func (s Scanner) visitRoot(root Root, visit func(Root, string, string) error) error {
	if root.Kind == KindCommand {
		return s.visitCommands(root, root.Path, true, visit)
	}
	if root.Kind != KindSkill {
		return nil
	}
	return s.visitSkillDirectory(root, root.Path, true, map[string]bool{}, visit)
}

func (s Scanner) visitSkillDirectory(root Root, directory string, isRoot bool, chain map[string]bool, visit func(Root, string, string) error) error {
	entries, err := s.FS.ReadDir(directory)
	if err != nil {
		if isRoot && errors.Is(err, fs.ErrNotExist) {
			if _, linkErr := s.FS.Lstat(directory); errors.Is(linkErr, fs.ErrNotExist) {
				return nil
			}
		}
		return fmt.Errorf("read directory %s: %w", directory, err)
	}
	if root.ScanMode == CodexRecursive {
		realPath, err := s.FS.EvalSymlinks(directory)
		if err != nil {
			return fmt.Errorf("resolve %s: %w", directory, err)
		}
		realPath = cleanPath(realPath)
		if chain[realPath] {
			return nil
		}
		chain[realPath] = true
		defer delete(chain, realPath)
	}
	if root.ScanMode == CodexRecursive {
		if root.PluginID == "" && root.Source == SourceClaude {
			manifest := joinPath(directory, ".claude-plugin", "plugin.json")
			if _, err := s.FS.Stat(manifest); err == nil {
				return nil
			} else if !errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("stat %s: %w", manifest, err)
			}
		}
		if root.PluginID == "" {
			manifest, err := s.ReadCodexManifest(directory)
			if err == nil {
				root.NamePrefix = manifest.Name
			} else if !errors.Is(err, fs.ErrNotExist) {
				return err
			}
		}
		name := joinPath(directory, "SKILL.md")
		if _, err := s.FS.Lstat(name); err == nil {
			if err := visit(root, name, path.Base(directory)); err != nil {
				return err
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("lstat %s: %w", name, err)
		}
	}
	slices.SortFunc(entries, func(a, b fs.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	for _, entry := range entries {
		if root.ScanMode == CodexRecursive && strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		name := joinPath(directory, entry.Name())
		if root.ScanMode == DirectChildren {
			if !entry.IsDir() && entry.Type()&fs.ModeSymlink == 0 {
				continue
			}
			if root.PluginID == "" && root.Source == SourceClaude {
				manifest := joinPath(name, ".claude-plugin", "plugin.json")
				if _, err := s.FS.Stat(manifest); err == nil {
					continue
				} else if !errors.Is(err, fs.ErrNotExist) {
					return fmt.Errorf("stat %s: %w", manifest, err)
				}
			}
			discovery := joinPath(name, "SKILL.md")
			if _, err := s.FS.Lstat(discovery); err != nil {
				if errors.Is(err, fs.ErrNotExist) && entry.Type()&fs.ModeSymlink == 0 {
					continue
				}
				return fmt.Errorf("lstat %s: %w", discovery, err)
			}
			if err := visit(root, discovery, entry.Name()); err != nil {
				return err
			}
			continue
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			info, err := s.FS.Stat(name)
			if err != nil {
				return fmt.Errorf("stat %s: %w", name, err)
			}
			if !info.IsDir() {
				continue
			}
		} else if !entry.IsDir() {
			continue
		}
		if err := s.visitSkillDirectory(root, name, false, chain, visit); err != nil {
			return err
		}
	}
	return nil
}

func locationFor(root Root, name, realPath string) Location {
	return Location{Kind: root.Kind, DiscoveryPath: name, RealPath: cleanPath(realPath), Level: root.Level, Source: root.Source, Scope: root.Scope, PluginID: root.PluginID, PluginAgent: root.PluginAgent}
}

func (s Scanner) scanSkillRoot(root Root) ([]Location, error) {
	var locations []Location
	err := s.visitRoot(root, func(root Root, name, basename string) error {
		contents, err := s.readSkillFile(name)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		realPath, err := s.FS.EvalSymlinks(name)
		if err != nil {
			return fmt.Errorf("resolve %s: %w", name, err)
		}
		frontmatter, err := parseFrontmatterName(contents)
		if err != nil {
			return fmt.Errorf("invalid skill metadata %s", name)
		}
		if slices.Contains(root.VisibleTo, AgentCodex) && !codexDescriptionValid(contents) {
			return fmt.Errorf("invalid Codex skill metadata %s", name)
		}
		location := locationFor(root, name, realPath)
		location.FrontmatterName = frontmatter
		location.Names = rootNames(root, basename, frontmatter)
		locations = append(locations, location)
		return nil
	})
	return locations, err
}

// Native skill files have no byte limit, but special files must never be read.
// The optional regular opener also prevents a stat-to-open FIFO replacement
// from blocking production scanners.
func (s Scanner) readSkillFile(name string) (contents []byte, err error) {
	info, err := s.FS.Stat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, host.ErrNotRegular
	}
	if s.RegularFiles == nil {
		return s.FS.ReadFile(name)
	}
	file, err := s.RegularFiles.OpenRegular(name)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	return io.ReadAll(file)
}

func (s Scanner) scanCommandRoot(root Root) ([]Location, error) {
	var locations []Location
	err := s.visitRoot(root, func(root Root, name, basename string) error {
		if _, err := s.readSkillFile(name); err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		realPath, err := s.FS.EvalSymlinks(name)
		if err != nil {
			return fmt.Errorf("resolve %s: %w", name, err)
		}
		location := locationFor(root, name, realPath)
		location.Names = rootNames(root, basename, "")
		locations = append(locations, location)
		return nil
	})
	return locations, err
}

func (s Scanner) visitCommands(root Root, directory string, isRoot bool, visit func(Root, string, string) error) error {
	entries, err := s.FS.ReadDir(directory)
	if err != nil {
		if isRoot && errors.Is(err, fs.ErrNotExist) {
			if _, linkErr := s.FS.Lstat(directory); errors.Is(linkErr, fs.ErrNotExist) {
				return nil
			}
		}
		return fmt.Errorf("read directory %s: %w", directory, err)
	}
	slices.SortFunc(entries, func(a, b fs.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	for _, entry := range entries {
		name := joinPath(directory, entry.Name())
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == "node_modules" {
				continue
			}
			if err := s.visitCommands(root, name, false, visit); err != nil {
				return err
			}
			continue
		}
		if path.Ext(entry.Name()) != ".md" {
			continue
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			info, err := s.FS.Stat(name)
			if err != nil {
				return fmt.Errorf("stat %s: %w", name, err)
			}
			if info.IsDir() {
				continue
			}
		}
		relative, err := relativePath(root.Path, name)
		if err != nil {
			return fmt.Errorf("resolve command path %s: %w", name, err)
		}
		basename := strings.ReplaceAll(strings.TrimSuffix(relative, path.Ext(relative)), "/", ":")
		if err := visit(root, name, basename); err != nil {
			return err
		}
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

func rootNames(root Root, basename, frontmatterName string) map[Agent]string {
	names := make(map[Agent]string, len(root.VisibleTo))
	for _, agent := range root.VisibleTo {
		switch agent {
		case AgentClaude:
			names[agent] = scopedName(root.NamePrefix, scopedName(root.Scope, basename))
		case AgentCodex, AgentOpenCode:
			if root.Kind == KindCommand {
				continue
			}
			name := frontmatterName
			if name == "" {
				name = basename
			}
			names[agent] = scopedName(root.NamePrefix, name)
		}
	}
	return names
}
