package codex_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/scarb/skope/internal/agent/codex"
)

func TestParseCodexPluginsNoSkillsStillInstalled(t *testing.T) {
	c, e, p := setup(t)
	write(t, filepath.Join(p.CodexHome, "config.toml"), "[plugins.\"a@m\"]")
	plugin(t, p, "a@m", "local", `{"name":"a"}`)
	s, err := c.Read(context.Background(), e, p)
	if err != nil || len(s.InstalledIDs) != 1 || len(s.SkillRoots) != 0 {
		t.Fatalf("%+v %v", s, err)
	}
}
func TestParseCodexPluginsEmptyHighestFails(t *testing.T) {
	c, e, p := setup(t)
	write(t, filepath.Join(p.CodexHome, "config.toml"), "[plugins.\"a@m\"]")
	root := plugin(t, p, "a@m", "1.0.0", `{"name":"a"}`)
	if err := os.Mkdir(filepath.Join(filepath.Dir(root), "2.0.0"), 0700); err != nil {
		t.Fatal(err)
	}
	_, err := c.Read(context.Background(), e, p)
	var inv *codex.InventoryError
	if !errors.As(err, &inv) || !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
}
func TestParseCodexPluginsCandidateOnlyIgnored(t *testing.T) {
	c, e, p := setup(t)
	write(t, filepath.Join(p.CodexHome, "config.toml"), "[marketplaces.fixture]\nsource='SECRET_SENTINEL'\n[profiles.old.plugins.\"old@m\"]\nenabled=true")
	plugin(t, p, "candidate@fixture", "1.0.0", `{"name":"a"}`)
	s, err := c.Read(context.Background(), e, p)
	if err != nil || len(s.PluginIDs) != 0 {
		t.Fatalf("%+v %v", s, err)
	}
}
func TestParseCodexPluginsIgnoresInvalidVersionDirectories(t *testing.T) {
	c, e, p := setup(t)
	write(t, filepath.Join(p.CodexHome, "config.toml"), "[plugins.\"a@m\"]")
	root := plugin(t, p, "a@m", "2.0.0", `{"name":"a"}`)
	write(t, filepath.Join(root, "skills", "one", "SKILL.md"), "skill")
	for _, version := range []string{"zzz space", "中文"} {
		if err := os.Mkdir(filepath.Join(filepath.Dir(root), version), 0700); err != nil {
			t.Fatal(err)
		}
	}
	s, err := c.Read(context.Background(), e, p)
	if err != nil || len(s.SkillRoots) != 1 || !strings.Contains(s.SkillRoots[0].Path, "2.0.0") {
		t.Fatalf("%+v %v", s, err)
	}
}
func TestParseCodexPluginsUsesVersionDirectoryNotMetadata(t *testing.T) {
	c, e, p := setup(t)
	write(t, filepath.Join(p.CodexHome, "config.toml"), "[plugins.\"a@m\"]\n[plugins.\"a@m\".installed]\nlocalVersion='1.0.0'")
	old := plugin(t, p, "a@m", "1.0.0", `{"name":"old","version":"999.0.0"}`)
	future := time.Now().Add(365 * 24 * time.Hour)
	if err := os.Chtimes(old, future, future); err != nil {
		t.Fatal(err)
	}
	active := plugin(t, p, "a@m", "10.0.0", `{"name":"active","version":"0.0.0"}`)
	write(t, filepath.Join(active, "skills", "one", "SKILL.md"), "skill")
	s, err := c.Read(context.Background(), e, p)
	if err != nil || len(s.SkillRoots) != 1 || s.SkillRoots[0].NamePrefix != "active" {
		t.Fatalf("%+v %v", s, err)
	}
}
func TestParseCodexPluginsCycleFailsClosed(t *testing.T) {
	c, e, p := setup(t)
	write(t, filepath.Join(p.CodexHome, "config.toml"), "[plugins.\"a@m\"]")
	for _, version := range []string{"9.0.0", "10.0.0", "2x"} {
		plugin(t, p, "a@m", version, `{"name":"a"}`)
	}
	s, err := c.Read(context.Background(), e, p)
	var inv *codex.InventoryError
	if !errors.As(err, &inv) || inv.Field != "plugins.version" || len(s.PluginIDs) != 0 {
		t.Fatalf("%+v %v", s, err)
	}
	plugin(t, p, "a@m", "local", `{"name":"a"}`)
	s, err = c.Read(context.Background(), e, p)
	if err != nil || len(s.InstalledIDs) != 1 {
		t.Fatalf("%+v %v", s, err)
	}
}
func TestParseCodexPluginsValidatesCodexIDGrammar(t *testing.T) {
	for index, id := range []string{"a b@m", "a@market.dot", "a..b@m", ".a@m", "a.@m", "中文@m", "a@m@extra"} {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			c, e, p := setup(t)
			write(t, filepath.Join(p.CodexHome, "config.toml"), "[plugins.\""+id+"\"]")
			_, err := c.Read(context.Background(), e, p)
			var inv *codex.InventoryError
			if !errors.As(err, &inv) || inv.Field != "plugins.id" {
				t.Fatalf("%v", err)
			}
		})
	}
	c, e, p := setup(t)
	write(t, filepath.Join(p.CodexHome, "config.toml"), "[plugins.\"a.b-C_1@market-2_3\"]")
	s, err := c.Read(context.Background(), e, p)
	if err != nil || len(s.PluginIDs) != 1 {
		t.Fatalf("%+v %v", s, err)
	}
}
