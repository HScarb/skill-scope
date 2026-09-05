package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/scarb/skope/internal/host"
	"github.com/scarb/skope/internal/skill"
)

func (a Adapter) pluginRoots(ctx context.Context, env host.Env, installations []pluginInstallation) ([]skill.Root, error) {
	roots := make([]skill.Root, 0)
	seen := make(map[string]bool)
	for _, p := range installations {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if seen[p.ID] || !applicablePlugin(p, env.Cwd()) {
			continue
		}
		seen[p.ID] = true
		root, err := a.pluginRoot(env, p)
		if err != nil {
			return nil, err
		}
		manifestPath := joinPath(root, ".claude-plugin", "plugin.json")
		var manifest map[string]json.RawMessage
		if err := a.readPluginJSON(manifestPath, &manifest); err != nil {
			return nil, err
		}
		var name string
		if !stringField(manifest, "name", &name) || strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("plugin inventory %s: field name is invalid", manifestPath)
		}
		roots = append(roots, skill.Root{Path: joinPath(root, "skills"), Kind: skill.KindSkill, Level: skill.LevelPlugin, Source: skill.SourceClaude, VisibleTo: []skill.Agent{skill.AgentClaude}, PluginID: p.ID, PluginAgent: skill.AgentClaude, NamePrefix: name})
	}
	return roots, nil
}

func (a Adapter) pluginRoot(env host.Env, p pluginInstallation) (string, error) {
	name, market, _ := strings.Cut(p.ID, "@")
	if market == "skills-dir" {
		return p.InstallPath, nil
	}
	configDir := strings.TrimSpace(env.Get("CLAUDE_CONFIG_DIR"))
	if configDir == "" {
		configDir = joinPath(env.Home(), ".claude")
	}
	knownPath := joinPath(configDir, "plugins", "known_marketplaces.json")
	var known map[string]json.RawMessage
	if err := a.readPluginJSON(knownPath, &known); err != nil {
		return "", err
	}
	var entry struct {
		Source struct {
			Source string `json:"source"`
		} `json:"source"`
		InstallLocation string `json:"installLocation"`
	}
	if raw, ok := known[market]; !ok || json.Unmarshal(raw, &entry) != nil || !absolutePath(entry.InstallLocation) {
		return "", fmt.Errorf("plugin inventory %s: marketplace %s source/installLocation is invalid", knownPath, market)
	}
	switch entry.Source.Source {
	case "git", "github", "url":
		return p.InstallPath, nil
	case "directory":
		catalogPath := joinPath(entry.InstallLocation, ".claude-plugin", "marketplace.json")
		var catalog struct {
			Plugins []map[string]json.RawMessage `json:"plugins"`
		}
		if err := a.readPluginJSON(catalogPath, &catalog); err != nil {
			return "", err
		}
		for _, candidate := range catalog.Plugins {
			var candidateName string
			if !stringField(candidate, "name", &candidateName) || candidateName != name {
				continue
			}
			var source string
			if !stringField(candidate, "source", &source) || !localPluginSource(source) {
				return "", fmt.Errorf("plugin inventory %s: plugin %s source must be a local relative path", catalogPath, name)
			}
			return joinPath(entry.InstallLocation, source), nil
		}
		return "", fmt.Errorf("plugin inventory %s: plugin %s is missing", catalogPath, name)
	default:
		return "", fmt.Errorf("plugin inventory %s: marketplace %s source is unknown", knownPath, market)
	}
}

func localPluginSource(source string) bool {
	if !filepath.IsLocal(filepath.FromSlash(source)) {
		return false
	}
	for _, component := range strings.FieldsFunc(source, func(r rune) bool { return r == '/' || r == '\\' }) {
		if component == ".." {
			return false
		}
	}
	return true
}
func (a Adapter) readPluginJSON(path string, target any) error {
	data, err := a.fs.ReadFile(path)
	if err != nil {
		return fmt.Errorf("plugin inventory read %s: %w", path, err)
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' || json.Unmarshal(data, target) != nil {
		return fmt.Errorf("plugin inventory %s: invalid JSON object", path)
	}
	return nil
}
