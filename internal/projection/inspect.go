package projection

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"
	"syscall"
)

func (i Inspector) Inspect(ctx context.Context, directory string) (manifest Manifest, rejection *Rejection, err error) {
	if err := ctx.Err(); err != nil {
		return manifest, nil, err
	}
	r, err := i.OpenRoot(directory)
	if err != nil {
		return manifest, nil, err
	}
	defer func() {
		closeErr := r.Close()
		err = errors.Join(err, closeErr, ctx.Err())
	}()
	s := inspection{ctx: ctx, root: r, manifest: Manifest{Root: directory}, targets: make(map[string]bool)}
	rejection, err = s.walk(".", ".", nil)
	sort.Strings(s.manifest.Directories)
	sort.Slice(s.manifest.Files, func(a, b int) bool { return s.manifest.Files[a].Path < s.manifest.Files[b].Path })
	sort.Strings(s.manifest.Warnings)
	return s.manifest, rejection, err
}

type inspection struct {
	ctx      context.Context
	root     Root
	manifest Manifest
	targets  map[string]bool
}

func (s *inspection) walk(source, target string, ancestors []fs.FileInfo) (*Rejection, error) {
	if err := s.ctx.Err(); err != nil {
		return nil, err
	}
	source, contained, err := s.root.Resolve(source)
	if errors.Is(err, syscall.ELOOP) {
		return &Rejection{Reason: "symlink-loop", Path: target}, nil
	}
	if err != nil {
		return nil, err
	}
	if !contained {
		return &Rejection{Reason: "outside-root", Path: target}, nil
	}
	if !validPath(source) || !validPath(target) {
		return &Rejection{Reason: "invalid-path", Path: target}, nil
	}
	info, err := s.root.Lstat(source)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() && !info.Mode().IsRegular() {
		return &Rejection{Reason: "special-file", Path: target}, nil
	}
	if target != "." && s.conflict(target, info.IsDir()) {
		return &Rejection{Reason: "target-conflict", Path: target}, nil
	}
	if !info.IsDir() {
		return s.file(target, source, info)
	}
	for _, ancestor := range ancestors {
		if s.root.SameFile(info, ancestor) {
			return &Rejection{Reason: "symlink-loop", Path: target}, nil
		}
	}
	if target != "." {
		s.manifest.Directories = append(s.manifest.Directories, target)
	}
	entries, err := s.root.ReadDir(source)
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(a, b int) bool { return entries[a].Name() < entries[b].Name() })
	for _, entry := range entries {
		if !validComponent(entry.Name()) {
			return &Rejection{Reason: "invalid-path", Path: target}, nil
		}
		next := path.Join(target, entry.Name())
		if strings.EqualFold(path.Base(target), ".claude-plugin") && strings.EqualFold(entry.Name(), "plugin.json") {
			return &Rejection{Reason: "plugin-manifest", Path: next}, nil
		}
		if r, err := s.walk(path.Join(source, entry.Name()), next, append(ancestors, info)); r != nil || err != nil {
			return r, err
		}
	}
	return nil, nil
}

func (s *inspection) conflict(name string, directory bool) bool {
	for existing, isDir := range s.targets {
		if path.Dir(existing) == path.Dir(name) && TargetNamesConflict(path.Base(existing), path.Base(name)) && (existing != name || !isDir || !directory) {
			return true
		}
	}
	s.targets[name] = directory
	return false
}

func (s *inspection) file(target, source string, before fs.FileInfo) (*Rejection, error) {
	if len(s.manifest.Files) >= MaxFiles {
		return &Rejection{Reason: "too-many-files", Path: target}, nil
	}
	f, err := s.root.Open(source)
	if err != nil {
		return nil, err
	}
	opened, err := f.Stat()
	if err != nil {
		return nil, errors.Join(err, f.Close())
	}
	if !opened.Mode().IsRegular() {
		return &Rejection{Reason: "special-file", Path: target}, f.Close()
	}
	if !s.root.SameFile(before, opened) {
		return nil, errors.Join(errors.New("projection source changed while opening"), f.Close())
	}
	b, readErr := io.ReadAll(io.LimitReader(contextReader{ctx: s.ctx, reader: f}, MaxBytes-s.manifest.Bytes+1))
	if err := errors.Join(readErr, s.ctx.Err(), f.Close()); err != nil {
		return nil, err
	}
	if int64(len(b)) > MaxBytes-s.manifest.Bytes {
		return &Rejection{Reason: "too-many-bytes", Path: target}, nil
	}
	s.manifest.Files = append(s.manifest.Files, File{Path: target, Source: source, Size: int64(len(b)), SHA256: sha256.Sum256(b)})
	s.manifest.Bytes += int64(len(b))
	if strings.EqualFold(path.Ext(target), ".md") && externalReference(b) {
		s.manifest.Warnings = append(s.manifest.Warnings, target+": possible external reference")
	}
	return nil, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

var urlPattern = regexp.MustCompile(`https?://[^\s<>"\x60]+`)
var absolutePattern = regexp.MustCompile(`(^|[\s("'\x60=])(/[^/\s]|[A-Za-z]:[\\/]|\\\\)`)

func externalReference(body []byte) bool {
	text := urlPattern.ReplaceAllString(string(body), "")
	return strings.Contains(text, "../") || strings.Contains(text, "${CLAUDE_PLUGIN_ROOT}") || strings.Contains(text, "${CODEX_PLUGIN_ROOT}") || absolutePattern.MatchString(text)
}
