//go:build darwin

package session

import (
	"io/fs"
	"os"

	"golang.org/x/sys/unix"
)

func renameNoReplace(dir *os.File, oldName, newName string) error {
	fd := int(dir.Fd())
	return unix.RenameatxNp(fd, oldName, fd, newName, unix.RENAME_EXCL)
}

func isLinkOrReparse(info fs.FileInfo) bool {
	return info.Mode()&fs.ModeSymlink != 0
}
