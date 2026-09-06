package launch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/scarb/skope/internal/agent"
	"github.com/scarb/skope/internal/agent/codex"
	"github.com/scarb/skope/internal/config"
	"github.com/scarb/skope/internal/host"
	"github.com/scarb/skope/internal/projection"
	"github.com/scarb/skope/internal/session"
	"github.com/scarb/skope/internal/skill"
)

type contextualForeign func(context.Context, host.Env, skill.Agent, int64) (skill.ScanResult, error)

func (f contextualForeign) ScanForeign(c context.Context, e host.Env, a skill.Agent, n int64) (skill.ScanResult, error) {
	return f(c, e, a, n)
}

type codexCountScanner struct {
	skill.Scanner
	calls int
}

func (s *codexCountScanner) ScanCodex(e host.Env, p host.CodexPaths) (skill.ScanResult, error) {
	s.calls++
	return s.Scanner.ScanCodex(e, p)
}

type observedCodex struct {
	agent.Adapter
	f          *fixture
	planErr    error
	beforePlan func()
}

func (a *observedCodex) Inventory(c context.Context, e host.Env) (agent.Inventory, error) {
	a.f.record("inventory")
	return a.Adapter.Inventory(c, e)
}
func (a *observedCodex) Plan(r skill.Resolved, i agent.Inventory, s *session.Session) (agent.LaunchPlan, error) {
	a.f.record("plan")
	if a.beforePlan != nil {
		a.beforePlan()
	}
	if a.planErr != nil {
		return agent.LaunchPlan{}, a.planErr
	}
	return a.Adapter.Plan(r, i, s)
}

func serviceCodexFixture(t *testing.T) (*fixture, *observedCodex, *codexCountScanner) {
	t.Helper()
	f := newFixture()
	home := t.TempDir()
	repo := filepath.Join(home, "repo")
	write := func(path, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	paths := host.CodexPaths{Home: home, CodexHome: filepath.Join(home, ".codex"), AdminSkillRoots: []string{filepath.Join(home, "admin", "skills")}, SystemConfigPaths: []string{filepath.Join(home, "admin", "config.toml")}}
	body := "---\nname: shared\ndescription: shared skill\n---\n"
	write(filepath.Join(paths.CodexHome, "skills", "shared", "SKILL.md"), body)
	write(filepath.Join(home, ".claude", "skills", "shared", "SKILL.md"), body)
	write(filepath.Join(home, ".claude", "skills", "foreign", "SKILL.md"), "---\nname: foreign\n---\n")
	write(filepath.Join(home, ".claude", "commands", "ns", "run.md"), "command")
	f.service.Env = host.NewEnv(home, repo, nil)
	f.fsys.files[f.configPath] = []byte("version=1\n[agents.codex]\ncommand='configured-codex'\nargs=['--configured']\n")
	f.fsys.files[f.skillSetsPath] = []byte("version=1\n[skillsets.dev]\nskills=['shared','foreign','ns:run','missing']\n")
	f.sessions.session.Agent = skill.AgentCodex
	scanner := &codexCountScanner{Scanner: skill.Scanner{FS: host.OSFileSystem{}, RegularFiles: host.OSFileSystem{}}}
	adapter := &observedCodex{f: f, Adapter: codex.New(scanner, codex.NewCatalog(scanner.FS, scanner.RegularFiles, scanner.Scanner), host.OSFileSystem{}, codex.Options{ResolvePaths: func(host.Env) (host.CodexPaths, error) { return paths, nil }})}
	f.registry.adapter = adapter
	f.service.Foreign = contextualForeign(func(ctx context.Context, e host.Env, a skill.Agent, n int64) (skill.ScanResult, error) {
		f.record("foreign")
		if a != skill.AgentCodex || n != projection.MaxBytes || ctx == nil || e.Home() != home {
			t.Fatal("wrong foreign invocation")
		}
		roots, _, err := scanner.ClaudeRoots(e)
		if err != nil {
			return skill.ScanResult{}, err
		}
		return scanner.ScanForeignRoots(roots, n)
	})
	f.service.Inspector = inspectFunc(func(context.Context, string) (projection.Manifest, *projection.Rejection, error) {
		panic("Codex must never inspect projection")
	})
	f.service.Copier = copyFunc(func(context.Context, projection.Manifest, projection.Sink) error {
		panic("Codex must never copy projection")
	})
	return f, adapter, scanner
}

func TestServiceCodexOrchestratesWithoutProjection(t *testing.T) {
	for _, dry := range []bool{false, true} {
		t.Run(map[bool]string{false: "active", true: "dry"}[dry], func(t *testing.T) {
			f, _, scanner := serviceCodexFixture(t)
			var got Result
			err := f.service.Run(context.Background(), Request{Agent: skill.AgentCodex, SetPresent: true, SetValue: "dev", DryRun: dry}, func(r Result) error { got = r; return f.reporter(r) })
			if err != nil {
				t.Fatal(err)
			}
			want := []string{"read config.toml", "reap", "lookpath", "read skillsets.toml", "conflict", "factory", "registry", "inventory", "foreign"}
			if dry {
				want = append(want, "preview", "plan", "report")
			} else {
				want = append(want, "stage", "plan", "write", "publish", "report", "handoff")
			}
			if !reflect.DeepEqual(f.events, want) || scanner.calls != 1 {
				t.Fatalf("events=%v scans=%d", f.events, scanner.calls)
			}
			if len(got.Plan.Files) != 0 || len(f.sessions.files) != 0 || len(got.ProjectionFiles) != 0 {
				t.Fatalf("Codex wrote projection=%+v", got)
			}
			states := []skill.ResolutionState{skill.StateNative, skill.StateUnavailable, skill.StateUnavailable, skill.StateMissing}
			for i, state := range states {
				if got.Resolved.Entries[i].State != state {
					t.Fatalf("resolved=%+v", got.Resolved)
				}
			}
			if got.Resolved.Entries[1].Reason != skill.ReasonProjectionUnsupported || got.Resolved.Entries[2].Reason != skill.ReasonCommandOnly || got.Resolved.Entries[2].ID != "ns:run" {
				t.Fatalf("resolved=%+v", got.Resolved)
			}
			args := strings.Join(got.Args, " ")
			if strings.Contains(args, ".claude") || !strings.Contains(args, "shared/SKILL.md") || !strings.Contains(args, "enabled=true") || f.resolver.command != "configured-codex" {
				t.Fatalf("args=%v command=%s", got.Args, f.resolver.command)
			}
		})
	}
}
func TestServiceCodexFailureAndCancellationAbortWithJoinedCleanup(t *testing.T) {
	for _, at := range []string{"plan", "canonicalize", "report", "handoff", "cancel stage", "cancel plan", "cancel write", "cancel publish", "cancel report", "cancel handoff"} {
		t.Run(at, func(t *testing.T) {
			f, adapter, _ := serviceCodexFixture(t)
			cause := errors.New("primary failure")
			cleanup := errors.New("cleanup failure")
			f.sessions.abortErr = cleanup
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch at {
			case "plan":
				adapter.planErr = cause
			case "canonicalize":
				cause = os.ErrNotExist
				adapter.beforePlan = func() {
					if err := os.Remove(filepath.Join(f.service.Env.Home(), ".codex", "skills", "shared", "SKILL.md")); err != nil {
						t.Fatal(err)
					}
				}
			case "report":
				f.reportErr = cause
			case "handoff":
				f.handoff.err = cause
			default:
				f.cancelAt = strings.TrimPrefix(at, "cancel ")
				f.cancel = cancel
				cause = context.Canceled
			}
			err := f.service.Run(ctx, Request{Agent: skill.AgentCodex, SetPresent: true, SetValue: "dev"}, f.reporter)
			if !errors.Is(err, cause) || !errors.Is(err, cleanup) || f.sessions.aborted == nil || f.events[len(f.events)-1] != "abort" {
				t.Fatalf("err=%v events=%v", err, f.events)
			}
		})
	}
}
func TestServiceCodexNoneBypassesFactoryAndBrokenSources(t *testing.T) {
	f, _, scanner := serviceCodexFixture(t)
	f.fsys.files[f.skillSetsPath] = []byte("version=[")
	if err := os.WriteFile(filepath.Join(f.service.Env.Home(), ".codex", "config.toml"), []byte("[broken"), 0600); err != nil {
		t.Fatal(err)
	}
	f.service.NewRegistry = func(string, config.Selection) (AdapterRegistry, error) { panic("none factory") }
	f.service.Foreign = contextualForeign(func(context.Context, host.Env, skill.Agent, int64) (skill.ScanResult, error) { panic("none foreign") })
	for _, dry := range []bool{false, true} {
		err := f.service.Run(context.Background(), Request{Agent: skill.AgentCodex, SetPresent: true, SetValue: "none", DryRun: dry}, func(r Result) error {
			if !r.NoIsolation {
				t.Fatal("none isolation")
			}
			return nil
		})
		if err != nil || scanner.calls != 0 || slices.Contains(f.events, "stage") {
			t.Fatalf("err=%v calls=%d events=%v", err, scanner.calls, f.events)
		}
	}
}

type codexProcesses struct{}

func (codexProcesses) StartToken(int) (string, error) { return "test-process", nil }

type codexManagedSessions struct {
	SessionManager
	staged *session.Session
}

func (m *codexManagedSessions) Stage(a skill.Agent, set string) (*session.Session, error) {
	s, err := m.SessionManager.Stage(a, set)
	m.staged = s
	return s, err
}
func TestServiceCodexPublishedSessionContainsOnlyOwner(t *testing.T) {
	f, _, _ := serviceCodexFixture(t)
	manager := session.NewManager(t.TempDir())
	manager.Processes = codexProcesses{}
	managed := &codexManagedSessions{SessionManager: manager}
	f.service.Sessions = managed
	var root string
	err := f.service.Run(context.Background(), Request{Agent: skill.AgentCodex, SetPresent: true, SetValue: "dev"}, func(r Result) error { root = r.Session.Root; return nil })
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = manager.Abort(managed.staged) }()
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 || entries[0].Name() != "owner.json" {
		t.Fatalf("entries=%v err=%v", entries, err)
	}
}
