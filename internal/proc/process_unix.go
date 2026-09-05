//go:build !windows

package proc

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

type process struct{ cmd *exec.Cmd }

func startProcess(req Request, stdout, stderr *os.File) (*process, error) {
	cmd := exec.Command(req.Executable, req.Args...)
	cmd.Dir, cmd.Env = req.Dir, req.Env
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// A nil Stdin connects the command to the null device (immediate EOF).
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &process{cmd: cmd}, nil
}

func (p *process) wait() error   { return p.cmd.Wait() }
func (p *process) exitCode() int { return p.cmd.ProcessState.ExitCode() }
func (p *process) kill() error {
	err := syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
	if err == nil || errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return fmt.Errorf("terminate auxiliary process group: %w", err)
}
