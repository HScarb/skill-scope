package host

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// ProjectionRoot keeps content access anchored to an opened directory.
type ProjectionRoot struct {
	root      *os.Root
	directory string
}

func OpenProjectionRoot(directory string) (*ProjectionRoot, error) {
	resolved, err := evalLinks(directory)
	if err != nil {
		return nil, err
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return nil, err
	}
	before, err := os.Stat(resolved)
	if err != nil {
		return nil, err
	}
	r, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	opened, err := r.Stat(".")
	after, afterErr := os.Stat(directory)
	if err != nil || afterErr != nil || !os.SameFile(before, opened) || !os.SameFile(opened, after) {
		return nil, errors.Join(err, afterErr, errors.New("projection root changed while opening"), r.Close())
	}
	return &ProjectionRoot{root: r, directory: resolved}, nil
}

func (r *ProjectionRoot) Open(name string) (fs.File, error) {
	return openProjectionNode(r.root, filepath.FromSlash(name))
}

func (r *ProjectionRoot) ReadDir(name string) ([]fs.DirEntry, error) {
	before, err := r.root.Stat(filepath.FromSlash(name))
	if err != nil {
		return nil, err
	}
	f, err := openProjectionNode(r.root, filepath.FromSlash(name))
	if err != nil {
		return nil, err
	}
	opened, err := f.Stat()
	if err != nil {
		return nil, errors.Join(err, f.Close())
	}
	if !opened.IsDir() || !os.SameFile(before, opened) {
		return nil, errors.Join(errors.New("projection directory changed while opening"), f.Close())
	}
	entries, readErr := f.ReadDir(-1)
	return entries, errors.Join(readErr, f.Close())
}

func (r *ProjectionRoot) Lstat(name string) (fs.FileInfo, error) {
	return r.root.Lstat(filepath.FromSlash(name))
}

func (r *ProjectionRoot) Resolve(name string) (string, bool, error) {
	if !fs.ValidPath(name) || !filepath.IsLocal(filepath.FromSlash(name)) {
		return "", false, nil
	}
	resolved, err := evalLinks(filepath.Join(r.directory, filepath.FromSlash(name)))
	if err != nil {
		// EvalSymlinks loses ELOOP on some platforms; Stat retains the OS error.
		_, statErr := os.Stat(filepath.Join(r.directory, filepath.FromSlash(name)))
		if projectionLinkLoop(statErr) {
			return "", false, syscall.ELOOP
		}
		return "", false, err
	}
	if !strings.EqualFold(filepath.VolumeName(r.directory), filepath.VolumeName(resolved)) {
		return "", false, nil
	}
	rel, err := filepath.Rel(r.directory, resolved)
	if err != nil {
		return "", false, err
	}
	if !filepath.IsLocal(rel) {
		return "", false, nil
	}
	return filepath.ToSlash(rel), true, nil
}

func (*ProjectionRoot) SameFile(a, b fs.FileInfo) bool { return os.SameFile(a, b) }
func (r *ProjectionRoot) Close() error                 { return r.root.Close() }
