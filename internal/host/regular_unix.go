//go:build !windows

package host

import (
	"os"

	"golang.org/x/sys/unix"
)

func openRegular(name string) (*os.File, error) {
	// O_NONBLOCK prevents a raced replacement with a FIFO from hanging open.
	fd, err := unix.Open(name, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: name, Err: err}
	}
	return os.NewFile(uintptr(fd), name), nil
}
