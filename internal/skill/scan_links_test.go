package skill_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/scarb/skope/internal/host"
	"github.com/scarb/skope/internal/skill"
)

func TestScanCodexDirectoryAliasesAndCycle(t *testing.T) {
	if os.Getenv("SKOPE_TEST_CODEX_LINK_CHILD") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestScanCodexDirectoryAliasesAndCycle$", "-test.v")
		cmd.Env = append(os.Environ(), "SKOPE_TEST_CODEX_LINK_CHILD=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("link helper: %v (context %v)\n%s", err, ctx.Err(), output)
		}
		return
	}
	temp := t.TempDir()
	root := filepath.Join(temp, "skills")
	target := filepath.Join(temp, "target")
	mustMkdirAll(t, root)
	mustMkdirAll(t, filepath.Join(target, "nested"))
	for _, dir := range []string{target, filepath.Join(target, "nested")} {
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\ndescription: valid\n---\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	link := func(target, name string) {
		if runtime.GOOS == "windows" {
			if out, err := exec.Command("cmd", "/c", "mklink", "/J", name, target).CombinedOutput(); err != nil {
				t.Fatalf("junction: %v %s", err, out)
			}
		} else if err := os.Symlink(target, name); err != nil {
			t.Fatal(err)
		}
	}
	link(target, filepath.Join(root, "a"))
	link(target, filepath.Join(root, "b"))
	link(target, filepath.Join(target, "loop"))
	scanner := skill.Scanner{FS: host.OSFileSystem{}, RegularFiles: host.OSFileSystem{}}
	roots := []skill.Root{{Path: filepath.ToSlash(root), Kind: skill.KindSkill, Source: skill.SourceCodex, VisibleTo: []skill.Agent{skill.AgentCodex}, ScanMode: skill.CodexRecursive}}
	for _, foreign := range []bool{false, true} {
		for run := 0; run < 2; run++ {
			var got skill.ScanResult
			var err error
			if foreign {
				got, err = scanner.ScanForeignRoots(roots, 100)
			} else {
				got, err = scanner.ScanRoots(roots)
			}
			if err != nil {
				t.Fatal(err)
			}
			paths := map[string]bool{}
			for _, item := range got.Skills {
				for _, loc := range item.Locations {
					paths[loc.DiscoveryPath] = true
				}
			}
			if len(paths) != 4 {
				t.Fatalf("foreign=%v run=%d paths=%v", foreign, run, paths)
			}
			for _, suffix := range []string{"a/SKILL.md", "b/SKILL.md", "a/nested/SKILL.md", "b/nested/SKILL.md"} {
				if !paths[filepath.ToSlash(filepath.Join(root, filepath.FromSlash(suffix)))] {
					t.Fatalf("missing %s: %v", suffix, paths)
				}
			}
		}
	}
	// Remove only the link; the target remains available for the broken-link check.
	if err := os.Remove(filepath.Join(target, "loop")); err != nil {
		t.Fatal(err)
	}
	link(filepath.Join(temp, "missing"), filepath.Join(root, "broken"))
	for _, foreign := range []bool{false, true} {
		var err error
		if foreign {
			_, err = scanner.ScanForeignRoots(roots, 100)
		} else {
			_, err = scanner.ScanRoots(roots)
		}
		if err == nil {
			t.Fatalf("accepted broken directory foreign=%v", foreign)
		}
	}
}

func TestScannersRecognizeDiscoveryDirectoryLinksAndSkipCommandLinks(t *testing.T) {
	for _, broken := range []bool{false, true} {
		t.Run(map[bool]string{false: "valid", true: "broken"}[broken], func(t *testing.T) {
			root := t.TempDir()
			target := filepath.Join(root, "target")
			if err := os.MkdirAll(target, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(target, "SKILL.md"), []byte("---\nname: frontmatter\ndescription: valid\n---\nbody"), 0o600); err != nil {
				t.Fatal(err)
			}
			for _, relative := range []string{".agents/skills/foreign-alias", ".claude/skills/native-alias", ".claude/commands/command-alias"} {
				entry := filepath.Join(root, filepath.FromSlash(relative))
				if err := os.MkdirAll(filepath.Dir(entry), 0o700); err != nil {
					t.Fatal(err)
				}
				if runtime.GOOS == "windows" {
					if out, err := exec.Command("cmd", "/c", "mklink", "/J", entry, target).CombinedOutput(); err != nil {
						t.Fatalf("junction: %v %s", err, out)
					}
				} else if err := os.Symlink(target, entry); err != nil {
					t.Fatal(err)
				}
			}
			if broken {
				if err := os.Remove(filepath.Join(target, "SKILL.md")); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(target); err != nil {
					t.Fatal(err)
				}
			}
			fsys := host.OSFileSystem{}
			scanner := skill.Scanner{FS: fsys, RegularFiles: fsys}
			env := host.NewEnv(root, root, map[string]string{"CODEX_HOME": filepath.Join(root, "codex")})
			for _, foreign := range []bool{false, true} {
				var result skill.ScanResult
				var err error
				if foreign {
					result, err = scanner.ScanForeignGlobals(env, 20<<20)
				} else {
					result, err = scanner.ScanClaude(env)
				}
				if broken {
					if err == nil {
						t.Fatalf("broken link accepted foreign=%v: %+v", foreign, result)
					}
					continue
				}
				if err != nil || len(result.Skills) != 1 {
					t.Fatalf("foreign=%v scan=%+v err=%v", foreign, result, err)
				}
				loc := result.Skills[0].Locations[0]
				wantID := "native-alias"
				if foreign {
					wantID = "foreign-alias"
				}
				if result.Skills[0].ID != wantID || (!foreign && loc.Names[skill.AgentClaude] != wantID) || (foreign && loc.Names[skill.AgentCodex] != "frontmatter") {
					t.Fatalf("skill=%+v loc=%+v", result.Skills[0], loc)
				}
				want, err := fsys.EvalSymlinks(filepath.Join(target, "SKILL.md"))
				if err != nil || loc.RealPath != want {
					t.Fatalf("real=%s want=%s err=%v", loc.RealPath, want, err)
				}
			}
		})
	}
}
