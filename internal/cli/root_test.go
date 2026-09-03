package cli_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/scarb/skope/internal/cli"
)

func TestExecuteHelpListsRootCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := cli.Execute([]string{"--help"}, &stdout, &stderr, "test")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "skope") {
		t.Errorf("help output does not mention skope:\n%s", stdout.String())
	}
}

func TestExecuteUnknownCommandReturnsOne(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := cli.Execute([]string{"no-such-command"}, &stdout, &stderr, "test")

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "no-such-command") {
		t.Errorf("stderr should name the unknown command, got %q", stderr.String())
	}
}
