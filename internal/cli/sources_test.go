package cli_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/scarb/skope/internal/agent/codex"
	"github.com/scarb/skope/internal/cli"
	"github.com/scarb/skope/internal/host"
	"github.com/scarb/skope/internal/launch"
	"github.com/scarb/skope/internal/proc"
	"github.com/scarb/skope/internal/skill"
)

type sourceReadFunc func(context.Context, host.Env, host.CodexPaths) (codex.CatalogSnapshot, error)

func (f sourceReadFunc) Read(c context.Context, e host.Env, p host.CodexPaths) (codex.CatalogSnapshot, error) {
	return f(c, e, p)
}

func sourceFixture(t *testing.T) (host.Env, host.CodexPaths, skill.Scanner) {
	t.Helper()
	home := t.TempDir()
	repo := filepath.Join(home, "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	paths := host.CodexPaths{Home: home, CodexHome: filepath.Join(home, ".codex"), AdminSkillRoots: []string{filepath.Join(home, "admin", "skills")}, SystemConfigPaths: []string{filepath.Join(home, "admin", "config.toml")}}
	return host.NewEnv(home, repo, nil), paths, skill.Scanner{FS: host.OSFileSystem{}, RegularFiles: host.OSFileSystem{}}
}
func TestForeignSourcesSelectTargetAndPreserveMetadata(t *testing.T) {
	env, paths, scanner := sourceFixture(t)
	phaseTwoWrite(t, filepath.Join(env.Home(), ".claude", "skills", "claude-only", "SKILL.md"), skillDocument("claude-only"))
	phaseTwoWrite(t, filepath.Join(env.Home(), ".claude", "commands", "ns", "run.md"), "command")
	phaseTwoWrite(t, filepath.Join(env.Cwd(), ".agents", "skills", "project", "SKILL.md"), "---\nname: project\ndescription: project skill\n---\n")
	phaseTwoWrite(t, filepath.Join(paths.AdminSkillRoots[0], "admin", "SKILL.md"), "---\nname: admin\ndescription: admin skill\n---\n")
	phaseTwoWrite(t, filepath.Join(paths.CodexHome, "skills", ".system", "hidden", "SKILL.md"), skillDocument("hidden"))
	pluginRoot := filepath.Join(env.Home(), "plugin")
	phaseTwoWrite(t, filepath.Join(pluginRoot, "entry", "SKILL.md"), "---\nname: entry\ndescription: plugin skill\n---\n")
	catalogCalls, resolveCalls := 0, 0
	warnings := []string{"catalog warning"}
	composer := cli.NewForeignScanner(scanner, sourceReadFunc(func(_ context.Context, _ host.Env, p host.CodexPaths) (codex.CatalogSnapshot, error) {
		catalogCalls++
		if !reflect.DeepEqual(p, paths) {
			t.Fatalf("paths=%v", p)
		}
		return codex.CatalogSnapshot{Warnings: warnings, SkillRoots: []skill.Root{{Path: pluginRoot, Kind: skill.KindSkill, Level: skill.LevelPlugin, Source: skill.SourceCodex, PluginAgent: skill.AgentCodex, PluginID: "p@m", NamePrefix: "p", VisibleTo: []skill.Agent{skill.AgentCodex}, ScanMode: skill.CodexRecursive}}}, nil
	}), func(host.Env) (host.CodexPaths, error) { resolveCalls++; return paths, nil })
	if catalogCalls != 0 || resolveCalls != 0 {
		t.Fatal("constructor performed I/O")
	}
	got, err := composer.ScanForeign(context.Background(), env, skill.AgentCodex, 1024)
	if err != nil || catalogCalls != 0 || resolveCalls != 0 {
		t.Fatalf("got=%+v err=%v calls=%d/%d", got, err, catalogCalls, resolveCalls)
	}
	if len(got.Skills) != 2 || got.Skills[0].ID != "claude-only" || got.Skills[1].ID != "ns:run" {
		t.Fatalf("claude sources=%+v", got.Skills)
	}
	got, err = composer.ScanForeign(context.Background(), env, skill.AgentClaude, 1024)
	if err != nil || catalogCalls != 1 || resolveCalls != 1 {
		t.Fatalf("got=%+v err=%v calls=%d/%d", got, err, catalogCalls, resolveCalls)
	}
	if len(got.Skills) != 3 || !reflect.DeepEqual(got.Warnings, warnings) {
		t.Fatalf("codex sources=%+v", got)
	}
	var admin, project, plugin bool
	for _, s := range got.Skills {
		l := s.Locations[0]
		switch s.ID {
		case "admin":
			admin = l.Level == skill.LevelAdmin
		case "project":
			project = l.Level == skill.LevelProject && l.Scope == ""
		case "entry":
			plugin = l.PluginAgent == skill.AgentCodex && l.PluginID == "p@m" && l.Names[skill.AgentCodex] == "p:entry"
		default:
			t.Fatalf("unexpected skill=%+v", s)
		}
	}
	if !admin || !project || !plugin {
		t.Fatalf("metadata=%+v", got.Skills)
	}
	got.Warnings[0] = "mutated"
	if warnings[0] != "catalog warning" {
		t.Fatal("warnings alias catalog")
	}
}
func TestForeignSourcesFailClosedBeforeScanAndRespectCancellation(t *testing.T) {
	for _, boundary := range []string{"resolve error", "resolve cancellation", "catalog error", "catalog cancellation", "already canceled", "unsupported"} {
		t.Run(boundary, func(t *testing.T) {
			env, paths, scanner := sourceFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			failure := errors.New("source unavailable")
			calls := 0
			composer := cli.NewForeignScanner(scanner, sourceReadFunc(func(context.Context, host.Env, host.CodexPaths) (codex.CatalogSnapshot, error) {
				calls++
				if boundary == "catalog cancellation" {
					cancel()
					return codex.CatalogSnapshot{}, nil
				}
				return codex.CatalogSnapshot{}, failure
			}), func(host.Env) (host.CodexPaths, error) {
				calls++
				if boundary == "resolve error" {
					return paths, failure
				}
				if boundary == "resolve cancellation" {
					cancel()
				}
				return paths, nil
			})
			target := skill.AgentClaude
			if boundary == "already canceled" {
				cancel()
			}
			if boundary == "unsupported" {
				target = skill.AgentOpenCode
			}
			_, err := composer.ScanForeign(ctx, env, target, 1024)
			if err == nil {
				t.Fatal("accepted failed source")
			}
			if strings.Contains(boundary, "cancel") && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancel error=%v", err)
			}
			want := 2
			if boundary == "resolve error" || boundary == "resolve cancellation" {
				want = 1
			}
			if boundary == "already canceled" || boundary == "unsupported" {
				want = 0
			}
			if calls != want {
				t.Fatalf("calls=%d want=%d", calls, want)
			}
		})
	}
}
func TestForeignSourcesRejectionsContinueAndIOErrorsStop(t *testing.T) {
	env, paths, scanner := sourceFixture(t)
	phaseTwoWrite(t, filepath.Join(paths.CodexHome, "skills", "large", "SKILL.md"), "xxxxxxxxxxxxxxxx")
	phaseTwoWrite(t, filepath.Join(paths.CodexHome, "skills", "good", "SKILL.md"), "good")
	composer := cli.NewForeignScanner(scanner, codex.NewCatalog(scanner.FS, scanner.RegularFiles, scanner), func(host.Env) (host.CodexPaths, error) { return paths, nil })
	got, err := composer.ScanForeign(context.Background(), env, skill.AgentClaude, 8)
	if err != nil || len(got.Rejections) != 1 || len(got.Skills) != 2 {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	phaseTwoWrite(t, paths.SystemConfigPaths[0], "[broken")
	_, err = composer.ScanForeign(context.Background(), env, skill.AgentClaude, 8)
	if err == nil {
		t.Fatal("accepted invalid config")
	}
}

type sourceProbe struct{}

func (sourceProbe) Run(context.Context, proc.Request) (proc.Result, error) {
	return proc.Result{Stdout: []byte("[]")}, nil
}
func TestForeignSourcesClaudeServiceProjectsScopesAndExcludesPlugins(t *testing.T) {
	env, paths, scanner := sourceFixture(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	f := &integrationFixture{root: env.Home(), home: env.Home(), repo: filepath.Join(env.Cwd(), "sub"), skopeHome: filepath.Join(env.Home(), "skope"), claudeConfig: filepath.Join(env.Home(), "claude")}
	for _, dir := range []string{f.repo, f.skopeHome, f.claudeConfig} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	f.writeConfig(t, executable)
	writeFile(t, filepath.Join(f.skopeHome, "skillsets.toml"), "version=1\n[skillsets.dev]\nskills=['sub:project','admin','entry']\n")
	phaseTwoWrite(t, filepath.Join(f.repo, ".agents", "skills", "project", "SKILL.md"), "project body")
	phaseTwoWrite(t, filepath.Join(paths.AdminSkillRoots[0], "admin", "SKILL.md"), "admin body")
	pluginRoot := filepath.Join(env.Home(), "plugin")
	phaseTwoWrite(t, filepath.Join(pluginRoot, "entry", "SKILL.md"), "plugin body")
	service, handoff := phaseTwoService(t, f, sourceProbe{})
	service.Foreign = cli.NewForeignScanner(scanner, sourceReadFunc(func(context.Context, host.Env, host.CodexPaths) (codex.CatalogSnapshot, error) {
		return codex.CatalogSnapshot{PluginIDs: []string{"codex@m"}, Warnings: []string{"foreign warning"}, SkillRoots: []skill.Root{{Path: pluginRoot, Kind: skill.KindSkill, Source: skill.SourceCodex, Level: skill.LevelPlugin, PluginAgent: skill.AgentCodex, PluginID: "codex@m", NamePrefix: "p", ScanMode: skill.CodexRecursive, VisibleTo: []skill.Agent{skill.AgentCodex}}}}, nil
	}), func(host.Env) (host.CodexPaths, error) { return paths, nil })
	var got launch.Result
	err = service.Run(context.Background(), launch.Request{Agent: skill.AgentClaude, SetPresent: true, SetValue: "dev"}, func(r launch.Result) error { got = r; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if !handoff.called || len(got.ProjectionFiles) != 2 || len(got.Inventory.PluginIDs) != 0 || !reflect.DeepEqual(got.Inventory.Warnings, []string{"foreign warning"}) {
		t.Fatalf("result=%+v handoff=%v", got, handoff.called)
	}
	for i, id := range []string{"sub:project", "admin"} {
		if got.Resolved.Entries[i].ID != id || got.Resolved.Entries[i].State != skill.StateProjected {
			t.Fatalf("resolved=%+v", got.Resolved)
		}
	}
	if got.Resolved.Entries[0].Location.Scope != "sub" || got.Resolved.Entries[2].Reason != skill.ReasonPluginOnly {
		t.Fatalf("resolved=%+v", got.Resolved)
	}
	for _, file := range got.ProjectionFiles {
		if _, err := os.Stat(file.Path); err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range got.Plan.Files {
		if strings.Contains(string(file.Data), "codex@m") {
			t.Fatalf("foreign plugin leaked into Claude settings: %s", file.Data)
		}
	}
}

type sourceFailFS struct {
	skill.FileSystem
	path   string
	err    error
	cancel context.CancelFunc
	reads  int
}

func (f *sourceFailFS) ReadDir(path string) ([]fs.DirEntry, error) {
	f.reads++
	if f.cancel != nil {
		f.cancel()
	}
	if filepath.Clean(path) == filepath.Clean(f.path) {
		return nil, f.err
	}
	return f.FileSystem.ReadDir(path)
}
func TestForeignSourcesWalkerErrorsAndCancellation(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(map[bool]string{false: "io error", true: "cancel after walker"}[cancelled], func(t *testing.T) {
			env, paths, scanner := sourceFixture(t)
			failure := errors.New("read denied")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			wrapper := &sourceFailFS{FileSystem: scanner.FS, path: filepath.Join(paths.Home, ".agents", "skills"), err: failure}
			if cancelled {
				wrapper.path = ""
				wrapper.cancel = cancel
				failure = context.Canceled
			}
			scanner.FS = wrapper
			composer := cli.NewForeignScanner(scanner, sourceReadFunc(func(context.Context, host.Env, host.CodexPaths) (codex.CatalogSnapshot, error) {
				return codex.CatalogSnapshot{}, nil
			}), func(host.Env) (host.CodexPaths, error) { return paths, nil })
			_, err := composer.ScanForeign(ctx, env, skill.AgentClaude, 1024)
			if !errors.Is(err, failure) || wrapper.reads == 0 {
				t.Fatalf("err=%v reads=%d", err, wrapper.reads)
			}
		})
	}
}

type sourceSpecialInfo struct{ fs.FileInfo }

func (sourceSpecialInfo) Mode() fs.FileMode { return fs.ModeNamedPipe | 0600 }

type sourceSpecialFS struct {
	skill.FileSystem
	path string
}

func (f sourceSpecialFS) Stat(path string) (fs.FileInfo, error) {
	i, err := f.FileSystem.Stat(path)
	if err == nil && filepath.Clean(path) == filepath.Clean(f.path) {
		return sourceSpecialInfo{i}, nil
	}
	return i, err
}
func TestForeignSourcesSpecialFileContinuesWithoutReadingBody(t *testing.T) {
	env, paths, scanner := sourceFixture(t)
	special := filepath.Join(paths.CodexHome, "skills", "special", "SKILL.md")
	phaseTwoWrite(t, special, "---\nname: [INVALID BODY MUST NOT BE READ\n---\n")
	phaseTwoWrite(t, filepath.Join(paths.CodexHome, "skills", "good", "SKILL.md"), "good")
	scanner.FS = sourceSpecialFS{FileSystem: scanner.FS, path: special}
	composer := cli.NewForeignScanner(scanner, sourceReadFunc(func(context.Context, host.Env, host.CodexPaths) (codex.CatalogSnapshot, error) {
		return codex.CatalogSnapshot{}, nil
	}), func(host.Env) (host.CodexPaths, error) { return paths, nil })
	got, err := composer.ScanForeign(context.Background(), env, skill.AgentClaude, 1024)
	if err != nil || len(got.Skills) != 2 || len(got.Rejections) != 1 || got.Rejections[0].Reason != skill.ReasonSpecialFile {
		t.Fatalf("result=%+v err=%v", got, err)
	}
}
