package claude_test

import (
	"flag"
	"os"
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

	plan, err := (claude.Adapter{}).Plan(resolved, inv, sess)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if len(plan.Files) != 1 {
		t.Fatalf("Plan() files = %#v, want one settings file", plan.Files)
	}
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
	wantArgs := []string{"--settings", settingsPath}
	if !reflect.DeepEqual(plan.ControlArgs, wantArgs) {
		t.Fatalf("ControlArgs = %v, want %v", plan.ControlArgs, wantArgs)
	}
	if plan.Env == nil || len(plan.Env) != 0 {
		t.Fatalf("Env = %#v, want allocated empty map", plan.Env)
	}
	if !reflect.DeepEqual(inv, wantInv) || !reflect.DeepEqual(resolved, wantResolved) {
		t.Fatalf("Plan() modified its inputs:\ninventory = %#v\nresolved = %#v", inv, resolved)
	}

	plan.ControlArgs[0] = "changed"
	plan.Env["changed"] = "changed"
	plan.Files[0].Path = "changed"
	plan.Files[0].Data[0] = 'x'
	fresh, err := (claude.Adapter{}).Plan(resolved, inv, sess)
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
	want := "{\n  \"skillOverrides\": {\n    \"external\": \"on\",\n    \"projected\": \"on\"\n  }\n}\n"
	if got := string(plan.Files[0].Data); got != want {
		t.Fatalf("settings data =\n%s\nwant\n%s", got, want)
	}
}

func TestPlanWritesEmptyOverrideObjectForEmptyUniverse(t *testing.T) {
	t.Parallel()

	sess := &session.Session{Root: "/sessions/example", Agent: skill.AgentClaude}
	plan, err := (claude.Adapter{}).Plan(skill.Resolved{}, agent.Inventory{}, sess)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	want := "{\n  \"skillOverrides\": {}\n}\n"
	if got := string(plan.Files[0].Data); got != want {
		t.Fatalf("settings data = %q, want %q", got, want)
	}
}

func settingsInventoryFixture() agent.Inventory {
	return agent.Inventory{
		Skills: []skill.Skill{
			{
				ID:        "alpha",
				Locations: []skill.Location{{Names: map[skill.Agent]string{skill.AgentClaude: "alpha"}}},
			},
			{
				ID: "multi-location",
				Locations: []skill.Location{
					{Names: map[skill.Agent]string{skill.AgentClaude: "beta"}},
					{Names: map[skill.Agent]string{skill.AgentClaude: "legacy"}},
				},
			},
		},
		SkillNames: []string{"stale", "", "beta"},
		PluginIDs:  []string{"plugin-only"},
		Warnings:   []string{"warning-only"},
	}
}

func settingsResolvedFixture() skill.Resolved {
	return skill.Resolved{
		Agent: skill.AgentClaude,
		Entries: []skill.Resolution{{
			ID:    "selected",
			State: skill.StateNative,
			Names: []string{"beta", "legacy"},
		}},
	}
}
