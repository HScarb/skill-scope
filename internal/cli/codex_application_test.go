package cli_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
	"github.com/scarb/skope/internal/agent"
	"github.com/scarb/skope/internal/agent/claude"
	"github.com/scarb/skope/internal/agent/codex"
	"github.com/scarb/skope/internal/cli"
	"github.com/scarb/skope/internal/config"
	"github.com/scarb/skope/internal/host"
	"github.com/scarb/skope/internal/launch"
	"github.com/scarb/skope/internal/session"
	"github.com/scarb/skope/internal/skill"
	"github.com/scarb/skope/internal/termsafe"
)

func TestCodexApplicationHelpAndArguments(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"help", "codex"}} {
		var out, stderr bytes.Buffer
		app := cli.Application{RunLaunch: func(context.Context, launch.Request, launch.Reporter) error {
			t.Fatal("help invoked launch dependencies")
			return nil
		}, LoadSkillSets: func() (string, config.SkillSets, error) {
			t.Fatal("help read skill sets")
			return "", config.SkillSets{}, nil
		}}
		if code := app.Execute(args, &out, &stderr, "test"); code != 0 || !strings.Contains(out.String(), "codex") {
			t.Fatalf("code=%d out=%s err=%s", code, &out, &stderr)
		}
	}
	for _, present := range []bool{false, true} {
		var got launch.Request
		app := cli.Application{RunLaunch: func(_ context.Context, req launch.Request, _ launch.Reporter) error {
			got = req
			if !req.SetPresent {
				return launch.ErrSetRequired
			}
			return nil
		}}
		args := []string{"codex"}
		if present {
			args = append(args, "-s", "none", "--", "--help", "", "中文\u202e")
		}
		var out, stderr bytes.Buffer
		code := app.Execute(args, &out, &stderr, "test")
		if got.Agent != skill.AgentCodex || got.SetPresent != present {
			t.Fatalf("request=%+v stderr=%s", got, &stderr)
		}
		if present {
			if code != 0 || !reflect.DeepEqual(got.AgentArgs, []string{"--help", "", "中文\u202e"}) {
				t.Fatalf("code=%d request=%+v", code, got)
			}
		} else if code != 1 || !strings.Contains(stderr.String(), "pass -s <name> or -s none") {
			t.Fatalf("code=%d stderr=%s", code, &stderr)
		}
	}
}

func codexOutputFixture(t *testing.T) launch.Result {
	t.Helper()
	r := launch.Result{Executable: "codex", Session: &session.Session{Root: filepath.Join(t.TempDir(), "preview"), Agent: skill.AgentCodex}, Env: []string{"TOKEN=INHERITED_SECRET"}, Plugins: launch.PluginSummary{Allowed: 1, Disabled: 2},
		Resolved:  skill.Resolved{Agent: skill.AgentCodex, Entries: []skill.Resolution{{ID: "native", State: skill.StateNative}, {ID: "second", State: skill.StateNative}, {ID: "ClaudeOnly", State: skill.StateUnavailable, Reason: skill.ReasonProjectionUnsupported}, {ID: "disabledCodexplugin", State: skill.StateUnavailable, Reason: skill.ReasonPluginDisabled}, {ID: "absent", State: skill.StateMissing}}},
		Inventory: agent.Inventory{PluginIDs: []string{"allowed@m", "disabled@m", "stale@m"}, Warnings: []string{"source warning", "shared canonical path warning"}, Collisions: []skill.Collision{{Kind: skill.CollisionEffectiveName, Agent: skill.AgentCodex, IDs: []string{"native", "second"}, Name: "shared", Paths: []string{"/a/SKILL.md", "/b/SKILL.md"}}}}, Warnings: []error{errors.New("reap: old session retained")}}
	for _, id := range []string{"native", "second", "blocked"} {
		path := filepath.Join(r.Session.Root, "sources", id, "SKILL.md")
		phaseTwoWrite(t, path, skillDocument("shared"))
		canonical, err := filepath.EvalSymlinks(path)
		if err != nil {
			t.Fatal(err)
		}
		r.Inventory.Skills = append(r.Inventory.Skills, skill.Skill{ID: id, Locations: []skill.Location{{Kind: skill.KindSkill, Source: skill.SourceCodex, Level: skill.LevelGlobal, DiscoveryPath: path, RealPath: canonical, Names: map[skill.Agent]string{skill.AgentCodex: "shared"}}}})
	}
	adapter := codex.New(nil, nil, host.OSFileSystem{}, codex.Options{Plugins: []string{"allowed@m"}})
	var err error
	r.Plan, err = adapter.Plan(r.Resolved, r.Inventory, r.Session)
	if err != nil {
		t.Fatal(err)
	}
	r.Args = append(append([]string{}, r.Plan.ControlArgs...), "", "two words")
	return r
}

func executeCodexOutput(t *testing.T, r launch.Result, dry bool) string {
	t.Helper()
	args := []string{"codex", "-s", "dev"}
	if dry {
		args = append(args, "--dry-run")
	}
	var out, stderr bytes.Buffer
	if code := (cli.Application{RunLaunch: reportingRunner(t, r)}).Execute(args, &out, &stderr, "test"); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, &stderr)
	}
	return out.String()
}

func TestCodexOutputSummaryAndDryRun(t *testing.T) {
	for _, dry := range []bool{false, true} {
		t.Run(fmt.Sprint(dry), func(t *testing.T) {
			r := codexOutputFixture(t)
			var decoded struct {
				Plugins map[string]struct{ Enabled bool }
			}
			if err := toml.Unmarshal([]byte(r.Plan.ControlArgs[5]), &decoded); err != nil {
				t.Fatal(err)
			}
			var counts launch.PluginSummary
			for _, plugin := range decoded.Plugins {
				if plugin.Enabled {
					counts.Allowed++
				} else {
					counts.Disabled++
				}
			}
			if counts != r.Plugins {
				t.Fatalf("plan plugin counts=%+v summary=%+v", counts, r.Plugins)
			}
			out := executeCodexOutput(t, r, dry)
			for _, want := range []string{"2 native, 0 projected, 2 unavailable, 1 missing", "所属 Codex plugin 未在 plugins.codex 中允许", "目标 agent 不支持投影", "plugins: 1 allowed, 2 disabled\n", "bundled: off"} {
				if !strings.Contains(out, want) {
					t.Errorf("missing %q: %s", want, out)
				}
			}
			for _, forbidden := range []string{"Claude may enable", "INHERITED_SECRET", "settings.json", "Contents ", "projection"} {
				if strings.Contains(out, forbidden) {
					t.Errorf("unexpected %q: %s", forbidden, out)
				}
			}
			if dry {
				if !strings.HasSuffix(out, "Session files:\n  owner.json\n") {
					t.Errorf("files: %s", out)
				}
				for i, arg := range r.Args {
					if !strings.Contains(out, fmt.Sprintf("  [%d] %s\n", i+1, strconv.Quote(termsafe.Escape(arg)))) {
						t.Errorf("missing argv %d", i)
					}
				}
			}
			name := "codex-summary.golden.txt"
			if dry {
				name = "codex-dry-run.golden.txt"
			}
			root, err := filepath.EvalSymlinks(r.Session.Root)
			if err != nil {
				t.Fatal(err)
			}
			out = strings.ReplaceAll(out, filepath.ToSlash(root), "<SESSION>")
			path := filepath.Join("testdata", name)
			if *updateRenderGoldens {
				if err := os.WriteFile(path, []byte(out), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if out != string(want) {
				t.Errorf("golden mismatch: %s", out)
			}
		})
	}
}

func TestCodexOutputEscapesControlsWithoutMutatingArgv(t *testing.T) {
	r := codexOutputFixture(t)
	const raw = "中文\x1b\n\u202e"
	path := filepath.Join(r.Session.Root, "sources", "中文\u202e", "SKILL.md")
	phaseTwoWrite(t, path, "generated path fixture")
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	r.Inventory.Skills[0].Locations[0].DiscoveryPath = path
	r.Inventory.Skills[0].Locations[0].RealPath = canonical
	r.Plan, err = codex.New(nil, nil, host.OSFileSystem{}, codex.Options{Plugins: []string{"allowed@m"}}).Plan(r.Resolved, r.Inventory, r.Session)
	if err != nil {
		t.Fatal(err)
	}
	r.Args = append([]string{}, r.Plan.ControlArgs...)
	r.Inventory.Warnings = []string{"source " + raw, "shared canonical " + raw}
	r.Inventory.Collisions = []skill.Collision{{Kind: skill.CollisionEffectiveName, Agent: skill.AgentCodex, IDs: []string{raw, "second"}, Name: raw, Paths: []string{raw}}}
	r.Args = append(r.Args, raw)
	before := append([]string{}, r.Args...)
	out := executeCodexOutput(t, r, true)
	assertNoTerminalControls(t, out)
	if strings.Count(out, termsafe.Escape(raw)) < 5 || !reflect.DeepEqual(before, r.Args) {
		t.Fatalf("escaping changed data or missed fields: %s", out)
	}
	if !strings.Contains(out, `中文\\u202e/SKILL.md`) {
		t.Fatalf("generated TOML path was not safely displayed: %s", out)
	}
}

func TestCodexOutputPropagatesWriterFailures(t *testing.T) {
	r := codexOutputFixture(t)
	for _, dry := range []bool{false, true} {
		out := executeCodexOutput(t, r, dry)
		for i, c := range out {
			if c != '\n' {
				continue
			}
			args := []string{"codex", "-s", "dev"}
			if dry {
				args = append(args, "--dry-run")
			}
			var stderr bytes.Buffer
			if code := (cli.Application{RunLaunch: reportingRunner(t, r)}).Execute(args, &limitedOutput{remaining: i}, &stderr, "test"); code != 1 {
				t.Fatalf("code=%d dry=%t at byte %d", code, dry, i)
			}
		}
	}
}

type codexApplicationResolver struct{}

func (codexApplicationResolver) LookPath(command string) (string, error) { return command, nil }

func TestCodexApplicationUsesSelectedPluginsAndRealSources(t *testing.T) {
	for _, dry := range []bool{false, true} {
		t.Run(fmt.Sprint(dry), func(t *testing.T) {
			root := t.TempDir()
			home, repo, skopeHome := filepath.Join(root, "home"), filepath.Join(root, "repo"), filepath.Join(root, "skope")
			phaseTwoWrite(t, filepath.Join(repo, ".git", "HEAD"), "ref: refs/heads/test\n")
			phaseTwoWrite(t, filepath.Join(home, ".agents", "skills", "native", "SKILL.md"), "---\nname: native\ndescription: native skill\n---\n")
			phaseTwoWrite(t, filepath.Join(home, ".claude", "skills", "foreign", "SKILL.md"), skillDocument("foreign"))
			phaseTwoWrite(t, filepath.Join(home, ".claude", "plugins", "installed_plugins.json"), "corrupt Claude metadata must not be read")
			phaseTwoWrite(t, filepath.Join(home, ".codex", "config.toml"), "[plugins.'disabled@m']\nenabled=true\n")
			phaseTwoWrite(t, filepath.Join(skopeHome, "skillsets.toml"), "version=1\n[skillsets.dev]\nskills=['native','foreign','absent']\nplugins.claude=['claude-only@m']\nplugins.codex=['allowed@m']\n[skillsets.extra]\nskills=[]\nplugins.codex=['second@m']\nbundled=true\n")
			fsys := host.OSFileSystem{}
			scanner := skill.Scanner{FS: fsys, RegularFiles: fsys}
			catalog := codex.NewCatalog(fsys, fsys, scanner)
			pathCalls, factoryCalls := 0, 0
			paths := func(env host.Env) (host.CodexPaths, error) {
				pathCalls++
				if env.Cwd() != repo {
					return host.CodexPaths{}, errors.New("path resolver received unexpected working directory")
				}
				return host.CodexPaths{Home: home, CodexHome: filepath.Join(home, ".codex"), AdminSkillRoots: []string{filepath.Join(root, "admin", "skills")}, SystemConfigPaths: []string{filepath.Join(root, "admin", "config.toml")}}, nil
			}
			manager := session.NewManager(skopeHome)
			manager.Processes = phaseTwoProcesses{}
			handoff := &phaseTwoHandoff{}
			service := launch.Service{Env: host.NewEnv(home, repo, map[string]string{"HOME": home, "USERPROFILE": home, "CLAUDE_CONFIG_DIR": filepath.Join(home, ".claude"), "CODEX_HOME": filepath.Join(home, ".codex")}), FS: fsys, SkopeHome: skopeHome, Resolver: codexApplicationResolver{}, Sessions: phaseTwoManagedSessions{SessionManager: manager, t: t}, Handoff: handoff, CheckConflicts: cli.CheckConflicts, Foreign: cli.NewForeignScanner(scanner, catalog, paths)}
			service.NewRegistry = func(executable string, selected config.Selection) (launch.AdapterRegistry, error) {
				factoryCalls++
				return agent.NewRegistry(claude.New(scanner, fsys, phaseTwoProbeError{err: errors.New("unselected Claude probe ran")}, claude.Options{Executable: executable, Plugins: selected.Plugins["claude"], Bundled: selected.Bundled}), codex.New(scanner, catalog, fsys, codex.Options{Plugins: selected.Plugins["codex"], Bundled: selected.Bundled, ResolvePaths: paths}))
			}
			var result launch.Result
			app := cli.Application{RunLaunch: func(ctx context.Context, req launch.Request, report launch.Reporter) error {
				return service.Run(ctx, req, func(r launch.Result) error { result = r; return report(r) })
			}}
			args := []string{"codex", "-s", "dev,extra"}
			if dry {
				args = append(args, "--dry-run")
			}
			var out, stderr bytes.Buffer
			if code := app.Execute(args, &out, &stderr, "test"); code != 0 {
				t.Fatalf("code=%d stderr=%s", code, &stderr)
			}
			if pathCalls != 1 || factoryCalls != 1 || handoff.called == dry || result.Resolved.Agent != skill.AgentCodex || !result.Bundled || result.Plugins != (launch.PluginSummary{Allowed: 2, Disabled: 1}) {
				t.Fatalf("paths=%d factory=%d handoff=%t result=%+v", pathCalls, factoryCalls, handoff.called, result)
			}
			for _, want := range []string{"1 native, 0 projected, 1 unavailable, 1 missing", "plugins: 2 allowed, 1 disabled\n", "bundled: on"} {
				if !strings.Contains(out.String(), want) {
					t.Errorf("missing %q: %s", want, &out)
				}
			}
			var decoded struct {
				Plugins map[string]struct{ Enabled bool }
			}
			if err := toml.Unmarshal([]byte(result.Plan.ControlArgs[5]), &decoded); err != nil {
				t.Fatal(err)
			}
			if len(decoded.Plugins) != 3 || !decoded.Plugins["allowed@m"].Enabled || !decoded.Plugins["second@m"].Enabled || decoded.Plugins["disabled@m"].Enabled {
				t.Fatalf("plugins=%+v", decoded.Plugins)
			}
			if len(result.Plan.Files) != 0 || len(result.ProjectionFiles) != 0 {
				t.Fatalf("unexpected files: %+v", result)
			}
			// None bypasses skill sets and the registry even when local metadata is corrupt.
			phaseTwoWrite(t, filepath.Join(skopeHome, "skillsets.toml"), "invalid = [")
			phaseTwoWrite(t, filepath.Join(home, ".codex", "config.toml"), "invalid = [")
			out.Reset()
			stderr.Reset()
			if code := app.Execute([]string{"codex", "-s", "none", "--dry-run", "--", "--help"}, &out, &stderr, "test"); code != 0 || factoryCalls != 1 || pathCalls != 1 || !reflect.DeepEqual(result.Args, []string{"--help"}) {
				t.Fatalf("none code=%d paths=%d factory=%d args=%q stderr=%s", code, pathCalls, factoryCalls, result.Args, &stderr)
			}
			out.Reset()
			stderr.Reset()
			if code := app.Execute([]string{"codex"}, &out, &stderr, "test"); code != 1 || !strings.Contains(stderr.String(), "pass -s <name> or -s none") || factoryCalls != 1 {
				t.Fatalf("no set code=%d stderr=%s", code, &stderr)
			}
		})
	}
}
