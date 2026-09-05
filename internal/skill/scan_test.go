package skill_test

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"testing"
	"testing/fstest"
	"time"

	"github.com/scarb/skope/internal/host"
	"github.com/scarb/skope/internal/skill"
)

func TestScannerFindsClaudeGlobalSkillsAndCommands(t *testing.T) {
	t.Parallel()

	files := fstest.MapFS{
		"home/me/.claude/skills/foo/SKILL.md":    file("# foo\n"),
		"home/me/.claude/commands/review.md":     file("review\n"),
		"home/me/.claude/commands/git/commit.md": file("commit\n"),
		"custom/skills/bar/SKILL.md":             file("# bar\n"),
	}
	tests := []struct {
		name string
		vars map[string]string
		want []string
	}{
		{name: "default", want: []string{"foo", "git:commit", "review"}},
		{name: "override", vars: map[string]string{"CLAUDE_CONFIG_DIR": "  /custom  "}, want: []string{"bar"}},
		{name: "blank override", vars: map[string]string{"CLAUDE_CONFIG_DIR": " \t "}, want: []string{"foo", "git:commit", "review"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := (skill.Scanner{FS: newMapFS(files)}).ScanClaude(host.NewEnv("/home/me", "/work", tt.vars))
			if err != nil {
				t.Fatalf("ScanClaude() error = %v", err)
			}
			if got := skillIDs(result.Skills); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ScanClaude() IDs = %v, want %v", got, tt.want)
			}
			for _, candidate := range result.Skills {
				for _, location := range candidate.Locations {
					if location.Level != skill.LevelGlobal || location.Source != skill.SourceClaude {
						t.Errorf("location %q level/source = %q/%q", location.DiscoveryPath, location.Level, location.Source)
					}
					if got := location.Names; !reflect.DeepEqual(got, map[skill.Agent]string{skill.AgentClaude: candidate.ID}) {
						t.Errorf("location %q Names = %#v, want Claude name %q only", location.DiscoveryPath, got, candidate.ID)
					}
				}
			}
		})
	}
}

func TestScannerFindsEveryProjectScopeFromNearestGitRoot(t *testing.T) {
	t.Parallel()

	files := fstest.MapFS{
		"repo/.git":                                    dir(),
		"repo/.claude/skills/root/SKILL.md":            file("# root\n"),
		"repo/apps/.claude/skills/middle/SKILL.md":     file("# middle\n"),
		"repo/apps/web/.claude/skills/local/SKILL.md":  file("# local\n"),
		"repo/apps/web/.claude/commands/git/commit.md": file("# commit\n"),
	}
	result, err := (skill.Scanner{FS: newMapFS(files)}).ScanClaude(host.NewEnv("/home/me", "/repo/apps/web", nil))
	if err != nil {
		t.Fatalf("ScanClaude() error = %v", err)
	}
	if result.ProjectRoot != "/repo" {
		t.Fatalf("ProjectRoot = %q, want /repo", result.ProjectRoot)
	}
	if got, want := skillIDs(result.Skills), []string{"apps/web:git:commit", "apps/web:local", "apps:middle", "root"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ScanClaude() IDs = %v, want %v", got, want)
	}
	wantScopes := map[string]string{"root": "", "apps:middle": "apps", "apps/web:local": "apps/web", "apps/web:git:commit": "apps/web"}
	for _, candidate := range result.Skills {
		location := candidate.Locations[0]
		if location.Scope != wantScopes[candidate.ID] {
			t.Errorf("skill %q Scope = %q, want %q", candidate.ID, location.Scope, wantScopes[candidate.ID])
		}
		if location.Names[skill.AgentClaude] != candidate.ID {
			t.Errorf("skill %q Claude name = %q", candidate.ID, location.Names[skill.AgentClaude])
		}
	}
}

func TestScannerPreservesRepeatedCommandDirectoryNames(t *testing.T) {
	t.Parallel()
	files := fstest.MapFS{
		"repo/.git": dir(),
		"repo/apps/web/.claude/commands/review.md":               file("root review"),
		"repo/apps/web/.claude/commands/team/commands/review.md": file("nested review"),
	}
	result, err := (skill.Scanner{FS: newMapFS(files)}).ScanClaude(host.NewEnv("/home/me", "/repo/apps/web", nil))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"apps/web:review", "apps/web:team:commands:review"}
	if got := skillIDs(result.Skills); !reflect.DeepEqual(got, want) {
		t.Fatalf("IDs = %v, want %v", got, want)
	}
	resolved := skill.ResolveNative(skill.AgentClaude, want[:1], result.Skills)
	if got := resolved.AllowedNames(); !reflect.DeepEqual(got, want[:1]) {
		t.Fatalf("allowed names = %v, want only %v", got, want[:1])
	}
}

func TestScannerUsesOnlyCwdWithoutGitRoot(t *testing.T) {
	t.Parallel()

	files := fstest.MapFS{
		"work/parent/.claude/skills/ignored/SKILL.md":      file("ignored\n"),
		"work/parent/project/.claude/skills/only/SKILL.md": file("only\n"),
	}
	result, err := (skill.Scanner{FS: newMapFS(files)}).ScanClaude(host.NewEnv("/home/me", "/work/parent/project", nil))
	if err != nil {
		t.Fatalf("ScanClaude() error = %v", err)
	}
	if result.ProjectRoot != "/work/parent/project" {
		t.Fatalf("ProjectRoot = %q, want cwd", result.ProjectRoot)
	}
	if got, want := skillIDs(result.Skills), []string{"only"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ScanClaude() IDs = %v, want %v", got, want)
	}
}

func TestScannerStopsAtNearestNestedGitRoot(t *testing.T) {
	t.Parallel()

	files := fstest.MapFS{
		"repo/.git":                                     dir(),
		"repo/.claude/skills/outer/SKILL.md":            file("outer\n"),
		"repo/nested/.git":                              dir(),
		"repo/nested/.claude/skills/inner/SKILL.md":     file("inner\n"),
		"repo/nested/pkg/.claude/skills/local/SKILL.md": file("local\n"),
	}
	result, err := (skill.Scanner{FS: newMapFS(files)}).ScanClaude(host.NewEnv("/home/me", "/repo/nested/pkg", nil))
	if err != nil {
		t.Fatalf("ScanClaude() error = %v", err)
	}
	if result.ProjectRoot != "/repo/nested" {
		t.Fatalf("ProjectRoot = %q, want nearest nested root", result.ProjectRoot)
	}
	if got, want := skillIDs(result.Skills), []string{"inner", "pkg:local"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ScanClaude() IDs = %v, want %v", got, want)
	}
}

func TestScannerPreservesWindowsAbsoluteRoots(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows path semantics")
	}

	tests := []struct {
		name string
		cwd  string
		git  string
		want string
	}{
		{name: "volume root", cwd: "C:/", git: "C:/.git", want: "C:/"},
		{name: "volume directory", cwd: "C:/repo", git: "C:/repo/.git", want: "C:/repo"},
		{name: "UNC", cwd: "//server/share/repo", git: "UNC/server/share/repo/.git", want: "//server/share/repo"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mapped := newMapFS(fstest.MapFS{tt.git: dir()})
			result, err := (skill.Scanner{FS: mapped}).ScanClaude(host.NewEnv(tt.cwd, tt.cwd, nil))
			if err != nil {
				t.Fatalf("ScanClaude() error = %v", err)
			}
			if result.ProjectRoot != tt.want {
				t.Fatalf("ProjectRoot = %q, want %q", result.ProjectRoot, tt.want)
			}
			for _, called := range append(slices.Clone(mapped.statCalls), mapped.readDirCalls...) {
				if tt.name == "UNC" {
					if !strings.HasPrefix(called, "//server/share/") {
						t.Errorf("filesystem call lost UNC prefix: %q", called)
					}
				} else if !filepath.IsAbs(filepath.FromSlash(called)) {
					t.Errorf("filesystem call is relative: %q", called)
				}
			}
		})
	}
}

func TestScannerAppliesSkillAndCommandTraversalRules(t *testing.T) {
	t.Parallel()

	files := fstest.MapFS{
		"repo/.git":                                     dir(),
		"repo/.claude/skills/direct/SKILL.md":           file("direct\n"),
		"repo/.claude/skills/not-skill/nested/SKILL.md": file("nested\n"),
		"repo/.claude/skills/plain.txt":                 file("ignored\n"),
		"repo/.claude/commands/a.md":                    file("a\n"),
		"repo/.claude/commands/linked.md":               symlink("../../elsewhere/linked.md"),
		"repo/.claude/commands/manual.md":               symlink("../../elsewhere/manual-dir"),
		"repo/.claude/commands/z/deep.md":               file("deep\n"),
		"repo/.claude/commands/.git/hidden.md":          file("hidden\n"),
		"repo/.claude/commands/node_modules/hidden.md":  file("hidden\n"),
		"repo/.claude/commands/linked":                  symlink("elsewhere/commands"),
		"repo/.claude/commands/linked/hidden.md":        file("hidden\n"),
		"repo/elsewhere/linked.md":                      file("linked\n"),
		"repo/elsewhere/manual-dir":                     dir(),
	}
	mapped := newMapFS(files)
	mapped.reverseReadDir = true
	mapped.realPaths["/repo/.claude/commands/linked.md"] = "/real/linked.md"

	result, err := (skill.Scanner{FS: mapped}).ScanClaude(host.NewEnv("/home/me", "/repo", nil))
	if err != nil {
		t.Fatalf("ScanClaude() error = %v", err)
	}
	if got, want := skillIDs(result.Skills), []string{"a", "direct", "linked", "z:deep"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ScanClaude() IDs = %v, want %v", got, want)
	}
	if got, want := mapped.evalCalls, []string{
		"/repo/.claude/skills/direct/SKILL.md",
		"/repo/.claude/commands/a.md",
		"/repo/.claude/commands/linked.md",
		"/repo/.claude/commands/z/deep.md",
	}; !reflect.DeepEqual(got, want) {
		t.Errorf("EvalSymlinks calls = %v, want deterministic depth-first %v", got, want)
	}
}

func TestScannerSkipsMissingRootsAndFailsClosedAfterEnteringRoot(t *testing.T) {
	t.Parallel()

	files := fstest.MapFS{
		"repo/.git":                     dir(),
		"repo/.claude/commands/blocked": dir(),
	}
	mapped := newMapFS(files)
	blocked := "/repo/.claude/commands/blocked"
	mapped.errors["readDir:"+blocked] = fs.ErrPermission

	_, err := (skill.Scanner{FS: mapped}).ScanClaude(host.NewEnv("/missing-home", "/repo", nil))
	if err == nil {
		t.Fatal("ScanClaude() error = nil, want permission error")
	}
	if !errors.Is(err, fs.ErrPermission) || !strings.Contains(err.Error(), blocked) {
		t.Fatalf("ScanClaude() error = %v, want permission and path %q", err, blocked)
	}
}

func TestScannerFailsClosedWhenGitRootLookupFails(t *testing.T) {
	t.Parallel()

	mapped := newMapFS(nil)
	mapped.errors["stat:/repo/work/.git"] = fs.ErrPermission
	_, err := (skill.Scanner{FS: mapped}).ScanClaude(host.NewEnv("/home/me", "/repo/work", nil))
	if err == nil || !errors.Is(err, fs.ErrPermission) || !strings.Contains(err.Error(), "/repo/work/.git") {
		t.Fatalf("ScanClaude() error = %v, want git stat permission error with path", err)
	}
}

func TestScannerAllowsSkillEntrySymlinkAndRejectsBrokenEntry(t *testing.T) {
	t.Parallel()

	t.Run("valid", func(t *testing.T) {
		files := fstest.MapFS{
			"repo/.git":                           dir(),
			"repo/.claude/skills/linked":          symlink("shared/linked"),
			"repo/.claude/skills/linked/SKILL.md": file("linked\n"),
		}
		mapped := newMapFS(files)
		discovery := "/repo/.claude/skills/linked/SKILL.md"
		mapped.realPaths[discovery] = "/shared/linked/SKILL.md"
		result, err := (skill.Scanner{FS: mapped}).ScanClaude(host.NewEnv("/home/me", "/repo", nil))
		if err != nil {
			t.Fatalf("ScanClaude() error = %v", err)
		}
		location := result.Skills[0].Locations[0]
		if location.DiscoveryPath != discovery || location.RealPath != "/shared/linked/SKILL.md" {
			t.Fatalf("location paths = %q/%q", location.DiscoveryPath, location.RealPath)
		}
	})

	t.Run("broken", func(t *testing.T) {
		files := fstest.MapFS{
			"repo/.git":                  dir(),
			"repo/.claude/skills/broken": symlink("missing"),
		}
		discovery := "/repo/.claude/skills/broken/SKILL.md"
		_, err := (skill.Scanner{FS: newMapFS(files)}).ScanClaude(host.NewEnv("/home/me", "/repo", nil))
		if err == nil || !errors.Is(err, fs.ErrNotExist) || !strings.Contains(err.Error(), discovery) {
			t.Fatalf("ScanClaude() error = %v, want broken symlink path", err)
		}
	})
}

func TestScannerRejectsBrokenSkillFileSymlink(t *testing.T) {
	t.Parallel()

	discovery := "/repo/.claude/skills/broken-file/SKILL.md"
	files := fstest.MapFS{
		"repo/.git": dir(),
		"repo/.claude/skills/broken-file/SKILL.md": symlink("missing.md"),
	}
	_, err := (skill.Scanner{FS: newMapFS(files)}).ScanClaude(host.NewEnv("/home/me", "/repo", nil))
	if err == nil || !errors.Is(err, fs.ErrNotExist) || !strings.Contains(err.Error(), discovery) {
		t.Fatalf("ScanClaude() error = %v, want broken SKILL.md symlink path", err)
	}
}

func TestScannerRejectsBrokenCommandSymlink(t *testing.T) {
	t.Parallel()

	discovery := "/repo/.claude/commands/broken.md"
	files := fstest.MapFS{
		"repo/.git":                       dir(),
		"repo/.claude/commands/broken.md": symlink("missing.md"),
	}
	_, err := (skill.Scanner{FS: newMapFS(files)}).ScanClaude(host.NewEnv("/home/me", "/repo", nil))
	if err == nil || !errors.Is(err, fs.ErrNotExist) || !strings.Contains(err.Error(), discovery) {
		t.Fatalf("ScanClaude() error = %v, want broken command symlink path", err)
	}
}

func TestScannerDoesNotDuplicateOverlappingGlobalAndProjectRoots(t *testing.T) {
	t.Parallel()

	files := fstest.MapFS{
		"repo/.git":                         dir(),
		"repo/.claude/skills/once/SKILL.md": file("once\n"),
	}
	env := host.NewEnv("/home/me", "/repo", map[string]string{"CLAUDE_CONFIG_DIR": "/repo/.claude"})
	result, err := (skill.Scanner{FS: newMapFS(files)}).ScanClaude(env)
	if err != nil {
		t.Fatalf("ScanClaude() error = %v", err)
	}
	if len(result.Skills) != 1 || len(result.Skills[0].Locations) != 1 {
		t.Fatalf("ScanClaude() returned %#v, want one location", result.Skills)
	}
	if result.Skills[0].Locations[0].Level != skill.LevelProject {
		t.Fatalf("overlapping root level = %q, want project", result.Skills[0].Locations[0].Level)
	}
}

func TestScannerParsesSkillFrontmatter(t *testing.T) {
	t.Parallel()

	files := fstest.MapFS{
		"repo/.git":                                   dir(),
		"repo/.claude/skills/named/SKILL.md":          file("---\nname: public-name\n---\nbody\n"),
		"repo/.claude/skills/no-frontmatter/SKILL.md": file("# body\n"),
		"repo/.claude/skills/no-name/SKILL.md":        file("---\ndescription: no name\n---\nbody\n"),
		"repo/.claude/skills/non-mapping/SKILL.md":    file("---\n- one\n- two\n---\nbody\n"),
	}
	result, err := (skill.Scanner{FS: newMapFS(files)}).ScanClaude(host.NewEnv("/home/me", "/repo", nil))
	if err != nil {
		t.Fatalf("ScanClaude() error = %v", err)
	}
	want := map[string]string{"named": "public-name", "no-frontmatter": "", "no-name": "", "non-mapping": ""}
	for _, candidate := range result.Skills {
		location := candidate.Locations[0]
		if location.FrontmatterName != want[candidate.ID] {
			t.Errorf("skill %q FrontmatterName = %q, want %q", candidate.ID, location.FrontmatterName, want[candidate.ID])
		}
		if got := location.Names; !reflect.DeepEqual(got, map[skill.Agent]string{skill.AgentClaude: candidate.ID}) {
			t.Errorf("skill %q Names = %#v", candidate.ID, got)
		}
	}
	if got := countCollisions(result.Collisions, skill.CollisionFrontmatterName); got != 1 {
		t.Errorf("ScanClaude() frontmatter-name collisions = %d, want 1", got)
	}
}

func TestScannerRejectsMalformedSkillFrontmatter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
	}{
		{name: "invalid yaml", content: "---\nname: [\n---\n"},
		{name: "missing closing delimiter", content: "---\nname: public-name\n"},
		{name: "duplicate name", content: "---\nname: first\nname: second\n---\n"},
		{name: "duplicate unknown field", content: "---\nextra: one\nextra: two\nname: valid\n---\n"},
		{name: "nested duplicate unknown field", content: "---\ndescription:\n  item: one\n  item: two\nname: valid\n---\n"},
		{name: "name is not a string", content: "---\nname:\n  nested: value\n---\n"},
		{name: "name is bool", content: "---\nname: true\n---\n"},
		{name: "name is int", content: "---\nname: 42\n---\n"},
		{name: "name is null", content: "---\nname: null\n---\n"},
		{name: "name is sequence", content: "---\nname: [one, two]\n---\n"},
		{name: "name is binary", content: "---\nname: !!binary Zm9v\n---\n"},
		{name: "name has custom tag", content: "---\nname: !custom foo\n---\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			discovery := "/repo/.claude/skills/bad/SKILL.md"
			files := fstest.MapFS{
				"repo/.git":                        dir(),
				"repo/.claude/skills/bad/SKILL.md": file(tt.content),
			}
			_, err := (skill.Scanner{FS: newMapFS(files)}).ScanClaude(host.NewEnv("/home/me", "/repo", nil))
			if err == nil || !strings.Contains(err.Error(), discovery) {
				t.Fatalf("ScanClaude() error = %v, want malformed frontmatter path %q", err, discovery)
			}
		})
	}
}

func TestScannerResolvesMergedFrontmatterName(t *testing.T) {
	t.Parallel()

	files := fstest.MapFS{
		"repo/.git":                           dir(),
		"repo/.claude/skills/merged/SKILL.md": file("---\ndefaults: &defaults\n  name: merged-name\n<<: *defaults\n---\n"),
	}
	result, err := (skill.Scanner{FS: newMapFS(files)}).ScanClaude(host.NewEnv("/home/me", "/repo", nil))
	if err != nil {
		t.Fatalf("ScanClaude() error = %v", err)
	}
	if got := result.Skills[0].Locations[0].FrontmatterName; got != "merged-name" {
		t.Fatalf("FrontmatterName = %q, want merged-name", got)
	}
}

func TestScannerKeepsDiscoveryAliasesWithSameRealPath(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repo := filepath.Join(root, "repo")
	shared := filepath.Join(root, "shared")
	mustMkdirAll(t, filepath.Join(home, ".claude", "skills"))
	mustMkdirAll(t, filepath.Join(repo, ".claude", "skills"))
	mustMkdirAll(t, filepath.Join(repo, ".git"))
	mustMkdirAll(t, shared)
	mustWriteFile(t, filepath.Join(shared, "SKILL.md"), "shared\n")
	mustSymlink(t, shared, filepath.Join(home, ".claude", "skills", "shared"))
	mustSymlink(t, shared, filepath.Join(repo, ".claude", "skills", "shared"))

	result, err := (skill.Scanner{FS: host.OSFileSystem{}}).ScanClaude(host.NewEnv(filepath.ToSlash(home), filepath.ToSlash(repo), nil))
	if err != nil {
		t.Fatalf("ScanClaude() error = %v", err)
	}
	if len(result.Skills) != 1 || len(result.Skills[0].Locations) != 2 {
		t.Fatalf("ScanClaude() returned %#v, want one skill with two locations", result.Skills)
	}
	locations := result.Skills[0].Locations
	if locations[0].DiscoveryPath == locations[1].DiscoveryPath || locations[0].RealPath != locations[1].RealPath {
		t.Fatalf("locations = %#v, want distinct discovery paths and equal real paths", locations)
	}
}

func TestScannerResolvesSkillFileSymlinkToTargetFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	target := filepath.Join(root, "target.md")
	skillDir := filepath.Join(repo, ".claude", "skills", "linked-file")
	mustMkdirAll(t, filepath.Join(repo, ".git"))
	mustMkdirAll(t, skillDir)
	mustWriteFile(t, target, "target\n")
	mustSymlink(t, target, filepath.Join(skillDir, "SKILL.md"))

	result, err := (skill.Scanner{FS: host.OSFileSystem{}}).ScanClaude(host.NewEnv(filepath.ToSlash(filepath.Join(root, "home")), filepath.ToSlash(repo), nil))
	if err != nil {
		t.Fatalf("ScanClaude() error = %v", err)
	}
	location := result.Skills[0].Locations[0]
	targetRealPath, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", target, err)
	}
	if got, want := location.RealPath, filepath.ToSlash(targetRealPath); got != want {
		t.Fatalf("RealPath = %q, want target file %q", got, want)
	}
}

type mapFileSystem struct {
	files          fstest.MapFS
	errors         map[string]error
	realPaths      map[string]string
	evalCalls      []string
	statCalls      []string
	readDirCalls   []string
	reverseReadDir bool
}

func newMapFS(files fstest.MapFS) *mapFileSystem {
	if files == nil {
		files = fstest.MapFS{}
	}
	return &mapFileSystem{files: files, errors: make(map[string]error), realPaths: make(map[string]string)}
}

func (m *mapFileSystem) ReadFile(name string) ([]byte, error) {
	if err := m.operationError("readFile", name); err != nil {
		return nil, err
	}
	if mapped, ok := m.files[mapKey(name)]; ok && mapped.Mode&fs.ModeSymlink != 0 {
		return fs.ReadFile(m.files, mapKey(symlinkTarget(name, string(mapped.Data))))
	}
	if mapped, ok := m.files[mapKey(name)]; ok && !mapped.Mode.IsDir() {
		return slices.Clone(mapped.Data), nil
	}
	return fs.ReadFile(m.files, mapKey(name))
}

func (m *mapFileSystem) ReadDir(name string) ([]fs.DirEntry, error) {
	name = slashClean(name)
	m.readDirCalls = append(m.readDirCalls, name)
	if err := m.operationError("readDir", name); err != nil {
		return nil, err
	}
	entries, err := fs.ReadDir(m.files, mapKey(name))
	if err == nil && m.reverseReadDir {
		slices.Reverse(entries)
	}
	return entries, err
}

func (m *mapFileSystem) Stat(name string) (fs.FileInfo, error) {
	name = slashClean(name)
	m.statCalls = append(m.statCalls, name)
	if err := m.operationError("stat", name); err != nil {
		return nil, err
	}
	if mapped, ok := m.files[mapKey(name)]; ok && mapped.Mode&fs.ModeSymlink != 0 {
		return fs.Stat(m.files, mapKey(symlinkTarget(name, string(mapped.Data))))
	}
	return fs.Stat(m.files, mapKey(name))
}

func (m *mapFileSystem) Lstat(name string) (fs.FileInfo, error) {
	if err := m.operationError("lstat", name); err != nil {
		return nil, err
	}
	if mapped, ok := m.files[mapKey(name)]; ok {
		return mapFileInfo{name: path.Base(slashClean(name)), size: int64(len(mapped.Data)), mode: mapped.Mode}, nil
	}
	return fs.Stat(m.files, mapKey(name))
}

func (m *mapFileSystem) EvalSymlinks(name string) (string, error) {
	name = slashClean(name)
	m.evalCalls = append(m.evalCalls, name)
	if err := m.operationError("evalSymlinks", name); err != nil {
		return "", err
	}
	if realPath, ok := m.realPaths[name]; ok {
		return realPath, nil
	}
	return name, nil
}

func (m *mapFileSystem) operationError(operation, name string) error {
	return m.errors[operation+":"+slashClean(name)]
}

func mapKey(name string) string {
	name = slashClean(name)
	if strings.HasPrefix(name, "//") {
		return "UNC/" + strings.TrimPrefix(name, "//")
	}
	return strings.TrimPrefix(name, "/")
}

func slashClean(name string) string {
	return filepath.ToSlash(filepath.Clean(filepath.FromSlash(name)))
}

func symlinkTarget(name, target string) string {
	if strings.HasPrefix(target, "/") {
		return slashClean(target)
	}
	return path.Join(path.Dir(slashClean(name)), target)
}

type mapFileInfo struct {
	name string
	size int64
	mode fs.FileMode
}

func (i mapFileInfo) Name() string       { return i.name }
func (i mapFileInfo) Size() int64        { return i.size }
func (i mapFileInfo) Mode() fs.FileMode  { return i.mode }
func (i mapFileInfo) ModTime() time.Time { return time.Time{} }
func (i mapFileInfo) IsDir() bool        { return i.mode.IsDir() }
func (i mapFileInfo) Sys() any           { return nil }

func file(contents string) *fstest.MapFile {
	return &fstest.MapFile{Data: []byte(contents), Mode: 0o644}
}

func dir() *fstest.MapFile {
	return &fstest.MapFile{Mode: fs.ModeDir | 0o755}
}

func symlink(target string) *fstest.MapFile {
	return &fstest.MapFile{Data: []byte(target), Mode: fs.ModeSymlink | 0o777}
}

func mustMkdirAll(t *testing.T, name string) {
	t.Helper()
	if err := os.MkdirAll(name, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", name, err)
	}
}

func mustWriteFile(t *testing.T, name, contents string) {
	t.Helper()
	if err := os.WriteFile(name, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", name, err)
	}
}

func mustSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		if runtime.GOOS == "windows" && errors.Is(err, syscall.Errno(1314)) {
			t.Skipf("create symlink without Windows privilege: %v", err)
		}
		t.Fatalf("Symlink(%q, %q): %v", target, link, err)
	}
}

func TestScannerExplicitRootsCarryNamesAndPluginMetadata(t *testing.T) {
	t.Parallel()
	fsys := newMapFS(fstest.MapFS{"plugins/skills/review/SKILL.md": file("# review"), "shared/skills/check/SKILL.md": file("# check"), "commands/run.md": file("run")})
	roots := []skill.Root{
		{Path: "/plugins/skills", Kind: skill.KindSkill, Level: skill.LevelPlugin, Source: skill.SourceClaude, VisibleTo: []skill.Agent{skill.AgentClaude}, PluginID: "p@m", PluginAgent: skill.AgentClaude, NamePrefix: "namespace"},
		{Path: "/shared/skills", Kind: skill.KindSkill, Level: skill.LevelGlobal, Source: skill.SourceAgents, Scope: "app", VisibleTo: []skill.Agent{skill.AgentClaude, skill.AgentCodex, skill.AgentOpenCode}},
		{Path: "/commands", Kind: skill.KindCommand, Level: skill.LevelProject, Source: skill.SourceClaude, VisibleTo: []skill.Agent{skill.AgentClaude, skill.AgentCodex, skill.AgentOpenCode}},
	}
	got, err := (skill.Scanner{FS: fsys}).ScanRoots(roots)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Skills) != 3 {
		t.Fatalf("skills=%#v", got.Skills)
	}
	byID := map[string]skill.Location{}
	for _, s := range got.Skills {
		byID[s.ID] = s.Locations[0]
	}
	plugin := byID["review"]
	if plugin.PluginID != "p@m" || plugin.PluginAgent != skill.AgentClaude || plugin.Names[skill.AgentClaude] != "namespace:review" || plugin.Level != skill.LevelPlugin {
		t.Fatalf("plugin=%#v", plugin)
	}
	shared := byID["app:check"]
	if shared.Source != skill.SourceAgents || !reflect.DeepEqual(shared.Names, map[skill.Agent]string{skill.AgentClaude: "app:check", skill.AgentCodex: "check", skill.AgentOpenCode: "check"}) {
		t.Fatalf("shared=%#v", shared)
	}
	if !reflect.DeepEqual(byID["run"].Names, map[skill.Agent]string{skill.AgentClaude: "run"}) {
		t.Fatalf("command=%#v", byID["run"])
	}
}
func TestScannerExplicitForeignRootKeepsManifestCandidate(t *testing.T) {
	t.Parallel()
	fsys := newMapFS(fstest.MapFS{"skills/review/.claude-plugin/plugin.json": file(`{"name":"p"}`), "skills/review/SKILL.md": file("# review")})
	got, err := (skill.Scanner{FS: fsys}).ScanRoots([]skill.Root{{Path: "/skills", Kind: skill.KindSkill, Level: skill.LevelGlobal, Source: skill.SourceAgents, VisibleTo: []skill.Agent{skill.AgentCodex}}})
	if err != nil || len(got.Skills) != 1 {
		t.Fatalf("skills=%#v error=%v", got, err)
	}
}

func TestScannerExplicitRootsUseEachAgentsEffectiveSkillName(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ name, contents, want string }{
		{name: "frontmatter", contents: "---\nname: effective-name\n---\n# skill", want: "effective-name"},
		{name: "missing frontmatter", contents: "# skill", want: "directory-name"},
		{name: "missing name", contents: "---\ndescription: example\n---\n# skill", want: "directory-name"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fsys := newMapFS(fstest.MapFS{"skills/directory-name/SKILL.md": file(test.contents)})
			got, err := (skill.Scanner{FS: fsys}).ScanRoots([]skill.Root{{Path: "/skills", Kind: skill.KindSkill, Level: skill.LevelProject, Source: skill.SourceAgents, Scope: "app", VisibleTo: []skill.Agent{skill.AgentClaude, skill.AgentCodex, skill.AgentOpenCode}}})
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Skills) != 1 || got.Skills[0].ID != "app:directory-name" {
				t.Fatalf("skills=%#v", got.Skills)
			}
			want := map[skill.Agent]string{skill.AgentClaude: "app:directory-name", skill.AgentCodex: test.want, skill.AgentOpenCode: test.want}
			if !reflect.DeepEqual(got.Skills[0].Locations[0].Names, want) {
				t.Fatalf("names=%#v, want %#v", got.Skills[0].Locations[0].Names, want)
			}
		})
	}
}
