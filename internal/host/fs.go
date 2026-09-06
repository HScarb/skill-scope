package host

import (
	"io/fs"
	"os"
	"path/filepath"
)

// OSFileSystem provides read-only access to the host file system.
type OSFileSystem struct{}

func (OSFileSystem) ReadFile(name string) ([]byte, error) {
	return os.ReadFile(filepath.FromSlash(name))
}

func (OSFileSystem) ReadDir(name string) ([]fs.DirEntry, error) {
	return readDirectory(filepath.FromSlash(name))
}

func (OSFileSystem) Stat(name string) (fs.FileInfo, error) {
	return os.Stat(filepath.FromSlash(name))
}

func (OSFileSystem) Lstat(name string) (fs.FileInfo, error) {
	return os.Lstat(filepath.FromSlash(name))
}

func (OSFileSystem) EvalSymlinks(name string) (string, error) {
	realPath, err := evalLinks(filepath.FromSlash(name))
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(realPath), nil
}
