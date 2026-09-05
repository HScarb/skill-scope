package host_test

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/scarb/skope/internal/host"
)

func TestOpenRegular(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	name := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(name, []byte("skill"), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := (host.OSFileSystem{}).OpenRegular(name)
	if err != nil {
		t.Fatal(err)
	}
	contents, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if string(contents) != "skill" {
		t.Fatalf("contents = %q", contents)
	}
	if f, err := (host.OSFileSystem{}).OpenRegular(dir); f != nil || !errors.Is(err, host.ErrNotRegular) {
		t.Fatalf("directory = %v, %v", f, err)
	}
	if f, err := (host.OSFileSystem{}).OpenRegular(filepath.Join(dir, "missing")); f != nil || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing = %v, %v", f, err)
	}
	if runtime.GOOS == "windows" {
		if f, err := (host.OSFileSystem{}).OpenRegular("NUL"); f != nil || !errors.Is(err, host.ErrNotRegular) {
			t.Fatalf("device = %v, %v", f, err)
		}
	}
}
