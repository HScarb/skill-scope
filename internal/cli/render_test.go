package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/scarb/skope/internal/agent"
	"github.com/scarb/skope/internal/agent/claude"
	"github.com/scarb/skope/internal/cli"
	"github.com/scarb/skope/internal/config"
	"github.com/scarb/skope/internal/launch"
	"github.com/scarb/skope/internal/session"
	"github.com/scarb/skope/internal/skill"
)

var updateRenderGoldens = flag.Bool("update", false, "update CLI render golden files")

func renderFixture(t *testing.T) launch.Result {
	t.Helper()
	sess := &session.Session{Root: filepath.Join(t.TempDir(), "preview"), Agent: skill.AgentClaude}
	r := launch.Result{
		Executable: "claude", Session: sess, Env: []string{"AUTH_TOKEN=SECRET_SENTINEL"},
		Resolved: skill.Resolved{Agent: skill.AgentClaude, Entries: []skill.Resolution{
			{ID: "native", State: skill.StateNative, Names: []string{"native"}},
			{ID: "foreign", State: skill.StateProjected, Names: []string{"foreign"}},
			{ID: "unsafe", State: skill.StateUnavailable, Reason: skill.ReasonOutsideRoot},
			{ID: "absent", State: skill.StateMissing},
		}},
		Inventory: agent.Inventory{SkillNames: []string{"native", "blocked"}, PluginIDs: []string{"allowed@m", "disabled@m", "stale@m"},
			Warnings: []string{"allowed plugin missing@m is not installed"},
			Collisions: []skill.Collision{
				{Kind: skill.CollisionDifferentContent, IDs: []string{"native"}, Paths: []string{"/a/SKILL.md", "/b/SKILL.md"}},
				{Kind: skill.CollisionFrontmatterName, IDs: []string{"foreign"}, Name: "declared", Paths: []string{"/foreign/SKILL.md"}},
				{Kind: skill.CollisionEffectiveName, IDs: []string{"native", "foreign"}, Agent: skill.AgentClaude, Name: "shared", Paths: []string{"/a/SKILL.md", "/foreign/SKILL.md"}},
			}},
		Warnings: []error{errors.New("reap: old session retained"), errors.New("projection: source warning")},
		Plugins:  launch.PluginSummary{Allowed: 2, Disabled: 2},
	}
	adapter := claude.New(nil, nil, nil, claude.Options{Plugins: []string{"allowed@m", "missing@m"}})
	var err error
	r.Plan, err = adapter.Plan(r.Resolved, r.Inventory, sess)
	if err != nil {
		t.Fatal(err)
	}
	r.Args = append(append([]string{}, r.Plan.ControlArgs...), "", "two words")
	r.ProjectionFiles = []launch.ProjectionFile{{ID: "foreign", Path: sess.AgentPath("addDir", ".claude", "skills", "foreign", "refs", "note.txt")}, {ID: "foreign", Path: sess.AgentPath("addDir", ".claude", "skills", "foreign", "SKILL.md")}}
	return r
}

func executeRender(t *testing.T, r launch.Result, dry bool) string {
	t.Helper()
	args := []string{"claude", "-s", "dev"}
	if r.NoIsolation {
		args[2] = "none"
	}
	if dry {
		args = append(args, "--dry-run")
	}
	var out, stderr bytes.Buffer
	if code := (cli.Application{RunLaunch: reportingRunner(t, r)}).Execute(args, &out, &stderr, "test"); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, &stderr)
	}
	return out.String()
}

func TestRenderCompleteSummaryAndDryRun(t *testing.T) {
	for _, dry := range []bool{false, true} {
		t.Run(fmt.Sprint(dry), func(t *testing.T) {
			r := renderFixture(t)
			out := executeRender(t, r, dry)
			for _, want := range []string{"1 native, 1 projected, 1 unavailable, 1 missing", "unavailable: unsafe (引用目录外内容)", "plugins: 2 allowed, 2 disabled (Claude may enable dependencies of allowed plugins)", "bundled: off"} {
				if !strings.Contains(out, want) {
					t.Errorf("missing %q:\n%s", want, out)
				}
			}
			var settings struct {
				EnabledPlugins       map[string]bool
				DisableBundledSkills bool
				SkillOverrides       map[string]string
			}
			if err := json.Unmarshal(r.Plan.Files[0].Data, &settings); err != nil {
				t.Fatal(err)
			}
			counts := launch.PluginSummary{}
			for _, enabled := range settings.EnabledPlugins {
				if enabled {
					counts.Allowed++
				} else {
					counts.Disabled++
				}
			}
			if counts != r.Plugins || !settings.DisableBundledSkills || settings.SkillOverrides["foreign"] != "on" || settings.SkillOverrides["blocked"] != "off" {
				t.Fatalf("settings disagree: %+v", settings)
			}
			if strings.Contains(out, "SECRET_SENTINEL") || dry && strings.Contains(out, "Launching") {
				t.Fatalf("unsafe output: %s", out)
			}
			out = strings.ReplaceAll(out, strings.ReplaceAll(r.Session.Root, `\`, `\\`), "<SESSION>")
			out = strings.ReplaceAll(out, r.Session.Root, "<SESSION>")
			out = strings.ReplaceAll(out, `\\`, "/")
			name := "claude-summary.golden.txt"
			if dry {
				name = "claude-dry-run.golden.txt"
			}
			path := filepath.Join("testdata", name)
			if *updateRenderGoldens {
				if err := os.MkdirAll("testdata", 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(out), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if out != string(want) {
				t.Errorf("golden %s mismatch:\n%s", name, out)
			}
		})
	}
}

func TestRenderDryRunShowsOnlySafePlannedChanges(t *testing.T) {
	r := renderFixture(t)
	r.Plan.Env = map[string]string{"Z\x1b": "普通\n\u202e", "API_KEY": "SECRET_SENTINEL", "OPENCODE_CONFIG_CONTENT": "BODY_SENTINEL", "A": "changed"}
	r.Plan.Files = append(r.Plan.Files, agent.PlannedFile{Path: r.ProjectionFiles[0].Path, Data: []byte("BODY_SENTINEL")}, agent.PlannedFile{Path: filepath.Join(r.Session.Root, "owner.json"), Data: []byte("OWNER_SENTINEL")})
	r.Args = append(r.Args, "\x1b]0;evil\a\n\t\u009b\u202e")
	r.Executable = "claude\x1b"
	beforeArgs := append([]string{}, r.Args...)
	beforeData := append([]byte{}, r.Plan.Files[0].Data...)
	out := executeRender(t, r, true)
	for _, forbidden := range []string{"SECRET_SENTINEL", "BODY_SENTINEL", "OWNER_SENTINEL", "\x1b", "\u009b", "\u202e"} {
		if strings.Contains(out, forbidden) {
			t.Errorf("leaked %q: %s", forbidden, out)
		}
	}
	for _, want := range []string{"Environment changes:\n  A=changed\n  API_KEY=<redacted>\n  OPENCODE_CONFIG_CONTENT=<redacted>\n  Z\\x1b=普通\\x0a\\u202e\n", "Executable: claude\\x1b", "  owner.json\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q: %s", want, out)
		}
	}
	if strings.Count(out, "  claude/addDir/.claude/skills/foreign/refs/note.txt\n") != 1 {
		t.Errorf("duplicate/missing projection path: %s", out)
	}
	if !reflect.DeepEqual(beforeArgs, r.Args) || !bytes.Equal(beforeData, r.Plan.Files[0].Data) {
		t.Fatal("render mutated execution data")
	}
}

func TestRenderTerminalControlsAcrossOutputBoundaries(t *testing.T) {
	const raw = "中文\x1b]0;evil\a\n\t\u009b\u061c\u200e\u200f\u2028\u2029\u202a\u202b\u202c\u202d\u202e\u2066\u2067\u2068\u2069"
	const safe = `中文\x1b]0;evil\x07\x0a\x09\x9b\u061c\u200e\u200f\u2028\u2029\u202a\u202b\u202c\u202d\u202e\u2066\u2067\u2068\u2069`
	cases := []struct {
		name    string
		args    []string
		app     cli.Application
		version string
		want    string
		code    int
	}{
		{"version", []string{"version"}, cli.Application{}, raw, "skope " + safe + "\n", 0},
		{"missing config", []string{"list"}, cli.Application{LoadSkillSets: func() (string, config.SkillSets, error) { return raw, config.SkillSets{}, nil }}, "", safe + "\n暂无配置\n", 0},
		{"list", []string{"list"}, cli.Application{LoadSkillSets: func() (string, config.SkillSets, error) {
			return "", config.SkillSets{Exists: true, Items: []config.SkillSet{{Name: raw, Description: raw}}}, nil
		}}, "", safe, 0},
		{"error", []string{"claude", "-s", "dev"}, cli.Application{RunLaunch: func(context.Context, launch.Request, launch.Reporter) error { return errors.New(raw) }}, "", "Error: launch claude: " + safe + "\n", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out, stderr bytes.Buffer
			code := tc.app.Execute(tc.args, &out, &stderr, tc.version)
			combined := out.String() + stderr.String()
			if code != tc.code || !strings.Contains(combined, tc.want) {
				t.Errorf("code=%d output=%q want=%q", code, combined, tc.want)
			}
			assertNoTerminalControls(t, combined)
		})
	}
	r := renderFixture(t)
	r.Warnings = []error{errors.New(raw)}
	r.Inventory.Warnings = []string{raw}
	r.Inventory.Collisions = []skill.Collision{{Kind: skill.CollisionKind(raw), IDs: []string{raw}, Agent: skill.Agent(raw), Name: raw, Paths: []string{raw}}}
	r.Resolved.Entries = []skill.Resolution{{ID: raw, State: skill.StateUnavailable, Reason: skill.ResolutionReason(raw)}, {ID: raw, State: skill.StateMissing}}
	r.Plan.Files[0].Data = []byte("{\n  \"" + raw + "\": true\n}\n")
	before := append([]byte{}, r.Plan.Files[0].Data...)
	out := executeRender(t, r, true)
	assertNoTerminalControls(t, out)
	if strings.Count(out, safe) < 9 {
		t.Errorf("not all fields escaped: %q", out)
	}
	if !bytes.Equal(before, r.Plan.Files[0].Data) {
		t.Fatal("JSON bytes mutated")
	}
}

func assertNoTerminalControls(t *testing.T, text string) {
	t.Helper()
	for _, r := range text {
		if r != '\n' && (r <= 0x1f || r >= 0x7f && r <= 0x9f || r == 0x061c || r >= 0x200e && r <= 0x200f || r >= 0x2028 && r <= 0x202e || r >= 0x2066 && r <= 0x2069) {
			t.Errorf("raw control U+%04X in %q", r, text)
			return
		}
	}
}

func TestRenderCobraErrorsUseSingleSafeExit(t *testing.T) {
	for _, args := range [][]string{{"unknown\u202e"}, {"--unknown\u202e"}} {
		var out, stderr bytes.Buffer
		if code := (cli.Application{}).Execute(args, &out, &stderr, ""); code != 1 {
			t.Fatalf("code=%d", code)
		}
		if strings.Count(stderr.String(), "Error:") != 1 {
			t.Errorf("stderr=%q", stderr.String())
		}
		assertNoTerminalControls(t, stderr.String())
	}
}

func TestRenderWriterFailuresReturnOneIncludingHelp(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"help", "claude"}, {"version"}, {"list"}, {"claude", "-s", "dev", "--dry-run"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var stderr bytes.Buffer
			app := cli.Application{LoadSkillSets: func() (string, config.SkillSets, error) { return "", config.SkillSets{Exists: true}, nil }, RunLaunch: reportingRunner(t, renderFixture(t))}
			if code := app.Execute(args, errorWriter{}, &stderr, ""); code != 1 {
				t.Errorf("code=%d stderr=%s", code, &stderr)
			}
		})
	}
}

func TestRenderNoneDryRunHasNoSessionOrInheritedEnvironment(t *testing.T) {
	r := launch.Result{NoIsolation: true, Executable: "claude", Env: []string{"AUTH_TOKEN=SECRET_SENTINEL"}, Warnings: []error{errors.New("reap\x1b")}}
	out := executeRender(t, r, true)
	for _, want := range []string{"Skillset [none]", "no isolation", `warning: reap\x1b`, "Environment changes:\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q: %s", want, out)
		}
	}
	for _, unwanted := range []string{"Session files:", "Contents", "SECRET_SENTINEL", "Launching", "skills:", "plugins:", "bundled:"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("unexpected %q: %s", unwanted, out)
		}
	}
}

func TestRenderGeneratedJSONEscapesUnicodeWithoutChangingPlan(t *testing.T) {
	r := renderFixture(t)
	r.Resolved.Entries = []skill.Resolution{{ID: "bad\u202e", State: skill.StateNative, Names: []string{"bad\u202e"}}}
	adapter := claude.New(nil, nil, nil, claude.Options{})
	var err error
	r.Plan, err = adapter.Plan(r.Resolved, r.Inventory, r.Session)
	if err != nil {
		t.Fatal(err)
	}
	before := append([]byte{}, r.Plan.Files[0].Data...)
	if !json.Valid(before) || !bytes.Contains(before, []byte("\u202e")) {
		t.Fatalf("fixture must contain valid JSON with raw bidi: %q", before)
	}
	out := executeRender(t, r, true)
	if !strings.Contains(out, `"bad\u202e": "on"`) {
		t.Errorf("missing safely displayed settings: %s", out)
	}
	assertNoTerminalControls(t, out)
	if !bytes.Equal(before, r.Plan.Files[0].Data) {
		t.Fatal("plan JSON changed")
	}
}

func TestRenderUnavailableReasons(t *testing.T) {
	cases := []struct {
		reason skill.ResolutionReason
		text   string
	}{
		{skill.ReasonSpecialFile, "包含非普通文件"}, {skill.ReasonLimitExceeded, "超过文件数或字节数限额"},
		{skill.ReasonProjectionUnsupported, "目标 agent 不支持投影"}, {skill.ReasonPluginDisabled, "所属 Claude plugin 未在 plugins.claude 中允许"},
		{skill.ReasonPluginOnly, "仅存在于其他 agent 的 plugin 中"}, {skill.ReasonCommandOnly, "command 不能投影为 skill"},
		{skill.ReasonOutsideRoot, "引用目录外内容"}, {skill.ReasonTargetConflict, "投影目标路径或名称冲突"},
		{skill.ReasonSymlinkLoop, "符号链接循环或层级过深"}, {skill.ReasonPluginManifest, "包含 plugin 清单，不能作为普通 skill 投影"},
		{skill.ReasonInvalidPath, "投影路径无效"}, {"unknown\u202e", `unknown\u202e`},
	}
	for _, tc := range cases {
		t.Run(string(tc.reason), func(t *testing.T) {
			r := launch.Result{Resolved: skill.Resolved{Entries: []skill.Resolution{{ID: "id", State: skill.StateUnavailable, Reason: tc.reason}}}}
			out := executeRender(t, r, false)
			if !strings.Contains(out, "unavailable: id ("+tc.text+")\n") {
				t.Errorf("output=%s", out)
			}
		})
	}
}

func TestRenderRejectsInvalidProjectionAndPlanPaths(t *testing.T) {
	for _, source := range []string{"plan", "projection"} {
		for _, kind := range []string{"no session", "relative", "outside", "root", "relative root"} {
			t.Run(source+"/"+kind, func(t *testing.T) {
				r := renderFixture(t)
				path := r.Session.AgentPath("asset")
				switch kind {
				case "no session":
					r.Session = nil
				case "relative":
					path = "asset"
				case "outside":
					path = filepath.Join(r.Session.Root, "..", "outside\u202e")
				case "root":
					path = r.Session.Root
				case "relative root":
					r.Session.Root = "relative"
					path = filepath.Join("relative", "asset")
				}
				r.Plan.Files = nil
				r.ProjectionFiles = nil
				if source == "plan" {
					r.Plan.Files = []agent.PlannedFile{{Path: path, Data: []byte("BODY_SENTINEL")}}
				} else {
					r.ProjectionFiles = []launch.ProjectionFile{{Path: path}}
				}
				var out, stderr bytes.Buffer
				code := (cli.Application{RunLaunch: reportingRunner(t, r)}).Execute([]string{"claude", "-s", "dev", "--dry-run"}, &out, &stderr, "")
				if code != 1 {
					t.Errorf("code=%d output=%s stderr=%s", code, &out, &stderr)
				}
				assertNoTerminalControls(t, stderr.String())
				if strings.Contains(out.String(), "BODY_SENTINEL") {
					t.Fatal("body leaked")
				}
			})
		}
	}
}

func TestRenderEscapesSelectionAndSessionFileNames(t *testing.T) {
	r := renderFixture(t)
	r.ProjectionFiles = append(r.ProjectionFiles, launch.ProjectionFile{Path: filepath.Join(r.Session.Root, "asset\x1b\n\t\u009b\u202e")})
	var out, stderr bytes.Buffer
	code := (cli.Application{RunLaunch: reportingRunner(t, r)}).Execute([]string{"claude", "-s", "dev\x1b\u202e", "--dry-run"}, &out, &stderr, "")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, &stderr)
	}
	for _, want := range []string{`Skillset [dev\x1b\u202e]`, `asset\x1b\x0a\x09\x9b\u202e`} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q: %s", want, &out)
		}
	}
	assertNoTerminalControls(t, out.String())
}

type limitedOutput struct{ remaining int }

func (w *limitedOutput) Write(p []byte) (int, error) {
	if len(p) > w.remaining {
		n := w.remaining
		w.remaining = 0
		return n, io.ErrClosedPipe
	}
	w.remaining -= len(p)
	return len(p), nil
}

func TestRenderPropagatesFailureAtEveryOutputLine(t *testing.T) {
	r := renderFixture(t)
	r.Plan.Env = map[string]string{"HOME": "changed"}
	for _, dry := range []bool{false, true} {
		out := executeRender(t, r, dry)
		for i, c := range out {
			if c != '\n' {
				continue
			}
			t.Run(fmt.Sprintf("%t/%d", dry, i), func(t *testing.T) {
				args := []string{"claude", "-s", "dev"}
				if dry {
					args = append(args, "--dry-run")
				}
				var stderr bytes.Buffer
				code := (cli.Application{RunLaunch: reportingRunner(t, r)}).Execute(args, &limitedOutput{remaining: i}, &stderr, "")
				if code != 1 {
					t.Fatalf("code=%d at byte %d", code, i)
				}
			})
		}
	}
}
