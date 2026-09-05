package host_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scarb/skope/internal/host"
)

func projectionJunction(t *testing.T, target, name string) {
	t.Helper()
	output, err := exec.Command("cmd", "/c", "mklink", "/J", name, target).CombinedOutput()
	if err != nil {
		t.Skipf("junction fixture unavailable: %v: %s", err, output)
	}
}

func TestProjectionRootInspectsWindowsDiscoveryJunction(t *testing.T) {
	parent := t.TempDir()
	target := filepath.Join(parent, "target")
	writeProjectionFile(t, filepath.Join(target, "SKILL.md"), "skill")
	entry := filepath.Join(parent, "entry")
	projectionJunction(t, target, entry)
	m, reject, err := projectionInspector().Inspect(context.Background(), entry)
	if err != nil || reject != nil || len(m.Files) != 1 || m.Root != entry {
		t.Fatalf("manifest=%+v reject=%+v err=%v", m, reject, err)
	}
}

func TestProjectionRootRejectsCrossVolumeJunction(t *testing.T) {
	dir := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if strings.EqualFold(filepath.VolumeName(dir), filepath.VolumeName(cwd)) {
		t.Skip("fixture requires two accessible volumes")
	}
	target, err := os.MkdirTemp(cwd, "projection-volume-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(target); err != nil {
			t.Error(err)
		}
	})
	writeProjectionFile(t, filepath.Join(target, "secret"), "external")
	projectionJunction(t, target, filepath.Join(dir, "external"))
	r, err := host.OpenProjectionRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	_, contained, resolveErr := r.Resolve("external")
	if f, err := r.Open("external/secret"); err == nil {
		if closeErr := f.Close(); closeErr != nil {
			t.Fatal(closeErr)
		}
		t.Fatal("opened external junction content")
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if contained || resolveErr != nil {
		t.Fatalf("contained=%v err=%v", contained, resolveErr)
	}
	_, reject, err := projectionInspector().Inspect(context.Background(), dir)
	if err != nil || reject == nil || reject.Reason != "outside-root" {
		t.Fatalf("reject=%+v err=%v", reject, err)
	}
}

func TestProjectionRootExpandsWindowsInternalJunctions(t *testing.T) {
	dir := t.TempDir()
	references := filepath.Join(dir, "references")
	writeProjectionFile(t, filepath.Join(references, "note.md"), "note")
	projectionJunction(t, references, filepath.Join(dir, "alias"))
	projectionJunction(t, filepath.Join(dir, "alias"), filepath.Join(dir, "chain"))
	m, reject, err := projectionInspector().Inspect(context.Background(), dir)
	if err != nil || reject != nil || len(m.Files) != 3 || m.Bytes != 12 {
		t.Fatalf("manifest=%+v reject=%+v err=%v", m, reject, err)
	}
	for _, f := range m.Files {
		if f.Source != "references/note.md" {
			t.Fatalf("unresolved source=%q", f.Source)
		}
	}
}

func TestProjectionRootRejectsWindowsJunctionLoops(t *testing.T) {
	dir := t.TempDir()
	projectionJunction(t, dir, filepath.Join(dir, "loop"))
	_, reject, err := projectionInspector().Inspect(context.Background(), dir)
	if err != nil || reject == nil || reject.Reason != "symlink-loop" {
		t.Fatalf("reject=%+v err=%v", reject, err)
	}
}

func TestProjectionRootRejectsWindowsPluginManifestCaseVariant(t *testing.T) {
	dir := t.TempDir()
	writeProjectionFile(t, filepath.Join(dir, ".CLAUDE-PLUGIN", "PLUGIN.JSON"), "{}")
	_, reject, err := projectionInspector().Inspect(context.Background(), dir)
	if err != nil || reject == nil || reject.Reason != "plugin-manifest" {
		t.Fatalf("reject=%+v err=%v", reject, err)
	}
}
