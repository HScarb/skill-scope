package skill

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type CodexManifest struct {
	Name   string
	Skills string
}

func (s Scanner) readOptionalCodexManifest(directory string) (CodexManifest, error) {
	manifestDirectory := joinPath(directory, ".codex-plugin")
	for _, name := range []string{manifestDirectory, joinPath(manifestDirectory, "plugin.json")} {
		_, err := s.FS.Lstat(name)
		if onlyError(err, fs.ErrNotExist) {
			return CodexManifest{}, nil
		}
		if err != nil {
			return CodexManifest{}, fmt.Errorf("lstat %s: %w", name, err)
		}
		if name == manifestDirectory {
			if _, err := s.FS.Stat(name); err != nil {
				return CodexManifest{}, fmt.Errorf("stat %s: %w", name, err)
			}
		}
	}
	return s.ReadCodexManifest(directory)
}

func (s Scanner) ReadCodexManifest(pluginRoot string) (CodexManifest, error) {
	name := joinPath(pluginRoot, ".codex-plugin", "plugin.json")
	contents, err := s.readSkillFile(name)
	if err != nil {
		return CodexManifest{}, fmt.Errorf("read manifest %s: %w", name, err)
	}
	var raw struct {
		Name   json.RawMessage `json:"name"`
		Skills json.RawMessage `json:"skills"`
	}
	invalid := func() (CodexManifest, error) { return CodexManifest{}, fmt.Errorf("invalid Codex manifest %s", name) }
	if json.Unmarshal(contents, &raw) != nil {
		return invalid()
	}
	manifest := CodexManifest{Skills: "skills/"}
	if json.Unmarshal(raw.Name, &manifest.Name) != nil || strings.TrimSpace(manifest.Name) == "" {
		return invalid()
	}
	if raw.Skills != nil {
		if string(raw.Skills) == "null" || json.Unmarshal(raw.Skills, &manifest.Skills) != nil || strings.TrimSpace(manifest.Skills) == "" {
			return invalid()
		}
	}
	normalized := strings.ReplaceAll(manifest.Skills, "\\", "/")
	cleaned := path.Clean(normalized)
	if path.IsAbs(normalized) || filepath.IsAbs(manifest.Skills) || strings.Contains(normalized, ":") || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return invalid()
	}
	return manifest, nil
}

func codexDescriptionValid(contents []byte) bool {
	lines := strings.Split(strings.ReplaceAll(string(contents), "\r\n", "\n"), "\n")
	if len(lines) == 0 || lines[0] != "---" {
		return false
	}
	for end := 1; end < len(lines); end++ {
		if lines[end] == "---" {
			var metadata struct {
				Description yaml.Node `yaml:"description"`
			}
			if yaml.Unmarshal([]byte(strings.Join(lines[1:end], "\n")), &metadata) != nil {
				return false
			}
			return metadata.Description.Kind == yaml.ScalarNode && metadata.Description.Tag == "!!str" && strings.TrimSpace(metadata.Description.Value) != ""
		}
	}
	return false
}
