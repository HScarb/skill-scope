package session

import (
	"time"

	"github.com/scarb/skope/internal/skill"
)

type Owner struct {
	PID          int         `json:"pid"`
	ProcessStart string      `json:"processStart"`
	Agent        skill.Agent `json:"agent"`
	SkillSet     string      `json:"skillSet"`
	CreatedAt    time.Time   `json:"createdAt"`
}
