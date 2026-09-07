package cli_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/scarb/skope/internal/cli"
	"github.com/scarb/skope/internal/config"
	"github.com/scarb/skope/internal/host"
	"github.com/scarb/skope/internal/launch"
	"github.com/scarb/skope/internal/skill"
)

func TestCheckConflictsCodexConfiguration(t *testing.T) {
	for _, key := range []string{"skills", "skills.config", "plugins", `plugins."p@m".enabled`, "features", "features.remote_plugin", "features.remote_plugin.child", "profile", "profile.child", "profiles", "profiles.dev.model", "project_root_markers", "project_root_markers.child", "marketplaces", "marketplaces.local", " features.remote_plugin "} {
		for _, form := range [][]string{{"-c", key + "=SECRET_SENTINEL=a=b"}, {"-c=" + key + "=SECRET_SENTINEL"}, {"-c" + key + "=SECRET_SENTINEL"}, {"--config", key + "=SECRET_SENTINEL"}, {"--config=" + key + "=SECRET_SENTINEL"}} {
			for _, source := range []string{"config", "command-line"} {
				t.Run(key+form[0]+source, func(t *testing.T) {
					before := append([]string(nil), form...)
					var config, user []string
					if source == "config" {
						config = form
					} else {
						user = form
					}
					err := cli.CheckConflicts(skill.AgentCodex, config, user)
					var conflict *cli.ConflictError
					flag := "-c"
					if strings.HasPrefix(form[0], "--config") {
						flag = "--config"
					}
					if !errors.As(err, &conflict) || conflict.Flag != flag || conflict.Source != source || conflict.Agent != skill.AgentCodex || conflict.ValueSource != "" {
						t.Fatalf("error=%v", err)
					}
					if strings.Contains(err.Error(), "SECRET_SENTINEL") || strings.Contains(err.Error(), key+"=") || !strings.Contains(err.Error(), "Codex") {
						t.Fatalf("unsafe error=%v", err)
					}
					if !reflect.DeepEqual(before, form) {
						t.Fatal("mutated input")
					}
				})
			}
		}
	}
}

func TestCodexConflictFlagsAndMalformedValues(t *testing.T) {
	for _, args := range [][]string{
		{"-C", "SECRET_SENTINEL"}, {"-C=SECRET_SENTINEL"}, {"-CSECRET_SENTINEL"}, {"--cd", "SECRET_SENTINEL"}, {"--cd=SECRET_SENTINEL"},
		{"-p", "SECRET_SENTINEL"}, {"-p=SECRET_SENTINEL"}, {"-pSECRET_SENTINEL"}, {"--profile", "SECRET_SENTINEL"}, {"--profile=SECRET_SENTINEL"}, {"--"},
		{"--enable", "remote_plugin"}, {"--enable=remote_plugin"}, {"--disable", "remote_plugin"}, {"--disable=remote_plugin"},
		{"-c"}, {"--config"}, {"-c", ""}, {"-c="}, {"--config="}, {"-c", "SECRET_SENTINEL"}, {"-c", "=SECRET_SENTINEL"},
		{"--enable"}, {"--disable="},
	} {
		for _, source := range []string{"config", "command-line"} {
			t.Run(strings.Join(args, " ")+source, func(t *testing.T) {
				var config, user []string
				if source == "config" {
					config = args
				} else {
					user = args
				}
				err := cli.CheckConflicts(skill.AgentCodex, config, user)
				if err == nil || strings.Contains(err.Error(), "SECRET_SENTINEL") || !strings.Contains(err.Error(), source) {
					t.Fatalf("error=%v", err)
				}
			})
		}
	}
}

func TestCodexConflictAllowsUnrelatedKeysAndConsumedValues(t *testing.T) {
	for _, args := range [][]string{
		{"-c", "skills_extra=SECRET_SENTINEL"}, {"-c", "my.skills=[]"}, {"-c", "model=--cd"}, {"-c", "model_reasoning_effort=high"},
		{"-c", "features.other=true"}, {"-c", "features .remote_plugin=false"}, {"-c", "features. remote_plugin=false"}, {"-c", `"features".remote_plugin=false`}, {"-c", `features."remote_plugin"=false`}, {"-c", `"plugins".p=true`},
		{"--enable", "--cd"}, {"--disable=other"}, {"--enable", "other"}, {"explain --cd and --profile"}, {"skills.config=[]"}, {"--model", "--cd"}, {"--model=--profile"}, {"-m", "--profile"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			for _, config := range []bool{false, true} {
				var c, u []string
				if config {
					c = args
				} else {
					u = args
				}
				if err := cli.CheckConflicts(skill.AgentCodex, c, u); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestCodexConflictCrossSourceValue(t *testing.T) {
	config, user := []string{"-c"}, []string{"skills.config=SECRET_SENTINEL"}
	err := cli.CheckConflicts(skill.AgentCodex, config, user)
	var conflict *cli.ConflictError
	if !errors.As(err, &conflict) || conflict.Flag != "-c" || conflict.Source != "config" || !strings.Contains(err.Error(), "command-line") || strings.Contains(err.Error(), "SECRET_SENTINEL") {
		t.Fatalf("error=%v", err)
	}
	if !reflect.DeepEqual(config, []string{"-c"}) || !reflect.DeepEqual(user, []string{"skills.config=SECRET_SENTINEL"}) {
		t.Fatal("mutated inputs")
	}
	if err := cli.CheckConflicts(skill.AgentCodex, []string{"-c"}, []string{"model=--profile"}); err != nil {
		t.Fatal(err)
	}
}

// Only Reap is legal before checking conflicts; other session calls panic.
type codexConflictSessions struct{ launch.SessionManager }

func (codexConflictSessions) Reap() []error { return nil }

type codexConflictResolver struct{}

func (codexConflictResolver) LookPath(string) (string, error) { return "fixture-codex", nil }

type codexConflictFS struct{}

func (codexConflictFS) ReadFile(path string) ([]byte, error) { return os.ReadFile(path) }

func TestCodexConflictServiceStopsBeforeIsolationAndNoneBypasses(t *testing.T) {
	home := t.TempDir()
	for name, content := range map[string]string{
		"config.toml":    "version=1\n[agents.codex]\nargs=['-c']\n",
		"skillsets.toml": "version=1\n[skillsets.dev]\nskills=[]\n",
	} {
		if err := os.WriteFile(filepath.Join(home, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	checks := 0
	service := launch.Service{
		Env: host.NewEnv(home, home, nil), FS: codexConflictFS{}, SkopeHome: home,
		Resolver: codexConflictResolver{}, Sessions: codexConflictSessions{},
		CheckConflicts: func(agent skill.Agent, c, u []string) error { checks++; return cli.CheckConflicts(agent, c, u) },
		NewRegistry: func(string, config.Selection) (launch.AdapterRegistry, error) {
			t.Fatal("factory called before conflict rejection")
			return nil, nil
		},
	}
	err := service.Run(context.Background(), launch.Request{Agent: skill.AgentCodex, SetPresent: true, SetValue: "dev", AgentArgs: []string{"skills.config=SECRET_SENTINEL"}}, func(launch.Result) error { t.Fatal("reported rejected launch"); return nil })
	var conflict *cli.ConflictError
	if !errors.As(err, &conflict) || conflict.Source != "config" || conflict.ValueSource != "command-line" || conflict.Agent != skill.AgentCodex || strings.Contains(err.Error(), "SECRET_SENTINEL") {
		t.Fatalf("error=%v", err)
	}
	err = service.Run(context.Background(), launch.Request{Agent: skill.AgentCodex, SetPresent: true, SetValue: "none", DryRun: true, AgentArgs: []string{"skills.config=SECRET_SENTINEL"}}, func(result launch.Result) error {
		if !result.NoIsolation || !reflect.DeepEqual(result.Args, []string{"-c", "skills.config=SECRET_SENTINEL"}) {
			t.Fatalf("unexpected none result: %#v", result)
		}
		return nil
	})
	if err != nil || checks != 1 {
		t.Fatalf("none error=%v checks=%d", err, checks)
	}
}
func TestCodexConflictMalformedCrossSourceKeepsOnlySourceLabels(t *testing.T) {
	err := cli.CheckConflicts(skill.AgentCodex, []string{"-c"}, []string{"SECRET_SENTINEL"})
	if err == nil || !strings.Contains(err.Error(), "config") || !strings.Contains(err.Error(), "command-line") || strings.Contains(err.Error(), "SECRET_SENTINEL") {
		t.Fatalf("error=%v", err)
	}
}
