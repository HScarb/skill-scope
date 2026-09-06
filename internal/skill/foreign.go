package skill

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"slices"
	"strings"

	"github.com/scarb/skope/internal/host"
)

type ScanRejection struct {
	Source        Source
	DiscoveryPath string
	Reason        ResolutionReason
}

func (s Scanner) ScanForeignGlobals(env host.Env, maxBytes int64) (ScanResult, error) {
	if maxBytes <= 0 {
		return ScanResult{}, errors.New("foreign scan requires a positive byte limit")
	}
	if s.RegularFiles == nil {
		return ScanResult{}, errors.New("foreign scan requires a regular file opener")
	}
	var result ScanResult
	var locations []Location
	for _, root := range foreignGlobalRoots(env) {
		entries, err := s.FS.ReadDir(root.Path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return ScanResult{}, fmt.Errorf("read directory %s: %w", root.Path, err)
		}
		slices.SortFunc(entries, func(a, b fs.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
		for _, entry := range entries {
			isSymlink := entry.Type()&fs.ModeSymlink != 0
			if !entry.IsDir() && !isSymlink {
				continue
			}
			name := joinPath(root.Path, entry.Name(), "SKILL.md")
			_, err := s.FS.Lstat(name)
			if errors.Is(err, fs.ErrNotExist) && !isSymlink {
				continue
			}
			if err != nil {
				return ScanResult{}, fmt.Errorf("lstat %s: %w", name, err)
			}
			info, err := s.FS.Stat(name)
			if err != nil {
				return ScanResult{}, fmt.Errorf("stat %s: %w", name, err)
			}
			realPath, err := s.FS.EvalSymlinks(name)
			if err != nil {
				return ScanResult{}, fmt.Errorf("resolve %s: %w", name, err)
			}
			location := Location{Kind: root.Kind, Source: root.Source, Level: root.Level, DiscoveryPath: name, RealPath: cleanPath(realPath)}
			reason := foreignFileRejection(info, maxBytes)
			if reason == "" {
				var contents []byte
				contents, reason, err = s.readForeignFile(name, maxBytes)
				if err != nil {
					return ScanResult{}, err
				}
				if reason == "" {
					frontmatter, err := parseFrontmatterName(contents)
					if err != nil {
						return ScanResult{}, fmt.Errorf("parse frontmatter %s: %w", name, err)
					}
					location.FrontmatterName = frontmatter
					location.Names = rootNames(root, entry.Name(), frontmatter)
				}
			}
			locations = append(locations, location)
			if reason != "" {
				result.Rejections = append(result.Rejections, ScanRejection{Source: root.Source, DiscoveryPath: name, Reason: reason})
			}
		}
	}
	result.Skills, result.Collisions = Build(locations)
	return result, nil
}

func foreignFileRejection(info fs.FileInfo, maxBytes int64) ResolutionReason {
	if !info.Mode().IsRegular() {
		return ReasonSpecialFile
	}
	if info.Size() > maxBytes {
		return ReasonLimitExceeded
	}
	return ""
}

func (s Scanner) readForeignFile(name string, maxBytes int64) (contents []byte, reason ResolutionReason, err error) {
	file, err := s.RegularFiles.OpenRegular(name)
	if err != nil {
		if onlyNotRegular(err) {
			return nil, ReasonSpecialFile, nil
		}
		return nil, "", fmt.Errorf("open %s: %w", name, err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close %s: %w", name, closeErr))
		}
	}()
	info, err := file.Stat()
	if err != nil {
		return nil, "", fmt.Errorf("stat open file %s: %w", name, err)
	}
	if reason := foreignFileRejection(info, maxBytes); reason != "" {
		return nil, reason, nil
	}
	readLimit := maxBytes
	if readLimit < math.MaxInt64 {
		readLimit++
	}
	contents, err = io.ReadAll(io.LimitReader(file, readLimit))
	if err != nil {
		return nil, "", fmt.Errorf("read %s: %w", name, err)
	}
	if int64(len(contents)) > maxBytes {
		return nil, ReasonLimitExceeded, nil
	}
	return contents, "", nil
}

// Preserve I/O failures if an opener joins them with a structural rejection.
func onlyNotRegular(err error) bool {
	if !errors.Is(err, host.ErrNotRegular) {
		return false
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, cause := range joined.Unwrap() {
			if !onlyNotRegular(cause) {
				return false
			}
		}
	} else if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return onlyNotRegular(wrapped.Unwrap())
	}
	return true
}
