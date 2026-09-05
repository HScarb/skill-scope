package claude_test

import (
	"context"
	"errors"
	"io/fs"
	"reflect"
	"strings"
	"testing"

	"github.com/scarb/skope/internal/agent"
	"github.com/scarb/skope/internal/agent/claude"
	"github.com/scarb/skope/internal/host"
	"github.com/scarb/skope/internal/skill"
)

func TestInventoryAdapterMetadata(t *testing.T) {
	t.Parallel()

	adapter := claude.Adapter{}
	if got := adapter.Name(); got != skill.AgentClaude {
		t.Fatalf("Name() = %q, want %q", got, skill.AgentClaude)
	}
	want := agent.Capabilities{Projection: true, TogglePlugins: true, ToggleBundled: true}
	if got := adapter.Capabilities(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Capabilities() = %#v, want %#v", got, want)
	}
}

func TestInventoryScansOnceAndDeepCopiesScannerResults(t *testing.T) {
	t.Parallel()

	fixture := skill.ScanResult{
		ProjectRoot: "/repo",
		Skills: []skill.Skill{{
			ID: "review",
			Locations: []skill.Location{{
				Kind:            skill.KindSkill,
				DiscoveryPath:   "/repo/.claude/skills/review/SKILL.md",
				RealPath:        "/real/review/SKILL.md",
				Level:           skill.LevelProject,
				Source:          skill.SourceClaude,
				Scope:           "",
				FrontmatterName: "review-tool",
				Names:           map[skill.Agent]string{skill.AgentClaude: "review"},
				PluginID:        "review@market",
				PluginAgent:     skill.AgentClaude,
			}},
		}},
	}
	fixture.Skills, fixture.Collisions = skill.Build(fixture.Skills[0].Locations)

	scanner := &fakeScanner{result: fixture}
	adapter := newTestAdapter(scanner, missingFS{})

	got, err := adapter.Inventory(context.Background(), host.NewEnv("/home/me", "/repo", nil))
	if err != nil {
		t.Fatalf("Inventory() error = %v", err)
	}
	if scanner.calls != 1 {
		t.Fatalf("ScanClaude() calls = %d, want 1", scanner.calls)
	}
	if got.SkillNames == nil || got.PluginIDs == nil || got.Warnings == nil {
		t.Fatalf("empty inventory slices = %#v, want independently allocated empty slices", got)
	}
	if !reflect.DeepEqual(got.Skills, fixture.Skills) || !reflect.DeepEqual(got.Collisions, fixture.Collisions) {
		t.Fatalf("Inventory() scanner data = %#v/%#v, want %#v/%#v", got.Skills, got.Collisions, fixture.Skills, fixture.Collisions)
	}

	got.Skills[0].ID = "changed"
	got.Skills[0].Locations[0].DiscoveryPath = "/changed"
	got.Skills[0].Locations[0].Names[skill.AgentClaude] = "changed"
	got.Collisions[0].IDs[0] = "changed"
	got.Collisions[0].Paths[0] = "/changed"
	if fixture.Skills[0].ID != "review" ||
		fixture.Skills[0].Locations[0].DiscoveryPath != "/repo/.claude/skills/review/SKILL.md" ||
		fixture.Skills[0].Locations[0].Names[skill.AgentClaude] != "review" ||
		fixture.Collisions[0].IDs[0] != "review" ||
		fixture.Collisions[0].Paths[0] != "/repo/.claude/skills/review/SKILL.md" {
		t.Fatalf("Inventory() returned scanner-owned storage: %#v", fixture)
	}
}

func TestInventoryCollectsSortedSkillOverrideNamesFromSettingsLayers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		vars      map[string]string
		userPath  string
		wantReads []string
	}{
		{
			name:     "default user settings",
			userPath: "/home/me/.claude/settings.json",
			wantReads: []string{
				"/home/me/.claude/settings.json",
				"/repo/.claude/settings.json",
				"/repo/.claude/settings.local.json",
			},
		},
		{
			name:     "blank config directory",
			vars:     map[string]string{"CLAUDE_CONFIG_DIR": " \t "},
			userPath: "/home/me/.claude/settings.json",
			wantReads: []string{
				"/home/me/.claude/settings.json",
				"/repo/.claude/settings.json",
				"/repo/.claude/settings.local.json",
			},
		},
		{
			name:     "trimmed config directory",
			vars:     map[string]string{"CLAUDE_CONFIG_DIR": "  /custom/claude  "},
			userPath: "/custom/claude/settings.json",
			wantReads: []string{
				"/custom/claude/settings.json",
				"/repo/.claude/settings.json",
				"/repo/.claude/settings.local.json",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fileSystem := &mapReadFileFS{files: map[string]string{
				tt.userPath:                         `{"skillOverrides":{"z":"off","shared":"disabled"},"unknown":true}`,
				"/repo/.claude/settings.json":       `{"skillOverrides":{"a":"on","shared":"enabled"}}`,
				"/repo/.claude/settings.local.json": `{"skillOverrides":{"middle":"anything"}}`,
			}}
			adapter := newTestAdapter(&fakeScanner{result: skill.ScanResult{ProjectRoot: "/repo"}}, fileSystem)

			got, err := adapter.Inventory(context.Background(), host.NewEnv("/home/me", "/repo/work", tt.vars))
			if err != nil {
				t.Fatalf("Inventory() error = %v", err)
			}
			wantNames := []string{"a", "middle", "shared", "z"}
			if !reflect.DeepEqual(got.SkillNames, wantNames) {
				t.Fatalf("SkillNames = %v, want %v", got.SkillNames, wantNames)
			}
			if !reflect.DeepEqual(fileSystem.calls, tt.wantReads) {
				t.Fatalf("ReadFile() calls = %v, want %v", fileSystem.calls, tt.wantReads)
			}
		})
	}
}

func TestInventoryRejectsMalformedSettingsWithPath(t *testing.T) {
	t.Parallel()

	settingsPath := "/home/me/.claude/settings.json"
	tests := []struct {
		name     string
		contents string
	}{
		{name: "invalid JSON", contents: `{"skillOverrides":`},
		{name: "null root", contents: `null`},
		{name: "array root", contents: `[]`},
		{name: "string root", contents: `"settings"`},
		{name: "null overrides", contents: `{"skillOverrides":null}`},
		{name: "array overrides", contents: `{"skillOverrides":[]}`},
		{name: "string overrides", contents: `{"skillOverrides":"all"}`},
		{name: "non-string value", contents: `{"skillOverrides":{"review":true}}`},
		{name: "trailing JSON value", contents: `{"skillOverrides":{}} {}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			adapter := newTestAdapter(&fakeScanner{result: skill.ScanResult{ProjectRoot: "/repo"}}, &mapReadFileFS{files: map[string]string{settingsPath: tt.contents}})
			_, err := adapter.Inventory(context.Background(), host.NewEnv("/home/me", "/repo", nil))
			if err == nil || !strings.Contains(err.Error(), settingsPath) {
				t.Fatalf("Inventory() error = %v, want malformed settings path %q", err, settingsPath)
			}
		})
	}
}

func TestInventoryFailsClosedOnSettingsReadError(t *testing.T) {
	t.Parallel()

	settingsPath := "/repo/.claude/settings.json"
	fileSystem := &mapReadFileFS{errors: map[string]error{settingsPath: fs.ErrPermission}}
	adapter := newTestAdapter(&fakeScanner{result: skill.ScanResult{ProjectRoot: "/repo"}}, fileSystem)

	_, err := adapter.Inventory(context.Background(), host.NewEnv("/home/me", "/repo", nil))
	if err == nil || !errors.Is(err, fs.ErrPermission) || !strings.Contains(err.Error(), settingsPath) {
		t.Fatalf("Inventory() error = %v, want permission error with path %q", err, settingsPath)
	}
}

func TestInventoryChecksContextBeforeScanner(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	scanner := &fakeScanner{}
	fileSystem := &mapReadFileFS{}
	adapter := newTestAdapter(scanner, fileSystem)

	_, err := adapter.Inventory(ctx, host.NewEnv("/home/me", "/repo", nil))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Inventory() error = %v, want context.Canceled", err)
	}
	if scanner.calls != 0 || len(fileSystem.calls) != 0 {
		t.Fatalf("canceled Inventory() scanner/filesystem calls = %d/%v, want none", scanner.calls, fileSystem.calls)
	}
}

func TestInventoryChecksContextAfterScannerAndBeforeEachSettingsRead(t *testing.T) {
	t.Parallel()

	t.Run("after scanner", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		scanner := &fakeScanner{result: skill.ScanResult{ProjectRoot: "/repo"}, onScan: cancel}
		fileSystem := &mapReadFileFS{}
		adapter := newTestAdapter(scanner, fileSystem)

		_, err := adapter.Inventory(ctx, host.NewEnv("/home/me", "/repo", nil))
		if !errors.Is(err, context.Canceled) || len(fileSystem.calls) != 0 {
			t.Fatalf("Inventory() error/calls = %v/%v, want canceled before settings I/O", err, fileSystem.calls)
		}
	})

	t.Run("before next settings read", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		fileSystem := &mapReadFileFS{onRead: func(string) { cancel() }}
		adapter := newTestAdapter(&fakeScanner{result: skill.ScanResult{ProjectRoot: "/repo"}}, fileSystem)

		_, err := adapter.Inventory(ctx, host.NewEnv("/home/me", "/repo", nil))
		wantCalls := []string{"/home/me/.claude/settings.json"}
		if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(fileSystem.calls, wantCalls) {
			t.Fatalf("Inventory() error/calls = %v/%v, want canceled before second read", err, fileSystem.calls)
		}
	})
}

func TestInventoryWrapsScannerErrorWithoutSettingsIO(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("scan failed")
	fileSystem := &mapReadFileFS{}
	adapter := newTestAdapter(&fakeScanner{err: wantErr}, fileSystem)

	_, err := adapter.Inventory(context.Background(), host.NewEnv("/home/me", "/repo", nil))
	if !errors.Is(err, wantErr) || !strings.Contains(err.Error(), "scan Claude") {
		t.Fatalf("Inventory() error = %v, want wrapped scanner error", err)
	}
	if len(fileSystem.calls) != 0 {
		t.Fatalf("ReadFile() calls = %v, want none after scanner error", fileSystem.calls)
	}
}

type fakeScanner struct {
	result skill.ScanResult
	err    error
	calls  int
	onScan func()
}

func (s *fakeScanner) ScanClaude(host.Env) (skill.ScanResult, error) {
	s.calls++
	if s.onScan != nil {
		s.onScan()
	}
	return s.result, s.err
}

type missingFS struct{}

func (missingFS) ReadFile(string) ([]byte, error) {
	return nil, fs.ErrNotExist
}

type mapReadFileFS struct {
	files  map[string]string
	errors map[string]error
	calls  []string
	onRead func(string)
}

func (m *mapReadFileFS) ReadFile(name string) ([]byte, error) {
	m.calls = append(m.calls, name)
	if m.onRead != nil {
		m.onRead(name)
	}
	if err := m.errors[name]; err != nil {
		return nil, err
	}
	contents, ok := m.files[name]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return []byte(contents), nil
}

func (s *fakeScanner) ScanRoots([]skill.Root) (skill.ScanResult, error) {
	return skill.ScanResult{}, nil
}
func newTestAdapter(scanner claude.SkillScanner, fsys claude.ReadFileFS) claude.Adapter {
	return claude.New(scanner, fsys, &probeStub{stdout: "[]"}, claude.Options{Executable: "fixture-claude"})
}
