//go:build windows

package session_test

import (
	"fmt"
	"io"

	"golang.org/x/sys/windows"
)

type windowsDeleteLock struct {
	handle windows.Handle
}

func lockDeletion(path string) (io.Closer, error) {
	path16, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, fmt.Errorf("encode delete lock path %q: %w", path, err)
	}
	handle, err := windows.CreateFile(
		path16,
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return nil, fmt.Errorf("open delete lock for %q: %w", path, err)
	}
	return windowsDeleteLock{handle: handle}, nil
}

func (l windowsDeleteLock) Close() error {
	if err := windows.CloseHandle(l.handle); err != nil {
		return fmt.Errorf("close delete lock: %w", err)
	}
	return nil
}
