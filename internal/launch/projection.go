package launch

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/scarb/skope/internal/projection"
	"github.com/scarb/skope/internal/skill"
)

type ProjectionInspector interface {
	Inspect(context.Context, string) (projection.Manifest, *projection.Rejection, error)
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
		if rejection != nil {
			return projectionRejectionReason(rejection.Reason)
		}
		name := filepath.Base(directory)
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
