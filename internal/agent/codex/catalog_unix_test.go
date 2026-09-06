//go:build !windows

package codex_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestCatalogBrokenSkillsAncestorFails(t *testing.T) {
	c, e, p := setup(t)
	write(t, filepath.Join(p.CodexHome, "config.toml"), "[plugins.\"a@m\"]")
	root := plugin(t, p, "a@m", "1.0.0", `{"name":"a","skills":"custom/deep"}`)
	if err := os.Symlink(filepath.Join(root, "gone"), filepath.Join(root, "custom")); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Read(context.Background(), e, p); err == nil {
		t.Fatal("broken ancestor treated as no skills")
	}
}
func TestCatalogSkillsEscapeFails(t *testing.T) {
	c, e, p := setup(t)
	write(t, filepath.Join(p.CodexHome, "config.toml"), "[plugins.\"a@m\"]")
	root := plugin(t, p, "a@m", "1.0.0", `{"name":"a"}`)
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "skills")); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Read(context.Background(), e, p); err == nil {
		t.Fatal("escape accepted")
	}
}
func TestCatalogSpecialManifestFails(t *testing.T) {
	c, e, p := setup(t)
	write(t, filepath.Join(p.CodexHome, "config.toml"), "[plugins.\"a@m\"]")
	root := plugin(t, p, "a@m", "1.0.0", `{"name":"a"}`)
	manifest := filepath.Join(root, ".codex-plugin", "plugin.json")
	if err := os.Remove(manifest); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(manifest, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Read(context.Background(), e, p); err == nil {
		t.Fatal("special manifest accepted")
	}
}
func TestCatalogMissingRootThroughEscapeFails(t *testing.T) {
	c, e, p := setup(t)
	write(t, filepath.Join(p.CodexHome, "config.toml"), "[plugins.\"a@m\"]")
	root := plugin(t, p, "a@m", "1.0.0", `{"name":"a","skills":"custom/missing"}`)
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "custom")); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Read(context.Background(), e, p); err == nil {
		t.Fatal("absent skills path through escaping link accepted")
	}
}
func TestCatalogFIFOsFailWithoutOpening(t *testing.T) {
	for _, kind := range []string{"config", "manifest"} {
		t.Run(kind, func(t *testing.T) {
			c, e, p := setup(t)
			path := filepath.Join(p.CodexHome, "config.toml")
			write(t, path, "[plugins.\"a@m\"]")
			if kind == "manifest" {
				root := plugin(t, p, "a@m", "1.0.0", `{"name":"a"}`)
				path = filepath.Join(root, ".codex-plugin", "plugin.json")
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := unix.Mkfifo(path, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := c.Read(context.Background(), e, p); err == nil {
				t.Fatal("FIFO accepted")
			}
		})
	}
}
func TestCatalogIgnoresVersionSymlink(t *testing.T) {
	c, e, p := setup(t)
	write(t, filepath.Join(p.CodexHome, "config.toml"), "[plugins.\"a@m\"]")
	root := plugin(t, p, "a@m", "1.0.0", `{"name":"a"}`)
	if err := os.Symlink(t.TempDir(), filepath.Join(filepath.Dir(root), "local")); err != nil {
		t.Fatal(err)
	}
	s, err := c.Read(context.Background(), e, p)
	if err != nil || len(s.InstalledIDs) != 1 {
		t.Fatalf("%+v %v", s, err)
	}
}
