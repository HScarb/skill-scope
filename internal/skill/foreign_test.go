package skill_test

import (
	"errors"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/scarb/skope/internal/host"
	"github.com/scarb/skope/internal/skill"
)

type foreignFS struct {
	*mapFileSystem
	opens, reads, closes                      int
	openErr, readErr, closeErr, handleStatErr error
	handleInfo                                fs.FileInfo
	statInfo                                  fs.FileInfo
	readErrAfterBytes                         bool
}

func (f *foreignFS) ReadFile(string) ([]byte, error) {
	panic("foreign must not call unbounded ReadFile")
}
func (f *foreignFS) Stat(name string) (fs.FileInfo, error) {
	if f.statInfo != nil {
		return f.statInfo, nil
	}
	return f.mapFileSystem.Stat(name)
}
func (f *foreignFS) OpenRegular(name string) (fs.File, error) {
	f.opens++
	if f.openErr != nil {
		return nil, f.openErr
	}
	file, err := f.files.Open(mapKey(name))
	if err != nil {
		return nil, err
	}
	return &foreignFile{File: file, owner: f}, nil
}

type foreignFile struct {
	fs.File
	owner *foreignFS
}

func (f *foreignFile) Read(p []byte) (int, error) {
	if f.owner.readErr != nil && !f.owner.readErrAfterBytes {
		return 0, f.owner.readErr
	}
	n, err := f.File.Read(p)
	f.owner.reads += n
	if f.owner.readErrAfterBytes {
		return n, f.owner.readErr
	}
	return n, err
}
func (f *foreignFile) Stat() (fs.FileInfo, error) {
	if f.owner.handleStatErr != nil {
		return nil, f.owner.handleStatErr
	}
	if f.owner.handleInfo != nil {
		return f.owner.handleInfo, nil
	}
	return f.File.Stat()
}
func (f *foreignFile) Close() error {
	f.owner.closes++
	return errors.Join(f.File.Close(), f.owner.closeErr)
}

func TestScanForeignGlobalsRootsAndNames(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ name, override, codex string }{
		{"default", "", "default"}, {"blank", " \t ", "default"}, {"trim", "  /custom  ", "custom"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := &foreignFS{mapFileSystem: newMapFS(fstest.MapFS{
				"home/.agents/skills/agents/SKILL.md":                     file("---\nname: effective\n---\n"),
				"home/.agents/skills/bare/SKILL.md":                       file("# body"),
				"home/.agents/skills/manifest/SKILL.md":                   file("---\ndescription: body\n---\n"),
				"home/.agents/skills/manifest/.claude-plugin/plugin.json": file("{}"),
				"home/.agents/skills/empty/README.md":                     file("no skill"),
				"home/.codex/skills/default/SKILL.md":                     file("# body"),
				"custom/skills/custom/SKILL.md":                           file("# body"),
				"home/.codex/plugins/ignored/SKILL.md":                    file("# body"),
				"repo/.agents/skills/ignored/SKILL.md":                    file("# body"),
				"home/.claude/skills/ignored/SKILL.md":                    file("# body"),
			})}
			got, err := (skill.Scanner{FS: f, RegularFiles: f}).ScanForeignGlobals(host.NewEnv("/home", "/repo", map[string]string{"CODEX_HOME": tt.override}), 1000)
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Skills) != 4 || len(got.Rejections) != 0 {
				t.Fatalf("result = %#v", got)
			}
			for _, candidate := range got.Skills {
				loc := candidate.Locations[0]
				want := map[skill.Agent]string{skill.AgentCodex: candidate.ID, skill.AgentOpenCode: candidate.ID}
				if candidate.ID == "agents" {
					want = map[skill.Agent]string{skill.AgentCodex: "effective", skill.AgentOpenCode: "effective"}
				}
				if candidate.ID == tt.codex {
					delete(want, skill.AgentOpenCode)
					if loc.Source != skill.SourceCodex {
						t.Fatal(loc)
					}
				} else if loc.Source != skill.SourceAgents {
					t.Fatal(loc)
				}
				if loc.Level != skill.LevelGlobal || loc.Kind != skill.KindSkill || !reflect.DeepEqual(loc.Names, want) {
					t.Fatalf("location = %#v want names %#v", loc, want)
				}
			}
			if f.opens != 4 || f.closes != 4 {
				t.Fatalf("opens/closes = %d/%d", f.opens, f.closes)
			}
		})
	}
}

func TestScanForeignGlobalsBoundaries(t *testing.T) {
	t.Parallel()
	const name = "/home/.agents/skills/example/SKILL.md"
	for _, tt := range []struct {
		name                 string
		mode                 fs.FileMode
		data                 string
		info                 fs.FileInfo
		reason               skill.ResolutionReason
		wantReads, wantOpens int
	}{
		{name: "exact limit", data: "12345678", wantReads: 8, wantOpens: 1},
		{name: "known oversize", data: "123456789", reason: skill.ReasonLimitExceeded},
		{name: "fifo", mode: fs.ModeNamedPipe, reason: skill.ReasonSpecialFile},
		{name: "socket", mode: fs.ModeSocket, reason: skill.ReasonSpecialFile},
		{name: "device", mode: fs.ModeDevice, reason: skill.ReasonSpecialFile},
		{name: "directory", mode: fs.ModeDir, reason: skill.ReasonSpecialFile},
		{name: "changed handle type", data: "x", info: mapFileInfo{mode: fs.ModeNamedPipe}, reason: skill.ReasonSpecialFile, wantOpens: 1},
		{name: "changed handle size", data: "x", info: mapFileInfo{size: 9}, reason: skill.ReasonLimitExceeded, wantOpens: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := &foreignFS{mapFileSystem: newMapFS(fstest.MapFS{mapKey(name): {Data: []byte(tt.data), Mode: tt.mode}}), handleInfo: tt.info}
			got, err := (skill.Scanner{FS: f, RegularFiles: f}).ScanForeignGlobals(host.NewEnv("/home", "/repo", nil), 8)
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Skills) != 1 || got.Skills[0].ID != "example" {
				t.Fatalf("skills = %#v", got.Skills)
			}
			if tt.reason != "" {
				want := []skill.ScanRejection{{Source: skill.SourceAgents, DiscoveryPath: name, Reason: tt.reason}}
				if !reflect.DeepEqual(got.Rejections, want) || len(got.Skills[0].Locations[0].Names) != 0 {
					t.Fatalf("rejection = %#v, skills = %#v", got.Rejections, got.Skills)
				}
			} else if len(got.Rejections) != 0 {
				t.Fatal(got.Rejections)
			}
			if f.reads != tt.wantReads || f.opens != tt.wantOpens || f.closes != f.opens {
				t.Fatalf("reads/opens/closes = %d/%d/%d", f.reads, f.opens, f.closes)
			}
		})
	}
}

func TestScanForeignGlobalsErrors(t *testing.T) {
	t.Parallel()
	const name = "/home/.agents/skills/example/SKILL.md"
	boom := errors.New("io failure")
	for _, tt := range []struct {
		name      string
		configure func(*foreignFS)
		want      error
	}{
		{"open", func(f *foreignFS) { f.openErr = boom }, boom},
		{"joined opener rejection and close failure", func(f *foreignFS) { f.openErr = errors.Join(host.ErrNotRegular, boom) }, boom},
		{"read", func(f *foreignFS) { f.readErr = boom }, boom},
		{"close", func(f *foreignFS) { f.closeErr = boom }, boom},
		{"handle stat", func(f *foreignFS) { f.handleStatErr = boom }, boom},
		{"close after special", func(f *foreignFS) { f.handleInfo = mapFileInfo{mode: fs.ModeNamedPipe}; f.closeErr = boom }, boom},
		{"close after oversize", func(f *foreignFS) { f.handleInfo = mapFileInfo{size: 1001}; f.closeErr = boom }, boom},
		{"read and close", func(f *foreignFS) { f.readErr = boom; f.closeErr = boom }, boom},
		{"directory permission", func(f *foreignFS) { f.errors["readDir:/home/.agents/skills"] = fs.ErrPermission }, fs.ErrPermission},
		{"stat", func(f *foreignFS) { f.errors["stat:"+name] = boom }, boom},
		{"special stat failure", func(f *foreignFS) {
			f.files[mapKey(name)] = &fstest.MapFile{Mode: fs.ModeNamedPipe}
			f.errors["stat:"+name] = boom
		}, boom},
		{"lstat", func(f *foreignFS) { f.errors["lstat:"+name] = boom }, boom},
		{"bad yaml", func(f *foreignFS) { f.files[mapKey(name)] = file("---\nname: [\n---\n") }, nil},
		{"broken link", func(f *foreignFS) { f.files[mapKey(name)] = symlink("missing") }, fs.ErrNotExist},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := &foreignFS{mapFileSystem: newMapFS(fstest.MapFS{mapKey(name): file("# body")})}
			tt.configure(f)
			_, err := (skill.Scanner{FS: f, RegularFiles: f}).ScanForeignGlobals(host.NewEnv("/home", "/repo", nil), 1000)
			if err == nil || (tt.want != nil && !errors.Is(err, tt.want)) || !strings.Contains(err.Error(), "/home/.agents/skills") {
				t.Fatalf("error = %v", err)
			}
			if f.opens > 0 && f.openErr == nil && f.closes != 1 {
				t.Fatalf("closes=%d", f.closes)
			}
		})
	}
}

func TestScanForeignGlobalsLimitsActualReads(t *testing.T) {
	t.Parallel()
	const name = "/home/.agents/skills/example/SKILL.md"
	for _, tt := range []struct {
		name, data string
		limit      int64
		wantReason skill.ResolutionReason
		wantName   string
		wantReads  int
	}{
		{"growing file", strings.Repeat("x", 100), 8, skill.ReasonLimitExceeded, "", 9},
		{"oversize invalid yaml", "---\nname: [\n---\n", 8, skill.ReasonLimitExceeded, "", 9},
		{"exact valid yaml", "---\nname: ok\n---\n", 17, "", "ok", 17},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := &foreignFS{mapFileSystem: newMapFS(fstest.MapFS{mapKey(name): file(tt.data)}), statInfo: mapFileInfo{size: 1}, handleInfo: mapFileInfo{size: 1}}
			got, err := (skill.Scanner{FS: f, RegularFiles: f}).ScanForeignGlobals(host.NewEnv("/home", "/repo", nil), tt.limit)
			if err != nil {
				t.Fatal(err)
			}
			if f.reads != tt.wantReads || f.closes != 1 {
				t.Fatalf("reads/closes = %d/%d", f.reads, f.closes)
			}
			if tt.wantReason != "" {
				if len(got.Rejections) != 1 || got.Rejections[0].Reason != tt.wantReason || len(got.Skills[0].Locations[0].Names) != 0 {
					t.Fatalf("result=%#v", got)
				}
			} else if got.Skills[0].Locations[0].Names[skill.AgentCodex] != tt.wantName {
				t.Fatal(got)
			}
		})
	}
}

func TestScanForeignGlobalsSpecialSymlinkAndOpenRejection(t *testing.T) {
	t.Parallel()
	const name = "/home/.agents/skills/example/SKILL.md"
	for _, tt := range []struct {
		name      string
		configure func(*foreignFS)
		opens     int
	}{
		{"symlink to fifo", func(f *foreignFS) {
			f.files[mapKey(name)] = symlink("/fifo")
			f.files["fifo"] = &fstest.MapFile{Mode: fs.ModeNamedPipe}
			f.realPaths[name] = "/fifo"
		}, 0},
		{"opener rejects race", func(f *foreignFS) { f.openErr = &fs.PathError{Op: "open", Path: name, Err: host.ErrNotRegular} }, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := &foreignFS{mapFileSystem: newMapFS(fstest.MapFS{mapKey(name): file("body")})}
			tt.configure(f)
			got, err := (skill.Scanner{FS: f, RegularFiles: f}).ScanForeignGlobals(host.NewEnv("/home", "/repo", nil), 100)
			if err != nil || len(got.Rejections) != 1 || got.Rejections[0].Reason != skill.ReasonSpecialFile || len(got.Skills[0].Locations[0].Names) != 0 {
				t.Fatalf("result=%#v error=%v", got, err)
			}
			if f.reads != 0 || f.opens != tt.opens {
				t.Fatalf("reads/opens=%d/%d", f.reads, f.opens)
			}
		})
	}
}

func TestScanForeignGlobalsRequiresLimitsAndOpener(t *testing.T) {
	t.Parallel()
	f := &foreignFS{mapFileSystem: newMapFS(fstest.MapFS{})}
	for _, limit := range []int64{-1, 0} {
		if _, err := (skill.Scanner{FS: f, RegularFiles: f}).ScanForeignGlobals(host.NewEnv("/home", "/repo", nil), limit); err == nil {
			t.Fatalf("limit %d accepted", limit)
		}
	}
	if _, err := (skill.Scanner{FS: f}).ScanForeignGlobals(host.NewEnv("/home", "/repo", nil), 10); err == nil {
		t.Fatal("nil opener accepted")
	}
	if got, err := (skill.Scanner{FS: f, RegularFiles: f}).ScanForeignGlobals(host.NewEnv("/home", "/repo", nil), 10); err != nil || len(got.Skills) != 0 {
		t.Fatalf("missing roots = %#v, %v", got, err)
	}
	if _, err := (skill.Scanner{FS: f, RegularFiles: f}).ScanForeignGlobals(host.NewEnv("/home", "/repo", nil), math.MaxInt64); err != nil {
		t.Fatalf("positive MaxInt64 rejected: %v", err)
	}
}

func TestScanForeignGlobalsPreservesReadAndCloseErrorsAtLimit(t *testing.T) {
	t.Parallel()
	readErr, closeErr := errors.New("read failed"), errors.New("close failed")
	const name = "/home/.agents/skills/example/SKILL.md"
	f := &foreignFS{mapFileSystem: newMapFS(fstest.MapFS{mapKey(name): file(strings.Repeat("x", 100))}), statInfo: mapFileInfo{size: 1}, handleInfo: mapFileInfo{size: 1}, readErr: readErr, closeErr: closeErr, readErrAfterBytes: true}
	_, err := (skill.Scanner{FS: f, RegularFiles: f}).ScanForeignGlobals(host.NewEnv("/home", "/repo", nil), 8)
	if !errors.Is(err, readErr) || !errors.Is(err, closeErr) || !strings.Contains(err.Error(), name) || f.closes != 1 || f.reads != 9 {
		t.Fatalf("error=%v closes=%d reads=%d", err, f.closes, f.reads)
	}
}

func TestScanForeignGlobalsPreservesLinkedDiscoveryPaths(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	root := filepath.Join(home, ".agents", "skills")
	target := filepath.Join(home, "actual")
	mustMkdirAll(t, root)
	mustMkdirAll(t, target)
	mustWriteFile(t, filepath.Join(target, "SKILL.md"), "---\nname: common\n---\n")
	mustSymlink(t, target, filepath.Join(root, "first"))
	mustSymlink(t, target, filepath.Join(root, "second"))
	fsys := host.OSFileSystem{}
	wantFile, err := os.Stat(filepath.Join(target, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := (skill.Scanner{FS: fsys, RegularFiles: fsys}).ScanForeignGlobals(host.NewEnv(home, home, map[string]string{"CODEX_HOME": filepath.Join(home, ".agents")}), 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Skills) != 2 {
		t.Fatalf("skills=%#v", got.Skills)
	}
	for _, candidate := range got.Skills {
		if len(candidate.Locations) != 2 {
			t.Fatalf("locations=%#v", candidate.Locations)
		}
		for _, loc := range candidate.Locations {
			if loc.DiscoveryPath != filepath.ToSlash(filepath.Join(root, candidate.ID, "SKILL.md")) {
				t.Fatalf("location=%#v", loc)
			}
			gotFile, err := os.Stat(filepath.FromSlash(loc.RealPath))
			if err != nil || !os.SameFile(wantFile, gotFile) {
				t.Fatalf("resolved file differs: location=%#v error=%v", loc, err)
			}
		}
	}
	if len(got.Collisions) == 0 {
		t.Fatal("effective name collisions missing")
	}
	if err := os.Remove(filepath.Join(target, "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := (skill.Scanner{FS: fsys, RegularFiles: fsys}).ScanForeignGlobals(host.NewEnv(home, home, nil), 100); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("broken entry link error=%v", err)
	}
}
