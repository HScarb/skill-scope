package cli_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

const phaseTwoBody = "BODY_SENTINEL_DO_NOT_RENDER"

type phaseTwoFixture struct {
	*integrationFixture
	plugins string
	files   map[string]string
}

func newPhaseTwoFixture(t *testing.T) *phaseTwoFixture {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test builds binaries; skipped with -short")
	}
	f := &phaseTwoFixture{integrationFixture: newIntegrationFixture(t)}
	f.files = map[string]string{
		"SKILL.md":       skillDocument("foreign") + phaseTwoBody,
		"refs/note.md":   "resource " + phaseTwoBody,
		"scripts/run.sh": "#!/bin/sh\nprintf fixture\n",
		"bin/data.bin":   "\x00\x01\xff\x7f",
	}
	for name, body := range f.files {
		phaseTwoWrite(t, filepath.Join(f.home, ".agents", "skills", "foreign", filepath.FromSlash(name)), body)
	}
	if err := os.MkdirAll(filepath.Join(f.home, ".agents", "skills", "foreign", "empty"), 0o700); err != nil {
		t.Fatal(err)
	}
	// The same ID in two sources produces a real cross-layer content collision.
	phaseTwoWrite(t, filepath.Join(f.home, ".agents", "skills", "allowed", "SKILL.md"), skillDocument("allowed")+phaseTwoBody)
	phaseTwoWrite(t, filepath.Join(f.home, ".agents", "skills", "rejected", "SKILL.md"), skillDocument("rejected"))
	phaseTwoWrite(t, filepath.Join(f.root, "outside", "private.txt"), phaseTwoBody)
	phaseTwoLink(t, filepath.Join(f.root, "outside"), filepath.Join(f.home, ".agents", "skills", "rejected", "escape"))
	var rows []map[string]any
	for _, name := range []string{"permit", "deny"} {
		root := filepath.Join(f.root, "installed", name)
		phaseTwoWrite(t, filepath.Join(root, ".claude-plugin", "plugin.json"), `{"name":`+strconv.Quote(name)+`}`)
		phaseTwoWrite(t, filepath.Join(root, "skills", name+"-skill", "SKILL.md"), skillDocument(name+"-skill"))
		rows = append(rows, map[string]any{"id": name + "@market", "enabled": true, "scope": "user", "installPath": root})
	}
	data, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	f.plugins = string(data)
	phaseTwoWrite(t, filepath.Join(f.claudeConfig, "plugins", "known_marketplaces.json"), `{"market":{"source":{"source":"git"},"installLocation":`+strconv.Quote(filepath.Join(f.root, "installed"))+`}}`)
	writeFile(t, filepath.Join(f.claudeConfig, "settings.json"), `{"skillOverrides":{"stale":"on"},"enabledPlugins":{"stale@market":true}}`)
	writeFile(t, filepath.Join(f.skopeHome, "skillsets.toml"), `version = 1
[skillsets.dev]
skills = ["allowed", "foreign", "rejected", "unknown", "permit-skill", "deny-skill"]
bundled = false
[skillsets.dev.plugins]
claude = ["permit@market", "missing@market"]
`)
	return f
}

func phaseTwoWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, body)
}

func phaseTwoLink(t *testing.T, target, path string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		if output, err := exec.Command("cmd", "/c", "mklink", "/J", path, target).CombinedOutput(); err != nil {
			t.Fatalf("create fixture junction: %v\n%s", err, output)
		}
	} else if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
}

func (f *phaseTwoFixture) runPhaseTwo(t *testing.T, args []string, extra map[string]string) (string, int) {
	t.Helper()
	env := map[string]string{"FAKEAGENT_PLUGIN_JSON": f.plugins, "AUTH_TOKEN": "INHERITED_AUTH_SENTINEL"}
	for key, value := range extra {
		env[key] = value
	}
	// Keep all binary launches behind the shared system-source isolation guard.
	return f.run(t, args, env)
}

func assertPhaseTwoSummary(t *testing.T, output string) {
	t.Helper()
	for _, fragment := range []string{
		"skills: 2 native, 1 projected, 2 unavailable, 1 missing",
		"unavailable: rejected (引用目录外内容)",
		"unavailable: deny-skill (所属 Claude plugin 未在 plugins.claude 中允许)",
		"missing: unknown", "plugins: 2 allowed, 2 disabled", "bundled: off",
		"warning: plugin missing@market is not installed", "collision different-content: IDs=allowed",
	} {
		if !strings.Contains(output, fragment) {
			t.Errorf("missing %q in output:\n%s", fragment, output)
		}
	}
	for _, secret := range []string{phaseTwoBody, "INHERITED_AUTH_SENTINEL", ".staging-"} {
		if strings.Contains(output, secret) {
			t.Errorf("unexpected %q in output:\n%s", secret, output)
		}
	}
}

func assertPhaseTwoProbe(t *testing.T, f *integrationFixture, count int) []fakeRecord {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(f.root, "probe.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
	if len(lines) != count {
		t.Fatalf("probe calls=%d want=%d", len(lines), count)
	}
	records := make([]fakeRecord, len(lines))
	for i, line := range lines {
		if err := json.Unmarshal(line, &records[i]); err != nil {
			t.Fatal(err)
		}
		r := records[i]
		if !reflect.DeepEqual(r.Args, []string{"plugin", "list", "--json"}) {
			t.Errorf("probe args=%q", r.Args)
		}
		assertSameFile(t, r.Cwd, f.repo)
		for key, want := range map[string]string{"HOME": f.home, "USERPROFILE": f.home, "CLAUDE_CONFIG_DIR": f.claudeConfig, "CODEX_HOME": filepath.Join(f.home, ".codex"), "SKOPE_HOME": f.skopeHome} {
			assertSameFile(t, r.Env[key], want)
		}
	}
	return records
}

func assertPhaseTwoSettings(t *testing.T, data []byte) {
	t.Helper()
	var settings struct {
		SkillOverrides       map[string]string `json:"skillOverrides"`
		EnabledPlugins       map[string]bool   `json:"enabledPlugins"`
		DisableBundledSkills bool              `json:"disableBundledSkills"`
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("settings=%s: %v", data, err)
	}
	// Plugin skills are controlled at plugin granularity, not by skillOverrides.
	wantSkills := map[string]string{"allowed": "on", "foreign": "on", "blocked": "off", "stale": "off"}
	wantPlugins := map[string]bool{"permit@market": true, "missing@market": true, "deny@market": false, "stale@market": false}
	if !reflect.DeepEqual(settings.SkillOverrides, wantSkills) || !reflect.DeepEqual(settings.EnabledPlugins, wantPlugins) || !settings.DisableBundledSkills {
		t.Fatalf("settings=%s want skills=%v plugins=%v bundled disabled", data, wantSkills, wantPlugins)
	}
	allowed, disabled := 0, 0
	for _, enabled := range settings.EnabledPlugins {
		if enabled {
			allowed++
		} else {
			disabled++
		}
	}
	if allowed != 2 || disabled != 2 {
		t.Fatalf("settings plugin counts=%d/%d disagree with summary", allowed, disabled)
	}
}

func TestIntegrationPhaseTwoDryRunInspectsCompleteInventoryWithoutWriting(t *testing.T) {
	f := newPhaseTwoFixture(t)
	writeFile(t, f.fakeOutput, "preserve-final-output")
	output, code := f.runPhaseTwo(t, []string{"claude", "-s", "dev", "--dry-run", "--from-user", "value"}, nil)
	if code != 0 {
		t.Fatalf("code=%d\n%s", code, output)
	}
	assertPhaseTwoSummary(t, output)
	assertPhaseTwoProbe(t, f.integrationFixture, 1)
	for _, fragment := range []string{f.fakeAgent, "--from-config", "configured", "--from-user", "value", "--settings", "--add-dir", "owner.json", "claude/settings.json", "claude/addDir/.claude/skills/foreign/bin/data.bin"} {
		if !strings.Contains(output, fragment) {
			t.Errorf("missing %q:\n%s", fragment, output)
		}
	}
	_, data, ok := strings.Cut(output, "Contents claude/settings.json:\n")
	if !ok {
		t.Fatalf("missing settings contents:\n%s", output)
	}
	assertPhaseTwoSettings(t, []byte(data))
	assertPhaseTwoNoSession(t, f.integrationFixture)
	got, err := os.ReadFile(f.fakeOutput)
	if err != nil || string(got) != "preserve-final-output" {
		t.Fatalf("final output=%q err=%v", got, err)
	}
}

func TestIntegrationPhaseTwoLaunchCopiesCompleteTreeAndReapsExitedOwner(t *testing.T) {
	requireUnixIntegration(t)
	f := newPhaseTwoFixture(t)
	output, code := f.runPhaseTwo(t, []string{"claude", "-s", "dev", "--from-user", "value"}, map[string]string{"FAKEAGENT_EXIT": "23"})
	if code != 23 {
		t.Fatalf("code=%d\n%s", code, output)
	}
	assertPhaseTwoSummary(t, output)
	probe := assertPhaseTwoProbe(t, f.integrationFixture, 1)[0]
	r := readFakeRecord(t, f.fakeOutput)
	assertSameFile(t, r.Cwd, probe.Cwd)
	if !reflect.DeepEqual(r.Env, probe.Env) {
		t.Fatal("probe and handoff inherited different environments")
	}
	entries := sessionEntries(t, f.skopeHome)
	if len(entries) != 1 || strings.HasPrefix(entries[0].Name(), ".staging-") {
		t.Fatalf("sessions=%v", entryNames(entries))
	}
	root := filepath.Join(f.skopeHome, "sessions", entries[0].Name())
	settingsPath := filepath.Join(root, "claude", "settings.json")
	addDir := filepath.Join(root, "claude", "addDir")
	wantArgs := []string{"--from-config", "configured", "--from-user", "value", "--settings", settingsPath, "--add-dir", addDir}
	if !reflect.DeepEqual(r.Args, wantArgs) {
		t.Fatalf("argv=%q want=%q", r.Args, wantArgs)
	}
	assertOwner(t, filepath.Join(root, "owner.json"))
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	assertPhaseTwoSettings(t, data)
	for name, body := range f.files {
		path := filepath.Join(addDir, ".claude", "skills", "foreign", filepath.FromSlash(name))
		data, err := os.ReadFile(path)
		if err != nil || string(data) != body {
			t.Fatalf("copied %s=%q err=%v", name, data, err)
		}
	}
	empty, err := os.ReadDir(filepath.Join(addDir, ".claude", "skills", "foreign", "empty"))
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty directory=%v err=%v", empty, err)
	}
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			t.Errorf("session contains link %s", path)
		}
		if entry.IsDir() {
			assertMode(t, path, 0o700)
		} else {
			assertMode(t, path, 0o600)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"rejected", "unknown", "deny-skill"} {
		assertPhaseTwoAbsent(t, filepath.Join(addDir, ".claude", "skills", name))
	}
	before, err := os.ReadFile(f.fakeOutput)
	if err != nil {
		t.Fatal(err)
	}
	output, code = f.runPhaseTwo(t, []string{"claude", "-s", "dev", "--dry-run"}, nil)
	if code != 0 {
		t.Fatalf("reaping dry-run code=%d\n%s", code, output)
	}
	assertPhaseTwoSummary(t, output)
	assertPhaseTwoProbe(t, f.integrationFixture, 2)
	assertPhaseTwoNoSession(t, f.integrationFixture)
	after, err := os.ReadFile(f.fakeOutput)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("dry-run changed handoff log: %v", err)
	}
}

func assertPhaseTwoNoSession(t *testing.T, f *integrationFixture) {
	t.Helper()
	if entries := sessionEntries(t, f.skopeHome); len(entries) != 0 {
		t.Fatalf("unexpected sessions=%v", entryNames(entries))
	}
}

func assertPhaseTwoAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unexpected path %s: %v", path, err)
	}
}

func TestIntegrationPhaseTwoConflictsStopBeforeProbeAndHideValues(t *testing.T) {
	if testing.Short() {
		t.Skip("builds binaries")
	}
	for _, flag := range []string{"--settings", "--setting-sources", "--plugin-dir", "--plugin-url", "--add-dir"} {
		for _, source := range []string{"config", "user"} {
			for _, equals := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/equals=%t", flag, source, equals), func(t *testing.T) {
					f := newIntegrationFixture(t)
					args := []string{flag, "SECRET_VALUE_SENTINEL"}
					if equals {
						args = []string{flag + "=SECRET_VALUE_SENTINEL"}
					}
					cliArgs := []string{"claude", "-s", "dev"}
					if source == "config" {
						var quoted []string
						for _, arg := range args {
							quoted = append(quoted, strconv.Quote(arg))
						}
						writeFile(t, filepath.Join(f.skopeHome, "config.toml"), "version=1\n[agents.claude]\ncommand="+strconv.Quote(f.fakeAgent)+"\nargs=["+strings.Join(quoted, ",")+"]\n")
					} else {
						cliArgs = append(cliArgs, args...)
					}
					output, code := f.run(t, cliArgs, nil)
					if code != 1 || !strings.Contains(output, flag) || strings.Contains(output, "SECRET_VALUE_SENTINEL") {
						t.Fatalf("code=%d\n%s", code, output)
					}
					assertPhaseTwoNoSession(t, f)
					assertPhaseTwoAbsent(t, f.fakeOutput)
					assertPhaseTwoAbsent(t, filepath.Join(f.root, "probe.jsonl"))
				})
			}
		}
	}
}

const phaseTwoSuppressed = `[{"id":"(suppressed)@skills-dir","version":"unknown","scope":"project","enabled":false,"installPath":"","notes":["PRIVATE_NOTES_SENTINEL"]}]`

func TestIntegrationPhaseTwoPluginFailuresDoNotCreateSessions(t *testing.T) {
	if testing.Short() {
		t.Skip("builds binaries")
	}
	for _, test := range []struct{ name, data, exit, message string }{
		{"exit", "[]", "17", "claude plugin list"},
		{"invalid JSON", "[{", "0", "invalid JSON"},
		{"invalid field", `[{"id":"p@m","enabled":"PRIVATE_NOTES_SENTINEL"}]`, "0", "field enabled is invalid"},
		{"suppressed", phaseTwoSuppressed, "0", "complete workspace trust in Claude independently"},
	} {
		for _, dry := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/dry=%t", test.name, dry), func(t *testing.T) {
				f := newIntegrationFixture(t)
				args := []string{"claude", "-s", "dev"}
				if dry {
					args = append(args, "--dry-run")
				}
				output, code := f.run(t, args, map[string]string{"FAKEAGENT_PLUGIN_JSON": test.data, "FAKEAGENT_PLUGIN_EXIT": test.exit})
				if code != 1 || !strings.Contains(output, test.message) || strings.Contains(output, "PRIVATE_NOTES_SENTINEL") {
					t.Fatalf("code=%d\n%s", code, output)
				}
				assertPhaseTwoProbe(t, f, 1)
				assertPhaseTwoNoSession(t, f)
				assertPhaseTwoAbsent(t, f.fakeOutput)
			})
		}
	}
}

func TestIntegrationPhaseTwoNoneBypassesBrokenInventoryAndStillValidatesConfig(t *testing.T) {
	if testing.Short() {
		t.Skip("builds binaries")
	}
	for _, test := range []struct{ name, data string }{{"invalid JSON", "invalid plugin JSON"}, {"suppressed", phaseTwoSuppressed}} {
		for _, dry := range []bool{true, false} {
			if !dry && runtime.GOOS == "windows" {
				continue
			}
			t.Run(fmt.Sprintf("%s/dry=%t", test.name, dry), func(t *testing.T) {
				f := newIntegrationFixture(t)
				writeFile(t, filepath.Join(f.skopeHome, "skillsets.toml"), "version=[\n")
				phaseTwoWrite(t, filepath.Join(f.home, ".agents", "skills", "broken", "SKILL.md"), "---\nname: [\n---\n")
				args := []string{"claude", "-s", "none"}
				if dry {
					args = append(args, "--dry-run")
				}
				args = append(args, "--from-user", "value")
				output, code := f.run(t, args, map[string]string{"FAKEAGENT_PLUGIN_JSON": test.data})
				if code != 0 || !strings.Contains(output, "no isolation") {
					t.Fatalf("code=%d\n%s", code, output)
				}
				assertPhaseTwoAbsent(t, filepath.Join(f.root, "probe.jsonl"))
				assertPhaseTwoNoSession(t, f)
				if dry {
					assertPhaseTwoAbsent(t, f.fakeOutput)
				} else {
					r := readFakeRecord(t, f.fakeOutput)
					if !reflect.DeepEqual(r.Args, []string{"--from-config", "configured", "--from-user", "value"}) {
						t.Fatalf("argv=%q", r.Args)
					}
					if err := os.Remove(f.fakeOutput); err != nil {
						t.Fatal(err)
					}
				}
				writeFile(t, filepath.Join(f.skopeHome, "config.toml"), "version=[\n")
				output, code = f.run(t, args, nil)
				if code != 1 || !strings.Contains(output, "config.toml") {
					t.Fatalf("invalid config code=%d\n%s", code, output)
				}
				assertPhaseTwoAbsent(t, filepath.Join(f.root, "probe.jsonl"))
				assertPhaseTwoAbsent(t, f.fakeOutput)
				assertPhaseTwoNoSession(t, f)
			})
		}
	}
}

func TestIntegrationPhaseTwoNoProjectionAvoidsAddDirectory(t *testing.T) {
	if testing.Short() {
		t.Skip("builds binaries")
	}
	for _, dry := range []bool{true, false} {
		if !dry && runtime.GOOS == "windows" {
			continue
		}
		t.Run(fmt.Sprintf("dry=%t", dry), func(t *testing.T) {
			f := newIntegrationFixture(t)
			args := []string{"claude", "-s", "dev"}
			if dry {
				args = append(args, "--dry-run")
			}
			output, code := f.run(t, args, nil)
			if code != 0 || strings.Contains(output, "--add-dir") || !strings.Contains(output, "plugins: 1 allowed, 0 disabled") || !strings.Contains(output, "plugin sample@market is not installed") {
				t.Fatalf("code=%d\n%s", code, output)
			}
			assertPhaseTwoProbe(t, f, 1)
			if dry {
				assertPhaseTwoNoSession(t, f)
				return
			}
			r := readFakeRecord(t, f.fakeOutput)
			for _, arg := range r.Args {
				if arg == "--add-dir" {
					t.Fatalf("args=%q", r.Args)
				}
			}
			for _, entry := range sessionEntries(t, f.skopeHome) {
				assertPhaseTwoAbsent(t, filepath.Join(f.skopeHome, "sessions", entry.Name(), "claude", "addDir"))
				assertSettings(t, filepath.Join(f.skopeHome, "sessions", entry.Name(), "claude", "settings.json"))
			}
		})
	}
}

func TestIntegrationPhaseTwoCaseConflictPreservesFirstProjection(t *testing.T) {
	if testing.Short() {
		t.Skip("builds binaries")
	}
	for _, dry := range []bool{true, false} {
		if !dry && runtime.GOOS == "windows" {
			continue
		}
		t.Run(fmt.Sprintf("dry=%t", dry), func(t *testing.T) {
			f := newIntegrationFixture(t)
			first := skillDocument("Foo") + "FIRST_BODY_SENTINEL"
			phaseTwoWrite(t, filepath.Join(f.home, ".agents", "skills", "Foo", "SKILL.md"), first)
			phaseTwoWrite(t, filepath.Join(f.home, ".codex", "skills", "foo", "SKILL.md"), skillDocument("foo")+"SECOND_BODY_SENTINEL")
			writeFile(t, filepath.Join(f.skopeHome, "skillsets.toml"), "version=1\n[skillsets.dev]\nskills=['Foo','foo']\n")
			args := []string{"claude", "-s", "dev"}
			if dry {
				args = append(args, "--dry-run")
			}
			output, code := f.run(t, args, nil)
			if code != 0 || !strings.Contains(output, "0 native, 1 projected, 1 unavailable, 0 missing") || !strings.Contains(output, "unavailable: foo (投影目标路径或名称冲突)") || strings.Contains(output, "BODY_SENTINEL") {
				t.Fatalf("code=%d\n%s", code, output)
			}
			assertPhaseTwoProbe(t, f, 1)
			if dry {
				assertPhaseTwoNoSession(t, f)
				return
			}
			entries := sessionEntries(t, f.skopeHome)
			if len(entries) != 1 {
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

func TestIntegrationPhaseTwoForeignOversizeAndFIFOAreUnavailable(t *testing.T) {
	if testing.Short() {
		t.Skip("builds binaries")
	}
	for _, kind := range []string{"oversize", "fifo"} {
		t.Run(kind, func(t *testing.T) {
			if kind == "fifo" && runtime.GOOS == "windows" {
				t.Skip("FIFO is Unix only")
			}
			f := newIntegrationFixture(t)
			path := filepath.Join(f.home, ".agents", "skills", "rejected", "SKILL.md")
			phaseTwoWrite(t, path, "")
			if kind == "oversize" {
				if err := os.Truncate(path, (20<<20)+1); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if out, err := exec.Command("mkfifo", path).CombinedOutput(); err != nil {
					t.Fatalf("mkfifo: %v %s", err, out)
				}
			}
			writeFile(t, filepath.Join(f.skopeHome, "skillsets.toml"), "version=1\n[skillsets.dev]\nskills=['rejected']\n")
			// fixture.run uses CommandContext: even a blocking FIFO regression is killed.
			output, code := f.run(t, []string{"claude", "-s", "dev", "--dry-run"}, nil)
			reason := "超过文件数或字节数限额"
			if kind == "fifo" {
				reason = "包含非普通文件"
			}
			if code != 0 || !strings.Contains(output, "0 native, 0 projected, 1 unavailable, 0 missing") || !strings.Contains(output, "unavailable: rejected ("+reason+")") || strings.Contains(output, "--add-dir") {
				t.Fatalf("code=%d\n%s", code, output)
			}
			assertPhaseTwoProbe(t, f, 1)
			assertPhaseTwoNoSession(t, f)
			assertPhaseTwoAbsent(t, f.fakeOutput)
		})
	}
}
