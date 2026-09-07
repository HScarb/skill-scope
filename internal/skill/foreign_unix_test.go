//go:build !windows

package skill_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/scarb/skope/internal/host"
	"github.com/scarb/skope/internal/skill"
	"golang.org/x/sys/unix"
)

func TestScanForeignGlobalsFIFO(t *testing.T) {
	if os.Getenv("SKOPE_TEST_FOREIGN_FIFO_CHILD") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestScanForeignGlobalsFIFO$", "-test.v")
		cmd.Env = append(os.Environ(), "SKOPE_TEST_FOREIGN_FIFO_CHILD=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("FIFO helper: %v (context %v)\n%s", err, ctx.Err(), output)
		}
		return
	}
	home := t.TempDir()
	entry := filepath.Join(home, ".agents", "skills", "fifo")
	mustMkdirAll(t, entry)
	if err := unix.Mkfifo(filepath.Join(entry, "SKILL.md"), 0600); err != nil {
		t.Fatal(err)
	}
	fsys := host.OSFileSystem{}
	got, err := (skill.Scanner{FS: fsys, RegularFiles: fsys}).ScanForeignGlobals(host.NewEnv(home, home, nil), 8)
	if err != nil || len(got.Skills) != 1 || got.Skills[0].ID != "fifo" || len(got.Skills[0].Locations[0].Names) != 0 || len(got.Rejections) != 1 || got.Rejections[0].Reason != skill.ReasonSpecialFile {
		t.Fatalf("result=%#v error=%v", got, err)
	}
	plugin := filepath.Join(home, "plugin")
	mustMkdirAll(t, filepath.Join(plugin, ".codex-plugin"))
	if err := unix.Mkfifo(filepath.Join(plugin, ".codex-plugin", "plugin.json"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := (skill.Scanner{FS: fsys, RegularFiles: fsys}).ReadCodexManifest(plugin); !errors.Is(err, host.ErrNotRegular) {
		t.Fatalf("manifest FIFO error=%v", err)
	}
}
