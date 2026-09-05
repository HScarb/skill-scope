package launch

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/scarb/skope/internal/projection"
	"github.com/scarb/skope/internal/session"
	"github.com/scarb/skope/internal/skill"
)

type projectionSink struct {
	sess    *session.Session
	manager SessionManager
	prefix  string
}

func newProjectionSink(sess *session.Session, manager SessionManager, name string) (*projectionSink, error) {
	if !validProjectionDirectoryName(name) {
		return nil, fmt.Errorf("invalid projection directory name %q", name)
	}
	return &projectionSink{sess: sess, manager: manager, prefix: sess.AgentPath("addDir", ".claude", "skills", name)}, nil
}

func validProjectionDirectoryName(name string) bool {
	return name != "." && fs.ValidPath(name) && !strings.ContainsAny(name, `/\:`) && filepath.IsLocal(name)
}

func (s *projectionSink) target(relative string, directory bool) (string, error) {
	if !fs.ValidPath(relative) || strings.ContainsAny(relative, `\:`) || (!directory && relative == ".") || !filepath.IsLocal(relative) {
		return "", fmt.Errorf("invalid projection relative path %q", relative)
	}
	return filepath.Join(s.prefix, filepath.FromSlash(relative)), nil
}

func (s *projectionSink) Mkdir(relative string) error {
	target, err := s.target(relative, true)
	if err != nil {
		return err
	}
	return s.manager.WriteDirectories(s.sess, []string{target})
}

func (s *projectionSink) WriteFile(relative string, data []byte) error {
	target, err := s.target(relative, false)
	if err != nil {
		return err
	}
	return s.manager.WriteNew(s.sess, []session.File{{Path: target, Data: data, Mode: 0o600}})
}

type ProjectionInspector interface {
	Inspect(context.Context, string) (projection.Manifest, *projection.Rejection, error)
}

type ProjectionCopier interface {
	Copy(context.Context, projection.Manifest, projection.Sink) error
}

type preparedSkillProjection struct {
	ID       string
	Name     string // Destination directory basename; Claude also uses it as the effective name.
	Manifest projection.Manifest
}

type preparedProjection struct {
	Resolved skill.Resolved
	Skills   []preparedSkillProjection
}

type projectionLocationKey struct {
	Source        skill.Source
	DiscoveryPath string
}

func prepareProjection(ctx context.Context, target skill.Agent, selected []string, inventory []skill.Skill, opts skill.ResolveOptions, rejections []skill.ScanRejection, inspector ProjectionInspector) (preparedProjection, error) {
	rejected := make(map[projectionLocationKey]skill.ResolutionReason, len(rejections))
	for _, rejection := range rejections {
		rejected[projectionLocationKey{rejection.Source, rejection.DiscoveryPath}] = rejection.Reason
	}
	var occupied []string
	for _, candidate := range inventory {
		for _, loc := range candidate.Locations {
			if loc.Level == skill.LevelPlugin || loc.PluginID != "" || loc.PluginAgent != "" {
				continue
			}
			if name := loc.Names[target]; name != "" {
				occupied = append(occupied, name)
			}
		}
	}
	manifests := make(map[projectionLocationKey]projection.Manifest)
	check := func(loc skill.Location) (skill.ResolutionReason, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		key := projectionLocationKey{loc.Source, loc.DiscoveryPath}
		if reason, ok := rejected[key]; ok {
			return reason, nil
		}
		if inspector == nil {
			return "", errors.New("projection inspector is required")
		}
		directory := filepath.Dir(filepath.FromSlash(loc.DiscoveryPath))
		manifest, rejection, err := inspector.Inspect(ctx, directory)
		if err != nil {
			return "", err
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if rejection != nil {
			return projectionRejectionReason(rejection.Reason)
		}
		name := filepath.Base(directory)
		if !validProjectionDirectoryName(name) {
			return skill.ReasonInvalidPath, nil
		}
		for _, existing := range occupied {
			if projection.TargetNamesConflict(existing, name) {
				return skill.ReasonTargetConflict, nil
			}
		}
		occupied = append(occupied, name)
		manifests[key] = manifest
		return "", nil
	}
	resolved, err := skill.Resolve(target, selected, inventory, opts, check)
	if err != nil {
		return preparedProjection{}, err
	}
	prepared := preparedProjection{Resolved: resolved}
	for _, entry := range resolved.Entries {
		if entry.State != skill.StateProjected {
			continue
		}
		loc := entry.Location
		prepared.Skills = append(prepared.Skills, preparedSkillProjection{
			ID:       entry.ID,
			Name:     filepath.Base(filepath.Dir(filepath.FromSlash(loc.DiscoveryPath))),
			Manifest: manifests[projectionLocationKey{loc.Source, loc.DiscoveryPath}],
		})
	}
	return prepared, nil
}

func projectionFiles(sess *session.Session, prepared preparedProjection) ([]ProjectionFile, error) {
	var files []ProjectionFile
	for _, projected := range prepared.Skills {
		sink, err := newProjectionSink(sess, nil, projected.Name)
		if err != nil {
			return nil, err
		}
		for _, file := range projected.Manifest.Files {
			target, err := sink.target(file.Path, false)
			if err != nil {
				return nil, err
			}
			files = append(files, ProjectionFile{ID: projected.ID, Path: target})
		}
	}
	return files, nil
}

func projectionRejectionReason(reason string) (skill.ResolutionReason, error) {
	switch reason {
	case "outside-root":
		return skill.ReasonOutsideRoot, nil
	case "symlink-loop":
		return skill.ReasonSymlinkLoop, nil
	case "special-file":
		return skill.ReasonSpecialFile, nil
	case "plugin-manifest":
		return skill.ReasonPluginManifest, nil
	case "target-conflict":
		return skill.ReasonTargetConflict, nil
	case "invalid-path":
		return skill.ReasonInvalidPath, nil
	case "too-many-files", "too-many-bytes":
		return skill.ReasonLimitExceeded, nil
	default:
		return "", fmt.Errorf("unknown projection rejection reason %q", reason)
	}
}
