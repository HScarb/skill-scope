package claude

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

type pluginInstallation struct{ ID, Scope, InstallPath, ProjectPath string }

// IncompletePluginInventoryError means workspace trust hid auto-discovered plugins.
type IncompletePluginInventoryError struct{}

func (*IncompletePluginInventoryError) Error() string {
	return "Claude plugin inventory is incomplete; complete workspace trust in Claude independently, then retry"
}

func parsePlugins(data []byte) ([]pluginInstallation, error) {
	if trimmed := bytes.TrimSpace(data); len(trimmed) == 0 || trimmed[0] != '[' {
		return nil, fmt.Errorf("plugin list root must be an array")
	}
	var rows []json.RawMessage
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, fmt.Errorf("plugin list contains invalid JSON")
	}
	plugins := make([]pluginInstallation, 0, len(rows))
	for index, raw := range rows {
		invalid := func(field string) error { return fmt.Errorf("plugin list item %d field %s is invalid", index, field) }
		var row map[string]json.RawMessage
		if len(raw) == 0 || raw[0] != '{' || json.Unmarshal(raw, &row) != nil {
			return nil, invalid("record")
		}
		var p pluginInstallation
		if !stringField(row, "id", &p.ID) || strings.Count(p.ID, "@") != 1 {
			return nil, invalid("id")
		}
		name, market, _ := strings.Cut(p.ID, "@")
		if strings.TrimSpace(name) == "" || strings.TrimSpace(market) == "" {
			return nil, invalid("id")
		}
		enabled, exists := row["enabled"]
		if !exists || (string(enabled) != "true" && string(enabled) != "false") {
			return nil, invalid("enabled")
		}
		if !stringField(row, "scope", &p.Scope) || (p.Scope != "user" && p.Scope != "project" && p.Scope != "local") {
			return nil, invalid("scope")
		}
		if !stringField(row, "installPath", &p.InstallPath) {
			return nil, invalid("installPath")
		}
		if p.ID == "(suppressed)@skills-dir" && p.Scope == "project" && string(enabled) == "false" && p.InstallPath == "" {
			var version string
			var notes []json.RawMessage
			if stringField(row, "version", &version) && version == "unknown" && len(row["notes"]) > 0 && bytes.TrimSpace(row["notes"])[0] == '[' && json.Unmarshal(row["notes"], &notes) == nil {
				validNotes := true
				for _, note := range notes {
					var text string
					if len(note) == 0 || note[0] != '"' || json.Unmarshal(note, &text) != nil {
						validNotes = false
						break
					}
				}
				if validNotes {
					return nil, &IncompletePluginInventoryError{}
				}
			}
		}
		if !absolutePath(p.InstallPath) {
			return nil, invalid("installPath")
		}
		if raw, ok := row["projectPath"]; ok {
			if json.Unmarshal(raw, &p.ProjectPath) != nil || !absolutePath(p.ProjectPath) {
				return nil, invalid("projectPath")
			}
		} else if p.Scope != "user" && (market != "skills-dir" || p.Scope != "project") {
			return nil, invalid("projectPath")
		}
		plugins = append(plugins, p)
	}
	return plugins, nil
}
func stringField(row map[string]json.RawMessage, key string, out *string) bool {
	raw, ok := row[key]
	return ok && len(bytes.TrimSpace(raw)) > 0 && bytes.TrimSpace(raw)[0] == '"' && json.Unmarshal(raw, out) == nil
}
func absolutePath(path string) bool {
	return strings.TrimSpace(path) != "" && filepath.IsAbs(filepath.FromSlash(path))
}

func applicablePlugin(p pluginInstallation, cwd string) bool {
	if p.Scope == "user" || p.ProjectPath == "" {
		return true
	}
	relative, err := filepath.Rel(filepath.FromSlash(p.ProjectPath), filepath.FromSlash(cwd))
	return err == nil && filepath.IsLocal(relative)
}
