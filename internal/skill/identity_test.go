package skill_test

import (
	"reflect"
	"slices"
	"testing"

	"github.com/scarb/skope/internal/skill"
)

func TestBuildMergesSkillAndCommandWithSameID(t *testing.T) {
	t.Parallel()

	locations := []skill.Location{
		{
			Kind:          skill.KindSkill,
			DiscoveryPath: "/repo/git/.claude/skills/commit/SKILL.md",
			RealPath:      "/shared/commit",
			Level:         skill.LevelProject,
			Source:        skill.SourceClaude,
			Scope:         "git",
		},
		{
			Kind:          skill.KindCommand,
			DiscoveryPath: "/repo/.claude/commands/git/commit.md",
			RealPath:      "/shared/commit",
			Level:         skill.LevelProject,
			Source:        skill.SourceClaude,
		},
	}

	skills, _ := skill.Build(locations)

	if len(skills) != 1 {
		t.Fatalf("Build() returned %d skills, want 1", len(skills))
	}
	if skills[0].ID != "git:commit" {
		t.Fatalf("Build() skill ID = %q, want %q", skills[0].ID, "git:commit")
	}
	if len(skills[0].Locations) != 2 {
		t.Fatalf("Build() returned %d locations, want 2", len(skills[0].Locations))
	}
	kinds := []skill.Kind{skills[0].Locations[0].Kind, skills[0].Locations[1].Kind}
	slices.Sort(kinds)
	if want := []skill.Kind{skill.KindCommand, skill.KindSkill}; !reflect.DeepEqual(kinds, want) {
		t.Errorf("Build() location kinds = %v, want %v", kinds, want)
	}
}

func TestBuildDeduplicatesOnlySourceAndDiscoveryPath(t *testing.T) {
	t.Parallel()

	locations := []skill.Location{
		skillLocation("/one/shared/SKILL.md", "/real/shared", skill.LevelGlobal, skill.SourceClaude),
		skillLocation("/one/shared/SKILL.md", "/other/target", skill.LevelProject, skill.SourceClaude),
		skillLocation("/two/shared/SKILL.md", "/real/shared", skill.LevelGlobal, skill.SourceClaude),
		skillLocation("/one/shared/SKILL.md", "/real/shared", skill.LevelGlobal, skill.SourceAgents),
	}

	skills, _ := skill.Build(locations)

	if len(skills) != 1 {
		t.Fatalf("Build() returned %d skills, want 1", len(skills))
	}
	if got := len(skills[0].Locations); got != 3 {
		t.Fatalf("Build() returned %d unique locations, want 3", got)
	}
	want := map[string]bool{
		"claude\x00/one/shared/SKILL.md": true,
		"claude\x00/two/shared/SKILL.md": true,
		"agents\x00/one/shared/SKILL.md": true,
	}
	for _, location := range skills[0].Locations {
		key := string(location.Source) + "\x00" + location.DiscoveryPath
		if !want[key] {
			t.Errorf("Build() retained unexpected location %q", key)
		}
		delete(want, key)
	}
	if len(want) != 0 {
		t.Errorf("Build() omitted locations %v", want)
	}
}

func TestBuildSortsSkillsAndLocationsByPriority(t *testing.T) {
	t.Parallel()

	locations := []skill.Location{
		skillLocation("/z/SKILL.md", "/z", skill.LevelGlobal, skill.SourceClaude),
		skillLocation("/b/shared/SKILL.md", "/b", skill.LevelGlobal, skill.SourceClaude),
		skillLocation("/plugin/shared/SKILL.md", "/plugin", skill.LevelPlugin, skill.SourceClaude),
		skillLocation("/opencode-paths/shared/SKILL.md", "/opencode-paths", skill.LevelGlobal, skill.SourceOpenCodePath),
		skillLocation("/admin/shared/SKILL.md", "/admin", skill.LevelAdmin, skill.SourceClaude),
		skillLocation("/opencode/shared/SKILL.md", "/opencode", skill.LevelGlobal, skill.SourceOpenCode),
		skillLocation("/codex/shared/SKILL.md", "/codex", skill.LevelGlobal, skill.SourceCodex),
		skillLocation("/agents/shared/SKILL.md", "/agents", skill.LevelGlobal, skill.SourceAgents),
		skillLocation("/a/shared/SKILL.md", "/a", skill.LevelGlobal, skill.SourceClaude),
		skillLocation("/project/shared/SKILL.md", "/project", skill.LevelProject, skill.SourceOpenCodePath),
	}

	skills, _ := skill.Build(locations)

	if got, want := skillIDs(skills), []string{"shared", "z"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Build() skill IDs = %v, want %v", got, want)
	}
	got := locationPaths(skills[0].Locations)
	want := []string{
		"/project/shared/SKILL.md",
		"/a/shared/SKILL.md",
		"/b/shared/SKILL.md",
		"/agents/shared/SKILL.md",
		"/codex/shared/SKILL.md",
		"/opencode/shared/SKILL.md",
		"/opencode-paths/shared/SKILL.md",
		"/admin/shared/SKILL.md",
		"/plugin/shared/SKILL.md",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Build() location order = %v, want %v", got, want)
	}
}

func TestBuildReportsDifferentContentOncePerID(t *testing.T) {
	t.Parallel()

	locations := []skill.Location{
		skillLocation("/c/shared/SKILL.md", "/real/b", skill.LevelGlobal, skill.SourceClaude),
		skillLocation("/a/shared/SKILL.md", "/real/a", skill.LevelGlobal, skill.SourceClaude),
		skillLocation("/b/shared/SKILL.md", "/real/a", skill.LevelGlobal, skill.SourceAgents),
		skillLocation("/empty/shared/SKILL.md", "", skill.LevelGlobal, skill.SourceCodex),
	}

	_, collisions := skill.Build(locations)
	collision, ok := collisionOfKind(collisions, skill.CollisionDifferentContent)
	if !ok {
		t.Fatal("Build() did not report different-content collision")
	}
	if got, want := collision.IDs, []string{"shared"}; !reflect.DeepEqual(got, want) {
		t.Errorf("different-content IDs = %v, want %v", got, want)
	}
	wantPaths := []string{
		"/a/shared/SKILL.md",
		"/b/shared/SKILL.md",
		"/c/shared/SKILL.md",
		"/empty/shared/SKILL.md",
	}
	if !reflect.DeepEqual(collision.Paths, wantPaths) {
		t.Errorf("different-content Paths = %v, want %v", collision.Paths, wantPaths)
	}
	if got := countCollisions(collisions, skill.CollisionDifferentContent); got != 1 {
		t.Errorf("Build() reported %d different-content collisions, want 1", got)
	}
}

func TestBuildReportsFrontmatterNameMismatchForSkillsOnly(t *testing.T) {
	t.Parallel()

	locations := []skill.Location{
		{
			Kind:            skill.KindSkill,
			DiscoveryPath:   "/skills/directory-name/SKILL.md",
			RealPath:        "/real/directory-name",
			Level:           skill.LevelGlobal,
			Source:          skill.SourceClaude,
			FrontmatterName: "frontmatter-name",
		},
		{
			Kind:          skill.KindSkill,
			DiscoveryPath: "/skills/no-frontmatter/SKILL.md",
			RealPath:      "/real/no-frontmatter",
			Level:         skill.LevelGlobal,
			Source:        skill.SourceClaude,
		},
		{
			Kind:            skill.KindCommand,
			DiscoveryPath:   "/commands/directory-name.md",
			RealPath:        "/real/command",
			Level:           skill.LevelGlobal,
			Source:          skill.SourceClaude,
			FrontmatterName: "ignored-command-name",
		},
	}

	_, collisions := skill.Build(locations)

	if got := countCollisions(collisions, skill.CollisionFrontmatterName); got != 1 {
		t.Fatalf("Build() reported %d frontmatter-name collisions, want 1", got)
	}
	collision, _ := collisionOfKind(collisions, skill.CollisionFrontmatterName)
	if got, want := collision.IDs, []string{"directory-name"}; !reflect.DeepEqual(got, want) {
		t.Errorf("frontmatter-name IDs = %v, want %v", got, want)
	}
	if collision.Name != "frontmatter-name" {
		t.Errorf("frontmatter-name Name = %q, want %q", collision.Name, "frontmatter-name")
	}
	if got, want := collision.Paths, []string{"/skills/directory-name/SKILL.md"}; !reflect.DeepEqual(got, want) {
		t.Errorf("frontmatter-name Paths = %v, want %v", got, want)
	}
}

func TestBuildReportsEffectiveNameCollisionWithStableIDsAndPaths(t *testing.T) {
	t.Parallel()

	locations := []skill.Location{
		locationWithName("/z/beta/SKILL.md", "z", skill.AgentOpenCode, "shared"),
		locationWithName("/alpha/SKILL.md", "", skill.AgentOpenCode, "shared"),
		locationWithName("/a/beta/SKILL.md", "z", skill.AgentOpenCode, "shared"),
		locationWithName("/codex/gamma/SKILL.md", "", skill.AgentCodex, "shared"),
	}

	_, collisions := skill.Build(locations)
	collision, ok := effectiveCollision(collisions, skill.AgentOpenCode, "shared")
	if !ok {
		t.Fatal("Build() did not report effective-name collision")
	}
	if got, want := collision.IDs, []string{"alpha", "z:beta"}; !reflect.DeepEqual(got, want) {
		t.Errorf("effective-name IDs = %v, want %v", got, want)
	}
	wantPaths := []string{"/a/beta/SKILL.md", "/alpha/SKILL.md", "/z/beta/SKILL.md"}
	if !reflect.DeepEqual(collision.Paths, wantPaths) {
		t.Errorf("effective-name Paths = %v, want %v", collision.Paths, wantPaths)
	}
	if got := countCollisions(collisions, skill.CollisionEffectiveName); got != 1 {
		t.Errorf("Build() reported %d effective-name collisions, want 1", got)
	}

	reversed := slices.Clone(locations)
	slices.Reverse(reversed)
	_, reversedCollisions := skill.Build(reversed)
	if !reflect.DeepEqual(collisions, reversedCollisions) {
		t.Errorf("Build() collision order depends on input order:\nforward: %#v\nreverse: %#v", collisions, reversedCollisions)
	}
}

func TestBuildDoesNotMutateOrAliasInput(t *testing.T) {
	t.Parallel()

	locations := []skill.Location{
		locationWithName("/z/SKILL.md", "", skill.AgentClaude, "z"),
		locationWithName("/a/SKILL.md", "", skill.AgentClaude, "a"),
	}
	original := cloneLocations(locations)

	skills, _ := skill.Build(locations)

	if !reflect.DeepEqual(locations, original) {
		t.Fatalf("Build() mutated input:\ngot:  %#v\nwant: %#v", locations, original)
	}
	locations[1].Names[skill.AgentClaude] = "input-mutated"
	if got := skills[0].Locations[0].Names[skill.AgentClaude]; got != "a" {
		t.Errorf("output Names aliases input map: got %q after input mutation", got)
	}
	skills[0].Locations[0].Names[skill.AgentClaude] = "output-mutated"
	if got := locations[1].Names[skill.AgentClaude]; got != "input-mutated" {
		t.Errorf("input Names aliases output map: got %q after output mutation", got)
	}
}

func skillLocation(discoveryPath, realPath string, level skill.Level, source skill.Source) skill.Location {
	return skill.Location{
		Kind:          skill.KindSkill,
		DiscoveryPath: discoveryPath,
		RealPath:      realPath,
		Level:         level,
		Source:        source,
	}
}

func locationWithName(discoveryPath, scope string, agent skill.Agent, name string) skill.Location {
	location := skillLocation(discoveryPath, discoveryPath, skill.LevelGlobal, skill.SourceClaude)
	location.Scope = scope
	location.Names = map[skill.Agent]string{agent: name}
	return location
}

func skillIDs(skills []skill.Skill) []string {
	ids := make([]string, len(skills))
	for i := range skills {
		ids[i] = skills[i].ID
	}
	return ids
}

func locationPaths(locations []skill.Location) []string {
	paths := make([]string, len(locations))
	for i := range locations {
		paths[i] = locations[i].DiscoveryPath
	}
	return paths
}

func collisionOfKind(collisions []skill.Collision, kind skill.CollisionKind) (skill.Collision, bool) {
	for _, collision := range collisions {
		if collision.Kind == kind {
			return collision, true
		}
	}
	return skill.Collision{}, false
}

func effectiveCollision(collisions []skill.Collision, agent skill.Agent, name string) (skill.Collision, bool) {
	for _, collision := range collisions {
		if collision.Kind == skill.CollisionEffectiveName && collision.Agent == agent && collision.Name == name {
			return collision, true
		}
	}
	return skill.Collision{}, false
}

func countCollisions(collisions []skill.Collision, kind skill.CollisionKind) int {
	count := 0
	for _, collision := range collisions {
		if collision.Kind == kind {
			count++
		}
	}
	return count
}

func cloneLocations(locations []skill.Location) []skill.Location {
	cloned := make([]skill.Location, len(locations))
	for i, location := range locations {
		cloned[i] = location
		if location.Names != nil {
			cloned[i].Names = make(map[skill.Agent]string, len(location.Names))
			for agent, name := range location.Names {
				cloned[i].Names[agent] = name
			}
		}
	}
	return cloned
}
