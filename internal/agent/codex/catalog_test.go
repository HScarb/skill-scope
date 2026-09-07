package codex_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/scarb/skope/internal/agent/codex"
	"github.com/scarb/skope/internal/host"
	"github.com/scarb/skope/internal/skill"
)

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}
func setup(t *testing.T) (codex.Catalog, host.Env, host.CodexPaths) {
	t.Helper()
	home := t.TempDir()
	cwd := filepath.Join(home, "project")
	if err := os.MkdirAll(cwd, 0700); err != nil {
		t.Fatal(err)
	}
	fs := host.OSFileSystem{}
	return codex.NewCatalog(fs, fs, skill.Scanner{FS: fs, RegularFiles: fs}), host.NewEnv(home, cwd, nil), host.CodexPaths{Home: home, CodexHome: filepath.Join(home, "codex")}
}
func plugin(t *testing.T, p host.CodexPaths, id, version, manifest string) string {
	t.Helper()
	name, market, _ := strings.Cut(id, "@")
	root := filepath.Join(p.CodexHome, "plugins", "cache", market, name, version)
	write(t, filepath.Join(root, ".codex-plugin", "plugin.json"), manifest)
	return root
}
func TestCatalogMissing(t *testing.T) {
	c, e, p := setup(t)
	s, err := c.Read(context.Background(), e, p)
	if err != nil || len(s.PluginIDs) != 0 || len(s.InstalledIDs) != 0 {
		t.Fatalf("%+v %v", s, err)
	}
}
func TestCatalogInstalledAndIndependent(t *testing.T) {
	c, e, p := setup(t)
	write(t, filepath.Join(p.CodexHome, "config.toml"), "[plugins.\"alpha@fixture\"]\nenabled=false\n[plugins.\"missing@fixture\"]\nenabled=true\n")
	plugin(t, p, "alpha@fixture", "1.0.0", `{"name":"old"}`)
	root := plugin(t, p, "alpha@fixture", "2.0.0", `{"name":"active","skills":"custom"}`)
	write(t, filepath.Join(root, "custom", "one", "SKILL.md"), "skill")
	plugin(t, p, "residual@fixture", "1.0.0", `{"name":"residual"}`)
	s, err := c.Read(context.Background(), e, p)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.PluginIDs, []string{"alpha@fixture", "missing@fixture"}) || !reflect.DeepEqual(s.InstalledIDs, []string{"alpha@fixture"}) || len(s.SkillRoots) != 1 || s.SkillRoots[0].NamePrefix != "active" {
		t.Fatalf("%+v", s)
	}
	s.PluginIDs[0] = "changed"
	s.SkillRoots[0].VisibleTo[0] = skill.AgentClaude
	s2, err := c.Read(context.Background(), e, p)
	if err != nil || s2.PluginIDs[0] != "alpha@fixture" || s2.SkillRoots[0].VisibleTo[0] != skill.AgentCodex {
		t.Fatalf("%+v %v", s2, err)
	}
}
func TestCatalogBrokenActiveDoesNotFallback(t *testing.T) {
	for _, manifest := range []string{"", `{"name":3}`, `{"name":"bad","skills":[]}`} {
		t.Run(manifest, func(t *testing.T) {
			c, e, p := setup(t)
			write(t, filepath.Join(p.CodexHome, "config.toml"), "[plugins.\"a@m\"]\nenabled=true")
			plugin(t, p, "a@m", "1.0.0", `{"name":"good"}`)
			plugin(t, p, "a@m", "2.0.0", manifest)
			s, err := c.Read(context.Background(), e, p)
			var inv *codex.InventoryError
			if !errors.As(err, &inv) || len(s.PluginIDs) > 0 {
				t.Fatalf("%+v %v", s, err)
			}
		})
	}
}
func TestCatalogCanceled(t *testing.T) {
	c, e, p := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.Read(ctx, e, p)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestCatalogVerifiedFixtures(t *testing.T) {
	c, e, p := setup(t)
	config, err := os.ReadFile("testdata/catalog/config.toml")
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(p.CodexHome, "config.toml"), string(config))
	err = filepath.WalkDir("testdata/catalog/cache", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel("testdata/catalog/cache", path)
		if err != nil {
			return err
		}
		write(t, filepath.Join(p.CodexHome, "plugins", "cache", rel), string(data))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	s, err := c.Read(context.Background(), e, p)
	if err != nil || !reflect.DeepEqual(s.InstalledIDs, []string{"alpha@fixture", "beta@fixture"}) || len(s.SkillRoots) != 2 {
		t.Fatalf("%+v %v", s, err)
	}
	if !strings.Contains(s.SkillRoots[0].Path, "2.0.0") || filepath.Base(s.SkillRoots[1].Path) != "custom" {
		t.Fatalf("%+v", s.SkillRoots)
	}
}
