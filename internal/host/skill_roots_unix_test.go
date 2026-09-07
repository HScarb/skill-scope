//go:build unix

package host_test

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/scarb/skope/internal/host"
)

func TestResolveCodexPathsUnixUsesSnapshotHomeAndFixedSystemRoots(t *testing.T) {
	home := t.TempDir()
	got, err := host.ResolveCodexPaths(host.NewEnv(home, home, map[string]string{"HOME": "ignored", "USERPROFILE": "ignored"}))
	if err != nil || got.Home != home || got.CodexHome != filepath.Join(home, ".codex") || !reflect.DeepEqual(got.AdminSkillRoots, []string{"/etc/codex/skills"}) || !reflect.DeepEqual(got.SystemConfigPaths, []string{"/etc/codex/config.toml"}) {
		t.Fatalf("%+v %v", got, err)
	}
}
