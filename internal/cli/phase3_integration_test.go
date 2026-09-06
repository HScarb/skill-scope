package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/scarb/skope/internal/session"
	"github.com/scarb/skope/internal/skill"
)

func newPhaseThreeBinary(t *testing.T) *integrationFixture {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test builds binaries; skipped with -short")
	}
	f := newIntegrationFixture(t)
	phaseThreeConfig(t, f.skopeHome, f.fakeAgent, []string{"--from-config", "configured"})
	return f
}

func TestIntegrationPhaseThreeBinaryIsolationAndReaping(t *testing.T) {
	requireUnixIntegration(t)
	f := newPhaseThreeBinary(t)
	want := phaseThreePopulate(t, f.home, f.repo, filepath.Join(f.home, ".codex"), f.claudeConfig)
	phaseTwoWrite(t, filepath.Join(f.skopeHome, "skillsets.toml"), "version=1\n[skillsets.dev]\nskills=['selected','foreign','command','deny-skill','absent']\nplugins.codex=['permit@market','missing@market']\nbundled=false\n")
	out, code := f.run(t, []string{"codex", "-s", "dev", "--", "--from-user", "中文\u202e", ""}, map[string]string{"AUTH_TOKEN": phaseThreeSecret, "FAKEAGENT_EXIT": "23"})
	if code != 23 {
		t.Fatalf("%d %s", code, out)
	}
	phaseThreeAssertSummary(t, out)
	r := readFakeRecord(t, f.fakeOutput)
	wantPrefix := []string{"--from-config", "configured", "--from-user", "中文\u202e", ""}
	if len(r.Args) != len(wantPrefix)+8 || !reflect.DeepEqual(r.Args[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("argv=%q", r.Args)
	}
	phaseThreeAssertControls(t, r.Args[len(wantPrefix):], want, map[string]bool{"permit@market": true, "deny@market": false, "missing@market": true}, false)
	assertSameFile(t, r.Cwd, f.repo)
	for key, want := range map[string]string{"HOME": f.home, "USERPROFILE": f.home, "SKOPE_HOME": f.skopeHome, "CODEX_HOME": filepath.Join(f.home, ".codex"), "CLAUDE_CONFIG_DIR": f.claudeConfig, "AUTH_TOKEN": phaseThreeSecret} {
		if r.Env[key] != want {
			t.Fatalf("environment %s=%q want %q", key, r.Env[key], want)
		}
	}
	assertPhaseTwoAbsent(t, filepath.Join(f.root, "probe.jsonl"))
	entries := sessionEntries(t, f.skopeHome)
	if len(entries) != 1 {
		t.Fatalf("sessions=%v", entryNames(entries))
	}
	root := filepath.Join(f.skopeHome, "sessions", entries[0].Name())
	files, err := os.ReadDir(root)
	if err != nil || !reflect.DeepEqual(entryNames(files), []string{"owner.json"}) {
		t.Fatalf("files=%v err=%v", entryNames(files), err)
	}
	data, err := os.ReadFile(filepath.Join(root, "owner.json"))
	if err != nil {
		t.Fatal(err)
	}
	var owner session.Owner
	if err := json.Unmarshal(data, &owner); err != nil {
		t.Fatal(err)
	}
	var recorded struct{ PID int }
	data, err = os.ReadFile(f.fakeOutput)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &recorded); err != nil {
		t.Fatal(err)
	}
	if owner.Agent != skill.AgentCodex || owner.PID <= 0 || owner.PID != recorded.PID || owner.ProcessStart == "" {
		t.Fatalf("owner=%+v fake PID=%d", owner, recorded.PID)
	}
	before := phaseThreeSnapshot(t, filepath.Join(f.home, ".codex"))
	out, code = f.run(t, []string{"codex", "-s", "dev", "--dry-run"}, nil)
	if code != 0 {
		t.Fatalf("%d %s", code, out)
	}
	phaseThreeAssertSummary(t, out)
	assertPhaseTwoNoSession(t, f)
	if !reflect.DeepEqual(before, phaseThreeSnapshot(t, filepath.Join(f.home, ".codex"))) {
		t.Fatal("dry run changed Codex files")
	}
	after, err := os.ReadFile(f.fakeOutput)
	if err != nil || !bytes.Equal(data, after) {
		t.Fatalf("dry run changed fake output: %v", err)
	}
	if !strings.HasSuffix(out, "Session files:\n  owner.json\n") {
		t.Fatalf("dry run=%s", out)
	}
}

func TestIntegrationPhaseThreeBinaryNoneHelpAndBrokenSources(t *testing.T) {
	for _, mode := range []string{"help", "dry none", "none"} {
		t.Run(mode, func(t *testing.T) {
			if mode == "none" {
				requireUnixIntegration(t)
			}
			f := newPhaseThreeBinary(t)
			for _, path := range []string{filepath.Join(f.skopeHome, "skillsets.toml"), filepath.Join(f.home, ".codex", "config.toml"), filepath.Join(f.home, ".codex", "plugins", "cache", "broken")} {
				phaseTwoWrite(t, path, "invalid=["+phaseThreeSecret)
			}
			args := []string{"codex", "-s", "none"}
			if mode == "dry none" {
				args = append(args, "--dry-run")
			}
			args = append(args, "--", "--help", "", "中文\u202e")
			if mode == "help" {
				args = []string{"help", "codex"}
			}
			out, code := f.run(t, args, map[string]string{"AUTH_TOKEN": phaseThreeSecret})
			if code != 0 || strings.Contains(out, phaseThreeSecret) {
				t.Fatalf("%d %s", code, out)
			}
			assertPhaseTwoNoSession(t, f)
			assertPhaseTwoAbsent(t, filepath.Join(f.root, "probe.jsonl"))
			if mode == "none" {
				r := readFakeRecord(t, f.fakeOutput)
				if !reflect.DeepEqual(r.Args, []string{"--from-config", "configured", "--help", "", "中文\u202e"}) {
					t.Fatalf("argv=%q", r.Args)
				}
				assertSameFile(t, r.Cwd, f.repo)
				if r.Env["AUTH_TOKEN"] != phaseThreeSecret || r.Env["CODEX_HOME"] != filepath.Join(f.home, ".codex") {
					t.Fatal("none changed environment")
				}
				if err := os.Remove(f.fakeOutput); err != nil {
					t.Fatal(err)
				}
			} else {
				assertPhaseTwoAbsent(t, f.fakeOutput)
			}
			if mode != "help" {
				phaseTwoWrite(t, filepath.Join(f.skopeHome, "config.toml"), "invalid=["+phaseThreeSecret)
				out, code = f.run(t, args, nil)
				if code != 1 || !strings.Contains(out, "config.toml") || strings.Contains(out, phaseThreeSecret) {
					t.Fatalf("%d %s", code, out)
				}
				assertPhaseTwoAbsent(t, f.fakeOutput)
			}
		})
	}
}

func TestIntegrationPhaseThreeBinaryConflictsDoNotStartFake(t *testing.T) {
	for _, test := range []struct {
		name         string
		config, user []string
		source       string
	}{
		{"cross boundary", []string{"-c"}, []string{"skills.config=" + phaseThreeSecret}, "config"},
		{"profile", nil, []string{"--profile", phaseThreeSecret}, "command-line"},
		{"cwd", nil, []string{"--cd", phaseThreeSecret}, "command-line"},
		{"enable", []string{"--enable", "remote_plugin"}, nil, "config"},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newPhaseThreeBinary(t)
			phaseThreeConfig(t, f.skopeHome, f.fakeAgent, test.config)
			out, code := f.run(t, append([]string{"codex", "-s", "dev", "--"}, test.user...), nil)
			if code != 1 || !strings.Contains(out, test.source) || strings.Contains(out, phaseThreeSecret) {
				t.Fatalf("%d %s", code, out)
			}
			if test.name == "cross boundary" && !strings.Contains(out, "command-line") {
				t.Fatalf("missing value source: %s", out)
			}
			assertPhaseTwoAbsent(t, f.fakeOutput)
			assertPhaseTwoAbsent(t, filepath.Join(f.root, "probe.jsonl"))
			assertPhaseTwoNoSession(t, f)
		})
	}
}

func TestIntegrationPhaseThreeBinaryCanonicalLink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("binary active Codex system paths cannot be redirected on Windows; application junction coverage runs here")
	}
	f := newPhaseThreeBinary(t)
	root := filepath.Join(f.root, "linked-source")
	path := filepath.Join(root, "SKILL.md")
	phaseTwoWrite(t, path, phaseThreeDocument("linked"))
	link := filepath.Join(f.home, ".agents", "skills", "linked")
	if err := os.MkdirAll(filepath.Dir(link), 0700); err != nil {
		t.Fatal(err)
	}
	phaseTwoLink(t, root, link)
	phaseTwoWrite(t, filepath.Join(f.skopeHome, "skillsets.toml"), "version=1\n[skillsets.dev]\nskills=['linked']\nbundled=false\n")
	out, code := f.run(t, []string{"codex", "-s", "dev"}, nil)
	if code != 0 {
		t.Fatalf("%d %s", code, out)
	}
	r := readFakeRecord(t, f.fakeOutput)
	phaseThreeAssertControls(t, r.Args[2:], map[string]bool{phaseThreeCanonical(t, path): true}, map[string]bool{}, false)
}
