//go:build linux || darwin

package session_test

import (
	"errors"
	"os"
	"os/exec"
	"testing"

	"github.com/scarb/skope/internal/session"
)

func TestOSProcessInspectorFindsCurrentAndExitedProcesses(t *testing.T) {
	inspector := session.OSProcessInspector{}
	token, err := inspector.StartToken(os.Getpid())
	if err != nil {
		t.Fatalf("current process StartToken() error = %v", err)
	}
	if token == "" {
		t.Fatal("current process StartToken() is empty")
	}

	cmd := exec.Command(os.Args[0], "-test.run=^$")
	if err := cmd.Run(); err != nil {
		t.Fatalf("run short-lived child: %v", err)
	}
	_, err = inspector.StartToken(cmd.Process.Pid)
	if !errors.Is(err, session.ErrProcessNotFound) {
		t.Fatalf("exited process StartToken() error = %v, want ErrProcessNotFound", err)
	}
}
