package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
	"github.com/scarb/skope/internal/agent"
	"github.com/scarb/skope/internal/agent/codex"
	"github.com/scarb/skope/internal/cli"
	"github.com/scarb/skope/internal/config"
	"github.com/scarb/skope/internal/host"
	"github.com/scarb/skope/internal/launch"
	"github.com/scarb/skope/internal/projection"
	"github.com/scarb/skope/internal/session"
	"github.com/scarb/skope/internal/skill"
)

const phaseThreeSecret = "SECRET_SENTINEL"

func phaseThreeDocument(name string) string {
	return "---\nname: " + name + "\ndescription: fixture description\n---\n"
}

// This fixture never builds executables or resolves production system sources.
type phaseThreeApplication struct {
	root, home, repo, skopeHome string
	paths                       host.CodexPaths
	service                     launch.Service
	handoff                     *phaseThreeHandoff
	result                      launch.Result
}

type phaseThreeHandoff struct {
	called    bool
	args, env []string
}

func (h *phaseThreeHandoff) Exec(_ string, args, env []string) error {
	h.called = true
	h.args, h.env = append([]string{}, args...), append([]string{}, env...)
	return nil
}

type phaseThreeNoProjection struct{ t *testing.T }

func (p phaseThreeNoProjection) Inspect(context.Context, string) (projection.Manifest, *projection.Rejection, error) {
	p.t.Fatal("Codex called projection inspector")
	return projection.Manifest{}, nil, nil
}
func (p phaseThreeNoProjection) Copy(context.Context, projection.Manifest, projection.Sink) error {
	p.t.Fatal("Codex called projection copier")
	return nil
}

func newPhaseThreeApplication(t *testing.T) *phaseThreeApplication {
	t.Helper()
	root := t.TempDir()
	f := &phaseThreeApplication{root: root, home: filepath.Join(root, "home"), repo: filepath.Join(root, "repo"), skopeHome: filepath.Join(root, "skope"), handoff: &phaseThreeHandoff{}}
	f.paths = host.CodexPaths{Home: f.home, CodexHome: filepath.Join(f.home, ".codex"), AdminSkillRoots: []string{filepath.Join(root, "admin", "skills")}, SystemConfigPaths: []string{filepath.Join(root, "system", "config.toml")}}
	for _, dir := range []string{f.home, f.repo, f.skopeHome, f.paths.CodexHome, filepath.Dir(f.paths.SystemConfigPaths[0]), f.paths.AdminSkillRoots[0]} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	phaseTwoWrite(t, filepath.Join(f.repo, ".git", "HEAD"), "ref: refs/heads/test\n")
	phaseThreeConfig(t, f.skopeHome, "fake-codex", []string{"--from-config", "configured"})
	f.set(t, "[]", "[]")
	fsys := host.OSFileSystem{}
	scanner := skill.Scanner{FS: fsys, RegularFiles: fsys}
	catalog := codex.NewCatalog(fsys, fsys, scanner)
	paths := func(env host.Env) (host.CodexPaths, error) {
		if env.Cwd() != f.repo {
			return host.CodexPaths{}, fmt.Errorf("fixture resolver received unexpected cwd %q", env.Cwd())
		}
		return f.paths, nil
	}
	manager := session.NewManager(f.skopeHome)
	manager.Processes = phaseTwoProcesses{}
	f.service = launch.Service{Env: host.NewEnv(f.home, f.repo, map[string]string{"HOME": f.home, "USERPROFILE": f.home, "CODEX_HOME": f.paths.CodexHome, "CLAUDE_CONFIG_DIR": filepath.Join(f.home, ".claude"), "SKOPE_HOME": f.skopeHome, "AUTH_TOKEN": phaseThreeSecret}), FS: fsys, SkopeHome: f.skopeHome, Resolver: codexApplicationResolver{}, Sessions: phaseTwoManagedSessions{SessionManager: manager, t: t}, Handoff: f.handoff, CheckConflicts: cli.CheckConflicts, Foreign: cli.NewForeignScanner(scanner, catalog, paths), Inspector: phaseThreeNoProjection{t}, Copier: phaseThreeNoProjection{t}}
	f.service.NewRegistry = func(_ string, selected config.Selection) (launch.AdapterRegistry, error) {
		return agent.NewRegistry(codex.New(scanner, catalog, fsys, codex.Options{Plugins: selected.Plugins["codex"], Bundled: selected.Bundled, ResolvePaths: paths}))
	}
	return f
}

func phaseThreeConfig(t *testing.T, home, command string, args []string) {
	t.Helper()
	data, err := toml.Marshal(map[string]any{"version": 1, "agents": map[string]any{"codex": map[string]any{"command": command, "args": args}}})
	if err != nil {
		t.Fatal(err)
	}
	phaseTwoWrite(t, filepath.Join(home, "config.toml"), string(data))
}

func (f *phaseThreeApplication) set(t *testing.T, skills, plugins string) {
	t.Helper()
	phaseTwoWrite(t, filepath.Join(f.skopeHome, "skillsets.toml"), "version=1\n[skillsets.dev]\nbundled=false\nskills="+skills+"\nplugins.codex="+plugins+"\n")
}

func (f *phaseThreeApplication) run(t *testing.T, args ...string) (string, int) {
	t.Helper()
	f.handoff.called = false
	f.result = launch.Result{}
	var out, stderr bytes.Buffer
	app := cli.Application{RunLaunch: func(ctx context.Context, req launch.Request, report launch.Reporter) error {
		return f.service.Run(ctx, req, func(r launch.Result) error { f.result = r; return report(r) })
	}}
	code := app.Execute(args, &out, &stderr, "test")
	return out.String() + stderr.String(), code
}

func phaseThreeCanonical(t *testing.T, path string) string {
	t.Helper()
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.ToSlash(canonical)
}

func phaseThreeAssertControls(t *testing.T, args []string, wantPaths, wantPlugins map[string]bool, bundled bool) {
	t.Helper()
	if len(args) != 8 {
		t.Fatalf("control args=%q", args)
	}
	for i := 0; i < 8; i += 2 {
		if args[i] != "-c" {
			t.Fatalf("control argv[%d]=%q", i, args[i])
		}
	}
	var decoded struct {
		Skills struct {
			Config []struct {
				Path    string
				Enabled bool
			}
			Bundled struct{ Enabled bool }
		}
		Plugins  map[string]struct{ Enabled bool }
		Features struct {
			RemotePlugin bool `toml:"remote_plugin"`
		}
	}
	if err := toml.Unmarshal([]byte(strings.Join([]string{args[1], args[3], args[5], args[7]}, "\n")), &decoded); err != nil {
		t.Fatal(err)
	}
	gotPaths := map[string]bool{}
	for _, row := range decoded.Skills.Config {
		if _, exists := gotPaths[row.Path]; exists {
			t.Fatalf("duplicate path %s", row.Path)
		}
		gotPaths[row.Path] = row.Enabled
	}
	gotPlugins := map[string]bool{}
	for id, p := range decoded.Plugins {
		gotPlugins[id] = p.Enabled
	}
	if !reflect.DeepEqual(gotPaths, wantPaths) || !reflect.DeepEqual(gotPlugins, wantPlugins) || decoded.Skills.Bundled.Enabled != bundled || args[7] != "features.remote_plugin=false" {
		t.Fatalf("paths=%v plugins=%v bundled=%t remote=%s", gotPaths, gotPlugins, decoded.Skills.Bundled.Enabled, args[7])
	}
}

func phaseThreePopulate(t *testing.T, home, repo, codexHome, claudeHome string) map[string]bool {
	t.Helper()
	want := map[string]bool{}
	for _, row := range []struct {
		root, id string
		allow    bool
	}{{filepath.Join(home, ".agents", "skills"), "selected", true}, {filepath.Join(repo, ".agents", "skills"), "selected", true}, {filepath.Join(codexHome, "skills"), "other", false}} {
		path := filepath.Join(row.root, row.id, "SKILL.md")
		phaseTwoWrite(t, path, phaseThreeDocument("same-name")+phaseThreeSecret)
		want[phaseThreeCanonical(t, path)] = row.allow
	}
	for _, id := range []string{"permit", "deny"} {
		root := filepath.Join(codexHome, "plugins", "cache", "market", id, "1.0.0")
		phaseTwoWrite(t, filepath.Join(root, ".codex-plugin", "plugin.json"), `{"name":`+strconv.Quote(id)+`}`)
		for _, name := range []string{id + "-skill", id + "-extra"} {
			path := filepath.Join(root, "skills", name, "SKILL.md")
			phaseTwoWrite(t, path, phaseThreeDocument(name))
			want[phaseThreeCanonical(t, path)] = id == "permit"
		}
	}
	phaseTwoWrite(t, filepath.Join(codexHome, "config.toml"), "[[skills.config]]\nname='same-name'\nenabled=false\n[[skills.config]]\npath="+strconv.Quote(filepath.Join(home, ".agents", "skills", "selected", "SKILL.md"))+"\nenabled=false\n[plugins.'permit@market']\nenabled=false\n[plugins.'deny@market']\nenabled=true\n")
	phaseTwoWrite(t, filepath.Join(claudeHome, "skills", "foreign", "SKILL.md"), skillDocument("foreign"))
	phaseTwoWrite(t, filepath.Join(claudeHome, "commands", "command.md"), "command "+phaseThreeSecret)
	phaseTwoWrite(t, filepath.Join(claudeHome, "plugins", "installed_plugins.json"), "corrupt "+phaseThreeSecret)
	return want
}

func phaseThreeAssertSummary(t *testing.T, out string) {
	t.Helper()
	for _, want := range []string{"1 native, 0 projected, 3 unavailable, 1 missing", "plugins: 2 allowed, 1 disabled", "plugin missing@market is not installed", "bundled: off"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q: %s", want, out)
		}
	}
	for _, bad := range []string{phaseThreeSecret, "settings.json", "--add-dir", "Contents "} {
		if strings.Contains(out, bad) {
			t.Errorf("unexpected %q: %s", bad, out)
		}
	}
}

func phaseThreeSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	result := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			result[path] = "directory"
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		result[path] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestIntegrationPhaseThreeApplicationIsolationMatrix(t *testing.T) {
	for _, dry := range []bool{false, true} {
		t.Run(fmt.Sprint(dry), func(t *testing.T) {
			f := newPhaseThreeApplication(t)
			want := phaseThreePopulate(t, f.home, f.repo, f.paths.CodexHome, filepath.Join(f.home, ".claude"))
			f.set(t, "['selected','foreign','command','deny-skill','absent']", "['permit@market','missing@market']")
			before := phaseThreeSnapshot(t, f.root)
			args := []string{"codex", "-s", "dev"}
			if dry {
				args = append(args, "--dry-run")
			}
			args = append(args, "--", "--from-user", "中文\u202e", "")
			out, code := f.run(t, args...)
			if code != 0 || f.handoff.called == dry {
				t.Fatalf("code=%d handoff=%t %s", code, f.handoff.called, out)
			}
			phaseThreeAssertSummary(t, out)
			wantStates := map[string]skill.ResolutionState{"selected": skill.StateNative, "foreign": skill.StateUnavailable, "command": skill.StateUnavailable, "deny-skill": skill.StateUnavailable, "absent": skill.StateMissing}
			for _, entry := range f.result.Resolved.Entries {
				if entry.State != wantStates[entry.ID] {
					t.Fatalf("resolution=%+v", entry)
				}
				if entry.ID == "deny-skill" && entry.Reason != skill.ReasonPluginDisabled {
					t.Fatalf("plugin reason=%+v", entry)
				}
			}
			phaseThreeAssertControls(t, f.result.Plan.ControlArgs, want, map[string]bool{"permit@market": true, "deny@market": false, "missing@market": true}, false)
			prefix := []string{"--from-config", "configured", "--from-user", "中文\u202e", ""}
			if !reflect.DeepEqual(f.result.Args, append(prefix, f.result.Plan.ControlArgs...)) {
				t.Fatalf("args=%q", f.result.Args)
			}
			assertNoTerminalControls(t, out)
			if len(f.result.ProjectionFiles) != 0 || len(f.result.Plan.Files) != 0 {
				t.Fatal("Codex created projection/settings")
			}
			if dry {
				if !reflect.DeepEqual(before, phaseThreeSnapshot(t, f.root)) {
					t.Fatal("dry run changed filesystem")
				}
				if !strings.HasSuffix(out, "Session files:\n  owner.json\n") {
					t.Fatalf("preview=%s", out)
				}
			} else {
				if !reflect.DeepEqual(f.handoff.args, f.result.Args) || !reflect.DeepEqual(f.handoff.env, f.result.Env) {
					t.Fatal("handoff arguments/environment differ")
				}
				files, err := os.ReadDir(f.result.Session.Root)
				if err != nil || !reflect.DeepEqual(entryNames(files), []string{"owner.json"}) {
					t.Fatalf("session files=%v err=%v", entryNames(files), err)
				}
				data, err := os.ReadFile(filepath.Join(f.result.Session.Root, "owner.json"))
				if err != nil {
					t.Fatal(err)
				}
				var owner session.Owner
				if err := json.Unmarshal(data, &owner); err != nil {
					t.Fatal(err)
				}
				if owner.Agent != skill.AgentCodex || owner.PID != os.Getpid() {
					t.Fatalf("owner=%+v", owner)
				}
			}
		})
	}
}

func TestIntegrationPhaseThreeApplicationAllEmptyAndEmptyInventory(t *testing.T) {
	for _, selection := range []string{"all", "empty", "empty inventory"} {
		t.Run(selection, func(t *testing.T) {
			f := newPhaseThreeApplication(t)
			want := map[string]bool{}
			if selection != "empty inventory" {
				for _, id := range []string{"one", "two"} {
					path := filepath.Join(f.home, ".agents", "skills", id, "SKILL.md")
					phaseTwoWrite(t, path, phaseThreeDocument(id))
					want[phaseThreeCanonical(t, path)] = selection == "all"
				}
			}
			if selection == "all" {
				phaseTwoWrite(t, filepath.Join(f.skopeHome, "skillsets.toml"), "version=1\n[skillsets.dev]\nskills=['one','two']\nbundled=true\n")
			}
			out, code := f.run(t, "codex", "-s", "dev", "--dry-run")
			if code != 0 {
				t.Fatalf("%d %s", code, out)
			}
			phaseThreeAssertControls(t, f.result.Plan.ControlArgs, want, map[string]bool{}, selection == "all")
			if (f.result.Plan.ControlArgs[1] == "skills.config=[]") != (selection == "empty inventory") {
				t.Fatalf("skills config=%s", f.result.Plan.ControlArgs[1])
			}
		})
	}
}

func TestIntegrationPhaseThreeApplicationNoneAndParserPrivacy(t *testing.T) {
	f := newPhaseThreeApplication(t)
	for _, path := range []string{filepath.Join(f.skopeHome, "skillsets.toml"), filepath.Join(f.paths.CodexHome, "config.toml"), filepath.Join(f.paths.CodexHome, "plugins", "cache", "bad")} {
		phaseTwoWrite(t, path, "invalid=["+phaseThreeSecret)
	}
	f.service.NewRegistry = func(string, config.Selection) (launch.AdapterRegistry, error) {
		t.Fatal("none constructed adapter")
		return nil, nil
	}
	for _, dry := range []bool{true, false} {
		args := []string{"codex", "-s", "none"}
		if dry {
			args = append(args, "--dry-run")
		}
		args = append(args, "--", "--help", "", "中文\u202e")
		out, code := f.run(t, args...)
		if code != 0 || f.handoff.called == dry || strings.Contains(out, phaseThreeSecret) {
			t.Fatalf("%d %s", code, out)
		}
		if !reflect.DeepEqual(f.result.Args, []string{"--from-config", "configured", "--help", "", "中文\u202e"}) {
			t.Fatalf("args=%q", f.result.Args)
		}
		if !reflect.DeepEqual(f.result.Env, f.service.Env.Environ()) || (!dry && !reflect.DeepEqual(f.handoff.env, f.service.Env.Environ())) {
			t.Fatal("none changed the inherited environment")
		}
		if len(sessionEntries(t, f.skopeHome)) != 0 {
			t.Fatal("none created sessions")
		}
	}
	phaseTwoWrite(t, filepath.Join(f.skopeHome, "config.toml"), "invalid=["+phaseThreeSecret)
	out, code := f.run(t, "codex", "-s", "none", "--dry-run")
	if code != 1 || !strings.Contains(out, "config.toml") || strings.Contains(out, phaseThreeSecret) || f.handoff.called {
		t.Fatalf("%d %s", code, out)
	}
}

type phaseThreeRetargetSessions struct {
	launch.SessionManager
	retarget func()
}

func (m phaseThreeRetargetSessions) Stage(agent skill.Agent, set string) (*session.Session, error) {
	sess, err := m.SessionManager.Stage(agent, set)
	if err == nil {
		m.retarget()
	}
	return sess, err
}

func TestIntegrationPhaseThreeApplicationCanonicalAliasAndRetarget(t *testing.T) {
	for _, retarget := range []bool{false, true} {
		t.Run(fmt.Sprint(retarget), func(t *testing.T) {
			f := newPhaseThreeApplication(t)
			pluginRoot := filepath.Join(f.paths.CodexHome, "plugins", "cache", "market", "denied", "1.0.0")
			phaseTwoWrite(t, filepath.Join(pluginRoot, ".codex-plugin", "plugin.json"), `{"name":"denied"}`)
			target := filepath.Join(pluginRoot, "skills", "alias")
			phaseTwoWrite(t, filepath.Join(target, "SKILL.md"), phaseThreeDocument("alias"))
			phaseTwoWrite(t, filepath.Join(f.paths.CodexHome, "config.toml"), "[plugins.'denied@market']\nenabled=true\n")
			link := filepath.Join(f.home, ".agents", "skills", "selected")
			if err := os.MkdirAll(filepath.Dir(link), 0700); err != nil {
				t.Fatal(err)
			}
			phaseTwoLink(t, target, link)
			f.set(t, "['selected']", "[]")
			if retarget {
				newTarget := filepath.Join(f.root, "replacement")
				phaseTwoWrite(t, filepath.Join(newTarget, "SKILL.md"), phaseThreeDocument("replacement"))
				f.service.Sessions = phaseThreeRetargetSessions{SessionManager: f.service.Sessions, retarget: func() {
					// Remove only the junction/symlink itself, never its target tree.
					if err := os.Remove(link); err != nil {
						t.Fatal(err)
					}
					phaseTwoLink(t, newTarget, link)
				}}
			}
			out, code := f.run(t, "codex", "-s", "dev")
			if retarget {
				if code != 1 || f.handoff.called || len(sessionEntries(t, f.skopeHome)) != 0 {
					t.Fatalf("retarget code=%d handoff=%t %s", code, f.handoff.called, out)
				}
				if !strings.Contains(out, "changed") {
					t.Fatalf("missing retarget error: %s", out)
				}
				return
			}
			if code != 0 || !f.handoff.called {
				t.Fatalf("%d %s", code, out)
			}
			phaseThreeAssertControls(t, f.result.Plan.ControlArgs, map[string]bool{phaseThreeCanonical(t, filepath.Join(target, "SKILL.md")): true}, map[string]bool{"denied@market": false}, false)
			if !strings.Contains(out, "canonical") {
				t.Fatalf("missing alias warning: %s", out)
			}
		})
	}
}

func TestIntegrationPhaseThreeApplicationConflictsAndSourceErrors(t *testing.T) {
	for _, test := range []struct {
		name         string
		config, user []string
		want         string
	}{
		{"cross boundary", []string{"-c"}, []string{"skills.config=" + phaseThreeSecret}, "value from command-line"},
		{"profile", nil, []string{"--profile", phaseThreeSecret}, "command-line argument --profile"},
		{"cwd", nil, []string{"--cd", phaseThreeSecret}, "command-line argument --cd"},
		{"enable", []string{"--enable", "remote_plugin"}, nil, "config argument --enable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newPhaseThreeApplication(t)
			phaseThreeConfig(t, f.skopeHome, "fake-codex", test.config)
			f.service.NewRegistry = func(string, config.Selection) (launch.AdapterRegistry, error) {
				t.Fatal("conflict constructed registry")
				return nil, nil
			}
			out, code := f.run(t, append([]string{"codex", "-s", "dev", "--"}, test.user...)...)
			if code != 1 || !strings.Contains(out, test.want) || strings.Contains(out, phaseThreeSecret) || f.handoff.called || len(sessionEntries(t, f.skopeHome)) != 0 {
				t.Fatalf("%d %s", code, out)
			}
		})
	}
	for _, kind := range []string{"config", "metadata", "cache"} {
		t.Run(kind, func(t *testing.T) {
			f := newPhaseThreeApplication(t)
			switch kind {
			case "config":
				phaseTwoWrite(t, filepath.Join(f.paths.CodexHome, "config.toml"), "invalid=["+phaseThreeSecret)
			case "metadata":
				phaseTwoWrite(t, filepath.Join(f.home, ".agents", "skills", "bad", "SKILL.md"), "---\nname: ["+phaseThreeSecret+"\n---\n")
			case "cache":
				phaseTwoWrite(t, filepath.Join(f.paths.CodexHome, "config.toml"), "[plugins.'bad@market']\nenabled=true\n")
				phaseTwoWrite(t, filepath.Join(f.paths.CodexHome, "plugins", "cache", "market", "bad", "1.0.0", ".codex-plugin", "plugin.json"), `{"name":`+phaseThreeSecret)
			}
			out, code := f.run(t, "codex", "-s", "dev", "--dry-run")
			if code != 1 || strings.Contains(out, phaseThreeSecret) || f.handoff.called || len(sessionEntries(t, f.skopeHome)) != 0 {
				t.Fatalf("%d %s", code, out)
			}
		})
	}
}

func TestIntegrationPhaseThreeApplicationClaudeProjectsCodexSources(t *testing.T) {
	f := newPhaseThreeApplication(t)
	fixture := &integrationFixture{root: f.root, home: f.home, repo: f.repo, skopeHome: f.skopeHome, claudeConfig: filepath.Join(f.home, ".claude")}
	fixture.writeConfig(t, "fake-claude")
	f.set(t, "['project','admin','entry']", "[]")
	for _, path := range []string{filepath.Join(f.repo, ".agents", "skills", "project", "SKILL.md"), filepath.Join(f.paths.AdminSkillRoots[0], "admin", "SKILL.md")} {
		phaseTwoWrite(t, path, phaseThreeDocument("foreign")+phaseThreeSecret)
	}
	phaseTwoWrite(t, filepath.Join(f.paths.CodexHome, "config.toml"), "[plugins.'p@market']\nenabled=true\n")
	pluginRoot := filepath.Join(f.paths.CodexHome, "plugins", "cache", "market", "p", "1.0.0")
	phaseTwoWrite(t, filepath.Join(pluginRoot, ".codex-plugin", "plugin.json"), `{"name":"p"}`)
	phaseTwoWrite(t, filepath.Join(pluginRoot, "skills", "entry", "SKILL.md"), phaseThreeDocument("entry"))
	service, handoff := phaseTwoService(t, fixture, sourceProbe{})
	service.Resolver = codexApplicationResolver{}
	var result launch.Result
	app := cli.Application{RunLaunch: func(ctx context.Context, req launch.Request, report launch.Reporter) error {
		return service.Run(ctx, req, func(r launch.Result) error { result = r; return report(r) })
	}}
	var out, stderr bytes.Buffer
	if code := app.Execute([]string{"claude", "-s", "dev"}, &out, &stderr, "test"); code != 0 || !handoff.called {
		t.Fatalf("%d %s", code, &stderr)
	}
	if !strings.Contains(out.String(), "0 native, 2 projected, 1 unavailable, 0 missing") || strings.Contains(out.String(), phaseThreeSecret) {
		t.Fatalf("output=%s", &out)
	}
	if result.Resolved.Entries[2].Reason != skill.ReasonPluginOnly || len(result.Inventory.PluginIDs) != 0 {
		t.Fatalf("resolved=%+v inventory=%+v", result.Resolved, result.Inventory)
	}
	for _, id := range []string{"project", "admin"} {
		path := filepath.Join(result.Session.Root, "claude", "addDir", ".claude", "skills", id, "SKILL.md")
		data, err := os.ReadFile(path)
		if err != nil || string(data) != phaseThreeDocument("foreign")+phaseThreeSecret {
			t.Fatalf("projection %s=%q err=%v", id, data, err)
		}
	}
	assertPhaseTwoAbsent(t, filepath.Join(result.Session.Root, "claude", "addDir", ".claude", "skills", "entry"))
}
