package skill_test

import (
	"errors"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/scarb/skope/internal/skill"
)

func TestCodexManifestValidation(t *testing.T) {
	for _, tt := range []struct {
		body, want string
		bad        bool
	}{
		{`{"name":"demo","version":"1","description":"extra"}`, "skills/", false},
		{`{"name":"demo","skills":"custom"}`, "custom", false},
		{`{"name":"demo","skills":"../secret"}`, "", true},
		{`{"name":"demo","skills":"/secret"}`, "", true},
		{`{"name":4,"secret":"DO_NOT_ECHO"}`, "", true},
		{`{"secret":"DO_NOT_ECHO"`, "", true},
	} {
		f := newMapFS(fstest.MapFS{"plugin/.codex-plugin/plugin.json": file(tt.body)})
		got, err := (skill.Scanner{FS: f}).ReadCodexManifest("/plugin")
		if tt.bad {
			if err == nil || strings.Contains(err.Error(), "DO_NOT_ECHO") || !strings.Contains(err.Error(), "plugin.json") {
				t.Fatalf("%+v %v", got, err)
			}
		} else if err != nil || got.Name != "demo" || got.Skills != tt.want {
			t.Fatalf("%+v %v", got, err)
		}
	}
	_, err := (skill.Scanner{FS: newMapFS(fstest.MapFS{})}).ReadCodexManifest("/missing")
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
}

func TestScanCodexOrdinaryManifestOnlyNamespacesSubtree(t *testing.T) {
	f := newMapFS(fstest.MapFS{"skills/bundle/.codex-plugin/plugin.json": file(`{"name":"demo"}`), "skills/bundle/SKILL.md": file("---\ndescription: valid\n---\n"), "skills/bundle/group/child/SKILL.md": file("---\nname: declared\ndescription: valid\n---\n")})
	got, err := (skill.Scanner{FS: f}).ScanRoots([]skill.Root{{Path: "/skills", Kind: skill.KindSkill, Source: skill.SourceCodex, VisibleTo: []skill.Agent{skill.AgentCodex}, ScanMode: skill.CodexRecursive}})
	if err != nil || len(got.Skills) != 2 {
		t.Fatalf("%+v %v", got, err)
	}
	for _, item := range got.Skills {
		loc := item.Locations[0]
		if loc.PluginID != "" || !strings.HasPrefix(loc.Names[skill.AgentCodex], "demo:") {
			t.Fatal(loc)
		}
	}
}

func TestScanCodexIncludesRootSkillAndRootManifestNamespace(t *testing.T) {
	f := newMapFS(fstest.MapFS{
		"skills/.codex-plugin/plugin.json": file(`{"name":"rootns"}`),
		"skills/SKILL.md":                  file("---\ndescription: valid\n---\n"),
		"skills/child/SKILL.md":            file("---\ndescription: valid\n---\n"),
	})
	got, err := (skill.Scanner{FS: f}).ScanRoots([]skill.Root{{Path: "/skills", Kind: skill.KindSkill, Source: skill.SourceCodex, VisibleTo: []skill.Agent{skill.AgentCodex}, ScanMode: skill.CodexRecursive}})
	if err != nil || len(got.Skills) != 2 {
		t.Fatalf("%+v %v", got, err)
	}
	for _, item := range got.Skills {
		loc := item.Locations[0]
		if loc.Names[skill.AgentCodex] != "rootns:"+item.ID || loc.PluginID != "" {
			t.Fatal(loc)
		}
	}
}
