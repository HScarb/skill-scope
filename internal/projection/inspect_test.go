package projection_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"testing/fstest"

	"github.com/scarb/skope/internal/projection"
)

type testRoot struct {
	fstest.MapFS
	closed bool
}

func (r *testRoot) Lstat(name string) (fs.FileInfo, error)    { return r.Stat(name) }
func (r *testRoot) Resolve(name string) (string, bool, error) { return name, true, nil }
func (*testRoot) SameFile(a, b fs.FileInfo) bool              { return a.Name() == b.Name() && a.Mode() == b.Mode() }
func (r *testRoot) Close() error                              { r.closed = true; return nil }

func inspect(t *testing.T, files fstest.MapFS) (projection.Manifest, *projection.Rejection, error) {
	t.Helper()
	r := &testRoot{MapFS: files}
	i := projection.Inspector{OpenRoot: func(string) (projection.Root, error) { return r, nil }}
	m, reject, err := i.Inspect(context.Background(), "discovery-root")
	if !r.closed {
		t.Error("root was not closed")
	}
	return m, reject, err
}

type faultRoot struct {
	*testRoot
	openErr, readDirErr, closeErr, errorRead, errorStat, errorFileClose error
	openedMode                                                          fs.FileMode
	changed                                                             bool
	read, closedFiles                                                   int
	remaining                                                           int64
	cancel                                                              context.CancelFunc
	cancelReadDir, cancelClose                                          context.CancelFunc
}

func (r *faultRoot) ReadDir(name string) ([]fs.DirEntry, error) {
	if r.cancelReadDir != nil {
		r.cancelReadDir()
	}
	if r.readDirErr != nil {
		return nil, r.readDirErr
	}
	return r.testRoot.ReadDir(name)
}
func (r *faultRoot) Close() error {
	r.closed = true
	if r.cancelClose != nil {
		r.cancelClose()
	}
	return r.closeErr
}
func (r *faultRoot) Open(name string) (fs.File, error) {
	if r.openErr != nil {
		return nil, r.openErr
	}
	f, err := r.testRoot.Open(name)
	if err != nil {
		return nil, err
	}
	return &faultFile{File: f, root: r}, nil
}

type faultFile struct {
	fs.File
	root *faultRoot
}

func (f *faultFile) Stat() (fs.FileInfo, error) {
	info, err := f.File.Stat()
	if err != nil {
		return nil, err
	}
	if f.root.errorStat != nil {
		return nil, f.root.errorStat
	}
	return changedInfo{FileInfo: info, mode: f.root.openedMode, changed: f.root.changed}, nil
}

type changedInfo struct {
	fs.FileInfo
	mode    fs.FileMode
	changed bool
}

func (i changedInfo) Mode() fs.FileMode {
	if i.mode != 0 {
		return i.mode
	}
	return i.FileInfo.Mode()
}
func (i changedInfo) Name() string {
	if i.changed {
		return "replacement"
	}
	return i.FileInfo.Name()
}
func (f *faultFile) Read(p []byte) (int, error) {
	if f.root.errorRead != nil {
		return 0, f.root.errorRead
	}
	if f.root.cancel != nil {
		f.root.cancel()
	}
	if f.root.remaining > 0 {
		n := int64(len(p))
		if n > f.root.remaining {
			n = f.root.remaining
		}
		clear(p[:n])
		f.root.remaining -= n
		f.root.read += int(n)
		return int(n), nil
	}
	n, err := f.File.Read(p)
	f.root.read += n
	return n, err
}
func (f *faultFile) Close() error {
	f.root.closedFiles++
	return errors.Join(f.File.Close(), f.root.errorFileClose)
}

func TestInspectVerifiesOpenedFileBeforeReading(t *testing.T) {
	for _, tt := range []struct {
		name    string
		mode    fs.FileMode
		changed bool
	}{
		{"fifo", fs.ModeNamedPipe, false}, {"directory", fs.ModeDir, false}, {"identity", 0, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := &faultRoot{testRoot: &testRoot{MapFS: fstest.MapFS{"file": {Data: []byte("secret")}}}, openedMode: tt.mode, changed: tt.changed}
			i := projection.Inspector{OpenRoot: func(string) (projection.Root, error) { return r, nil }}
			_, reject, err := i.Inspect(context.Background(), "root")
			if tt.changed {
				if err == nil {
					t.Fatal("changed file accepted")
				}
			} else if reject == nil || reject.Reason != "special-file" || err != nil {
				t.Fatalf("reject=%+v err=%v", reject, err)
			}
			if r.read != 0 || r.closedFiles != 1 || !r.closed {
				t.Fatalf("read=%d closedFiles=%d rootClosed=%v", r.read, r.closedFiles, r.closed)
			}
		})
	}
}

func TestInspectPreservesIOErrors(t *testing.T) {
	sentinel := errors.New("injected IO failure")
	for _, name := range []string{"open-root", "read-dir", "open", "stat", "read", "close-file", "close-root"} {
		t.Run(name, func(t *testing.T) {
			r := &faultRoot{testRoot: &testRoot{MapFS: fstest.MapFS{"file": {}}}}
			switch name {
			case "read-dir":
				r.readDirErr = sentinel
			case "open":
				r.openErr = sentinel
			case "stat":
				r.errorStat = sentinel
			case "read":
				r.errorRead = sentinel
			case "close-file":
				r.errorFileClose = sentinel
			case "close-root":
				r.closeErr = sentinel
			}
			i := projection.Inspector{OpenRoot: func(string) (projection.Root, error) {
				if name == "open-root" {
					return nil, sentinel
				}
				return r, nil
			}}
			_, _, err := i.Inspect(context.Background(), "root")
			if !errors.Is(err, sentinel) {
				t.Fatalf("error=%v", err)
			}
			if name != "open-root" && !r.closed {
				t.Fatal("root not closed")
			}
		})
	}
}

func TestInspectHonorsCancellationBeforeReturning(t *testing.T) {
	readErr := errors.New("read directory failed")
	closeErr := errors.New("close root failed")
	for _, name := range []string{"last-read-dir", "close", "close-with-io-errors"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			r := &faultRoot{testRoot: &testRoot{MapFS: fstest.MapFS{}}}
			if name == "last-read-dir" {
				r.cancelReadDir = cancel
			} else {
				r.cancelClose = cancel
			}
			if name == "close-with-io-errors" {
				r.readDirErr, r.closeErr = readErr, closeErr
			}
			i := projection.Inspector{OpenRoot: func(string) (projection.Root, error) { return r, nil }}
			_, reject, err := i.Inspect(ctx, "root")
			if reject != nil || !errors.Is(err, context.Canceled) || !r.closed {
				t.Fatalf("reject=%+v err=%v rootClosed=%v", reject, err, r.closed)
			}
			if name == "close-with-io-errors" && (!errors.Is(err, readErr) || !errors.Is(err, closeErr)) {
				t.Fatalf("lost IO errors: %v", err)
			}
		})
	}
}

func TestInspectBoundsReadsDespiteSmallStat(t *testing.T) {
	r := &faultRoot{testRoot: &testRoot{MapFS: fstest.MapFS{"file": {}}}, remaining: projection.MaxBytes * 2}
	i := projection.Inspector{OpenRoot: func(string) (projection.Root, error) { return r, nil }}
	_, reject, err := i.Inspect(context.Background(), "root")
	if err != nil || reject == nil || reject.Reason != "too-many-bytes" || r.read != int(projection.MaxBytes+1) {
		t.Fatalf("reject=%+v err=%v read=%d", reject, err, r.read)
	}
}

func TestInspectCancelsDuringRead(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := &faultRoot{testRoot: &testRoot{MapFS: fstest.MapFS{"file": {}}}, cancel: cancel}
	i := projection.Inspector{OpenRoot: func(string) (projection.Root, error) { return r, nil }}
	_, _, err := i.Inspect(ctx, "root")
	if !errors.Is(err, context.Canceled) || !r.closed || r.closedFiles != 1 {
		t.Fatalf("error=%v rootClosed=%v filesClosed=%d", err, r.closed, r.closedFiles)
	}
}

var _ io.Reader = (*faultFile)(nil)

type linkRoot struct {
	*testRoot
	links      map[string]string
	resolveErr error
}

func (r *linkRoot) Resolve(name string) (string, bool, error) {
	if r.resolveErr != nil && name != "." {
		return "", false, r.resolveErr
	}
	if target, ok := r.links[name]; ok {
		return target, target != "outside", nil
	}
	return name, true, nil
}

func TestInspectExpandsInternalAliasesPerBranch(t *testing.T) {
	r := &linkRoot{testRoot: &testRoot{MapFS: fstest.MapFS{
		"SKILL.md": {Data: []byte("skill")}, "references/note.md": {Data: []byte("note")},
		"links/inside": {Mode: fs.ModeSymlink}, "alias.md": {Mode: fs.ModeSymlink}, "again": {Mode: fs.ModeSymlink},
	}}, links: map[string]string{"links/inside": "references", "alias.md": "SKILL.md", "again": "references"}}
	i := projection.Inspector{OpenRoot: func(string) (projection.Root, error) { return r, nil }}
	m, reject, err := i.Inspect(context.Background(), "root")
	if err != nil || reject != nil {
		t.Fatalf("reject=%+v err=%v", reject, err)
	}
	if len(m.Files) != 5 || m.Bytes != 22 {
		t.Fatalf("files=%+v bytes=%d", m.Files, m.Bytes)
	}
	for _, f := range m.Files {
		if f.Path == "links/inside/note.md" && f.Source != "references/note.md" {
			t.Fatalf("file=%+v", f)
		}
	}
}

func TestInspectRejectsLinkEscapesAndLoops(t *testing.T) {
	for _, tt := range []struct {
		name, target, reason string
		err                  error
	}{
		{"escape", "outside", "outside-root", nil}, {"directory-loop", ".", "symlink-loop", nil},
		{"file-loop", "", "symlink-loop", syscall.ELOOP}, {"broken", "", "", fs.ErrNotExist}, {"permission", "", "", fs.ErrPermission},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := &linkRoot{testRoot: &testRoot{MapFS: fstest.MapFS{"link": {Mode: fs.ModeSymlink}}}, links: map[string]string{"link": tt.target}, resolveErr: tt.err}
			i := projection.Inspector{OpenRoot: func(string) (projection.Root, error) { return r, nil }}
			_, reject, err := i.Inspect(context.Background(), "root")
			if tt.reason != "" {
				if err != nil || reject == nil || reject.Reason != tt.reason {
					t.Fatalf("reject=%+v err=%v", reject, err)
				}
			} else if reject != nil || !errors.Is(err, tt.err) {
				t.Fatalf("reject=%+v err=%v", reject, err)
			}
			if !r.closed {
				t.Fatal("root not closed")
			}
		})
	}
}

func TestInspectRejectsUnsafeTrees(t *testing.T) {
	for _, tt := range []struct {
		name, reason string
		files        fstest.MapFS
	}{
		{"fifo", "special-file", fstest.MapFS{"pipe": {Mode: fs.ModeNamedPipe}}},
		{"socket", "special-file", fstest.MapFS{"socket": {Mode: fs.ModeSocket}}},
		{"device", "special-file", fstest.MapFS{"device": {Mode: fs.ModeDevice}}},
		{"char-device", "special-file", fstest.MapFS{"device": {Mode: fs.ModeDevice | fs.ModeCharDevice}}},
		{"root-plugin", "plugin-manifest", fstest.MapFS{".claude-plugin/plugin.json": {Data: []byte("{}")}}},
		{"nested-plugin", "plugin-manifest", fstest.MapFS{"nested/.claude-plugin/plugin.json": {Data: []byte("{}")}}},
		{"case-files", "target-conflict", fstest.MapFS{"references/Foo.md": {}, "references/foo.md": {}}},
		{"case-directories", "target-conflict", fstest.MapFS{"Refs/a.md": {}, "refs/b.md": {}}},
		{"case-file-directory", "target-conflict", fstest.MapFS{"Docs": {}, "docs/a.md": {}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, r, err := inspect(t, tt.files)
			if err != nil || r == nil || r.Reason != tt.reason {
				t.Fatalf("reject = %+v, err = %v", r, err)
			}
		})
	}
}

func TestInspectEnforcesExpandedLimits(t *testing.T) {
	for _, count := range []int{projection.MaxFiles, projection.MaxFiles + 1} {
		t.Run(fmt.Sprint("files-", count), func(t *testing.T) {
			files := fstest.MapFS{}
			for n := 0; n < count; n++ {
				files[fmt.Sprintf("%04d", n)] = &fstest.MapFile{}
			}
			m, r, err := inspect(t, files)
			if err != nil {
				t.Fatal(err)
			}
			if count == projection.MaxFiles {
				if r != nil || len(m.Files) != count {
					t.Fatalf("manifest=%+v reject=%+v", m, r)
				}
			} else if r == nil || r.Reason != "too-many-files" {
				t.Fatalf("reject=%+v", r)
			}
		})
	}
	for _, size := range []int64{projection.MaxBytes, projection.MaxBytes + 1} {
		t.Run(fmt.Sprint("bytes-", size), func(t *testing.T) {
			m, r, err := inspect(t, fstest.MapFS{"large": {Data: make([]byte, size)}})
			if err != nil {
				t.Fatal(err)
			}
			if size == projection.MaxBytes {
				if r != nil || m.Bytes != size {
					t.Fatalf("bytes=%d reject=%+v", m.Bytes, r)
				}
			} else if r == nil || r.Reason != "too-many-bytes" {
				t.Fatalf("reject=%+v", r)
			}
		})
	}
}

func TestInspectWarnsOnlyForMarkdownExternalReferences(t *testing.T) {
	for _, body := range []string{"../secret", "${CLAUDE_PLUGIN_ROOT}/x", "${CODEX_PLUGIN_ROOT}/x", "/etc/passwd", `C:\\private\\file`, `\\server\share\file`} {
		m, r, err := inspect(t, fstest.MapFS{"SKILL.md": {Data: []byte(body)}})
		if err != nil || r != nil || len(m.Warnings) != 1 || strings.Contains(m.Warnings[0], body) {
			t.Fatalf("body %q: warnings=%v reject=%+v err=%v", body, m.Warnings, r, err)
		}
	}
	for _, files := range []fstest.MapFS{{"SKILL.md": {Data: []byte("https://example.com/a http://example.org/b")}}, {"script.sh": {Data: []byte("/etc/passwd ../secret")}}} {
		m, r, err := inspect(t, files)
		if err != nil || r != nil || len(m.Warnings) != 0 {
			t.Fatalf("warnings=%v reject=%+v err=%v", m.Warnings, r, err)
		}
	}
}

func TestInspectHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	i := projection.Inspector{OpenRoot: func(string) (projection.Root, error) { t.Fatal("opened after cancellation"); return nil, nil }}
	_, r, err := i.Inspect(ctx, "root")
	if r != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("reject=%+v err=%v", r, err)
	}
}

func TestInspectPreservesFilesAndEmptyDirectories(t *testing.T) {
	m, r, err := inspect(t, fstest.MapFS{
		"SKILL.md": {Data: []byte("skill")}, "references/note.md": {Data: []byte("note")},
		"scripts/helper.sh": {Data: []byte("echo ok")}, "empty": {Mode: fs.ModeDir},
	})
	if err != nil || r != nil {
		t.Fatalf("inspect = %v %v", r, err)
	}
	if m.Root != "discovery-root" || m.Bytes != 16 || len(m.Files) != 3 {
		t.Fatalf("manifest = %+v", m)
	}
	if !reflect.DeepEqual(m.Directories, []string{"empty", "references", "scripts"}) {
		t.Fatalf("dirs = %v", m.Directories)
	}
	if m.Files[0] != (projection.File{Path: "SKILL.md", Source: "SKILL.md", Size: 5, SHA256: sha256.Sum256([]byte("skill"))}) {
		t.Fatalf("file = %+v", m.Files[0])
	}
}

func TestInspectRejectsInvalidResolvedPaths(t *testing.T) {
	for _, name := range []string{"../outside", "/absolute", `C:/outside`, `..\outside`, `a\b`, `a/../outside`} {
		t.Run(name, func(t *testing.T) {
			r := &linkRoot{testRoot: &testRoot{MapFS: fstest.MapFS{"link": {}}}, links: map[string]string{"link": name}}
			i := projection.Inspector{OpenRoot: func(string) (projection.Root, error) { return r, nil }}
			_, reject, err := i.Inspect(context.Background(), "root")
			if err != nil || reject == nil || reject.Reason != "invalid-path" {
				t.Fatalf("reject=%+v err=%v", reject, err)
			}
		})
	}
}

func TestInspectManifestOmitsBodies(t *testing.T) {
	const secret = "unique private source body"
	m, reject, err := inspect(t, fstest.MapFS{"SKILL.md": {Data: []byte(secret)}})
	if err != nil || reject != nil {
		t.Fatalf("reject=%+v err=%v", reject, err)
	}
	if strings.Contains(fmt.Sprintf("%+v", m), secret) {
		t.Fatal("manifest retained source body")
	}
	for _, typ := range []reflect.Type{reflect.TypeOf(m), reflect.TypeOf(projection.File{})} {
		for n := 0; n < typ.NumField(); n++ {
			if typ.Field(n).Type == reflect.TypeOf([]byte{}) {
				t.Fatal("manifest contains body slice")
			}
		}
	}
}
