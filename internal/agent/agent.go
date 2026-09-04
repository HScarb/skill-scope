package agent

import (
	"context"
	"io/fs"

	"github.com/scarb/skope/internal/host"
	"github.com/scarb/skope/internal/session"
	"github.com/scarb/skope/internal/skill"
)

type Adapter interface {
	Name() skill.Agent
	Capabilities() Capabilities
	Inventory(ctx context.Context, env host.Env) (Inventory, error)
	Plan(resolved skill.Resolved, inv Inventory, sess *session.Session) (LaunchPlan, error)
}

type Capabilities struct {
	Projection    bool
	TogglePlugins bool
	ToggleBundled bool
}

type Inventory struct {
	Skills     []skill.Skill
	SkillNames []string
	PluginIDs  []string
	Collisions []skill.Collision
	Warnings   []string
}

type LaunchPlan struct {
	ControlArgs []string
	Env         map[string]string
	Files       []PlannedFile
}

type PlannedFile struct {
	Path string
	Data []byte
	Mode fs.FileMode
}
