package codex

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/scarb/skope/internal/skill"
)

// canonicalPath checks every controlled path again, including disabled skills.
func (a Adapter) canonicalPath(loc skill.Location) (string, error) {
	if a.paths == nil {
		return "", errors.New("codex plan requires a canonicalizer")
	}
	if !filepath.IsAbs(loc.DiscoveryPath) || !filepath.IsAbs(loc.RealPath) {
		return "", errors.New("codex plan requires absolute discovery and recorded paths")
	}
	canonical, err := a.paths.EvalSymlinks(loc.DiscoveryPath)
	if err != nil {
		return "", fmt.Errorf("canonicalize Codex skill: %w", err)
	}
	if !filepath.IsAbs(canonical) {
		return "", errors.New("codex canonical skill path must be absolute")
	}
	canonical = filepath.ToSlash(filepath.Clean(canonical))
	if canonical != filepath.ToSlash(filepath.Clean(loc.RealPath)) {
		return "", errors.New("codex skill target changed after inventory")
	}
	return canonical, nil
}

// TOML basic strings use Unicode escapes for controls, never Go's \x escapes.
func tomlString(value string) (string, error) {
	if !utf8.ValidString(value) {
		return "", errors.New("TOML string contains invalid UTF-8")
	}
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range value {
		switch r {
		case '"', '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, "\\u%04X", r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String(), nil
}
