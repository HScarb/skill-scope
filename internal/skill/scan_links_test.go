package skill_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/scarb/skope/internal/host"
	"github.com/scarb/skope/internal/skill"
)

func TestScannersRecognizeDiscoveryDirectoryLinksAndSkipCommandLinks(t *testing.T) {
	for _, broken := range []bool{false, true} {
		t.Run(map[bool]string{false: "valid", true: "broken"}[broken], func(t *testing.T) {
			root := t.TempDir()
			target := filepath.Join(root, "target")
			if err := os.MkdirAll(target, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(target, "SKILL.md"), []byte("---\nname: frontmatter\n---\nbody"), 0o600); err != nil {
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
