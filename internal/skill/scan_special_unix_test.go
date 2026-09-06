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

func TestScanPluginSpecialEntryDoesNotBlock(t *testing.T) {
	if os.Getenv("SKOPE_TEST_PLUGIN_FIFO") == "1" {
		root := t.TempDir()
		dir := filepath.Join(root, "blocked-plugin")
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := unix.Mkfifo(filepath.Join(dir, "SKILL.md"), 0o600); err != nil {
			t.Fatal(err)
		}
		fsys := host.OSFileSystem{}
		scanner := skill.Scanner{FS: fsys, RegularFiles: fsys}
		_, err := scanner.ScanRoots([]skill.Root{{Path: root, Kind: skill.KindSkill, Level: skill.LevelPlugin, Source: skill.SourceClaude, PluginID: "blocked@m", PluginAgent: skill.AgentClaude, VisibleTo: []skill.Agent{skill.AgentClaude}}})
		if !errors.Is(err, host.ErrNotRegular) {
			t.Fatalf("err=%v", err)
		}
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestScanPluginSpecialEntryDoesNotBlock$", "-test.count=1")
	cmd.Env = append(os.Environ(), "SKOPE_TEST_PLUGIN_FIFO=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("plugin scan blocked or failed: %v ctx=%v output=%s", err, ctx.Err(), output)
	}
}
