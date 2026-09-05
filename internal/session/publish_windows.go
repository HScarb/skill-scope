//go:build windows

package session

import (
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

func renameNoReplace(dir *os.File, oldName, newName string) error {
	return os.Rename(filepath.Join(dir.Name(), oldName), filepath.Join(dir.Name(), newName))
}

func isLinkOrReparse(info fs.FileInfo) bool {
	data, ok := info.Sys().(*syscall.Win32FileAttributeData)
	return info.Mode()&fs.ModeSymlink != 0 || ok && data.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0
}
