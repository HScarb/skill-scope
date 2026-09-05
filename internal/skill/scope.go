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

type scanRoot struct {
	Path   string
	Kind   Kind
	Level  Level
	Source Source
	Scope  string
}

func claudeScanRoots(fileSystem FileSystem, env host.Env) ([]scanRoot, string, error) {
	cwd := cleanPath(env.Cwd())
	projectRoot, found, err := findGitRoot(fileSystem, cwd)
	if err != nil {
		return nil, "", err
	}
	if !found {
		projectRoot = cwd
	}

	var roots []scanRoot
	for _, directory := range projectDirectories(projectRoot, cwd) {
		scope, err := relativePath(projectRoot, directory)
		if err != nil {
			return nil, "", fmt.Errorf("resolve scope for %s from %s: %w", directory, projectRoot, err)
		}
		roots = append(roots,
			scanRoot{Path: joinPath(directory, ".claude/skills"), Kind: KindSkill, Level: LevelProject, Source: SourceClaude, Scope: scope},
			scanRoot{Path: joinPath(directory, ".claude/commands"), Kind: KindCommand, Level: LevelProject, Source: SourceClaude, Scope: scope},
		)
	}

	configDirectory := strings.TrimSpace(env.Get("CLAUDE_CONFIG_DIR"))
	if configDirectory == "" {
		configDirectory = joinPath(cleanPath(env.Home()), ".claude")
	} else {
		configDirectory = cleanPath(configDirectory)
	}
	roots = append(roots,
		scanRoot{Path: joinPath(configDirectory, "skills"), Kind: KindSkill, Level: LevelGlobal, Source: SourceClaude},
		scanRoot{Path: joinPath(configDirectory, "commands"), Kind: KindCommand, Level: LevelGlobal, Source: SourceClaude},
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

func uniqueScanRoots(roots []scanRoot) []scanRoot {
	unique := make([]scanRoot, 0, len(roots))
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
