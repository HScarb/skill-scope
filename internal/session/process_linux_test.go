//go:build linux

package session_test

import (
	"errors"
	"io/fs"
	"testing"

	"github.com/scarb/skope/internal/session"
)

type fakeReadFileFS struct {
	data []byte
	err  error
	path string
}

func (f *fakeReadFileFS) ReadFile(name string) ([]byte, error) {
	f.path = name
	return f.data, f.err
}

func TestLinuxProcessInspectorParsesStartTimeAfterLastParenthesis(t *testing.T) {
	t.Parallel()

	fsys := &fakeReadFileFS{data: []byte("99 (worker ) name) S 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 424242\n")}
	token, err := (session.LinuxProcessInspector{FS: fsys}).StartToken(99)
	if err != nil {
		t.Fatalf("StartToken() error = %v", err)
	}
	if token != "424242" {
		t.Fatalf("StartToken() = %q, want 424242", token)
	}
	if fsys.path != "/proc/99/stat" {
		t.Fatalf("ReadFile() path = %q, want /proc/99/stat", fsys.path)
	}
}

func TestLinuxProcessInspectorRejectsShortStat(t *testing.T) {
	t.Parallel()

	_, err := (session.LinuxProcessInspector{FS: &fakeReadFileFS{data: []byte("99 (short) S 1 2")}}).StartToken(99)
	if err == nil {
		t.Fatal("StartToken() error = nil, want malformed stat error")
	}
}

func TestLinuxProcessInspectorMapsMissingProcess(t *testing.T) {
	t.Parallel()

	_, err := (session.LinuxProcessInspector{FS: &fakeReadFileFS{err: fs.ErrNotExist}}).StartToken(99)
	if !errors.Is(err, session.ErrProcessNotFound) {
		t.Fatalf("StartToken() error = %v, want ErrProcessNotFound", err)
	}
}

func TestLinuxProcessInspectorPreservesReadError(t *testing.T) {
	t.Parallel()

	want := errors.New("read denied")
	_, err := (session.LinuxProcessInspector{FS: &fakeReadFileFS{err: want}}).StartToken(99)
	if !errors.Is(err, want) {
		t.Fatalf("StartToken() error = %v, want wrapped read error", err)
	}
}
