package codex_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
	"github.com/scarb/skope/internal/agent"
	"github.com/scarb/skope/internal/agent/codex"
	"github.com/scarb/skope/internal/host"
	"github.com/scarb/skope/internal/skill"
)

type planCanonicalizer func(string) (string, error)

func (f planCanonicalizer) EvalSymlinks(p string) (string, error) { return f(p) }
func identityPaths(p string) (string, error)                      { return p, nil }
func planLocation(root, name, plugin string) skill.Location {
	p := filepath.Join(root, name, "SKILL.md")
	l := inventoryLocation(p, "same-name")
	if plugin != "" {
		l.Level = skill.LevelPlugin
		l.PluginAgent = skill.AgentCodex
		l.PluginID = plugin
	}
	return l
}
func native(ids ...string) skill.Resolved {
	r := skill.Resolved{Agent: skill.AgentCodex}
	for _, id := range ids {
		r.Entries = append(r.Entries, skill.Resolution{ID: id, State: skill.StateNative})
	}
	return r
}

type pathControl struct {
	Path    string `toml:"path"`
	Enabled bool   `toml:"enabled"`
}
type decodedControls struct {
	Skills struct {
		Config  []pathControl `toml:"config"`
		Bundled struct {
			Enabled bool `toml:"enabled"`
		} `toml:"bundled"`
	} `toml:"skills"`
	Plugins map[string]struct {
		Enabled bool `toml:"enabled"`
	} `toml:"plugins"`
	Features struct {
		RemotePlugin bool `toml:"remote_plugin"`
	} `toml:"features"`
}

func decodePlan(t *testing.T, p agent.LaunchPlan) decodedControls {
	t.Helper()
	if len(p.ControlArgs) != 8 || len(p.Env) != 0 || len(p.Files) != 0 {
		t.Fatalf("unexpected plan: %+v", p)
	}
	keys := []string{"skills.config=", "skills.bundled.enabled=", "plugins=", "features.remote_plugin="}
	var assignments []string
	for i, key := range keys {
		if p.ControlArgs[i*2] != "-c" || !strings.HasPrefix(p.ControlArgs[i*2+1], key) {
			t.Fatalf("argument order: %q", p.ControlArgs)
		}
		assignments = append(assignments, p.ControlArgs[i*2+1])
		var value map[string]any
		if err := toml.Unmarshal([]byte(p.ControlArgs[i*2+1]), &value); err != nil {
			t.Fatal(err)
		}
	}
	var d decodedControls
	if err := toml.Unmarshal([]byte(strings.Join(assignments, "\n")), &d); err != nil {
		t.Fatal(err)
	}
	if d.Features.RemotePlugin {
		t.Fatal("remote plugin enabled")
	}
	return d
}
func TestCodexPlanGoldenAndImmutable(t *testing.T) {
	root := t.TempDir()
	inv := agent.Inventory{Skills: []skill.Skill{{ID: "keep", Locations: []skill.Location{planLocation(root, "keep", "keep@market")}}, {ID: "block", Locations: []skill.Location{planLocation(root, "block", "")}}, {ID: "allow", Locations: []skill.Location{planLocation(root, "allow", "")}}}, PluginIDs: []string{"keep@market", "block@market", "block@market"}, Warnings: []string{"preserved alias warning"}}
	r := native("allow")
	opts := codex.Options{Plugins: []string{"missing@market", "keep@market"}}
	before, _ := json.Marshal([]any{inv, r, opts.Plugins})
	a := codex.New(nil, nil, planCanonicalizer(identityPaths), opts)
	p, err := a.Plan(r, inv, nil)
	if err != nil {
		t.Fatal(err)
	}
	d := decodePlan(t, p)
	if d.Skills.Bundled.Enabled || len(d.Skills.Config) != 3 || !d.Skills.Config[0].Enabled || d.Skills.Config[1].Enabled || !d.Skills.Config[2].Enabled {
		t.Fatalf("controls: %+v", d)
	}
	after, _ := json.Marshal([]any{inv, r, opts.Plugins})
	if string(before) != string(after) {
		t.Fatal("input mutated")
	}
	normalized := slices.Clone(p.ControlArgs)
	for i := range normalized {
		normalized[i] = strings.ReplaceAll(normalized[i], filepath.ToSlash(root), "/fixture")
	}
	assertPlanGolden(t, "control-args.golden.json", normalized)
	slices.Reverse(inv.Skills)
	slices.Reverse(inv.PluginIDs)
	p2, err := a.Plan(r, inv, nil)
	if err != nil || !reflect.DeepEqual(p, p2) {
		t.Fatalf("unstable: %+v %v", p2, err)
	}
}
func assertPlanGolden(t *testing.T, name string, args []string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	if err := json.Unmarshal(data, &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("got %q\nwant %q", args, want)
	}
}
func TestCodexPlanEmpty(t *testing.T) {
	p, err := codex.New(nil, nil, nil, codex.Options{}).Plan(skill.Resolved{}, agent.Inventory{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	decodePlan(t, p)
	assertPlanGolden(t, "empty-control-args.golden.json", p.ControlArgs)
}
func TestCodexPlanSelectionAndAliases(t *testing.T) {
	root := t.TempDir()
	ordinary := planLocation(root, "one", "")
	second := planLocation(root, "two", "")
	blocked := planLocation(root, "three", "")
	keep := planLocation(root, "four", "keep@market")
	disabled := planLocation(root, "five", "block@market")
	foreign := planLocation(root, "foreign", "")
	foreign.Names = map[skill.Agent]string{skill.AgentClaude: "foreign"}
	command := planLocation(root, "command", "")
	command.Kind = skill.KindCommand
	foreignPlugin := planLocation(root, "foreign-plugin", "keep@market")
	foreignPlugin.PluginAgent = skill.AgentClaude
	inv := agent.Inventory{Skills: []skill.Skill{{ID: "chosen", Locations: []skill.Location{ordinary, second, disabled, foreign, command, foreignPlugin}}, {ID: "other", Locations: []skill.Location{blocked, keep}}}, PluginIDs: []string{"keep@market", "block@market"}}
	a := codex.New(nil, nil, planCanonicalizer(identityPaths), codex.Options{Plugins: []string{"keep@market"}, Bundled: true})
	r := native("chosen")
	r.Entries = append(r.Entries, skill.Resolution{ID: "other", State: skill.StateUnavailable}, skill.Resolution{ID: "absent", State: skill.StateMissing, Location: &ordinary})
	p, err := a.Plan(r, inv, nil)
	if err != nil {
		t.Fatal(err)
	}
	d := decodePlan(t, p)
	want := map[string]bool{ordinary.RealPath: true, second.RealPath: true, blocked.RealPath: false, keep.RealPath: true, disabled.RealPath: false}
	if len(d.Skills.Config) != len(want) || !d.Skills.Bundled.Enabled {
		t.Fatalf("controls: %+v", d)
	}
	for _, row := range d.Skills.Config {
		v, ok := want[filepath.FromSlash(row.Path)]
		if !ok || v != row.Enabled {
			t.Fatalf("row: %+v", row)
		}
	}
	// Shared ordinary and multiple plugin controls collapse to a single OR result.
	alias := ordinary
	alias.PluginID = "block@market"
	alias.PluginAgent = skill.AgentCodex
	alias.Level = skill.LevelPlugin
	alias2 := alias
	alias2.PluginID = "keep@market"
	inv.Skills = []skill.Skill{{ID: "unselected", Locations: []skill.Location{ordinary, alias, alias2}}}
	p, err = a.Plan(skill.Resolved{}, inv, nil)
	if err != nil {
		t.Fatal(err)
	}
	d = decodePlan(t, p)
	if len(d.Skills.Config) != 1 || !d.Skills.Config[0].Enabled {
		t.Fatalf("alias controls: %+v", d)
	}
	inv.Skills = []skill.Skill{{ID: "chosen", Locations: []skill.Location{ordinary, second}}}
	p, err = a.Plan(native("chosen"), inv, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range decodePlan(t, p).Skills.Config {
		if !row.Enabled {
			t.Fatal("full selection disabled a path")
		}
	}
}
func TestCodexPlanRejectsInvalidPluginIDs(t *testing.T) {
	for _, id := range []string{"bad", "bad\"@market", "bad@market.dot", string([]byte{255})} {
		for _, source := range []string{"options", "inventory", "location"} {
			t.Run(source+id, func(t *testing.T) {
				opts := codex.Options{}
				inv := agent.Inventory{}
				switch source {
				case "options":
					opts.Plugins = []string{id}
				case "inventory":
					inv.PluginIDs = []string{id}
				case "location":
					inv.Skills = []skill.Skill{{Locations: []skill.Location{planLocation(t.TempDir(), "skill", id)}}}
				}
				p, err := codex.New(nil, nil, planCanonicalizer(identityPaths), opts).Plan(skill.Resolved{}, inv, nil)
				if err == nil || !reflect.DeepEqual(p, agent.LaunchPlan{}) {
					t.Fatalf("invalid ID accepted: %+v %v", p, err)
				}
				if strings.Contains(err.Error(), id) {
					t.Fatal("error leaks ID")
				}
			})
		}
	}
}

func TestCodexPlanProductionInventoryExcludesBundled(t *testing.T) {
	catalog, env, paths := setup(t)
	ordinary := filepath.Join(paths.CodexHome, "skills", "ordinary", "SKILL.md")
	write(t, ordinary, "---\nname: ordinary\ndescription: fixture\n---\n")
	write(t, filepath.Join(paths.CodexHome, "skills", ".system", "hidden", "SKILL.md"), "invalid bundled metadata")
	config := filepath.Join(paths.CodexHome, "config.toml")
	write(t, config, "[skills.bundled]\nenabled=true\n")
	fs := host.OSFileSystem{}
	a := codex.New(skill.Scanner{FS: fs, RegularFiles: fs}, catalog, fs, codex.Options{ResolvePaths: func(host.Env) (host.CodexPaths, error) { return paths, nil }})
	inv, err := a.Inventory(context.Background(), env)
	if err != nil {
		t.Fatal(err)
	}
	if len(inv.Skills) != 1 {
		t.Fatalf("bundled leaked into inventory: %+v", inv)
	}
	before, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	p, err := a.Plan(native(inv.Skills[0].ID), inv, nil)
	if err != nil {
		t.Fatal(err)
	}
	d := decodePlan(t, p)
	expected, err := fs.EvalSymlinks(ordinary)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Skills.Config) != 1 || d.Skills.Config[0].Path != expected || !d.Skills.Config[0].Enabled {
		t.Fatalf("production paths: %+v", d)
	}
	after, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("Plan changed config")
	}
}
