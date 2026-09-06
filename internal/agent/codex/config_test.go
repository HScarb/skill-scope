package codex_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scarb/skope/internal/agent/codex"
)

func TestReadCodexConfigRejectsManagedFields(t *testing.T) {
	for index, text := range []string{`profile="SECRET_SENTINEL"`, `project_root_markers=["SECRET_SENTINEL"]`, `plugins="SECRET_SENTINEL"`, `[plugins."SECRET_SENTINEL/x@m"]`, `[plugins."a@m"]
enabled="SECRET_SENTINEL"`, `[skills]
config="SECRET_SENTINEL"`, `[[skills.config]]
path=42`, `[skills.bundled]
enabled="SECRET_SENTINEL"`, `broken="SECRET_SENTINEL`} {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			c, e, p := setup(t)
			write(t, filepath.Join(p.CodexHome, "config.toml"), text)
			_, err := c.Read(context.Background(), e, p)
			var inv *codex.InventoryError
			if !errors.As(err, &inv) || strings.Contains(err.Error(), "SECRET_SENTINEL") {
				t.Fatalf("%v", err)
			}
		})
	}
}
func TestReadCodexConfigUnrelatedAndProjectUnion(t *testing.T) {
	c, e, p := setup(t)
	write(t, filepath.Join(p.CodexHome, "config.toml"), "model='SECRET_SENTINEL'\nproject_root_markers=['.git']\n[mcp_servers.foo]\ncommand='SECRET_SENTINEL'\n[plugins.\"user@m\"]\nenabled=false")
	write(t, filepath.Join(e.Cwd(), ".codex", "config.toml"), "[plugins.\"project@m\"]\nenabled=true")
	s, err := c.Read(context.Background(), e, p)
	if err != nil || len(s.PluginIDs) != 2 || len(s.InstalledIDs) != 0 {
		t.Fatalf("%+v %v", s, err)
	}
}
func TestReadCodexConfigRootMarkersAcrossLayers(t *testing.T) {
	for _, layer := range []string{"system", "user", "project"} {
		for _, markers := range []string{"['.git']", "['custom']", "false", "[]"} {
			t.Run(layer+markers, func(t *testing.T) {
				c, e, p := setup(t)
				path := filepath.Join(p.CodexHome, "config.toml")
				switch layer {
				case "system":
					path = filepath.Join(p.Home, "system.toml")
					p.SystemConfigPaths = []string{path}
				case "project":
					path = filepath.Join(e.Cwd(), ".codex", "config.toml")
				}
				write(t, path, "project_root_markers="+markers)
				_, err := c.Read(context.Background(), e, p)
				if (err == nil) != (markers == "['.git']") {
					t.Fatalf("%v", err)
				}
			})
		}
	}
}
func TestReadCodexConfigValidSkillRulesIgnored(t *testing.T) {
	c, e, p := setup(t)
	write(t, filepath.Join(p.CodexHome, "config.toml"), "[[skills.config]]\npath='SECRET_SENTINEL'\nname='SECRET_SENTINEL'\nenabled=false\n[skills.bundled]\nenabled=true\n[auth]\ntoken='SECRET_SENTINEL'")
	s, err := c.Read(context.Background(), e, p)
	if err != nil || len(s.PluginIDs) != 0 || len(s.SkillRoots) != 0 {
		t.Fatalf("%+v %v", s, err)
	}
}
