package claude_test

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/scarb/skope/internal/agent"
	"github.com/scarb/skope/internal/agent/claude"
	"github.com/scarb/skope/internal/session"
	"github.com/scarb/skope/internal/skill"
)

var updateGolden = flag.Bool("update", false, "update golden files")

func TestPlanWritesStableClaudeSettings(t *testing.T) {
	t.Parallel()

	inv := settingsInventoryFixture()
	resolved := settingsResolvedFixture()
	wantInv := settingsInventoryFixture()
	wantResolved := settingsResolvedFixture()
	sess := &session.Session{Root: "/sessions/example", Agent: skill.AgentClaude}
	settingsPath := sess.AgentPath("settings.json")
	goldenPath := "testdata/settings.golden.json"

	options := claude.Options{Plugins: []string{"allowed@market", "missing@market"}}
	adapter := claude.New(nil, nil, nil, options)
	plan, err := adapter.Plan(resolved, inv, sess)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if len(plan.Files) != 1 {
		t.Fatalf("Plan() files = %#v, want one settings file", plan.Files)
	}
	assertGeneratedSettings(t, plan, map[string]string{"allowed": "on", "blocked": "off", "foreign": "on", "stale": "off"}, map[string]bool{"allowed@market": true, "blocked@market": false, "missing@market": true, "stale@market": false}, true)
	if *updateGolden {
		if err := os.WriteFile(goldenPath, plan.Files[0].Data, 0o644); err != nil {
			t.Fatalf("update golden file: %v", err)
		}
	}
	wantData, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden file: %v", err)
	}
	if !reflect.DeepEqual(plan.Files[0].Data, wantData) {
		t.Fatalf("settings data =\n%s\nwant\n%s", plan.Files[0].Data, wantData)
	}
	if plan.Files[0].Path != settingsPath || plan.Files[0].Mode != 0o600 {
		t.Fatalf("settings file = %#v, want path %q and mode 0600", plan.Files[0], settingsPath)
	}
	wantArgs := []string{"--settings", settingsPath, "--add-dir", sess.AgentPath("addDir")}
	if !reflect.DeepEqual(plan.ControlArgs, wantArgs) {
		t.Fatalf("ControlArgs = %v, want %v", plan.ControlArgs, wantArgs)
	}
	if plan.Env == nil || len(plan.Env) != 0 {
		t.Fatalf("Env = %#v, want allocated empty map", plan.Env)
	}
	if !reflect.DeepEqual(inv, wantInv) || !reflect.DeepEqual(resolved, wantResolved) {
		t.Fatalf("Plan() modified its inputs:\ninventory = %#v\nresolved = %#v", inv, resolved)
	}
	if !reflect.DeepEqual(options.Plugins, []string{"allowed@market", "missing@market"}) || options.Bundled {
		t.Fatalf("Plan() modified options: %#v", options)
	}

	plan.ControlArgs[0] = "changed"
	plan.Env["changed"] = "changed"
	plan.Files[0].Path = "changed"
	plan.Files[0].Data[0] = 'x'
	fresh, err := adapter.Plan(resolved, inv, sess)
	if err != nil {
		t.Fatalf("second Plan() error = %v", err)
	}
	if !reflect.DeepEqual(fresh.ControlArgs, wantArgs) || fresh.Env == nil || len(fresh.Env) != 0 || len(fresh.Files) != 1 {
		t.Fatalf("second Plan() reused mutable output: %#v", fresh)
	}
	if fresh.Files[0].Path != settingsPath || !reflect.DeepEqual(fresh.Files[0].Data, wantData) {
		t.Fatalf("second settings file reused mutable output: %#v", fresh.Files[0])
	}
}

func TestPlanIncludesAllowedNameAbsentFromInventory(t *testing.T) {
	t.Parallel()

	resolved := skill.Resolved{
		Agent: skill.AgentClaude,
		Entries: []skill.Resolution{
			{ID: "external", State: skill.StateNative, Names: []string{"", "external"}},
			{ID: "projected", State: skill.StateProjected, Names: []string{"projected"}},
		},
	}
	inv := agent.Inventory{Skills: []skill.Skill{{
		ID: "irrelevant",
		Locations: []skill.Location{{Names: map[skill.Agent]string{
			skill.AgentClaude: "",
			skill.AgentCodex:  "codex-only",
		}}},
	}}}
	sess := &session.Session{Root: "/sessions/example", Agent: skill.AgentClaude}

	plan, err := (claude.Adapter{}).Plan(resolved, inv, sess)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	assertGeneratedSettings(t, plan, map[string]string{"external": "on", "projected": "on"}, map[string]bool{}, true)
	if want := []string{"--settings", sess.AgentPath("settings.json"), "--add-dir", sess.AgentPath("addDir")}; !reflect.DeepEqual(plan.ControlArgs, want) {
		t.Fatalf("ControlArgs = %v, want %v", plan.ControlArgs, want)
	}
}

func TestPlanWritesEmptyOverrideObjectForEmptyUniverse(t *testing.T) {
	t.Parallel()

	sess := &session.Session{Root: "/sessions/example", Agent: skill.AgentClaude}
	plan, err := (claude.Adapter{}).Plan(skill.Resolved{}, agent.Inventory{}, sess)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	assertGeneratedSettings(t, plan, map[string]string{}, map[string]bool{}, true)
	goldenPath := "testdata/settings-empty.golden.json"
	if *updateGolden {
		if err := os.WriteFile(goldenPath, plan.Files[0].Data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(plan.Files[0].Data, want) {
		t.Fatalf("settings data = %q, want %q", plan.Files[0].Data, want)
	}
	if want := []string{"--settings", sess.AgentPath("settings.json")}; !reflect.DeepEqual(plan.ControlArgs, want) {
		t.Fatalf("ControlArgs = %v, want %v", plan.ControlArgs, want)
	}
}

func settingsInventoryFixture() agent.Inventory {
	return agent.Inventory{
		Skills: []skill.Skill{
			{
				ID:        "allowed",
				Locations: []skill.Location{{Names: map[skill.Agent]string{skill.AgentClaude: "allowed"}}},
			},
			{
				ID: "blocked",
				Locations: []skill.Location{
					{Names: map[skill.Agent]string{skill.AgentClaude: "blocked"}},
				},
			},
		},
		SkillNames: []string{"stale", "", "blocked"},
		PluginIDs:  []string{"allowed@market", "blocked@market", "stale@market"},
		Warnings:   []string{"warning-only"},
	}
}

func settingsResolvedFixture() skill.Resolved {
	return skill.Resolved{
		Agent: skill.AgentClaude,
		Entries: []skill.Resolution{
			{ID: "allowed", State: skill.StateNative, Names: []string{"allowed"}},
			{ID: "foreign", State: skill.StateProjected, Names: []string{"foreign"}},
		},
	}
}

func assertGeneratedSettings(t *testing.T, plan agent.LaunchPlan, overrides map[string]string, plugins map[string]bool, disabled bool) {
	t.Helper()
	if len(plan.Files) != 1 {
		t.Fatalf("Plan() files = %#v, want one settings file", plan.Files)
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(plan.Files[0].Data, &root); err != nil {
		t.Fatal(err)
	}
	if len(root) != 3 {
		t.Fatalf("settings = %s, want exactly skillOverrides, enabledPlugins, disableBundledSkills", plan.Files[0].Data)
	}
	var gotOverrides map[string]string
	var gotPlugins map[string]bool
	var gotDisabled *bool
	for key, destination := range map[string]any{"skillOverrides": &gotOverrides, "enabledPlugins": &gotPlugins, "disableBundledSkills": &gotDisabled} {
		if err := json.Unmarshal(root[key], destination); err != nil {
			t.Fatalf("decode %s: %v", key, err)
		}
	}
	if !reflect.DeepEqual(gotOverrides, overrides) || !reflect.DeepEqual(gotPlugins, plugins) || gotDisabled == nil || *gotDisabled != disabled {
		t.Fatalf("settings = %s, want overrides=%v plugins=%v disabled=%v", plan.Files[0].Data, overrides, plugins, disabled)
	}
}

func TestPlanKeepsPluginNamesSeparateFromOrdinaryNames(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name          string
		plugin        skill.Location
		ordinary      bool
		projected     bool
		historical    bool
		allowedPlugin bool
	}{
		{name: "plugin level", plugin: skill.Location{Level: skill.LevelPlugin}},
		{name: "plugin id", plugin: skill.Location{PluginID: "plugin-only@market"}},
		{name: "plugin agent", plugin: skill.Location{PluginAgent: skill.AgentClaude}},
		{name: "allowed plugin stays separate", plugin: skill.Location{Level: skill.LevelPlugin, PluginID: "plugin-only@market", PluginAgent: skill.AgentClaude}, allowedPlugin: true},
		{name: "historical plugin key stays off", plugin: skill.Location{Level: skill.LevelPlugin}, historical: true},
		{name: "ordinary name also present", plugin: skill.Location{Level: skill.LevelPlugin}, ordinary: true},
		{name: "projected name also present", plugin: skill.Location{Level: skill.LevelPlugin}, projected: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.plugin.Names = map[skill.Agent]string{skill.AgentClaude: "shared"}
			inv := agent.Inventory{Skills: []skill.Skill{{ID: "plugin", Locations: []skill.Location{tc.plugin}}}, PluginIDs: []string{"plugin-only@market"}}
			want := map[string]string{}
			if tc.historical {
				inv.SkillNames = []string{"shared"}
				want["shared"] = "off"
			}
			if tc.ordinary {
				inv.Skills[0].Locations = append(inv.Skills[0].Locations, skill.Location{Names: map[skill.Agent]string{skill.AgentClaude: "shared"}})
				want["shared"] = "on"
			}
			resolved := skill.Resolved{Entries: []skill.Resolution{{State: skill.StateNative, Names: []string{"shared"}}}}
			if tc.projected {
				resolved.Entries = append(resolved.Entries, skill.Resolution{State: skill.StateProjected, Names: []string{"shared"}})
				want["shared"] = "on"
			}
			var opts claude.Options
			if tc.allowedPlugin {
				opts.Plugins = []string{"plugin-only@market"}
			}
			plan, err := claude.New(nil, nil, nil, opts).Plan(resolved, inv, &session.Session{Root: t.TempDir(), Agent: skill.AgentClaude})
			if err != nil {
				t.Fatal(err)
			}
			assertGeneratedSettings(t, plan, want, map[string]bool{"plugin-only@market": tc.allowedPlugin}, true)
		})
	}
}

func TestPlanControlsAllOrdinaryLocationsAndBundledSkills(t *testing.T) {
	t.Parallel()
	inv := agent.Inventory{Skills: []skill.Skill{{ID: "multi-location", Locations: []skill.Location{
		{Names: map[skill.Agent]string{skill.AgentClaude: "beta"}},
		{Names: map[skill.Agent]string{skill.AgentClaude: "legacy"}},
		{Names: map[skill.Agent]string{skill.AgentClaude: "beta"}},
	}}}}
	resolved := skill.Resolved{Entries: []skill.Resolution{{State: skill.StateNative, Names: []string{"beta", "legacy"}}}}
	sess := &session.Session{Root: t.TempDir(), Agent: skill.AgentClaude}
	plan, err := claude.New(nil, nil, nil, claude.Options{Bundled: true}).Plan(resolved, inv, sess)
	if err != nil {
		t.Fatal(err)
	}
	assertGeneratedSettings(t, plan, map[string]string{"beta": "on", "legacy": "on"}, map[string]bool{}, false)
	if want := []string{"--settings", sess.AgentPath("settings.json")}; !reflect.DeepEqual(plan.ControlArgs, want) {
		t.Fatalf("ControlArgs = %v, want %v", plan.ControlArgs, want)
	}
}

func TestPlanUsesFinalSessionPathsWithoutWritingFiles(t *testing.T) {
	t.Parallel()
	home := filepath.Join(t.TempDir(), "not-created")
	sess, err := session.NewManager(home).Preview(skill.AgentClaude, "dev")
	if err != nil {
		t.Fatal(err)
	}
	settingsPath, addDir := sess.AgentPath("settings.json"), sess.AgentPath("addDir")
	sess.Root = filepath.Join(home, ".staging-untrusted")
	plan, err := (claude.Adapter{}).Plan(settingsResolvedFixture(), settingsInventoryFixture(), sess)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"--settings", settingsPath, "--add-dir", addDir}; !reflect.DeepEqual(plan.ControlArgs, want) || plan.Files[0].Path != settingsPath {
		t.Fatalf("Plan() = %#v, want final paths %v", plan, want)
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Fatalf("Plan() created session files: stat error = %v", err)
	}
}
