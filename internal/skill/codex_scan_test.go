package skill_test

import (
	"errors"
	"io/fs"
	"reflect"
	"testing"
	"testing/fstest"

	"github.com/scarb/skope/internal/host"
	"github.com/scarb/skope/internal/skill"
)

func TestScanCodexEnteredDirectoryErrorsFailClosed(t *testing.T) {
	for _, operation := range []string{"readDir:/skills/group", "lstat:/skills/group/SKILL.md", "evalSymlinks:/skills/group"} {
		f := newMapFS(fstest.MapFS{"skills/group/SKILL.md": file("---\ndescription: valid\n---\n")})
		f.errors[operation] = fs.ErrPermission
		_, err := (skill.Scanner{FS: f}).ScanRoots([]skill.Root{{Path: "/skills", Kind: skill.KindSkill, ScanMode: skill.CodexRecursive}})
		if !errors.Is(err, fs.ErrPermission) {
			t.Fatalf("%s: %v", operation, err)
		}
	}
}

func TestScanCodexRejectsDanglingSkillFile(t *testing.T) {
	f := newMapFS(fstest.MapFS{"skills/group/SKILL.md": symlink("missing.md")})
	_, err := (skill.Scanner{FS: f}).ScanRoots([]skill.Root{{Path: "/skills", Kind: skill.KindSkill, ScanMode: skill.CodexRecursive}})
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("dangling SKILL.md: %v", err)
	}
}

func TestScanCodexAndForeignRootsMatchValidMetadata(t *testing.T) {
	root := skill.Root{Path: "/skills", Kind: skill.KindSkill, Level: skill.LevelPlugin, Source: skill.SourceCodex, Scope: "apps", PluginID: "demo@market", PluginAgent: skill.AgentCodex, NamePrefix: "demo", VisibleTo: []skill.Agent{skill.AgentCodex}, ScanMode: skill.CodexRecursive}
	f := &foreignFS{mapFileSystem: newMapFS(fstest.MapFS{"skills/group/example/SKILL.md": file("---\nname: declared\ndescription: valid\n---\n")})}
	scanner := skill.Scanner{FS: f.mapFileSystem, RegularFiles: f}
	native, err := scanner.ScanRoots([]skill.Root{root})
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := scanner.ScanForeignRoots([]skill.Root{root}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(native, foreign) {
		t.Fatalf("native=%+v foreign=%+v", native, foreign)
	}
}

func TestScanCodexRootsAndRecursiveScope(t *testing.T) {
	for _, marker := range []*fstest.MapFile{dir(), file("gitdir: elsewhere")} {
		f := newMapFS(fstest.MapFS{
			"repo/.git":                                          marker,
			"home/.agents/skills/global/SKILL.md":                file("---\ndescription: valid\n---\n"),
			"custom/skills/group/parent/SKILL.md":                file("---\nname: parent-name\ndescription: valid\n---\n"),
			"custom/skills/group/parent/child/SKILL.md":          file("---\ndescription: valid\n---\n"),
			"custom/skills/.system/hidden/SKILL.md":              file("invalid"),
			"custom/skills/group/.hidden/SKILL.md":               file("invalid"),
			"repo/.agents/skills/repo/SKILL.md":                  file("---\ndescription: valid\n---\n"),
			"repo/apps/.codex/skills/middle/SKILL.md":            file("---\ndescription: valid\n---\n"),
			"repo/apps/api/.agents/skills/leaf/SKILL.md":         file("---\ndescription: valid\n---\n"),
			"repo/apps/other/.agents/skills/ignored/SKILL.md":    file("invalid"),
			"repo/apps/api/deep/.agents/skills/ignored/SKILL.md": file("invalid"),
			"admin/admin/SKILL.md":                               file("---\ndescription: valid\n---\n"),
		})
		env := host.NewEnv("/wrong", "/repo/apps/api", map[string]string{"CODEX_HOME": "/wrong"})
		before := env.Environ()
		paths := host.CodexPaths{Home: "/home", CodexHome: "/custom", AdminSkillRoots: []string{"/admin"}}
		result, err := (skill.Scanner{FS: f}).ScanCodex(env, paths)
		if err != nil {
			t.Fatal(err)
		}
		if result.ProjectRoot != "/repo" || len(result.Skills) != 7 {
			t.Fatalf("result = %+v", result)
		}
		for _, item := range result.Skills {
			for _, loc := range item.Locations {
				if loc.Kind != skill.KindSkill || loc.PluginID != "" || loc.PluginAgent != "" || loc.Names[skill.AgentCodex] == "" {
					t.Fatal(loc)
				}
				if loc.DiscoveryPath == "/repo/apps/.codex/skills/middle/SKILL.md" && loc.Scope != "apps" {
					t.Fatal(loc)
				}
				if loc.DiscoveryPath == "/admin/admin/SKILL.md" && (loc.Level != skill.LevelAdmin || loc.Source != skill.SourceCodex) {
					t.Fatal(loc)
				}
			}
		}
		if !reflect.DeepEqual(before, env.Environ()) {
			t.Fatal("env changed")
		}
	}
}

func TestScanCodexWithoutGitUsesOnlyCwd(t *testing.T) {
	f := newMapFS(fstest.MapFS{"repo/.agents/skills/ignored/SKILL.md": file("bad"), "repo/app/.codex/skills/ok/SKILL.md": file("---\ndescription: ok\n---\n")})
	got, err := (skill.Scanner{FS: f}).ScanCodex(host.NewEnv("/home", "/repo/app", nil), host.CodexPaths{Home: "/home", CodexHome: "/codex"})
	if err != nil || got.ProjectRoot != "/repo/app" || len(got.Skills) != 1 {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestScanCodexRequiresDescription(t *testing.T) {
	for _, body := range []string{"body", "---\nname: only\n---\n", "---\ndescription: ''\n---\n", "---\ndescription: 42\n---\n", "---\nname: 42\ndescription: ok\n---\n", "---\ndescription: [\n---\n"} {
		f := newMapFS(fstest.MapFS{"codex/skills/bad/SKILL.md": file(body)})
		if _, err := (skill.Scanner{FS: f}).ScanCodex(host.NewEnv("/home", "/repo", nil), host.CodexPaths{Home: "/home", CodexHome: "/codex"}); err == nil {
			t.Fatalf("accepted %q", body)
		}
	}
}
