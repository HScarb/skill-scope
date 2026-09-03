package cli_test

import (
	"bytes"
	"testing"

	"github.com/scarb/skope/internal/cli"
)

func TestVersionPrintsInjectedVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := cli.Execute([]string{"version"}, &stdout, &stderr, "1.2.3")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	if got, want := stdout.String(), "skope 1.2.3\n"; got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
}

func TestVersionAppearsInHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer

	cli.Execute([]string{"--help"}, &stdout, &stderr, "test")

	if !bytes.Contains(stdout.Bytes(), []byte("version")) {
		t.Errorf("help should list the version command:\n%s", stdout.String())
	}
}
