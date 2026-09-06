//go:build !windows

package host

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

func openProjectionNode(root *os.Root, name string) (*os.File, error) {
	// Keep the root boundary even when a regular file is replaced with a FIFO.
	return root.OpenFile(name, os.O_RDONLY|unix.O_NONBLOCK, 0)
}

func projectionLinkLoop(err error) bool { return errors.Is(err, syscall.ELOOP) }

func evalLinks(name string) (string, error) { return filepath.EvalSymlinks(name) }
