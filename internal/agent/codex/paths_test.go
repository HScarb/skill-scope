package codex_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
	"github.com/scarb/skope/internal/agent"
	"github.com/scarb/skope/internal/agent/codex"
	"github.com/scarb/skope/internal/host"
	"github.com/scarb/skope/internal/skill"
)

func TestCanonicalPlanRejectsUntrustedPaths(t *testing.T) {
	root := t.TempDir()
	valid := planLocation(root, "valid", "")
	cases := []struct {
		name, discovery, real, result string
		fail                          bool
	}{
		{"missing discovery", "", valid.RealPath, valid.RealPath, false},
		{"relative discovery", "relative", valid.RealPath, valid.RealPath, false},
		{"missing recorded", valid.DiscoveryPath, "", valid.RealPath, false},
		{"relative recorded", valid.DiscoveryPath, "relative", valid.RealPath, false},
		{"missing result", valid.DiscoveryPath, valid.RealPath, "", false},
		{"relative result", valid.DiscoveryPath, valid.RealPath, "relative", false},
		{"retarget", valid.DiscoveryPath, valid.RealPath, filepath.Join(root, "changed"), false},
		{"case changed", valid.DiscoveryPath, valid.RealPath, strings.Replace(valid.RealPath, "valid", "VALID", 1), false},
		{"resolution failure", valid.DiscoveryPath, valid.RealPath, "", true},
		{"invalid UTF8", valid.DiscoveryPath + string([]byte{255}), valid.RealPath + string([]byte{255}), valid.RealPath + string([]byte{255}), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			loc := valid
			loc.DiscoveryPath = tc.discovery
			loc.RealPath = tc.real
			inv := agent.Inventory{Skills: []skill.Skill{{ID: "id", Locations: []skill.Location{loc}}}}
			for _, selected := range []bool{false, true} {
				r := skill.Resolved{}
				if selected {
					r = native("id")
				}
				a := codex.New(nil, nil, planCanonicalizer(func(string) (string, error) {
					if tc.fail {
						return "", errors.New("lookup failed")
					}
					return tc.result, nil
				}), codex.Options{})
				p, err := a.Plan(r, inv, nil)
				if err == nil || !reflect.DeepEqual(p, agent.LaunchPlan{}) {
					t.Fatalf("unsafe path accepted: %+v %v", p, err)
				}
			}
		})
	}
	inv := agent.Inventory{Skills: []skill.Skill{{Locations: []skill.Location{valid}}}}
	if _, err := codex.New(nil, nil, nil, codex.Options{}).Plan(skill.Resolved{}, inv, nil); err == nil {
		t.Fatal("nil canonicalizer accepted")
	}
}
func TestCanonicalPlanRealAliasesAndRetarget(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"one", "two"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name, "SKILL.md"), []byte("fixture"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(root, "alias")
	makeLink := func(target string) {
		t.Helper()
		if runtime.GOOS == "windows" {
			if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
				t.Fatalf("junction: %v %s", err, out)
			}
		} else if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
	}
	makeLink(filepath.Join(root, "one"))
	fs := host.OSFileSystem{}
	original := planLocation(root, "one", "")
	var err error
	original.RealPath, err = fs.EvalSymlinks(original.DiscoveryPath)
	if err != nil {
		t.Fatal(err)
	}
	alias := original
	alias.DiscoveryPath = filepath.Join(link, "SKILL.md")
	inv := agent.Inventory{Skills: []skill.Skill{{ID: "allowed", Locations: []skill.Location{original}}, {ID: "blocked", Locations: []skill.Location{alias}}}}
	a := codex.New(nil, nil, fs, codex.Options{})
	p, err := a.Plan(native("allowed"), inv, nil)
	if err != nil {
		t.Fatal(err)
	}
	rows := decodePlan(t, p).Skills.Config
	if len(rows) != 1 || !rows[0].Enabled || rows[0].Path != original.RealPath {
		t.Fatalf("alias rows: %+v", rows)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	makeLink(filepath.Join(root, "two"))
	if _, err := a.Plan(native("allowed"), inv, nil); err == nil {
		t.Fatal("retargeted disabled path accepted")
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Plan(native("allowed"), inv, nil); err == nil {
		t.Fatal("removed path accepted")
	}
}
func TestCodexPlanTOMLPathRoundTrip(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"quote\"", "back\\slash", "中文 space", "tab\tline\n", "emoji😀", "controls\a\v\x7f"} {
		t.Run(name, func(t *testing.T) {
			loc := planLocation(root, name, "")
			p, err := codex.New(nil, nil, planCanonicalizer(identityPaths), codex.Options{Plugins: []string{"plugin.dot@market"}}).Plan(native("id"), agent.Inventory{Skills: []skill.Skill{{ID: "id", Locations: []skill.Location{loc}}}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			d := decodePlan(t, p)
			if len(d.Skills.Config) != 1 || d.Skills.Config[0].Path != filepath.ToSlash(filepath.Clean(loc.RealPath)) || !d.Plugins["plugin.dot@market"].Enabled {
				t.Fatalf("round trip: %+v", d)
			}
		})
	}
}
func TestCodexPlanTOMLStringRoundTrip(t *testing.T) {
	for _, value := range []string{"", "quote\"slash\\", "中文 😀\t\n\r", "\x00\a\b\v\f\x1f\x7f", "a.b@c"} {
		encoded, err := codex.TOMLStringForTest(value)
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]string
		if err := toml.Unmarshal([]byte(encoded+"="+encoded), &got); err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[value] != value {
			t.Fatalf("roundtrip %q: %v", value, got)
		}
	}
	if _, err := codex.TOMLStringForTest(string([]byte{255})); err == nil {
		t.Fatal("invalid UTF8 accepted")
	}
}
