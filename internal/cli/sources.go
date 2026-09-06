package cli

import (
	"context"
	"errors"

	"github.com/scarb/skope/internal/agent/codex"
	"github.com/scarb/skope/internal/host"
	"github.com/scarb/skope/internal/launch"
	"github.com/scarb/skope/internal/skill"
)

type foreignScanner struct {
	scanner      skill.Scanner
	catalog      codex.SourceReader
	resolvePaths func(host.Env) (host.CodexPaths, error)
}

// NewForeignScanner composes only the other agent's sources; construction does no I/O.
func NewForeignScanner(scanner skill.Scanner, catalog codex.SourceReader, resolvePaths func(host.Env) (host.CodexPaths, error)) launch.ForeignScanner {
	return foreignScanner{scanner: scanner, catalog: catalog, resolvePaths: resolvePaths}
}

func (f foreignScanner) ScanForeign(ctx context.Context, env host.Env, target skill.Agent, maxBytes int64) (skill.ScanResult, error) {
	if err := ctx.Err(); err != nil {
		return skill.ScanResult{}, err
	}
	var roots []skill.Root
	var projectRoot string
	var warnings []string
	var err error
	switch target {
	case skill.AgentCodex:
		roots, projectRoot, err = f.scanner.ClaudeRoots(env)
	case skill.AgentClaude:
		paths, pathErr := f.resolvePaths(env)
		if pathErr != nil {
			return skill.ScanResult{}, pathErr
		}
		if err := ctx.Err(); err != nil {
			return skill.ScanResult{}, err
		}
		catalog, catalogErr := f.catalog.Read(ctx, env, paths)
		if catalogErr != nil {
			return skill.ScanResult{}, catalogErr
		}
		if err := ctx.Err(); err != nil {
			return skill.ScanResult{}, err
		}
		roots, projectRoot, err = f.scanner.CodexRoots(env, paths)
		roots = append(roots, catalog.SkillRoots...)
		warnings = append([]string(nil), catalog.Warnings...)
	default:
		return skill.ScanResult{}, errors.New("foreign sources for target agent are unsupported")
	}
	if err != nil {
		return skill.ScanResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return skill.ScanResult{}, err
	}
	result, err := f.scanner.ScanForeignRoots(roots, maxBytes)
	if err != nil {
		return skill.ScanResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return skill.ScanResult{}, err
	}
	result.ProjectRoot = projectRoot
	result.Warnings = warnings
	return result, nil
}
