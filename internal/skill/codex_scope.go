package skill

import (
	"fmt"

	"github.com/scarb/skope/internal/host"
)

func ProjectDirectories(fileSystem FileSystem, cwd string) (string, []string, error) {
	cwd = cleanPath(cwd)
	root, found, err := findGitRoot(fileSystem, cwd)
	if err != nil {
		return "", nil, err
	}
	if !found {
		root = cwd
	}
	return root, projectDirectories(root, cwd), nil
}

func (s Scanner) ClaudeRoots(env host.Env) ([]Root, string, error) {
	return claudeScanRoots(s.FS, env)
}

func (s Scanner) CodexRoots(env host.Env, paths host.CodexPaths) ([]Root, string, error) {
	projectRoot, dirs, err := ProjectDirectories(s.FS, env.Cwd())
	if err != nil {
		return nil, "", err
	}
	roots := []Root{
		{Path: joinPath(paths.Home, ".agents", "skills"), Kind: KindSkill, Level: LevelGlobal, Source: SourceAgents, VisibleTo: []Agent{AgentCodex, AgentOpenCode}, ScanMode: CodexRecursive},
		{Path: joinPath(paths.CodexHome, "skills"), Kind: KindSkill, Level: LevelGlobal, Source: SourceCodex, VisibleTo: []Agent{AgentCodex}, ScanMode: CodexRecursive},
	}
	for _, directory := range dirs {
		scope, err := relativePath(projectRoot, directory)
		if err != nil {
			return nil, "", fmt.Errorf("resolve scope for %s: %w", directory, err)
		}
		roots = append(roots,
			Root{Path: joinPath(directory, ".agents", "skills"), Kind: KindSkill, Level: LevelProject, Source: SourceAgents, Scope: scope, VisibleTo: []Agent{AgentCodex, AgentOpenCode}, ScanMode: CodexRecursive},
			Root{Path: joinPath(directory, ".codex", "skills"), Kind: KindSkill, Level: LevelProject, Source: SourceCodex, Scope: scope, VisibleTo: []Agent{AgentCodex}, ScanMode: CodexRecursive},
		)
	}
	for _, directory := range paths.AdminSkillRoots {
		roots = append(roots, Root{Path: cleanPath(directory), Kind: KindSkill, Level: LevelAdmin, Source: SourceCodex, VisibleTo: []Agent{AgentCodex}, ScanMode: CodexRecursive})
	}
	return uniqueScanRoots(roots), projectRoot, nil
}

func (s Scanner) ScanCodex(env host.Env, paths host.CodexPaths) (ScanResult, error) {
	roots, projectRoot, err := s.CodexRoots(env, paths)
	if err != nil {
		return ScanResult{}, err
	}
	result, err := s.ScanRoots(roots)
	result.ProjectRoot = projectRoot
	return result, err
}
