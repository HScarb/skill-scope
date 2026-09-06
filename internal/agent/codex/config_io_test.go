package codex_test

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scarb/skope/internal/agent/codex"
	"github.com/scarb/skope/internal/host"
	"github.com/scarb/skope/internal/skill"
)

type cancelOpener struct {
	host.OSFileSystem
	cancel context.CancelFunc
	file   *countedFile
}

func (o cancelOpener) OpenRegular(string) (fs.File, error) { o.cancel(); return o.file, nil }

type countedFile struct {
	reads, closes     int
	readErr, closeErr error
}

func (f *countedFile) Stat() (fs.FileInfo, error) { return nil, fs.ErrInvalid }
func (f *countedFile) Read([]byte) (int, error)   { f.reads++; return 0, f.readErr }
func (f *countedFile) Close() error               { f.closes++; return f.closeErr }
func TestReadCodexConfigCancellationStopsReadAndCloses(t *testing.T) {
	_, e, p := setup(t)
	write(t, filepath.Join(p.CodexHome, "config.toml"), "model='ok'")
	ctx, cancel := context.WithCancel(context.Background())
	file := &countedFile{readErr: fs.ErrPermission}
	disk := host.OSFileSystem{}
	c := codex.NewCatalog(disk, cancelOpener{cancel: cancel, file: file}, skill.Scanner{FS: disk, RegularFiles: disk})
	_, err := c.Read(ctx, e, p)
	if !errors.Is(err, context.Canceled) || file.reads != 0 || file.closes != 1 {
		t.Fatalf("err=%v reads=%d closes=%d", err, file.reads, file.closes)
	}
}

type cancelProjectFS struct {
	host.OSFileSystem
	cancel context.CancelFunc
	calls  *int
}

func (f cancelProjectFS) Stat(_ string) (fs.FileInfo, error) {
	*f.calls++
	f.cancel()
	return nil, fs.ErrNotExist
}
func TestCatalogCancellationStopsAncestorDiscovery(t *testing.T) {
	_, e, p := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	disk := cancelProjectFS{cancel: cancel, calls: &calls}
	c := codex.NewCatalog(disk, host.OSFileSystem{}, skill.Scanner{})
	_, err := c.Read(ctx, e, p)
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

type fixedOpener struct{ file fs.File }

func (o fixedOpener) OpenRegular(string) (fs.File, error) { return o.file, nil }
func TestReadCodexConfigPreservesReadAndCloseErrors(t *testing.T) {
	_, e, p := setup(t)
	write(t, filepath.Join(p.CodexHome, "config.toml"), "model='ok'")
	readErr := errors.New("read SECRET_SENTINEL")
	closeErr := errors.New("close SECRET_SENTINEL")
	file := &countedFile{readErr: readErr, closeErr: closeErr}
	c := codex.NewCatalog(host.OSFileSystem{}, fixedOpener{file}, skill.Scanner{})
	_, err := c.Read(context.Background(), e, p)
	if !errors.Is(err, readErr) || !errors.Is(err, closeErr) || strings.Contains(err.Error(), "SECRET_SENTINEL") || file.closes != 1 {
		t.Fatalf("%v closes=%d", err, file.closes)
	}
}

type failingLstatFS struct {
	host.OSFileSystem
	err   error
	calls *int
}

func (f failingLstatFS) Lstat(string) (fs.FileInfo, error) { *f.calls++; return nil, f.err }
func TestCatalogIOFailureStopsNextSource(t *testing.T) {
	_, e, p := setup(t)
	p.SystemConfigPaths = []string{filepath.Join(p.Home, "system.toml")}
	cause := errors.Join(fs.ErrNotExist, fs.ErrPermission)
	calls := 0
	c := codex.NewCatalog(failingLstatFS{err: cause, calls: &calls}, host.OSFileSystem{}, skill.Scanner{})
	_, err := c.Read(context.Background(), e, p)
	if !errors.Is(err, fs.ErrPermission) || calls != 1 {
		t.Fatalf("%v calls=%d", err, calls)
	}
}
