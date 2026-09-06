package skill

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"

	"github.com/scarb/skope/internal/host"
)

type ScanMode uint8

const (
	DirectChildren ScanMode = iota
	CodexRecursive
)

type Root struct {
	ScanMode    ScanMode
	Path        string
	Kind        Kind
	Level       Level
	Source      Source
	Scope       string
	VisibleTo   []Agent
	PluginID    string
	PluginAgent Agent
	NamePrefix  string
}

func foreignGlobalRoots(env host.Env) []Root {
	codexHome := strings.TrimSpace(env.Get("CODEX_HOME"))
	if codexHome == "" {
		codexHome = joinPath(env.Home(), ".codex")
	}
	return []Root{
		{Path: joinPath(env.Home(), ".agents", "skills"), Kind: KindSkill, Level: LevelGlobal, Source: SourceAgents, VisibleTo: []Agent{AgentCodex, AgentOpenCode}, ScanMode: CodexRecursive},
		{Path: joinPath(codexHome, "skills"), Kind: KindSkill, Level: LevelGlobal, Source: SourceCodex, VisibleTo: []Agent{AgentCodex}, ScanMode: CodexRecursive},
	}
}

func claudeScanRoots(fileSystem FileSystem, env host.Env) ([]Root, string, error) {
	projectRoot, directories, err := ProjectDirectories(fileSystem, env.Cwd())
	if err != nil {
		return nil, "", err
	}

	var roots []Root
	for _, directory := range directories {
		scope, err := relativePath(projectRoot, directory)
		if err != nil {
			return nil, "", fmt.Errorf("resolve scope for %s from %s: %w", directory, projectRoot, err)
		}
		roots = append(roots,
			Root{Path: joinPath(directory, ".claude/skills"), Kind: KindSkill, Level: LevelProject, Source: SourceClaude, VisibleTo: []Agent{AgentClaude}, Scope: scope},
			Root{Path: joinPath(directory, ".claude/commands"), Kind: KindCommand, Level: LevelProject, Source: SourceClaude, VisibleTo: []Agent{AgentClaude}, Scope: scope},
		)
	}

	configDirectory := strings.TrimSpace(env.Get("CLAUDE_CONFIG_DIR"))
	if configDirectory == "" {
		configDirectory = joinPath(cleanPath(env.Home()), ".claude")
	} else {
		configDirectory = cleanPath(configDirectory)
	}
	roots = append(roots,
		Root{Path: joinPath(configDirectory, "skills"), Kind: KindSkill, Level: LevelGlobal, Source: SourceClaude, VisibleTo: []Agent{AgentClaude}},
		Root{Path: joinPath(configDirectory, "commands"), Kind: KindCommand, Level: LevelGlobal, Source: SourceClaude, VisibleTo: []Agent{AgentClaude}},
	)

	return uniqueScanRoots(roots), projectRoot, nil
}

func findGitRoot(fileSystem FileSystem, cwd string) (string, bool, error) {
	for directory := cwd; ; directory = parentPath(directory) {
		marker := joinPath(directory, ".git")
		if _, err := fileSystem.Stat(marker); err == nil {
			return directory, true, nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			return "", false, fmt.Errorf("stat %s: %w", marker, err)
		}

		parent := parentPath(directory)
		if parent == directory {
			return "", false, nil
		}
	}
}

func projectDirectories(projectRoot, cwd string) []string {
	directories := []string{cwd}
	for directory := cwd; directory != projectRoot; {
		directory = parentPath(directory)
		directories = append(directories, directory)
	}
	slices.Reverse(directories)
	return directories
}

func uniqueScanRoots(roots []Root) []Root {
	unique := make([]Root, 0, len(roots))
	seen := make(map[string]struct{}, len(roots))
	for _, root := range roots {
		key := string(root.Source) + "\x00" + string(root.Kind) + "\x00" + root.Path
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, root)
	}
	return unique
}

func parentPath(name string) string {
	return filepath.ToSlash(filepath.Dir(filepath.FromSlash(name)))
}

func cleanPath(name string) string {
	return filepath.ToSlash(filepath.Clean(filepath.FromSlash(name)))
}

func joinPath(elements ...string) string {
	native := make([]string, len(elements))
	for index, element := range elements {
		native[index] = filepath.FromSlash(element)
	}
	return filepath.ToSlash(filepath.Join(native...))
}

func relativePath(base, target string) (string, error) {
	relative, err := filepath.Rel(filepath.FromSlash(base), filepath.FromSlash(target))
	if err != nil {
		return "", err
	}
	if relative == "." {
		return "", nil
	}
	return filepath.ToSlash(relative), nil
}
