// Package projection inspects self-contained skill trees without writing them.
package projection

import "io/fs"

const MaxFiles = 2000
const MaxBytes int64 = 20 << 20

type File struct {
	Path   string // Slash-separated path relative to the destination.
	Source string // Resolved path relative to the opened source root.
	Size   int64
	SHA256 [32]byte
}

type Manifest struct {
	Root        string
	Directories []string
	Files       []File
	Bytes       int64
	Warnings    []string
}

type Rejection struct {
	Reason string
	Path   string
}

// Root anchors every content operation. Open must not block on special files;
// the inspector checks the opened handle's type before reading any bytes.
type Root interface {
	fs.ReadDirFS
	Lstat(string) (fs.FileInfo, error)
	Resolve(name string) (relative string, contained bool, err error)
	SameFile(a, b fs.FileInfo) bool
	Close() error
}

type OpenRoot func(directory string) (Root, error)
type Inspector struct{ OpenRoot OpenRoot }
