package host_test

import (
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/scarb/skope/internal/host"
)

func TestOSFileSystemRecognizesAndResolvesJunction(t *testing.T) {
	parent := t.TempDir()
	target := filepath.Join(parent, "target")
	writeProjectionFile(t, filepath.Join(target, "SKILL.md"), "skill")
	projectionJunction(t, target, filepath.Join(parent, "alias"))
	fsys := host.OSFileSystem{}
	entries, err := fsys.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	if entries[0].Name() != "alias" || entries[0].Type()&fs.ModeSymlink == 0 || entries[0].IsDir() {
		t.Errorf("junction must be a link candidate: %v", entries[0])
	}
	want, err := fsys.EvalSymlinks(filepath.Join(target, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := fsys.EvalSymlinks(filepath.Join(parent, "alias", "SKILL.md"))
	if err != nil || got != want {
		t.Fatalf("resolved=%q err=%v", got, err)
	}
}
