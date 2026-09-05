package host_test

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"

	"github.com/scarb/skope/internal/host"
	"github.com/scarb/skope/internal/projection"
)

func TestProjectionRootReadsWithinOpenedDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("skill"), 0600); err != nil {
		t.Fatal(err)
	}
	r, err := host.OpenProjectionRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	})
	rel, contained, err := r.Resolve("SKILL.md")
	if err != nil || !contained || rel != "SKILL.md" {
		t.Fatalf("resolve = %q %v %v", rel, contained, err)
	}
	f, err := r.Open(rel)
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if string(b) != "skill" {
		t.Fatalf("body = %q", b)
	}
	if _, err := r.Open("../outside"); err == nil {
		t.Fatal("opened escape")
	}
}

func projectionInspector() projection.Inspector {
	return projection.Inspector{OpenRoot: func(directory string) (projection.Root, error) { return host.OpenProjectionRoot(directory) }}
}

func writeProjectionFile(t *testing.T, name, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}

func projectionLink(t *testing.T, target, name string) {
	t.Helper()
	if err := os.Symlink(target, name); err != nil {
		t.Skipf("symlink fixture unavailable: %v", err)
	}
}

func TestProjectionRootRemainsAnchoredAfterDirectoryReplacement(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "root")
	writeProjectionFile(t, filepath.Join(dir, "file"), "original")
	r, err := host.OpenProjectionRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := os.Rename(dir, filepath.Join(parent, "moved")); err != nil {
		t.Skipf("platform cannot rename opened root: %v", err)
	}
	writeProjectionFile(t, filepath.Join(dir, "file"), "replacement")
	f, err := r.Open("file")
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if string(b) != "original" {
		t.Fatalf("read unanchored content %q", b)
	}
}

func TestProjectionRootInspectsRealInternalLinksAndDiscoveryEntry(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "real")
	writeProjectionFile(t, filepath.Join(dir, "SKILL.md"), "skill")
	writeProjectionFile(t, filepath.Join(dir, "references", "note.md"), "note")
	writeProjectionFile(t, filepath.Join(dir, "scripts", "helper.sh"), "echo ok")
	if err := os.MkdirAll(filepath.Join(dir, "empty"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "links"), 0700); err != nil {
		t.Fatal(err)
	}
	projectionLink(t, "../references", filepath.Join(dir, "links", "inside"))
	projectionLink(t, "SKILL.md", filepath.Join(dir, "alias.md"))
	projectionLink(t, "alias.md", filepath.Join(dir, "chain.md"))
	entry := filepath.Join(parent, "discovered")
	projectionLink(t, dir, entry)
	before := snapshotProjection(t, dir)
	m, reject, err := projectionInspector().Inspect(context.Background(), entry)
	if err != nil || reject != nil {
		t.Fatalf("reject=%+v err=%v", reject, err)
	}
	if m.Root != entry || len(m.Files) != 6 || m.Bytes != 30 {
		t.Fatalf("manifest=%+v", m)
	}
	if !reflect.DeepEqual(before, snapshotProjection(t, dir)) {
		t.Fatal("inspection modified source tree")
	}
	other := filepath.Join(parent, "other")
	writeProjectionFile(t, filepath.Join(other, "different"), "other")
	if err := os.Remove(entry); err != nil {
		t.Fatal(err)
	}
	projectionLink(t, other, entry)
	again, reject, err := projectionInspector().Inspect(context.Background(), m.Root)
	if err != nil || reject != nil || len(again.Files) != 1 || again.Files[0].Path != "different" {
		t.Fatalf("reopened manifest=%+v reject=%+v err=%v", again, reject, err)
	}
}

func snapshotProjection(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		value := info.Mode().String() + info.ModTime().String()
		if info.Mode().IsRegular() {
			b, err := os.ReadFile(name)
			if err != nil {
				return err
			}
			value += string(b)
		}
		out[name] = value
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestProjectionRootRejectsRealExternalLinksAndLoops(t *testing.T) {
	for _, name := range []string{"parent", "prefix-sibling", "skill-file", "directory-loop", "file-loop", "broken"} {
		t.Run(name, func(t *testing.T) {
			parent := t.TempDir()
			dir := filepath.Join(parent, "skill")
			writeProjectionFile(t, filepath.Join(dir, "SKILL.md"), "skill")
			writeProjectionFile(t, filepath.Join(parent, "skill-other", "secret"), "external secret")
			target := ".."
			link := filepath.Join(dir, "link")
			reason := "outside-root"
			switch name {
			case "prefix-sibling":
				target = "../skill-other/secret"
			case "skill-file":
				target = "../skill-other/secret"
				link = filepath.Join(dir, "SKILL.md")
				if err := os.Remove(link); err != nil {
					t.Fatal(err)
				}
			case "directory-loop":
				target = "."
				reason = "symlink-loop"
			case "file-loop":
				target = "link"
				reason = "symlink-loop"
			case "broken":
				target = "missing"
				reason = ""
			}
			projectionLink(t, target, link)
			_, reject, err := projectionInspector().Inspect(context.Background(), dir)
			if reason == "" {
				if !errors.Is(err, fs.ErrNotExist) || reject != nil {
					t.Fatalf("reject=%+v err=%v", reject, err)
				}
			} else if err != nil || reject == nil || reject.Reason != reason {
				t.Fatalf("reject=%+v err=%v", reject, err)
			}
			if name == "file-loop" {
				r, err := host.OpenProjectionRoot(dir)
				if err != nil {
					t.Fatal(err)
				}
				_, _, resolveErr := r.Resolve("link")
				if err := r.Close(); err != nil {
					t.Fatal(err)
				}
				if !errors.Is(resolveErr, syscall.ELOOP) {
					t.Fatalf("unstructured loop error %v", resolveErr)
				}
			}
		})
	}
}
