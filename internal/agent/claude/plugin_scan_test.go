package claude_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/scarb/skope/internal/agent"
	"github.com/scarb/skope/internal/agent/claude"
	"github.com/scarb/skope/internal/host"
	"github.com/scarb/skope/internal/proc"
	"github.com/scarb/skope/internal/skill"
)

type pluginFixture struct {
	root, config, repo string
	env                host.Env
}

func newPluginFixture(t *testing.T) pluginFixture {
	t.Helper()
	root := filepath.ToSlash(t.TempDir())
	f := pluginFixture{root: root, config: root + "/config", repo: root + "/repo"}
	f.env = host.NewEnv(root+"/home", f.repo, map[string]string{"CLAUDE_CONFIG_DIR": f.config})
	return f
}
func pluginWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}
func jsonText(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
func pluginRow(id, scope, root, project string) map[string]any {
	row := map[string]any{"id": id, "scope": scope, "enabled": false, "installPath": root}
	if project != "" {
		row["projectPath"] = project
	}
	return row
}
func (f pluginFixture) inventory(t *testing.T, rows []map[string]any, allowed ...string) (agent.Inventory, error) {
	t.Helper()
	fsys := host.OSFileSystem{}
	a := claude.New(skill.Scanner{FS: fsys}, fsys, &probeStub{stdout: jsonText(t, rows)}, claude.Options{Executable: "fixture", Plugins: allowed})
	return a.Inventory(context.Background(), f.env)
}
func (f pluginFixture) cacheMarket(t *testing.T, source string) {
	t.Helper()
	pluginWrite(t, f.config+"/plugins/known_marketplaces.json", jsonText(t, map[string]any{"market": map[string]any{"source": map[string]string{"source": source}, "installLocation": f.root + "/market"}}))
}
func makePlugin(t *testing.T, root, name, skillName string) {
	t.Helper()
	pluginWrite(t, root+"/.claude-plugin/plugin.json", jsonText(t, map[string]string{"name": name}))
	pluginWrite(t, root+"/skills/"+skillName+"/SKILL.md", "# skill")
}

func TestPluginCacheSelectsFirstApplicableRowInOriginalOrder(t *testing.T) {
	for _, test := range []struct {
		name, firstScope, firstProject string
		reverse                        bool
		want                           string
	}{
		{name: "user before project", firstScope: "user", want: "first"},
		{name: "project before user", firstScope: "project", firstProject: "repo", want: "first"},
		{name: "nested project", firstScope: "local", firstProject: "", want: "first"},
		{name: "similar prefix", firstScope: "project", firstProject: "re", want: "second"},
		{name: "other project", firstScope: "local", firstProject: "other", want: "second"},
		{name: "reversed", firstScope: "user", reverse: true, want: "second"},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newPluginFixture(t)
			f.cacheMarket(t, "git")
			makePlugin(t, f.root+"/first", "first", "review")
			makePlugin(t, f.root+"/second", "second", "review")
			project := ""
			if test.firstScope != "user" {
				project = f.root + "/" + test.firstProject
			}
			rows := []map[string]any{pluginRow("p@market", test.firstScope, f.root+"/first", project), pluginRow("p@market", "user", f.root+"/second", "")}
			if test.reverse {
				rows[0], rows[1] = rows[1], rows[0]
			}
			inv, err := f.inventory(t, rows)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(inv.PluginIDs, []string{"p@market"}) || len(inv.Skills) != 1 || len(inv.Skills[0].Locations) != 1 || inv.Skills[0].Locations[0].Names[skill.AgentClaude] != test.want+":review" {
				t.Fatalf("inventory=%#v", inv)
			}
		})
	}
}
func TestPluginOtherProjectKeepsInstalledIDWithoutScanning(t *testing.T) {
	f := newPluginFixture(t)
	inv, err := f.inventory(t, []map[string]any{pluginRow("p@market", "project", f.root+"/missing", f.root+"/other")}, "p@market")
	if err != nil || !reflect.DeepEqual(inv.PluginIDs, []string{"p@market"}) || len(inv.Skills) != 0 || len(inv.Warnings) != 0 {
		t.Fatalf("inventory=%#v err=%v", inv, err)
	}
}
func TestPluginDirectoryMarketUsesLiveSourceAndIgnoresUninstalledCatalogEntries(t *testing.T) {
	f := newPluginFixture(t)
	f.cacheMarket(t, "directory")
	pluginWrite(t, f.root+"/market/.claude-plugin/marketplace.json", `{"plugins":[{"name":"p","source":"./live"},{"name":"uninstalled","source":"./unused"}]}`)
	makePlugin(t, f.root+"/market/live", "namespace", "new-skill")
	makePlugin(t, f.root+"/cache", "old", "old-skill")
	inv, err := f.inventory(t, []map[string]any{pluginRow("p@market", "user", f.root+"/cache", "")})
	if err != nil {
		t.Fatal(err)
	}
	if len(inv.Skills) != 1 || inv.Skills[0].ID != "new-skill" || !reflect.DeepEqual(inv.PluginIDs, []string{"p@market"}) {
		t.Fatalf("inventory=%#v", inv)
	}
	location := inv.Skills[0].Locations[0]
	if location.Names[skill.AgentClaude] != "namespace:new-skill" || location.PluginID != "p@market" || location.PluginAgent != skill.AgentClaude || location.Level != skill.LevelPlugin || location.Source != skill.SourceClaude || location.Scope != "" {
		t.Fatalf("location=%#v", location)
	}
}
func TestPluginAutoEntriesDoNotDuplicateNativeSkills(t *testing.T) {
	f := newPluginFixture(t)
	personal := f.config + "/skills/personal"
	project := f.repo + "/.claude/skills/project"
	makePlugin(t, personal, "personal", "review")
	makePlugin(t, project, "project", "review")
	pluginWrite(t, personal+"/SKILL.md", "# not ordinary")
	pluginWrite(t, project+"/SKILL.md", "# not ordinary")
	// An ancestor auto plugin omitted by the CLI must not be discovered as a plugin.
	pluginWrite(t, f.repo+"/.git/config", "")
	makePlugin(t, f.repo+"/.claude/skills/omitted", "omitted", "ignored")
	inv, err := f.inventory(t, []map[string]any{pluginRow("personal@skills-dir", "user", personal, ""), pluginRow("project@skills-dir", "project", project, "")})
	if err != nil {
		t.Fatal(err)
	}
	if len(inv.Skills) != 1 || inv.Skills[0].ID != "review" || len(inv.Skills[0].Locations) != 2 || len(inv.Collisions) != 1 {
		t.Fatalf("inventory=%#v", inv)
	}
}
func TestPluginSuppressedFailsWithTypedStaticErrorAndNoScan(t *testing.T) {
	raw, err := os.ReadFile("testdata/plugin-list.suppressed.json")
	if err != nil {
		t.Fatal(err)
	}
	scanner := &recordingScanner{}
	a := claude.New(scanner, missingFS{}, &probeStub{stdout: string(raw)}, claude.Options{Executable: "fixture"})
	inv, err := a.Inventory(context.Background(), host.NewEnv("/home", "/repo", nil))
	var incomplete *claude.IncompletePluginInventoryError
	if !errors.As(err, &incomplete) || !strings.Contains(err.Error(), "trust") || strings.Contains(err.Error(), "reload-plugins") || scanner.rootCalls != 0 || len(inv.PluginIDs) != 0 {
		t.Fatalf("inventory=%#v error=%v calls=%d", inv, err, scanner.rootCalls)
	}
}
func TestPluginProbeFailuresStopBeforePluginScan(t *testing.T) {
	for _, failure := range []error{errors.New("exit failure"), &proc.TimeoutError{}, &proc.OutputLimitError{}} {
		t.Run(failure.Error(), func(t *testing.T) {
			scanner := &recordingScanner{}
			a := claude.New(scanner, missingFS{}, &probeStub{stdout: "[]", err: failure}, claude.Options{Executable: "fixture"})
			inv, err := a.Inventory(context.Background(), host.NewEnv("/home", "/repo", nil))
			if !errors.Is(err, failure) || scanner.rootCalls != 0 || len(inv.PluginIDs) != 0 {
				t.Fatalf("error=%v scan=%d", err, scanner.rootCalls)
			}
		})
	}
}

type recordingScanner struct{ rootCalls int }

func (*recordingScanner) ScanClaude(host.Env) (skill.ScanResult, error) {
	return skill.ScanResult{ProjectRoot: "/repo"}, nil
}
func (s *recordingScanner) ScanRoots([]skill.Root) (skill.ScanResult, error) {
	s.rootCalls++
	return skill.ScanResult{}, nil
}

func TestPluginRejectsUnsafeDirectorySourcesAndBrokenMetadata(t *testing.T) {
	for _, source := range []any{"../escape", "./safe/../../escape", "/absolute", map[string]string{"source": "github"}, nil} {
		t.Run(jsonText(t, source), func(t *testing.T) {
			f := newPluginFixture(t)
			f.cacheMarket(t, "directory")
			pluginWrite(t, f.root+"/market/.claude-plugin/marketplace.json", jsonText(t, map[string]any{"plugins": []any{map[string]any{"name": "p", "source": source}}}))
			_, err := f.inventory(t, []map[string]any{pluginRow("p@market", "user", f.root+"/cache", "")})
			if err == nil || !strings.Contains(err.Error(), "source") {
				t.Fatalf("error=%v", err)
			}
		})
	}
	for _, part := range []string{"missing known", "unknown source", "missing catalog", "missing manifest", "invalid manifest"} {
		t.Run(part, func(t *testing.T) {
			f := newPluginFixture(t)
			if part != "missing known" {
				f.cacheMarket(t, "git")
			}
			if part == "unknown source" {
				f.cacheMarket(t, "unsupported")
			}
			if part == "missing catalog" {
				f.cacheMarket(t, "directory")
			}
			if part == "invalid manifest" {
				pluginWrite(t, f.root+"/cache/.claude-plugin/plugin.json", `{"name":null}`)
			}
			_, err := f.inventory(t, []map[string]any{pluginRow("p@market", "user", f.root+"/cache", "")})
			if err == nil {
				t.Fatal("accepted missing metadata")
			}
		})
	}
}

func TestPluginRealSchemaFixtureUsesPlatformAbsoluteRoots(t *testing.T) {
	f := newPluginFixture(t)
	read := func(name string) string {
		data, err := os.ReadFile("testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return strings.ReplaceAll(string(data), "/fixture", f.root)
	}
	pluginWrite(t, f.config+"/plugins/known_marketplaces.json", read("plugin-marketplaces.valid.json"))
	pluginWrite(t, f.root+"/market/.claude-plugin/marketplace.json", read("plugin-marketplace-directory.valid.json"))
	for _, name := range []string{"allowed-plugin", "blocked-plugin", "uninstalled-plugin"} {
		makePlugin(t, f.root+"/market/"+name, name, name)
	}
	makePlugin(t, f.root+"/claude/plugins/cache/skope-git-fixture/cache-plugin/1.0.0", "cache-plugin", "cache-plugin")
	makePlugin(t, f.root+"/claude/skills/personal-auto", "personal-auto", "personal-auto")
	makePlugin(t, f.root+"/repo/.claude/skills/project-auto", "project-auto", "project-auto")
	fsys := host.OSFileSystem{}
	a := claude.New(skill.Scanner{FS: fsys}, fsys, &probeStub{stdout: read("plugin-list.valid.json")}, claude.Options{Executable: "fixture"})
	inv, err := a.Inventory(context.Background(), f.env)
	if err != nil {
		t.Fatal(err)
	}
	if len(inv.PluginIDs) != 6 || len(inv.Skills) != 6 {
		t.Fatalf("inventory=%#v", inv)
	}
}

func TestPluginSuppressedRequiresStringNotes(t *testing.T) {
	a := claude.New(&recordingScanner{}, missingFS{}, &probeStub{stdout: `[{"id":"(suppressed)@skills-dir","scope":"project","enabled":false,"installPath":"","version":"unknown","notes":[null]}]`}, claude.Options{Executable: "fixture"})
	_, err := a.Inventory(context.Background(), host.NewEnv("/home", "/repo", nil))
	var incomplete *claude.IncompletePluginInventoryError
	if err == nil || errors.As(err, &incomplete) {
		t.Fatalf("error=%v, want malformed item, not trust placeholder", err)
	}
}
func TestPluginInvalidItemReportsIndexAndField(t *testing.T) {
	for _, field := range []string{"id", "enabled", "scope", "installPath", "projectPath"} {
		t.Run(field, func(t *testing.T) {
			f := newPluginFixture(t)
			row := pluginRow("p@market", "project", f.root+"/cache", f.repo)
			delete(row, field)
			_, err := f.inventory(t, []map[string]any{row})
			if err == nil || !strings.Contains(err.Error(), "item 0") || !strings.Contains(err.Error(), field) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestPluginMalformedRecordReportsItemWithoutPayload(t *testing.T) {
	a := claude.New(&recordingScanner{}, missingFS{}, &probeStub{stdout: `["secret-sentinel"]`}, claude.Options{Executable: "fixture"})
	_, err := a.Inventory(context.Background(), host.NewEnv("/home", "/repo", nil))
	if err == nil || !strings.Contains(err.Error(), "item 0") || strings.Contains(err.Error(), "secret-sentinel") {
		t.Fatalf("error=%v", err)
	}
}

func TestPluginDirectoryOtherProjectKeepsSnapshotWithoutPredictingInstallation(t *testing.T) {
	f := newPluginFixture(t)
	f.cacheMarket(t, "directory")
	pluginWrite(t, f.root+"/market/.claude-plugin/marketplace.json", `{"plugins":[{"name":"p","source":"./live"}]}`)
	makePlugin(t, f.root+"/market/live", "p", "review")
	inv, err := f.inventory(t, []map[string]any{pluginRow("p@market", "project", f.root+"/cache", f.root+"/other")}, "p@market")
	if err != nil || len(inv.Skills) != 0 || !reflect.DeepEqual(inv.PluginIDs, []string{"p@market"}) || len(inv.Warnings) != 0 {
		t.Fatalf("inventory=%#v error=%v", inv, err)
	}
}
