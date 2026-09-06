package proc_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/scarb/skope/internal/proc"
)

// Windows forcibly closes sockets when the owning process is terminated.
func isClosedTreeConnection(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, windows.WSAECONNRESET)
}

func configureTreeChild(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
}

func TestRunnerFailsClosedInsideRestrictedWindowsJob(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "must-not-run")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "-test.run=^TestWindowsRestrictedJobHelper$", "--", "restricted", marker)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if output, err := cmd.CombinedOutput(); err != nil || string(output) != "contained failure" {
		t.Fatalf("helper: %s, error: %v", output, err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("probe executed: %v", err)
	}
}

func TestWindowsRestrictedJobHelper(_ *testing.T) {
	if len(os.Args) != 5 || os.Args[2] != "--" {
		return
	}
	if os.Args[3] == "marker" {
		_ = os.WriteFile(os.Args[4], []byte("executed"), 0600)
		os.Exit(0)
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		fmt.Fprint(os.Stderr, err)
		os.Exit(20)
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_ACTIVE_PROCESS | windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	info.BasicLimitInformation.ActiveProcessLimit = 1
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		fmt.Fprint(os.Stderr, err)
		os.Exit(21)
	}
	// This is an isolated test subprocess, never the test runner or Codex host.
	if err := windows.AssignProcessToJobObject(job, windows.CurrentProcess()); err != nil {
		fmt.Fprint(os.Stderr, err)
		os.Exit(22)
	}
	_, err = (proc.Runner{}).Run(context.Background(), proc.Request{
		Executable: os.Args[0], Args: []string{"-test.run=^TestWindowsRestrictedJobHelper$", "--", "marker", os.Args[4]}, Env: []string{},
	})
	var containmentErr *proc.ContainmentError
	if !errors.As(err, &containmentErr) {
		fmt.Fprint(os.Stderr, err)
		os.Exit(23)
	}
	fmt.Print("contained failure")
	// Process exit closes the Job handle; closing it earlier would terminate
	// this helper before its status and output can be returned.
	os.Exit(0)
}

func TestRunnerReturnsWindowsHandlesAfterSuccessCancellationAndStartFailure(t *testing.T) {
	getCount := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetProcessHandleCount")
	count := func() uint32 {
		var count uint32
		ok, _, err := getCount.Call(uintptr(windows.CurrentProcess()), uintptr(unsafe.Pointer(&count)))
		if ok == 0 {
			t.Fatal(err)
		}
		return count
	}
	// Warm up Go's process and timer infrastructure before measuring handles.
	if _, err := (proc.Runner{}).Run(context.Background(), helperRequest(t, "success")); err != nil {
		t.Fatal(err)
	}
	before := count()
	for range 20 {
		if _, err := (proc.Runner{}).Run(context.Background(), helperRequest(t, "success")); err != nil {
			t.Fatal(err)
		}
		_, err := (proc.Runner{Timeout: 10 * time.Millisecond}).Run(context.Background(), helperRequest(t, "wait"))
		var timeoutErr *proc.TimeoutError
		if !errors.As(err, &timeoutErr) {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		stop := time.AfterFunc(10*time.Millisecond, cancel)
		_, err = (proc.Runner{}).Run(ctx, helperRequest(t, "wait"))
		stop.Stop()
		cancel()
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		_, err = (proc.Runner{}).Run(context.Background(), proc.Request{Executable: filepath.Join(t.TempDir(), "missing.exe"), Env: []string{}})
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
	}
	if after := count(); after > before+4 {
		t.Fatalf("handles before=%d, after=%d", before, after)
	}
}

func TestRunnerPreservesWindowsArgumentsAndExecutableWithSpaces(t *testing.T) {
	args := []string{"", "two words", `a"b`, `back\slash`, `ends with slash\`, `\"`, "中文"}
	req := helperRequest(t, "arguments", args...)
	contents, err := os.ReadFile(req.Executable)
	if err != nil {
		t.Fatal(err)
	}
	req.Executable = filepath.Join(t.TempDir(), "probe with spaces.exe")
	if err := os.WriteFile(req.Executable, contents, 0600); err != nil {
		t.Fatal(err)
	}
	result, err := (proc.Runner{}).Run(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	if err := json.Unmarshal(result.Stdout, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, args) {
		t.Fatalf("arguments = %q, want %q", got, args)
	}
}

func TestRunnerPreservesWindowsEnvironmentValueWithEquals(t *testing.T) {
	req := helperRequest(t, "success")
	req.Env = []string{"PROBE_VALUE=a=b=中文"}
	result, err := (proc.Runner{}).Run(context.Background(), req)
	if err != nil || !strings.HasPrefix(string(result.Stdout), "a=b=中文\n\n") {
		t.Fatalf("stdout = %q, error = %v", result.Stdout, err)
	}
}

func TestRunnerRejectsWindowsCommandScriptsWithoutExecutingThem(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "marker")
	script := filepath.Join(t.TempDir(), "probe.cmd")
	if err := os.WriteFile(script, []byte("@echo executed > \""+marker+"\"\r\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := (proc.Runner{}).Run(context.Background(), proc.Request{Executable: script, Env: []string{}})
	if err == nil || !strings.Contains(err.Error(), ".exe") {
		t.Fatalf("error = %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("script ran: %v", err)
	}
}
