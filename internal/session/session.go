package session

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/scarb/skope/internal/skill"
)

type Session struct {
	Root  string
	Agent skill.Agent

	finalRoot    string
	finalName    string
	stagingName  string
	sessionsRoot *os.Root
	publishDir   *os.File
	published    bool
	managed      bool
	closed       bool
}

type File struct {
	Path string
	Data []byte
	Mode fs.FileMode
}

type Manager struct {
	Home      string
	Now       func() time.Time
	Random    io.Reader
	PID       int
	Processes ProcessInspector
}

func NewManager(home string) *Manager {
	return &Manager{
		Home:      home,
		Now:       time.Now,
		Random:    rand.Reader,
		PID:       os.Getpid(),
		Processes: OSProcessInspector{},
	}
}

func (s *Session) AgentPath(parts ...string) string {
	all := append([]string{s.immutableRoot(), string(s.Agent)}, parts...)
	return filepath.Join(all...)
}

func (m *Manager) Preview(agent skill.Agent, _ string) (*Session, error) {
	suffix := make([]byte, 2)
	if _, err := io.ReadFull(m.random(), suffix); err != nil {
		return nil, fmt.Errorf("read session suffix: %w", err)
	}
	name := fmt.Sprintf("%s-%s-%s", agent, m.now().Format("20060102-150405"), hex.EncodeToString(suffix))
	finalRoot := filepath.Join(m.Home, "sessions", name)
	return &Session{
		Root:      finalRoot,
		Agent:     agent,
		finalRoot: finalRoot,
		finalName: name,
	}, nil
}

func (m *Manager) Stage(agent skill.Agent, displayName string) (_ *Session, retErr error) {
	sess, err := m.Preview(agent, displayName)
	if err != nil {
		return nil, err
	}
	startToken, err := m.processes().StartToken(m.PID)
	if err != nil {
		return nil, fmt.Errorf("inspect owner process %d: %w", m.PID, err)
	}
	sess.sessionsRoot, err = openSessionsRoot(m.Home, true)
	if err != nil {
		return nil, err
	}
	sess.managed = true
	defer func() {
		if retErr == nil {
			return
		}
		if sess.stagingName != "" {
			if cleanupErr := removeRootChild(sess.sessionsRoot, sess.stagingName); cleanupErr != nil {
				retErr = errors.Join(retErr, fmt.Errorf("clean staging session: %w", cleanupErr))
			}
		}
		if closeErr := sess.closeHandles(); closeErr != nil {
			retErr = errors.Join(retErr, fmt.Errorf("close failed session: %w", closeErr))
		}
	}()

	sess.stagingName, err = createStagingDir(sess.sessionsRoot)
	if err != nil {
		return nil, err
	}
	owner := Owner{
		PID:          m.PID,
		ProcessStart: startToken,
		Agent:        agent,
		SkillSet:     displayName,
		CreatedAt:    m.now().UTC(),
	}
	raw, err := json.Marshal(owner)
	if err != nil {
		return nil, fmt.Errorf("encode owner: %w", err)
	}
	if err := writeRootFile(sess.sessionsRoot, filepath.Join(sess.stagingName, "owner.json"), raw); err != nil {
		return nil, fmt.Errorf("write owner: %w", err)
	}
	sess.publishDir, err = sess.sessionsRoot.Open(".")
	if err != nil {
		return nil, fmt.Errorf("open sessions directory for publish: %w", err)
	}
	return sess, nil
}

func (m *Manager) Write(sess *Session, files []File) error {
	return sess.WriteFiles(files)
}

func (m *Manager) Publish(sess *Session) error {
	return sess.Publish()
}

func (m *Manager) Abort(sess *Session) error {
	return sess.Abort()
}

func (s *Session) WriteFiles(files []File) error {
	if s.sessionsRoot == nil || s.stagingName == "" || s.published || s.closed {
		return errors.New("session is not staged")
	}
	type target struct {
		path string
		data []byte
	}
	targets := make([]target, len(files))
	for i, file := range files {
		rel, err := filepath.Rel(s.finalRoot, file.Path)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			return fmt.Errorf("file path %q is outside session root %q", file.Path, s.finalRoot)
		}
		targets[i] = target{path: filepath.Join(s.stagingName, rel), data: file.Data}
	}
	for _, target := range targets {
		parent := filepath.Dir(target.path)
		if err := mkdirAllRoot(s.sessionsRoot, parent, 0o700); err != nil {
			return fmt.Errorf("create session file parent: %w", err)
		}
		if err := chmodRootDir(s.sessionsRoot, parent, 0o700); err != nil {
			return fmt.Errorf("set session file parent permissions: %w", err)
		}
		if err := writeRootFile(s.sessionsRoot, target.path, target.data); err != nil {
			return fmt.Errorf("write session file: %w", err)
		}
	}
	return nil
}

func (s *Session) Publish() error {
	if s.sessionsRoot == nil || s.publishDir == nil || s.stagingName == "" || s.published || s.closed {
		return errors.New("session is not staged")
	}
	if err := validateChildName(s.finalName); err != nil {
		return err
	}
	if err := renameNoReplace(s.publishDir, s.stagingName, s.finalName); err != nil {
		return fmt.Errorf("publish session: %w", err)
	}
	s.published = true
	err := s.publishDir.Close()
	s.publishDir = nil
	if err != nil {
		return fmt.Errorf("close publish directory: %w", err)
	}
	return nil
}

func (s *Session) Abort() error {
	if !s.managed {
		return errors.New("session is not managed")
	}
	if s.closed {
		return nil
	}
	target := s.stagingName
	if s.published {
		target = s.finalName
	}
	if target != "" {
		if err := removeRootChild(s.sessionsRoot, target); err != nil {
			return fmt.Errorf("abort session: %w", err)
		}
	}
	err := s.closeHandles()
	s.closed = true
	if err != nil {
		return fmt.Errorf("close aborted session: %w", err)
	}
	return nil
}

func (s *Session) immutableRoot() string {
	if s.finalRoot != "" {
		return s.finalRoot
	}
	return s.Root
}

func (s *Session) closeHandles() error {
	var errs []error
	if s.publishDir != nil {
		if err := s.publishDir.Close(); err != nil {
			errs = append(errs, err)
		}
		s.publishDir = nil
	}
	if s.sessionsRoot != nil {
		if err := s.sessionsRoot.Close(); err != nil {
			errs = append(errs, err)
		}
		s.sessionsRoot = nil
	}
	return errors.Join(errs...)
}

func (m *Manager) now() time.Time {
	if m.Now == nil {
		return time.Now()
	}
	return m.Now()
}

func (m *Manager) random() io.Reader {
	if m.Random == nil {
		return rand.Reader
	}
	return m.Random
}

func (m *Manager) processes() ProcessInspector {
	if m.Processes == nil {
		return OSProcessInspector{}
	}
	return m.Processes
}

func openSessionsRoot(home string, create bool) (*os.Root, error) {
	path := filepath.Join(home, "sessions")
	if create {
		if err := os.MkdirAll(home, 0o700); err != nil {
			return nil, fmt.Errorf("create skope home: %w", err)
		}
		if err := os.Mkdir(path, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
			return nil, fmt.Errorf("create sessions root: %w", err)
		}
	}
	before, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect sessions root: %w", err)
	}
	if isLinkOrReparse(before) || !before.IsDir() {
		return nil, fmt.Errorf("sessions root %q is not a direct directory", path)
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, fmt.Errorf("open sessions root: %w", err)
	}
	after, err := root.Stat(".")
	if err != nil {
		return nil, errors.Join(fmt.Errorf("verify sessions root: %w", err), root.Close())
	}
	if !os.SameFile(before, after) {
		return nil, errors.Join(errors.New("sessions root changed while opening"), root.Close())
	}
	if create {
		if err := chmodRootDir(root, ".", 0o700); err != nil {
			return nil, errors.Join(fmt.Errorf("set sessions root permissions: %w", err), root.Close())
		}
	}
	return root, nil
}

func createStagingDir(root *os.Root) (string, error) {
	for {
		suffix := make([]byte, 8)
		if _, err := io.ReadFull(rand.Reader, suffix); err != nil {
			return "", fmt.Errorf("read staging suffix: %w", err)
		}
		name := ".staging-" + hex.EncodeToString(suffix)
		if err := root.Mkdir(name, 0o700); errors.Is(err, fs.ErrExist) {
			continue
		} else if err != nil {
			return "", fmt.Errorf("create staging session: %w", err)
		}
		if err := chmodRootDir(root, name, 0o700); err != nil {
			return name, fmt.Errorf("set staging permissions: %w", err)
		}
		return name, nil
	}
}

func chmodRootDir(root *os.Root, name string, mode fs.FileMode) (retErr error) {
	dir, err := root.Open(name)
	if err != nil {
		return err
	}
	defer func() {
		if err := dir.Close(); err != nil {
			retErr = errors.Join(retErr, err)
		}
	}()
	return dir.Chmod(mode)
}

func writeRootFile(root *os.Root, name string, data []byte) (retErr error) {
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		if err := file.Close(); err != nil {
			retErr = errors.Join(retErr, err)
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		return err
	}
	written, err := file.Write(data)
	if err != nil {
		return err
	}
	if written != len(data) {
		return io.ErrShortWrite
	}
	return nil
}

func removeRootChild(root *os.Root, name string) error {
	if err := validateChildName(name); err != nil {
		return err
	}
	info, err := root.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if isLinkOrReparse(info) || !info.IsDir() {
		return fmt.Errorf("refusing to recursively remove non-directory session child %q", name)
	}
	child, err := openVerifiedChildRoot(root, name, info)
	if err != nil {
		return err
	}
	if err := removeRootContents(child); err != nil {
		return errors.Join(err, child.Close())
	}
	if err := child.Close(); err != nil {
		return err
	}
	return root.Remove(name)
}

func validateChildName(name string) error {
	if name == "" || name == "." || name == ".." || filepath.Base(name) != name || filepath.IsAbs(name) {
		return fmt.Errorf("refusing session target %q outside direct children", name)
	}
	return nil
}

func mkdirAllRoot(root *os.Root, name string, mode fs.FileMode) error {
	clean := filepath.Clean(name)
	if clean == "." {
		return nil
	}
	if !filepath.IsLocal(clean) {
		return fmt.Errorf("directory path %q is outside root", name)
	}
	current := ""
	for _, part := range strings.FieldsFunc(clean, func(r rune) bool {
		return r == rune(filepath.Separator) || filepath.Separator == '\\' && r == '/'
	}) {
		current = filepath.Join(current, part)
		if err := root.Mkdir(current, mode); err != nil && !errors.Is(err, fs.ErrExist) {
			return err
		}
		info, err := root.Lstat(current)
		if err != nil {
			return err
		}
		if isLinkOrReparse(info) || !info.IsDir() {
			return fmt.Errorf("directory path %q contains a linked or non-directory component", name)
		}
	}
	return nil
}

func readRootFile(root *os.Root, name string) (data []byte, retErr error) {
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := file.Close(); err != nil {
			retErr = errors.Join(retErr, err)
		}
	}()
	return io.ReadAll(file)
}

func removeRootContents(root *os.Root) error {
	entries, err := readRootDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		info, err := root.Lstat(name)
		if err != nil {
			return err
		}
		if info.IsDir() && !isLinkOrReparse(info) {
			child, err := openVerifiedChildRoot(root, name, info)
			if err != nil {
				return err
			}
			if err := removeRootContents(child); err != nil {
				return errors.Join(err, child.Close())
			}
			if err := child.Close(); err != nil {
				return err
			}
		}
		if err := root.Remove(name); err != nil {
			return err
		}
	}
	return nil
}

func openVerifiedChildRoot(root *os.Root, name string, before fs.FileInfo) (*os.Root, error) {
	child, err := root.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	after, err := child.Stat(".")
	if err != nil {
		return nil, errors.Join(err, child.Close())
	}
	if !os.SameFile(before, after) {
		return nil, errors.Join(fmt.Errorf("session child %q changed while opening", name), child.Close())
	}
	return child, nil
}
