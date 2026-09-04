package session

import (
	"path/filepath"

	"github.com/scarb/skope/internal/skill"
)

type Session struct {
	Root  string
	Agent skill.Agent
}

func (s *Session) AgentPath(parts ...string) string {
	all := append([]string{s.Root, string(s.Agent)}, parts...)
	return filepath.Join(all...)
}
