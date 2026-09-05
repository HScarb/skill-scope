package proc_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/scarb/skope/internal/proc"
)

func helperRequest(t *testing.T, mode string, args ...string) proc.Request {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return proc.Request{Executable: exe, Args: append([]string{"-test.run=^TestProcHelperProcess$", "--", mode}, args...), Env: []string{"PROBE_VALUE=explicit"}}
}

func requireBackend(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Task 6 implements the Windows Job backend")
	}
}

func TestRunnerDefaultsAndInvalidConfiguration(t *testing.T) {
	if proc.DefaultTimeout != 15*time.Second || proc.DefaultOutputLimit != 4<<20 {
		t.Fatal("incorrect defaults")
	}
	for _, tc := range []struct {
		name   string
		runner proc.Runner
		req    proc.Request
	}{
		{"negative timeout", proc.Runner{Timeout: -time.Second}, helperRequest(t, "success")},
		{"negative limit", proc.Runner{OutputLimit: -1}, helperRequest(t, "success")},
		{"nil environment", proc.Runner{}, proc.Request{Executable: "must-not-start"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.runner.Run(context.Background(), tc.req); err == nil {
				t.Fatal("accepted invalid configuration")
			}
		})
	}
}

func TestRunnerDoesNotStartAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := (proc.Runner{}).Run(ctx, proc.Request{Executable: "must-not-start", Env: []string{}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}

func TestRunnerReportsUnavailableExecutableOrBackend(t *testing.T) {
	req := proc.Request{Executable: filepath.Join(t.TempDir(), "missing-probe"), Env: []string{}}
	_, err := (proc.Runner{}).Run(context.Background(), req)
	if runtime.GOOS == "windows" {
		if !errors.Is(err, proc.ErrUnsupported) {
			t.Fatalf("error = %v", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("error = %v", err)
	}
}

func TestRunnerCapturesStreamsAndUsesExplicitEnvironmentDirectoryAndEOF(t *testing.T) {
	requireBackend(t)
	t.Setenv("PROBE_PARENT_SECRET", "parent-secret")
	req := helperRequest(t, "success")
	req.Dir = t.TempDir()
	result, err := (proc.Runner{}).Run(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	wantDir, err := filepath.EvalSymlinks(req.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if string(result.Stdout) != "explicit\n\n"+wantDir+"\n0\n" || string(result.Stderr) != "stderr" {
		t.Fatalf("result = %+v", result)
	}
	req.Env = []string{}
	result, err = (proc.Runner{}).Run(context.Background(), req)
	if err != nil || !strings.HasPrefix(string(result.Stdout), "\n\n") {
		t.Fatalf("empty env inherited: %q, %v", result.Stdout, err)
	}
}

func TestRunnerReturnsTypedExitWithBoundedRawStderr(t *testing.T) {
	requireBackend(t)
	_, err := (proc.Runner{}).Run(context.Background(), helperRequest(t, "exit", "argv-secret"))
	var exitErr *proc.ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 7 {
		t.Fatalf("error = %v", err)
	}
	if len(exitErr.Stderr) != 2048 || !strings.HasPrefix(exitErr.Stderr, "\x1b[31m") {
		t.Fatalf("stderr prefix = %q", exitErr.Stderr)
	}
	for _, secret := range []string{"stdout-secret", "argv-secret", "PROBE_VALUE"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error exposed %s", secret)
		}
	}
}

func TestRunnerBoundsEachStreamIndependently(t *testing.T) {
	requireBackend(t)
	for _, tc := range []struct {
		name, stream string
		count        int
		overflow     bool
	}{
		{"stdout exact", "stdout", 1024, false}, {"stdout overflow", "stdout", 1025, true},
		{"stderr exact", "stderr", 1024, false}, {"stderr overflow", "stderr", 1025, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := (proc.Runner{Timeout: 3 * time.Second, OutputLimit: 1024}).Run(context.Background(), helperRequest(t, "output", tc.stream, strconv.Itoa(tc.count)))
			var limitErr *proc.OutputLimitError
			if tc.overflow {
				if !errors.As(err, &limitErr) || limitErr.Stream != tc.stream || limitErr.Limit != 1024 {
					t.Fatalf("error = %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if len(result.Stdout) > 1024 || len(result.Stderr) > 1024 {
				t.Fatal("retained output exceeds limit")
			}
		})
	}
	result, err := (proc.Runner{}).Run(context.Background(), helperRequest(t, "both"))
	if err != nil || len(result.Stdout) != 3<<20 || len(result.Stderr) != 3<<20 {
		t.Fatalf("independent streams: lengths %d/%d, error %v", len(result.Stdout), len(result.Stderr), err)
	}
}

func TestRunnerTimeoutAndEarlierContextDeadline(t *testing.T) {
	requireBackend(t)
	for _, tc := range []struct {
		name           string
		timeout        time.Duration
		contextTimeout time.Duration
	}{
		{"runner", 150 * time.Millisecond, 3 * time.Second}, {"context", 3 * time.Second, 150 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), tc.contextTimeout)
			defer cancel()
			_, err := (proc.Runner{Timeout: tc.timeout}).Run(ctx, helperRequest(t, "wait"))
			var timeoutErr *proc.TimeoutError
			if !errors.As(err, &timeoutErr) || !errors.Is(err, context.DeadlineExceeded) || timeoutErr.Command == "" || timeoutErr.Timeout <= 0 {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestProcHelperProcess(_ *testing.T) {
	if len(os.Args) < 4 || os.Args[2] != "--" {
		return
	}
	mode := os.Args[3]
	switch mode {
	case "success":
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			os.Exit(11)
		}
		dir, err := os.Getwd()
		if err != nil {
			os.Exit(12)
		}
		fmt.Printf("%s\n%s\n%s\n%d\n", os.Getenv("PROBE_VALUE"), os.Getenv("PROBE_PARENT_SECRET"), dir, len(data))
		fmt.Fprint(os.Stderr, "stderr")
	case "exit":
		fmt.Print("stdout-secret")
		fmt.Fprint(os.Stderr, "\x1b[31m"+strings.Repeat("e", 4096))
		os.Exit(7)
	case "output":
		n, err := strconv.Atoi(os.Args[5])
		if err != nil {
			os.Exit(13)
		}
		var out io.Writer = os.Stdout
		if os.Args[4] == "stderr" {
			out = os.Stderr
		}
		_, _ = io.WriteString(out, strings.Repeat("x", n))
		if n > 1024 {
			time.Sleep(30 * time.Second)
		}
	case "both":
		fmt.Fprint(os.Stdout, strings.Repeat("o", 3<<20))
		fmt.Fprint(os.Stderr, strings.Repeat("e", 3<<20))
	case "wait":
		time.Sleep(30 * time.Second)
	case "tree", "tree-output", "tree-exit", "tree-no-pipes":
		cmd := exec.Command(os.Args[0], "-test.run=^TestProcHelperProcess$", "--", "descendant", os.Args[4], os.Args[5])
		cmd.Env = []string{}
		if mode != "tree-no-pipes" {
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
		}
		if err := cmd.Start(); err != nil {
			os.Exit(14)
		}
		// The parent deliberately exits without waiting for its descendant.
		deadline := time.Now().Add(10 * time.Second)
		for {
			if _, err := os.Stat(os.Args[5]); err == nil {
				break
			}
			if time.Now().After(deadline) {
				os.Exit(15)
			}
			time.Sleep(time.Millisecond)
		}
		if mode == "tree-output" {
			fmt.Fprint(os.Stdout, strings.Repeat("x", 1025))
		}
		if mode == "tree" || mode == "tree-output" {
			time.Sleep(30 * time.Second)
		}
		os.Exit(0)
	case "descendant":
		conn, err := net.DialTimeout("tcp", os.Args[4], 5*time.Second)
		if err != nil {
			os.Exit(16)
		}
		if _, err := conn.Write([]byte{1}); err != nil {
			os.Exit(17)
		}
		if err := os.WriteFile(os.Args[5], []byte("ready"), 0600); err != nil {
			os.Exit(18)
		}
		var b [1]byte
		_, _ = conn.Read(b[:])
		_ = conn.Close()
	default:
		os.Exit(19)
	}
	os.Exit(0)
}
