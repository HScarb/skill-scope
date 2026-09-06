package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/scarb/skope/internal/host"
	"github.com/scarb/skope/internal/launch"
	"github.com/scarb/skope/internal/skill"
	"github.com/scarb/skope/internal/testutil"
)

func TestProductionLaunchProjectsDiscoveryLinkWithFakeProbe(t *testing.T) {
	if testing.Short() {
		t.Skip("builds fake probe")
	}
	root := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	link := func(target, name string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
			t.Fatal(err)
		}
		if runtime.GOOS == "windows" {
			if out, err := exec.Command("cmd", "/c", "mklink", "/J", name, target).CombinedOutput(); err != nil {
				t.Fatalf("junction %v: %s", err, out)
			}
		} else if err := os.Symlink(target, name); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(root, "foreign-target", "SKILL.md"), "---\nname: display-other\n---\nforeign body")
	write(filepath.Join(root, "foreign-target", "refs", "note.txt"), "reference body")
	write(filepath.Join(root, "native-target", "SKILL.md"), "native body")
	link(filepath.Join(root, "foreign-target"), filepath.Join(root, "home", ".agents", "skills", "foreign"))
	link(filepath.Join(root, "native-target"), filepath.Join(root, "claude", "skills", "native"))
	write(filepath.Join(root, "repo", ".git", "fixture"), "")
	write(filepath.Join(root, "skope", "config.toml"), "version=1\n[agents.claude]\ncommand="+strconv.Quote(testutil.BuildFakeAgent(t))+"\n")
	write(filepath.Join(root, "skope", "skillsets.toml"), "version=1\n[skillsets.dev]\nskills=['foreign','native']\nbundled=true\n[skillsets.dev.plugins]\nclaude=['missing@m','also-missing@m']\n")
	write(filepath.Join(root, "claude", "settings.json"), `{"enabledPlugins":{"stale@m":true,"disabled@m":false}}`)
	env := host.NewEnv(filepath.Join(root, "home"), filepath.Join(root, "repo"), map[string]string{"SKOPE_HOME": filepath.Join(root, "skope"), "CLAUDE_CONFIG_DIR": filepath.Join(root, "claude"), "CODEX_HOME": filepath.Join(root, "codex"), "FAKEAGENT_PLUGIN_JSON": "[]", "FAKEAGENT_PROBE_LOG": filepath.Join(root, "probe.log")})
	d := dependencies{snapshot: func() (host.Env, error) { return env, nil }}
	called := false
	err := d.runLaunch(context.Background(), launch.Request{Agent: skill.AgentClaude, SetValue: "dev", SetPresent: true, DryRun: true}, func(r launch.Result) error {
		called = true
		if r.Resolved.Entries[0].State != skill.StateProjected || r.Resolved.Entries[1].State != skill.StateNative || r.Resolved.Entries[0].Names[0] != "foreign" {
			t.Fatalf("resolved=%+v", r.Resolved)
		}
		actual, err := os.Stat(filepath.FromSlash(r.Resolved.Entries[0].Location.RealPath))
		if err != nil {
			t.Fatal(err)
		}
		want, err := os.Stat(filepath.Join(root, "foreign-target", "SKILL.md"))
		if err != nil || !os.SameFile(actual, want) {
			t.Fatalf("realpath=%s err=%v", r.Resolved.Entries[0].Location.RealPath, err)
		}
		if len(r.ProjectionFiles) != 2 || r.Plugins != (launch.PluginSummary{Allowed: 2, Disabled: 2}) || !r.Bundled {
			t.Fatalf("metadata=%+v", r)
		}
		var settings struct {
			EnabledPlugins map[string]bool `json:"enabledPlugins"`
		}
		if err := json.Unmarshal(r.Plan.Files[0].Data, &settings); err != nil {
			t.Fatal(err)
		}
		var counts launch.PluginSummary
		for _, enabled := range settings.EnabledPlugins {
			if enabled {
				counts.Allowed++
			} else {
				counts.Disabled++
			}
		}
		if counts != r.Plugins {
			t.Fatalf("counts=%v summary=%v", counts, r.Plugins)
		}
		for _, file := range r.ProjectionFiles {
			if !filepath.IsAbs(file.Path) || strings.Contains(file.Path, ".staging-") {
				t.Fatalf("path=%s", file.Path)
			}
			_, err := os.Stat(file.Path)
			if !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("dry wrote path=%s err=%v", file.Path, err)
			}
		}
		return nil
	})
	if !called {
		t.Fatalf("report not reached: %v", err)
	}
	if err != nil {
		t.Fatal(err)
	}
	entries, e := os.ReadDir(filepath.Join(root, "skope", "sessions"))
	if e != nil && !errors.Is(e, os.ErrNotExist) {
		t.Fatal(e)
	}
	if len(entries) != 0 {
		t.Fatalf("sessions=%v", entries)
	}
}
