package cli

import (
	"fmt"
	"strings"

	"github.com/scarb/skope/internal/skill"
)

type ConflictError struct {
	Agent        skill.Agent
	Flag, Source string
	ValueSource  string
}

func (e *ConflictError) Error() string {
	name := "Claude"
	if e.Agent == skill.AgentCodex {
		name = "Codex"
	}
	message := fmt.Sprintf("%s isolation conflicts with %s argument %s", name, e.Source, e.Flag)
	if e.ValueSource != "" {
		message += fmt.Sprintf(" (value from %s)", e.ValueSource)
	}
	return message
}

func CheckConflicts(agent skill.Agent, configArgs, userArgs []string) error {
	if agent == skill.AgentCodex {
		return checkCodexConflicts(configArgs, userArgs)
	}
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

// Codex consumes the concatenated argv, including values crossing the two sources.
func checkCodexConflicts(configArgs, userArgs []string) error {
	args := append(append([]string(nil), configArgs...), userArgs...)
	source := func(index int) string {
		if index < len(configArgs) {
			return "config"
		}
		return "command-line"
	}
	for i := 0; i < len(args); i++ {
		flag, value, inline := codexOption(args[i])
		switch flag {
		case "--", "-C", "--cd", "-p", "--profile":
			return &ConflictError{Agent: skill.AgentCodex, Flag: flag, Source: source(i)}
		case "-c", "--config", "--enable", "--disable":
			conflict := &ConflictError{Agent: skill.AgentCodex, Flag: flag, Source: source(i)}
			if !inline && i+1 < len(args) {
				i++
				value = args[i]
				if source(i) != conflict.Source {
					conflict.ValueSource = source(i)
				}
			}
			invalid := func() error {
				// This intentionally contains no user-controlled key or value.
				message := fmt.Sprintf("invalid Codex %s argument %s: missing or malformed value", conflict.Source, flag)
				if conflict.ValueSource != "" {
					message += fmt.Sprintf(" (value from %s)", conflict.ValueSource)
				}
				return fmt.Errorf("%s", message)
			}
			if strings.TrimSpace(value) == "" {
				return invalid()
			}
			if flag == "--enable" || flag == "--disable" {
				if value == "remote_plugin" {
					return conflict
				}
				continue
			}
			key, _, found := strings.Cut(value, "=")
			key = strings.TrimSpace(key)
			if !found || key == "" {
				return invalid()
			}
			parts := strings.Split(key, ".")
			switch parts[0] {
			case "skills", "plugins", "profile", "profiles", "project_root_markers", "marketplaces":
				return conflict
			case "features":
				if len(parts) == 1 || parts[1] == "remote_plugin" {
					return conflict
				}
			}
		case "-m", "--model", "-i", "--image", "-s", "--sandbox", "-a", "--ask-for-approval",
			"--remote", "--remote-auth-token-env", "--local-provider", "--add-dir":
			// A known value token must not be scanned again as an option.
			if !inline && i+1 < len(args) {
				i++
			}
		}
	}
	return nil
}

// Only split actual CLI option spellings; quoted config keys remain literal.
func codexOption(arg string) (flag, value string, inline bool) {
	if strings.HasPrefix(arg, "--") {
		return strings.Cut(arg, "=")
	}
	for _, short := range []string{"-c", "-C", "-p", "-m", "-i", "-s", "-a"} {
		if strings.HasPrefix(arg, short) {
			if arg == short {
				return short, "", false
			}
			return short, strings.TrimPrefix(arg[len(short):], "="), true
		}
	}
	return arg, "", false
}
