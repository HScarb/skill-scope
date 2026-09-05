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

const (
	fakeAgentPkg = "github.com/scarb/skope/internal/testutil/fakeagent"
	skopePkg     = "github.com/scarb/skope/cmd/skope"
)

var (
	fakeAgentBuildOnce sync.Once
	fakeAgentBuildPath string
	fakeAgentBuildErr  error
	skopeBuildOnce     sync.Once
	skopeBuildPath     string
	skopeBuildErr      error
)

// BuildFakeAgent compiles internal/testutil/fakeagent into a temporary
// directory and returns the executable path. The build runs at most
// once per test binary. Because tests share it, the directory remains in
// os.TempDir for the host's normal temporary-file cleanup.
func BuildFakeAgent(tb testing.TB) string {
	tb.Helper()

	fakeAgentBuildOnce.Do(func() {
		fakeAgentBuildPath, fakeAgentBuildErr = buildPackage("fakeagent", fakeAgentPkg)
	})

	if fakeAgentBuildErr != nil {
		tb.Fatalf("build fakeagent: %v", fakeAgentBuildErr)
	}
	return fakeAgentBuildPath
}

// BuildSkope compiles cmd/skope into a temporary directory and returns the
// executable path. The build runs at most once per test binary.
func BuildSkope(tb testing.TB) string {
	tb.Helper()

	skopeBuildOnce.Do(func() {
		skopeBuildPath, skopeBuildErr = buildPackage("skope", skopePkg)
	})

	if skopeBuildErr != nil {
		tb.Fatalf("build skope: %v", skopeBuildErr)
	}
	return skopeBuildPath
}

func buildPackage(name, pkg string) (string, error) {
	dir, err := os.MkdirTemp("", "skope-"+name+"-")
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	out := filepath.Join(dir, name)

	cmd := exec.Command("go", "build", "-o", out, pkg)
	cmd.Env = os.Environ()
	if output, err := cmd.CombinedOutput(); err != nil {
		return "", &buildError{output: string(output), err: err}
	}
	return out, nil
}

type buildError struct {
	output string
	err    error
}

func (e *buildError) Error() string {
	return e.err.Error() + "\n" + e.output
}

func (e *buildError) Unwrap() error { return e.err }
