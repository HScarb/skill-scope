//go:build linux

package session

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

type ReadFileFS interface {
	ReadFile(name string) ([]byte, error)
}

type LinuxProcessInspector struct {
	FS ReadFileFS
}

func (p LinuxProcessInspector) StartToken(pid int) (string, error) {
	if p.FS == nil {
		return "", errors.New("linux process inspector requires a file system")
	}
	path := fmt.Sprintf("/proc/%d/stat", pid)
	raw, err := p.FS.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", ErrProcessNotFound
	}
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	end := strings.LastIndex(string(raw), ") ")
	if end < 0 {
		return "", fmt.Errorf("parse %s: missing process name terminator", path)
	}
	fields := strings.Fields(string(raw[end+2:]))
	const startTimeIndex = 22 - 3
	if len(fields) <= startTimeIndex {
		return "", fmt.Errorf("parse %s: got %d fields after process name, need at least %d", path, len(fields), startTimeIndex+1)
	}
	return fields[startTimeIndex], nil
}

func (OSProcessInspector) StartToken(pid int) (string, error) {
	return (LinuxProcessInspector{FS: osReadFileFS{}}).StartToken(pid)
}

type osReadFileFS struct{}

func (osReadFileFS) ReadFile(name string) ([]byte, error) {
	return os.ReadFile(name)
}
