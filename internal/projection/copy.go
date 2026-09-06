package projection

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"slices"
)

type Sink interface {
	Mkdir(relative string) error
	WriteFile(relative string, data []byte) error
}

type Copier struct{ OpenRoot OpenRoot }

// Copy rechecks the tree before writing and after the last write. These checks
// are not a filesystem snapshot; identical content replacements can pass.
func (c Copier) Copy(ctx context.Context, manifest Manifest, sink Sink) (err error) {
	if err := c.verify(ctx, manifest); err != nil {
		return err
	}
	r, err := c.OpenRoot(manifest.Root)
	if err != nil {
		return err
	}
	defer func() {
		closeErr := r.Close()
		err = errors.Join(err, closeErr, ctx.Err())
	}()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := sink.Mkdir("."); err != nil {
		return err
	}
	for _, dir := range manifest.Directories {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := sink.Mkdir(dir); err != nil {
			return err
		}
	}
	for _, file := range manifest.Files {
		if err := copyFile(ctx, r, file, sink); err != nil {
			return err
		}
	}
	// Reopen the discovery entry too: it may have been redirected during copy.
	return c.verify(ctx, manifest)
}

func (c Copier) verify(ctx context.Context, expected Manifest) error {
	actual, rejection, err := Inspector(c).Inspect(ctx, expected.Root)
	if err != nil {
		return err
	}
	if rejection != nil {
		return fmt.Errorf("projection source rejected during copy: %s at %q", rejection.Reason, rejection.Path)
	}
	if actual.Bytes != expected.Bytes || !slices.Equal(actual.Files, expected.Files) || !slices.Equal(actual.Directories, expected.Directories) {
		return errors.New("projection source changed since inspection")
	}
	return nil
}

func copyFile(ctx context.Context, root Root, expected File, sink Sink) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	source, contained, err := root.Resolve(expected.Path)
	if err != nil {
		return err
	}
	if !contained || !validPath(source) || source != expected.Source {
		return fmt.Errorf("projection source path changed for %q", expected.Path)
	}
	before, err := root.Lstat(source)
	if err != nil {
		return err
	}
	f, err := root.Open(source)
	if err != nil {
		return err
	}
	opened, err := f.Stat()
	if err != nil {
		return errors.Join(err, f.Close())
	}
	if !opened.Mode().IsRegular() || !root.SameFile(before, opened) || opened.Size() != expected.Size {
		return errors.Join(fmt.Errorf("projection source changed while opening %q", expected.Path), f.Close())
	}
	data, readErr := io.ReadAll(io.LimitReader(contextReader{ctx: ctx, reader: f}, expected.Size+1))
	if err := errors.Join(readErr, f.Close(), ctx.Err()); err != nil {
		return err
	}
	if int64(len(data)) != expected.Size || sha256.Sum256(data) != expected.SHA256 {
		return fmt.Errorf("projection source content changed for %q", expected.Path)
	}
	return sink.WriteFile(expected.Path, data)
}
