package skill_test

import (
	"errors"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/scarb/skope/internal/host"
	"github.com/scarb/skope/internal/skill"
)

func TestCodexManifestAcceptsLargeUnrelatedFields(t *testing.T) {
	body := `{"name":"demo","skills":"custom","description":"` + strings.Repeat("x", 1<<20) + `"}`
	for _, regularOpener := range []bool{false, true} {
		f := &foreignFS{mapFileSystem: newMapFS(fstest.MapFS{"plugin/.codex-plugin/plugin.json": file(body)})}
		scanner := skill.Scanner{FS: f.mapFileSystem}
		if regularOpener {
			scanner = skill.Scanner{FS: f, RegularFiles: f}
		}
		got, err := scanner.ReadCodexManifest("/plugin")
		if err != nil || got.Name != "demo" || got.Skills != "custom" {
			t.Fatalf("regular opener=%v: manifest=%+v error=%v", regularOpener, got, err)
		}
		if regularOpener && (f.reads != len(body) || f.closes != 1) {
			t.Fatalf("read=%d close=%d", f.reads, f.closes)
		}
	}
}

func TestCodexManifestPreservesSafeReadsAndIOErrors(t *testing.T) {
	readErr, closeErr := errors.New("read failure"), errors.New("close failure")
	for _, tt := range []struct {
		name                       string
		mode                       fs.FileMode
		openErr, readErr, closeErr error
	}{
		{name: "special file", mode: fs.ModeNamedPipe},
		{name: "replaced before open", openErr: host.ErrNotRegular},
		{name: "joined read and close", readErr: readErr, closeErr: closeErr},
	} {
		f := &foreignFS{mapFileSystem: newMapFS(fstest.MapFS{"plugin/.codex-plugin/plugin.json": {Data: []byte(`{"name":"demo"}`), Mode: tt.mode}}), openErr: tt.openErr, readErr: tt.readErr, closeErr: tt.closeErr}
		_, err := (skill.Scanner{FS: f, RegularFiles: f}).ReadCodexManifest("/plugin")
		if err == nil || !strings.Contains(err.Error(), "plugin.json") {
			t.Fatalf("%s: %v", tt.name, err)
		}
		if tt.mode != 0 && (f.opens != 0 || f.reads != 0) {
			t.Fatal("opened a special file")
		}
		if tt.openErr != nil && !errors.Is(err, tt.openErr) {
			t.Fatalf("%s: %v", tt.name, err)
		}
		if tt.readErr != nil && (!errors.Is(err, readErr) || !errors.Is(err, closeErr) || f.closes != 1) {
			t.Fatalf("%s: %v", tt.name, err)
		}
	}
}

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
