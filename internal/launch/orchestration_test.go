package launch

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/scarb/skope/internal/agent/claude"
	"github.com/scarb/skope/internal/host"
	"github.com/scarb/skope/internal/proc"
	"github.com/scarb/skope/internal/projection"
	"github.com/scarb/skope/internal/skill"
)

type foreignScanFunc func(host.Env, int64) (skill.ScanResult, error)

func TestServiceDryRunRejectsInvalidRealForeignDirectoryAndContinues(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows cannot create a directory containing a colon; deterministic preparation test covers this platform")
	}
	t.Parallel()
	f := newFixture()
	home := t.TempDir()
	for _, name := range []string{"foo:bar", "good"} {
		dir := filepath.Join(home, ".agents", "skills", name)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("skill"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	f.service.Env = host.NewEnv(home, home, nil)
	f.adapter.capabilities.Projection = true
	f.fsys.files[f.skillSetsPath] = []byte("version=1\n[skillsets.dev]\nskills=['foo:bar','good','one','missing']\n")
	f.service.Foreign = skill.Scanner{FS: host.OSFileSystem{}, RegularFiles: host.OSFileSystem{}}
	f.service.Inspector = projection.Inspector{OpenRoot: func(dir string) (projection.Root, error) { return host.OpenProjectionRoot(dir) }}
	var result Result
	err := f.service.Run(context.Background(), Request{Agent: skill.AgentClaude, SetPresent: true, SetValue: "dev", DryRun: true}, func(r Result) error {
		result = r
		return f.reporter(r)
	})
	if err != nil {
		t.Fatalf("invalid foreign basename must not abort dry-run: %v", err)
	}
	if len(result.Resolved.Entries) != 4 {
		t.Fatalf("resolved=%#v", result.Resolved)
	}
	for i, want := range []skill.ResolutionState{skill.StateUnavailable, skill.StateProjected, skill.StateNative, skill.StateMissing} {
		if result.Resolved.Entries[i].State != want {
			t.Fatalf("entry %d=%#v, want %s", i, result.Resolved.Entries[i], want)
		}
	}
	if result.Resolved.Entries[0].Reason != skill.ReasonInvalidPath || len(result.ProjectionFiles) != 1 || result.ProjectionFiles[0].ID != "good" {
		t.Fatalf("resolved=%#v projection files=%#v", result.Resolved, result.ProjectionFiles)
	}
	if !slices.Contains(f.events, "preview") || !slices.Contains(f.events, "report") || slices.Contains(f.events, "stage") || len(f.sessions.newFiles) != 0 {
		t.Fatalf("dry-run events=%v new files=%v", f.events, f.sessions.newFiles)
	}
}

func (f foreignScanFunc) ScanForeignGlobals(env host.Env, limit int64) (skill.ScanResult, error) {
	return f(env, limit)
}

func TestServiceScansForeignBeforeSession(t *testing.T) {
	f := newFixture()
	f.service.Foreign = foreignScanFunc(func(env host.Env, limit int64) (skill.ScanResult, error) {
		f.record("foreign")
		if env.Home() != f.service.Env.Home() || limit != 20<<20 {
			t.Fatalf("env=%v limit=%d", env, limit)
		}
		return skill.ScanResult{}, nil
	})
	if err := f.service.Run(context.Background(), Request{Agent: skill.AgentClaude, SetPresent: true, SetValue: "dev"}, f.reporter); err != nil {
		t.Fatal(err)
	}
	want := []string{"read config.toml", "reap", "lookpath", "read skillsets.toml", "conflict", "factory", "registry", "inventory", "foreign", "stage", "plan", "write", "publish", "report", "handoff"}
	if !reflect.DeepEqual(f.events, want) {
		t.Fatalf("events=%v", f.events)
	}
}

func foreignLocation(name string) skill.Location {
	return skill.Location{Kind: skill.KindSkill, Source: skill.SourceCodex, Level: skill.LevelGlobal, DiscoveryPath: filepath.ToSlash(filepath.Join("foreign", name, "SKILL.md")), RealPath: filepath.ToSlash(filepath.Join("foreign", name, "SKILL.md")), Names: map[skill.Agent]string{skill.AgentCodex: name}}
}

type inspectFunc func(context.Context, string) (projection.Manifest, *projection.Rejection, error)

func (f inspectFunc) Inspect(ctx context.Context, dir string) (projection.Manifest, *projection.Rejection, error) {
	return f(ctx, dir)
}

type copyFunc func(context.Context, projection.Manifest, projection.Sink) error

func (f copyFunc) Copy(ctx context.Context, m projection.Manifest, sink projection.Sink) error {
	return f(ctx, m, sink)
}

func projectedFixture(t *testing.T) *fixture {
	t.Helper()
	f := newFixture()
	f.adapter.capabilities.Projection = true
	f.sessions.session.Root = t.TempDir()
	f.fsys.files[f.skillSetsPath] = []byte("version=1\n[skillsets.dev]\nskills=['two','one','three','missing']\nbundled=true\n[skillsets.dev.plugins]\nclaude=['allowed@m','missing@m']\n")
	f.adapter.inventory.PluginIDs = []string{"allowed@m", "disabled@m", "disabled@m", "stale@m"}
	f.service.Foreign = foreignScanFunc(func(host.Env, int64) (skill.ScanResult, error) {
		f.record("foreign")
		skills, collisions := skill.Build([]skill.Location{foreignLocation("two"), foreignLocation("three")})
		return skill.ScanResult{Skills: skills, Collisions: collisions}, nil
	})
	f.service.Inspector = inspectFunc(func(_ context.Context, dir string) (projection.Manifest, *projection.Rejection, error) {
		f.record("inspect " + filepath.Base(dir))
		return projection.Manifest{Root: dir, Files: []projection.File{{Path: "SKILL.md", Source: "private-source"}}, Warnings: []string{"expanded link"}}, nil, nil
	})
	f.service.Copier = copyFunc(func(ctx context.Context, m projection.Manifest, sink projection.Sink) error {
		f.record("copy " + filepath.Base(m.Root))
		if err := ctx.Err(); err != nil {
			return err
		}
		return sink.WriteFile("SKILL.md", []byte(filepath.Base(m.Root)))
	})
	return f
}

func TestServiceProjectsInSelectionOrderAndReportsFinalMetadata(t *testing.T) {
	for _, dry := range []bool{false, true} {
		t.Run(map[bool]string{false: "active", true: "dry"}[dry], func(t *testing.T) {
			f := projectedFixture(t)
			var result Result
			err := f.service.Run(context.Background(), Request{Agent: skill.AgentClaude, SetPresent: true, SetValue: "dev", DryRun: dry}, func(r Result) error { f.record("report"); result = r; return nil })
			if err != nil {
				t.Fatal(err)
			}
			want := []string{"read config.toml", "reap", "lookpath", "read skillsets.toml", "conflict", "factory", "registry", "inventory", "foreign", "inspect two", "inspect three"}
			if dry {
				want = append(want, "preview", "plan", "report")
			} else {
				want = append(want, "stage", "copy two", "write-new", "copy three", "write-new", "plan", "write", "publish", "report", "handoff")
			}
			if !reflect.DeepEqual(f.events, want) {
				t.Fatalf("events=%v want=%v", f.events, want)
			}
			wantFiles := []ProjectionFile{{ID: "two", Path: f.sessions.session.AgentPath("addDir", ".claude", "skills", "two", "SKILL.md")}, {ID: "three", Path: f.sessions.session.AgentPath("addDir", ".claude", "skills", "three", "SKILL.md")}}
			if !reflect.DeepEqual(result.ProjectionFiles, wantFiles) {
				t.Fatalf("files=%+v", result.ProjectionFiles)
			}
			if !dry {
				for i, file := range f.sessions.newFiles {
					if file.Path != wantFiles[i].Path {
						t.Fatalf("actual=%v display=%v", file, wantFiles[i])
					}
				}
			}
			if result.Plugins != (PluginSummary{Allowed: 2, Disabled: 2}) || !result.Bundled {
				t.Fatalf("plugins=%+v bundled=%v", result.Plugins, result.Bundled)
			}
			if len(result.Warnings) != 3 || !strings.Contains(result.Warnings[1].Error(), "two") || !strings.Contains(result.Warnings[2].Error(), "three") {
				t.Fatalf("warnings=%v", result.Warnings)
			}
			if len(result.Inventory.Warnings) != 1 {
				t.Fatalf("synthetic plugin warnings=%v", result.Inventory.Warnings)
			}
			if result.Resolved.Entries[0].State != skill.StateProjected || result.Resolved.Entries[1].State != skill.StateNative {
				t.Fatalf("resolved=%+v", result.Resolved)
			}
		})
	}
}

func TestServiceProjectionFailuresAndCancellationStopAtBoundary(t *testing.T) {
	original, cleanup := errors.New("step failed"), errors.New("abort failed")
	for _, step := range []string{"foreign", "inspect two", "inspect three", "copy two", "copy three", "handoff"} {
		for _, canceled := range []bool{false, true} {
			t.Run(step+map[bool]string{true: " cancel", false: " error"}[canceled], func(t *testing.T) {
				f := projectedFixture(t)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				postStage := strings.HasPrefix(step, "copy") || step == "handoff"
				f.sessions.abortErr = cleanup
				if canceled {
					f.cancelAt = step
					f.cancel = cancel
				} else {
					switch {
					case step == "foreign":
						f.service.Foreign = foreignScanFunc(func(host.Env, int64) (skill.ScanResult, error) { f.record(step); return skill.ScanResult{}, original })
					case strings.HasPrefix(step, "inspect"):
						originalInspect := f.service.Inspector
						f.service.Inspector = inspectFunc(func(c context.Context, d string) (projection.Manifest, *projection.Rejection, error) {
							m, r, e := originalInspect.Inspect(c, d)
							if step == "inspect "+filepath.Base(d) {
								return m, r, original
							}
							return m, r, e
						})
					case strings.HasPrefix(step, "copy"):
						f.service.Copier = copyFunc(func(_ context.Context, m projection.Manifest, _ projection.Sink) error {
							f.record("copy " + filepath.Base(m.Root))
							if step == "copy "+filepath.Base(m.Root) {
								return original
							}
							return nil
						})
					case step == "handoff":
						f.handoff.err = original
					}
				}
				err := f.service.Run(ctx, Request{Agent: skill.AgentClaude, SetPresent: true, SetValue: "dev"}, f.reporter)
				wantErr := original
				if canceled {
					wantErr = context.Canceled
				}
				if !errors.Is(err, wantErr) || errors.Is(err, cleanup) != postStage {
					t.Fatalf("err=%v events=%v", err, f.events)
				}
				count := 0
				for _, e := range f.events {
					if e == "abort" {
						count++
					}
				}
				wantCount := 0
				if postStage {
					wantCount = 1
				}
				if count != wantCount {
					t.Fatalf("events=%v", f.events)
				}
				last := len(f.events) - 1
				if postStage {
					last--
				}
				if f.events[last] != step {
					t.Fatalf("continued after failure: %v", f.events)
				}
			})
		}
	}
}

func TestServiceNoneBypassesProjectionDependencies(t *testing.T) {
	f := projectedFixture(t)
	f.fsys.errs[f.skillSetsPath] = errors.New("invalid sets")
	err := f.service.Run(context.Background(), Request{Agent: skill.AgentClaude, SetPresent: true, SetValue: "none", DryRun: true}, f.reporter)
	if err != nil || !reflect.DeepEqual(f.events, []string{"read config.toml", "reap", "lookpath", "report"}) {
		t.Fatalf("err=%v events=%v", err, f.events)
	}
}

func TestSummarizeReportsAllResolutionStates(t *testing.T) {
	s := Summarize(skill.Resolved{Entries: []skill.Resolution{{ID: "n", State: skill.StateNative}, {ID: "p", State: skill.StateProjected}, {ID: "u", State: skill.StateUnavailable, Reason: skill.ReasonSpecialFile}, {ID: "m", State: skill.StateMissing}}})
	if s.Native != 1 || s.Projected != 1 || len(s.Unavailable) != 1 || s.Unavailable[0].ID != "u" || s.Unavailable[0].Reason != skill.ReasonSpecialFile || !slices.Equal(s.Missing, []string{"m"}) {
		t.Fatalf("summary=%+v", s)
	}
}

func TestServiceScanRejectionsStayUnavailableAndTryLaterSources(t *testing.T) {
	for _, scenario := range []string{"special", "oversize", "native wins", "later source"} {
		t.Run(scenario, func(t *testing.T) {
			f := projectedFixture(t)
			f.fsys.files[f.skillSetsPath] = []byte("version=1\n[skillsets.dev]\nskills=['two']\n")
			loc := foreignLocation("two")
			loc.Source = skill.SourceAgents
			loc.Names = nil
			reason := skill.ReasonSpecialFile
			if scenario == "oversize" {
				reason = skill.ReasonLimitExceeded
			}
			locations := []skill.Location{loc}
			want := skill.StateUnavailable
			if scenario == "later source" {
				next := foreignLocation("two")
				next.DiscoveryPath = "codex/two/SKILL.md"
				locations = append(locations, next)
				want = skill.StateProjected
			}
			if scenario == "native wins" {
				native := foreignLocation("two")
				native.Source = skill.SourceClaude
				native.DiscoveryPath = "claude/two/SKILL.md"
				native.Names = map[skill.Agent]string{skill.AgentClaude: "two"}
				f.adapter.inventory.Skills, _ = skill.Build([]skill.Location{native})
				want = skill.StateNative
			}
			f.service.Foreign = foreignScanFunc(func(host.Env, int64) (skill.ScanResult, error) {
				skills, collisions := skill.Build(locations)
				return skill.ScanResult{Skills: skills, Collisions: collisions, Rejections: []skill.ScanRejection{{Source: loc.Source, DiscoveryPath: loc.DiscoveryPath, Reason: reason}}}, nil
			})
			err := f.service.Run(context.Background(), Request{Agent: skill.AgentClaude, SetPresent: true, SetValue: "dev"}, func(r Result) error {
				if len(r.Resolved.Entries) != 1 || r.Resolved.Entries[0].State != want {
					t.Fatalf("resolved=%+v", r.Resolved)
				}
				if want == skill.StateUnavailable && r.Resolved.Entries[0].Reason != reason {
					t.Fatalf("reason=%v", r.Resolved.Entries[0].Reason)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			inspects, copies := 0, 0
			for _, event := range f.events {
				if strings.HasPrefix(event, "inspect ") {
					inspects++
				}
				if strings.HasPrefix(event, "copy ") {
					copies++
				}
			}
			wantCount := 0
			if want == skill.StateProjected {
				wantCount = 1
			}
			if inspects != wantCount || copies != wantCount {
				t.Fatalf("events=%v", f.events)
			}
			if wantCount == 0 && (len(f.sessions.directories) != 0 || len(f.sessions.newFiles) != 0) {
				t.Fatal("created unused projection")
			}
		})
	}
}

func TestCloneResultCopiesProjectionDisplayAndLocation(t *testing.T) {
	loc := foreignLocation("two")
	source := Result{ProjectionFiles: []ProjectionFile{{ID: "two", Path: "final/SKILL.md"}}, Resolved: skill.Resolved{Entries: []skill.Resolution{{ID: "two", Names: []string{"two"}, Location: &loc}}}}
	cloned := cloneResult(source)
	cloned.ProjectionFiles[0].Path = "mutated"
	cloned.Resolved.Entries[0].Location.Names[skill.AgentCodex] = "mutated"
	cloned.Resolved.Entries[0].Location.DiscoveryPath = "mutated"
	cloned.Resolved.Entries[0].Names[0] = "mutated"
	if source.ProjectionFiles[0].Path != "final/SKILL.md" || source.Resolved.Entries[0].Location.Names[skill.AgentCodex] != "two" || source.Resolved.Entries[0].Location.DiscoveryPath == "mutated" || source.Resolved.Entries[0].Names[0] != "two" {
		t.Fatalf("source mutated: %+v", source)
	}
}

type probeFunc func(context.Context, proc.Request) (proc.Result, error)

func (f probeFunc) Run(c context.Context, r proc.Request) (proc.Result, error) { return f(c, r) }

type closeFailureRoot struct {
	projection.Root
	failure error
}

func (r closeFailureRoot) Close() error { return errors.Join(r.Root.Close(), r.failure) }

func TestServiceCopiesRealTreeBeforePlanAndProtectsReportSnapshot(t *testing.T) {
	for _, closeFail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "source close failure"}[closeFail], func(t *testing.T) {
			f := newFixture()
			root := t.TempDir()
			source := filepath.Join(root, "source")
			for _, name := range []string{"SKILL.md", "refs/note.md"} {
				p := filepath.Join(source, filepath.FromSlash(name))
				if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte("body "+name), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			entry := filepath.Join(root, "home", ".agents", "skills", "linked")
			if err := os.MkdirAll(filepath.Dir(entry), 0o700); err != nil {
				t.Fatal(err)
			}
			projectionDirectoryLink(t, source, entry)
			f.service.Env = host.NewEnv(filepath.Join(root, "home"), root, map[string]string{"CODEX_HOME": filepath.Join(root, "codex"), "CLAUDE_CONFIG_DIR": filepath.Join(root, "claude")})
			f.fsys.files[f.skillSetsPath] = []byte("version=1\n[skillsets.dev]\nskills=['linked']\n")
			fsys := host.OSFileSystem{}
			scanner := skill.Scanner{FS: fsys, RegularFiles: fsys}
			f.service.Foreign = scanner
			f.registry.adapter = claude.New(scanner, fsys, probeFunc(func(context.Context, proc.Request) (proc.Result, error) {
				return proc.Result{Stdout: []byte("[]")}, nil
			}), claude.Options{Executable: "fake"})
			manager := projectionTestManager(t)
			f.service.Sessions = manager
			open := func(d string) (projection.Root, error) { return host.OpenProjectionRoot(d) }
			f.service.Inspector = projection.Inspector{OpenRoot: open}
			closeErr := errors.New("source copy close failed")
			opened := 0
			f.service.Copier = projection.Copier{OpenRoot: func(d string) (projection.Root, error) {
				r, e := open(d)
				opened++
				if e == nil && closeFail && opened == 2 {
					return closeFailureRoot{Root: r, failure: closeErr}, nil
				}
				return r, e
			}}
			handoffErr := errors.New("fake handoff returns")
			f.handoff.err = handoffErr
			var paths []string
			var args, env []string
			reported := false
			err := f.service.Run(context.Background(), Request{Agent: skill.AgentClaude, SetPresent: true, SetValue: "dev"}, func(r Result) error {
				reported = true
				args = append([]string(nil), r.Args...)
				env = append([]string(nil), r.Env...)
				for _, file := range r.ProjectionFiles {
					paths = append(paths, file.Path)
					data, e := os.ReadFile(file.Path)
					if e != nil || !strings.HasPrefix(string(data), "body ") {
						t.Fatalf("copied=%s err=%v", data, e)
					}
				}
				if len(paths) != 2 || !slices.Contains(r.Args, "--add-dir") {
					t.Fatalf("report=%+v", r)
				}
				configBytes, e := os.ReadFile(r.Plan.Files[0].Path)
				if e != nil || !reflect.DeepEqual(configBytes, r.Plan.Files[0].Data) {
					t.Fatalf("config=%s err=%v", configBytes, e)
				}
				if e := r.Session.Abort(); e == nil {
					t.Fatal("display session can abort live state")
				}
				r.Args[0] = "changed"
				r.Env[0] = "changed"
				r.Plan.Files[0].Data[0] = '!'
				r.Resolved.Entries[0].Location.Names[skill.AgentCodex] = "changed"
				r.Resolved.Entries[0].Location.RealPath = "changed"
				r.ProjectionFiles[0].Path = "changed"
				r.Session.Root = "changed"
				r.Session.Agent = skill.AgentCodex
				return nil
			})
			entries, e := os.ReadDir(filepath.Join(manager.Home, "sessions"))
			if e != nil || len(entries) != 0 {
				t.Fatalf("session cleanup=%v err=%v", entries, e)
			}
			if closeFail {
				if !errors.Is(err, closeErr) || reported || slices.Contains(f.events, "handoff") {
					t.Fatalf("err=%v reported=%v events=%v", err, reported, f.events)
				}
				return
			}
			if !errors.Is(err, handoffErr) || !reported {
				t.Fatalf("err=%v reported=%v", err, reported)
			}
			if !reflect.DeepEqual(f.handoff.args, args) || !reflect.DeepEqual(f.handoff.env, env) {
				t.Fatalf("handoff mutated=%v %v", f.handoff.args, f.handoff.env)
			}
			for _, p := range paths {
				if _, e := os.Stat(p); !errors.Is(e, os.ErrNotExist) {
					t.Fatalf("failed handoff left files: %v", e)
				}
			}
		})
	}
}

func TestServiceCancellationAfterFinalNoneStep(t *testing.T) {
	for _, dry := range []bool{false, true} {
		t.Run(map[bool]string{false: "handoff", true: "report"}[dry], func(t *testing.T) {
			f := newFixture()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f.cancel = cancel
			f.cancelAt = "handoff"
			if dry {
				f.cancelAt = "report"
			}
			err := f.service.Run(ctx, Request{Agent: skill.AgentClaude, SetValue: "none", SetPresent: true, DryRun: dry}, f.reporter)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("err=%v events=%v", err, f.events)
			}
		})
	}
}

func TestPluginSummaryMatchesClaudeSettingsGolden(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "agent", "claude", "testdata", "settings.golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	var settings struct {
		EnabledPlugins map[string]bool `json:"enabledPlugins"`
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	var want PluginSummary
	for _, enabled := range settings.EnabledPlugins {
		if enabled {
			want.Allowed++
		} else {
			want.Disabled++
		}
	}
	got := summarizePlugins([]string{"allowed@market", "blocked@market", "stale@market", "blocked@market"}, []string{"allowed@market", "missing@market", "allowed@market"})
	if got != want || got != (PluginSummary{Allowed: 2, Disabled: 2}) {
		t.Fatalf("summary=%+v golden=%+v", got, want)
	}
}
