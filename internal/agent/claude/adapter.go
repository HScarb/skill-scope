package claude

import (
	"github.com/scarb/skope/internal/agent"
	"github.com/scarb/skope/internal/host"
	"github.com/scarb/skope/internal/skill"
)

type SkillScanner interface {
	ScanClaude(env host.Env) (skill.ScanResult, error)
}

type ReadFileFS interface {
	ReadFile(name string) ([]byte, error)
}

type Adapter struct {
	Scanner SkillScanner
	FS      ReadFileFS
}

func (Adapter) Name() skill.Agent {
	return skill.AgentClaude
}

func (Adapter) Capabilities() agent.Capabilities {
	return agent.Capabilities{Projection: true, TogglePlugins: true, ToggleBundled: true}
}
