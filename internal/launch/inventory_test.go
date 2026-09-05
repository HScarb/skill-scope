package launch

import (
	"reflect"
	"testing"

	"github.com/scarb/skope/internal/agent"
	"github.com/scarb/skope/internal/skill"
)

func TestMergeForeignInventoryPreservesSourcesAndCopies(t *testing.T) {
	t.Parallel()
	nativeLoc := skill.Location{Kind: skill.KindSkill, Level: skill.LevelGlobal, Source: skill.SourceClaude, DiscoveryPath: "/claude/shared/SKILL.md", RealPath: "/claude/shared/SKILL.md", Names: map[skill.Agent]string{skill.AgentClaude: "shared"}}
	foreignLoc := skill.Location{Kind: skill.KindSkill, Level: skill.LevelGlobal, Source: skill.SourceAgents, DiscoveryPath: "/agents/shared/SKILL.md", RealPath: "/agents/shared/SKILL.md", Names: map[skill.Agent]string{skill.AgentCodex: "effective"}}
	rejectedLoc := skill.Location{Kind: skill.KindSkill, Level: skill.LevelGlobal, Source: skill.SourceCodex, DiscoveryPath: "/codex/rejected/SKILL.md", RealPath: "/codex/rejected/SKILL.md"}
	native := agent.Inventory{SkillNames: []string{"shared"}, PluginIDs: []string{"p@m"}, Warnings: []string{"warning"}}
	native.Skills, native.Collisions = skill.Build([]skill.Location{nativeLoc})
	foreign := skill.ScanResult{Rejections: []skill.ScanRejection{{Source: skill.SourceCodex, DiscoveryPath: rejectedLoc.DiscoveryPath, Reason: skill.ReasonLimitExceeded}}}
	foreign.Skills, foreign.Collisions = skill.Build([]skill.Location{foreignLoc, rejectedLoc})
	wantNative := cloneInventory(native)
	wantNative.Skills, wantNative.Collisions = skill.Build([]skill.Location{nativeLoc})
	wantForeign := skill.ScanResult{Rejections: append([]skill.ScanRejection(nil), foreign.Rejections...)}
	wantForeign.Skills, wantForeign.Collisions = skill.Build([]skill.Location{foreignLoc, rejectedLoc})
	got, rejections := mergeForeignInventory(native, foreign)
	if len(got.Skills) != 2 || got.Skills[0].ID != "rejected" || got.Skills[1].ID != "shared" || len(got.Skills[1].Locations) != 2 || len(got.Collisions) != 1 || got.Collisions[0].Kind != skill.CollisionDifferentContent {
		t.Fatalf("inventory=%#v", got)
	}
	if !reflect.DeepEqual(rejections, foreign.Rejections) || !reflect.DeepEqual(got.SkillNames, native.SkillNames) || !reflect.DeepEqual(got.PluginIDs, native.PluginIDs) || !reflect.DeepEqual(got.Warnings, native.Warnings) {
		t.Fatalf("metadata=%#v rejections=%#v", got, rejections)
	}
	got.SkillNames[0] = "changed"
	got.PluginIDs[0] = "changed"
	got.Warnings[0] = "changed"
	got.Skills[1].Locations[0].Names[skill.AgentClaude] = "changed"
	got.Skills[1].Locations[1].Names[skill.AgentCodex] = "changed"
	got.Collisions[0].Paths[0] = "changed"
	got.Collisions[0].IDs[0] = "changed"
	rejections[0].Reason = skill.ReasonSpecialFile
	if !reflect.DeepEqual(native, wantNative) || !reflect.DeepEqual(foreign, wantForeign) {
		t.Fatalf("inputs changed: native=%#v foreign=%#v", native, foreign)
	}
}
