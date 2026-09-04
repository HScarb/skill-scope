//go:build unix

package handoff

import "syscall"

func (Handoff) Exec(executable string, args, env []string) error {
	argv := make([]string, 0, len(args)+1)
	argv = append(argv, executable)
	argv = append(argv, args...)
	return syscall.Exec(executable, argv, env)
}
