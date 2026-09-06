package skill_test

import (
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/scarb/skope/internal/host"
	"github.com/scarb/skope/internal/skill"
)

func TestNativeScanRejectsSpecialBeforeReadAndPreservesRegularIOErrors(t *testing.T) {
	ioErr := errors.New("regular file IO failed")
	for _, step := range []string{"special without opener", "special with opener", "open", "read", "close", "success"} {
		t.Run(step, func(t *testing.T) {
			f := &foreignFS{mapFileSystem: newMapFS(fstest.MapFS{"skills/blocked/SKILL.md": file("body")})}
			want := ioErr
			scanner := skill.Scanner{FS: f, RegularFiles: f}
			switch step {
			case "special without opener":
				scanner.RegularFiles = nil
				f.files["skills/blocked/SKILL.md"].Mode = fs.ModeNamedPipe
				want = host.ErrNotRegular
			case "special with opener":
				f.files["skills/blocked/SKILL.md"].Mode = fs.ModeNamedPipe
				want = host.ErrNotRegular
			case "open":
				f.openErr = ioErr
			case "read":
				f.readErr = ioErr
			case "close":
				f.closeErr = ioErr
			case "success":
				want = nil
			}
			result, err := scanner.ScanRoots([]skill.Root{{Path: "/skills", Kind: skill.KindSkill, Source: skill.SourceClaude, Level: skill.LevelPlugin, PluginID: "blocked@m", PluginAgent: skill.AgentClaude, VisibleTo: []skill.Agent{skill.AgentClaude}}})
			if !errors.Is(err, want) {
				t.Fatalf("error=%v want=%v", err, want)
			}
			if step == "success" && len(result.Skills) != 1 {
				t.Fatalf("result=%+v", result)
			}
			if errors.Is(want, host.ErrNotRegular) && (f.opens != 0 || f.reads != 0) {
				t.Fatalf("special opened/read: %d/%d", f.opens, f.reads)
			}
			if (step == "read" || step == "close" || step == "success") && f.closes != 1 {
				t.Fatalf("closes=%d", f.closes)
			}
		})
	}
}
