package host

import "os/exec"

type ExecutableResolver struct{}

func (ExecutableResolver) LookPath(command string) (string, error) {
	return exec.LookPath(command)
}
