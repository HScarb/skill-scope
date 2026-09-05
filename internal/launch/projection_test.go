package launch

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/scarb/skope/internal/host"
	"github.com/scarb/skope/internal/projection"
	"github.com/scarb/skope/internal/session"
	"github.com/scarb/skope/internal/skill"
)

type projectionProcesses struct{}

func (projectionProcesses) StartToken(int) (string, error) { return "test-process", nil }
func projectionTestManager(t *testing.T) *session.Manager {
	t.Helper()
	m := session.NewManager(t.TempDir())
	m.Processes = projectionProcesses{}
	return m
}

func projectionDirectoryLink(t *testing.T, target, link string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
			t.Fatalf("junction: %v: %s", err, out)
		}
		return
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

func TestCopyProjectionStagesCompleteRealTreeAndExpandsLinks(t *testing.T) {
	source := t.TempDir()
	files := map[string][]byte{"SKILL.md": []byte("skill"), "refs/note.md": []byte("note"), "scripts/run.sh": []byte("echo ok"), "assets/data.bin": {0, 255, 1}}
	for name, data := range files {
		p := filepath.Join(source, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(source, "empty"), 0o700); err != nil {
		t.Fatal(err)
	}
	projectionDirectoryLink(t, filepath.Join(source, "refs"), filepath.Join(source, "alias"))
	if err := os.Symlink(filepath.Join(source, "SKILL.md"), filepath.Join(source, "alias-file.md")); err == nil {
		files["alias-file.md"] = []byte("skill")
	} else if runtime.GOOS != "windows" {
		t.Fatal(err)
	} else {
		t.Logf("file symlink unavailable; directory junction remains covered: %v", err)
	}
	open := func(dir string) (projection.Root, error) { return host.OpenProjectionRoot(dir) }
	m, reject, err := (projection.Inspector{OpenRoot: open}).Inspect(context.Background(), source)
	if err != nil || reject != nil {
		t.Fatalf("inspect=%v %v", reject, err)
	}
	manager := projectionTestManager(t)
	sess, err := manager.Stage(skill.AgentClaude, "copy")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.Abort() })
	sink, err := newProjectionSink(sess, manager, "one")
	if err != nil {
		t.Fatal(err)
	}
	if err := (projection.Copier{OpenRoot: open}).Copy(context.Background(), m, sink); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sess.Root); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("final exists before publish: %v", err)
	}
	if err := sess.Publish(); err != nil {
		t.Fatal(err)
	}
	root := sess.AgentPath("addDir", ".claude", "skills", "one")
	files["alias/note.md"] = []byte("note")
	for name, want := range files {
		got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("file %s=%v error=%v", name, got, err)
		}
	}
	if info, err := os.Stat(filepath.Join(root, "empty")); err != nil || !info.IsDir() {
		t.Fatalf("empty directory=%v error=%v", info, err)
	}
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			t.Fatalf("link escaped into output: %s", path)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if runtime.GOOS != "windows" {
			want := fs.FileMode(0o600)
			if entry.IsDir() {
				want = 0o700
			}
			if info.Mode().Perm() != want {
				t.Fatalf("mode %s=%o want=%o", path, info.Mode().Perm(), want)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestCopyProjectionRejectsRetargetedDiscoveryAndInternalLinks(t *testing.T) {
	for _, kind := range []string{"discovery", "internal"} {
		t.Run(kind, func(t *testing.T) {
			base := t.TempDir()
			first, second := filepath.Join(base, "first"), filepath.Join(base, "second")
			for _, dir := range []string{first, second} {
				if err := os.Mkdir(dir, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(dir), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			link := filepath.Join(base, "entry")
			source := link
			if kind == "internal" {
				source = base
				link = filepath.Join(base, "alias")
			}
			projectionDirectoryLink(t, first, link)
			open := func(dir string) (projection.Root, error) { return host.OpenProjectionRoot(dir) }
			m, reject, err := (projection.Inspector{OpenRoot: open}).Inspect(context.Background(), source)
			if err != nil || reject != nil {
				t.Fatalf("inspect=%v %v", reject, err)
			}
			if err := os.Remove(link); err != nil {
				t.Fatal(err)
			}
			projectionDirectoryLink(t, second, link)
			manager := projectionTestManager(t)
			sess, err := manager.Stage(skill.AgentClaude, "copy")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = sess.Abort() })
			sink, err := newProjectionSink(sess, manager, "one")
			if err != nil {
				t.Fatal(err)
			}
			if err := (projection.Copier{OpenRoot: open}).Copy(context.Background(), m, sink); err == nil {
				t.Fatal("retargeted source accepted")
			}
			if _, err := os.Stat(sess.Root); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("failed copy published: %v", err)
			}
			if err := sess.Abort(); err != nil {
				t.Fatal(err)
			}
			entries, err := os.ReadDir(filepath.Join(manager.Home, "sessions"))
			if err != nil || len(entries) != 0 {
				t.Fatalf("abort entries=%v error=%v", entries, err)
			}
		})
	}
}

func TestProjectionWriteRejectsUnexpectedAliasWithoutPublishing(t *testing.T) {
	manager := projectionTestManager(t)
	sess, err := manager.Stage(skill.AgentClaude, "copy")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.Abort() })
	sink, err := newProjectionSink(sess, manager, "one")
	if err != nil {
		t.Fatal(err)
	}
	if err := sink.WriteFile("Foo/SKILL.md", []byte("first")); err != nil {
		t.Fatal(err)
	}
	probe := filepath.Join(manager.Home, "CaseProbe")
	if err := os.WriteFile(probe, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	_, probeErr := os.Stat(filepath.Join(manager.Home, "caseprobe"))
	if probeErr != nil && !errors.Is(probeErr, fs.ErrNotExist) {
		t.Fatal(probeErr)
	}
	err = sink.WriteFile("foo/SKILL.md", []byte("second"))
	if probeErr == nil {
		if !errors.Is(err, fs.ErrExist) {
			t.Fatalf("alias error=%v", err)
		}
	} else if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sess.Root); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("final exists: %v", err)
	}
	if err := sess.Abort(); err != nil {
		t.Fatal(err)
	}
}

func TestCopyProjectionFailsOnExistingDestinationWithoutPublishing(t *testing.T) {
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("checked source"), 0o600); err != nil {
		t.Fatal(err)
	}
	open := func(dir string) (projection.Root, error) { return host.OpenProjectionRoot(dir) }
	m, reject, err := (projection.Inspector{OpenRoot: open}).Inspect(context.Background(), source)
	if err != nil || reject != nil {
		t.Fatalf("inspect=%v %v", reject, err)
	}
	manager := projectionTestManager(t)
	sess, err := manager.Stage(skill.AgentClaude, "copy")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.Abort() })
	sink, err := newProjectionSink(sess, manager, "one")
	if err != nil {
		t.Fatal(err)
	}
	if err := sink.WriteFile("SKILL.md", []byte("keep first")); err != nil {
		t.Fatal(err)
	}
	if err := (projection.Copier{OpenRoot: open}).Copy(context.Background(), m, sink); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("copy duplicate=%v", err)
	}
	if _, err := os.Stat(sess.Root); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("copy published: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(manager.Home, "sessions"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("staging entries=%v error=%v", entries, err)
	}
	data, err := os.ReadFile(filepath.Join(manager.Home, "sessions", entries[0].Name(), "claude", "addDir", ".claude", "skills", "one", "SKILL.md"))
	if err != nil || string(data) != "keep first" {
		t.Fatalf("existing destination=%q error=%v", data, err)
	}
	if err := sess.Abort(); err != nil {
		t.Fatal(err)
	}
	entries, err = os.ReadDir(filepath.Join(manager.Home, "sessions"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("after Abort entries=%v error=%v", entries, err)
	}
}

func TestProjectionWriteUsesExclusiveManagerAndFixedSkillPrefix(t *testing.T) {
	f := newFixture()
	sess := f.sessions.session
	sink, err := newProjectionSink(sess, f.sessions, "one")
	if err != nil {
		t.Fatal(err)
	}
	if err := sink.Mkdir("."); err != nil {
		t.Fatal(err)
	}
	if err := sink.Mkdir("empty/nested"); err != nil {
		t.Fatal(err)
	}
	if err := sink.WriteFile("refs/note.md", []byte("note")); err != nil {
		t.Fatal(err)
	}
	prefix := sess.AgentPath("addDir", ".claude", "skills", "one")
	if !reflect.DeepEqual(f.sessions.directories, []string{prefix, filepath.Join(prefix, "empty", "nested")}) {
		t.Fatalf("directories=%v", f.sessions.directories)
	}
	want := []session.File{{Path: filepath.Join(prefix, "refs", "note.md"), Data: []byte("note"), Mode: 0o600}}
	if !reflect.DeepEqual(f.sessions.newFiles, want) || f.sessions.newWritten != sess || len(f.sessions.files) != 0 {
		t.Fatalf("newFiles=%v files=%v", f.sessions.newFiles, f.sessions.files)
	}
	settings := []session.File{{Path: sess.AgentPath("settings.json"), Data: []byte("settings")}}
	if err := f.sessions.Write(sess, settings); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.sessions.newFiles, want) || !reflect.DeepEqual(f.sessions.files, settings) {
		t.Fatal("settings and projection writes mixed")
	}
	for _, relative := range []string{"../two/SKILL.md", "../one-similar/SKILL.md", "/absolute", `C:/absolute`, `..\escape`, "a/../escape", "", "a//b"} {
		if err := sink.Mkdir(relative); err == nil {
			t.Fatalf("accepted directory %q", relative)
		}
		if err := sink.WriteFile(relative, nil); err == nil {
			t.Fatalf("accepted file %q", relative)
		}
	}
	if err := sink.WriteFile(".", nil); err == nil {
		t.Fatal("accepted file root")
	}
	if len(f.sessions.newFiles) != 1 || len(f.sessions.directories) != 2 {
		t.Fatal("invalid paths reached manager")
	}
	for _, name := range []string{".", "..", "../other", "a/b", `a\b`, "C:", ""} {
		if _, err := newProjectionSink(sess, f.sessions, name); err == nil {
			t.Fatalf("accepted skill name %q", name)
		}
	}
}

func TestProjectionWritePreservesManagerErrors(t *testing.T) {
	f := newFixture()
	boom := errors.New("projection write failed")
	f.sessions.writeNewErr, f.sessions.directoryErr = boom, boom
	sink, err := newProjectionSink(f.sessions.session, f.sessions, "one")
	if err != nil {
		t.Fatal(err)
	}
	if err := sink.Mkdir("."); !errors.Is(err, boom) {
		t.Fatalf("mkdir error=%v", err)
	}
	if err := sink.WriteFile("SKILL.md", nil); !errors.Is(err, boom) {
		t.Fatalf("write error=%v", err)
	}
}

func TestPrepareProjectionRetainsOneManifestPerProjectedSelection(t *testing.T) {
	t.Parallel()
	inv, _ := skill.Build([]skill.Location{projectionLocation(skill.SourceAgents, "/foreign/alpha"), projectionLocation(skill.SourceCodex, "/foreign/beta")})
	got, err := prepareProjection(context.Background(), skill.AgentClaude, []string{"beta", "missing", "alpha", "beta"}, inv, skill.ResolveOptions{Projection: true}, nil, projectionInspectorFunc(func(_ context.Context, dir string) (projection.Manifest, *projection.Rejection, error) {
		return projection.Manifest{Root: dir}, nil, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if got.Resolved.Count(skill.StateProjected) != 2 || len(got.Skills) != 2 || got.Skills[0].ID != "beta" || got.Skills[1].ID != "alpha" {
		t.Fatalf("prepared=%#v", got)
	}
	for _, entry := range got.Skills {
		if entry.Name != entry.ID || entry.Manifest.Root != filepath.FromSlash("/foreign/"+entry.ID) {
			t.Fatalf("entry=%#v", entry)
		}
	}
}

func TestPrepareProjectionKeepsDirectoryNameSeparateFromEffectiveName(t *testing.T) {
	t.Parallel()
	loc := projectionLocation(skill.SourceClaude, "/foreign/directory")
	loc.FrontmatterName = "declared"
	inv, _ := skill.Build([]skill.Location{loc})
	got, err := prepareProjection(context.Background(), skill.AgentOpenCode, []string{"directory"}, inv, skill.ResolveOptions{Projection: true}, nil, projectionInspectorFunc(func(_ context.Context, dir string) (projection.Manifest, *projection.Rejection, error) {
		return projection.Manifest{Root: dir}, nil, nil
	}))
	if err != nil || len(got.Skills) != 1 || got.Skills[0].Name != "directory" || !reflect.DeepEqual(got.Resolved.Entries[0].Names, []string{"declared"}) {
		t.Fatalf("prepared=%#v err=%v", got, err)
	}
}

func TestPrepareProjectionTriesNextLocationAfterConflict(t *testing.T) {
	t.Parallel()
	inv := []skill.Skill{
		{ID: "first", Locations: []skill.Location{projectionLocation(skill.SourceAgents, "/first/foo")}},
		{ID: "second", Locations: []skill.Location{projectionLocation(skill.SourceAgents, "/second/Foo"), projectionLocation(skill.SourceCodex, "/fallback/bar")}},
	}
	calls := 0
	got, err := prepareProjection(context.Background(), skill.AgentClaude, []string{"first", "second"}, inv, skill.ResolveOptions{Projection: true}, nil, projectionInspectorFunc(func(_ context.Context, dir string) (projection.Manifest, *projection.Rejection, error) {
		calls++
		return projection.Manifest{Root: dir}, nil, nil
	}))
	if err != nil || calls != 3 || len(got.Skills) != 2 || got.Skills[1].Name != "bar" || got.Skills[1].Manifest.Root != filepath.FromSlash("/fallback/bar") {
		t.Fatalf("prepared=%#v calls=%d err=%v", got, calls, err)
	}
}

func TestPrepareProjectionReportsConfigurationErrors(t *testing.T) {
	t.Parallel()
	inv, _ := skill.Build([]skill.Location{projectionLocation(skill.SourceAgents, "/foreign/foo")})
	for _, inspector := range []ProjectionInspector{nil, projectionInspectorFunc(func(context.Context, string) (projection.Manifest, *projection.Rejection, error) {
		return projection.Manifest{}, &projection.Rejection{Reason: "unrecognized"}, nil
	})} {
		if _, err := prepareProjection(context.Background(), skill.AgentClaude, []string{"foo"}, inv, skill.ResolveOptions{Projection: true}, nil, inspector); err == nil {
			t.Fatal("expected configuration error")
		}
	}
}

func TestPrepareProjectionPreservesAllScanRejectionsWithoutInspector(t *testing.T) {
	t.Parallel()
	loc := projectionLocation(skill.SourceAgents, "/foreign/foo")
	inv, _ := skill.Build([]skill.Location{loc})
	got, err := prepareProjection(context.Background(), skill.AgentClaude, []string{"foo"}, inv, skill.ResolveOptions{Projection: true}, []skill.ScanRejection{{Source: loc.Source, DiscoveryPath: loc.DiscoveryPath, Reason: skill.ReasonLimitExceeded}}, nil)
	if err != nil || len(got.Skills) != 0 || got.Resolved.Entries[0].Reason != skill.ReasonLimitExceeded {
		t.Fatalf("prepared=%#v err=%v", got, err)
	}
}

type projectionCloseErrorRoot struct {
	projection.Root
	err error
}

func (r projectionCloseErrorRoot) Close() error { return errors.Join(r.Root.Close(), r.err) }

func TestPrepareProjectionUsesRealInspectorAndPropagatesFailures(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("skill body"), 0o600); err != nil {
		t.Fatal(err)
	}
	loc := projectionLocation(skill.SourceAgents, filepath.ToSlash(dir))
	inv := []skill.Skill{{ID: "selected", Locations: []skill.Location{loc}}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	closeErr := errors.New("root close failure")
	for _, tt := range []struct {
		name    string
		ctx     context.Context
		open    projection.OpenRoot
		wantErr error
	}{
		{"success", context.Background(), func(directory string) (projection.Root, error) { return host.OpenProjectionRoot(directory) }, nil},
		{"open failure", context.Background(), func(string) (projection.Root, error) { return host.OpenProjectionRoot(filepath.Join(dir, "missing")) }, fs.ErrNotExist},
		{"close failure", context.Background(), func(directory string) (projection.Root, error) {
			root, err := host.OpenProjectionRoot(directory)
			if err != nil {
				return nil, err
			}
			return projectionCloseErrorRoot{Root: root, err: closeErr}, nil
		}, closeErr},
		{"canceled", ctx, func(string) (projection.Root, error) {
			t.Fatal("canceled inspector must not open root")
			return nil, nil
		}, context.Canceled},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := prepareProjection(tt.ctx, skill.AgentClaude, []string{"selected"}, inv, skill.ResolveOptions{Projection: true}, nil, projection.Inspector{OpenRoot: tt.open})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error=%v want=%v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				if !reflect.DeepEqual(got, preparedProjection{}) {
					t.Fatalf("failed preparation escaped: %#v", got)
				}
				return
			}
			if len(got.Skills) != 1 || got.Skills[0].Manifest.Root != dir || got.Skills[0].Manifest.Bytes != 10 || len(got.Skills[0].Manifest.Files) != 1 {
				t.Fatalf("prepared=%#v", got)
			}
		})
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "SKILL.md" {
		t.Fatalf("preparation changed source directory: %v, err=%v", entries, err)
	}
}

type projectionInspectorFunc func(context.Context, string) (projection.Manifest, *projection.Rejection, error)

func (f projectionInspectorFunc) Inspect(ctx context.Context, directory string) (projection.Manifest, *projection.Rejection, error) {
	return f(ctx, directory)
}

func projectionLocation(source skill.Source, directory string) skill.Location {
	return skill.Location{Kind: skill.KindSkill, Source: source, Level: skill.LevelGlobal, DiscoveryPath: directory + "/SKILL.md", RealPath: "/resolved/unrelated/SKILL.md"}
}

func TestPrepareProjectionRejectsInvalidDirectoryNamesBeforePlanning(t *testing.T) {
	t.Parallel()
	invalid := projectionLocation(skill.SourceAgents, "/first/foo:bar")
	repeated := projectionLocation(skill.SourceCodex, "/second/foo:bar")
	valid := projectionLocation(skill.SourceAgents, "/third/foo")
	valid.Scope = "app"
	inv := []skill.Skill{
		{ID: "invalid", Locations: []skill.Location{invalid}},
		{ID: "repeated", Locations: []skill.Location{repeated}},
		{ID: "app:foo", Locations: []skill.Location{valid}},
	}
	calls := 0
	got, err := prepareProjection(context.Background(), skill.AgentClaude, []string{"invalid", "repeated", "app:foo"}, inv, skill.ResolveOptions{Projection: true}, nil, projectionInspectorFunc(func(_ context.Context, dir string) (projection.Manifest, *projection.Rejection, error) {
		calls++
		return projection.Manifest{Root: dir, Files: []projection.File{{Path: "SKILL.md"}}}, nil, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range got.Resolved.Entries[:2] {
		if entry.State != skill.StateUnavailable || entry.Reason != skill.ReasonInvalidPath {
			t.Fatalf("invalid directory must be rejected without occupying its name: %#v", entry)
		}
	}
	if calls != 3 || len(got.Skills) != 1 || got.Skills[0].ID != "app:foo" || got.Skills[0].Name != "foo" {
		t.Fatalf("prepared=%#v calls=%d", got, calls)
	}
	if _, err := projectionFiles(&session.Session{Root: t.TempDir(), Agent: skill.AgentClaude}, got); err != nil {
		t.Fatalf("planning received an invalid projection name: %v", err)
	}
}

func TestPrepareProjectionPreservesIOErrorForInvalidDirectoryName(t *testing.T) {
	t.Parallel()
	ioErr := errors.New("source read failed")
	inv := []skill.Skill{{ID: "foo:bar", Locations: []skill.Location{projectionLocation(skill.SourceAgents, "/foreign/foo:bar")}}}
	_, err := prepareProjection(context.Background(), skill.AgentClaude, []string{"foo:bar"}, inv, skill.ResolveOptions{Projection: true}, nil, projectionInspectorFunc(func(context.Context, string) (projection.Manifest, *projection.Rejection, error) {
		return projection.Manifest{}, nil, ioErr
	}))
	if !errors.Is(err, ioErr) {
		t.Fatalf("error=%v, want inspection I/O error", err)
	}
}

func TestPrepareProjectionSkipsScanRejectionsAndPreservesDiscoveryRoot(t *testing.T) {
	t.Parallel()
	for _, reason := range []skill.ResolutionReason{skill.ReasonSpecialFile, skill.ReasonLimitExceeded} {
		t.Run(string(reason), func(t *testing.T) {
			first := projectionLocation(skill.SourceAgents, "/first/foo")
			second := projectionLocation(skill.SourceCodex, "/second/foo")
			inv, _ := skill.Build([]skill.Location{second, first})
			calls := 0
			wantRoot := filepath.FromSlash("/second/foo")
			inspector := projectionInspectorFunc(func(_ context.Context, directory string) (projection.Manifest, *projection.Rejection, error) {
				calls++
				if directory != wantRoot {
					t.Fatalf("Inspect directory = %q, want discovered directory %q", directory, wantRoot)
				}
				return projection.Manifest{Root: directory, Files: []projection.File{{Path: "SKILL.md", Source: "SKILL.md", Size: 3}}, Bytes: 3}, nil, nil
			})
			got, err := prepareProjection(context.Background(), skill.AgentClaude, []string{"foo", "foo", "missing"}, inv, skill.ResolveOptions{Projection: true}, []skill.ScanRejection{{Source: first.Source, DiscoveryPath: first.DiscoveryPath, Reason: reason}}, inspector)
			if err != nil {
				t.Fatal(err)
			}
			if calls != 1 || got.Resolved.Count(skill.StateProjected) != 1 || len(got.Skills) != 1 {
				t.Fatalf("calls=%d prepared=%#v", calls, got)
			}
			if got.Skills[0].ID != "foo" || got.Skills[0].Name != "foo" || got.Skills[0].Manifest.Root != wantRoot || got.Skills[0].Manifest.Bytes != 3 {
				t.Fatalf("prepared skill = %#v", got.Skills[0])
			}
		})
	}
}

func TestPrepareProjectionScanRejectionKeyIncludesSource(t *testing.T) {
	t.Parallel()
	first := projectionLocation(skill.SourceAgents, "/shared/foo")
	second := projectionLocation(skill.SourceCodex, "/shared/foo")
	inv, _ := skill.Build([]skill.Location{first, second})
	calls := 0
	got, err := prepareProjection(context.Background(), skill.AgentClaude, []string{"foo"}, inv, skill.ResolveOptions{Projection: true}, []skill.ScanRejection{{Source: first.Source, DiscoveryPath: first.DiscoveryPath, Reason: skill.ReasonSpecialFile}}, projectionInspectorFunc(func(_ context.Context, dir string) (projection.Manifest, *projection.Rejection, error) {
		calls++
		return projection.Manifest{Root: dir}, nil, nil
	}))
	if err != nil || calls != 1 || got.Resolved.Count(skill.StateProjected) != 1 {
		t.Fatalf("prepared=%#v calls=%d err=%v", got, calls, err)
	}
}

func TestPrepareProjectionNativeAndUnsupportedNeverInspect(t *testing.T) {
	t.Parallel()
	foreign := projectionLocation(skill.SourceAgents, "/foreign/foo")
	native := projectionLocation(skill.SourceClaude, "/native/foo")
	native.Level = skill.LevelAdmin
	native.Names = map[skill.Agent]string{skill.AgentClaude: "foo"}
	for _, tt := range []struct {
		name      string
		locations []skill.Location
		opts      skill.ResolveOptions
		state     skill.ResolutionState
	}{
		{"native survives rejected foreign", []skill.Location{foreign, native}, skill.ResolveOptions{Projection: true}, skill.StateNative},
		{"projection disabled", []skill.Location{foreign}, skill.ResolveOptions{}, skill.StateUnavailable},
	} {
		t.Run(tt.name, func(t *testing.T) {
			inv, _ := skill.Build(tt.locations)
			got, err := prepareProjection(context.Background(), skill.AgentClaude, []string{"foo"}, inv, tt.opts, []skill.ScanRejection{{Source: foreign.Source, DiscoveryPath: foreign.DiscoveryPath, Reason: skill.ReasonSpecialFile}}, nil)
			if err != nil || len(got.Skills) != 0 || got.Resolved.Entries[0].State != tt.state {
				t.Fatalf("prepared=%#v err=%v", got, err)
			}
		})
	}
}

func TestPrepareProjectionConservativelyRejectsTargetConflicts(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ name, first, second string }{
		{"case variants", "Foo", "foo"},
		{"scoped IDs", "foo", "foo"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			first := projectionLocation(skill.SourceAgents, "/first/"+tt.first)
			first.Scope = "one"
			second := projectionLocation(skill.SourceCodex, "/second/"+tt.second)
			second.Scope = "two"
			second.Names = map[skill.Agent]string{skill.AgentCodex: "keep-case"}
			inv, _ := skill.Build([]skill.Location{first, second})
			selection := []string{"one:" + tt.first, "two:" + tt.second}
			got, err := prepareProjection(context.Background(), skill.AgentClaude, selection, inv, skill.ResolveOptions{Projection: true}, nil, projectionInspectorFunc(func(_ context.Context, dir string) (projection.Manifest, *projection.Rejection, error) {
				return projection.Manifest{Root: dir}, nil, nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Skills) != 1 || got.Skills[0].ID != selection[0] || got.Skills[0].Name != tt.first || got.Resolved.Entries[1].ID != selection[1] || got.Resolved.Entries[1].State != skill.StateUnavailable || got.Resolved.Entries[1].Reason != skill.ReasonTargetConflict {
				t.Fatalf("prepared=%#v", got)
			}
			if inv[1].Locations[0].Names[skill.AgentCodex] != "keep-case" {
				t.Fatal("mutated names")
			}
		})
	}
}

func TestPrepareProjectionReservesUnselectedNativeEffectiveNames(t *testing.T) {
	t.Parallel()
	for _, kind := range []skill.Kind{skill.KindSkill, skill.KindCommand} {
		t.Run(string(kind), func(t *testing.T) {
			foreign := projectionLocation(skill.SourceAgents, "/foreign/Foo")
			native := projectionLocation(skill.SourceClaude, "/native/different-id")
			native.Kind = kind
			native.Names = map[skill.Agent]string{skill.AgentClaude: "foo"}
			inv, _ := skill.Build([]skill.Location{foreign, native})
			got, err := prepareProjection(context.Background(), skill.AgentClaude, []string{"Foo"}, inv, skill.ResolveOptions{Projection: true}, nil, projectionInspectorFunc(func(_ context.Context, dir string) (projection.Manifest, *projection.Rejection, error) {
				return projection.Manifest{Root: dir}, nil, nil
			}))
			if err != nil || len(got.Skills) != 0 || got.Resolved.Entries[0].Reason != skill.ReasonTargetConflict {
				t.Fatalf("prepared=%#v err=%v", got, err)
			}
		})
	}
}

func TestPrepareProjectionRejectedCandidateDoesNotReserveName(t *testing.T) {
	t.Parallel()
	first := projectionLocation(skill.SourceAgents, "/first/Foo")
	first.Scope = "one"
	second := projectionLocation(skill.SourceCodex, "/second/foo")
	second.Scope = "two"
	inv, _ := skill.Build([]skill.Location{first, second})
	calls := 0
	got, err := prepareProjection(context.Background(), skill.AgentClaude, []string{"one:Foo", "two:foo"}, inv, skill.ResolveOptions{Projection: true}, nil, projectionInspectorFunc(func(_ context.Context, dir string) (projection.Manifest, *projection.Rejection, error) {
		calls++
		if calls == 1 {
			return projection.Manifest{Root: dir}, &projection.Rejection{Reason: "outside-root"}, nil
		}
		return projection.Manifest{Root: dir}, nil, nil
	}))
	if err != nil || calls != 2 || len(got.Skills) != 1 || got.Skills[0].ID != "two:foo" {
		t.Fatalf("prepared=%#v calls=%d err=%v", got, calls, err)
	}
}

func TestPrepareProjectionMapsStructuralRejections(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		reason string
		want   skill.ResolutionReason
	}{
		{"outside-root", skill.ReasonOutsideRoot}, {"symlink-loop", skill.ReasonSymlinkLoop},
		{"special-file", skill.ReasonSpecialFile}, {"plugin-manifest", skill.ReasonPluginManifest},
		{"target-conflict", skill.ReasonTargetConflict}, {"invalid-path", skill.ReasonInvalidPath},
		{"too-many-files", skill.ReasonLimitExceeded}, {"too-many-bytes", skill.ReasonLimitExceeded},
	} {
		t.Run(tt.reason, func(t *testing.T) {
			inv, _ := skill.Build([]skill.Location{projectionLocation(skill.SourceAgents, "/foreign/foo")})
			got, err := prepareProjection(context.Background(), skill.AgentClaude, []string{"foo"}, inv, skill.ResolveOptions{Projection: true}, nil, projectionInspectorFunc(func(context.Context, string) (projection.Manifest, *projection.Rejection, error) {
				return projection.Manifest{}, &projection.Rejection{Reason: tt.reason}, nil
			}))
			if err != nil || len(got.Skills) != 0 || got.Resolved.Entries[0].State != skill.StateUnavailable || got.Resolved.Entries[0].Reason != tt.want {
				t.Fatalf("prepared=%#v err=%v", got, err)
			}
		})
	}
}

func TestPrepareProjectionPropagatesInspectionErrors(t *testing.T) {
	t.Parallel()
	for _, wantErr := range []error{errors.New("read failed"), errors.New("close failed"), context.Canceled} {
		t.Run(wantErr.Error(), func(t *testing.T) {
			inv, _ := skill.Build([]skill.Location{projectionLocation(skill.SourceAgents, "/first/foo"), projectionLocation(skill.SourceCodex, "/second/foo")})
			calls := 0
			got, err := prepareProjection(context.Background(), skill.AgentClaude, []string{"foo"}, inv, skill.ResolveOptions{Projection: true}, nil, projectionInspectorFunc(func(context.Context, string) (projection.Manifest, *projection.Rejection, error) {
				calls++
				return projection.Manifest{}, &projection.Rejection{Reason: "outside-root"}, wantErr
			}))
			if !errors.Is(err, wantErr) || calls != 1 || !reflect.DeepEqual(got, preparedProjection{}) {
				t.Fatalf("prepared=%#v calls=%d err=%v", got, calls, err)
			}
		})
	}
}
