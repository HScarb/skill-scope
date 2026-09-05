package cli

import (
	"fmt"
	"strings"

	"github.com/scarb/skope/internal/skill"
)

type ConflictError struct{ Flag, Source string }

func (e *ConflictError) Error() string {
	return fmt.Sprintf("Claude isolation conflicts with %s argument %s", e.Source, e.Flag)
}

func CheckConflicts(agent skill.Agent, configArgs, userArgs []string) error {
	if agent != skill.AgentClaude {
		return nil
	}
	for _, group := range []struct {
		source string
		args   []string
	}{{"config", configArgs}, {"command-line", userArgs}} {
		for _, arg := range group.args {
			flag, _, _ := strings.Cut(arg, "=")
			switch flag {
			case "--settings", "--setting-sources", "--plugin-dir", "--plugin-url", "--add-dir":
				return &ConflictError{Flag: flag, Source: group.source}
			}
		}
	}
	return nil
}
