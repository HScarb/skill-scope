package codex

import (
	"context"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/scarb/skope/internal/host"
	"github.com/scarb/skope/internal/skill"
)

func (c Catalog) readPlugin(ctx context.Context, paths host.CodexPaths, id string) (*skill.Root, bool, error) {
	name, market, _ := strings.Cut(id, "@")
	cache := filepath.Join(paths.CodexHome, "plugins", "cache", market, name)
	fail := func(path, field string, err error) (*skill.Root, bool, error) {
		return nil, false, inventoryError(path, field, err)
	}
	if err := ctx.Err(); err != nil {
		return fail(cache, "plugins.cache", err)
	}
	_, err := c.optionalStat(ctx, cache)
	if missingOnly(err) {
		return nil, false, nil
	}
	if err != nil {
		return fail(cache, "plugins.cache", err)
	}
	if err := ctx.Err(); err != nil {
		return fail(cache, "plugins.cache", err)
	}
	entries, err := c.fs.ReadDir(cache)
	if err != nil {
		return fail(cache, "plugins.cache", err)
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() && validVersionDirectory(entry.Name()) {
			names = append(names, entry.Name())
		}
	}
	active, err := activeVersion(names)
	if err != nil {
		return fail(cache, "plugins.version", err)
	}
	if active == "" {
		return nil, false, nil
	}
	pluginRoot := filepath.Join(cache, active)
	if err := ctx.Err(); err != nil {
		return fail(pluginRoot, "plugins.manifest", err)
	}
	manifest, err := c.manifests.ReadCodexManifest(pluginRoot)
	if err != nil {
		return fail(filepath.Join(pluginRoot, ".codex-plugin", "plugin.json"), "plugins.manifest", err)
	}
	if err := ctx.Err(); err != nil {
		return fail(pluginRoot, "plugins.manifest", err)
	}
	rootPath := filepath.Join(pluginRoot, filepath.FromSlash(strings.ReplaceAll(manifest.Skills, "\\", "/")))
	// Validate both lexical and resolved containment; injected manifest readers obey the same boundary.
	if !contained(pluginRoot, rootPath) {
		return fail(pluginRoot, "plugins.skills", fs.ErrInvalid)
	}
	if err := ctx.Err(); err != nil {
		return fail(rootPath, "plugins.skills", err)
	}
	_, err = c.optionalStat(ctx, rootPath)
	if missingOnly(err) {
		if cancelErr := ctx.Err(); cancelErr != nil {
			return fail(rootPath, "plugins.skills", cancelErr)
		}
		realPlugin, resolveErr := c.fs.EvalSymlinks(pluginRoot)
		if resolveErr != nil {
			return fail(pluginRoot, "plugins.skills", resolveErr)
		}
		parent := filepath.Dir(rootPath)
		for {
			if cancelErr := ctx.Err(); cancelErr != nil {
				return fail(parent, "plugins.skills", cancelErr)
			}
			_, parentErr := c.fs.Lstat(parent)
			if parentErr == nil {
				break
			}
			if !missingOnly(parentErr) || filepath.Dir(parent) == parent {
				return fail(parent, "plugins.skills", parentErr)
			}
			parent = filepath.Dir(parent)
		}
		if cancelErr := ctx.Err(); cancelErr != nil {
			return fail(parent, "plugins.skills", cancelErr)
		}
		realParent, resolveErr := c.fs.EvalSymlinks(parent)
		if resolveErr != nil {
			return fail(parent, "plugins.skills", resolveErr)
		}
		if !contained(realPlugin, realParent) {
			return fail(rootPath, "plugins.skills", fs.ErrInvalid)
		}
		return nil, true, nil
	}
	if err != nil {
		return fail(rootPath, "plugins.skills", err)
	}
	if err := ctx.Err(); err != nil {
		return fail(rootPath, "plugins.skills", err)
	}
	realPlugin, err := c.fs.EvalSymlinks(pluginRoot)
	if err != nil {
		return fail(pluginRoot, "plugins.skills", err)
	}
	if err := ctx.Err(); err != nil {
		return fail(rootPath, "plugins.skills", err)
	}
	realRoot, err := c.fs.EvalSymlinks(rootPath)
	if err != nil {
		return fail(rootPath, "plugins.skills", err)
	}
	if !contained(realPlugin, realRoot) {
		return fail(rootPath, "plugins.skills", fs.ErrInvalid)
	}
	if err := ctx.Err(); err != nil {
		return fail(rootPath, "plugins.skills", err)
	}
	info, err := c.fs.Stat(rootPath)
	if err != nil {
		return fail(rootPath, "plugins.skills", err)
	}
	if !info.IsDir() {
		return fail(rootPath, "plugins.skills", fs.ErrInvalid)
	}
	return &skill.Root{Path: rootPath, Kind: skill.KindSkill, Level: skill.LevelPlugin, Source: skill.SourceCodex, VisibleTo: []skill.Agent{skill.AgentCodex}, PluginID: id, PluginAgent: skill.AgentCodex, NamePrefix: manifest.Name, ScanMode: skill.CodexRecursive}, true, nil
}
func contained(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	return err == nil && !filepath.IsAbs(rel) && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
