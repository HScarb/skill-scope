package cli_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/scarb/skope/internal/cli"
	"github.com/scarb/skope/internal/config"
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

func TestApplicationHelpListsListWithoutLoadingSkillSets(t *testing.T) {
	var stdout, stderr bytes.Buffer
	app := cli.Application{
		LoadSkillSets: func() (string, config.SkillSets, error) {
			t.Fatal("loader called while rendering help")
			return "", config.SkillSets{}, nil
		},
	}

	code := app.Execute([]string{"--help"}, &stdout, &stderr, "test")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "list") {
		t.Errorf("help should list the list command:\n%s", stdout.String())
	}
}

func TestZeroApplicationAllowsHelpAndVersion(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"version"}} {
		var stdout, stderr bytes.Buffer

		code := (cli.Application{}).Execute(args, &stdout, &stderr, "test")

		if code != 0 {
			t.Errorf("args %q: exit code = %d, want 0; stderr=%q", args, code, stderr.String())
		}
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
