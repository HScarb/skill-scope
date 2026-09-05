package launch

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/scarb/skope/internal/agent"
	"github.com/scarb/skope/internal/config"
	"github.com/scarb/skope/internal/host"
	"github.com/scarb/skope/internal/session"
	"github.com/scarb/skope/internal/skill"
)

func TestServiceRunActiveSetOrchestratesLaunch(t *testing.T) {
	t.Parallel()

	fixture := newFixture()
	var reported Result
	err := fixture.service.Run(context.Background(), Request{
		Agent:      skill.AgentClaude,
		SetValue:   "dev",
		SetPresent: true,
		AgentArgs:  []string{"--request"},
	}, func(result Result) error {
		fixture.record("report")
		reported = result
		return nil
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	wantEvents := []string{
		"read config.toml", "reap", "lookpath", "read skillsets.toml",
		"conflict", "factory", "registry", "inventory", "stage", "plan", "write", "publish", "report", "handoff",
	}
	if !reflect.DeepEqual(fixture.events, wantEvents) {
		t.Fatalf("events = %#v, want %#v", fixture.events, wantEvents)
	}
	if fixture.resolver.command != "configured-claude" {
		t.Fatalf("LookPath command = %q, want configured-claude", fixture.resolver.command)
	}
	wantArgs := []string{"--config", "--request", "--settings", fixture.settingsPath}
	if !reflect.DeepEqual(fixture.handoff.args, wantArgs) {
		t.Fatalf("handoff args = %#v, want %#v", fixture.handoff.args, wantArgs)
	}
	wantEnv := []string{"ADDED=2", "BASE=1", "CHANGE=new"}
	if !reflect.DeepEqual(fixture.handoff.env, wantEnv) {
		t.Fatalf("handoff env = %#v, want %#v", fixture.handoff.env, wantEnv)
	}
	if fixture.handoff.path != fixture.resolver.path {
		t.Fatalf("handoff path = %q, want %q", fixture.handoff.path, fixture.resolver.path)
	}
	if len(fixture.sessions.files) != 1 || fixture.sessions.files[0].Path != fixture.settingsPath || string(fixture.sessions.files[0].Data) != "settings" || fixture.sessions.files[0].Mode != 0o600 {
		t.Fatalf("written files = %#v", fixture.sessions.files)
	}
	if reported.Executable != fixture.resolver.path || reported.Session == nil || reported.NoIsolation {
		t.Fatalf("reported result = %#v", reported)
	}
	if reported.Session == fixture.sessions.session {
		t.Fatal("reported Session aliases the live session")
	}
	if reported.Session.Root != fixture.sessions.session.Root || reported.Session.Agent != fixture.sessions.session.Agent {
		t.Fatalf("reported Session = %#v, want display fields from %#v", reported.Session, fixture.sessions.session)
	}
	if !reflect.DeepEqual(reported.Args, wantArgs) || !reflect.DeepEqual(reported.Env, wantEnv) {
		t.Fatalf("reported args/env = %#v / %#v", reported.Args, reported.Env)
	}
	if got := reported.Resolved.Entries; len(got) != 2 || got[0].ID != "one" || got[0].State != skill.StateNative || got[1].ID != "missing" || got[1].State != skill.StateMissing {
		t.Fatalf("reported resolved entries = %#v", got)
	}
	if !reflect.DeepEqual(reported.Warnings, fixture.sessions.reapWarnings) {
		t.Fatalf("reported warnings = %#v, want %#v", reported.Warnings, fixture.sessions.reapWarnings)
	}
	if fixture.adapter.planInventory.Skills[0].ID != "one" || fixture.adapter.planResolved.Entries[0].ID != "one" || fixture.adapter.planSession != fixture.sessions.session {
		t.Fatalf("Plan() inputs = %#v, %#v, %p", fixture.adapter.planResolved, fixture.adapter.planInventory, fixture.adapter.planSession)
	}
	if fixture.sessions.stageAgent != skill.AgentClaude || fixture.sessions.stageDisplay != "dev" {
		t.Fatalf("Stage() agent/display = %q/%q", fixture.sessions.stageAgent, fixture.sessions.stageDisplay)
	}
	if fixture.sessions.written != fixture.sessions.session || fixture.sessions.published != fixture.sessions.session {
		t.Fatalf("Write()/Publish() sessions = %p/%p, want %p", fixture.sessions.written, fixture.sessions.published, fixture.sessions.session)
	}
}

func TestServiceRunDefaultsCommandAndDoesNotAliasArgumentInputs(t *testing.T) {
	t.Parallel()

	fixture := newFixture()
	fixture.fsys.files[fixture.configPath] = []byte("version = 1\n[agents.claude]\nargs = [\"--config\"]\n")
	requestBacking := []string{"--request", "sentinel"}
	requestArgs := requestBacking[:1]
	err := fixture.service.Run(context.Background(), Request{
		Agent:      skill.AgentClaude,
		SetValue:   "dev",
		SetPresent: true,
		AgentArgs:  requestArgs,
	}, func(Result) error {
		fixture.record("report")
		return nil
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if fixture.resolver.command != "claude" {
		t.Fatalf("LookPath command = %q, want claude", fixture.resolver.command)
	}
	if requestBacking[1] != "sentinel" {
		t.Fatalf("Run() appended into request backing array: %#v", requestBacking)
	}
}

func TestServiceRunNoneSkipsIsolation(t *testing.T) {
	t.Parallel()

	fixture := newFixture()
	fixture.fsys.errs[fixture.skillSetsPath] = errors.New("must not be read")
	var reported Result
	err := fixture.service.Run(context.Background(), Request{
		Agent:      skill.AgentClaude,
		SetValue:   "none",
		SetPresent: true,
		AgentArgs:  []string{"--request"},
	}, func(result Result) error {
		fixture.record("report")
		reported = result
		return nil
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	wantEvents := []string{"read config.toml", "reap", "lookpath", "report", "handoff"}
	if !reflect.DeepEqual(fixture.events, wantEvents) {
		t.Fatalf("events = %#v, want %#v", fixture.events, wantEvents)
	}
	if !reported.NoIsolation {
		t.Fatal("reported NoIsolation = false, want true")
	}
	if reported.Session != nil || len(reported.Plan.Files) != 0 || len(reported.Inventory.Skills) != 0 {
		t.Fatalf("none result contains isolation state: %#v", reported)
	}
	wantArgs := []string{"--config", "--request"}
	if !reflect.DeepEqual(fixture.handoff.args, wantArgs) {
		t.Fatalf("handoff args = %#v, want %#v", fixture.handoff.args, wantArgs)
	}
	wantEnv := []string{"BASE=1", "CHANGE=old"}
	if !reflect.DeepEqual(fixture.handoff.env, wantEnv) {
		t.Fatalf("handoff env = %#v, want %#v", fixture.handoff.env, wantEnv)
	}
}

func TestServiceRunDryRunUsesPreviewAndStopsAfterReport(t *testing.T) {
	t.Parallel()

	fixture := newFixture()
	err := fixture.service.Run(context.Background(), Request{
		Agent:      skill.AgentClaude,
		SetValue:   "dev",
		SetPresent: true,
		DryRun:     true,
	}, func(Result) error {
		fixture.record("report")
		return nil
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	want := []string{"read config.toml", "reap", "lookpath", "read skillsets.toml", "conflict", "factory", "registry", "inventory", "preview", "plan", "report"}
	if !reflect.DeepEqual(fixture.events, want) {
		t.Fatalf("events = %#v, want %#v", fixture.events, want)
	}
}

func TestServiceRunNoneDryRunDoesNotPreviewOrHandoff(t *testing.T) {
	t.Parallel()

	fixture := newFixture()
	err := fixture.service.Run(context.Background(), Request{
		Agent: skill.AgentClaude, SetValue: "none", SetPresent: true, DryRun: true,
	}, func(Result) error {
		fixture.record("report")
		return nil
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	want := []string{"read config.toml", "reap", "lookpath", "report"}
	if !reflect.DeepEqual(fixture.events, want) {
		t.Fatalf("events = %#v, want %#v", fixture.events, want)
	}
}

func TestServiceRunReportsReapWarningsWithoutStopping(t *testing.T) {
	t.Parallel()

	fixture := newFixture()
	warningA := errors.New("stale A")
	warningB := errors.New("stale B")
	fixture.sessions.reapWarnings = []error{warningA, warningB}
	var got []error
	err := fixture.service.Run(context.Background(), Request{
		Agent: skill.AgentClaude, SetValue: "none", SetPresent: true, DryRun: true,
	}, func(result Result) error {
		fixture.record("report")
		got = result.Warnings
		return nil
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !reflect.DeepEqual(got, []error{warningA, warningB}) {
		t.Fatalf("warnings = %#v", got)
	}
}

func TestServiceRunFailsClosedBeforeStage(t *testing.T) {
	t.Parallel()

	errConfig := errors.New("config failed")
	errResolver := errors.New("resolver failed")
	errSkillSets := errors.New("skillsets failed")
	errInventory := errors.New("inventory failed")
	errPreview := errors.New("preview failed")

	tests := []struct {
		name      string
		request   Request
		setup     func(*fixture)
		wantError error
		contains  string
		wantLast  string
	}{
		{
			name:     "nil reporter",
			request:  Request{Agent: skill.AgentClaude, SetValue: "none", SetPresent: true},
			contains: "reporter",
		},
		{
			name:      "config",
			request:   Request{Agent: skill.AgentClaude, SetValue: "none", SetPresent: true},
			setup:     func(f *fixture) { f.fsys.errs[f.configPath] = errConfig },
			wantError: errConfig,
			wantLast:  "read config.toml",
		},
		{
			name:      "resolver",
			request:   Request{Agent: skill.AgentClaude, SetValue: "none", SetPresent: true},
			setup:     func(f *fixture) { f.resolver.err = errResolver },
			wantError: errResolver,
			contains:  "[agents.claude] command",
			wantLast:  "lookpath",
		},
		{
			name:      "set required after resolver",
			request:   Request{Agent: skill.AgentClaude},
			wantError: ErrSetRequired,
			wantLast:  "lookpath",
		},
		{
			name:     "selection parse",
			request:  Request{Agent: skill.AgentClaude, SetValue: "none,dev", SetPresent: true},
			contains: "none",
			wantLast: "lookpath",
		},
		{
			name:     "missing skillsets",
			request:  Request{Agent: skill.AgentClaude, SetValue: "dev", SetPresent: true},
			setup:    func(f *fixture) { delete(f.fsys.files, f.skillSetsPath) },
			contains: "skillsets.toml",
			wantLast: "read skillsets.toml",
		},
		{
			name:     "corrupt skillsets",
			request:  Request{Agent: skill.AgentClaude, SetValue: "dev", SetPresent: true},
			setup:    func(f *fixture) { f.fsys.files[f.skillSetsPath] = []byte("not toml =") },
			contains: "skillsets.toml",
			wantLast: "read skillsets.toml",
		},
		{
			name:      "skillsets read",
			request:   Request{Agent: skill.AgentClaude, SetValue: "dev", SetPresent: true},
			setup:     func(f *fixture) { f.fsys.errs[f.skillSetsPath] = errSkillSets },
			wantError: errSkillSets,
			wantLast:  "read skillsets.toml",
		},
		{
			name:     "unknown set",
			request:  Request{Agent: skill.AgentClaude, SetValue: "unknown", SetPresent: true},
			contains: "unknown",
			wantLast: "read skillsets.toml",
		},
		{
			name:     "missing adapter",
			request:  Request{Agent: skill.AgentClaude, SetValue: "dev", SetPresent: true},
			setup:    func(f *fixture) { f.registry.exists = false },
			contains: "adapter",
			wantLast: "registry",
		},
		{
			name:      "inventory",
			request:   Request{Agent: skill.AgentClaude, SetValue: "dev", SetPresent: true},
			setup:     func(f *fixture) { f.adapter.inventoryErr = errInventory },
			wantError: errInventory,
			wantLast:  "inventory",
		},
		{
			name:     "stage",
			request:  Request{Agent: skill.AgentClaude, SetValue: "dev", SetPresent: true},
			setup:    func(f *fixture) { f.sessions.stageErr = errors.New("stage failed") },
			contains: "stage failed",
			wantLast: "stage",
		},
		{
			name:      "preview",
			request:   Request{Agent: skill.AgentClaude, SetValue: "dev", SetPresent: true, DryRun: true},
			setup:     func(f *fixture) { f.sessions.previewErr = errPreview },
			wantError: errPreview,
			wantLast:  "preview",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fixture := newFixture()
			if tt.setup != nil {
				tt.setup(fixture)
			}
			reporter := Reporter(func(Result) error {
				fixture.record("report")
				return nil
			})
			if tt.name == "nil reporter" {
				reporter = nil
			}
			err := fixture.service.Run(context.Background(), tt.request, reporter)
			if err == nil {
				t.Fatal("Run() error = nil")
			}
			if tt.wantError != nil && !errors.Is(err, tt.wantError) {
				t.Fatalf("Run() error = %v, want errors.Is(%v)", err, tt.wantError)
			}
			if tt.contains != "" && !strings.Contains(err.Error(), tt.contains) {
				t.Fatalf("Run() error = %q, want substring %q", err, tt.contains)
			}
			if tt.wantLast != "" && fixture.events[len(fixture.events)-1] != tt.wantLast {
				t.Fatalf("events = %#v, want last %q", fixture.events, tt.wantLast)
			}
			if slices.Contains(fixture.events, "abort") || slices.Contains(fixture.events, "handoff") {
				t.Fatalf("events unexpectedly cleaned or handed off: %#v", fixture.events)
			}
		})
	}
}

func TestServiceRunAbortsAfterEveryPostStageFailureAndJoinsCleanupError(t *testing.T) {
	t.Parallel()

	original := errors.New("original failure")
	cleanup := errors.New("cleanup failure")
	tests := []struct {
		name            string
		setup           func(*fixture)
		wantBeforeAbort string
	}{
		{name: "plan", setup: func(f *fixture) { f.adapter.planErr = original }, wantBeforeAbort: "plan"},
		{name: "write", setup: func(f *fixture) { f.sessions.writeErr = original }, wantBeforeAbort: "write"},
		{name: "publish", setup: func(f *fixture) { f.sessions.publishErr = original }, wantBeforeAbort: "publish"},
		{name: "report", setup: func(f *fixture) { f.reportErr = original }, wantBeforeAbort: "report"},
		{name: "handoff", setup: func(f *fixture) { f.handoff.err = original }, wantBeforeAbort: "handoff"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fixture := newFixture()
			fixture.sessions.abortErr = cleanup
			tt.setup(fixture)
			err := fixture.service.Run(context.Background(), Request{
				Agent: skill.AgentClaude, SetValue: "dev", SetPresent: true,
			}, fixture.reporter)
			if !errors.Is(err, original) || !errors.Is(err, cleanup) {
				t.Fatalf("Run() error = %v, want original and cleanup", err)
			}
			if got := fixture.events[len(fixture.events)-2:]; !reflect.DeepEqual(got, []string{tt.wantBeforeAbort, "abort"}) {
				t.Fatalf("event tail = %#v", got)
			}
			if fixture.sessions.aborted != fixture.sessions.session {
				t.Fatalf("Abort session = %p, want %p", fixture.sessions.aborted, fixture.sessions.session)
			}
		})
	}
}

func TestServiceRunDryRunPlanAndReportFailuresDoNotAbortPreview(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setup func(*fixture)
	}{
		{name: "plan", setup: func(f *fixture) { f.adapter.planErr = errors.New("plan") }},
		{name: "report", setup: func(f *fixture) { f.reportErr = errors.New("report") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fixture := newFixture()
			tt.setup(fixture)
			err := fixture.service.Run(context.Background(), Request{
				Agent: skill.AgentClaude, SetValue: "dev", SetPresent: true, DryRun: true,
			}, fixture.reporter)
			if err == nil {
				t.Fatal("Run() error = nil")
			}
			if slices.Contains(fixture.events, "abort") {
				t.Fatalf("events = %#v, preview must not be aborted", fixture.events)
			}
		})
	}
}

func TestServiceRunChecksContextBeforeExternalSteps(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		cancelAt  string
		wantTail  []string
		wantAbort bool
	}{
		{name: "before config", cancelAt: "before", wantTail: nil},
		{name: "after config", cancelAt: "read config.toml", wantTail: []string{"read config.toml"}},
		{name: "after reap", cancelAt: "reap", wantTail: []string{"reap"}},
		{name: "after resolver", cancelAt: "lookpath", wantTail: []string{"lookpath"}},
		{name: "after skillsets", cancelAt: "read skillsets.toml", wantTail: []string{"read skillsets.toml"}},
		{name: "after registry", cancelAt: "registry", wantTail: []string{"registry"}},
		{name: "after inventory", cancelAt: "inventory", wantTail: []string{"inventory"}},
		{name: "after stage", cancelAt: "stage", wantTail: []string{"stage", "abort"}, wantAbort: true},
		{name: "after plan", cancelAt: "plan", wantTail: []string{"plan", "abort"}, wantAbort: true},
		{name: "after write", cancelAt: "write", wantTail: []string{"write", "abort"}, wantAbort: true},
		{name: "after publish", cancelAt: "publish", wantTail: []string{"publish", "abort"}, wantAbort: true},
		{name: "after report", cancelAt: "report", wantTail: []string{"report", "abort"}, wantAbort: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(context.Background())
			fixture := newFixture()
			fixture.cancelAt = tt.cancelAt
			fixture.cancel = cancel
			if tt.cancelAt == "before" {
				cancel()
			}
			err := fixture.service.Run(ctx, Request{
				Agent: skill.AgentClaude, SetValue: "dev", SetPresent: true,
			}, fixture.reporter)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("Run() error = %v, want context.Canceled", err)
			}
			if len(tt.wantTail) == 0 {
				if len(fixture.events) != 0 {
					t.Fatalf("events = %#v, want none", fixture.events)
				}
				return
			}
			if len(fixture.events) < len(tt.wantTail) || !reflect.DeepEqual(fixture.events[len(fixture.events)-len(tt.wantTail):], tt.wantTail) {
				t.Fatalf("events = %#v, want tail %#v", fixture.events, tt.wantTail)
			}
			if tt.wantAbort != slices.Contains(fixture.events, "abort") {
				t.Fatalf("events = %#v, wantAbort %v", fixture.events, tt.wantAbort)
			}
		})
	}
}

func TestServiceRunReporterCannotMutateHandoffOrCleanupData(t *testing.T) {
	t.Parallel()

	fixture := newFixture()
	fixture.handoff.err = errors.New("handoff")
	originalPlan := clonePlanFixture(fixture.adapter.plan)
	var snapshotAbortErr error
	var snapshotAliased bool
	var snapshotFieldsMatch bool
	err := fixture.service.Run(context.Background(), Request{
		Agent: skill.AgentClaude, SetValue: "dev", SetPresent: true, AgentArgs: []string{"--request"},
	}, func(result Result) error {
		fixture.record("report")
		result.Args[0] = "mutated-arg"
		result.Env[0] = "MUTATED=1"
		result.Warnings[0] = errors.New("mutated warning")
		result.Inventory.Skills[0].ID = "mutated-skill"
		result.Inventory.Skills[0].Locations[0].Names[skill.AgentClaude] = "mutated-name"
		result.Inventory.SkillNames[0] = "mutated-skill-name"
		result.Inventory.PluginIDs[0] = "mutated-plugin"
		result.Inventory.Collisions[0].IDs[0] = "mutated-collision"
		result.Inventory.Collisions[0].Paths[0] = "mutated-path"
		result.Inventory.Warnings[0] = "mutated-inventory-warning"
		result.Resolved.Entries[0].ID = "mutated-resolution"
		result.Resolved.Entries[0].Names[0] = "mutated-resolution-name"
		result.Plan.ControlArgs[0] = "mutated-control"
		result.Plan.Env["CHANGE"] = "mutated"
		result.Plan.Files[0].Data[0] = 'X'
		snapshotAliased = result.Session == fixture.sessions.session
		snapshotFieldsMatch = result.Session.Root == fixture.sessions.session.Root && result.Session.Agent == fixture.sessions.session.Agent
		snapshotAbortErr = result.Session.Abort()
		result.Session.Root = "mutated-root"
		result.Session.Agent = skill.AgentCodex
		return nil
	})
	if !errors.Is(err, fixture.handoff.err) {
		t.Fatalf("Run() error = %v", err)
	}
	wantArgs := []string{"--config", "--request", "--settings", fixture.settingsPath}
	if !reflect.DeepEqual(fixture.handoff.args, wantArgs) {
		t.Fatalf("handoff args mutated = %#v", fixture.handoff.args)
	}
	wantEnv := []string{"ADDED=2", "BASE=1", "CHANGE=new"}
	if !reflect.DeepEqual(fixture.handoff.env, wantEnv) {
		t.Fatalf("handoff env mutated = %#v", fixture.handoff.env)
	}
	if !reflect.DeepEqual(fixture.adapter.plan, originalPlan) {
		t.Fatalf("adapter plan mutated = %#v, want %#v", fixture.adapter.plan, originalPlan)
	}
	if fixture.adapter.inventory.Skills[0].ID != "one" || fixture.adapter.inventory.Skills[0].Locations[0].Names[skill.AgentClaude] != "one" {
		t.Fatalf("adapter inventory mutated = %#v", fixture.adapter.inventory)
	}
	if fixture.sessions.aborted != fixture.sessions.session {
		t.Fatalf("cleanup session = %p, want original %p", fixture.sessions.aborted, fixture.sessions.session)
	}
	if snapshotAliased || !snapshotFieldsMatch {
		t.Fatalf("reported Session aliases live session or lost display fields: aliased=%v fieldsMatch=%v", snapshotAliased, snapshotFieldsMatch)
	}
	if snapshotAbortErr == nil || !strings.Contains(snapshotAbortErr.Error(), "session is not managed") {
		t.Fatalf("reported Session Abort() error = %v, want not managed", snapshotAbortErr)
	}
	if fixture.sessions.session.Root != "session-root" || fixture.sessions.session.Agent != skill.AgentClaude {
		t.Fatalf("live session mutated = %#v", fixture.sessions.session)
	}
}

func TestSummarizeCountsNativeAndReturnsMissingInResolutionOrder(t *testing.T) {
	t.Parallel()

	resolved := skill.Resolved{Entries: []skill.Resolution{
		{ID: "native-a", State: skill.StateNative},
		{ID: "missing-b", State: skill.StateMissing},
		{ID: "projected-c", State: skill.StateProjected},
		{ID: "missing-d", State: skill.StateMissing},
		{ID: "native-e", State: skill.StateNative},
	}}
	got := Summarize(resolved)
	if got.Native != 2 || !reflect.DeepEqual(got.Missing, []string{"missing-b", "missing-d"}) {
		t.Fatalf("Summarize() = %#v", got)
	}
	got.Missing[0] = "mutated"
	if resolved.Entries[1].ID != "missing-b" {
		t.Fatal("Summarize() returned aliased Missing data")
	}
}

type fixture struct {
	events        []string
	cancelAt      string
	cancel        context.CancelFunc
	configPath    string
	skillSetsPath string
	settingsPath  string
	fsys          *fakeFS
	registry      *fakeRegistry
	resolver      *fakeResolver
	sessions      *fakeSessions
	adapter       *fakeAdapter
	handoff       *fakeHandoff
	service       *Service
	reportErr     error
}

func newFixture() *fixture {
	f := &fixture{}
	f.configPath = filepath.Join("skope-home", "config.toml")
	f.skillSetsPath = filepath.Join("skope-home", "skillsets.toml")
	f.settingsPath = filepath.Join("session-root", "claude", "settings.json")
	f.fsys = &fakeFS{fixture: f, files: map[string][]byte{
		f.configPath:    []byte("version = 1\n[agents.claude]\ncommand = \"configured-claude\"\nargs = [\"--config\"]\n"),
		f.skillSetsPath: []byte("version = 1\n[skillsets.dev]\nskills = [\"one\", \"missing\"]\n"),
	}, errs: make(map[string]error)}
	f.adapter = &fakeAdapter{fixture: f}
	f.adapter.inventory = completeInventory()
	f.adapter.plan = agent.LaunchPlan{
		ControlArgs: []string{"--settings", f.settingsPath},
		Env:         map[string]string{"CHANGE": "new", "ADDED": "2"},
		Files:       []agent.PlannedFile{{Path: f.settingsPath, Data: []byte("settings"), Mode: 0o600}},
	}
	f.registry = &fakeRegistry{fixture: f, adapter: f.adapter, exists: true}
	f.resolver = &fakeResolver{fixture: f, path: filepath.Join("bin", "claude")}
	f.sessions = &fakeSessions{
		fixture:      f,
		session:      &session.Session{Root: "session-root", Agent: skill.AgentClaude},
		reapWarnings: []error{errors.New("stale warning")},
	}
	f.handoff = &fakeHandoff{fixture: f}
	f.service = &Service{
		Env:            host.NewEnv("user-home", "cwd", map[string]string{"BASE": "1", "CHANGE": "old"}),
		FS:             f.fsys,
		SkopeHome:      "skope-home",
		NewRegistry:    func(string, config.Selection) (AdapterRegistry, error) { f.record("factory"); return f.registry, nil },
		CheckConflicts: func(skill.Agent, []string, []string) error { f.record("conflict"); return nil },
		Resolver:       f.resolver,
		Sessions:       f.sessions,
		Handoff:        f.handoff,
	}
	return f
}

func (f *fixture) record(event string) {
	f.events = append(f.events, event)
	if f.cancelAt == event && f.cancel != nil {
		f.cancel()
	}
}

func (f *fixture) reporter(_ Result) error {
	f.record("report")
	return f.reportErr
}

type fakeFS struct {
	fixture *fixture
	files   map[string][]byte
	errs    map[string]error
}

func (f *fakeFS) ReadFile(path string) ([]byte, error) {
	f.fixture.record("read " + filepath.Base(path))
	if err := f.errs[path]; err != nil {
		return nil, err
	}
	data, ok := f.files[path]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return append([]byte(nil), data...), nil
}

type fakeRegistry struct {
	fixture *fixture
	adapter agent.Adapter
	exists  bool
}

func (f *fakeRegistry) Get(skill.Agent) (agent.Adapter, bool) {
	f.fixture.record("registry")
	return f.adapter, f.exists
}

type fakeResolver struct {
	fixture *fixture
	command string
	path    string
	err     error
}

func (f *fakeResolver) LookPath(command string) (string, error) {
	f.fixture.record("lookpath")
	f.command = command
	return f.path, f.err
}

type fakeSessions struct {
	fixture      *fixture
	session      *session.Session
	reapWarnings []error
	previewErr   error
	stageErr     error
	writeErr     error
	publishErr   error
	abortErr     error
	files        []session.File
	aborted      *session.Session
	stageAgent   skill.Agent
	stageDisplay string
	written      *session.Session
	published    *session.Session
}

func (f *fakeSessions) Reap() []error {
	f.fixture.record("reap")
	return append([]error(nil), f.reapWarnings...)
}

func (f *fakeSessions) Preview(skill.Agent, string) (*session.Session, error) {
	f.fixture.record("preview")
	return f.session, f.previewErr
}

func (f *fakeSessions) Stage(agent skill.Agent, displayName string) (*session.Session, error) {
	f.fixture.record("stage")
	f.stageAgent = agent
	f.stageDisplay = displayName
	return f.session, f.stageErr
}

func (f *fakeSessions) Write(sess *session.Session, files []session.File) error {
	f.fixture.record("write")
	f.written = sess
	f.files = cloneSessionFiles(files)
	return f.writeErr
}

func (f *fakeSessions) Publish(sess *session.Session) error {
	f.fixture.record("publish")
	f.published = sess
	return f.publishErr
}

func (f *fakeSessions) Abort(sess *session.Session) error {
	f.fixture.record("abort")
	f.aborted = sess
	return f.abortErr
}

type fakeAdapter struct {
	fixture       *fixture
	inventory     agent.Inventory
	inventoryErr  error
	plan          agent.LaunchPlan
	planErr       error
	planResolved  skill.Resolved
	planInventory agent.Inventory
	planSession   *session.Session
}

func (*fakeAdapter) Name() skill.Agent { return skill.AgentClaude }

func (*fakeAdapter) Capabilities() agent.Capabilities { return agent.Capabilities{} }

func (f *fakeAdapter) Inventory(context.Context, host.Env) (agent.Inventory, error) {
	f.fixture.record("inventory")
	return f.inventory, f.inventoryErr
}

func (f *fakeAdapter) Plan(resolved skill.Resolved, inventory agent.Inventory, sess *session.Session) (agent.LaunchPlan, error) {
	f.fixture.record("plan")
	f.planResolved = resolved
	f.planInventory = inventory
	f.planSession = sess
	return f.plan, f.planErr
}

type fakeHandoff struct {
	fixture *fixture
	path    string
	args    []string
	env     []string
	err     error
}

func (f *fakeHandoff) Exec(path string, args, env []string) error {
	f.fixture.record("handoff")
	f.path = path
	f.args = append([]string(nil), args...)
	f.env = append([]string(nil), env...)
	return f.err
}

func completeInventory() agent.Inventory {
	return agent.Inventory{
		Skills: []skill.Skill{{ID: "one", Locations: []skill.Location{{
			DiscoveryPath: "one/SKILL.md",
			Names:         map[skill.Agent]string{skill.AgentClaude: "one"},
		}}}},
		SkillNames: []string{"configured-name"},
		PluginIDs:  []string{"plugin@marketplace"},
		Collisions: []skill.Collision{{
			Kind: skill.CollisionDifferentContent, IDs: []string{"one", "other"}, Paths: []string{"one", "other"},
		}},
		Warnings: []string{"inventory warning"},
	}
}

func clonePlanFixture(source agent.LaunchPlan) agent.LaunchPlan {
	cloned := agent.LaunchPlan{
		ControlArgs: append([]string(nil), source.ControlArgs...),
		Env:         make(map[string]string, len(source.Env)),
		Files:       make([]agent.PlannedFile, len(source.Files)),
	}
	for key, value := range source.Env {
		cloned.Env[key] = value
	}
	for i, file := range source.Files {
		cloned.Files[i] = file
		cloned.Files[i].Data = append([]byte(nil), file.Data...)
	}
	return cloned
}

func cloneSessionFiles(source []session.File) []session.File {
	cloned := make([]session.File, len(source))
	for i, file := range source {
		cloned[i] = file
		cloned[i].Data = append([]byte(nil), file.Data...)
	}
	return cloned
}

func TestServiceChecksConflictsBeforeFactory(t *testing.T) {
	f := newFixture()
	want := errors.New("conflicting flag")
	f.service.CheckConflicts = func(agent skill.Agent, configArgs, userArgs []string) error {
		if agent != skill.AgentClaude || !reflect.DeepEqual(configArgs, []string{"--config"}) || !reflect.DeepEqual(userArgs, []string{"--request"}) {
			t.Fatalf("inputs=%s %v %v", agent, configArgs, userArgs)
		}
		return want
	}
	err := f.service.Run(context.Background(), Request{Agent: skill.AgentClaude, SetPresent: true, SetValue: "dev", AgentArgs: []string{"--request"}}, f.reporter)
	if !errors.Is(err, want) || !reflect.DeepEqual(f.events, []string{"read config.toml", "reap", "lookpath", "read skillsets.toml"}) {
		t.Fatalf("err=%v events=%v", err, f.events)
	}
}
func TestServiceFactoryReceivesLaunchSelectionCopy(t *testing.T) {
	f := newFixture()
	f.fsys.files[f.skillSetsPath] = []byte("version=1\n[skillsets.dev]\nskills=['one']\nbundled=false\n[skillsets.dev.plugins]\nclaude=['p@m']\n[skillsets.ops]\nskills=['one']\nbundled=true\n[skillsets.ops.plugins]\nclaude=['q@m','p@m']\n")
	f.service.NewRegistry = func(executable string, selection config.Selection) (AdapterRegistry, error) {
		if executable != f.resolver.path || !selection.Bundled || !reflect.DeepEqual(selection.Plugins["claude"], []string{"p@m", "q@m"}) {
			t.Fatalf("factory inputs=%s %#v", executable, selection)
		}
		selection.Skills[0] = "changed"
		selection.Plugins["claude"][0] = "changed"
		delete(selection.Plugins, "claude")
		return f.registry, nil
	}
	err := f.service.Run(context.Background(), Request{Agent: skill.AgentClaude, SetPresent: true, SetValue: "dev,ops", DryRun: true}, func(result Result) error {
		if len(result.Resolved.Entries) != 1 || result.Resolved.Entries[0].ID != "one" {
			t.Fatalf("resolved=%#v", result.Resolved)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
func TestServiceRequiresActiveFactoryAndCheckerButNoneBypasses(t *testing.T) {
	for _, dep := range []string{"factory", "checker"} {
		t.Run(dep, func(t *testing.T) {
			f := newFixture()
			if dep == "factory" {
				f.service.NewRegistry = nil
			} else {
				f.service.CheckConflicts = nil
			}
			err := f.service.Run(context.Background(), Request{Agent: skill.AgentClaude, SetPresent: true, SetValue: "dev", DryRun: true}, f.reporter)
			if err == nil || !strings.Contains(err.Error(), "configuration") {
				t.Fatalf("error=%v", err)
			}
			err = f.service.Run(context.Background(), Request{Agent: skill.AgentClaude, SetPresent: true, SetValue: "none", DryRun: true}, f.reporter)
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestServiceFactoryFailureStopsBeforeInventory(t *testing.T) {
	f := newFixture()
	want := errors.New("factory failed")
	f.service.NewRegistry = func(string, config.Selection) (AdapterRegistry, error) { f.record("factory"); return nil, want }
	err := f.service.Run(context.Background(), Request{Agent: skill.AgentClaude, SetPresent: true, SetValue: "dev"}, f.reporter)
	if !errors.Is(err, want) || !reflect.DeepEqual(f.events, []string{"read config.toml", "reap", "lookpath", "read skillsets.toml", "conflict", "factory"}) {
		t.Fatalf("error=%v events=%v", err, f.events)
	}
}
