package codex

import (
	"slices"

	"github.com/scarb/skope/internal/agent"
	"github.com/scarb/skope/internal/host"
	"github.com/scarb/skope/internal/skill"
)

type SkillScanner interface {
	ScanCodex(host.Env, host.CodexPaths) (skill.ScanResult, error)
	ScanRoots([]skill.Root) (skill.ScanResult, error)
}

type Canonicalizer interface {
	EvalSymlinks(string) (string, error)
}

type Options struct {
	Plugins      []string
	Bundled      bool
	ResolvePaths func(host.Env) (host.CodexPaths, error)
}

type Adapter struct {
	scanner SkillScanner
	sources SourceReader
	paths   Canonicalizer
	options Options
}

func New(scanner SkillScanner, sources SourceReader, paths Canonicalizer, opts Options) Adapter {
	opts.Plugins = slices.Clone(opts.Plugins)
	return Adapter{scanner: scanner, sources: sources, paths: paths, options: opts}
}

func (Adapter) Name() skill.Agent { return skill.AgentCodex }

func (Adapter) Capabilities() agent.Capabilities {
	return agent.Capabilities{TogglePlugins: true, ToggleBundled: true}
}
