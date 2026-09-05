package config

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/scarb/skope/internal/host"
)

// ResolveHome returns the absolute skope home path from the environment.
func ResolveHome(env host.Env) (string, error) {
	value := strings.TrimSpace(env.Get("SKOPE_HOME"))
	switch {
	case value == "":
		value = filepath.Join(env.Home(), ".skope")
	case value == "~":
		value = env.Home()
	case strings.HasPrefix(value, "~/") || strings.HasPrefix(value, `~\`):
		value = filepath.Join(env.Home(), value[2:])
	}

	value = filepath.Clean(value)
	if !filepath.IsAbs(value) {
		return "", &PathError{
			Path:  value,
			Field: "SKOPE_HOME",
			Err:   errors.New("must be an absolute path"),
		}
	}
	return value, nil
}
