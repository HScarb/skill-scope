//go:build darwin

package session

import (
	"fmt"

	"golang.org/x/sys/unix"
)

func (OSProcessInspector) StartToken(pid int) (string, error) {
	items, err := unix.SysctlKinfoProcSlice("kern.proc.pid", pid)
	if err != nil {
		return "", fmt.Errorf("inspect process %d: %w", pid, err)
	}
	if len(items) == 0 {
		return "", ErrProcessNotFound
	}
	if len(items) != 1 {
		return "", fmt.Errorf("inspect process %d: got %d records, want one", pid, len(items))
	}
	started := items[0].Proc.P_starttime
	return fmt.Sprintf("%d:%d", started.Sec, started.Usec), nil
}
