package skill_test

import (
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/scarb/skope/internal/skill"
)

func TestScanForeignRootsDescriptionControlsOnlyCodexVisibility(t *testing.T) {
	for _, body := range []string{"body", "---\nname: declared\n---\n", "---\ndescription: ''\n---\n", "---\ndescription: 42\n---\n"} {
		f := &foreignFS{mapFileSystem: newMapFS(fstest.MapFS{"skills/example/SKILL.md": file(body)})}
		root := skill.Root{Path: "/skills", Kind: skill.KindSkill, Source: skill.SourceCodex, VisibleTo: []skill.Agent{skill.AgentCodex, skill.AgentClaude}, ScanMode: skill.CodexRecursive}
		got, err := (skill.Scanner{FS: f, RegularFiles: f}).ScanForeignRoots([]skill.Root{root}, 100)
		if err != nil || len(got.Skills) != 1 || len(got.Rejections) != 0 {
			t.Fatalf("%+v %v", got, err)
		}
		loc := got.Skills[0].Locations[0]
		if loc.Names[skill.AgentCodex] != "" || loc.Names[skill.AgentClaude] != "example" || loc.DiscoveryPath != "/skills/example/SKILL.md" {
			t.Fatal(loc)
		}
		if _, err := (skill.Scanner{FS: f.mapFileSystem}).ScanRoots([]skill.Root{root}); err == nil {
			t.Fatalf("native accepted %q", body)
		}
	}
}

func TestScanForeignRootsRejectsInvalidYamlAndName(t *testing.T) {
	for _, body := range []string{"---\nname: 42\n---\n", "---\nname: [\n---\n"} {
		f := &foreignFS{mapFileSystem: newMapFS(fstest.MapFS{"skills/example/SKILL.md": file(body)})}
		_, err := (skill.Scanner{FS: f, RegularFiles: f}).ScanForeignRoots([]skill.Root{{Path: "/skills", Kind: skill.KindSkill, ScanMode: skill.CodexRecursive}}, 100)
		if err == nil {
			t.Fatalf("accepted %q", body)
		}
	}
}

func TestScanForeignRootsUsesClaudePluginBoundary(t *testing.T) {
	f := &foreignFS{mapFileSystem: newMapFS(fstest.MapFS{"skills/plugin/.claude-plugin/plugin.json": file(`{"name":"plugin"}`), "skills/plugin/SKILL.md": file("body"), "skills/ordinary/SKILL.md": file("body")})}
	got, err := (skill.Scanner{FS: f, RegularFiles: f}).ScanForeignRoots([]skill.Root{{Path: "/skills", Source: skill.SourceClaude, Kind: skill.KindSkill}}, 100)
	if err != nil || len(got.Skills) != 1 || got.Skills[0].ID != "ordinary" {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestScanForeignRootsCommandRejectionsAndReadErrors(t *testing.T) {
	for _, tt := range []struct {
		name   string
		mode   fs.FileMode
		body   string
		reason skill.ResolutionReason
	}{
		{"large", 0, "123456789", skill.ReasonLimitExceeded},
		{"special", fs.ModeNamedPipe, "", skill.ReasonSpecialFile},
	} {
		f := &foreignFS{mapFileSystem: newMapFS(fstest.MapFS{"commands/git/run.md": {Data: []byte(tt.body), Mode: tt.mode}})}
		got, err := (skill.Scanner{FS: f, RegularFiles: f}).ScanForeignRoots([]skill.Root{{Path: "/commands", Kind: skill.KindCommand, Source: skill.SourceClaude}}, 8)
		if err != nil || len(got.Skills) != 1 || got.Skills[0].ID != "git:run" || len(got.Skills[0].Locations[0].Names) != 0 || len(got.Rejections) != 1 || got.Rejections[0].Reason != tt.reason || f.opens != 0 {
			t.Fatalf("%s: %+v %v", tt.name, got, err)
		}
	}
	boom := errors.New("read failure")
	closeBoom := errors.New("close failure")
	f := &foreignFS{mapFileSystem: newMapFS(fstest.MapFS{"commands/run.md": file("body")}), readErr: boom, closeErr: closeBoom}
	_, err := (skill.Scanner{FS: f, RegularFiles: f}).ScanForeignRoots([]skill.Root{{Path: "/commands", Kind: skill.KindCommand}}, 100)
	if !errors.Is(err, boom) || !errors.Is(err, closeBoom) {
		t.Fatal(err)
	}
}

func TestScanForeignRootsRejectsDanglingCommandRoot(t *testing.T) {
	f := &foreignFS{mapFileSystem: newMapFS(fstest.MapFS{"commands": symlink("missing")})}
	// Go 1.24 MapFS does not follow symlinks; model the host ReadDir error explicitly.
	f.errors["readDir:/commands"] = fs.ErrNotExist
	_, err := (skill.Scanner{FS: f, RegularFiles: f}).ScanForeignRoots([]skill.Root{{Path: "/commands", Kind: skill.KindCommand}}, 100)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("dangling command root: %v", err)
	}
}

func TestScanForeignRootsRejectedCommandsKeepCustomRootIdentity(t *testing.T) {
	f := &foreignFS{mapFileSystem: newMapFS(fstest.MapFS{
		"custom/a/run.md": file("too large"),
		"custom/b/run.md": {Mode: fs.ModeNamedPipe},
	})}
	root := skill.Root{Path: "/custom", Kind: skill.KindCommand, Source: skill.SourceClaude, Scope: "app", NamePrefix: "plugin", PluginID: "p@m", PluginAgent: skill.AgentClaude}
	got, err := (skill.Scanner{FS: f, RegularFiles: f}).ScanForeignRoots([]skill.Root{root}, 2)
	if err != nil || len(got.Skills) != 2 || got.Skills[0].ID != "plugin:app:a:run" || got.Skills[1].ID != "plugin:app:b:run" || len(got.Rejections) != 2 {
		t.Fatalf("result=%+v error=%v", got, err)
	}
	for _, candidate := range got.Skills {
		loc := candidate.Locations[0]
		if len(loc.Names) != 0 || loc.Scope != root.Scope || loc.PluginID != root.PluginID || loc.PluginAgent != root.PluginAgent {
			t.Fatal(loc)
		}
	}
}

func TestScanForeignRootsPreservesMetadataAndBounds(t *testing.T) {
	f := &foreignFS{mapFileSystem: newMapFS(fstest.MapFS{
		"skills/group/valid/SKILL.md":     file("---\nname: declared\ndescription: valid\n---\n"),
		"skills/group/bare/SKILL.md":      file("body"),
		"skills/group/large/SKILL.md":     file(string(make([]byte, 200))),
		"skills/.system/ignored/SKILL.md": file("body"),
	})}
	root := skill.Root{Path: "/skills", Kind: skill.KindSkill, Level: skill.LevelPlugin, Source: skill.SourceCodex, Scope: "apps", PluginID: "demo@market", PluginAgent: skill.AgentCodex, NamePrefix: "demo", VisibleTo: []skill.Agent{skill.AgentCodex}, ScanMode: skill.CodexRecursive}
	got, err := (skill.Scanner{FS: f, RegularFiles: f}).ScanForeignRoots([]skill.Root{root}, 100)
	if err != nil || len(got.Skills) != 3 || len(got.Rejections) != 1 {
		t.Fatalf("%+v %v", got, err)
	}
	for _, item := range got.Skills {
		loc := item.Locations[0]
		if loc.Scope != root.Scope || loc.PluginID != root.PluginID || loc.PluginAgent != root.PluginAgent || loc.Source != root.Source || loc.Kind != root.Kind {
			t.Fatal(loc)
		}
		if loc.FrontmatterName == "declared" {
			if loc.Names[skill.AgentCodex] != "demo:declared" {
				t.Fatal(loc)
			}
		} else if len(loc.Names) != 0 {
			t.Fatal(loc)
		}
	}
	if f.reads > 100 {
		t.Fatalf("read %d bytes", f.reads)
	}
}

func TestScanForeignRootsCommandsKeepNamespace(t *testing.T) {
	f := &foreignFS{mapFileSystem: newMapFS(fstest.MapFS{"commands/git/commit.md": file("body")})}
	root := skill.Root{Path: "/commands", Kind: skill.KindCommand, Level: skill.LevelProject, Source: skill.SourceClaude, Scope: "app", NamePrefix: "plugin", VisibleTo: []skill.Agent{skill.AgentClaude, skill.AgentCodex}}
	got, err := (skill.Scanner{FS: f, RegularFiles: f}).ScanForeignRoots([]skill.Root{root}, 100)
	if err != nil || len(got.Skills) != 1 {
		t.Fatalf("%+v %v", got, err)
	}
	loc := got.Skills[0].Locations[0]
	if loc.Names[skill.AgentClaude] != "plugin:app:git:commit" || loc.Names[skill.AgentCodex] != "" {
		t.Fatal(loc)
	}
}
