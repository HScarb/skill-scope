package cli_test

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/scarb/skope/internal/agent"
	"github.com/scarb/skope/internal/agent/claude"
	"github.com/scarb/skope/internal/agent/codex"
	"github.com/scarb/skope/internal/cli"
	"github.com/scarb/skope/internal/config"
	"github.com/scarb/skope/internal/host"
	"github.com/scarb/skope/internal/launch"
	"github.com/scarb/skope/internal/proc"
	"github.com/scarb/skope/internal/projection"
	"github.com/scarb/skope/internal/session"
	"github.com/scarb/skope/internal/skill"
)

type phaseTwoProcesses struct{}

func (phaseTwoProcesses) StartToken(int) (string, error) { return "isolated-test-process", nil }

type phaseTwoHandoff struct{ called bool }

func (h *phaseTwoHandoff) Exec(string, []string, []string) error {
	h.called = true
	return nil
}

// Use production filesystem, discovery, adapter and copy components. Only the
// process identity and final exec are controlled because Windows defers them.
func phaseTwoService(t *testing.T, f *integrationFixture, runner claude.ProbeRunner) (*launch.Service, *phaseTwoHandoff) {
	t.Helper()
	env := make(map[string]string)
	for _, pair := range withEnv(os.Environ(), map[string]string{
		"HOME": f.home, "USERPROFILE": f.home, "SKOPE_HOME": f.skopeHome,
		"CLAUDE_CONFIG_DIR": f.claudeConfig, "CODEX_HOME": filepath.Join(f.home, ".codex"),
		"FAKEAGENT_PLUGIN_JSON": "[]", "FAKEAGENT_PLUGIN_EXIT": "0",
		"FAKEAGENT_PROBE_LOG": filepath.Join(f.root, "probe.jsonl"),
	}) {
		key, value, ok := strings.Cut(pair, "=")
		if ok && key != "" {
			env[key] = value
		}
	}
	fsys := host.OSFileSystem{}
	scanner := skill.Scanner{FS: fsys, RegularFiles: fsys}
	manager := session.NewManager(f.skopeHome)
	manager.Processes = phaseTwoProcesses{}
	handoff := &phaseTwoHandoff{}
	open := func(dir string) (projection.Root, error) { return host.OpenProjectionRoot(dir) }
	return &launch.Service{
		Env: host.NewEnv(f.home, f.repo, env), FS: fsys, SkopeHome: f.skopeHome,
		NewRegistry: func(executable string, selected config.Selection) (launch.AdapterRegistry, error) {
			return agent.NewRegistry(claude.New(scanner, fsys, runner, claude.Options{Executable: executable, Plugins: selected.Plugins["claude"], Bundled: selected.Bundled}))
		},
		CheckConflicts: cli.CheckConflicts, Foreign: cli.NewForeignScanner(scanner, codex.NewCatalog(fsys, fsys, scanner), func(host.Env) (host.CodexPaths, error) {
			return host.CodexPaths{Home: f.home, CodexHome: filepath.Join(f.home, ".codex"), AdminSkillRoots: []string{filepath.Join(f.root, "admin", "skills")}, SystemConfigPaths: []string{filepath.Join(f.root, "admin", "config.toml")}}, nil
		}), Resolver: host.ExecutableResolver{},
		Inspector: projection.Inspector{OpenRoot: open}, Copier: projection.Copier{OpenRoot: open},
		Sessions: phaseTwoManagedSessions{SessionManager: manager, t: t}, Handoff: handoff,
	}, handoff
}

type phaseTwoManagedSessions struct {
	launch.SessionManager
	t *testing.T
}

func (m phaseTwoManagedSessions) Stage(agent skill.Agent, set string) (*session.Session, error) {
	sess, err := m.SessionManager.Stage(agent, set)
	if err == nil {
		// A returning fake handoff leaves the real manager's handles in this process.
		m.t.Cleanup(func() { _ = m.Abort(sess) })
	}
	return sess, err
}

type phaseTwoProbeError struct{ err error }

func (r phaseTwoProbeError) Run(context.Context, proc.Request) (proc.Result, error) {
	return proc.Result{}, r.err
}

func TestIntegrationPhaseTwoApplicationPropagatesBoundedProbeErrors(t *testing.T) {
	if testing.Short() {
		t.Skip("builds configured executable")
	}
	for _, failure := range []error{
		&proc.TimeoutError{Command: "fakeagent", Timeout: time.Second},
		&proc.OutputLimitError{Command: "fakeagent", Stream: "stdout", Limit: 1024},
	} {
		t.Run(failure.Error(), func(t *testing.T) {
			f := newIntegrationFixture(t)
			service, handoff := phaseTwoService(t, f, phaseTwoProbeError{err: failure})
			var chain error
			app := cli.Application{RunLaunch: func(ctx context.Context, req launch.Request, report launch.Reporter) error {
				chain = service.Run(ctx, req, report)
				return chain
			}}
			var stdout, stderr bytes.Buffer
			code := app.Execute([]string{"claude", "-s", "dev"}, &stdout, &stderr, "test")
			if code != 1 || !errors.Is(chain, failure) || !strings.Contains(stderr.String(), failure.Error()) || handoff.called || stdout.Len() != 0 {
				t.Fatalf("code=%d chain=%v handoff=%t stdout=%s stderr=%s", code, chain, handoff.called, &stdout, &stderr)
			}
			assertPhaseTwoNoSession(t, f)
		})
	}
}

type phaseTwoExistingTarget struct {
	launch.SessionManager
	t    *testing.T
	home string
}

func (m phaseTwoExistingTarget) WriteNew(sess *session.Session, files []session.File) error {
	if len(files) == 0 {
		return m.SessionManager.WriteNew(sess, files)
	}
	first := files[0]
	first.Data = []byte("ORIGINAL_TARGET_SENTINEL")
	if err := m.SessionManager.WriteNew(sess, []session.File{first}); err != nil {
		return err
	}
	err := m.SessionManager.WriteNew(sess, files)
	if !errors.Is(err, fs.ErrExist) {
		m.t.Fatalf("exclusive write error=%v", err)
	}
	entries := sessionEntries(m.t, m.home)
	if len(entries) != 1 || !strings.HasPrefix(entries[0].Name(), ".staging-") {
		m.t.Fatalf("staging=%v", entryNames(entries))
	}
	relative, relErr := filepath.Rel(sess.Root, first.Path)
	if relErr != nil {
		m.t.Fatal(relErr)
	}
	data, readErr := os.ReadFile(filepath.Join(m.home, "sessions", entries[0].Name(), relative))
	if readErr != nil || string(data) != "ORIGINAL_TARGET_SENTINEL" {
		m.t.Fatalf("original target=%q err=%v", data, readErr)
	}
	return err
}

func TestIntegrationPhaseTwoApplicationCaseConflictAndExclusiveWrite(t *testing.T) {
	if testing.Short() {
		t.Skip("builds real probe")
	}
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "first projection preserved", true: "existing target preserved"}[existing], func(t *testing.T) {
			f := newIntegrationFixture(t)
			first := skillDocument("Foo") + "FIRST_BODY_SENTINEL"
			phaseTwoWrite(t, filepath.Join(f.home, ".agents", "skills", "Foo", "SKILL.md"), first)
			phaseTwoWrite(t, filepath.Join(f.home, ".codex", "skills", "foo", "SKILL.md"), skillDocument("foo")+"SECOND_BODY_SENTINEL")
			writeFile(t, filepath.Join(f.skopeHome, "skillsets.toml"), "version=1\n[skillsets.dev]\nskills=['Foo','foo']\n")
			service, handoff := phaseTwoService(t, f, proc.Runner{})
			if existing {
				service.Sessions = phaseTwoExistingTarget{SessionManager: service.Sessions, t: t, home: f.skopeHome}
			}
			app := cli.Application{RunLaunch: service.Run}
			var stdout, stderr bytes.Buffer
			code := app.Execute([]string{"claude", "-s", "dev"}, &stdout, &stderr, "test")
			assertPhaseTwoProbe(t, f, 1)
			if existing {
				if code != 1 || handoff.called || stdout.Len() != 0 || stderr.Len() == 0 {
					t.Fatalf("code=%d handoff=%t stdout=%s stderr=%s", code, handoff.called, &stdout, &stderr)
				}
				assertPhaseTwoNoSession(t, f)
				return
			}
			if code != 0 || !handoff.called || !strings.Contains(stdout.String(), "0 native, 1 projected, 1 unavailable, 0 missing") || !strings.Contains(stdout.String(), "unavailable: foo (投影目标路径或名称冲突)") {
				t.Fatalf("code=%d handoff=%t stdout=%s stderr=%s", code, handoff.called, &stdout, &stderr)
			}
			entries := sessionEntries(t, f.skopeHome)
			if len(entries) != 1 || strings.HasPrefix(entries[0].Name(), ".staging-") {
				t.Fatalf("sessions=%v", entryNames(entries))
			}
			dir := filepath.Join(f.skopeHome, "sessions", entries[0].Name(), "claude", "addDir", ".claude", "skills")
			children, err := os.ReadDir(dir)
			if err != nil || len(children) != 1 || children[0].Name() != "Foo" {
				t.Fatalf("targets=%v err=%v", children, err)
			}
			data, err := os.ReadFile(filepath.Join(dir, "Foo", "SKILL.md"))
			if err != nil || string(data) != first {
				t.Fatalf("first projection=%q err=%v", data, err)
			}
		})
	}
}
