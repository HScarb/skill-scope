package session_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/scarb/skope/internal/session"
	"github.com/scarb/skope/internal/skill"
)

var testNow = time.Date(2026, time.September, 5, 6, 7, 8, 0, time.FixedZone("test", 8*60*60))

func TestSessionProjectionWritesRejectLinkedParents(t *testing.T) {
	m := testManager(t.TempDir(), []byte{1, 2})
	sess, err := m.Stage(skill.AgentClaude, "copy")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.Abort() })
	outside := t.TempDir()
	link := filepath.Join(onlyStagingDir(t, filepath.Join(m.Home, "sessions")), "linked")
	if runtime.GOOS == "windows" {
		if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, outside).CombinedOutput(); err != nil {
			t.Fatalf("junction=%s error=%v", out, err)
		}
	} else if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Remove(link); err != nil {
			t.Error(err)
		}
	}()
	for _, path := range []string{filepath.Join(sess.Root, "linked", "escape"), filepath.Join(sess.Root, "linked")} {
		if err := sess.WriteDirectories([]string{path}); err == nil {
			t.Fatalf("accepted linked directory %q", path)
		}
		if err := sess.WriteNewFiles([]session.File{{Path: path, Data: []byte("unsafe")}}); err == nil {
			t.Fatalf("accepted linked file %q", path)
		}
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatalf("outside changed: %v error=%v", entries, err)
	}
}

func TestSessionProjectionWritesRejectFinalSymlinkWithoutChangingTarget(t *testing.T) {
	m := testManager(t.TempDir(), []byte{1, 2})
	sess, err := m.Stage(skill.AgentClaude, "copy")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.Abort() })
	staging := onlyStagingDir(t, filepath.Join(m.Home, "sessions"))
	target := filepath.Join(staging, "first")
	if err := os.WriteFile(target, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(staging, "link")
	if err := os.Symlink(target, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("file symlink privileges unavailable: %v", err)
		}
		t.Fatal(err)
	}
	err = sess.WriteNewFiles([]session.File{{Path: filepath.Join(sess.Root, "link"), Data: []byte("overwrite")}})
	if !errors.Is(err, fs.ErrExist) {
		t.Fatalf("symlink error=%v", err)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "keep" {
		t.Fatalf("target=%s error=%v", data, err)
	}
}

func TestSessionDirectoriesAndNewFilesRemainPrivateUntilPublish(t *testing.T) {
	m := testManager(t.TempDir(), []byte{1, 2})
	sess, err := m.Stage(skill.AgentClaude, "copy")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.Abort() })
	root := sess.Root
	sess.Root = filepath.Join(t.TempDir(), "untrusted")
	dir := sess.AgentPath("addDir", ".claude", "skills", "one", "empty")
	if err := m.WriteDirectories(sess, []string{dir}); err != nil {
		t.Fatal(err)
	}
	name := sess.AgentPath("addDir", ".claude", "skills", "one", "script.sh")
	if err := m.WriteNew(sess, []session.File{{Path: name, Data: []byte("first"), Mode: 0o777}}); err != nil {
		t.Fatal(err)
	}
	if err := sess.WriteNewFiles([]session.File{{Path: name, Data: []byte("second")}}); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("duplicate error=%v", err)
	}
	if _, err := os.Stat(root); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("final exists: %v", err)
	}
	if err := sess.Publish(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(name)
	if err != nil || string(data) != "first" {
		t.Fatalf("data=%s error=%v", data, err)
	}
	assertMode(t, name, 0o600)
	for p := dir; p != root; p = filepath.Dir(p) {
		assertMode(t, p, 0o700)
	}
}

func TestSessionProjectionWritesRejectInvalidStateAndPaths(t *testing.T) {
	for _, state := range []string{"preview", "published", "closed", "staged"} {
		t.Run(state, func(t *testing.T) {
			m := testManager(t.TempDir(), []byte{1, 2})
			var sess *session.Session
			var err error
			if state == "preview" {
				sess, err = m.Preview(skill.AgentClaude, "copy")
			} else {
				sess, err = m.Stage(skill.AgentClaude, "copy")
			}
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = sess.Abort() })
			if state == "published" {
				if err := sess.Publish(); err != nil {
					t.Fatal(err)
				}
			}
			if state == "closed" {
				if err := sess.Abort(); err != nil {
					t.Fatal(err)
				}
			}
			paths := []string{sess.AgentPath("valid")}
			if state == "staged" {
				paths = []string{sess.Root, filepath.Dir(sess.Root), sess.Root + "-similar/escape", filepath.Join(sess.Root, "..", "escape"), "relative"}
			}
			for _, path := range paths {
				if err := sess.WriteNewFiles([]session.File{{Path: path}}); err == nil {
					t.Fatalf("accepted file %q", path)
				}
				if err := sess.WriteDirectories([]string{path}); err == nil {
					t.Fatalf("accepted directory %q", path)
				}
			}
		})
	}
}

func TestSessionProjectionWritesRespectVolumeCaseAliases(t *testing.T) {
	home := t.TempDir()
	probe := filepath.Join(home, "CaseProbe")
	if err := os.WriteFile(probe, []byte("probe"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, probeErr := os.Stat(filepath.Join(home, "caseprobe"))
	if probeErr != nil && !errors.Is(probeErr, fs.ErrNotExist) {
		t.Fatal(probeErr)
	}
	m := testManager(home, []byte{1, 2})
	sess, err := m.Stage(skill.AgentClaude, "copy")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.Abort() })
	first, second := sess.AgentPath("Foo", "SKILL.md"), sess.AgentPath("foo", "SKILL.md")
	if err := sess.WriteNewFiles([]session.File{{Path: first, Data: []byte("first")}}); err != nil {
		t.Fatal(err)
	}
	err = sess.WriteNewFiles([]session.File{{Path: second, Data: []byte("second")}})
	if probeErr == nil {
		if !errors.Is(err, fs.ErrExist) {
			t.Fatalf("alias error=%v", err)
		}
	} else if err != nil {
		t.Fatal(err)
	}
	if err := sess.Publish(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(first)
	if err != nil || string(data) != "first" {
		t.Fatalf("first=%s error=%v", data, err)
	}
	if probeErr != nil {
		data, err = os.ReadFile(second)
		if err != nil || string(data) != "second" {
			t.Fatalf("second=%s error=%v", data, err)
		}
	}
}

type fakeProcesses struct {
	tokens map[int]string
	errors map[int]error
}

func (p fakeProcesses) StartToken(pid int) (string, error) {
	if err := p.errors[pid]; err != nil {
		return "", err
	}
	if token, ok := p.tokens[pid]; ok {
		return token, nil
	}
	return "", session.ErrProcessNotFound
}

func TestSessionAgentPath(t *testing.T) {
	t.Parallel()

	finalRoot := filepath.Join("final", "sessions", "abc")
	sess := session.Session{Root: finalRoot, Agent: skill.AgentClaude}
	parts := []string{"settings.json"}
	wantParts := append([]string(nil), parts...)

	got := sess.AgentPath(parts...)
	want := filepath.Join(finalRoot, "claude", "settings.json")
	if got != want {
		t.Fatalf("AgentPath() = %q, want %q", got, want)
	}
	if !reflect.DeepEqual(parts, wantParts) {
		t.Fatalf("AgentPath() changed parts to %v, want %v", parts, wantParts)
	}
}

func TestManagerPreviewDoesNotTouchDisk(t *testing.T) {
	t.Parallel()

	home := filepath.Join(t.TempDir(), "skope")
	m := testManager(home, []byte{0xab, 0x01})

	sess, err := m.Preview(skill.AgentClaude, "dev")
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	wantRoot := filepath.Join(home, "sessions", "claude-20260905-060708-ab01")
	if sess.Root != wantRoot {
		t.Fatalf("Preview().Root = %q, want %q", sess.Root, wantRoot)
	}
	if got := sess.AgentPath("settings.json"); got != filepath.Join(wantRoot, "claude", "settings.json") {
		t.Fatalf("AgentPath() = %q", got)
	}
	if _, err := os.Stat(filepath.Join(home, "sessions")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Preview() created sessions root: %v", err)
	}
}

func TestManagerPreviewReadsExactlyTwoRandomBytes(t *testing.T) {
	t.Parallel()

	random := bytes.NewReader([]byte{0x01, 0x02, 0x03})
	m := testManager(filepath.Join(t.TempDir(), "skope"), nil)
	m.Random = random
	if _, err := m.Preview(skill.AgentClaude, "dev"); err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	if random.Len() != 1 {
		t.Fatalf("Preview() left %d random bytes, want 1", random.Len())
	}
}

func TestManagerPreviewReturnsRandomReadErrorWithoutTouchingDisk(t *testing.T) {
	t.Parallel()

	home := filepath.Join(t.TempDir(), "skope")
	m := testManager(home, nil)
	m.Random = bytes.NewReader([]byte{0x01})
	if _, err := m.Preview(skill.AgentClaude, "dev"); err == nil {
		t.Fatal("Preview() error = nil, want short random read error")
	}
	if _, err := os.Stat(filepath.Join(home, "sessions")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Preview() created sessions root after error: %v", err)
	}
}

func TestManagerStageWritesPrivateOwnerInStaging(t *testing.T) {
	t.Parallel()

	home := filepath.Join(t.TempDir(), "skope")
	m := testManager(home, []byte{0x00, 0xff})
	sess, err := m.Stage(skill.AgentClaude, "team dev")
	if err != nil {
		t.Fatalf("Stage() error = %v", err)
	}
	t.Cleanup(func() { _ = sess.Abort() })

	wantRoot := filepath.Join(home, "sessions", "claude-20260905-060708-00ff")
	if sess.Root != wantRoot {
		t.Fatalf("Stage().Root = %q, want %q", sess.Root, wantRoot)
	}
	if _, err := os.Stat(wantRoot); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("final root exists before Publish: %v", err)
	}
	sessionsRoot := filepath.Join(home, "sessions")
	assertMode(t, sessionsRoot, 0o700)
	stagingRoot := onlyStagingDir(t, sessionsRoot)
	assertMode(t, stagingRoot, 0o700)
	ownerPath := filepath.Join(stagingRoot, "owner.json")
	assertMode(t, ownerPath, 0o600)

	raw, err := os.ReadFile(ownerPath)
	if err != nil {
		t.Fatal(err)
	}
	var owner session.Owner
	if err := json.Unmarshal(raw, &owner); err != nil {
		t.Fatal(err)
	}
	wantOwner := session.Owner{PID: 321, ProcessStart: "start-321", Agent: skill.AgentClaude, SkillSet: "team dev", CreatedAt: testNow.UTC()}
	if !reflect.DeepEqual(owner, wantOwner) {
		t.Fatalf("owner = %#v, want %#v", owner, wantOwner)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	if want := []string{"agent", "createdAt", "pid", "processStart", "skillSet"}; !slices.Equal(keys, want) {
		t.Fatalf("owner fields = %v, want %v", keys, want)
	}
}

func TestManagerStageProcessFailureLeavesNoStaging(t *testing.T) {
	t.Parallel()

	home := filepath.Join(t.TempDir(), "skope")
	m := testManager(home, []byte{1, 2})
	m.Processes = fakeProcesses{errors: map[int]error{321: errors.New("inspection failed")}}
	if _, err := m.Stage(skill.AgentClaude, "dev"); err == nil {
		t.Fatal("Stage() error = nil, want process inspection error")
	}
	entries, err := os.ReadDir(filepath.Join(home, "sessions"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("entries after failed Stage = %v", entryNames(entries))
	}
}

func TestManagerStageOwnerEncodingFailureCleansStaging(t *testing.T) {
	t.Parallel()

	home := filepath.Join(t.TempDir(), "skope")
	m := testManager(home, []byte{21, 22})
	m.Now = func() time.Time { return time.Date(10000, time.January, 1, 0, 0, 0, 0, time.UTC) }
	if _, err := m.Stage(skill.AgentClaude, "dev"); err == nil || !strings.Contains(err.Error(), "encode owner") {
		t.Fatalf("Stage() error = %v, want owner encoding error", err)
	}
	entries, err := os.ReadDir(filepath.Join(home, "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("entries after failed owner encode = %v, want empty", entryNames(entries))
	}
}

func TestSessionWriteFilesUsesFinalPathsAndPreservesInput(t *testing.T) {
	t.Parallel()

	home := filepath.Join(t.TempDir(), "skope")
	m := testManager(home, []byte{3, 4})
	sess, err := m.Stage(skill.AgentClaude, "dev")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.Abort() })
	files := []session.File{{Path: filepath.Join(sess.Root, "claude", "settings.json"), Data: []byte("settings"), Mode: 0o777}}
	wantFiles := cloneFiles(files)
	if err := sess.WriteFiles(files); err != nil {
		t.Fatalf("WriteFiles() error = %v", err)
	}
	if !reflect.DeepEqual(files, wantFiles) {
		t.Fatalf("WriteFiles() changed input: got %#v, want %#v", files, wantFiles)
	}
	if _, err := os.Stat(files[0].Path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("file exists under final root before Publish: %v", err)
	}
	stagedPath := filepath.Join(onlyStagingDir(t, filepath.Join(home, "sessions")), "claude", "settings.json")
	got, err := os.ReadFile(stagedPath)
	if err != nil || string(got) != "settings" {
		t.Fatalf("staged file = %q, %v", got, err)
	}
	assertMode(t, filepath.Dir(stagedPath), 0o700)
	assertMode(t, stagedPath, 0o600)
	if err := sess.Publish(); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	got, err = os.ReadFile(files[0].Path)
	if err != nil || string(got) != "settings" {
		t.Fatalf("published file = %q, %v", got, err)
	}
	if sess.Root != filepath.Join(home, "sessions", "claude-20260905-060708-0304") {
		t.Fatalf("Publish() changed Root to %q", sess.Root)
	}
}

func TestSessionWriteFilesRejectsPathsOutsideFinalRoot(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	cases := map[string]func(*session.Session) string{
		"root itself":      func(sess *session.Session) string { return sess.Root },
		"absolute outside": func(*session.Session) string { return filepath.Join(base, "outside") },
		"parent traversal": func(sess *session.Session) string { return filepath.Join(sess.Root, "..", "escape") },
	}
	for name, path := range cases {
		name, path := name, path
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			m := testManager(filepath.Join(base, strings.ReplaceAll(name, " ", "-")), []byte{5, 6})
			sess, err := m.Stage(skill.AgentClaude, "dev")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = sess.Abort() })
			if err := sess.WriteFiles([]session.File{{Path: path(sess), Data: []byte("x")}}); err == nil {
				t.Fatal("WriteFiles() error = nil, want invalid path error")
			}
		})
	}
}

func TestSessionAbortCleansPartialWrite(t *testing.T) {
	t.Parallel()

	home := filepath.Join(t.TempDir(), "skope")
	m := testManager(home, []byte{7, 8})
	sess, err := m.Stage(skill.AgentClaude, "dev")
	if err != nil {
		t.Fatal(err)
	}
	files := []session.File{
		{Path: filepath.Join(sess.Root, "block"), Data: []byte("file")},
		{Path: filepath.Join(sess.Root, "block", "child"), Data: []byte("cannot write")},
	}
	if err := m.Write(sess, files); err == nil {
		t.Fatal("Write() error = nil, want parent-is-file error")
	}
	if err := m.Abort(sess); err != nil {
		t.Fatalf("Abort() error = %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(home, "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("entries after Abort = %v", entryNames(entries))
	}
}

func TestSessionPublishDoesNotReplaceExistingFinal(t *testing.T) {
	t.Parallel()

	home := filepath.Join(t.TempDir(), "skope")
	m := testManager(home, []byte{9, 10})
	sess, err := m.Stage(skill.AgentClaude, "dev")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.Abort() })
	if err := os.MkdirAll(sess.Root, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(sess.Root, "marker")
	if err := os.WriteFile(marker, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := m.Publish(sess); err == nil {
		t.Fatal("Publish() error = nil, want destination-exists error")
	}
	got, err := os.ReadFile(marker)
	if err != nil || string(got) != "keep" {
		t.Fatalf("existing final changed: data=%q err=%v", got, err)
	}
}

func TestSessionPublishDoesNotReplaceEmptyFinal(t *testing.T) {
	t.Parallel()

	home := filepath.Join(t.TempDir(), "skope")
	m := testManager(home, []byte{19, 20})
	sess, err := m.Stage(skill.AgentClaude, "dev")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.Abort() })
	if err := os.MkdirAll(sess.Root, 0o700); err != nil {
		t.Fatal(err)
	}

	if err := sess.Publish(); err == nil {
		t.Fatal("Publish() error = nil, want destination-exists error")
	}
	entries, err := os.ReadDir(sess.Root)
	if err != nil {
		t.Fatalf("ReadDir(empty final) error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("empty final was replaced with entries %v", entryNames(entries))
	}
}

func TestSessionAbortAfterPublishRemovesOnlyFinalChild(t *testing.T) {
	t.Parallel()

	home := filepath.Join(t.TempDir(), "skope")
	m := testManager(home, []byte{11, 12})
	sess, err := m.Stage(skill.AgentClaude, "dev")
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.Publish(); err != nil {
		t.Fatal(err)
	}
	if err := sess.Abort(); err != nil {
		t.Fatalf("Abort() error = %v", err)
	}
	if _, err := os.Stat(sess.Root); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("final root after Abort: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "sessions")); err != nil {
		t.Fatalf("Abort removed sessions root: %v", err)
	}
}

func TestSessionOperationsIgnoreMutatedPublicRoot(t *testing.T) {
	t.Parallel()

	home := filepath.Join(t.TempDir(), "skope")
	m := testManager(home, []byte{23, 24})
	sess, err := m.Stage(skill.AgentClaude, "dev")
	if err != nil {
		t.Fatal(err)
	}
	finalA := sess.Root
	siblingB := filepath.Join(home, "sessions", "sibling-b")
	if err := os.Mkdir(siblingB, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(siblingB, "marker")
	if err := os.WriteFile(marker, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	sess.Root = siblingB

	settings := filepath.Join(finalA, "claude", "settings.json")
	if err := sess.WriteFiles([]session.File{{Path: settings, Data: []byte("settings")}}); err != nil {
		t.Fatalf("WriteFiles() after Root mutation error = %v", err)
	}
	if err := sess.Publish(); err != nil {
		t.Fatalf("Publish() after Root mutation error = %v", err)
	}
	if got, err := os.ReadFile(settings); err != nil || string(got) != "settings" {
		t.Fatalf("published settings = %q, %v", got, err)
	}
	if err := sess.Abort(); err != nil {
		t.Fatalf("Abort() after Root mutation error = %v", err)
	}
	if _, err := os.Stat(finalA); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("private final root remains after Abort: %v", err)
	}
	if got, err := os.ReadFile(marker); err != nil || string(got) != "keep" {
		t.Fatalf("sibling marker = %q, %v", got, err)
	}
}

func TestManagerReapClassifiesSessionsAndContinuesAfterError(t *testing.T) {
	t.Parallel()

	home := filepath.Join(t.TempDir(), "skope")
	m := testManager(home, []byte{13, 14})
	m.Processes = fakeProcesses{tokens: map[int]string{1: "live", 2: "changed"}, errors: map[int]error{4: errors.New("access denied")}}
	sessionsRoot := filepath.Join(home, "sessions")
	if err := os.MkdirAll(sessionsRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	writeOwnerDir(t, sessionsRoot, "live", validOwner(1, "live"), testNow.Add(-2*time.Hour))
	writeOwnerDir(t, sessionsRoot, "reused", validOwner(2, "old"), testNow)
	writeOwnerDir(t, sessionsRoot, "exited", validOwner(3, "gone"), testNow)
	writeOwnerDir(t, sessionsRoot, "inspect-error", validOwner(4, "unknown"), testNow.Add(-2*time.Hour))
	makeAgedDir(t, sessionsRoot, ".staging-new", testNow.Add(-59*time.Minute))
	makeAgedDir(t, sessionsRoot, ".staging-old", testNow.Add(-61*time.Minute))
	makeAgedDir(t, sessionsRoot, "ownerless-new", testNow.Add(-59*time.Minute))
	makeAgedDir(t, sessionsRoot, "ownerless-old", testNow.Add(-61*time.Minute))
	bad := makeAgedDir(t, sessionsRoot, "bad-owner-old", testNow.Add(-61*time.Minute))
	if err := os.WriteFile(filepath.Join(bad, "owner.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(bad, testNow.Add(-61*time.Minute), testNow.Add(-61*time.Minute)); err != nil {
		t.Fatal(err)
	}
	readError := makeAgedDir(t, sessionsRoot, "owner-read-error", testNow.Add(-2*time.Hour))
	if err := os.Mkdir(filepath.Join(readError, "owner.json"), 0o700); err != nil {
		t.Fatal(err)
	}

	warnings := m.Reap()
	if len(warnings) != 2 {
		t.Fatalf("Reap() warnings = %v, want process and owner read warnings", warnings)
	}
	assertEntries(t, sessionsRoot, []string{".staging-new", "inspect-error", "live", "owner-read-error", "ownerless-new"})
}

func TestManagerReapMissingRootReturnsNoWarnings(t *testing.T) {
	t.Parallel()
	m := testManager(filepath.Join(t.TempDir(), "missing"), []byte{15, 16})
	if warnings := m.Reap(); len(warnings) != 0 {
		t.Fatalf("Reap() warnings = %v, want none", warnings)
	}
}

func TestManagerRejectsLinkedSessionsRootWithoutTouchingTarget(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	home := filepath.Join(base, "home")
	external := filepath.Join(base, "external")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(external, 0o700); err != nil {
		t.Fatal(err)
	}
	oldSession := makeAgedDir(t, external, "old-session", testNow.Add(-2*time.Hour))
	marker := filepath.Join(oldSession, "marker")
	if err := os.WriteFile(marker, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(home, "sessions")); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("directory symlink unavailable: %v", err)
		}
		t.Fatal(err)
	}
	m := testManager(home, []byte{25, 26})
	if sess, err := m.Stage(skill.AgentClaude, "dev"); err == nil {
		_ = sess.Abort()
		t.Fatal("Stage() error = nil for linked sessions root")
	}
	if warnings := m.Reap(); len(warnings) != 1 {
		t.Fatalf("Reap() warnings = %v, want linked-root warning", warnings)
	}
	if got, err := os.ReadFile(marker); err != nil || string(got) != "keep" {
		t.Fatalf("external marker = %q, %v", got, err)
	}
}

func TestManagerReapDoesNotTraverseLinkedChild(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	home := filepath.Join(base, "home")
	sessionsRoot := filepath.Join(home, "sessions")
	external := filepath.Join(base, "external")
	if err := os.MkdirAll(sessionsRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(external, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(external, "marker")
	if err := os.WriteFile(marker, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(sessionsRoot, "linked-child")); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("directory symlink unavailable: %v", err)
		}
		t.Fatal(err)
	}
	m := testManager(home, []byte{27, 28})
	if warnings := m.Reap(); len(warnings) != 1 {
		t.Fatalf("Reap() warnings = %v, want linked-child warning", warnings)
	}
	if got, err := os.ReadFile(marker); err != nil || string(got) != "keep" {
		t.Fatalf("external marker = %q, %v", got, err)
	}
}

func TestManagerReapTreatsInvalidOwnerAsAbandonedByAge(t *testing.T) {
	t.Parallel()

	home := filepath.Join(t.TempDir(), "skope")
	sessionsRoot := filepath.Join(home, "sessions")
	if err := os.MkdirAll(sessionsRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	invalid := map[string]string{
		"null":           "null",
		"empty":          `{}`,
		"missing-field":  `{"pid":1,"processStart":"token","agent":"claude","skillSet":"dev"}`,
		"unknown-field":  `{"pid":1,"processStart":"token","agent":"claude","skillSet":"dev","createdAt":"2026-09-04T22:07:08Z","extra":true}`,
		"invalid-agent":  `{"pid":1,"processStart":"token","agent":"other","skillSet":"dev","createdAt":"2026-09-04T22:07:08Z"}`,
		"trailing-value": `{"pid":1,"processStart":"token","agent":"claude","skillSet":"dev","createdAt":"2026-09-04T22:07:08Z"} {}`,
	}
	want := make([]string, 0, len(invalid))
	for name, raw := range invalid {
		writeRawOwnerDir(t, sessionsRoot, "new-"+name, raw, testNow.Add(-59*time.Minute))
		writeRawOwnerDir(t, sessionsRoot, "old-"+name, raw, testNow.Add(-61*time.Minute))
		want = append(want, "new-"+name)
	}
	m := testManager(home, []byte{29, 30})
	if warnings := m.Reap(); len(warnings) != 0 {
		t.Fatalf("Reap() warnings = %v, want none", warnings)
	}
	assertEntries(t, sessionsRoot, want)
}

func TestManagerReapContinuesAfterRemoveError(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("uses a Windows open-file lock for deterministic removal failure")
	}

	home := filepath.Join(t.TempDir(), "skope")
	m := testManager(home, []byte{17, 18})
	sessionsRoot := filepath.Join(home, "sessions")
	if err := os.MkdirAll(sessionsRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	blocked := makeAgedDir(t, sessionsRoot, "blocked", testNow.Add(-2*time.Hour))
	lockedPath := filepath.Join(blocked, "locked")
	if err := os.WriteFile(lockedPath, []byte("locked"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(blocked, testNow.Add(-2*time.Hour), testNow.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	locked, err := lockDeletion(lockedPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := locked.Close(); err != nil {
			t.Errorf("close deletion lock: %v", err)
		}
	})
	makeAgedDir(t, sessionsRoot, "removable", testNow.Add(-2*time.Hour))

	warnings := m.Reap()
	if len(warnings) != 1 {
		t.Fatalf("Reap() warnings = %v, want one removal warning", warnings)
	}
	if _, err := os.Stat(filepath.Join(sessionsRoot, "removable")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Reap() did not continue after remove error: %v", err)
	}
	if _, err := os.Stat(blocked); err != nil {
		t.Fatalf("blocked session should remain: %v", err)
	}
}

func TestOSProcessInspectorIsUnsupportedOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows contract")
	}
	_, err := (session.OSProcessInspector{}).StartToken(os.Getpid())
	if !errors.Is(err, session.ErrUnsupported) {
		t.Fatalf("StartToken() error = %v, want ErrUnsupported", err)
	}
}

func testManager(home string, random []byte) *session.Manager {
	return &session.Manager{
		Home: home, Now: func() time.Time { return testNow }, Random: bytes.NewReader(random), PID: 321,
		Processes: fakeProcesses{tokens: map[int]string{321: "start-321"}},
	}
}

func onlyStagingDir(t *testing.T, sessionsRoot string) string {
	t.Helper()
	entries, err := os.ReadDir(sessionsRoot)
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), ".staging-") {
			found = append(found, filepath.Join(sessionsRoot, entry.Name()))
		}
	}
	if len(found) != 1 {
		t.Fatalf("staging dirs = %v, want one", found)
	}
	return found[0]
}

func cloneFiles(files []session.File) []session.File {
	cloned := make([]session.File, len(files))
	for i, file := range files {
		cloned[i] = file
		cloned[i].Data = append([]byte(nil), file.Data...)
	}
	return cloned
}

func assertMode(t *testing.T, path string, want fs.FileMode) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("mode(%s) = %04o, want %04o", path, got, want)
	}
}

func entryNames(entries []os.DirEntry) []string {
	names := make([]string, len(entries))
	for i, entry := range entries {
		names[i] = entry.Name()
	}
	return names
}

func writeOwnerDir(t *testing.T, root, name string, owner session.Owner, mtime time.Time) {
	t.Helper()
	dir := makeAgedDir(t, root, name, mtime)
	raw, err := json.Marshal(owner)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "owner.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(dir, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

func validOwner(pid int, token string) session.Owner {
	return session.Owner{
		PID:          pid,
		ProcessStart: token,
		Agent:        skill.AgentClaude,
		SkillSet:     "dev",
		CreatedAt:    testNow.UTC(),
	}
}

func writeRawOwnerDir(t *testing.T, root, name, raw string, mtime time.Time) {
	t.Helper()
	dir := makeAgedDir(t, root, name, mtime)
	if err := os.WriteFile(filepath.Join(dir, "owner.json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(dir, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

func makeAgedDir(t *testing.T, root, name string, mtime time.Time) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(dir, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	return dir
}

func assertEntries(t *testing.T, root string, want []string) {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	got := entryNames(entries)
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("entries = %v, want %v", got, want)
	}
}
