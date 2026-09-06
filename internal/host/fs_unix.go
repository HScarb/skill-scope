//go:build !windows

package host

import (
	"io/fs"
	"os"
)

func readDirectory(name string) ([]fs.DirEntry, error) { return os.ReadDir(name) }
