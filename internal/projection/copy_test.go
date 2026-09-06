package projection_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"reflect"
	"testing"
	"testing/fstest"

	"github.com/scarb/skope/internal/projection"
)

func TestCopyPreservesSourceErrorsAndClosesHandles(t *testing.T) {
	for _, phase := range []int{1, 2, 3} {
		for _, operation := range []string{"open-root", "open-file", "read-dir", "stat", "read", "file-close", "root-close", "cancel-read", "cancel-close"} {
			if phase == 2 && operation == "read-dir" {
				continue // The copy root resolves files; inspection roots enumerate directories.
			}
			t.Run(fmt.Sprintf("phase-%d/%s", phase, operation), func(t *testing.T) {
				files := fstest.MapFS{"SKILL.md": {Data: []byte("skill")}}
				m, _, _ := inspect(t, files)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				boom := errors.New("source failure")
				want := boom
				if operation == "cancel-read" || operation == "cancel-close" {
					want = context.Canceled
				}
				var roots []*faultRoot
				calls := 0
				c := projection.Copier{OpenRoot: func(string) (projection.Root, error) {
					calls++
					if calls == phase && operation == "open-root" {
						return nil, boom
					}
					r := &faultRoot{testRoot: &testRoot{MapFS: files}}
					roots = append(roots, r)
					if calls == phase {
						switch operation {
						case "open-file":
							r.openErr = boom
						case "read-dir":
							r.readDirErr = boom
						case "stat":
							r.errorStat = boom
						case "read":
							r.errorRead = boom
						case "file-close":
							r.errorFileClose = boom
						case "root-close":
							r.closeErr = boom
						case "cancel-read":
							r.cancel = cancel
						case "cancel-close":
							r.cancelClose = cancel
						}
					}
					return r, nil
				}}
				err := c.Copy(ctx, m, &copySink{})
				if !errors.Is(err, want) {
					t.Fatalf("error=%v want=%v", err, want)
				}
				for n, r := range roots {
					if !r.closed {
						t.Fatalf("root %d leaked", n)
					}
					if r.openErr == nil && r.readDirErr == nil && r.closedFiles != 1 {
						t.Fatalf("root %d closed files=%d", n, r.closedFiles)
					}
				}
			})
		}
	}
}

func TestCopyClosesBothFilesWhenSecondReadFails(t *testing.T) {
	files := fstest.MapFS{"a": {Data: []byte("first")}, "b": {Data: []byte("second")}}
	m, _, _ := inspect(t, files)
	boom := errors.New("second read failed")
	calls := 0
	var actual *faultRoot
	c := projection.Copier{OpenRoot: func(string) (projection.Root, error) {
		calls++
		if calls == 2 {
			actual = &faultRoot{testRoot: &testRoot{MapFS: files}}
			return actual, nil
		}
		return &testRoot{MapFS: files}, nil
	}}
	sink := &copySink{write: func(string, []byte) error { actual.errorRead = boom; return nil }}
	if err := c.Copy(context.Background(), m, sink); !errors.Is(err, boom) {
		t.Fatalf("error=%v", err)
	}
	if !actual.closed || actual.closedFiles != 2 || len(sink.files) != 1 || string(sink.files["a"]) != "first" {
		t.Fatalf("closed=%v closedFiles=%d files=%v", actual.closed, actual.closedFiles, sink.files)
	}
}

func TestCopyExpandsInternalFileLinks(t *testing.T) {
	files := fstest.MapFS{"SKILL.md": {Data: []byte("skill")}, "alias.md": {Mode: fs.ModeSymlink}}
	open := func(string) (projection.Root, error) {
		return &linkRoot{testRoot: &testRoot{MapFS: files}, links: map[string]string{"alias.md": "SKILL.md"}}, nil
	}
	m, reject, err := (projection.Inspector{OpenRoot: open}).Inspect(context.Background(), "entry")
	if err != nil || reject != nil {
		t.Fatalf("inspect=%v %v", reject, err)
	}
	sink := &copySink{}
	if err := (projection.Copier{OpenRoot: open}).Copy(context.Background(), m, sink); err != nil {
		t.Fatal(err)
	}
	if len(sink.files) != 2 || string(sink.files["alias.md"]) != "skill" {
		t.Fatalf("files=%v", sink.files)
	}
}

func TestCopyValidatesActualReadAfterInitialRecheck(t *testing.T) {
	for _, operation := range []string{"edit", "grow", "special", "identity", "redirect", "grow-during-read"} {
		t.Run(operation, func(t *testing.T) {
			files := fstest.MapFS{"SKILL.md": {Data: []byte("old")}}
			m, _, _ := inspect(t, files)
			calls := 0
			var actual *faultRoot
			c := projection.Copier{OpenRoot: func(string) (projection.Root, error) {
				calls++
				if calls != 2 {
					return &testRoot{MapFS: files}, nil
				}
				actual = &faultRoot{testRoot: &testRoot{MapFS: files}}
				switch operation {
				case "edit":
					files["SKILL.md"].Data = []byte("new")
				case "grow":
					files["SKILL.md"].Data = make([]byte, projection.MaxBytes+1)
				case "special":
					actual.openedMode = fs.ModeNamedPipe
				case "identity":
					actual.changed = true
				case "redirect":
					return &linkRoot{testRoot: actual.testRoot, links: map[string]string{"SKILL.md": "outside"}}, nil
				case "grow-during-read":
					actual.remaining = projection.MaxBytes + 100
				}
				return actual, nil
			}}
			sink := &copySink{}
			if err := c.Copy(context.Background(), m, sink); err == nil {
				t.Fatal("changed actual read accepted")
			}
			if len(sink.files) != 0 {
				t.Fatal("unchecked contents reached sink")
			}
			if !actual.closed {
				t.Fatal("root leaked")
			}
			if operation != "redirect" && actual.closedFiles != 1 {
				t.Fatalf("closed files=%d", actual.closedFiles)
			}
			if (operation == "special" || operation == "identity" || operation == "grow") && actual.read != 0 {
				t.Fatal("read invalid opened file")
			}
			if operation == "grow-during-read" && actual.read > 4 {
				t.Fatalf("read beyond expected size+1: %d", actual.read)
			}
		})
	}
}

func TestCopyChecksCancellationBeforeOpening(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := projection.Copier{OpenRoot: func(string) (projection.Root, error) { t.Fatal("opened after cancellation"); return nil, nil }}
	if err := c.Copy(ctx, projection.Manifest{}, &copySink{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
}

func TestCopyJoinsSinkAndRootCloseFailures(t *testing.T) {
	files := fstest.MapFS{"SKILL.md": {}}
	m, _, _ := inspect(t, files)
	writeErr, closeErr := errors.New("write failure"), errors.New("close failure")
	calls := 0
	c := projection.Copier{OpenRoot: func(string) (projection.Root, error) {
		calls++
		r := &faultRoot{testRoot: &testRoot{MapFS: files}}
		if calls == 2 {
			r.closeErr = closeErr
		}
		return r, nil
	}}
	err := c.Copy(context.Background(), m, &copySink{write: func(string, []byte) error { return writeErr }})
	if !errors.Is(err, writeErr) || !errors.Is(err, closeErr) {
		t.Fatalf("error=%v", err)
	}
}

type copySink struct {
	files map[string][]byte
	dirs  []string
	write func(string, []byte) error
	mkdir func(string) error
}

func (s *copySink) Mkdir(name string) error {
	if s.mkdir != nil {
		if err := s.mkdir(name); err != nil {
			return err
		}
	}
	s.dirs = append(s.dirs, name)
	return nil
}
func (s *copySink) WriteFile(name string, data []byte) error {
	if s.write != nil {
		if err := s.write(name, data); err != nil {
			return err
		}
	}
	if s.files == nil {
		s.files = make(map[string][]byte)
	}
	s.files[name] = append([]byte(nil), data...)
	return nil
}

func TestCopyPreservesCompleteTree(t *testing.T) {
	files := fstest.MapFS{"SKILL.md": {Data: []byte("skill")}, "refs/note.md": {Data: []byte("note")}, "scripts/run.sh": {Data: []byte("echo ok"), Mode: 0o755}, "asset.bin": {Data: []byte{0, 255, 1}}, "empty": {Mode: fs.ModeDir}}
	m, reject, err := inspect(t, files)
	if err != nil || reject != nil {
		t.Fatalf("inspect=%v %v", reject, err)
	}
	var roots []*testRoot
	c := projection.Copier{OpenRoot: func(name string) (projection.Root, error) {
		if name != m.Root {
			t.Fatalf("opened %q instead of discovery root", name)
		}
		r := &testRoot{MapFS: files}
		roots = append(roots, r)
		return r, nil
	}}
	sink := &copySink{}
	if err := c.Copy(context.Background(), m, sink); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sink.dirs, []string{".", "empty", "refs", "scripts"}) {
		t.Fatalf("dirs=%v", sink.dirs)
	}
	for name, file := range files {
		if !file.Mode.IsDir() && !reflect.DeepEqual(sink.files[name], file.Data) {
			t.Fatalf("file %s=%v", name, sink.files[name])
		}
	}
	if len(sink.files) != 4 {
		t.Fatalf("files=%v", sink.files)
	}
	for _, r := range roots {
		if !r.closed {
			t.Fatal("root leaked")
		}
	}
}

func TestCopyRejectsSourceChangesBeforeWriting(t *testing.T) {
	for _, change := range []string{"edit", "grow", "add-file", "delete-file", "add-directory", "delete-directory", "special", "plugin"} {
		t.Run(change, func(t *testing.T) {
			files := fstest.MapFS{"SKILL.md": {Data: []byte("old")}, "empty": {Mode: fs.ModeDir}}
			m, _, err := inspect(t, files)
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "edit":
				files["SKILL.md"].Data = []byte("new")
			case "grow":
				files["SKILL.md"].Data = make([]byte, projection.MaxBytes+1)
			case "add-file":
				files["new"] = &fstest.MapFile{}
			case "delete-file":
				delete(files, "SKILL.md")
			case "add-directory":
				files["new"] = &fstest.MapFile{Mode: fs.ModeDir}
			case "delete-directory":
				delete(files, "empty")
			case "special":
				files["pipe"] = &fstest.MapFile{Mode: fs.ModeNamedPipe}
			case "plugin":
				files[".claude-plugin/plugin.json"] = &fstest.MapFile{}
			}
			c := projection.Copier{OpenRoot: func(string) (projection.Root, error) { return &testRoot{MapFS: files}, nil }}
			sink := &copySink{}
			if err := c.Copy(context.Background(), m, sink); err == nil {
				t.Fatal("changed source accepted")
			}
			if len(sink.files) != 0 || len(sink.dirs) != 0 {
				t.Fatal("wrote before validating tree")
			}
		})
	}
}

func TestCopyRejectsChangesDuringLastWrite(t *testing.T) {
	for _, change := range []string{"add-directory", "delete-directory", "edit", "delete-file"} {
		t.Run(change, func(t *testing.T) {
			files := fstest.MapFS{"SKILL.md": {Data: []byte("old")}, "empty": {Mode: fs.ModeDir}}
			m, _, err := inspect(t, files)
			if err != nil {
				t.Fatal(err)
			}
			c := projection.Copier{OpenRoot: func(string) (projection.Root, error) { return &testRoot{MapFS: files}, nil }}
			sink := &copySink{write: func(string, []byte) error {
				switch change {
				case "add-directory":
					files["new"] = &fstest.MapFile{Mode: fs.ModeDir}
				case "delete-directory":
					delete(files, "empty")
				case "edit":
					files["SKILL.md"].Data = []byte("new")
				case "delete-file":
					delete(files, "SKILL.md")
				}
				return nil
			}}
			if err := c.Copy(context.Background(), m, sink); err == nil {
				t.Fatal("late source change accepted")
			}
		})
	}
}

func TestCopyPreservesSinkFailures(t *testing.T) {
	for _, operation := range []string{"mkdir", "write"} {
		t.Run(operation, func(t *testing.T) {
			files := fstest.MapFS{"a": {}, "b": {}}
			m, _, _ := inspect(t, files)
			boom := errors.New("sink failure")
			var roots []*testRoot
			c := projection.Copier{OpenRoot: func(string) (projection.Root, error) {
				r := &testRoot{MapFS: files}
				roots = append(roots, r)
				return r, nil
			}}
			sink := &copySink{}
			if operation == "mkdir" {
				sink.mkdir = func(string) error { return boom }
			} else {
				sink.write = func(name string, _ []byte) error {
					if name == "b" {
						return boom
					}
					return nil
				}
			}
			if err := c.Copy(context.Background(), m, sink); !errors.Is(err, boom) {
				t.Fatalf("error=%v", err)
			}
			for _, r := range roots {
				if !r.closed {
					t.Fatal("root leaked")
				}
			}
		})
	}
}
