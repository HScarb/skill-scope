package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

type AgentConfig struct {
	Command string
	Args    []string
}

type Config struct {
	Exists bool
	Agents map[string]AgentConfig
}

type ReadFileFS interface {
	ReadFile(name string) ([]byte, error)
}

type configFile struct {
	Version *int       `toml:"version"`
	Agents  agentsFile `toml:"agents"`
}

type agentsFile struct {
	Claude   *agentFile `toml:"claude"`
	Codex    *agentFile `toml:"codex"`
	OpenCode *agentFile `toml:"opencode"`
}

type agentFile struct {
	Command string   `toml:"command"`
	Args    []string `toml:"args"`
}

// Load reads and strictly decodes a config file.
func Load(fsys ReadFileFS, path string) (Config, error) {
	data, err := fsys.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Config{Agents: make(map[string]AgentConfig)}, nil
	}
	if err != nil {
		return Config{}, &PathError{Path: path, Err: err}
	}

	var raw configFile
	err = toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields().Decode(&raw)
	if err != nil {
		pathErr := &PathError{Path: path, Err: err}
		var decodeErr *toml.DecodeError
		if errors.As(err, &decodeErr) {
			pathErr.Field = strings.Join(decodeErr.Key(), ".")
		}
		return Config{}, pathErr
	}

	if raw.Version == nil {
		return Config{}, &PathError{
			Path:  path,
			Field: "version",
			Err:   errors.New("is required"),
		}
	}
	if *raw.Version != 1 {
		return Config{}, &PathError{
			Path:  path,
			Field: "version",
			Err:   fmt.Errorf("unsupported version %d", *raw.Version),
		}
	}

	agents := make(map[string]AgentConfig, 3)
	addAgent(agents, "claude", raw.Agents.Claude)
	addAgent(agents, "codex", raw.Agents.Codex)
	addAgent(agents, "opencode", raw.Agents.OpenCode)

	return Config{Exists: true, Agents: agents}, nil
}

func addAgent(agents map[string]AgentConfig, name string, raw *agentFile) {
	if raw == nil {
		return
	}
	agents[name] = AgentConfig{
		Command: raw.Command,
		Args:    append([]string{}, raw.Args...),
	}
}
