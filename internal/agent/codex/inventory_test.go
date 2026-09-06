package codex_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/scarb/skope/internal/agent"
	"github.com/scarb/skope/internal/agent/codex"
	"github.com/scarb/skope/internal/host"
	"github.com/scarb/skope/internal/skill"
)

type inventoryScanner struct {
	native func(host.Env, host.CodexPaths) (skill.ScanResult, error)
	roots  func([]skill.Root) (skill.ScanResult, error)
}

func (s inventoryScanner) ScanCodex(e host.Env, p host.CodexPaths) (skill.ScanResult, error) {
	return s.native(e, p)
}
func (s inventoryScanner) ScanRoots(r []skill.Root) (skill.ScanResult, error) { return s.roots(r) }

type inventorySource func(context.Context, host.Env, host.CodexPaths) (codex.CatalogSnapshot, error)

func (s inventorySource) Read(c context.Context, e host.Env, p host.CodexPaths) (codex.CatalogSnapshot, error) {
	return s(c, e, p)
}

type inventoryCanonicalizer struct{}

func (inventoryCanonicalizer) EvalSymlinks(string) (string, error) {
	panic("inventory must not canonicalize")
}
func inventoryLocation(path, name string) skill.Location {
	return skill.Location{Kind: skill.KindSkill, Source: skill.SourceCodex, Level: skill.LevelGlobal, DiscoveryPath: path, RealPath: path, Names: map[skill.Agent]string{skill.AgentCodex: name}}
}

func TestCodexCapabilitiesAndOptionsConstruction(t *testing.T) {
	a := codex.New(nil, nil, nil, codex.Options{ResolvePaths: func(host.Env) (host.CodexPaths, error) { panic("constructor performed IO") }})
	if a.Name() != skill.AgentCodex || a.Capabilities() != (agent.Capabilities{TogglePlugins: true, ToggleBundled: true}) {
		t.Fatalf("wrong static identity: %s %+v", a.Name(), a.Capabilities())
	}
	if inv, err := a.Inventory(context.Background(), host.Env{}); err == nil || !reflect.DeepEqual(inv, agent.Inventory{}) {
		t.Fatalf("invalid dependencies: %+v %v", inv, err)
	}
}

func TestCodexInventoryMergesVisibilityWithoutAliasing(t *testing.T) {
	ordinary := skill.ScanResult{Skills: []skill.Skill{
		{ID: "retained-id", Locations: []skill.Location{inventoryLocation("/native/a/SKILL.md", "alpha"), inventoryLocation("/native/b/SKILL.md", "alpha")}},
		{ID: "foreign", Locations: []skill.Location{{Source: skill.SourceClaude, DiscoveryPath: "/foreign/SKILL.md", Names: map[skill.Agent]string{skill.AgentClaude: "foreign"}}}},
		{ID: "empty", Locations: []skill.Location{inventoryLocation("/empty/SKILL.md", "")}},
	}}
	pl := inventoryLocation("/plugin/a/SKILL.md", "zeta")
	pl.PluginID = "installed@market"
	pl.PluginAgent = skill.AgentCodex
	pl.Level = skill.LevelPlugin
	plugins := skill.ScanResult{Skills: []skill.Skill{{ID: "retained-id", Locations: []skill.Location{pl}}}}
	snapshot := codex.CatalogSnapshot{PluginIDs: []string{"installed@market", "config@market", "installed@market"}, InstalledIDs: []string{"installed@market"}, Warnings: []string{"catalog warning"}, SkillRoots: []skill.Root{{Path: "/plugin", VisibleTo: []skill.Agent{skill.AgentCodex}, PluginID: "installed@market", PluginAgent: skill.AgentCodex}}}
	paths := host.CodexPaths{Home: "/home", CodexHome: "/codex", AdminSkillRoots: []string{"/admin"}, SystemConfigPaths: []string{"/system"}}
	env := host.NewEnv("/home", "/work", map[string]string{"UNCHANGED": "value"})
	var calls []string
	scanner := inventoryScanner{
		native: func(e host.Env, p host.CodexPaths) (skill.ScanResult, error) {
			calls = append(calls, "native")
			if !reflect.DeepEqual(e, env) || !reflect.DeepEqual(p, paths) {
				t.Fatalf("scan inputs changed: %+v %+v", e, p)
			}
			p.AdminSkillRoots[0] = "mutated"
			return ordinary, nil
		},
		roots: func(r []skill.Root) (skill.ScanResult, error) {
			calls = append(calls, "roots")
			if !reflect.DeepEqual(r, snapshot.SkillRoots) {
				t.Fatalf("roots: %+v", r)
			}
			r[0].VisibleTo[0] = skill.AgentClaude
			r[0].Path = "mutated"
			return plugins, nil
		},
	}
	source := inventorySource(func(_ context.Context, e host.Env, p host.CodexPaths) (codex.CatalogSnapshot, error) {
		calls = append(calls, "catalog")
		if !reflect.DeepEqual(e, env) || !reflect.DeepEqual(p, paths) {
			t.Fatal("catalog inputs changed")
		}
		p.SystemConfigPaths[0] = "mutated"
		return snapshot, nil
	})
	allowed := []string{"missing@market", "missing@market", "config@market", "installed@market"}
	a := codex.New(scanner, source, inventoryCanonicalizer{}, codex.Options{Plugins: allowed, ResolvePaths: func(e host.Env) (host.CodexPaths, error) {
		calls = append(calls, "resolve")
		if !reflect.DeepEqual(e, env) {
			t.Fatal("resolver env changed")
		}
		return paths, nil
	}})
	allowed[0] = "different@market"
	inv, err := a.Inventory(context.Background(), env)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(calls, []string{"resolve", "catalog", "native", "roots"}) {
		t.Fatalf("dependency order %v", calls)
	}
	if !reflect.DeepEqual(inv.SkillNames, []string{"alpha", "zeta"}) || !reflect.DeepEqual(inv.PluginIDs, []string{"config@market", "installed@market"}) {
		t.Fatalf("visibility %+v", inv)
	}
	if !reflect.DeepEqual(inv.Warnings, []string{"catalog warning", "plugin missing@market is not installed", "plugin config@market is not installed"}) {
		t.Fatalf("warnings %v", inv.Warnings)
	}
	var merged *skill.Skill
	for i := range inv.Skills {
		if inv.Skills[i].ID == "retained-id" {
			merged = &inv.Skills[i]
		}
	}
	if merged == nil || len(merged.Locations) != 3 {
		t.Fatalf("merge lost IDs/locations %+v", inv.Skills)
	}
	found := false
	for _, loc := range merged.Locations {
		if loc.PluginID == "installed@market" && loc.PluginAgent == skill.AgentCodex {
			found = true
		}
	}
	if !found {
		t.Fatal("plugin identity lost")
	}
	if len(inv.Collisions) == 0 {
		t.Fatal("merge did not recompute collisions")
	}
	for _, s := range inv.Skills {
		for i := range s.Locations {
			s.Locations[i].Names[skill.AgentCodex] = "changed"
			s.Locations[i].DiscoveryPath = "changed"
		}
	}
	inv.PluginIDs[0] = "changed"
	inv.Warnings[0] = "changed"
	inv.Collisions[0].Paths[0] = "changed"
	if ordinary.Skills[0].Locations[0].Names[skill.AgentCodex] != "alpha" || plugins.Skills[0].Locations[0].Names[skill.AgentCodex] != "zeta" || snapshot.PluginIDs[0] != "installed@market" || snapshot.Warnings[0] != "catalog warning" || snapshot.SkillRoots[0].Path != "/plugin" || snapshot.SkillRoots[0].VisibleTo[0] != skill.AgentCodex || paths.AdminSkillRoots[0] != "/admin" || paths.SystemConfigPaths[0] != "/system" {
		t.Fatal("inventory aliases dependency data")
	}
}

func TestCodexInventoryStopsOnFailureOrCancellation(t *testing.T) {
	for _, stage := range []string{"before", "resolve", "catalog", "native", "roots"} {
		for _, cancel := range []bool{false, true} {
			t.Run(stage+map[bool]string{true: " cancellation", false: " failure"}[cancel], func(t *testing.T) {
				ctx, stop := context.WithCancel(context.Background())
				defer stop()
				sentinel := errors.New("dependency failed")
				var calls []string
				step := func(name string) error {
					calls = append(calls, name)
					if stage == name {
						if cancel {
							stop()
							return nil
						}
						return sentinel
					}
					return nil
				}
				if stage == "before" {
					stop()
				}
				scanner := inventoryScanner{native: func(host.Env, host.CodexPaths) (skill.ScanResult, error) {
					return skill.ScanResult{Skills: []skill.Skill{{ID: "partial", Locations: []skill.Location{inventoryLocation("/partial/SKILL.md", "partial")}}}}, step("native")
				}, roots: func([]skill.Root) (skill.ScanResult, error) { return skill.ScanResult{}, step("roots") }}
				source := inventorySource(func(context.Context, host.Env, host.CodexPaths) (codex.CatalogSnapshot, error) {
					return codex.CatalogSnapshot{PluginIDs: []string{"partial@market"}}, step("catalog")
				})
				a := codex.New(scanner, source, inventoryCanonicalizer{}, codex.Options{ResolvePaths: func(host.Env) (host.CodexPaths, error) { return host.CodexPaths{}, step("resolve") }})
				inv, err := a.Inventory(ctx, host.Env{})
				want := sentinel
				if cancel || stage == "before" {
					want = context.Canceled
				}
				if !errors.Is(err, want) || !reflect.DeepEqual(inv, agent.Inventory{}) {
					t.Fatalf("failure leaked inventory or lost error: %+v %v", inv, err)
				}
				expected := []string{"resolve", "catalog", "native", "roots"}
				n := 0
				for _, name := range expected {
					if stage == "before" {
						break
					}
					n++
					if name == stage {
						break
					}
				}
				if len(calls) != n {
					t.Fatalf("continued after failure: %v", calls)
				}
			})
		}
	}
}

func TestCodexOptionsRejectInvalidPluginBeforeDependencies(t *testing.T) {
	scanner := inventoryScanner{native: func(host.Env, host.CodexPaths) (skill.ScanResult, error) { panic("scan called") }, roots: func([]skill.Root) (skill.ScanResult, error) { panic("roots called") }}
	source := inventorySource(func(context.Context, host.Env, host.CodexPaths) (codex.CatalogSnapshot, error) {
		panic("catalog called")
	})
	for _, id := range []string{"", "secret/invalid@market", "missing-at"} {
		a := codex.New(scanner, source, inventoryCanonicalizer{}, codex.Options{Plugins: []string{id}, ResolvePaths: func(host.Env) (host.CodexPaths, error) { panic("resolver called") }})
		inv, err := a.Inventory(context.Background(), host.Env{})
		if err == nil || !reflect.DeepEqual(inv, agent.Inventory{}) || strings.Contains(err.Error(), "secret") {
			t.Fatalf("invalid option: %+v %v", inv, err)
		}
	}
}

func TestCodexInventoryRejectsEachMissingDependency(t *testing.T) {
	for _, missing := range []string{"scanner", "sources", "paths", "resolver"} {
		t.Run(missing, func(t *testing.T) {
			var scanner codex.SkillScanner = inventoryScanner{native: func(host.Env, host.CodexPaths) (skill.ScanResult, error) { panic("scanner called") }, roots: func([]skill.Root) (skill.ScanResult, error) { panic("roots called") }}
			var source codex.SourceReader = inventorySource(func(context.Context, host.Env, host.CodexPaths) (codex.CatalogSnapshot, error) {
				panic("source called")
			})
			var paths codex.Canonicalizer = inventoryCanonicalizer{}
			resolver := func(host.Env) (host.CodexPaths, error) { panic("resolver called") }
			switch missing {
			case "scanner":
				scanner = nil
			case "sources":
				source = nil
			case "paths":
				paths = nil
			case "resolver":
				resolver = nil
			}
			a := codex.New(scanner, source, paths, codex.Options{ResolvePaths: resolver})
			inv, err := a.Inventory(context.Background(), host.Env{})
			if err == nil || !reflect.DeepEqual(inv, agent.Inventory{}) {
				t.Fatalf("missing %s accepted: %+v %v", missing, inv, err)
			}
		})
	}
}
