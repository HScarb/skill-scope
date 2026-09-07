package cli_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/scarb/skope/internal/cli"
	"github.com/scarb/skope/internal/skill"
)

func TestCheckConflictsRejectsClaudeControlFlagsWithoutValues(t *testing.T) {
	for _, flag := range []string{"--settings", "--setting-sources", "--plugin-dir", "--plugin-url", "--add-dir"} {
		for _, inline := range []bool{false, true} {
			for _, source := range []string{"config", "command-line"} {
				t.Run(flag+source+map[bool]string{true: "inline", false: "separate"}[inline], func(t *testing.T) {
					args := []string{"--", flag, "secret-sentinel"}
					if inline {
						args = []string{"--", flag + "=secret-sentinel"}
					}
					saved := append([]string(nil), args...)
					var configArgs, userArgs []string
					if source == "config" {
						configArgs = args
					} else {
						userArgs = args
					}
					err := cli.CheckConflicts(skill.AgentClaude, configArgs, userArgs)
					var conflict *cli.ConflictError
					if !errors.As(err, &conflict) || conflict.Flag != flag || conflict.Source != source || strings.Contains(err.Error(), "secret-sentinel") {
						t.Fatalf("error=%v", err)
					}
					if !reflect.DeepEqual(args, saved) {
						t.Fatal("mutated args")
					}
				})
			}
		}
	}
	if err := cli.CheckConflicts(skill.AgentClaude, []string{"--settings-file=x", "--model", "sonnet"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := cli.CheckConflicts(skill.AgentCodex, []string{"--settings=x"}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestConflictErrorPreservesClaudeMessage(t *testing.T) {
	for _, agent := range []skill.Agent{"", skill.AgentClaude} {
		err := &cli.ConflictError{Agent: agent, Flag: "--settings", Source: "config"}
		if err.Error() != "Claude isolation conflicts with config argument --settings" {
			t.Fatal(err)
		}
	}
}
