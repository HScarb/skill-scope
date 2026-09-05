package host

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

var ErrNotRegular = errors.New("not a regular file")

// OpenRegular refuses special files before opening and verifies the opened handle.
func (OSFileSystem) OpenRegular(name string) (fs.File, error) {
	name = filepath.FromSlash(name)
	info, err := os.Stat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, &fs.PathError{Op: "open regular", Path: name, Err: ErrNotRegular}
	}
	file, err := openRegular(name)
	if err != nil {
		return nil, err
	}
	info, err = file.Stat()
	if err != nil {
		return nil, errors.Join(err, file.Close())
	}
	if !info.Mode().IsRegular() {
		// A close failure must never be mistaken for a structural rejection.
		if err := file.Close(); err != nil {
			return nil, err
		}
		return nil, &fs.PathError{Op: "open regular", Path: name, Err: ErrNotRegular}
	}
	return file, nil
}
