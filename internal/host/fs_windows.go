package host

import (
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/windows"
)

// Go's winsymlink compatibility mode exposes junctions as irregular entries.
// Mark only reparse points that Readlink recognizes as links, keeping command
// walkers from treating them as ordinary directories.
func readDirectory(name string) ([]fs.DirEntry, error) {
	entries, err := os.ReadDir(name)
	if err != nil {
		return nil, err
	}
	for i, entry := range entries {
		if entry.Type()&fs.ModeIrregular == 0 {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		attributes, ok := info.Sys().(*syscall.Win32FileAttributeData)
		if !ok || attributes.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT == 0 {
			continue
		}
		if _, err := os.Readlink(filepath.Join(name, entry.Name())); err != nil {
			return nil, err
		}
		entries[i] = linkDirEntry{DirEntry: entry}
	}
	return entries, nil
}

type linkDirEntry struct{ fs.DirEntry }

func (linkDirEntry) Type() fs.FileMode { return fs.ModeSymlink }
func (linkDirEntry) IsDir() bool       { return false }
