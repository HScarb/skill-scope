package testutil_test

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/scarb/skope/internal/testutil"
)

func TestBuildFakeAgentProducesRunnableBinary(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary; skipped with -short")
	}

	bin := testutil.BuildFakeAgent(t)

	if _, err := os.Stat(bin); err != nil {
		t.Fatalf("binary not found at %s: %v", bin, err)
	}

	outFile := filepath.Join(t.TempDir(), "out.json")
	cmd := exec.Command(bin, "--flag", "value")
	cmd.Env = append(os.Environ(),
		"FAKEAGENT_OUT="+outFile,
		"FAKEAGENT_EXIT=3",
	)
	err := cmd.Run()

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 3 {
		t.Fatalf("expected exit code 3, got err=%v", err)
	}

	raw, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read record: %v", err)
	}
	var rec struct {
		Args []string          `json:"args"`
		Env  map[string]string `json:"env"`
		Cwd  string            `json:"cwd"`
	}
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatalf("unmarshal record: %v\n%s", err, raw)
	}
	if len(rec.Args) != 2 || rec.Args[0] != "--flag" || rec.Args[1] != "value" {
		t.Errorf("args = %v, want [--flag value]", rec.Args)
	}
	if rec.Env["FAKEAGENT_EXIT"] != "3" {
		t.Errorf("env FAKEAGENT_EXIT = %q, want 3", rec.Env["FAKEAGENT_EXIT"])
	}
	if rec.Cwd == "" {
		t.Error("cwd is empty")
	}
}

func TestBuildFakeAgentReturnsSamePathWithinOneTest(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary; skipped with -short")
	}

	first := testutil.BuildFakeAgent(t)
	second := testutil.BuildFakeAgent(t)

	if first != second {
		t.Errorf("expected cached path, got %q then %q", first, second)
	}
}

func TestBuildSkopeProducesRunnableBinary(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary; skipped with -short")
	}

	bin := testutil.BuildSkope(t)
	cmd := exec.Command(bin, "version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run skope version: %v\n%s", err, output)
	}
	if got, want := string(output), "skope dev\n"; got != want {
		t.Errorf("skope version output = %q, want %q", got, want)
	}
}
