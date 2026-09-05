package testutil_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/scarb/skope/internal/testutil"
)

func TestFakeAgentPluginOutputAndExit(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary; skipped with -short")
	}
	bin := testutil.BuildFakeAgent(t)
	for _, tc := range []struct {
		name string
		env  []string
		out  string
		exit int
	}{
		{name: "default", out: "[]"},
		{name: "raw JSON", env: []string{"FAKEAGENT_PLUGIN_JSON={invalid\n"}, out: "{invalid\n"},
		{name: "explicit empty", env: []string{"FAKEAGENT_PLUGIN_JSON="}},
		{name: "probe exit", env: []string{"FAKEAGENT_PLUGIN_EXIT=7", "FAKEAGENT_EXIT=23"}, out: "[]", exit: 7},
		{name: "ignore launch exit", env: []string{"FAKEAGENT_EXIT=23"}, out: "[]"},
		{name: "invalid probe exit", env: []string{"FAKEAGENT_PLUGIN_EXIT=bad"}, exit: 98},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(bin, "plugin", "list", "--json")
			cmd.Env = append(fakeAgentEnv(t), tc.env...)
			out, err := cmd.Output()
			assertFakeAgentExit(t, err, tc.exit)
			if string(out) != tc.out {
				t.Errorf("stdout = %q, want %q", out, tc.out)
			}
		})
	}
}

func TestFakeAgentPluginLogAppendsWithoutChangingLaunchRecord(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary; skipped with -short")
	}
	bin := testutil.BuildFakeAgent(t)
	dir := t.TempDir()
	outPath := filepath.Join(dir, "launch.json")
	logPath := filepath.Join(dir, "probe.jsonl")
	env := append(fakeAgentEnv(t), "FAKEAGENT_OUT="+outPath, "FAKEAGENT_EXIT=23", "FAKEAGENT_PROBE_LOG="+logPath)
	launch := exec.Command(bin, "--flag", "value")
	launch.Env, launch.Dir = env, dir
	assertFakeAgentExit(t, launch.Run(), 23)
	before, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(logPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ordinary launch created probe log: %v", err)
	}
	args := []string{"plugin", "list", "--json"}
	for range 2 {
		cmd := exec.Command(bin, args...)
		cmd.Env, cmd.Dir = env, dir
		if err := cmd.Run(); err != nil {
			t.Fatalf("probe: %v", err)
		}
	}
	after, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("probe changed ordinary launch record")
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Count(log, []byte("\n")) != 2 {
		t.Fatal("probe log must have exactly two JSON lines")
	}
	decoder := json.NewDecoder(bytes.NewReader(log))
	wantDir, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		var rec struct {
			Args []string          `json:"args"`
			Env  map[string]string `json:"env"`
			Cwd  string            `json:"cwd"`
		}
		if err := decoder.Decode(&rec); err != nil {
			t.Fatalf("decode probe log: %v", err)
		}
		if !reflect.DeepEqual(rec.Args, args) {
			t.Errorf("recorded args = %v, want %v", rec.Args, args)
		}
		gotDir, err := os.Stat(rec.Cwd)
		if err != nil {
			t.Fatalf("stat recorded cwd: %v", err)
		}
		if !os.SameFile(gotDir, wantDir) {
			t.Errorf("recorded cwd = %q, want %q", rec.Cwd, dir)
		}
		if rec.Env["FAKEAGENT_EXIT"] != "23" || rec.Env["CLAUDE_CONFIG_DIR"] == "" {
			t.Error("probe did not record its supplied environment")
		}
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		t.Fatalf("expected end of probe log, got %v", err)
	}
}

func TestFakeAgentPluginRequiresExactArguments(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary; skipped with -short")
	}
	bin := testutil.BuildFakeAgent(t)
	for _, args := range [][]string{
		{"plugin", "list"},
		{"plugin", "list", "--json", "extra"},
		{"plugin", "show", "--json"},
		{"plugins", "list", "--json"},
		{"plugin", "list", "--JSON"},
	} {
		cmd := exec.Command(bin, args...)
		outPath := filepath.Join(t.TempDir(), "launch.json")
		cmd.Env = append(fakeAgentEnv(t), "FAKEAGENT_OUT="+outPath, "FAKEAGENT_EXIT=23", "FAKEAGENT_PLUGIN_EXIT=7")
		out, err := cmd.Output()
		assertFakeAgentExit(t, err, 23)
		if len(out) != 0 {
			t.Errorf("ordinary launch stdout = %q, want empty", out)
		}
		if _, err := os.Stat(outPath); err != nil {
			t.Fatalf("ordinary launch record missing: %v", err)
		}
	}
}

func fakeAgentEnv(t *testing.T) []string {
	t.Helper()
	home := t.TempDir()
	env := []string{"HOME=" + home, "USERPROFILE=" + home, "CLAUDE_CONFIG_DIR=" + home}
	if systemRoot := os.Getenv("SystemRoot"); systemRoot != "" {
		env = append(env, "SystemRoot="+systemRoot)
	}
	return env
}

func assertFakeAgentExit(t *testing.T, err error, want int) {
	t.Helper()
	if want == 0 {
		if err != nil {
			t.Fatalf("expected successful exit, got %v", err)
		}
		return
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != want {
		t.Fatalf("expected exit code %d, got %v", want, err)
	}
}
