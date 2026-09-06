package skill_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/scarb/skope/internal/skill"
)

func TestResolveClassifiesSelectionBeforeProjection(t *testing.T) {
	t.Parallel()
	native := skill.Location{Kind: skill.KindSkill, Names: map[skill.Agent]string{skill.AgentClaude: "zeta"}}
	foreign := skill.Location{Kind: skill.KindSkill, DiscoveryPath: "/foreign/foo/SKILL.md"}
	plugin := skill.Location{Kind: skill.KindSkill, Level: skill.LevelPlugin, PluginID: "p@m", PluginAgent: skill.AgentClaude, Names: map[skill.Agent]string{skill.AgentClaude: "p:foo"}}
	foreignPlugin := plugin
	foreignPlugin.PluginAgent = skill.AgentCodex
	for _, tt := range []struct {
		name      string
		locations []skill.Location
		opts      skill.ResolveOptions
		state     skill.ResolutionState
		names     []string
		reason    skill.ResolutionReason
	}{
		{"native beats earlier foreign", []skill.Location{foreign, native}, skill.ResolveOptions{Projection: true}, skill.StateNative, []string{"zeta"}, ""},
		{"native beats later foreign", []skill.Location{native, foreign}, skill.ResolveOptions{Projection: true}, skill.StateNative, []string{"zeta"}, ""},
		{"allowed target plugin", []skill.Location{plugin}, skill.ResolveOptions{AllowedPlugins: []string{"p@m"}}, skill.StateNative, []string{"p:foo"}, ""},
		{"disabled target plugin", []skill.Location{plugin}, skill.ResolveOptions{Projection: true}, skill.StateUnavailable, nil, skill.ReasonPluginDisabled},
		{"ordinary native survives disabled plugin", []skill.Location{plugin, native}, skill.ResolveOptions{Projection: true}, skill.StateNative, []string{"zeta"}, ""},
		{"native and allowed plugin names sorted and deduplicated", []skill.Location{foreign, native, plugin, native, plugin}, skill.ResolveOptions{Projection: true, AllowedPlugins: []string{"p@m"}}, skill.StateNative, []string{"p:foo", "zeta"}, ""},
		{"foreign plugin not native even if allowed", []skill.Location{foreignPlugin}, skill.ResolveOptions{Projection: true, AllowedPlugins: []string{"p@m"}}, skill.StateUnavailable, nil, skill.ReasonPluginOnly},
		{"command cannot project", []skill.Location{{Kind: skill.KindCommand}}, skill.ResolveOptions{Projection: true}, skill.StateUnavailable, nil, skill.ReasonCommandOnly},
		{"no projection capability", []skill.Location{foreign}, skill.ResolveOptions{}, skill.StateUnavailable, nil, skill.ReasonProjectionUnsupported},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := skill.Resolve(skill.AgentClaude, []string{"foo", "missing", "foo"}, []skill.Skill{{ID: "foo", Locations: tt.locations}}, tt.opts, func(skill.Location) (skill.ResolutionReason, error) {
				t.Fatal("native, missing and ineligible locations must not be inspected")
				return "", nil
			})
			if err != nil {
				t.Fatal(err)
			}
			want := skill.Resolved{Agent: skill.AgentClaude, Entries: []skill.Resolution{{ID: "foo", State: tt.state, Names: tt.names, Reason: tt.reason}, {ID: "missing", State: skill.StateMissing}}}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("Resolve() = %#v, want %#v", got, want)
			}
		})
	}
}

func TestResolveUsesOrderedProjectionCandidates(t *testing.T) {
	t.Parallel()
	first := skill.Location{Kind: skill.KindSkill, DiscoveryPath: "/project/Foo/SKILL.md", Scope: "app", FrontmatterName: "different", Names: map[skill.Agent]string{skill.AgentCodex: "different"}}
	second := first
	second.DiscoveryPath = "/global/Foo/SKILL.md"
	ioErr := errors.New("read failed")
	for _, tt := range []struct {
		name    string
		reasons []skill.ResolutionReason
		err     error
		state   skill.ResolutionState
		reason  skill.ResolutionReason
		calls   int
	}{
		{"first success", []skill.ResolutionReason{""}, nil, skill.StateProjected, "", 1},
		{"rejection then success", []skill.ResolutionReason{skill.ReasonOutsideRoot, ""}, nil, skill.StateProjected, "", 2},
		{"all rejected keep first reason", []skill.ResolutionReason{skill.ReasonOutsideRoot, skill.ReasonSpecialFile}, nil, skill.StateUnavailable, skill.ReasonOutsideRoot, 2},
		{"io error stops", nil, ioErr, "", "", 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			locations := []skill.Location{first, second}
			got, err := skill.Resolve(skill.AgentClaude, []string{"app:Foo"}, []skill.Skill{{ID: "app:Foo", Locations: locations}}, skill.ResolveOptions{Projection: true}, func(loc skill.Location) (skill.ResolutionReason, error) {
				if !reflect.DeepEqual(loc, locations[calls]) {
					t.Fatalf("candidate order = %#v", loc)
				}
				calls++
				if tt.err != nil {
					return "", tt.err
				}
				return tt.reasons[calls-1], nil
			})
			if !errors.Is(err, tt.err) || calls != tt.calls {
				t.Fatalf("err=%v calls=%d", err, calls)
			}
			if err != nil {
				return
			}
			entry := got.Entries[0]
			if entry.State != tt.state || entry.Reason != tt.reason {
				t.Fatalf("entry = %#v", entry)
			}
			if tt.state == skill.StateProjected && (!reflect.DeepEqual(entry.Names, []string{"Foo"}) || !reflect.DeepEqual(entry.Location, &locations[calls-1])) {
				t.Fatalf("projected = %#v", entry)
			}
		})
	}
}

func TestResolveRequiresCheckOnlyForEligibleProjection(t *testing.T) {
	t.Parallel()
	_, err := skill.Resolve(skill.AgentClaude, []string{"foo"}, []skill.Skill{{ID: "foo", Locations: []skill.Location{{Kind: skill.KindSkill, DiscoveryPath: "/foo/SKILL.md"}}}}, skill.ResolveOptions{Projection: true}, nil)
	if err == nil {
		t.Fatal("missing projection check must return a configuration error")
	}
	if _, err := skill.Resolve(skill.AgentClaude, []string{"missing"}, nil, skill.ResolveOptions{Projection: true}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestResolveCopiesProjectedLocationAndNames(t *testing.T) {
	t.Parallel()
	inv := []skill.Skill{{ID: "foo", Locations: []skill.Location{{Kind: skill.KindSkill, DiscoveryPath: "/foreign/foo/SKILL.md", Names: map[skill.Agent]string{skill.AgentCodex: "original"}}}}}
	want := cloneSkillInventory(inv)
	got, err := skill.Resolve(skill.AgentClaude, []string{"foo"}, inv, skill.ResolveOptions{Projection: true}, func(skill.Location) (skill.ResolutionReason, error) { return "", nil })
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(inv, want) {
		t.Fatal("Resolve mutated input")
	}
	if len(got.Entries) != 1 || got.Entries[0].State != skill.StateProjected || got.Entries[0].Location == nil {
		t.Fatalf("expected projected location, got %#v", got)
	}
	inv[0].Locations[0].Names[skill.AgentCodex] = "input changed"
	if got.Entries[0].Location.Names[skill.AgentCodex] != "original" {
		t.Fatal("location aliases input")
	}
	got.Entries[0].Location.Names[skill.AgentClaude] = "location changed"
	if got.Entries[0].Names[0] != "foo" {
		t.Fatal("effective name aliases location")
	}
	got.Entries[0].Names[0] = "result changed"
	if _, ok := inv[0].Locations[0].Names[skill.AgentClaude]; ok {
		t.Fatal("result aliases input")
	}
}

func TestAllowedNamesIncludesNativeAndProjectedOnly(t *testing.T) {
	t.Parallel()
	got := (skill.Resolved{Entries: []skill.Resolution{
		{State: skill.StateNative, Names: []string{"zeta", "alpha"}},
		{State: skill.StateProjected, Names: []string{"beta", "alpha"}},
		{State: skill.StateUnavailable, Names: []string{"unavailable"}},
		{State: skill.StateMissing, Names: []string{"missing"}},
	}}).AllowedNames()
	if !reflect.DeepEqual(got, []string{"alpha", "beta", "zeta"}) {
		t.Fatalf("AllowedNames() = %v", got)
	}
}

func TestResolveUsesTargetProjectionNames(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		target            skill.Agent
		frontmatter, want string
	}{
		{skill.AgentClaude, "declared", "directory"},
		{skill.AgentCodex, "declared", "declared"},
		{skill.AgentOpenCode, "declared", "declared"},
		{skill.AgentOpenCode, "", "directory"},
	} {
		t.Run(string(tt.target)+"/"+tt.frontmatter, func(t *testing.T) {
			inv := []skill.Skill{{ID: "scope:directory", Locations: []skill.Location{{Kind: skill.KindSkill, DiscoveryPath: "/skills/directory/SKILL.md", Scope: "scope", FrontmatterName: tt.frontmatter}}}}
			got, err := skill.Resolve(tt.target, []string{"scope:directory"}, inv, skill.ResolveOptions{Projection: true}, func(skill.Location) (skill.ResolutionReason, error) { return "", nil })
			if err != nil || !reflect.DeepEqual(got.Entries[0].Names, []string{tt.want}) {
				t.Fatalf("resolved=%#v err=%v", got, err)
			}
		})
	}
}

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
