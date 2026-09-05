package cli_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/scarb/skope/internal/testutil"
)

func TestIntegrationWithEnvReplacesOverridesWithoutDuplicates(t *testing.T) {
	base := []string{"HOME=/home/test", "PATH=/bin", "KEEP=value", "TARGET=old", "TARGET=older"}
	overrides := map[string]string{"TARGET": "new", "ADDED": "value"}

	got := withEnv(base, overrides)
	want := []string{"HOME=/home/test", "PATH=/bin", "KEEP=value", "ADDED=value", "TARGET=new"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("withEnv() = %q, want %q", got, want)
	}
}

func TestIntegrationClaudeLaunchWritesFinalSessionAndReapsItOnDryRun(t *testing.T) {
	requireUnixIntegration(t)
	fixture := newIntegrationFixture(t)

	output, code := fixture.run(t, []string{"claude", "-s", "dev", "--from-user", "value"}, map[string]string{
		"FAKEAGENT_EXIT": "23",
	})
	if code != 23 {
		t.Fatalf("launch exit code = %d, want 23\n%s", code, output)
	}

	record := readFakeRecord(t, fixture.fakeOutput)
	entries := sessionEntries(t, fixture.skopeHome)
	if len(entries) != 1 || strings.HasPrefix(entries[0].Name(), ".staging-") {
		t.Fatalf("session entries = %v, want one final session", entryNames(entries))
	}
	sessionPath := filepath.Join(fixture.skopeHome, "sessions", entries[0].Name())
	settingsPath := filepath.Join(sessionPath, "claude", "settings.json")
	wantArgs := []string{
		"--from-config", "configured",
		"--from-user", "value",
		"--settings", settingsPath,
	}
	if !reflect.DeepEqual(record.Args, wantArgs) {
		t.Errorf("fake argv = %q, want %q", record.Args, wantArgs)
	}
	assertSameFile(t, record.Cwd, fixture.repo)
	if record.Env["HOME"] != fixture.home {
		t.Errorf("fake HOME = %q, want %q", record.Env["HOME"], fixture.home)
	}
	if record.Env["PATH"] == "" || record.Env["PATH"] != os.Getenv("PATH") {
		t.Errorf("fake PATH = %q, want inherited PATH %q", record.Env["PATH"], os.Getenv("PATH"))
	}

	assertMode(t, sessionPath, 0o700)
	assertMode(t, filepath.Join(sessionPath, "claude"), 0o700)
	ownerPath := filepath.Join(sessionPath, "owner.json")
	assertMode(t, ownerPath, 0o600)
	assertMode(t, settingsPath, 0o600)
	assertOwner(t, ownerPath)
	assertSettings(t, settingsPath)

	marker := []byte("dry-run must not replace this marker")
	if err := os.WriteFile(fixture.fakeOutput, marker, 0o600); err != nil {
		t.Fatal(err)
	}
	dryOutput, dryCode := fixture.run(t, []string{"claude", "-s", "dev", "--dry-run"}, nil)
	if dryCode != 0 {
		t.Fatalf("dry-run exit code = %d, want 0\n%s", dryCode, dryOutput)
	}
	gotMarker, err := os.ReadFile(fixture.fakeOutput)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotMarker, marker) {
		t.Errorf("dry-run changed fake output to %q", gotMarker)
	}
	if entries := sessionEntries(t, fixture.skopeHome); len(entries) != 0 {
		t.Errorf("sessions after dry-run = %v, want none", entryNames(entries))
	}
}

func TestIntegrationNoneIgnoresCorruptSkillSetsWithoutCreatingSession(t *testing.T) {
	requireUnixIntegration(t)
	fixture := newIntegrationFixture(t)
	writeFile(t, filepath.Join(fixture.skopeHome, "skillsets.toml"), "version = [\n")

	output, code := fixture.run(t, []string{"claude", "-s", "none", "--from-user", "value"}, nil)
	if code != 0 {
		t.Fatalf("none launch exit code = %d, want 0\n%s", code, output)
	}
	record := readFakeRecord(t, fixture.fakeOutput)
	wantArgs := []string{"--from-config", "configured", "--from-user", "value"}
	if !reflect.DeepEqual(record.Args, wantArgs) {
		t.Errorf("fake argv = %q, want %q", record.Args, wantArgs)
	}
	for _, arg := range record.Args {
		if arg == "--settings" {
			t.Fatalf("none launch argv contains --settings: %q", record.Args)
		}
	}
	if entries := sessionEntries(t, fixture.skopeHome); len(entries) != 0 {
		t.Errorf("none launch sessions = %v, want none", entryNames(entries))
	}
}

func TestIntegrationLaunchReportsPhaseOneErrors(t *testing.T) {
	requireUnixIntegration(t)
	tests := []struct {
		name      string
		args      []string
		mutate    func(*testing.T, *integrationFixture)
		fragments []string
	}{
		{
			name: "corrupt config even with none",
			args: []string{"claude", "-s", "none"},
			mutate: func(t *testing.T, fixture *integrationFixture) {
				writeFile(t, filepath.Join(fixture.skopeHome, "config.toml"), "version = [\n")
			},
			fragments: []string{"config.toml"},
		},
		{
			name: "missing skillsets",
			args: []string{"claude", "-s", "dev"},
			mutate: func(t *testing.T, fixture *integrationFixture) {
				if err := os.Remove(filepath.Join(fixture.skopeHome, "skillsets.toml")); err != nil {
					t.Fatal(err)
				}
			},
			fragments: []string{"skillsets.toml", "does not exist"},
		},
		{
			name: "corrupt skillsets",
			args: []string{"claude", "-s", "dev"},
			mutate: func(t *testing.T, fixture *integrationFixture) {
				writeFile(t, filepath.Join(fixture.skopeHome, "skillsets.toml"), "version = [\n")
			},
			fragments: []string{"skillsets.toml"},
		},
		{
			name:      "unknown skill set",
			args:      []string{"claude", "-s", "unknown"},
			mutate:    func(*testing.T, *integrationFixture) {},
			fragments: []string{"unknown skill set", "available: dev"},
		},
		{
			name: "missing command",
			args: []string{"claude", "-s", "dev"},
			mutate: func(t *testing.T, fixture *integrationFixture) {
				fixture.writeConfig(t, filepath.Join(fixture.root, "missing-agent-command"))
			},
			fragments: []string{"[agents.claude] command"},
		},
		{
			name:      "missing explicit set",
			args:      []string{"claude"},
			mutate:    func(*testing.T, *integrationFixture) {},
			fragments: []string{"-s <name>", "-s none"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newIntegrationFixture(t)
			test.mutate(t, fixture)

			output, code := fixture.run(t, test.args, nil)
			if code != 1 {
				t.Fatalf("exit code = %d, want 1\n%s", code, output)
			}
			for _, fragment := range test.fragments {
				if !strings.Contains(output, fragment) {
					t.Errorf("output does not contain %q:\n%s", fragment, output)
				}
			}
			if _, err := os.Stat(fixture.fakeOutput); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("fake agent output exists after failed launch: %v", err)
			}
		})
	}
}

func TestIntegrationMissingSkillWarnsAndStillLaunches(t *testing.T) {
	requireUnixIntegration(t)
	fixture := newIntegrationFixture(t)
	writeFile(t, filepath.Join(fixture.skopeHome, "skillsets.toml"), `version = 1

[skillsets.dev]
skills = ["allowed", "missing"]
`)

	output, code := fixture.run(t, []string{"claude", "-s", "dev"}, nil)
	if code != 0 {
		t.Fatalf("launch exit code = %d, want 0\n%s", code, output)
	}
	if !strings.Contains(output, "missing: missing") {
		t.Errorf("launch output does not report the missing skill:\n%s", output)
	}
	readFakeRecord(t, fixture.fakeOutput)
}

type integrationFixture struct {
	root         string
	home         string
	skopeHome    string
	claudeConfig string
	repo         string
	fakeOutput   string
	skope        string
	fakeAgent    string
}

type fakeRecord struct {
	Args []string          `json:"args"`
	Env  map[string]string `json:"env"`
	Cwd  string            `json:"cwd"`
}

func requireUnixIntegration(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test builds binaries; skipped with -short")
	}
	if runtime.GOOS == "windows" {
		t.Skip("Unix exec integration is covered by CI")
	}
}

func newIntegrationFixture(t *testing.T) *integrationFixture {
	t.Helper()
	root := t.TempDir()
	fixture := &integrationFixture{
		root:         root,
		home:         filepath.Join(root, "home"),
		skopeHome:    filepath.Join(root, "skope-home"),
		claudeConfig: filepath.Join(root, "claude-config"),
		repo:         filepath.Join(root, "repo"),
		fakeOutput:   filepath.Join(root, "fakeagent.json"),
		skope:        testutil.BuildSkope(t),
		fakeAgent:    testutil.BuildFakeAgent(t),
	}
	for _, directory := range []string{
		fixture.home,
		fixture.skopeHome,
		filepath.Join(fixture.claudeConfig, "skills", "allowed"),
		filepath.Join(fixture.claudeConfig, "skills", "blocked"),
		filepath.Join(fixture.repo, ".git"),
	} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	fixture.writeConfig(t, fixture.fakeAgent)
	writeFile(t, filepath.Join(fixture.skopeHome, "skillsets.toml"), `version = 1

[skillsets.dev]
skills = ["allowed"]
bundled = false

[skillsets.dev.plugins]
claude = ["sample@market"]
`)
	writeFile(t, filepath.Join(fixture.claudeConfig, "skills", "allowed", "SKILL.md"), skillDocument("allowed"))
	writeFile(t, filepath.Join(fixture.claudeConfig, "skills", "blocked", "SKILL.md"), skillDocument("blocked"))
	writeFile(t, filepath.Join(fixture.claudeConfig, "settings.json"), `{"skillOverrides":{"stale":"on"}}`)
	return fixture
}

func (f *integrationFixture) writeConfig(t *testing.T, command string) {
	t.Helper()
	writeFile(t, filepath.Join(f.skopeHome, "config.toml"), fmt.Sprintf(`version = 1

[agents.claude]
command = %s
args = ["--from-config", "configured"]
`, strconv.Quote(command)))
}

func (f *integrationFixture) run(t *testing.T, args []string, extraEnv map[string]string) (string, int) {
	t.Helper()
	overrides := map[string]string{
		"HOME":              f.home,
		"SKOPE_HOME":        f.skopeHome,
		"CLAUDE_CONFIG_DIR": f.claudeConfig,
		"FAKEAGENT_OUT":     f.fakeOutput,
		"FAKEAGENT_EXIT":    "0",
	}
	for key, value := range extraEnv {
		overrides[key] = value
	}
	cmd := exec.Command(f.skope, args...)
	cmd.Dir = f.repo
	cmd.Env = withEnv(os.Environ(), overrides)
	output, err := cmd.CombinedOutput()
	if err == nil {
		return string(output), 0
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("run skope: %v\n%s", err, output)
	}
	return string(output), exitErr.ExitCode()
}

func skillDocument(name string) string {
	return "---\nname: " + name + "\n---\n# " + name + "\n"
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readFakeRecord(t *testing.T, path string) fakeRecord {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var record fakeRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatalf("decode fake agent record: %v\n%s", err, raw)
	}
	return record
}

func sessionEntries(t *testing.T, skopeHome string) []os.DirEntry {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(skopeHome, "sessions"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func entryNames(entries []os.DirEntry) []string {
	names := make([]string, len(entries))
	for index, entry := range entries {
		names[index] = entry.Name()
	}
	return names
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("mode for %s = %#o, want %#o", path, got, want)
	}
}

func assertSameFile(t *testing.T, gotPath, wantPath string) {
	t.Helper()
	got, err := os.Stat(gotPath)
	if err != nil {
		t.Fatalf("stat got path %q: %v", gotPath, err)
	}
	want, err := os.Stat(wantPath)
	if err != nil {
		t.Fatalf("stat want path %q: %v", wantPath, err)
	}
	if !os.SameFile(got, want) {
		t.Errorf("paths identify different files: got %q, want %q", gotPath, wantPath)
	}
}

func assertOwner(t *testing.T, path string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var owner struct {
		PID          int    `json:"pid"`
		ProcessStart string `json:"processStart"`
		Agent        string `json:"agent"`
		SkillSet     string `json:"skillSet"`
		CreatedAt    string `json:"createdAt"`
	}
	if err := json.Unmarshal(raw, &owner); err != nil {
		t.Fatalf("decode owner: %v\n%s", err, raw)
	}
	if owner.PID <= 0 || owner.ProcessStart == "" || owner.Agent != "claude" || owner.SkillSet != "dev" || owner.CreatedAt == "" {
		t.Errorf("owner = %#v, want complete Claude dev ownership", owner)
	}
}

func assertSettings(t *testing.T, path string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]json.RawMessage
	if err := json.Unmarshal(raw, &settings); err != nil {
		t.Fatalf("decode settings: %v\n%s", err, raw)
	}
	if len(settings) != 1 {
		t.Errorf("settings keys = %v, want only skillOverrides", reflect.ValueOf(settings).MapKeys())
	}
	for _, forbidden := range []string{"enabledPlugins", "disableBundledSkills"} {
		if _, exists := settings[forbidden]; exists {
			t.Errorf("settings unexpectedly contains %s", forbidden)
		}
	}
	var overrides map[string]string
	if err := json.Unmarshal(settings["skillOverrides"], &overrides); err != nil {
		t.Fatalf("decode skillOverrides: %v", err)
	}
	want := map[string]string{"allowed": "on", "blocked": "off", "stale": "off"}
	if !reflect.DeepEqual(overrides, want) {
		t.Errorf("skillOverrides = %#v, want %#v", overrides, want)
	}
}

func withEnv(base []string, overrides map[string]string) []string {
	environ := make([]string, 0, len(base)+len(overrides))
	for _, entry := range base {
		key, _, ok := strings.Cut(entry, "=")
		if _, replaced := overrides[key]; ok && replaced {
			continue
		}
		environ = append(environ, entry)
	}
	keys := make([]string, 0, len(overrides))
	for key := range overrides {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		environ = append(environ, key+"="+overrides[key])
	}
	return environ
}
