package skill

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"

	"github.com/scarb/skope/internal/host"
)

type ScanRejection struct {
	Source        Source
	DiscoveryPath string
	Reason        ResolutionReason
}

func (s Scanner) ScanForeignGlobals(env host.Env, maxBytes int64) (ScanResult, error) {
	return s.ScanForeignRoots(foreignGlobalRoots(env), maxBytes)
}

func (s Scanner) ScanForeignRoots(roots []Root, maxBytes int64) (ScanResult, error) {
	if maxBytes <= 0 {
		return ScanResult{}, errors.New("foreign scan requires a positive byte limit")
	}
	if s.RegularFiles == nil {
		return ScanResult{}, errors.New("foreign scan requires a regular file opener")
	}
	var result ScanResult
	var locations []Location
	for _, root := range roots {
		err := s.visitRoot(root, func(root Root, name, basename string) error {
			info, err := s.FS.Stat(name)
			if err != nil {
				return fmt.Errorf("stat %s: %w", name, err)
			}
			realPath, err := s.FS.EvalSymlinks(name)
			if err != nil {
				return fmt.Errorf("resolve %s: %w", name, err)
			}
			location := locationFor(root, name, realPath)
			reason := foreignFileRejection(info, maxBytes)
			if reason == "" {
				var contents []byte
				contents, reason, err = s.readForeignFile(name, maxBytes)
				if err != nil {
					return err
				}
				if reason == "" {
					frontmatter := ""
					if root.Kind == KindSkill {
						frontmatter, err = parseFrontmatterName(contents)
						if err != nil {
							return fmt.Errorf("invalid skill metadata %s", name)
						}
					}
					location.FrontmatterName = frontmatter
					location.Names = rootNames(root, basename, frontmatter)
					if !codexDescriptionValid(contents) {
						delete(location.Names, AgentCodex)
					}
				}
			}
			locations = append(locations, location)
			if reason != "" {
				result.Rejections = append(result.Rejections, ScanRejection{Source: root.Source, DiscoveryPath: name, Reason: reason})
			}
			return nil
		})
		if err != nil {
			return ScanResult{}, err
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
