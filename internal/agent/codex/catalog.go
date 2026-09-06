// Package codex reads local Codex sources and prepares session controls.
package codex

import (
	"context"
	"io/fs"
	"path/filepath"
	"slices"

	"github.com/scarb/skope/internal/host"
	"github.com/scarb/skope/internal/skill"
)

type CatalogSnapshot struct {
	PluginIDs, InstalledIDs []string
	SkillRoots              []skill.Root
	Warnings                []string
}
type SourceReader interface {
	Read(context.Context, host.Env, host.CodexPaths) (CatalogSnapshot, error)
}
type ManifestReader interface {
	ReadCodexManifest(string) (skill.CodexManifest, error)
}
type Catalog struct {
	fs        skill.FileSystem
	opener    skill.RegularFileOpener
	manifests ManifestReader
}

func NewCatalog(fs skill.FileSystem, opener skill.RegularFileOpener, manifests ManifestReader) Catalog {
	return Catalog{fs: fs, opener: opener, manifests: manifests}
}
func (c Catalog) Read(ctx context.Context, env host.Env, paths host.CodexPaths) (CatalogSnapshot, error) {
	var result CatalogSnapshot
	configs := append(slices.Clone(paths.SystemConfigPaths), filepath.Join(paths.CodexHome, "config.toml"))
	for _, path := range configs {
		ids, err := c.readConfig(ctx, path)
		if err != nil {
			return CatalogSnapshot{}, err
		}
		result.PluginIDs = append(result.PluginIDs, ids...)
	}
	if err := ctx.Err(); err != nil {
		return CatalogSnapshot{}, err
	}
	_, dirs, err := skill.ProjectDirectories(projectFileSystem{FileSystem: c.fs, ctx: ctx}, env.Cwd())
	if err != nil {
		return CatalogSnapshot{}, inventoryError(env.Cwd(), "project", err)
	}
	for _, dir := range dirs {
		ids, err := c.readConfig(ctx, filepath.Join(dir, ".codex", "config.toml"))
		if err != nil {
			return CatalogSnapshot{}, err
		}
		result.PluginIDs = append(result.PluginIDs, ids...)
	}
	slices.Sort(result.PluginIDs)
	result.PluginIDs = slices.Compact(result.PluginIDs)
	for _, id := range result.PluginIDs {
		root, installed, err := c.readPlugin(ctx, paths, id)
		if err != nil {
			return CatalogSnapshot{}, err
		}
		if installed {
			result.InstalledIDs = append(result.InstalledIDs, id)
		}
		if root != nil {
			result.SkillRoots = append(result.SkillRoots, *root)
		}
	}
	if err := ctx.Err(); err != nil {
		return CatalogSnapshot{}, err
	}
	return result, nil
}

// ProjectDirectories owns ancestor traversal; guard each of its Stat calls.
type projectFileSystem struct {
	skill.FileSystem
	ctx context.Context
}

func (f projectFileSystem) Stat(path string) (fs.FileInfo, error) {
	if err := f.ctx.Err(); err != nil {
		return nil, err
	}
	info, err := f.FileSystem.Stat(path)
	if cancelErr := f.ctx.Err(); cancelErr != nil {
		return nil, cancelErr
	}
	return info, err
}
