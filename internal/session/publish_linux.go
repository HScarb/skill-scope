//go:build linux

package session

import (
	"io/fs"
	"os"

	"golang.org/x/sys/unix"
)

func renameNoReplace(dir *os.File, oldName, newName string) error {
	fd := int(dir.Fd())
	return unix.Renameat2(fd, oldName, fd, newName, unix.RENAME_NOREPLACE)
}

func isLinkOrReparse(info fs.FileInfo) bool {
	return info.Mode()&fs.ModeSymlink != 0
}
