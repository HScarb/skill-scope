// Package claude provides the Claude Code agent integration.
package claude

import (
	"context"

	"github.com/scarb/skope/internal/agent"
	"github.com/scarb/skope/internal/host"
	"github.com/scarb/skope/internal/proc"
	"github.com/scarb/skope/internal/skill"
)

type SkillScanner interface {
	ScanClaude(env host.Env) (skill.ScanResult, error)
	ScanRoots([]skill.Root) (skill.ScanResult, error)
}

type ReadFileFS interface {
	ReadFile(name string) ([]byte, error)
}

type ProbeRunner interface {
	Run(context.Context, proc.Request) (proc.Result, error)
}

type Options struct {
	Executable string
	Plugins    []string
	Bundled    bool
}

type Adapter struct {
	scanner SkillScanner
	fs      ReadFileFS
	runner  ProbeRunner
	options Options
}

func New(scanner SkillScanner, fsys ReadFileFS, runner ProbeRunner, opts Options) Adapter {
	opts.Plugins = append([]string(nil), opts.Plugins...)
	return Adapter{scanner: scanner, fs: fsys, runner: runner, options: opts}
}

func (Adapter) Name() skill.Agent {
	return skill.AgentClaude
}

func (Adapter) Capabilities() agent.Capabilities {
	return agent.Capabilities{Projection: true, TogglePlugins: true, ToggleBundled: true}
}
