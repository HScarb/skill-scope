package host

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

type CodexPaths struct {
	Home              string
	CodexHome         string
	AdminSkillRoots   []string
	SystemConfigPaths []string
}

func ResolveCodexPaths(env Env) (CodexPaths, error) {
	return resolveCodexPaths(env, codexPlatformPaths)
}

func resolveCodexPaths(env Env, lookup func(Env) (string, []string, []string, error)) (CodexPaths, error) {
	home, admins, configs, err := lookup(env)
	if err != nil {
		return CodexPaths{}, fmt.Errorf("resolve Codex home: %w", err)
	}
	if !filepath.IsAbs(home) {
		return CodexPaths{}, fmt.Errorf("codex home must be absolute")
	}
	// Cleaning a parent component can change the directory reached through a symlink.
	if slices.Contains(strings.Split(filepath.ToSlash(home), "/"), "..") {
		return CodexPaths{}, fmt.Errorf("codex home must not contain parent-directory components")
	}
	// Codex receives the original environment, so whitespace is part of the path.
	codexHome := env.Get("CODEX_HOME")
	if codexHome == "" {
		codexHome = filepath.Join(home, ".codex")
	}
	if !filepath.IsAbs(codexHome) {
		return CodexPaths{}, fmt.Errorf("CODEX_HOME must be an absolute directory")
	}
	if slices.Contains(strings.Split(filepath.ToSlash(codexHome), "/"), "..") {
		return CodexPaths{}, fmt.Errorf("CODEX_HOME must not contain parent-directory components")
	}
	return CodexPaths{Home: filepath.Clean(home), CodexHome: filepath.Clean(codexHome), AdminSkillRoots: slices.Clone(admins), SystemConfigPaths: slices.Clone(configs)}, nil
}
