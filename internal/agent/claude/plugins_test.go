package claude_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/scarb/skope/internal/agent/claude"
	"github.com/scarb/skope/internal/host"
	"github.com/scarb/skope/internal/proc"
	"github.com/scarb/skope/internal/skill"
)

func TestInventoryRequiresConfiguredProbe(t *testing.T) {
	a := claude.New(&fakeScanner{result: skill.ScanResult{ProjectRoot: "/repo"}}, missingFS{}, nil, claude.Options{})
	_, err := a.Inventory(context.Background(), host.NewEnv("/home", "/repo", nil))
	if err == nil || !strings.Contains(err.Error(), "configuration") {
		t.Fatalf("error = %v, want configuration error", err)
	}
}

type probeStub struct {
	stdout   string
	err      error
	requests []proc.Request
}

func (r *probeStub) Run(_ context.Context, req proc.Request) (proc.Result, error) {
	r.requests = append(r.requests, req)
	return proc.Result{Stdout: []byte(r.stdout)}, r.err
}

func TestInventoryRejectsInvalidPluginList(t *testing.T) {
	for _, body := range []string{`null`, `{}`, `[] {}`, `[`,
		`[{"enabled":true,"scope":"user","installPath":"/x"}]`,
		`[{"id":"p@m","scope":"user","installPath":"/x"}]`,
		`[{"id":"p@m","enabled":null,"scope":"user","installPath":"/x"}]`,
		`[{"id":"p@m","enabled":"true","scope":"user","installPath":"/x"}]`,
		`[{"id":"p@m","enabled":true,"installPath":"/x"}]`,
		`[{"id":"p@@m","enabled":true,"scope":"user","installPath":"/x"}]`,
		`[{"id":"p@m","enabled":true,"scope":"unknown","installPath":"/x"}]`,
		`[{"id":"p@m","enabled":true,"scope":"user","installPath":""}]`,
		`[{"id":"p@m","enabled":true,"scope":"project","installPath":"relative"}]`,
	} {
		t.Run(body, func(t *testing.T) {
			a := claude.New(&fakeScanner{result: skill.ScanResult{ProjectRoot: "/repo"}}, missingFS{}, &probeStub{stdout: body}, claude.Options{Executable: "fixture"})
			if _, err := a.Inventory(context.Background(), host.NewEnv("/home", "/repo", nil)); err == nil {
				t.Fatal("accepted invalid list")
			}
		})
	}
}
func TestInventoryCollectsPluginSettingsAndWarnsForMissingInstalled(t *testing.T) {
	fsys := &mapReadFileFS{files: map[string]string{
		"/home/.claude/settings.json":       `{"enabledPlugins":{"z@m":false,"shared@m":true}}`,
		"/repo/.claude/settings.json":       `{"enabledPlugins":{"a@m":true,"shared@m":false}}`,
		"/repo/.claude/settings.local.json": `{"enabledPlugins":{"middle@m":true}}`,
	}}
	runner := &probeStub{stdout: "[]"}
	opts := claude.Options{Executable: "resolved-claude", Plugins: []string{"shared@m", "absent@m"}, Bundled: true}
	a := claude.New(&fakeScanner{result: skill.ScanResult{ProjectRoot: "/repo"}}, fsys, runner, opts)
	opts.Plugins[0] = "changed@m"
	env := host.NewEnv("/home", "/repo/work", map[string]string{"KEEP": "yes"})
	got, err := a.Inventory(context.Background(), env)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got.PluginIDs, ",") != "a@m,middle@m,shared@m,z@m" {
		t.Fatalf("IDs=%v", got.PluginIDs)
	}
	if len(got.Warnings) != 2 || !strings.Contains(got.Warnings[0], "shared@m") {
		t.Fatalf("warnings=%v", got.Warnings)
	}
	want := proc.Request{Executable: "resolved-claude", Args: []string{"plugin", "list", "--json"}, Dir: env.Cwd(), Env: env.Environ()}
	if len(runner.requests) != 1 || !reflect.DeepEqual(runner.requests[0], want) {
		t.Fatalf("requests=%#v", runner.requests)
	}
	got.PluginIDs[0] = "changed"
	got.Warnings[0] = "changed"
	next, err := a.Inventory(context.Background(), env)
	if err != nil || next.PluginIDs[0] != "a@m" || strings.Contains(next.Warnings[0], "changed") {
		t.Fatalf("mutated inventory=%#v %v", next, err)
	}
}
func TestInventoryRejectsMalformedEnabledPlugins(t *testing.T) {
	for _, value := range []string{`null`, `[]`, `"all"`, `{"p@m":null}`, `{"p@m":"true"}`, `{"p@m":1}`} {
		t.Run(value, func(t *testing.T) {
			a := newTestAdapter(&fakeScanner{result: skill.ScanResult{ProjectRoot: "/repo"}}, &mapReadFileFS{files: map[string]string{"/home/.claude/settings.json": `{"enabledPlugins":` + value + `}`}})
			_, err := a.Inventory(context.Background(), host.NewEnv("/home", "/repo", nil))
			if err == nil || !strings.Contains(err.Error(), "enabledPlugins") || !strings.Contains(err.Error(), "settings.json") {
				t.Fatalf("error=%v", err)
			}
		})
	}
}
