package codex

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// missingOnly does not discard an I/O failure joined to an absence error.
func missingOnly(err error) bool {
	if err == nil {
		return false
	}
	if group, ok := err.(interface{ Unwrap() []error }); ok {
		children := group.Unwrap()
		if len(children) == 0 {
			return false
		}
		for _, child := range children {
			if !missingOnly(child) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return missingOnly(wrapped.Unwrap())
	}
	return errors.Is(err, fs.ErrNotExist)
}
func (c Catalog) readConfig(ctx context.Context, path string) ([]string, error) {
	fail := func(field string, err error) ([]string, error) { return nil, inventoryError(path, field, err) }
	if err := ctx.Err(); err != nil {
		return fail("config", err)
	}
	err := c.optionalStat(ctx, path)
	if missingOnly(err) {
		return nil, nil
	}
	if err != nil {
		return fail("config", err)
	}
	if err := ctx.Err(); err != nil {
		return fail("config", err)
	}
	info, err := c.fs.Stat(path)
	if err != nil {
		return fail("config", err)
	}
	if err := ctx.Err(); err != nil {
		return fail("config", err)
	}
	if !info.Mode().IsRegular() {
		return fail("config", fs.ErrInvalid)
	}
	if err := ctx.Err(); err != nil {
		return fail("config", err)
	}
	file, err := c.opener.OpenRegular(path)
	if err != nil {
		return fail("config", err)
	}
	if err := ctx.Err(); err != nil {
		return fail("config", errors.Join(err, file.Close()))
	}
	data, readErr := io.ReadAll(contextReader{ctx: ctx, reader: file})
	closeErr := file.Close()
	if err := errors.Join(readErr, closeErr, ctx.Err()); err != nil {
		return fail("config", err)
	}
	var raw map[string]any
	if err := toml.Unmarshal(data, &raw); err != nil {
		return fail("config.toml", err)
	}
	if _, ok := raw["profile"]; ok {
		return fail("unsupported-source.profile", fs.ErrInvalid)
	}
	if markers, ok := raw["project_root_markers"]; ok {
		values, ok := markers.([]any)
		if !ok || len(values) != 1 || values[0] != ".git" {
			return fail("unsupported-source.project_root_markers", fs.ErrInvalid)
		}
	}
	if err := validateSkills(raw); err != nil {
		return fail(err.Error(), fs.ErrInvalid)
	}
	ids, err := parsePlugins(raw)
	if err != nil {
		return fail(err.Error(), fs.ErrInvalid)
	}
	return ids, nil
}
func parsePlugins(raw map[string]any) ([]string, error) {
	value, ok := raw["plugins"]
	if !ok {
		return nil, nil
	}
	plugins, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("plugins")
	}
	var ids []string
	for id, value := range plugins {
		if !validPluginID(id) {
			return nil, errors.New("plugins.id")
		}
		entry, ok := value.(map[string]any)
		if !ok {
			return nil, errors.New("plugins.entry")
		}
		if enabled, ok := entry["enabled"]; ok {
			if _, ok := enabled.(bool); !ok {
				return nil, errors.New("plugins.enabled")
			}
		}
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids, nil
}

// validPluginID follows plugin/src/plugin_id.rs in Codex rust-v0.153.1.
func validPluginID(id string) bool {
	parts := strings.Split(id, "@")
	if len(parts) != 2 {
		return false
	}
	for index, part := range parts {
		if part == "" || strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".") || strings.Contains(part, "..") {
			return false
		}
		for i := range len(part) {
			b := part[i]
			allowed := b >= '0' && b <= '9' || b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z' || b == '-' || b == '_' || index == 0 && b == '.'
			if !allowed {
				return false
			}
		}
	}
	return true
}
func validateSkills(raw map[string]any) error {
	value, ok := raw["skills"]
	if !ok {
		return nil
	}
	table, ok := value.(map[string]any)
	if !ok {
		return errors.New("skills")
	}
	if value, ok := table["config"]; ok {
		entries, ok := value.([]any)
		if !ok {
			return errors.New("skills.config")
		}
		for _, value := range entries {
			entry, ok := value.(map[string]any)
			if !ok {
				return errors.New("skills.config")
			}
			for _, field := range []string{"path", "name"} {
				if value, ok := entry[field]; ok {
					if _, ok := value.(string); !ok {
						return errors.New("skills.config." + field)
					}
				}
			}
			if value, ok := entry["enabled"]; ok {
				if _, ok := value.(bool); !ok {
					return errors.New("skills.config.enabled")
				}
			}
		}
	}
	if value, ok := table["bundled"]; ok {
		entry, ok := value.(map[string]any)
		if !ok {
			return errors.New("skills.bundled")
		}
		if value, ok := entry["enabled"]; ok {
			if _, ok := value.(bool); !ok {
				return errors.New("skills.bundled.enabled")
			}
		}
	}
	return nil
}

// optionalStat distinguishes genuinely absent paths from broken ancestor links.
func (c Catalog) optionalStat(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := c.fs.Lstat(name)
	if !missingOnly(err) {
		return err
	}
	for parent := filepath.Dir(name); ; parent = filepath.Dir(parent) {
		if cancelErr := ctx.Err(); cancelErr != nil {
			return cancelErr
		}
		ancestor, ancestorErr := c.fs.Lstat(parent)
		if ancestorErr == nil {
			if ancestor.Mode()&fs.ModeSymlink != 0 {
				if cancelErr := ctx.Err(); cancelErr != nil {
					return cancelErr
				}
				if _, statErr := c.fs.Stat(parent); statErr != nil {
					return errors.Join(fs.ErrInvalid, statErr)
				}
			}
			return err
		}
		if !missingOnly(ancestorErr) {
			return ancestorErr
		}
		if filepath.Dir(parent) == parent {
			return err
		}
	}
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
