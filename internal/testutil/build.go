// Package testutil holds helpers shared by skope's test suites. It is
// imported only from _test.go files.
package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

const fakeAgentPkg = "github.com/scarb/skope/internal/testutil/fakeagent"

var (
	buildOnce sync.Once
	buildPath string
	buildErr  error
)

// BuildFakeAgent compiles internal/testutil/fakeagent into a temporary
// directory and returns the executable path. The build runs at most
// once per test binary; the directory is removed when the process exits
// via the returned cleanup registered on the first caller.
func BuildFakeAgent(tb testing.TB) string {
	tb.Helper()

	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "skope-fakeagent-")
		if err != nil {
			buildErr = err
			return
		}
		name := "fakeagent"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		out := filepath.Join(dir, name)

		cmd := exec.Command("go", "build", "-o", out, fakeAgentPkg)
		cmd.Env = os.Environ()
		if output, err := cmd.CombinedOutput(); err != nil {
			buildErr = &buildError{output: string(output), err: err}
			return
		}
		buildPath = out
	})

	if buildErr != nil {
		tb.Fatalf("build fakeagent: %v", buildErr)
	}
	return buildPath
}

type buildError struct {
	output string
	err    error
}

func (e *buildError) Error() string {
	return e.err.Error() + "\n" + e.output
}

func (e *buildError) Unwrap() error { return e.err }
