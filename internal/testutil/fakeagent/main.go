// Command fakeagent stands in for claude/codex/opencode in integration
// tests. It records its argv, environment and working directory as JSON
// to the file named by FAKEAGENT_OUT, then exits with FAKEAGENT_EXIT
// (default 0).
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	exitMissingOut = 99
	exitBadExit    = 98
	exitWriteFail  = 97
)

type record struct {
	Args []string          `json:"args"`
	Env  map[string]string `json:"env"`
	Cwd  string            `json:"cwd"`
}

func main() {
	os.Exit(run())
}

func run() int {
	out := os.Getenv("FAKEAGENT_OUT")
	if out == "" {
		fmt.Fprintln(os.Stderr, "fakeagent: FAKEAGENT_OUT is not set")
		return exitMissingOut
	}

	exitCode := 0
	if raw := os.Getenv("FAKEAGENT_EXIT"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			fmt.Fprintf(os.Stderr, "fakeagent: FAKEAGENT_EXIT=%q is not an integer\n", raw)
			return exitBadExit
		}
		exitCode = parsed
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "fakeagent: getwd: %v\n", err)
		return exitWriteFail
	}

	rec := record{
		Args: os.Args[1:],
		Env:  environMap(os.Environ()),
		Cwd:  cwd,
	}
	data, err := json.Marshal(rec)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fakeagent: marshal: %v\n", err)
		return exitWriteFail
	}
	if err := os.WriteFile(out, data, 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "fakeagent: write %s: %v\n", out, err)
		return exitWriteFail
	}
	return exitCode
}

// environMap converts KEY=VALUE pairs into a map. Entries without '='
// are skipped; on Windows the leading "=C:=..." drive entries are
// skipped as well because their key is empty.
func environMap(pairs []string) map[string]string {
	env := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		key, value, ok := strings.Cut(pair, "=")
		if !ok || key == "" {
			continue
		}
		env[key] = value
	}
	return env
}
