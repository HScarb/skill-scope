package skill_test

import (
	"reflect"
	"testing"

	"github.com/scarb/skope/internal/skill"
)

func TestResolveNativeReturnsNativeAndMissingInSelectionOrder(t *testing.T) {
	t.Parallel()

	inventory := []skill.Skill{
		{
			ID: "alpha",
			Locations: []skill.Location{
				{Names: map[skill.Agent]string{skill.AgentClaude: "zeta"}},
				{Names: map[skill.Agent]string{skill.AgentClaude: "alpha"}},
			},
		},
		{
			ID: "beta",
			Locations: []skill.Location{
				{Names: map[skill.Agent]string{skill.AgentClaude: "beta"}},
				{Names: map[skill.Agent]string{skill.AgentClaude: "alpha"}},
			},
		},
	}

	got := skill.ResolveNative(skill.AgentClaude, []string{"alpha", "unknown", "alpha", "beta"}, inventory)
	want := skill.Resolved{
		Agent: skill.AgentClaude,
		Entries: []skill.Resolution{
			{ID: "alpha", State: skill.StateNative, Names: []string{"alpha", "zeta"}},
			{ID: "unknown", State: skill.StateMissing},
			{ID: "beta", State: skill.StateNative, Names: []string{"alpha", "beta"}},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ResolveNative() = %#v, want %#v", got, want)
	}
	for _, entry := range got.Entries {
		if entry.Location != nil || entry.Reason != "" {
			t.Errorf("entry %q Location/Reason = %#v/%q, want nil/empty", entry.ID, entry.Location, entry.Reason)
		}
	}
}

func TestResolveNativeDoesNotMutateOrAliasInput(t *testing.T) {
	t.Parallel()

	selected := []string{"alpha", "unknown"}
	inventory := []skill.Skill{{
		ID: "alpha",
		Locations: []skill.Location{
			{Names: map[skill.Agent]string{skill.AgentClaude: "zeta"}},
			{Names: map[skill.Agent]string{skill.AgentClaude: "alpha"}},
		},
	}}
	originalSelected := append([]string(nil), selected...)
	originalInventory := cloneSkillInventory(inventory)

	resolved := skill.ResolveNative(skill.AgentClaude, selected, inventory)

	if !reflect.DeepEqual(selected, originalSelected) {
		t.Fatalf("ResolveNative() mutated selected: got %v, want %v", selected, originalSelected)
	}
	if !reflect.DeepEqual(inventory, originalInventory) {
		t.Fatalf("ResolveNative() mutated inventory: got %#v, want %#v", inventory, originalInventory)
	}

	inventory[0].Locations[0].Names[skill.AgentClaude] = "input-mutated"
	if got, want := resolved.Entries[0].Names, []string{"alpha", "zeta"}; !reflect.DeepEqual(got, want) {
		t.Errorf("result Names aliases inventory: got %v, want %v", got, want)
	}
	resolved.Entries[0].Names[0] = "output-mutated"
	if got := inventory[0].Locations[1].Names[skill.AgentClaude]; got != "alpha" {
		t.Errorf("inventory Names aliases result: got %q, want alpha", got)
	}

	second := skill.ResolveNative(skill.AgentClaude, selected, originalInventory)
	resolved.Entries[0].ID = "entry-mutated"
	if second.Entries[0].ID != "alpha" {
		t.Errorf("result Entries share storage across calls: got %q, want alpha", second.Entries[0].ID)
	}
}

func TestResolveNativeAllowedNamesAndCount(t *testing.T) {
	t.Parallel()

	resolved := skill.ResolveNative(skill.AgentClaude, []string{"alpha", "unknown", "beta"}, []skill.Skill{
		{ID: "alpha", Locations: []skill.Location{
			{Names: map[skill.Agent]string{skill.AgentClaude: "zeta"}},
			{Names: map[skill.Agent]string{skill.AgentClaude: "alpha"}},
		}},
		{ID: "beta", Locations: []skill.Location{
			{Names: map[skill.Agent]string{skill.AgentClaude: "beta"}},
			{Names: map[skill.Agent]string{skill.AgentClaude: "alpha"}},
		}},
	})

	if got, want := resolved.AllowedNames(), []string{"alpha", "beta", "zeta"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("AllowedNames() = %v, want %v", got, want)
	}
	if got := resolved.Count(skill.StateNative); got != 2 {
		t.Errorf("Count(native) = %d, want 2", got)
	}
	if got := resolved.Count(skill.StateMissing); got != 1 {
		t.Errorf("Count(missing) = %d, want 1", got)
	}
	if got := resolved.Count(skill.StateProjected); got != 0 {
		t.Errorf("Count(projected) = %d, want 0", got)
	}

	names := resolved.AllowedNames()
	names[0] = "mutated"
	if got, want := resolved.AllowedNames(), []string{"alpha", "beta", "zeta"}; !reflect.DeepEqual(got, want) {
		t.Errorf("AllowedNames() result aliases resolution: got %v, want %v", got, want)
	}
}

func cloneSkillInventory(inventory []skill.Skill) []skill.Skill {
	cloned := make([]skill.Skill, len(inventory))
	for i, candidate := range inventory {
		cloned[i].ID = candidate.ID
		cloned[i].Locations = make([]skill.Location, len(candidate.Locations))
		for j, location := range candidate.Locations {
			cloned[i].Locations[j] = location
			if location.Names == nil {
				continue
			}
			cloned[i].Locations[j].Names = make(map[skill.Agent]string, len(location.Names))
			for agent, name := range location.Names {
				cloned[i].Locations[j].Names[agent] = name
			}
		}
	}
	return cloned
}
